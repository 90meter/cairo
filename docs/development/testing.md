# Testing

Cairo has a small test suite — roughly 16 tests across 3 packages. It's meant to catch seam-level regressions (does the DB cascade work? does the prompt compose right?), not to pin every behavior.

```bash
go test ./...
```

All tests should pass on a clean checkout. A test failure should block merge.

---

## What's covered

- **`internal/db/`** — session lifecycle, cascade deletes, FK enforcement, schema+migration idempotency
- **`internal/tools/`** — memory tool round-trips (add, search with embeddings), registry filter correctness
- **`internal/agent/`** — prompt composition order and template substitution

What's *not* covered:
- `internal/agent/loop.go` — the actual turn loop (would need an LLM stub)
- `internal/llm/chat.go` — Ollama interop (would need an HTTP stub)
- Every individual built-in tool (only a couple are tested directly)
- `internal/tui/` — Bubble Tea model (not test-friendly by default)
- `cmd/cairo/bundle.go` — export/import/diff roundtrips

These aren't in the suite because they'd need non-trivial fakes (LLM client, HTTP server, tea.Program harness). Candidates for future work, not urgent.

---

## Running a subset

```bash
go test ./internal/db/...         # just DB tests
go test ./internal/tools/...      # just tools tests
go test -run TestMemory ./...     # just tests matching name
go test -v ./...                  # verbose — prints each test
```

---

## Writing a new test

The existing tests show the idioms:

**Use a tempdir DB.** Don't touch `~/.cairo2`. `db.OpenAt(path)` takes an explicit path; use `t.TempDir()`:

```go
func TestSomething(t *testing.T) {
    dbpath := filepath.Join(t.TempDir(), "cairo.db")
    database, err := db.OpenAt(dbpath)
    if err != nil {
        t.Fatalf("open: %v", err)
    }
    defer database.Close()

    // ...
}
```

See `internal/db/testhelper_test.go` for the shared helper.

**Avoid LLM round-trips.** If you need to test code that would normally call the LLM, the answer is usually to extract the LLM-calling part into a separable function and test the pure bits. The `Tool` interface doesn't need a real LLM — `ctx.DB` is plenty for most tool tests.

**Test one thing.** Each test should fail for one reason. "Test that memory add + list works" is one thing. "Test that the whole memory tool works" is five things.

---

## Ollama in tests

Tests **shouldn't** require Ollama running. If you find yourself writing a test that needs a real Ollama, stop and ask whether the code under test can be refactored to isolate the HTTP call.

The rare legitimate case (integration-style test of the full LLM round-trip) would go under a build tag so it's off by default:

```go
//go:build ollama
```

Run with `go test -tags ollama ./...`. No such tests exist today; the pattern is ready if needed.

---

## CI

No CI is configured currently (`.github/workflows/` doesn't exist). A minimal workflow would run:

```bash
go build ./...
go vet ./...
go test ./...
```

on push and PR. Writing that is a plausible near-term task; it's not critical for a solo project but it's a quality-of-life improvement for anyone who forks.

---

## Coverage

`go test -cover ./...` reports coverage per package. Current numbers are modest — roughly 20-40% across the covered packages, with several packages at 0%. Don't chase coverage as a goal; chase *meaningful* coverage of the seams that would hurt if they broke.

---

## What to test when changing things

Rule of thumb — if you touch:

- **Schema or migrations** → add a test that opens a DB twice (idempotency) and one that exercises the new column/table
- **Prompt composition** → add a test that `BuildSystemPrompt` produces the expected structure
- **A tool's Execute method** → add a round-trip test: set up DB state, call Execute, assert on the result
- **The agent loop** → you probably need to refactor before you can test cleanly; ask in an issue first
- **The TUI** → no test expectation; run it by hand

---

## See also

- [Building](building.md) — getting the binary compiled
- [Contributing](contributing.md) — how test expectations relate to PR expectations
