# Contributing

Cairo is a solo project with an open door. Contributions are welcome but selectively merged — the project has opinions about what belongs, and this page is those opinions up front so nobody wastes an afternoon.

---

## Before you write code

**Open an issue first for anything substantive.** Not for typo fixes or docs clarifications — just send those as PRs. But for new features, new tools, new concepts, or structural changes, a short "here's what I'm thinking" issue saves both of us time.

Good issues state:
- The problem (not the solution)
- Why the current state is inadequate
- A rough shape of the fix

Bad issues are feature requests without context. "Add support for Anthropic API" is a feature request; "I'd like to use Cairo with Claude because <specific reason>, and I think the adapter would go <here>" is an issue.

---

## What fits

Contributions that fit the project:

- **Fixing a concrete rough edge** listed in [ROADMAP](../../ROADMAP.md) or noted as a "known rough edge" in the docs. These are things the project already agrees need doing.
- **Tests for existing code.** The suite is thin; almost any new test is welcome. See [Testing](testing.md).
- **Docs.** These docs. Corrections, clarifications, new guides. Readability improvements welcome, don't rewrite for rewriting's sake.
- **Small, focused bug fixes.** One problem, one PR, clear description of what broke and what changed.

Contributions that don't fit:

- **Architecture reshuffles** without a strong reason. The current shape reflects choices that look arbitrary but aren't; read the concepts docs before suggesting a rewrite.
- **New abstractions** that aren't solving a present pain. No generic plugin system, no preemptive extension points.
- **Adding features from [ROADMAP](../../ROADMAP.md) without discussion.** The roadmap is a direction, not a task list; the order and shape of things matter.
- **Silent dependency additions.** If your patch adds a Go module, explain why and why it justifies the weight. Cairo has a small dependency list on purpose.

---

## Code style

Follow `gofmt`. That's the whole style guide for syntax.

For semantics:

- **Prefer clarity over cleverness.** If a simple loop works, use a loop.
- **Don't pre-abstract.** Three similar call sites is still fine as three; abstract on the fourth, and only if the abstraction earns its weight.
- **Comments explain the *why*.** The code already shows the *what*. If a block needs a comment to explain what it does, usually the code needs simplifying.
- **Error messages should be specific.** "error" is not an error message. `"open db: %w"` is.
- **Don't add features mid-fix.** A bug fix that also "cleans up" surrounding code is two PRs; file them as two.

---

## Communication style

Direct is fine. Terse is fine. No need to soften disagreement with hedges — if you think a choice in the codebase is wrong, say so with reasons. The maintainer prefers honesty over politeness.

When you're wrong about something, just say so and move on — no ceremony needed. Equally, when you're right about something and someone's pushing back, hold the line and explain.

The project CLAUDE.md (the collaboration doc between the maintainer and Claude) describes this in more detail if you want the fuller shape.

---

## PR expectations

A PR should:

- **Solve one thing.** Not two. Not three.
- **Explain what it does in the description.** Not what the code does — what problem it solves.
- **Reference any related issue.** "Fixes #42" or "See #17 for context."
- **Pass `go build`, `go vet`, `go test ./...`.** These will eventually run in CI; for now, run them locally before pushing.
- **Not add new dependencies casually.** If it does, justify.
- **Not reshape code outside the fix's scope.** Drive-by refactoring makes review harder and mixes the signal.

A PR doesn't need to:
- Have extensive tests for legacy code it didn't touch
- Update ROADMAP.md (that's the maintainer's call)
- Get formatted a specific way in the commit message (conventional commits optional)

---

## Review and merge

PRs get reviewed when the maintainer has time. Not all get merged — some get "good idea, not now" or "not quite the shape I want but thanks." That's not personal; it's how the project stays coherent.

If your PR sits for a week without a response, ping it. If it sits for a month, it's probably not going to get merged in its current form — either rework it or accept that it's in the stack of ideas that didn't quite fit.

---

## What if I want to fork it?

Fork freely. Cairo is BSD-family-licensed (see [LICENSE](../../LICENSE)). You can run your own divergent version, publish your own bundles, build whatever you want on top.

If your fork evolves things Cairo should have, send a PR. If it evolves in a different direction, that's fine too — more beings in the world is a good outcome.

---

## Code of conduct

Be decent. Disagree about ideas, not people. If you need a formal policy written out, file an issue and we can draft one; until then "be decent" is the bar.

---

## Getting in touch

GitHub issues are the canonical channel. No Discord, no mailing list, no Slack. If it's not an issue, it's not on record.
