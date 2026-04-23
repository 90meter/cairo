# Cairo

**Collaborative AI Rhizomatic Orchestrator** — a local-first AI coding harness where the database *is* the being.

Cairo runs entirely on your machine. It talks to [Ollama](https://ollama.com) for inference, stores everything in one SQLite file, and exposes itself through a keyboard-first TUI with panels for threads, memory, prompt preview, sessions, and local files. The default identity is **Selene** — rename her any time.

---

## What's different about this

Most AI agents are stateless threads that connect to a service. Cairo is the opposite: a single `.db` file carries the whole identity — soul (persona), memories, skills, roles, prompt parts, conversation history, and every tool the being has authored for itself. Copy the `.db` to another machine with Ollama installed and the binary, and it resumes mid-thought. Export it as a `.cairo` bundle to share a tuned instance.

The architecture is **one being with parallel threads of attention** rather than a team of agents. Background tasks are spawned as sub-processes that share the same DB — the "orchestrator" isn't a separate identity, it's a focus mode of the same being. Completed background work surfaces in the next turn as an inbox note, never as push interruption.

---

## Quick start

```bash
# Prerequisites:
#   Go 1.25+
#   Ollama running locally (https://ollama.com)

go install github.com/scotmcc/cairo/cmd/cairo@latest

# Interactive TUI (recommended)
cairo --tui

# Or the line CLI
cairo
```

On first run, `cairo` initializes `~/.cairo2/cairo.db` with seeded roles, prompts, and skills. Run `/init` inside the TUI to have Selene introduce herself, capture your name, and learn about your project.

---

## Keyboard (TUI)

| Key        | Action                                                      |
| ---------- | ----------------------------------------------------------- |
| `/`        | open slash-command drawer (empty input only)                |
| `!<cmd>`   | run a shell command, output becomes the user turn            |
| `@<path>`  | file reference — contents get injected for the next turn     |
| `Ctrl-O`   | file picker (browse, then select to insert `@ref`)           |
| `Ctrl-T`   | threads panel (running + recent background tasks)            |
| `Ctrl-M`   | memory spotlight (semantic search)                           |
| `Ctrl-P`   | prompt preview (exactly what Selene will see next turn)      |
| `Ctrl-B`   | session browser                                              |
| `?`        | fullscreen help                                              |
| `Ctrl-C`   | context-aware: cancel turn · clear input · clear view       |
| `Ctrl-Q`   | quit                                                         |

---

## Command-line

```bash
cairo                         # resume latest session
cairo --tui                   # TUI (opt-in while it stabilizes)
cairo -new                    # force a new session
cairo -role coder -new        # start in a specialized role
cairo "ask something"         # one-shot prompt
cairo export bundle.cairo     # portable identity bundle (no history by default)
cairo import bundle.cairo     # replace local identity with a bundle
cairo diff bundle.cairo       # compare local identity vs bundle
```

---

## Concepts

- **Soul** — a self-maintained persona (≤300 chars), updated by the AI, not the user.
- **Role** — focus mode: `thinking_partner` (baseline), `coder`, `planner`, `reviewer`, `orchestrator`. Each can use its own Ollama model and a scoped tool whitelist.
- **Memory** — persistent knowledge with embeddings for semantic search.
- **Note / skill** — scratch pad and reusable workflows (the AI authors both).
- **Job / task** — orchestrated DAG of background work executed by spawned cairo sub-processes.
- **Prompt part** — modular system-prompt sections with optional triggers (`role:coder`, `tool:bash`, etc.).
- **Session** — one thread of conversation; messages auto-summarized past a threshold to keep context compact.

Deeper notes in [`docs/`](docs/).

---

## Status

Cairo is an active personal project, released publicly in the hope the patterns here are useful to others building local-first agents. It's not battle-tested across many environments yet. See [`ROADMAP.md`](ROADMAP.md) for direction.

## License

MIT — see [LICENSE](LICENSE).
