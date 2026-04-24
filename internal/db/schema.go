package db

// schema is executed once at open time; each statement is idempotent.
const schema = `
CREATE TABLE IF NOT EXISTS config (
    key        TEXT    PRIMARY KEY,
    value      TEXT    NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE IF NOT EXISTS prompt_parts (
    id         INTEGER PRIMARY KEY,
    key        TEXT    NOT NULL,
    content    TEXT    NOT NULL,
    trigger    TEXT,
    load_order INTEGER NOT NULL DEFAULT 100,
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at INTEGER NOT NULL DEFAULT (unixepoch())
);
CREATE INDEX IF NOT EXISTS idx_prompt_trigger ON prompt_parts(trigger, enabled, load_order);

CREATE TABLE IF NOT EXISTS roles (
    id              INTEGER PRIMARY KEY,
    name            TEXT    UNIQUE NOT NULL,
    description     TEXT    NOT NULL DEFAULT '',
    model           TEXT    NOT NULL DEFAULT '',
    base_prompt_key TEXT    NOT NULL DEFAULT '',
    tools           TEXT    NOT NULL DEFAULT '[]',
    created_at      INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at      INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE IF NOT EXISTS memories (
    id         INTEGER PRIMARY KEY,
    content    TEXT NOT NULL,
    tags       TEXT NOT NULL DEFAULT '[]',
    embedding  BLOB,
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE IF NOT EXISTS sessions (
    id          INTEGER PRIMARY KEY,
    name        TEXT,
    cwd         TEXT    NOT NULL DEFAULT '',
    role        TEXT    NOT NULL DEFAULT 'thinking_partner',
    created_at  INTEGER NOT NULL DEFAULT (unixepoch()),
    last_active INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE IF NOT EXISTS messages (
    id         INTEGER PRIMARY KEY,
    session_id INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    role       TEXT    NOT NULL,
    content    TEXT    NOT NULL,
    tool_calls TEXT,
    tool_name  TEXT,
    tool_id    TEXT,
    created_at INTEGER NOT NULL DEFAULT (unixepoch())
);
CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, created_at);

CREATE TABLE IF NOT EXISTS custom_tools (
    id              INTEGER PRIMARY KEY,
    name            TEXT    UNIQUE NOT NULL,
    description     TEXT    NOT NULL,
    parameters      TEXT    NOT NULL DEFAULT '{}',
    implementation  TEXT    NOT NULL,
    impl_type       TEXT    NOT NULL DEFAULT 'bash',
    prompt_addendum TEXT    NOT NULL DEFAULT '',
    enabled         INTEGER NOT NULL DEFAULT 1,
    created_at      INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at      INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE IF NOT EXISTS skills (
    id          INTEGER PRIMARY KEY,
    name        TEXT    UNIQUE NOT NULL,
    description TEXT    NOT NULL,
    content     TEXT    NOT NULL,
    tags        TEXT    NOT NULL DEFAULT '[]',
    created_at  INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at  INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE IF NOT EXISTS notes (
    id          INTEGER PRIMARY KEY,
    title       TEXT    NOT NULL DEFAULT '',
    content     TEXT    NOT NULL,
    tags        TEXT    NOT NULL DEFAULT '[]',
    created_at  INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at  INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE IF NOT EXISTS jobs (
    id                INTEGER PRIMARY KEY,
    title             TEXT    NOT NULL,
    description       TEXT    NOT NULL DEFAULT '',
    status            TEXT    NOT NULL DEFAULT 'pending',
    orchestrator_role TEXT    NOT NULL DEFAULT 'orchestrator',
    session_id        INTEGER REFERENCES sessions(id) ON DELETE CASCADE,
    result            TEXT    NOT NULL DEFAULT '',
    created_at        INTEGER NOT NULL DEFAULT (unixepoch()),
    started_at        INTEGER,
    completed_at      INTEGER
);

CREATE TABLE IF NOT EXISTS tasks (
    id            INTEGER PRIMARY KEY,
    job_id        INTEGER NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    title         TEXT    NOT NULL,
    description   TEXT    NOT NULL DEFAULT '',
    status        TEXT    NOT NULL DEFAULT 'pending',
    assigned_role TEXT    NOT NULL DEFAULT 'coder',
    depends_on    TEXT    NOT NULL DEFAULT '[]',
    result        TEXT    NOT NULL DEFAULT '',
    created_at    INTEGER NOT NULL DEFAULT (unixepoch()),
    started_at    INTEGER,
    completed_at  INTEGER
);
CREATE INDEX IF NOT EXISTS idx_tasks_job ON tasks(job_id, created_at);
`

