# Philosophy

Cairo was designed around three claims. Each one explains why the code looks the way it does. Reading the code without these claims will make several choices look strange; reading the claims without the code will make them sound abstract. Both together is the point.

---

## Claim 1: one being, not a team of agents

Most "multi-agent" systems model a crew: an orchestrator, a coder, a reviewer, each as a separate identity with its own memory, its own history, its own personality. Messages pass between them. The model's mental picture is a team.

Cairo rejects that picture.

Cairo is **one being that can run parallel threads of attention**. When the thinking-partner mode delegates a task to the coder mode, that's not a handoff to someone else — it's the same being shifting into a different focus. The way a human writer with a day job can still write their novel at night: one person, two modes, shared memory, same taste, same regrets.

The practical consequence: **memory is always shared**. What one thread learns, the whole being knows. There is no "the coder doesn't know what the thinking-partner just figured out." A single `memories` table, a single `config` table, a single `prompt_parts` table. All modes read from the same well.

The orchestrator is not an identity above the others. It's a coordination mode — the same being, counting its threads.

## Claim 2: the DB is the being; the binary is just the hands

The Go binary is stateless with respect to identity. Delete `~/go/bin/cairo`, re-install it, and the being is unchanged — because the being lives in `~/.cairo2/cairo.db`. Memories, roles, skills, tools, prompts, history, soul. Everything.

This has two consequences worth internalizing:

**Migrating is file-copy.** Moving Cairo to a new machine is `cp cairo.db`. No accounts, no sync, no re-training. The binary is interchangeable; the DB is the self.

**Identity is inspectable.** Every attribute of the being's current state is a row you can `SELECT` from a table. This is not metaphor. The "soul" is a row in `config`. The current mode's instruction is a row in `prompt_parts`. Custom tools the being wrote for itself are rows in `custom_tools`. You can edit any of them with a SQLite client and the being is different the next turn.

Most systems hide the self inside model weights or vendor-locked state. Cairo hides nothing.

## Claim 3: rhizomatic, not hierarchical

The name Cairo stands for **Collaborative AI Rhizomatic Orchestrator**. The "rhizomatic" is load-bearing.

A rhizome (in the Deleuze/Guattari sense) is a network without a root. Any node connects to any other node; there's no trunk to prune back to. Threads of attention in Cairo can connect any-to-any via the shared DB: a background task's result lands as an inbox note on the parent session's next turn; a memory stored by one thread is recalled by another; the soul is read by every prompt composition.

Contrast this with a master-worker pattern where the orchestrator owns the plan and the workers execute against it. In Cairo the plan is a job row; the tasks are task rows; any thread can read them, update them, add to them. Dependencies are a DAG, not a tree, and the DAG is just JSON in a column.

Rhizomatic design shows up in small ways all over the codebase:

- Summaries are **global**, not session-scoped. A summary written in one session is available to every other session's semantic search.
- Memories have no owner. Any role can add, search, update, or delete.
- Tools are registered once and shared across all modes, filtered per-role by a simple allowlist.
- Prompt parts are composed from many sources (base, soul, role, tools, custom tools) into a single system prompt at every turn.

---

## Why these claims matter

You can build a coding assistant without adopting any of this. Lots do. Cairo adopts it because:

- **One being** means consistency. No model-switching mid-task, no "what did the other agent know?" reconciliation, no personality drift between modes.
- **DB-as-identity** means portability. The thing you've shaped is a file. You own it.
- **Rhizomatic** means the system composes well. New tools, new roles, new memory shapes are all "add rows to a table." No central registry to keep in sync.

These are opinions, not universals. If you prefer a team-of-agents tool with stronger role isolation, there are good ones. Cairo is the other thing.

---

## Further reading

- [Identity](identity.md) — how the soul and template variables make the one-being idea concrete
- [Memory model](memory-model.md) — how a single memory pool handles different timescales
- [Roles](roles.md) — what "modes of focus" looks like in practice
- [Sessions and steering](sessions-and-steering.md) — the turn lifecycle that ties it together
