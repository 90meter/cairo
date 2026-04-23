# Cairo documentation

Cairo — **C**ollaborative **A**I **R**hizomatic **O**rchestrator — is a local-first coding harness backed by SQLite and Ollama. One being, many parallel threads of attention, with its complete identity stored in a single `.db` file.

This directory is the full documentation. The top-level [`README.md`](../README.md) is the landing page; start there if you're new. The [`ROADMAP.md`](../ROADMAP.md) covers where the project is headed.

---

## Getting started

New to Cairo? Read these in order.

- [Installation](getting-started/installation.md) — Go, Ollama, models, first build
- [Quickstart](getting-started/quickstart.md) — your first session in five minutes
- [First run](getting-started/first-run.md) — meeting Selene and running `/init`

## Concepts

The ideas that make Cairo coherent. Read these before the architecture docs — they explain *why* the architecture is shaped the way it is.

- [Philosophy](concepts/philosophy.md) — one being, rhizomatic threads, SQLite-as-identity
- [Identity](concepts/identity.md) — soul, ai_name, prompt composition, template substitution
- [Memory model](concepts/memory-model.md) — memories, summaries, facts, notes — when each
- [Roles](concepts/roles.md) — modes of focus, tool allowlists, role-specific models
- [Sessions and steering](concepts/sessions-and-steering.md) — turn lifecycle, resume, steering queue

## Architecture

How the code is laid out. Read these after concepts.

- [Overview](architecture/overview.md) — subsystem diagram and data flow
- [Database](architecture/database.md) — schema, WAL, cascades, migrations
- [Agent loop](architecture/agent-loop.md) — `runLoop`, event bus, tool-call iteration
- [LLM client](architecture/llm-client.md) — Ollama interop, `StreamOnce`, embeddings
- [TUI](architecture/tui.md) — Bubble Tea model, panels, hotkeys

## Reference

Dense tables and lookups.

- [CLI](reference/cli.md) — every flag and subcommand
- [Built-in tools](reference/tools.md) — the 23 tools the model can call
- [Config keys](reference/config-keys.md) — every row of the `config` table
- [Bundle format](reference/bundles.md) — what's inside a `.cairo` file

## Guides

Task-shaped walkthroughs.

- [Custom tools](guides/custom-tools.md) — how the AI writes its own tools
- [Skills](guides/skills.md) — reusable instructions: `/init`, `/init_codebase`, your own
- [Background work](guides/background-work.md) — jobs, tasks, and the dependency DAG
- [Portable identity](guides/portable-identity.md) — export, import, diff

## Development

For contributors and readers cloning the repo.

- [Building](development/building.md) — toolchain, `make build`, `make install`
- [Testing](development/testing.md) — what's covered, how to add tests
- [Contributing](development/contributing.md) — code style, PR expectations

---

## A note on honesty

These docs document the **current state of the code**, including known rough edges. If something reads as imperfect or incomplete, that's deliberate — [ROADMAP.md](../ROADMAP.md) is where the "what it will become" lives. Cairo is early. It works, it's useful, and it has rough corners. Everything here is pitched at a reader who'd rather see the shape of what's really there than a polished sales brochure.
