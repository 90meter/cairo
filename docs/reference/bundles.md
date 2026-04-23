# Bundle format

A `.cairo` bundle is a **gzipped tar archive** containing two files: a manifest and a SQLite database snapshot. The format is deliberately simple — readable by any standard `tar` + `gunzip` + `sqlite3`.

---

## Layout

```
bundle.cairo                       # the file you ship
  └── (gzipped tar archive)
       ├── manifest.json
       └── cairo.db
```

Nothing else is permitted. `unpackBundle` rejects entries whose basename differs from their full name (no path traversal) and ignores unknown entries for forward compatibility.

---

## `manifest.json`

```json
{
  "version": "1",
  "exported_at": "2026-04-22T15:30:00Z",
  "includes_history": false,
  "counts": {
    "memories": 47,
    "skills": 3,
    "notes": 0,
    "roles": 5,
    "prompt_parts": 12,
    "custom_tools": 1,
    "config_keys": 14,
    "sessions": 0,
    "messages": 0,
    "summaries": 0,
    "facts": 0,
    "jobs": 0,
    "tasks": 0
  }
}
```

Fields:

- **`version`** — current is `"1"`. Enforced exactly on import; no auto-migration between versions yet.
- **`exported_at`** — UTC timestamp of when the bundle was produced.
- **`includes_history`** — `true` if the bundle was created with `--full`; `false` (default) means sessions and everything cascaded from them (messages, summaries, facts, jobs, tasks, task_artifacts) have been stripped before packaging.
- **`counts`** — row counts per table, captured post-strip. These are what `cairo diff` reads.

---

## `cairo.db`

A SQLite database file, produced via `VACUUM INTO` so it's a clean, consistent snapshot regardless of concurrent writers on the source. The snapshot goes through one more step if `--full` was not passed: `DELETE FROM sessions` (with foreign keys on), which cascades through `messages`, `summaries`, `facts`, `jobs`, `tasks`, `task_artifacts` — leaving identity (memories, roles, prompts, tools, skills, notes, config) intact.

After the strip, a `VACUUM` reclaims the freed space so the bundle size matches the identity content.

The schema inside is the **current cairo schema** — everything in [Database](../architecture/database.md) applies. The bundle is self-describing: a future cairo can read it, count, diff, and import without out-of-band schema info.

---

## Producing a bundle

```bash
cairo export [--full] <path.cairo>
```

Writes a single file. Exit status is 0 on success, non-zero with a message on error.

Default (identity-only) bundles are small — typical ones are a few dozen KB. `--full` bundles depend on conversation volume but are usually still under a megabyte.

---

## Consuming a bundle

Two commands:

**`cairo import`** replaces the current DB with the bundle's DB, after backing up the existing DB alongside.

**`cairo diff`** compares the bundle's contents to the local DB without touching anything. Output covers:
- Count deltas per table (with `*` marker on differences)
- Soul match/mismatch (full text diff if different, truncated)
- Role→model assignment deltas

---

## Interacting without cairo

Because a bundle is just `tar.gz` of standard pieces, you can poke at one without cairo:

```bash
# Extract
tar -xzf bundle.cairo
cat manifest.json
sqlite3 cairo.db "SELECT key, value FROM config;"
sqlite3 cairo.db "SELECT id, content FROM memories;"
sqlite3 cairo.db "SELECT name, model FROM roles;"
```

This is by design. The bundle format is inspection-friendly.

---

## Versioning policy

The current manifest version is `"1"`. An import of a version-2 bundle by a version-1 cairo errors out — no partial / best-effort reads.

Additive schema changes (new columns, new tables) don't require a version bump; an older cairo reads a newer-but-additively-changed DB fine because all queries are explicit about columns.

Incompatible changes (column renames, removals, type changes) would bump to `"2"` and add an import-time migration step. See [ROADMAP](../../ROADMAP.md).

---

## Security and trust

A bundle carries no signature today. Anyone can produce one, and an import overwrites your DB (after backup). **Treat bundles like you'd treat any `sqlite3 ~/.cairo2/cairo.db < foreign.sql` — don't import bundles from sources you don't trust.**

Signed bundles are on the far horizon — see [ROADMAP](../../ROADMAP.md). Until then, the `cairo diff` command is the mitigation: inspect what you're about to import before you import it.

The backup written before import is your rollback:

```bash
# Restore previous state after a regretted import
mv ~/.cairo2/cairo.db.pre-import-20260423T153045Z ~/.cairo2/cairo.db
```

---

## Known rough edges

- **Version lock is strict.** A version-1 bundle can only be imported by a cairo that understands version 1. There's no "upgrade this bundle on import" path yet.
- **`--full` bundles ship real conversations.** If those conversations include pasted secrets, they're in the bundle. `--full` is useful for moving between your own machines; share with care.
- **No selective import.** Import is all-or-nothing. You can't import just the memories, or just a single role. Selective import is a candidate for the "skills marketplace" direction — see [ROADMAP](../../ROADMAP.md).
