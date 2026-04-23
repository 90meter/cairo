# Architecture overview

Cairo is a Go binary, a SQLite database, and an Ollama server. Those three pieces, with a few library dependencies for the TUI and embeddings. No daemon, no sync service, no cloud anything.

This doc is the picture that makes the subsystem docs easier to read.

---

## The three pieces

```
  ┌─────────────────────────────────────────────────────┐
  │                                                     │
  │    Ollama  ←──── HTTP ────→  cairo (Go binary)      │
  │  (LLM host)                     ↕                   │
  │                             SQLite DB               │
  │                        ~/.cairo2/cairo.db           │
  │                                                     │
  └─────────────────────────────────────────────────────┘
```

**Ollama** runs models locally. Cairo talks to it over HTTP at `http://localhost:11434` by default (configurable via `config.ollama_url`). Two endpoints are used: `/api/chat` for streaming generation and `/api/embeddings` for vector embeddings.

**The Go binary** is stateless. It opens the DB, loads roles/tools/prompts from it, talks to Ollama, and writes results back. Restarting the binary loses nothing.

**The SQLite database** is the being. Identity, memories, sessions, history, tools, skills, jobs, tasks — all in 14 tables. See [Database](database.md) for the schema.

---

## Subsystem map

Inside the Go binary, responsibility breaks down like this:

```
cmd/cairo/               CLI entrypoint + subcommands (export, import, diff)
  main.go                flag parsing, mode dispatch, session resolution
  bundle.go              portable identity: .cairo tar format

internal/db/             SQLite ownership — schema, queries, migrations
  db.go, schema.go       Open(), schema + migration application
  seed.go                defaults on first run
  *.go (per table)       CRUD per entity: memories, sessions, tasks, ...
  reap.go                startup sweep of orphaned running tasks

internal/llm/            Ollama interop
  client.go              HTTP client, 10-min timeout, Ping
  chat.go                StreamOnce — one request, streaming response
  embed.go               Embed — text → []float32
  types.go               Message, ToolCall, ToolDef shapes

internal/agent/          the agent itself
  agent.go               Agent struct, state, steering/follow-up queues
  loop.go                runLoop — the outer+inner turn loop
  prompt.go              BuildSystemPrompt — dynamic prompt composition
  events.go              typed event bus (Bus)
  summarizer.go          post-turn compression: summaries + facts
  types.go               Tool interface, ToolContext, ToolResult

internal/tools/          the tools the model can call. Entity families
                         (memory, note, skill, job, task, agent, session,
                         config, role, soul, prompt_part, custom_tool) are
                         each a single action-dispatched tool, not one-per-
                         verb — memory(action="add", ...) / ("list") /
                         ("search") etc. Keeps the surface at 23 tools
                         instead of ~60.
  registry.go            Default() returns built-in tools; custom-tool loader
  *.go (per tool)        read, write, edit, bash, grep, find, ls, memory,
                         note, skill, orchestration (job+task), spawn (agent),
                         dbtools (session/prompt_show/tool_list_builtin),
                         knowledge (summary_search/fact_promote), etc.
  spawn_unix.go          detached subprocess for background agents

internal/cli/            line-oriented chat interface
  cli.go                 Run, RunOnce, slash commands
  renderer.go            event-bus subscriber → stdout
  background.go          renderer variant for task logs

internal/tui/            Bubble Tea terminal UI
  tui.go                 model, Update, View
  panels.go              panel framework (top/bottom/left/right/fullscreen)
  panel_*.go             individual panels: help, memory, prompt, threads, files, sessions
  commands.go            slash-drawer command registry
  prefixes.go            input-prefix handling (!, @)
  style.go               color palette, lipgloss styles
```

---

## Data flow: one turn

```
  ┌────────────────────┐
  │ user types a line  │
  └──────────┬─────────┘
             ▼
     ┌─────────────┐     drainBackgroundInbox()
     │  Agent.     │ ──▶ persist [background] note if any
     │  Prompt()   │
     └──────┬──────┘
            │
            ▼
     ┌──────────────┐
     │ persist user │
     │   message    │
     └──────┬───────┘
            │
            ▼      ┌─────────────────────────────┐
     ┌─────────────│ BuildSystemPrompt():        │
     │ runLoop()   │   base + soul + role + tool │
     │             │   addenda + summaries +     │
     │             │   memories + cwd + date     │
     │             └─────────────────────────────┘
     │
     │     ┌──────────────────────────────────┐
     │     │  LLM.StreamOnce() → Ollama       │
     │     │  tokens stream back via event    │
     │     │  bus → UI renders live           │
     │     └──────────────────────────────────┘
     │
     │     if response contains tool_calls:
     │        for each call:
     │          persist assistant tool-call msg
     │          execute tool
     │          persist tool result
     │        re-stream with updated history
     │        (inner loop)
     │
     │     when stream ends without tool_calls:
     │        persist final assistant text
     │
     ▼
     (outer loop: drain steering, drain follow-up, or done)
            │
            ▼
     ┌─────────────┐
     │  background │
     │  summarizer │  (goroutine, fires after turn ends)
     │             │  reads messages, writes summaries+facts
     └─────────────┘
```

---

## The event bus

Agent execution publishes typed events on a `Bus`. Anyone can subscribe. This is how the same `runLoop` drives three different UIs (line CLI, TUI, background log renderer) without coupling.

Events:

- `AgentStart` / `AgentEnd` — bracketing a whole `Prompt()` call
- `TurnStart` / `TurnEnd` — per-turn within a `Prompt()` (outer loop iterations)
- `Tokens` / `Thinking` — streaming chunks from the model
- `ToolStart` / `ToolEnd` — tool call lifecycle
- `ToolUpdate` — progress during a long-running tool
- `Error` — anything that went wrong

Subscribers are non-blocking. A slow subscriber will lose events rather than stalling the agent. See [Agent loop](agent-loop.md) for the tradeoff and [ROADMAP](../../ROADMAP.md) for how this is planned to evolve.

---

## Two deployment shapes

Cairo is a single binary, but it runs in two distinct modes:

**Interactive / single-message mode** — what happens when you type `cairo`, `cairo -new`, `cairo -tui`, or `cairo "one-shot question"`. You're the user, the binary is the agent, one process, one session.

**Background task mode** — what happens when you type `cairo -task 42 -background`. The binary is a background worker subprocess, spawned by the `agent(action="spawn")` tool from an interactive session. It has no stdin, its output goes to a log file, and it writes its result back to the `tasks` table when done.

Both modes share all the same code. They diverge only in `main.go`'s flag dispatch.

See [Background work](../guides/background-work.md) for the job-and-task model.

---

## What's outside this picture

- **No separate database per user, per project, per role.** One `cairo.db` per install, at `~/.cairo2/cairo.db`. Multi-user is a future feature; solo-dev tooling is the current target.
- **No message queue, no job scheduler.** Background tasks are `exec.Cmd` subprocesses, coordinated through the DB.
- **No secret management.** Cairo doesn't store credentials. If a custom tool needs an API key, the environment is how to plumb it in, and `safe_env_extras` is the explicit whitelist (see [Custom tools](../guides/custom-tools.md)).

Simplicity is the architecture's main feature. One binary, one file, one LLM host.
