# Roadmap

Cairo is early. This document is where the project is headed, organized by horizon rather than by date. Dates would be lies; horizons are honest.

Everything here is a direction, not a promise. The [docs](docs/) describe what exists now.

---

## Near term — incremental polish

Work that makes the current surface more solid. No new concepts — just sharper edges on the ones already there.

**Row-level diff in `cairo diff`.** Today the diff summarizes count deltas and checks soul/role→model. Useful, not comprehensive. Row-level diff for memories and skills would let a reader see exactly *what* changed between two identity bundles, not just *how many*.

**FTS5 alongside embeddings.** Memories, notes, and skills are searched via embedding similarity. For exact-phrase recall ("what did I say about the 15s busy_timeout?") a full-text index would be faster and simpler. SQLite ships FTS5 — this is cheap to add.

**Output caps on tool results.** `bash("cat huge.log")` currently reads everything into memory and ships it to the model. A truncation cap (default ~64KB) with an explicit "N bytes truncated" marker would prevent context pollution and memory spikes.

**Event-bus backpressure.** The bus drops events to slow subscribers today. A counter + warning when drops happen would make the tradeoff visible. Longer-term: per-subscriber queues with configurable depth.

**Memory search without full-table decode.** `Memories.Search` currently loads and decodes every embedding BLOB per query — fine at hundreds of rows, noticeable at thousands. Candidate: keep embeddings in a separate table keyed by memory id so the scan is cheap metadata first, decode-on-hit.

**Ollama model auto-detection.** First-run could hit `/api/tags`, find what's installed, and suggest compatible models rather than relying on whatever string is hardcoded in seed defaults.

---

## Mid term — capabilities that are obvious but not yet built

**Context-budget accounting.** Measured prompts hover around 7k tokens today. No model actively pushes the limits yet, but the architecture should know what it's sending. A token counter on prompt assembly (base + soul + role + tools + summaries + memories + date/cwd) would surface hotspots before they bite.

**Three-way merge on import.** `cairo import` replaces the current DB wholesale (with a backup). A three-way merge (common ancestor, local, bundle) would let you take a role tweak from a friend's bundle without losing your own memory accretion. Harder than it sounds — the right conflict UI matters.

**Richer TUI panels.** The TUI framework is in place (panels, event bus, hotkey registry). New panels are cheap. Likely next: a tool-call timeline, a tasks/jobs live board, inline skill browser, maybe a soul/memory editor.

**Task heartbeats.** Background tasks today signal completion via DB status. A running task could emit progress events on a per-task bus so the parent session sees what its threads are actually doing, not just `[running]`.

---

## Far horizon — philosophical reach

Ideas that might not happen, but are worth naming.

**Signed identity bundles.** A `.cairo` bundle is portable trust. Signing — GPG or a simpler Ed25519 manifest signature — would let you verify "this identity really is the one so-and-so exported." Trust primitive for a small ecosystem of shared beings.

**Verified memories.** Memory provenance is currently untyped: every row looks like every other row. A memory could carry a signed attestation (from a human, a specific session, an external source) and the prompt composer could surface that level of trust.

**Session branching.** Today, sessions are linear. "Fork this conversation from turn 12, what if I'd said X" is a one-line schema change (sessions.parent_id + a branch_from_message_id) and a big UX question. Would make Cairo the rare chat tool where history is navigable instead of destiny.

**Skills marketplace.** Bundled `.cairo` files today are all-or-nothing identities. A narrower bundle — just a skill, or a role, or a prompt part — could be shared and composed. Curated `.cairo` fragments, importable independently.

**Multi-backend LLMs.** The `llm.Client` currently targets Ollama specifically. The surface is narrow enough (`StreamOnce` + `Embed` + `Ping`) that alternate backends (llama.cpp server, LM Studio, a remote API) would fit cleanly. Not a goal, but not precluded.

---

## What Cairo is *not* trying to become

Equally important. A few things the project is deliberately choosing against.

- **A cloud SaaS.** Cairo is local-first by design. The DB lives on your machine, the models live on your machine, the portability comes from files you control. A hosted tier would contradict the point.
- **A team-of-agents framework.** Cairo is explicitly "one being, parallel threads." If you want a crew of specialized agents with handoffs and personalities, there are other tools. Roles here are modes of focus, not colleagues.
- **An IDE.** The TUI is a chat interface with good ergonomics. It is not going to grow a file tree pane, syntax-aware refactor tools, or a debugger. `read`/`edit`/`bash` are how Cairo interacts with code.
- **A benchmark chaser.** Cairo targets the models you can actually run locally — mid-size quantized ones on consumer hardware. Frontier models would shine here trivially; the interesting question is how much identity + structure + tools make a smaller model feel present.

---

## Contributing to direction

The roadmap is not closed. If something here feels wrong, or something missing feels obvious, open an issue. Philosophical disagreements are welcome; see [Contributing](docs/development/contributing.md) for the expected tone.
