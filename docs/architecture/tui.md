# TUI

`cairo -tui` launches the Bubble Tea terminal UI. The line-oriented CLI still exists (and runs by default); the TUI is an optional richer surface that subscribes to the same agent event bus and adds panels, hotkeys, and motion.

Source: `internal/tui/`.

---

## High-level shape

```
┌────────────────────────────────────────────────┐
│  Selene  ·  session 42                         │  ← header
│━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━│
│                                                │
│  You: how should we structure the docs?        │
│                                                │
│  Selene: i'd suggest …                         │  ← transcript
│    ▸ read docs/                                │     (viewport)
│    ▸ grep -r …                                 │
│                                                │
│────────────────────────────────────────────────│
│  ▸ tell me more about …|                       │  ← input (textarea, 3 rows)
│────────────────────────────────────────────────│
│  • thinking  ·  12 mem  ·  ◇ 2 threads   …     │  ← status bar
└────────────────────────────────────────────────┘
```

Regions, from top to bottom:

- **Header** — name + session label. Role appears as "mode: X" when non-default.
- **Top-position panels** (optional) — slide-in from top (prompt preview).
- **Transcript viewport** — scrollable conversation history. Scroll with PgUp/PgDn/↑/↓ when input is empty.
- **Left/right-position panels** (optional) — side panels (memory spotlight, file picker).
- **Slash drawer** (optional) — appears above the input when `/` is typed.
- **Bottom-position panels** (optional) — slide-in from bottom (threads list).
- **Input** — 3-row textarea. Enter submits; Alt-Enter / Shift-Enter inserts newline.
- **Status bar** — ambient stats (thinking pulse when streaming, memory count, thread count) and hint row.

---

## Model, Update, View

Bubble Tea's pattern. The `model` struct holds everything the View function needs; `Update` is the message handler that returns `(model, cmd)` pairs.

```go
type model struct {
    agent   *agent.Agent
    db      *db.DB
    session *db.Session

    viewport   viewport.Model
    input      textarea.Model
    transcript strings.Builder

    streaming   bool
    cancel      context.CancelFunc

    aiName       string
    memoryCount  int
    threadCount  int
    tickCounter  int

    commands     []Command
    slashOpen    bool
    slashMatches []Command
    slashIndex   int

    openPanels   map[panelID]bool
    focusedPanel panelID

    threads, files, memory, prompt, sessions  // per-panel state
    eventCh   <-chan agent.Event
    unsub     func()
    styles    styles
}
```

Three inputs feed `Update`:

1. **`tea.KeyMsg`** — keystrokes from the user.
2. **`eventMsg`** wrapping an `agent.Event` — streaming tokens, tool starts/ends, turn lifecycle.
3. **`tickMsg`** — fires every 300ms from a self-rescheduling timer. Drives the breathing "thinking" pulse and the thread spinner.

---

## Event subscription

The TUI subscribes to the agent bus at `newModel`:

```go
ch, unsub := a.Bus().Subscribe()
```

A `listenEvents` command reads one event from the channel, wraps it as a `tea.Msg`, and returns. `Update` handles the event and re-issues `listenEvents` to keep the pump going. This is the standard Bubble Tea pattern for async sources.

When an `EventTokens` fires, `Update` calls `appendAssistantToken(token)` which writes into `m.transcript` and pushes into the viewport. Tool events drive the `  ▸ tool  ✓` or `  ✗` marks inline.

---

## Panel system

Panels are pluggable overlays registered at startup. Each has:

- **`ID`** — `panelHelpID`, `panelMemoryID`, `panelPromptID`, `panelThreadsID`, `panelFilesID`, `panelSessionsID`.
- **`Position`** — `posTop | posBottom | posLeft | posRight | posFullscreen`.
- **`ToggleKey`** — `Ctrl-?`, `Ctrl-M`, `Ctrl-P`, `Ctrl-T`, etc.
- **`Update` hook** — first crack at keys when focused.
- **`View` hook** — renders into its allocated rectangle.
- **`PreferredWidth` / `PreferredHeight`** — sizing hints.

