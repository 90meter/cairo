# Built-in tools

The model has access to a fixed set of tools built into the binary. This is the full list. Many are "consolidated" — one tool with an `action` argument that dispatches to a family of operations. A few are single-purpose.

See [Custom tools](../guides/custom-tools.md) for how the model can extend this set at runtime.

---

## Filesystem

Seven tools for reading, writing, and exploring files.

### `read`
Read a file's contents.
- `path` (string, required)

### `write`
Write a full file, creating directories as needed. Overwrites.
- `path` (string, required)
- `content` (string, required)

### `edit`
String-replacement edit: find `old_string`, replace with `new_string`. Errors if `old_string` isn't unique in the file.
- `path` (string, required)
- `old_string` (string, required)
- `new_string` (string, required)

### `bash`
Run a shell command. Combined stdout+stderr returned.
- `command` (string, required)
- `timeout` (integer, optional) — seconds, default 30, max 120

### `grep`
Search file contents with a regex.
- `pattern` (string, required)
- `path` (string, optional) — default cwd
- `include` (string, optional) — file glob filter

### `find`
List files by name pattern.
- `pattern` (string, required)
- `path` (string, optional) — default cwd

### `ls`
List a directory.
- `path` (string, optional) — default cwd

---

## Memory family

### `memory`
Actions: `add | list | search | update | delete`
- `add`: `content` (required), `tags` (optional comma-separated)
- `list`: no args
- `search`: `query` (required), `limit` (optional, default 5)
- `update`: `id` (required), `content` (required)
- `delete`: `id` (required)

### `summary_search`
Semantic search over summaries (cross-session).
- `query` (string, required)
- `limit` (integer, optional, default 5)

### `fact_promote`
Promote an atomic fact to a permanent memory.
- `id` (integer, required) — fact id
- `content` (string, optional) — if provided, overrides the fact text when storing

---

## Notes

### `note`
Actions: `create | list | read | update | delete`
- `create`: `title` (optional), `content` (required), `tags` (optional)
- `list`: no args
- `read`: `id` (required)
- `update`: `id` (required), `content` (required), `title` (optional), `tags` (optional)
- `delete`: `id` (required)

Notes are scratch space, not identity — see [Memory model](../concepts/memory-model.md).

---

## Skills

### `skill`
Actions: `list | read | create | update | delete`
- `list`: no args — returns all skill names + descriptions
- `read`: `name` (required) — returns the skill's full content
- `create`: `name` (required), `description` (required), `content` (required), `tags` (optional)
- `update`: `name` (required), + any field to change
- `delete`: `name` (required)

Skills are reusable instructions — runnable "prompts" the being can dispatch to itself. See [Skills](../guides/skills.md).

---

## Jobs and tasks

### `job`
Actions: `create | list | update | delete`
- `create`: `title` (required), `description` (optional), `orchestrator_role` (optional, default `orchestrator`)
- `list`: no args
- `update`: `id` (required), `status` (optional), `result` (optional)
- `delete`: `id` (required)

### `task`
Actions: `create | list | update | delete | ready | artifacts`
- `create`: `job_id` (required), `title` (required), `description` (optional), `assigned_role` (optional, default `coder`), `depends_on` (optional, array of task ids)
- `list`: `job_id` (optional) — filter to one job's tasks
- `update`: `id` (required), `status` (optional), `result` (optional)
- `delete`: `id` (required)
- `ready`: `job_id` (optional) — returns tasks whose deps are done and status is pending
- `artifacts`: `id` (required) — returns the task's `task_artifacts` rows

### `agent`
Actions: `spawn | wait | log` — controls background agents (parallel threads).
- `spawn`: `id` (required) — spawn a subprocess for task `id`. Atomic dependency check.
- `wait`: `id` (required), `timeout` (optional seconds, default 300, max 3600) — block until task terminal
- `log`: `id` (required), `tail` (optional) — read the task's captured stdout/stderr

