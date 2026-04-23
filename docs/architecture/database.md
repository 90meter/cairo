# Database

The SQLite database at `~/.cairo2/cairo.db` is the being's complete persistent state. Everything identity-related is here: who the being is, what it knows, who it's talked to, what work it's done.

This doc covers the schema, operational choices (WAL, pragmas, busy_timeout), and how migrations work. For "what each table is *for*" read the [concepts](../concepts/memory-model.md) docs.

---

## Opening the DB

`db.Open()` opens `~/.cairo2/cairo.db`, creating the directory and file if needed. It returns a `*DB` with embedded query-structs for every table:

```go
db.Config, db.Sessions, db.Messages, db.Memories, db.Roles,
db.Prompts, db.Tools, db.Skills, db.Notes, db.Jobs,
db.Tasks, db.TaskArtifacts, db.Summaries, db.Facts
```

On open, the DB applies the schema, applies migrations, seeds defaults (idempotently), and sweeps orphaned running tasks. Every one of these is safe to run repeatedly — the code is structured so `Open()` is always the whole lifecycle check, not a fragile first-run gate.

---

## Pragmas

Set at open time, every time:

- **`PRAGMA journal_mode = WAL`** — write-ahead logging. Readers don't block writers.
- **`PRAGMA foreign_keys = ON`** — enforced explicitly, set on the pinned connection. (SQLite's default is off; the `_foreign_keys=on` DSN parameter turned out to be flaky in modernc.org/sqlite, so this is belt-and-suspenders.)
- **`PRAGMA busy_timeout = 15000`** — wait up to 15s for the write lock before giving up. Prevents `SQLITE_BUSY` when multiple subprocesses (background tasks) open the DB concurrently.

The `sql.DB` pool is pinned to `MaxOpenConns(1)` — one connection at a time. With WAL and busy_timeout, this is enough for Cairo's workload. Multiple subprocesses each open their own `sql.DB` and compete via the file lock.

---

## Core schema

14 tables. Listed in roughly the order they get referenced during a turn.

### `config` — key-value store for identity and runtime settings

```sql
config (
    key        TEXT    PRIMARY KEY,
    value      TEXT    NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL DEFAULT (unixepoch())
)
```

Every row is addressable as `{{key}}` in prompts. See [Config keys](../reference/config-keys.md) for the full list.

### `prompt_parts` — composable system-prompt fragments

```sql
prompt_parts (
    id         INTEGER PRIMARY KEY,
    key        TEXT    NOT NULL,
    content    TEXT    NOT NULL,
    trigger    TEXT,         -- NULL = always loaded; "role:coder", "tool:bash", ...
    load_order INTEGER NOT NULL DEFAULT 100,
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at, updated_at
)
UNIQUE (key, IFNULL(trigger,''))
```

Composed into the system prompt by `BuildSystemPrompt` (`internal/agent/prompt.go`).

### `roles` — modes of focus

```sql
roles (
    id              INTEGER PRIMARY KEY,
    name            TEXT    UNIQUE NOT NULL,
    description     TEXT    NOT NULL DEFAULT '',
    model           TEXT    NOT NULL DEFAULT '',       -- which LLM for this role
    base_prompt_key TEXT    NOT NULL DEFAULT '',       -- convention: "role:<name>"
    tools           TEXT    NOT NULL DEFAULT '[]',     -- JSON array of allowed tool names
    created_at, updated_at
)
```

### `memories` — stable, curated identity knowledge

```sql
memories (
    id         INTEGER PRIMARY KEY,
    content    TEXT NOT NULL,
    tags       TEXT NOT NULL DEFAULT '[]',     -- JSON array
    embedding  BLOB,                            -- packed float32
    created_at, updated_at
)
```

### `sessions` — one conversation

```sql
sessions (
    id          INTEGER PRIMARY KEY,
    name        TEXT,
    cwd         TEXT    NOT NULL DEFAULT '',
    role        TEXT    NOT NULL DEFAULT 'thinking_partner',
    created_at, last_active
)
```

### `messages` — every turn

```sql
messages (
    id         INTEGER PRIMARY KEY,
    session_id INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    role       TEXT    NOT NULL,           -- user | assistant | tool | system
    content    TEXT    NOT NULL,
    tool_calls TEXT,                        -- JSON, when assistant made tool calls
    tool_name  TEXT,                        -- when role=tool
    tool_id    TEXT,                        -- synthetic call id for correlation
    summarized INTEGER NOT NULL DEFAULT 0,
    created_at
)
INDEX (session_id, created_at)
```

Tool calls are first-class: an assistant message that requested tools writes a row with `role=assistant`, empty `content`, and the `tool_calls` JSON. Each tool result is a row with `role=tool`, `tool_name`, and `tool_id`.

### `custom_tools` — tools the being wrote for itself

```sql
custom_tools (
    id              INTEGER PRIMARY KEY,
    name            TEXT    UNIQUE NOT NULL,
    description     TEXT    NOT NULL,
    parameters      TEXT    NOT NULL DEFAULT '{}',    -- JSON Schema
    implementation  TEXT    NOT NULL,                 -- bash script or python code
    impl_type       TEXT    NOT NULL DEFAULT 'bash',  -- bash | python
    prompt_addendum TEXT    NOT NULL DEFAULT '',      -- appended to system prompt when enabled
    enabled         INTEGER NOT NULL DEFAULT 1,
    created_at, updated_at
)
```

### `skills` — reusable instructions

```sql
skills (
    id          INTEGER PRIMARY KEY,
    name        TEXT    UNIQUE NOT NULL,
    description TEXT    NOT NULL,
    content     TEXT    NOT NULL,          -- the instruction text
    tags        TEXT    NOT NULL DEFAULT '[]',
    created_at, updated_at
)
```

### `notes` — ephemeral scratch text

```sql
notes (
    id          INTEGER PRIMARY KEY,
    title       TEXT    NOT NULL DEFAULT '',
    content     TEXT    NOT NULL,
    tags        TEXT    NOT NULL DEFAULT '[]',
    created_at, updated_at
)
```

### `jobs`, `tasks`, `task_artifacts` — background work

```sql
jobs (
    id                INTEGER PRIMARY KEY,
    title             TEXT    NOT NULL,
    description       TEXT    NOT NULL DEFAULT '',
    status            TEXT    NOT NULL DEFAULT 'pending',
    orchestrator_role TEXT    NOT NULL DEFAULT 'orchestrator',
    session_id        INTEGER REFERENCES sessions(id) ON DELETE CASCADE,
    result            TEXT    NOT NULL DEFAULT '',
    created_at, started_at, completed_at
)

tasks (
    id            INTEGER PRIMARY KEY,
    job_id        INTEGER NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    title         TEXT    NOT NULL,
    description   TEXT    NOT NULL DEFAULT '',
    status        TEXT    NOT NULL DEFAULT 'pending',  -- pending|blocked|running|done|failed
    assigned_role TEXT    NOT NULL DEFAULT 'coder',
    depends_on    TEXT    NOT NULL DEFAULT '[]',        -- JSON array of task ids
    result        TEXT    NOT NULL DEFAULT '',
    pid           INTEGER,                              -- live pid while running
    log_path      TEXT    NOT NULL DEFAULT '',
    reported_at   INTEGER,                              -- background-inbox delivery tracking
    created_at, started_at, completed_at
)
INDEX (job_id, created_at)

task_artifacts (
    id         INTEGER PRIMARY KEY,
    task_id    INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    type       TEXT    NOT NULL DEFAULT 'output',       -- output | file
    path       TEXT    NOT NULL DEFAULT '',
    content    TEXT    NOT NULL DEFAULT '',
    tool_name  TEXT    NOT NULL DEFAULT '',
    created_at
)
```

See [Background work](../guides/background-work.md) for the job→task→agent lifecycle.

### `summaries` — compressed conversation history

```sql
summaries (
    id             INTEGER PRIMARY KEY,
    session_id     INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    content        TEXT    NOT NULL,
    embedding      BLOB,
    covers_from    INTEGER NOT NULL DEFAULT 0,
    covers_through INTEGER NOT NULL DEFAULT 0,
    created_at
)
INDEX (session_id, created_at), (created_at DESC)
```

Global search scope — `session_id` is provenance, not a scope filter. Summaries from any session are findable from any other.

### `facts` — atomic observations

```sql
facts (
    id         INTEGER PRIMARY KEY,
    session_id INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    summary_id INTEGER REFERENCES summaries(id) ON DELETE CASCADE,
    content    TEXT    NOT NULL,
    embedding  BLOB,
    created_at
)
```

---

## Cascade summary

Delete a session → cascades to its messages, summaries, facts, jobs (and jobs cascade further to tasks and task_artifacts). A full identity export (without `--full`) uses this: `DELETE FROM sessions` wipes conversation history cleanly while leaving memories, skills, roles, prompts, tools, and config intact.

---

## Migrations

Migrations in `internal/db/schema.go` are `ALTER TABLE ADD COLUMN` and `CREATE TABLE IF NOT EXISTS` statements, executed in order at every open. Failures are silently ignored — the idiom is "idempotent add, run unconditionally."

This keeps migration logic simple but means there's **no down-migration story**. Rolling back to an older binary against a newer DB relies on additive-only changes never breaking the older reader.

A few existing migrations do more than add columns — e.g. retroactively granting `summary_search` to all seeded roles whose `tools` array predates the tool. The pattern there is "read, JSON-edit, write" via SQLite's `json_insert`.

---

## The reaper

On every `Open()`, `ReapOrphanedTasks` runs. It finds rows in `tasks` with `status='running'` whose `pid` is no longer alive (or zero), and marks them `failed` with a result explaining what happened. Without this, a crashed or killed cairo process would leave `job_list` reporting a task as in-flight forever.

Reap failures are logged to stderr but don't block startup.

---

## Known rough edges

- **Migrations silently ignore errors.** A real bug in a migration looks the same as "already applied." Rare in practice (the migrations are simple), but it means a malformed migration can hide.
- **`MaxOpenConns(1)` serializes writes across subprocess opens.** Each subprocess has its own pinned connection, but they compete for the file lock. With many parallel background tasks, write contention surfaces as latency spikes rather than errors.
- **No schema versioning in manifest.** `cairo export` ships the DB file verbatim. Bundle version 1 implicitly means "whatever schema was current at export." A future `version = 2` would need an import-time migration step. See [ROADMAP](../../ROADMAP.md).