// migrations are applied idempotently via ALTER TABLE (errors silently ignored).
var migrations = []string{
	`ALTER TABLE tasks ADD COLUMN pid      INTEGER`,
	`ALTER TABLE tasks ADD COLUMN log_path TEXT NOT NULL DEFAULT ''`,
	// reported_at tracks whether a terminal-status task's completion has been
	// surfaced to the parent session as a background-activity note. NULL means
	// "unreported, still in the inbox."
	`ALTER TABLE tasks ADD COLUMN reported_at INTEGER`,
	`CREATE TABLE IF NOT EXISTS task_artifacts (
		id         INTEGER PRIMARY KEY,
		task_id    INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
		type       TEXT    NOT NULL DEFAULT 'output',
		path       TEXT    NOT NULL DEFAULT '',
		content    TEXT    NOT NULL DEFAULT '',
		tool_name  TEXT    NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL DEFAULT (unixepoch())
	)`,
	`CREATE INDEX IF NOT EXISTS idx_artifacts_task ON task_artifacts(task_id, created_at)`,
	`ALTER TABLE messages ADD COLUMN summarized INTEGER NOT NULL DEFAULT 0`,

	// Summaries are global — session_id is provenance, not a scope filter.
	// Semantic search works across all sessions so context bleeds helpfully.
	// ON DELETE CASCADE so Sessions.Delete sweeps them cleanly.
	`CREATE TABLE IF NOT EXISTS summaries (
		id             INTEGER PRIMARY KEY,
		session_id     INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
		content        TEXT    NOT NULL,
		embedding      BLOB,
		covers_from    INTEGER NOT NULL DEFAULT 0,
		covers_through INTEGER NOT NULL DEFAULT 0,
		created_at     INTEGER NOT NULL DEFAULT (unixepoch())
	)`,
	`CREATE INDEX IF NOT EXISTS idx_summaries_session ON summaries(session_id, created_at)`,
	`CREATE INDEX IF NOT EXISTS idx_summaries_created ON summaries(created_at DESC)`,

	// Facts are atomic observations extracted during summarization.
	// They can be promoted to global memories later. Cascade on session delete
	// directly; summary_id cascade handles per-summary cleanup too.
	`CREATE TABLE IF NOT EXISTS facts (
		id         INTEGER PRIMARY KEY,
		session_id INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
		summary_id INTEGER REFERENCES summaries(id) ON DELETE CASCADE,
		content    TEXT    NOT NULL,
		embedding  BLOB,
		created_at INTEGER NOT NULL DEFAULT (unixepoch())
	)`,
	`CREATE INDEX IF NOT EXISTS idx_facts_session ON facts(session_id, created_at)`,

	// prompt_parts had no uniqueness on (key, trigger) and seed uses
	// INSERT OR IGNORE, so every startup re-inserted all seeded parts.
	// Dedupe first, keeping the earliest row per (key, trigger), then
	// enforce uniqueness going forward. IFNULL collapses NULL triggers
	// so the index treats "no trigger" as a single identity.
	`DELETE FROM prompt_parts
	 WHERE id NOT IN (
	     SELECT MIN(id) FROM prompt_parts GROUP BY key, IFNULL(trigger, '')
	 )`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_prompt_parts_unique
	 ON prompt_parts(key, IFNULL(trigger, ''))`,

	// Retroactively grant new knowledge tools to existing seeded roles.
	// seedRoles uses INSERT OR IGNORE keyed on (name), so edits to the
	// seeded tool lists don't propagate to DBs created before the edit.
	// Grant summary_search to all five roles (all benefit from recall),
	// fact_promote only to thinking_partner (the curation role).
	`UPDATE roles SET tools = json_insert(tools, '$[#]', 'summary_search')
	 WHERE name IN ('thinking_partner','orchestrator','coder','planner','reviewer')
	   AND NOT EXISTS (SELECT 1 FROM json_each(roles.tools) WHERE value = 'summary_search')`,
	`UPDATE roles SET tools = json_insert(tools, '$[#]', 'fact_promote')
	 WHERE name = 'thinking_partner'
	   AND NOT EXISTS (SELECT 1 FROM json_each(roles.tools) WHERE value = 'fact_promote')`,
	`UPDATE roles SET tools = json_insert(tools, '$[#]', 'prompt_show')
	 WHERE name IN ('thinking_partner','orchestrator','coder','planner','reviewer')
	   AND NOT EXISTS (SELECT 1 FROM json_each(roles.tools) WHERE value = 'prompt_show')`,

	// init_complete is derived on first upgrade: a DB with any stored
	// memories has already been initialized (even if ad-hoc); a fresh
	// DB has none and should show the /init hint. INSERT OR IGNORE
	// makes this a one-time decision — later config_set calls win.
	`INSERT OR IGNORE INTO config(key, value)
	 SELECT 'init_complete',
	        CASE WHEN EXISTS (SELECT 1 FROM memories LIMIT 1) THEN 'true' ELSE 'false' END`,

	// Grant search, fetch, fact_list, and summary_rewrite to existing roles.
	`UPDATE roles SET tools = json_insert(tools, '$[#]', 'search')
	 WHERE name IN ('thinking_partner','planner')
	   AND NOT EXISTS (SELECT 1 FROM json_each(roles.tools) WHERE value = 'search')`,
	`UPDATE roles SET tools = json_insert(tools, '$[#]', 'fetch')
	 WHERE name IN ('thinking_partner','planner')
	   AND NOT EXISTS (SELECT 1 FROM json_each(roles.tools) WHERE value = 'fetch')`,
	`UPDATE roles SET tools = json_insert(tools, '$[#]', 'fact_list')
	 WHERE name = 'thinking_partner'
	   AND NOT EXISTS (SELECT 1 FROM json_each(roles.tools) WHERE value = 'fact_list')`,
	`UPDATE roles SET tools = json_insert(tools, '$[#]', 'summary_rewrite')
	 WHERE name = 'thinking_partner'
	   AND NOT EXISTS (SELECT 1 FROM json_each(roles.tools) WHERE value = 'summary_rewrite')`,

	// Wire the dream role to its prompt and ensure the prompt exists.
	`UPDATE roles SET base_prompt_key = 'role:dream' WHERE name = 'dream' AND (base_prompt_key IS NULL OR base_prompt_key = '')`,
	`INSERT OR IGNORE INTO config(key, value) VALUES('searxng_url', '')`,
}
