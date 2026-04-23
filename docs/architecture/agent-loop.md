# Agent loop

The agent is the heart of the turn: it takes a user message, assembles a prompt, streams a response from the LLM, executes any tool calls, and persists everything. This doc covers how that code is shaped.

Source: `internal/agent/agent.go`, `internal/agent/loop.go`.

---

## Two layers: `Agent` and `runLoop`

```
  Agent (state, queues, history)      ← owns the session's working state
     │
     └──▶ runLoop (pure function)     ← pure turn lifecycle, no state
                │
                └──▶ llm.StreamOnce   ← one HTTP request, streaming response
```

**`Agent`** holds the per-session state — message history, streaming flag, steering and follow-up queues, event bus, a WaitGroup for background summarizer goroutines. It's the thing the CLI/TUI holds and calls `Prompt()` on.

**`runLoop`** is a pure function with no session concept — you give it a history slice, a tool list, closures for persistence and prompt-building, and it runs a complete turn. Callable in tests, re-usable outside a full Agent if you want.

This split matters: `runLoop` is testable without faking a DB or an LLM, and `Agent` is thin enough to reason about without wading through turn logic.

---

## Outer loop, inner loop

`runLoop` is actually two nested loops:

```
outer loop:
  rebuild system prompt from DB
  inner loop:
    StreamOnce → if tool calls: execute + persist, loop
                 else: persist final text, break inner

  drain steering queue (mid-turn user messages)
  drain follow-up queue (post-idle instructions)
  if either drained: loop outer
  else: done
```

**Why two loops?**

- **Inner loop** handles the tool-call iteration. The model may call tools, see their results, call more tools, and only then emit final text. Each iteration is one `StreamOnce` call.
- **Outer loop** handles user and system interventions between "done" states. Steering messages (typed mid-turn) and follow-ups (queued by `/init`) get merged in between inner-loop runs.

The system prompt is rebuilt fresh at the start of every outer iteration — so if the model edited a memory, or the soul, or a prompt part during the previous iteration, the change is live on the next pass without needing a process restart.

---

## Tool-call persistence

Every intermediate message is persisted:

1. **User message** — `role=user`, stored at the start of the turn.
2. **Assistant tool-call request** — `role=assistant`, `content=""`, `tool_calls=[{id, name, args}, ...]` as JSON. One row per request, which may carry multiple calls.
3. **Tool result** — `role=tool`, `content=<result>`, `tool_name`, `tool_id`. One row per tool call.
4. **Final assistant text** — `role=assistant`, `content=<text>`. One row per turn.

When `loadHistory` runs at session resume, it reconstructs the `llm.Message` slice from these rows — including the `tool_calls` array on assistant messages — so the next turn has full context of what was called and what came back.

This matters because "the DB is the being" is only as true as the DB is complete. If tool calls weren't persisted, every process restart would amnesia tool state and the model would re-read files, re-run greps, re-discover things.

---

## The event bus

`internal/agent/events.go`. A fan-out event publisher with these events:

```go
EventAgentStart / EventAgentEnd      // per-Prompt()
EventTurnStart / EventTurnEnd        // per outer-loop iteration
EventTokens                          // streaming content chunk
EventThinking                        // streaming thinking chunk (model reasoning)
EventToolStart / EventToolEnd        // tool lifecycle
EventToolUpdate                      // progress during a long tool
EventError                           // anything unrecoverable
```

Subscribers get a buffered (64) channel. `Publish` is non-blocking — if a subscriber's buffer is full, the event is dropped for that subscriber only.

This pattern is how three different UIs plug into the same `runLoop`:

- `internal/cli/renderer.go` — line-by-line stdout rendering
- `internal/tui/tui.go` — Bubble Tea `handleEvent` updates the transcript
- `internal/cli/background.go` — writes tool events to a background task log

None of them know about each other; each just subscribes.

### Drop tradeoff

Non-blocking publish + fixed buffer size means slow subscribers lose events rather than stalling the agent. For UI this is correct — a lagging renderer shouldn't freeze generation. For `cmd/cairo/main.go:collectArtifacts` (which pairs tool-start events with tool-end events by goroutine-local state) this is fragile: a dropped event mis-pairs the next one.

The known rough edge is documented in [ROADMAP](../../ROADMAP.md) under near-term — per-subscriber queues with configurable depth would address it.

---

## Streaming and cancellation

`StreamOnce` takes a `context.Context` and propagates it into the HTTP request. Cancelling the context aborts the in-flight stream; whatever tokens had arrived are returned along with `ctx.Err()`, so the caller can persist the partial response rather than discarding it.

In `runLoop`, a cancel is detected via `ctx.Err() != nil` and handled by:

1. Persisting the partial text with an `(interrupted)` suffix so the transcript reads as "Selene started a thought and stopped" rather than "Selene finished that sentence."
2. Publishing `EventTurnEnd` so UIs reset their streaming state.
3. Returning `nil` (not an error) — cancel is a user-initiated outcome, not a failure.

Cancellation is also checked between tool calls in a long chain, so Ctrl-C during "read five files in a row" doesn't wait for all five to finish.

---

## Steering and follow-up queues

Both queues live on the `Agent`, guarded by a mutex.

**Steering** (`Agent.Steer`): if the agent is streaming, append to queue; else call `Prompt` directly. The outer loop drains steering after each inner-loop completion.

**Follow-up** (`Agent.FollowUp`): always enqueue. The outer loop drains follow-ups after steering, so a follow-up only fires once the agent would otherwise be idle.

Draining is atomic — grab the queue under lock, return it, zero the slice. Messages run in order.

See [Sessions and steering](../concepts/sessions-and-steering.md) for the concept.

---

## The background summarizer

After each `Prompt` call returns, a goroutine fires:

```go
a.wg.Add(1)
go func() {
    defer a.wg.Done()
    Summarize(a.db, a.llm, a.session.ID)
}()
```

`Summarize` reads unsummarized messages, checks threshold, and if over it calls the summary model to produce a paragraph + atomic facts, writes them, and marks source messages `summarized=1`.

The `WaitGroup` matters: `Agent.Close()` waits for all summarizer goroutines to drain before process exit. Otherwise `cairo -task 42` (one-shot background worker) would sometimes exit before its final summarizer run finished, losing that summary.

---

## Why not `llm.Chat` with a built-in tool loop?

An earlier design had `llm.Chat` own the tool-call loop internally and return only final text. The inner loop was invisible to the agent layer; intermediate messages existed only as local variables inside `Chat`.

That design failed the DB-as-identity promise: tool calls never got persisted, resume across restarts was lossy, and steering queued mid-turn ended up appended to a local slice that was thrown away when `Chat` returned.

The current split — `llm.StreamOnce` does one request, `runLoop` owns iteration — is verbose by a few lines but correct across the system boundary.

---

## Known rough edges

- **Event bus drops.** Documented above and in [ROADMAP](../../ROADMAP.md).
- **`callSeq` synthesizes tool-call IDs.** Ollama doesn't emit stable call IDs, so `runLoop` generates `call_<name>_<seq>` strings. They're stable within a turn but re-synthesized on every resume, which means correlation across a process restart is name-based, not id-based.
- **Cancel between tool calls, not during.** A tool's `Execute` method isn't `context`-aware (the interface doesn't pass one). Cancellation happens between tools, not during a long `bash` invocation. Cleanup target for a future pass.
