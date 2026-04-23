# Sessions and steering

A session is a conversation. The being has many — each scoped to a working directory and a role, each with its own message history. Sessions don't own identity; they borrow it from the shared DB. But they own conversation.

---

## A turn's lifecycle

When you type a message at the prompt, this happens:

1. **Drain the background inbox.** Any background tasks that reached a terminal state since the last turn get formatted as a single `[background] while you were idle…` note, persisted as a system-role message, and prepended to the history. (Each task surfaces at most once; `tasks.reported_at` tracks this.)
2. **Persist your message.** Written to `messages` as `role="user"`.
3. **Rebuild the system prompt.** Fresh from DB state. See [Identity](identity.md) for the composition order.
4. **Stream from the LLM.** `StreamOnce` — one HTTP request to Ollama, streaming chunks back.
5. **Inner loop: tool calls.** If the stream contains tool calls, execute them one by one, persist each call and each result, then re-stream with the updated history. Repeat until the model emits text without further tool calls.
6. **Persist final text.** Written to `messages` as `role="assistant"`.
7. **Drain the steering queue.** If you typed anything mid-turn, those messages get appended now and we loop back to step 4 for another pass.
8. **Drain the follow-up queue.** If any code queued a post-idle instruction (the `/init` flow does this), those messages run next and loop.
9. **Background summarizer.** A goroutine kicks off — if `summary_threshold` unsummarized messages have accumulated, write a summary + facts.
10. **Return.** The prompt re-appears. Next turn.

The outer loop (steering + follow-up) is what makes multi-turn work feel single-turn — you can type a correction while Selene is mid-stream, and it gets queued rather than dropped.

---

## Steering

**Steering** is an async injection of a user message while a turn is running. In the CLI it's awkward; in the TUI it's natural — you keep typing, the text lands in the input field, and it gets queued rather than dropped.

```go
// internal/agent/agent.go
func (a *Agent) Steer(ctx context.Context, text string) error {
    if a.streaming {
        a.steerQueue = append(a.steerQueue, ...)
        return nil
    }
    return a.Prompt(ctx, text)  // idle → run immediately
}
```

Steering messages land in the conversation after the current turn's final text, and the model sees them at the start of the next inner loop. The model can't be interrupted mid-token; it can be redirected between turns. This matches how a thoughtful human pauses at a paragraph break to reread feedback.

---

## Follow-up

**Follow-up** is an instruction that runs *after* the agent would otherwise go idle. The `/init` flow uses this:

```
user types /init → CLI sends the init skill as a prompt
                    and ALSO queues a follow-up:
                    "Storage step: call memory(action='add') for every fact..."

→ model runs the init conversation, maybe forgets to store some things
→ model emits final text, would become idle
→ runLoop drains follow-up queue:
    follow-up message appended, inner loop re-runs
    model is now instructed to do the storage step
→ eventually idle for real
```

Follow-ups are how Cairo chains a deterministic post-condition onto an otherwise-conversational turn. They're not a general-purpose tool — there's only one queue, and it's drained after steering. But for flows where "make sure X happens at the end" matters, it's the clean hook.

---

## Resume across restarts

Sessions survive process restarts. When `Agent.loadHistory` runs, it pulls every `messages` row for the session (unsummarized only — summarized ones live in the prompt as context rather than raw turns) and reconstructs the `llm.Message` slice:

- User messages → `{Role: "user", Content: ...}`
- Assistant text → `{Role: "assistant", Content: ...}`
- Assistant tool-call requests → `{Role: "assistant", ToolCalls: [...]}` — reconstructed from the `tool_calls` JSON column
- Tool results → `{Role: "tool", Content: ...}`
- Background-inbox notes → `{Role: "system", Content: ...}`

The next turn has full context, including mid-turn tool calls and their results.

**Resume vs new session:**
- `cairo` — resume the most recent session for this cwd
- `cairo -new` — start a fresh session
- `cairo -session 42` — resume a specific session by id
- `cairo -new -name "spike"` — new session with a label

---

## Summaries and unsummarized-only history

Not every turn in a long session is pasted into every future prompt. The summarizer periodically condenses old turns into paragraph-sized summary rows, marks those messages `summarized = 1`, and `loadHistory` skips them.

From the LLM's perspective, a long conversation looks like:

```
system prompt:
  base + soul + role + tools + memories
  ## Conversation context
  [Apr 20 14:22] We built the export/import flow today...
  [Apr 20 16:40] Then moved on to the TUI panels...
  [Apr 21 09:05] Started adding live counts to the status bar...
  [Apr 21 11:30] Fixed the OSC 10/11 filter...
---
history:
  [only unsummarized messages from this session]
```

Summarized turns are still in the DB — nothing is deleted — they just aren't replayed as raw messages. If you need to pull them back, `summary_search(query=...)` finds them by similarity.

See [Memory model](memory-model.md) for more on the summarizer.

---

## Session deletion

`session(action="delete", id=N)` removes a session and cascades through:
- `messages` (ON DELETE CASCADE)
- `summaries` (ON DELETE CASCADE, via `session_id`)
- `facts` (ON DELETE CASCADE)
- `jobs`, `tasks`, `task_artifacts` (CASCADE where wired)

Memories and skills are not session-scoped, so they survive.

---

## Known rough edges

- **Mid-turn cancel is coarse.** Ctrl-C in the TUI cancels the in-flight LLM request; the model produces "partial text + (interrupted)" and the turn ends. Tool calls between cancel points are not interruptible.
- **Steering is not preserved on process kill.** If you queue a steering message and then kill the process before the inner loop gets to it, it's lost. Messages are persisted as they execute, not as they're queued.
- **Background-task inbox uses a simple once-and-only-once policy.** If you kill the process *during* inbox drain (between `tasks.UnreportedCompleted` and `tasks.MarkReported`), a task can surface twice. Rare; not catastrophic.
