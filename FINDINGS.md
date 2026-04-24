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

---

## internal/agent/prompt.go

**[magic] Hardcoded summary context count and memory limit**
`contextCount = 4` and `limit = 15` are inline defaults — should be named constants so they're findable and not scattered across the file as bare integers.

**[nice] Duplicate step numbers in BuildSystemPrompt comments**
Two "3." steps (soul, role addendum) and two "9." steps (stamp, template substitution). Cosmetic but confusing for anyone maintaining the function.

**[design] Memories loaded by recency, not relevance**
`RecentContent(limit)` returns the 15 most recently created memories regardless of relevance to the current turn. Semantic search would be more useful but is a chicken-and-egg problem (need to know the query before building the prompt). Worth noting as a future improvement — could be partially solved by doing a quick embedding of the last user message and using that to rank memories.

**[nice] Config.All() called on every turn for template substitution**
Full table scan of config on every prompt build. Fine now, worth watching as config grows.

---

---

## internal/tools/registry.go

**[nice] customTool implementation belongs in custom_tool.go**
`registry.go` contains ~150 lines of custom tool execution logic. The registry should only register and filter; execution belongs in `custom_tool.go` which already exists.

**[hardening] Custom tool uses context.Background() — same bug as old bash.go**
`customTool.Execute` builds its timeout from `context.Background()` not `tc.Ctx`. Ctrl-C won't cancel it, no process group killing. Same fix as bash.go: use `tc.Ctx`, set `Setpgid`, kill process group on timeout.

**[nice] Config snapshot at construction time for custom tools**
`db.Config.All()` is called when the tool is loaded at session start. Mid-session config changes are invisible to custom tools. Should call `db.Config.All()` at execute time instead.

**[nice] Embedder + embedModel passed separately to 4+ tools**
`(database, embedder, embedModel)` is a repeated signature across Memory, SummarySearch, FactPromote, SummaryRewrite. Could be wrapped in an `EmbedConfig` struct to reduce noise in `Default()`.

---

---

## internal/tools/orchestration.go

**[magic] Role defaults hardcoded inline**
`role = "orchestrator"` and `role = "coder"` as fallback defaults in tool methods — same issue as schema defaults. Should be constants shared with the schema/seed layer.

**[magic] Status strings not constants**
`"pending"`, `"running"`, `"done"`, `"failed"`, `"blocked"`, `"cancelled"` scattered across descriptions, enums, and DB layer with no shared constants. Renaming a status value requires hunting across multiple files.

**[nice] doUpdate uses two separate DB calls without a transaction**
`SetStatus` then `SetResult` — if the second fails, status is updated but result isn't. Low risk, worth fixing if DB operations get more complex.

---

---

## internal/tools/memory.go + knowledge.go

**[hardening] fact_promote has no delete option — dream mode can't clean up**
`FactQ.Delete` exists in the DB layer but no agent tool surfaces it. The dream agent can promote facts to memories but cannot delete the original fact row afterward — leaving orphaned facts. Need a `fact_delete` action on `factListTool` or a new tool.

**[nice] Embedder interface belongs in tool.go, not memory.go**
`Embedder` is defined in `memory.go` but used by 4 tools. Should be in `tool.go` or a shared `embed.go` alongside the `EmbedConfig` consolidation.

**[nice] Repeated embedding boilerplate across 3 tools**
`if t.embedder != nil && t.embedModel != ""` + the Embed call appears identically in memory, fact_promote, summary_rewrite. An `EmbedConfig` struct with an `Embed(text string) ([]float32, error)` method (nil-safe) would remove this repetition.

**[nice] formatTags uses Go string quoting instead of json.Marshal**
`fmt.Sprintf("%q", p)` in `formatTags` — Go's quoting is close to JSON but not identical. Should use `encoding/json` for correctness.

**[nice] doList() loads all memories including embeddings (BLOBs)**
`Memories.All()` loads every row including embedding BLOBs — unnecessary for a list operation. A content-only query would be lighter, especially for dream mode iterating hundreds of entries.

**[nice] Stale file comment in knowledge.go**
Header says "read-only queries" but 3 of 4 tools in the file write data.

---

---

## internal/tools/spawn.go

**[magic] Hardcoded ~/.cairo2/logs path and 0755 permissions**
Same ~./cairo2 magic string. Should use the shared data dir constant.

**[hardening] doLog reads entire file into memory, no size cap**
`os.ReadFile` slurps the whole log file. Unlike bash (capped at 65KB), a long-running task log could be megabytes returned to model context. The tail implementation also reads everything first then slices — should seek from end for large files.

---

## TUI (internal/tui/) — DEFERRED
Complex enough to warrant a dedicated review session. Known issues from earlier work:
- Glamour renderer now cached (fixed)
- Event bus buffer increased to 256 (fixed)  
- unsub() now called on exit (fixed)
- Remaining: tool progress timer, turn-level timeout, /status command visibility

---

---

## Data Directory Design [SKIP — design decision]

**[design] ~/.cairo2 → ~/.cairo with project-local and explicit override**
Resolution order should be:
1. `--data-dir <path>` flag (explicit, wins always)
2. `CAIRO_DB` env var (already supported)
3. `.cairo/` in current directory or any parent (project-local, like .git)
4. `~/.cairo` (global default, rename from ~/.cairo2)

First-run: detect old `~/.cairo2/` and migrate or use it if `~/.cairo/` doesn't exist.
Touches: db.Open(), flag parsing in main.go, runDream/runTask path resolution.
_This is a significant design change — implement as a dedicated feature, not a cleanup task._

---

## Review Status
Remaining for future session: internal/cli/, internal/tools/tool.go, internal/tools/dbtools.go, simple tool files (config, session, role, soul, skill, note, prompt_part, read, write, edit, bash, grep, find, ls, fetch, search)
