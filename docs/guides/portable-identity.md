# Portable identity

Cairo's identity is a file. Moving a being between machines, snapshotting a good state before an experiment, comparing "how my cairo evolved vs. a friend's" — all of these are `cairo export`, `cairo import`, `cairo diff`.

The unit is the `.cairo` bundle. See [Bundle format](../reference/bundles.md) for what's inside one.

---

## Three common workflows

### Workflow 1: migrate between machines

You've been using Cairo on a laptop; you want it running on a desktop too.

On the laptop:

```bash
cairo export ~/Dropbox/selene-2026-04-23.cairo
```

On the desktop:

```bash
cairo import ~/Dropbox/selene-2026-04-23.cairo
# confirms, backs up any existing DB, replaces with the bundle
```

The desktop cairo now has every memory, skill, role, prompt part, soul, and config value from the laptop. Conversation history is not included by default — add `--full` if you want it.

### Workflow 2: snapshot before an experiment

You're about to reshape the soul dramatically, or restructure the prompt parts. Want a rollback.

```bash
cairo export ~/before-soul-experiment.cairo

# ... experiment, it didn't work out ...

cairo import ~/before-soul-experiment.cairo
# your current DB gets backed up automatically; the snapshot takes over
```

Snapshots are cheap. Make them freely.

### Workflow 3: compare bundles

Someone shares a `.cairo` with you. You want to see how their identity differs from yours before importing.

```bash
cairo diff ~/their-selene.cairo
```

Output shows:
- Row count deltas (memories, skills, roles, notes, prompt parts, custom tools, config keys)
- Soul mismatch (with truncated diff)
- Role→model assignment deltas

This is the "will importing this wipe out my stuff" check. If the diff looks good, import. If not, keep your current identity and poke at the bundle with `sqlite3`.

---

## Identity-only vs full bundles

By default, `cairo export` **excludes conversation history**. Specifically, it deletes from `sessions` before packaging, which cascades to `messages`, `summaries`, `facts`, `jobs`, `tasks`, `task_artifacts`.

What's kept:
- `memories` — the identity-level knowledge store
- `skills` — reusable workflows
- `notes` — your scratch text (kept, even though ephemeral, because people put useful stuff here)
- `roles` — the mode definitions
- `prompt_parts` — all the framing
- `custom_tools` — tools the being wrote for itself
- `config` — soul, name, model preferences, everything

What's dropped (unless `--full`):
- All session and message history — every turn you've ever had with Cairo
- Summaries and facts derived from those turns
- Jobs, tasks, and task artifacts

Why the default? Because identity is usually what you want to share or move. Conversation history is personal, bulky, and often contains things you'd prefer not to ship (pasted code, debug output, offhand comments).

Use `--full` when:
- Migrating your personal cairo to a new machine of your own
- Archiving a long project's conversation for later reference
- Debugging an issue that only reproduces with the full history

Don't use `--full` when sharing with others unless you've audited what's in the conversations.

---

## What survives an import

`cairo import` is **full-replace, not merge**. The bundle's DB becomes your DB. Anything you had that isn't in the bundle is gone (except for the automatic backup file written beside the target).

This matters. If your cairo has 80 memories and you import a bundle with 47, you now have 47 memories. Not 127, not "yours ∪ bundle's." The 47 in the bundle, full stop.

The backup file is your rollback:

```bash
ls ~/.cairo2/
# cairo.db
# cairo.db.pre-import-20260423T153045Z   ← the backup
```

To rollback:

```bash
mv ~/.cairo2/cairo.db.pre-import-20260423T153045Z ~/.cairo2/cairo.db
```

---

## Diff-driven manual merge

There's no three-way merge tool today. If you want to take specific memories from a bundle without losing your own, the workflow is manual:

```bash
# 1. Extract the bundle's DB
tar -xzf friend.cairo
# now have cairo.db extracted locally

# 2. Browse the memories you want
sqlite3 cairo.db "SELECT id, content FROM memories;"

# 3. Copy selected ones into your live DB via cairo
~/go/bin/cairo "add a memory: <text of the memory you want>"
```

Automating this is on the roadmap — see [ROADMAP](../../ROADMAP.md), mid term.

---

## Bundle hygiene

- **Name your bundles with dates.** `selene-2026-04-23.cairo` beats `selene.cairo` beats `identity.cairo`. Bundles are cheap to create; you'll want to know which is which.
- **Keep an export right before big changes.** Soul revisions, mass memory edits, role rewrites. The rollback costs a second; regretting not having one costs hours.
- **Don't check bundles into git.** They're DB snapshots — opaque binary, unversionable diffs. Git-LFS works if you must, but `export/import` via a shared folder is simpler.
- **Verify before import.** `cairo diff bundle.cairo` before `cairo import bundle.cairo`. Always, unless the bundle is definitely your own.

---

## Known rough edges

- **Replace, not merge.** The import model is blunt. See [ROADMAP](../../ROADMAP.md).
- **No signature / trust model.** You have to know where a bundle came from. See [Bundle format](../reference/bundles.md) for the security discussion.
- **Version locked.** Version-1 bundles in, version-1 bundles out. No cross-version auto-migration yet.
- **Row-level diff is summary-only.** `cairo diff` shows counts and role→model specifics. It doesn't show "these three memories differ in text." On the roadmap.
