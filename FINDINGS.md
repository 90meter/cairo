# Cairo Code Review Findings

Working document — issues found during walkthrough. Each item is tagged:
- `[hardening]` — correctness / reliability concern
- `[magic]` — hardcoded string, path, or value that should be configurable
- `[nice]` — cleanup / polish, not urgent
- `[design]` — architectural question worth discussing
- `✅` — completed

---

## cmd/cairo/main.go

**[magic] Hardcoded model fallback**
`db.ResolveModel(database, session.Role, "qwen3.6:35b-a3b-mlx-bf16")` — the fallback model is Mac Studio specific. Should blow up if no model is configured rather than silently falling back.
_Fix: remove the fallback, return an error if no model is configured for the role._

**[hardening] runDream opens its own DB connection**
`runDream` calls `db.Open()` independently — no shared startup helpers, wizard check, or Ollama retry.
_Fix: consolidate startup path._

✅ **[nice] main.go has too many concerns** — split into startup.go, task.go, dream.go

---

## internal/db/db.go

✅ **[magic] Hardcoded DB path `~/.cairo2`** — `DefaultDataDir()` defined in constants.go, used everywhere

✅ **[magic] `busy_timeout=15000` hardcoded** — `busyTimeoutMs` constant defined

**[hardening] `SetMaxOpenConns(1)` is a hard concurrency ceiling**
Acceptable for now — note for future if write concurrency grows.

---

## internal/db/schema.go

**[magic] Role name magic strings in schema SQL defaults**
`DEFAULT 'thinking_partner'`, `DEFAULT 'coder'`, `DEFAULT 'orchestrator'` in SQL DDL — Go constants don't help here, SQL doesn't use them. Worth noting if roles are ever renamed.

**[hardening] Tool call / result pairing by order only**
No FK linking tool result rows to their tool call. Fragile if async tool execution is ever added.

**[nice] jobs vs tasks hierarchy worth verifying**
Both tables active — jobs are goals, tasks are steps. Verify both remain in active use.

---

## internal/db/constants.go (NEW)

✅ **Role constants** — `RoleThinkingPartner`, `RoleOrchestrator`, `RoleCoder`, `RolePlanner`, `RoleReviewer`, `RoleDream`

✅ **Status constants** — `StatusPending`, `StatusRunning`, `StatusDone`, `StatusFailed`, `StatusBlocked`, `StatusCancelled`

✅ **`DefaultDataDir()`** — single source of truth for data directory path

---

## internal/agent/summarizer.go

**[hardening] Summarizer is not tool-call aware — loses context**
Transcript builder maps everything non-assistant to "User" — tool results mislabeled, tool-call-only turns produce empty lines, system messages mislabeled. The "Cairo called bash, got X" narrative is lost from summaries.
_Fix: role-aware transcript builder — tool results as "Tool result:", skip system messages, summarize tool-call turns as "Cairo called: [names]"._

---

## internal/agent/loop.go

✅ **[nice] Dead code (contentPreview, init())** — removed

✅ **[nice] `_ = i` / `tc_call` naming** — fixed to `_, tcCall`

**[design] Tool calls not carried into in-memory history across steering turns**
`msgs` doesn't include tool calls from previous inner iterations. Steered turns have less context. May be intentional (system prompt has summaries). Worth being explicit about.

---

## internal/agent/prompt.go

**[magic] Hardcoded summary context count (4) and memory limit (15)**
Should be named constants.

**[nice] Duplicate step numbers in BuildSystemPrompt comments**
Two "3." and two "9." steps.

**[design] Memories loaded by recency, not relevance**
`RecentContent(limit)` — 15 most recent regardless of relevance. Future: embed last user message and rank by similarity.

**[nice] Config.All() on every turn**
Full table scan per prompt build. Fine now, watch as config grows.

---

## internal/tools/registry.go + custom_tool.go

✅ **[nice] customTool moved to custom_tool.go**

✅ **[hardening] Custom tool uses context.Background()** — fixed: tc.Ctx, Setpgid, process group kill

✅ **[nice] Config snapshot at construction time** — fixed: config loaded at execute time

**[nice] EmbedConfig struct**
`(embedder, embedModel)` pattern repeated across 4 tool constructors. Could wrap in `EmbedConfig` struct with nil-safe `Embed()` method.

---

## internal/tools/orchestration.go

✅ **[magic] Role defaults inline** — use `db.RoleOrchestrator`, `db.RoleCoder`

✅ **[magic] Status strings** — `db.Status*` constants used in Go logic (SQL literals left as-is)

**[nice] doUpdate without transaction**
`SetStatus` then `SetResult` — two separate calls. Low risk for now.

---

## internal/tools/memory.go + knowledge.go

✅ **[hardening] fact_delete missing** — `fact_list` now has `delete` action

✅ **[nice] Embedder interface** — moved to tool.go

✅ **[nice] formatTags** — uses json.Marshal

✅ **[nice] doList() loads BLOBs** — AllContent() added, doList() uses it

**[nice] Repeated embedding boilerplate**
`if t.embedder != nil && t.embedModel != ""` copy-pasted in 3 tools. EmbedConfig struct would fix.

**[nice] Stale comment in knowledge.go**
Header says "read-only queries" — needs updating.

---

## internal/tools/spawn.go

✅ **[magic] ~/.cairo2/logs path** — uses DefaultDataDir()

✅ **[hardening] doLog no size cap** — capped at 65536 bytes

---

## internal/agent/history.go (NEW)

✅ **History layer split from agent.go** — loadHistory, repairIncompleteTurn, persistMessage, unmarshalToolCalls

---

## internal/providers/ (NEW)

✅ **Context provider framework** — wsh, vscode, shell, git providers; registry replaces hardcoded env detection

---

## TUI (internal/tui/) — DEFERRED
Complex enough to warrant a dedicated review session.
- ✅ Glamour renderer cached
- ✅ Event bus buffer 64→256
- ✅ unsub() called on exit
- Remaining: tool progress timer, turn-level timeout, /status visibility

---

## Data Directory Design [PENDING — design decision]

**[design] ~/.cairo → project-local .cairo/ with fallback**
Resolution order:
1. `--data-dir <path>` flag
2. `CAIRO_DB` env var (existing)
3. `.cairo/` in cwd or any parent (project-local)
4. `~/.cairo/` global default (rename from ~/.cairo2)

First-run: detect old `~/.cairo2/` and migrate.
_Implement as a dedicated feature after design discussion._

---

## Review Status — Remaining Files
Not yet reviewed: `internal/cli/`, `internal/tools/tool.go`, `internal/tools/dbtools.go`, simple tools (config, session, role, soul, skill, note, prompt_part, read, write, edit, bash, grep, find, ls, fetch, search)