See [Background work](../guides/background-work.md) for workflows.

---

## Sessions

### `session`
Actions: `list | delete`
- `list`: no args
- `delete`: `id` (required) — cascades through messages, summaries, facts, jobs, tasks

---

## Identity and configuration

### `soul`
Actions: `get | set`
- `get`: no args — returns the current soul_prompt
- `set`: `content` (required, max 300 chars) — replace the soul

### `config`
Actions: `get | set | list`
- `get`: `key` (required)
- `set`: `key` (required), `value` (required)
- `list`: no args — all key/value pairs

See [Config keys](config-keys.md) for the canonical list.

### `prompt_part`
Actions: `add | list | update | delete | toggle`
- `add`: `key` (required), `content` (required), `trigger` (optional), `load_order` (optional, default 100)
- `list`: `trigger` (optional filter)
- `update`: `id` (required), + any field to change
- `delete`: `id` (required)
- `toggle`: `id` (required) — flip enabled

### `role`
Actions: `list | model_set`
- `list`: no args
- `model_set`: `name` (required), `model` (required)

Current tool only covers those two — role creation is DB-direct today (see [Roles](../concepts/roles.md)).

---

## Custom tools

### `custom_tool`
Actions: `list | create | delete`
- `list`: no args
- `create`: `name` (required), `description` (required), `implementation` (required), `impl_type` (optional, `bash` or `python`, default `bash`), `parameters` (optional JSON Schema object), `prompt_addendum` (optional)
- `delete`: `name` (required)

See [Custom tools](../guides/custom-tools.md) for the lifecycle.

---

## Self-inspection

### `prompt_show`
Show the current system prompt as the being would see it on the next turn.
- No args. Useful for debugging prompt composition.

### `tool_list_builtin`
List every built-in tool name.
- No args. Auto-populated from the registry — stays in sync as tools are added.

---

## Per-role availability

Different roles see different tools. From the seeded defaults:

| Tool | thinking_partner | orchestrator | planner | coder | reviewer |
|---|:---:|:---:|:---:|:---:|:---:|
| read | ✓ | ✓ | ✓ | ✓ | ✓ |
| write | ✓ |   |   | ✓ |   |
| edit | ✓ |   |   | ✓ |   |
| bash | ✓ | ✓ | ✓ | ✓ | ✓ |
| grep | ✓ |   | ✓ | ✓ | ✓ |
| find | ✓ |   | ✓ | ✓ | ✓ |
| ls | ✓ |   | ✓ | ✓ | ✓ |
| memory | ✓ | ✓ | ✓ | ✓ | ✓ |
| summary_search | ✓ | ✓ | ✓ | ✓ | ✓ |
| fact_promote | ✓ |   |   |   |   |
| prompt_show | ✓ | ✓ | ✓ |   |   |
| note | ✓ | ✓ | ✓ | ✓ | ✓ |
| skill | ✓ |   | ✓ |   |   |
| job | ✓ | ✓ |   |   |   |
| task | ✓ | ✓ |   | ✓ | ✓ |
| agent | ✓ | ✓ |   |   |   |
| session | ✓ |   |   |   |   |
| role | ✓ | ✓ | ✓ |   |   |
| soul | ✓ |   |   |   |   |
| config | ✓ |   |   |   |   |
| prompt_part | ✓ |   |   |   |   |
| custom_tool | ✓ |   |   |   |   |

Custom tools are always available regardless of role. See [Roles](../concepts/roles.md).

---

## Known rough edges

- **No size cap on tool output.** A large `read` or `bash` dumps full contents into the model's context. Cap is on the near-term roadmap.
- **No streaming tool progress.** Tools run to completion and return one result. Long-running tools can't show partial output to the model (only to the event bus as `EventToolUpdate`, which is a UI signal).
- **`agent(action="wait")` polls.** 2-second poll interval; up to 1 hour max. Fine for the current use case; not ideal if many concurrent waits stack up.
