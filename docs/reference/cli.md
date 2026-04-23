# CLI reference

`cairo` — the binary. All modes share one binary; behavior depends on flags and subcommands.

---

## Synopsis

```
cairo [flags]                              # interactive / single-message mode
cairo export [--full] <bundle.cairo>       # export identity
cairo import [--force] <bundle.cairo>      # import identity
cairo diff <bundle.cairo>                  # compare a bundle to local
```

---

## Interactive flags

| Flag | Type | Default | Effect |
|---|---|---|---|
| `-new` | bool | false | Start a new session instead of resuming the most recent one for this cwd |
| `-session <id>` | int64 | 0 | Resume a specific session by id |
| `-name <label>` | string | "" | Optional label for a new session |
| `-role <role>` | string | `thinking_partner` | Role for a new session: `thinking_partner`, `orchestrator`, `planner`, `coder`, `reviewer`, or any role you've added |
| `-tui` | bool | false | Use the Bubble Tea TUI instead of the line CLI |
| `-task <id>` | int64 | 0 | **Background task mode.** Run as a subprocess worker for this task id. Writes results to the DB and exits. Spawned by `agent(action="spawn")` — not usually invoked directly. |
| `-background` | bool | false | Background mode: plain log output, no banner. Pairs with `-task`. |

Positional args after flags are treated as a **single-message prompt**: `cairo "one-shot question"` sends one message and exits.

---

## Interactive modes

**Resume (default).** `cairo` with no flags resumes the most-recent session for the current working directory.

```bash
cairo                       # resume most recent
cairo -session 42           # resume by id
```

**New session.** `cairo -new` starts a fresh session.

```bash
cairo -new                               # new thinking_partner session
cairo -new -role coder                   # new coder session
cairo -new -role planner -name "refactor"  # new planner session, labeled
```

**TUI.** `cairo -tui` runs the richer terminal interface. Works for new or resumed sessions.

```bash
cairo -tui                  # resume most recent in TUI
cairo -new -tui             # new session in TUI
```

**Single-message.** Pass a prompt as positional args to send one message and exit.

```bash
cairo "summarize what we did last session"
cairo -new "start a new thread — what does this codebase do?"
```

Single-message mode streams the response to stdout, waits for the background summarizer to drain, then exits.

---

## Slash commands (line CLI)

In the line CLI, `/` at the start of input is a local command, not a message.

| Command | Effect |
|---|---|
| `/help` | Show available commands |
| `/init` | Run the guided setup skill (learns project + preferences) |
| `/init codebase` | Run the codebase-exploration skill (skips personal questions) |
| `/session` | Show current session info |
| `/sessions` | List all sessions |
| `/jobs` | List all jobs |
| `/memories` | List stored memories |
| `/tools` | List custom tools (tools the AI has written) |
| `/skills` | List skills |
| `/exit`, `/quit`, `/q` | Exit |

The TUI has its own slash-drawer with a fuller command list — see [TUI](../architecture/tui.md).

---

## Subcommands

### `cairo export [--full] <bundle.cairo>`

Export the current identity to a `.cairo` bundle (gzipped tar with manifest.json + a snapshot of the DB).

| Flag | Effect |
|---|---|
| `--full` | Include conversation history (sessions, messages, summaries, facts, jobs, tasks, task_artifacts). Default excludes them — the bundle carries identity without private transcripts. |

Example:

```bash
cairo export selene-snapshot.cairo          # identity only
cairo export --full selene-full.cairo       # everything
```

Output:

```
exported to selene-snapshot.cairo
  format: identity-only (no conversation history)
  memories: 47  skills: 3  roles: 5  prompt_parts: 12
```

### `cairo import [--force] <bundle.cairo>`

Replace the current DB with the contents of a bundle. Backs up the existing DB alongside the target before overwriting.

| Flag | Effect |
|---|---|
| `--force` | Skip the interactive confirmation |

Example:

```bash
cairo import selene-snapshot.cairo
```

Interactive prompt:

```
bundle:
  exported: 2026-04-22T15:30:00-05:00
  format:   identity-only (no conversation history)
  memories: 47  skills: 3  roles: 5  prompt_parts: 12
this will REPLACE your current cairo identity with the contents of the bundle.
a backup will be written alongside. proceed? [y/N]: y
backup: /Users/scot/.cairo2/cairo.db.pre-import-20260423T153045Z
imported into /Users/scot/.cairo2/cairo.db — next cairo run uses the bundle's identity
```

### `cairo diff <bundle.cairo>`

Compare a bundle to the local DB without touching anything.

```bash
cairo diff selene-snapshot.cairo
```

Output:

```
bundle (2026-04-22T15:30:00-05:00) vs local:
    memories        local=52   bundle=47
  * skills          local=3    bundle=5
    notes           local=2    bundle=0
    roles           local=5    bundle=5
    prompt_parts    local=12   bundle=12
    custom_tools    local=1    bundle=3
    config_keys     local=14   bundle=14

soul matches

role→model differs:
  coder: local="qwen35-35b-coding:latest" bundle="mistral-small-24b:latest"
```

A `*` marker highlights count deltas.

---

## Environment

| Variable | Effect |
|---|---|
| `HOME` | Used to resolve `~/.cairo2/` (DB + logs) |

Cairo doesn't read any other environment variables at the top level. Custom tools can read the environment they're given; see [Custom tools](../guides/custom-tools.md).

---

## Paths

| Path | Purpose |
|---|---|
| `~/.cairo2/cairo.db` | The main SQLite database — the being itself |
| `~/.cairo2/cairo.db-wal`, `~/.cairo2/cairo.db-shm` | SQLite WAL sidecars (auto-managed) |
| `~/.cairo2/cairo.db.pre-import-<timestamp>` | Backup written before `cairo import` |
| `~/.cairo2/logs/task_<id>.log` | Stdout/stderr capture for background tasks |

---

## Exit codes

- **0** — clean exit
- **non-zero** — any fatal error; message goes to stderr

Single-message mode propagates agent errors as non-zero exits; the background-task mode marks the task `failed` in the DB and exits non-zero.
