# Cairo Code Review Findings

Working document — issues found during walkthrough. Each item is tagged:
- `[hardening]` — correctness / reliability concern
- `[magic]` — hardcoded string, path, or value that should be configurable
- `[nice]` — cleanup / polish, not urgent
- `[design]` — architectural question worth discussing

---

## cmd/cairo/main.go

**[magic] Hardcoded model fallback**
`db.ResolveModel(database, session.Role, "qwen3.6:35b-a3b-mlx-bf16")` — the fallback model is Mac Studio specific. At work on the L40s the model names are different. Should blow up if no model is configured rather than silently falling back to a model that probably doesn't exist on the target machine.
_Fix: remove the fallback, return an error if no model is configured for the role. Force the user through the wizard._

**[hardening] runDream opens its own DB connection**
`runDream` calls `db.Open()` independently — completely separate from the main DB open in `main()`. No shared `bgWg`, no wizard check, no Ollama connection retry. If the DB doesn't exist yet it'll fail silently or in a confusing way.
_Fix: consolidate — share the same startup helpers, or at minimum give `runDream` the same error handling as the main path._

---

## internal/db/db.go

**[magic] Hardcoded DB path `~/.cairo2/cairo.db`**
The path is assembled inline. `~/.cairo2` is a leftover from an older naming scheme — logs and task files reference it too. Should be a named constant or derived from a single source of truth.
_Fix: define a `DefaultDataDir` constant, use it everywhere._

**[hardening] `SetMaxOpenConns(1)` is a hard concurrency ceiling**
Single writer is correct for SQLite, but it means the summarizer goroutine holding a write lock still causes TUI lag even after our timeout fix. WAL mode helps readers, but writes still serialize.
_Note: acceptable for now, just keep in mind as concurrency grows._

**[magic] `busy_timeout=15000` hardcoded in DSN string**
15 seconds is a reasonable default but it's buried in a DSN string. Should be a named constant.
_Fix: define `const busyTimeoutMs = 15000` and build the DSN from it._

---

---

## internal/db/schema.go

**[magic] Role name magic strings in schema defaults**
`sessions.role DEFAULT 'thinking_partner'`, `tasks.assigned_role DEFAULT 'coder'`, `jobs.orchestrator_role DEFAULT 'orchestrator'` — role names hardcoded in SQL schema. If a role is renamed, these silently produce orphaned references.
_Fix: define role name constants in Go, validate on insert rather than relying on SQL defaults._

**[hardening] Tool call / result pairing by order only**
`messages` table has no foreign key linking a `role='tool'` result row back to the specific tool call it answers. Pairing is purely by message order. Not currently a bug, but fragile — if messages are ever inserted out of order or loaded with a filter, the LLM would see mismatched tool call / result pairs.
_Note: track this, especially if we add any async tool execution later._

**[nice] `jobs` and `tasks` are a two-level hierarchy**
`jobs` is the parent (a high-level goal), `tasks` are the children (individual work items). Both have `status`, `result`, `assigned_role`. Worth verifying these are both actively used and not one being quietly abandoned in favor of the other.

---

---

## cmd/cairo/ (package structure)

**[nice] main.go has too many concerns**
`main.go` currently contains: subcommand dispatch, startup sequence, `runTask`, `collectArtifacts`, `runDream`, `resolveSession`, `resolveOllamaURL`, `connectOllama`, `fatalf`. `wizard.go` and `bundle.go` already exist showing the intent to split things out, but it was never finished.
_Fix: split into `startup.go` (connectOllama, resolveSession, resolveOllamaURL), `task.go` (runTask, collectArtifacts), `dream.go` (runDream). Leave main.go as pure dispatch + fatalf._

---

---

## internal/agent/summarizer.go

**[hardening] Summarizer is not tool-call aware — loses most of the context**
The transcript builder maps everything that isn't `role='assistant'` to "User", including `role='tool'` results, `role='system'` messages, and tool-call-only assistant turns (which have empty Content). This means:
- Tool results are labeled as "User" messages with raw output as content
- Assistant tool-call requests (empty Content, has ToolCalls) produce "Cairo: " with nothing
- System messages are labeled "User"
- The "Cairo called bash, got X result, then called edit" narrative is completely lost from summaries

_Fix: make the transcript builder role-aware — map tool results to "Tool result: [content]", skip or summarize tool-call-only assistant turns as "Cairo called: [tool names]", skip system messages entirely._

---

---

## internal/agent/loop.go

**[nice] Dead code suppressed in init()**
`contentPreview` is defined but never called. An `init()` block uses `_ = contentPreview` and `_ = strings.Builder{}` to suppress compiler warnings. Should just delete the function and the init block.

**[nice] Unused range variable suppressed with `_ = i`**
`for i, tc_call := range toolCalls` — `i` is unused, suppressed with `_ = i`. Should be `for _, tcCall := range toolCalls`. Also fixes the `tc_call` → `tcCall` naming convention.

**[design] Tool calls not carried into in-memory history across steering turns**
Tool calls and results are appended to `sendMsgs` (live context for the current inner loop) but not to `msgs` (history carried across outer loop iterations). After a steer, the rebuilt context doesn't include tool calls from the previous turn — only the final assistant text. Within a single session, this means a steered turn has less context than expected. The tool calls are in the DB and reload on next session start, but they're not available mid-session for the model to reason about. May be intentional (system prompt includes summaries) but worth being explicit about.

---

## (more to come as walkthrough continues)
