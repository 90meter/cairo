# Roles

A role is a **mode of focus**, not a separate identity. The being has one voice, one memory pool, and one soul. A role just tilts the turn in a direction: which tools are allowed, which model runs, what framing goes into the system prompt.

---

## The five built-in roles

| Role | What it's for | Default model | Tools allowed |
|---|---|---|---|
| `thinking_partner` | Interactive collaboration — the default | `qwen3.6:35b-a3b-mlx-bf16` | all (20+) |
| `orchestrator` | Coordinate a job — split work into tasks, track progress | `qwen3.6:35b-a3b-mlx-bf16` | read, bash, job, task, agent, memory, summary_search, prompt_show, note, role |
| `planner` | Research and design before implementation | `qwen3.6:35b-a3b-mlx-bf16` | read-heavy: read, bash, grep, find, ls, memory, summary_search, note, skill, role |
| `coder` | Write and edit code, run tests | `qwen35-35b-coding:latest` | read, write, edit, bash, grep, find, ls, memory, note, task |
| `reviewer` | Verify output — run tests, check requirements | `mistral-small-24b:latest` | read, bash, grep, find, ls, memory, note, task |

Each role is a row in the `roles` table with four fields:

- **`name`** — `thinking_partner`, `coder`, etc.
- **`description`** — one-line summary surfaced in `role(action="list")`
- **`model`** — which Ollama model to use when this role runs (optional; falls back to `config.model`)
- **`base_prompt_key`** — which `prompt_parts` row supplies the role's framing (convention: `role:<name>`)
- **`tools`** — JSON array of tool names this role can use. Empty array means unrestricted.

---

## How a role shapes a turn

Three things happen when a session's role is, say, `coder`:

1. **Model selection.** `db.ResolveModel(db, "coder", fallback)` reads `roles.model` and returns `qwen35-35b-coding:latest` — a coding-tuned model. The thinking_partner uses a different (bigger, generalist) model.
2. **Tool filtering.** `tools.FilterByAllowlist` returns only the tools listed in `roles.tools`. The coder role has `write`/`edit` but not `memory(action="delete")` via the memory tool's restricted form — actually memory *is* available to coder, but skills, jobs, prompt_part edits, soul, config are not.
3. **Prompt framing.** `BuildSystemPrompt` appends the `role:coder` prompt part, which reads:

> You are operating as a coder — one of Selene's focused attention modes. Your job is to implement: write clean, minimal code that solves the task exactly as specified. Do not add features beyond what was asked. Do not refactor surrounding code. Test your work with bash before reporting done.

Same being, different framing.

---

## Switching roles

Every session has a role, set at creation:

```bash
cairo -new -role coder       # start a new session in coder mode
cairo -new -role planner     # start in planner mode
cairo -new                    # default: thinking_partner
```

A session's role does not change after it's created. If you want a different mode, start a new session or use `agent(action="spawn")` to delegate to a task with a different assigned role.

You can inspect and change the *model* for a role at any time via the `role` tool:

```
> show roles
[calls role(action="list")]
> change the coder's model to a smaller one for quick edits
[calls role(action="model_set", name="coder", model="...")]
```

---

## Adding your own roles

Roles are just DB rows. A new role needs:

1. **A row in `roles`** with a name, description, model, `base_prompt_key`, and tools array. The `role` tool today covers list + model_set; to create a role you can currently edit the DB directly or add seed entries. (A `role(action="create")` is plausible future work.)
2. **A matching prompt part** with `key = "role:<name>"` (convention) or whatever you set in `base_prompt_key`. Write it via the `prompt_part(action="add", ...)` tool:

```
prompt_part(action="add", key="role:security_auditor",
            trigger="role:security_auditor",
            content="You are operating as a security auditor — focused attention mode for threat modeling and vulnerability review. ...")
```

3. **Start a session with it:** `cairo -new -role security_auditor`.

---

## Why the tool allowlist matters

The tool allowlist is the main way a role changes behavior beyond its framing. A coder role without `memory(action="delete")` simply can't delete memories — that tool isn't in its registry this turn. Same with the reviewer, which can't `write` or `edit`. The framing says "do not modify code"; the allowlist makes it impossible.

This is belt-and-suspenders. The framing sets intent; the allowlist enforces it.

---

## Tool filtering semantics

From `tools.FilterByAllowlist`:

- **Empty `roles.tools` JSON array** → unrestricted. Role gets every built-in tool.
- **Non-empty array** → intersection: role gets only tools whose names appear in the array.
- **Custom tools** are *always* available regardless of allowlist. Custom tools are the being's own work product; restricting a role from using its own tools didn't match the rhizomatic intent.

Names in the allowlist that don't match any registered tool are silently ignored.

---

## Known rough edges

- **Role creation is DB-direct.** You can't add a role from inside Cairo yet — the `role` tool exposes `list` and `model_set` only. Worked around today by editing seed data or executing `sqlite3 ~/.cairo2/cairo.db "INSERT INTO roles..."`. A `role(action="create")` is a plausible near-term addition.
- **Role framings are built-in defaults.** The five default prompt-part framings live in `internal/db/seed.go`. Edits to that seed don't back-populate existing DBs (seed uses `INSERT OR IGNORE`). To customize existing framings, use `prompt_part(action="update", ...)`.
