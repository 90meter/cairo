# Memory model

Cairo has four kinds of persistent text — memories, summaries, facts, and notes. Each has a job. Understanding which is which is most of what you need to know about how Cairo "remembers."

All four live in the same SQLite database. All four are available to every role. Three of them carry embedding vectors for semantic search.

---

## Memories — stable, curated identity knowledge

**Table:** `memories` · **Tool:** `memory`

A memory is a fact about the user, the project, or the being's preferences that should persist indefinitely. Things like "the user prefers blunt, terse responses" or "this project uses modernc.org/sqlite, not mattn."

Memories are added deliberately — by the being during `/init`, or whenever it learns something worth keeping. They're the equivalent of notes a thoughtful colleague would take in a notebook they'd reread for months.

**Shape:**
- `content` — free-form text
- `tags` — JSON array, optional
- `embedding` — vector for semantic search, optional

**Access patterns:**
- On every turn, the `memory_limit` most-recent memories (default 15) are pasted into the system prompt under `## Memories`.
- The model can call `memory(action="search", query=...)` to pull older ones by semantic similarity.
- The model can update or delete memories — they're not immutable.

**When to prefer memories:** things you want the being to *start every turn already knowing*, not just *be able to recall on demand*.

---

## Summaries — compressed conversation history

**Table:** `summaries` · **Tool:** `summary_search` (search-only)

A summary is a paragraph-sized distillation of a range of conversation turns. The summarizer goroutine (`internal/agent/summarizer.go`) runs after each turn if enough new messages have accumulated, and writes a single row covering messages `covers_from`…`covers_through`.

Summaries are **global** — they live across sessions. A summary written in one session is findable from another, which lets "oh, we figured this out last Thursday in the other terminal" actually work.

**Shape:**
- `content` — the summary paragraph
- `session_id` — provenance, not a scope filter
- `covers_from`, `covers_through` — message id range
- `embedding` — for semantic search

**Access patterns:**
- On every turn the most recent `summary_context` rows (default 4) are pasted into the system prompt under `## Conversation context`.
- The model can call `summary_search(query=...)` to pull relevant older summaries from any session.

**When summaries are created:** automatically, after a turn, when the number of unsummarized messages in the current session exceeds `summary_threshold` (default 4). The summarizer model is configurable via `summary_model`; it defaults to `ministral-8b:latest` — small and fast, because this runs often.

---

## Facts — atomic observations extracted during summarization

**Table:** `facts` · **Tool:** `fact_promote`

When the summarizer writes a summary, it also extracts atomic facts — single observations like "user's name is Scot" or "project uses Go 1.25." Facts are the raw material that could, if judged durable, become memories.

**Shape:**
- `content` — one-sentence observation
- `session_id`, `summary_id` — provenance
- `embedding` — for search

**Access patterns:**
- Facts are not injected into the prompt automatically — they'd be too noisy.
- The `fact_promote` tool lets the being promote a fact to a memory (with optional rewording) when it decides the fact deserves permanence.

**When facts matter:** they're the bridge between "we talked about X last session" and "the being *always knows* X." Summaries preserve the gist; facts preserve the atoms; promotion preserves them forever.

---

## Notes — ephemeral scratch space

**Table:** `notes` · **Tool:** `note`

Notes are free-form scratch text. A draft, a working plan for a multi-turn job, a list of things to come back to. They don't go into the prompt automatically; the being reads them when it decides to.

**Shape:**
- `title`
- `content`
- `tags` — JSON array

**When to prefer notes over memories:** for work-product, drafts, or context that isn't a "fact about the world." A note saying "plan for refactor X: step 1..., step 2..." doesn't belong in memories (it's not identity-level) but also shouldn't disappear when the session ends.

---

## How it fits together

Here's a rough picture of a turn's interaction with the memory model:

```
Turn N:
  incoming user message
  → system prompt composed:
      + ## Memories (top 15, always loaded)
      + ## Conversation context (top 4 summaries, always loaded)
  → LLM streams response, may call:
      memory(add|search|update)     — permanent stuff
      note(create|list|read|update) — scratch work
      summary_search(query=...)     — rare, for older context
      fact_promote(id=...)          — curate fact → memory
  → turn completes
  → background summarizer runs:
      if unsummarized messages ≥ summary_threshold:
          write a summary row
          write any extracted fact rows
```

Every rough corner of this has a note in [ROADMAP.md](../../ROADMAP.md) — FTS5 alongside embeddings, row-level diff, leaner search on large memory tables. The model as described is simple; the implementation has known scale corners.

---

## Known rough edges

- `memory(action="search")` does a full-table decode of every embedding BLOB per query. Fine at hundreds of rows, noticeable at thousands. See ROADMAP, near-term.
- Embeddings don't carry their source-model identity. If you swap embed models mid-life, old rows return spurious zero-similarity scores. Re-embedding is manual.
- Summaries and facts are written by a small fast model (ministral-8b by default). The quality of the distillation is bounded by that model. If you'd rather spend tokens on stronger summaries, set `summary_model` to something heavier.