`Update` routes a key first to the focused panel (if any), then to panel-toggle detection, then to slash-drawer handling, then to global hotkeys, then to the input field. This lets any panel own its own navigation (arrow keys in the memory spotlight, for instance) without the main loop needing to know.

Adding a new panel is ~1 file in `internal/tui/panel_<name>.go` plus one registration entry in `panels.go`.

---

## The slash drawer

Typing `/` as the first character of empty input opens the command drawer. It filters the registered command list as you type, selection via ↑/↓, Enter to run.

Commands in `internal/tui/commands.go`:

- `/help` — open fullscreen help panel
- `/clear` — clear transcript (DB untouched)
- `/init` — run the init skill
- `/memory` — open memory spotlight panel
- `/threads` — open threads panel
- `/files` — open file picker
- `/prompt` — open prompt preview panel
- `/sessions` — open sessions browser
- `/quit` — exit

All commands also have direct hotkeys (`Ctrl-M`, `Ctrl-T`, etc.) so you can trigger them without opening the drawer.

---

## Input prefixes

Two special prefixes transform input at submit time:

- **`!<command>`** — runs `<command>` via bash and uses the output as the user turn. Makes "pipe a command output into the conversation" a one-liner.
- **`@<path>`** — injects the named file's contents. The transcript shows `@path` (compact) but the agent sees the full file body. `@` at a word boundary also opens the file picker panel for discoverability.

Both live in `internal/tui/prefixes.go`.

---

## Context-aware Ctrl-C

Ctrl-C means "stop whatever I'm doing," and the "whatever" shifts with state:

- **Streaming** → cancel the context, kill the LLM request. `runLoop` catches the cancel, persists partial text with `(interrupted)`, and the UI resets.
- **Input has content** → clear the input field.
- **Idle, input empty** → clear the transcript viewport. DB is untouched.

Three levels of "stop," each non-destructive relative to the DB. Ctrl-D on empty input exits the program.

---

## OSC leak filter

Some terminal emulators (Waveterm has been observed) respond slowly to Bubble Tea's OSC 10/11 background-color probes. The late response arrives *after* the input reader is live, so the raw escape sequence — like `]11;rgb:0000/0000/0000\` — leaks into the textarea.

`oscFilter` in `tui.go` detects the characteristic `alt+]` prefix followed by a body that matches `[0-9a-fA-F;:/rgbRGB()\\\s,.]+` and drops both messages. Conservative by design — lets real keystrokes through if unsure.

---

## Motion

Animation uses a 300ms tick:

- **Thinking pulse** — breathing `• thinking` in the status bar, alternates bright/dim bullet per tick. Low-amplitude on purpose (presence, not alarm).
- **Thread spinner** — four-frame `◇ ◈ ◆ ◈` rotation next to the running-task count when any background task is live.
- **Live counts** — memory and thread counts refresh on every tick via cheap `COUNT(*)` queries.

The tick is cheap and the refresh is synchronous with a DB hit. At default cadence that's a handful of counts per second while Cairo is open — negligible.

---

## Where this is headed

The panel framework is general enough that adding new panels is low-effort. Likely next:

- **Tool-call timeline** — linear view of every tool call in the current session, with filters.
- **Live jobs/tasks board** — panel analog of `job(action="list")` + `task(action="list")` with auto-refresh.
- **Inline skill browser** — find a skill, read it, dispatch it without leaving the TUI.
- **Soul / memory editor** — inline editing instead of tool-call-mediated.

See [ROADMAP](../../ROADMAP.md) — mid term.

---

## Known rough edges

- **Textarea is fixed-height (3 rows) and scrolls internally on overflow.** Auto-growing would require measuring visual wrap width on every keystroke. Three rows is enough for most messages; long pastes scroll in-place.
- **Mid-turn steering works.** Typing while Selene streams enqueues; the outer loop drains between inner-loop iterations. Steering events are not shown in the transcript until they run — the input just empties.
- **Viewport re-rendering on panel open/close is expensive at large transcripts.** Current transcripts fit fine; pathological sessions (hours, many tool calls) might show a hitch.
