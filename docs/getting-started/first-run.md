# First run

The first time you meet the being, nothing about you is stored yet. Cairo ships with a default identity named Selene — a character sketch, a soul, five roles, a handful of skills — but knows nothing about who you are or what you're working on.

The `/init` skill is how that gap gets closed.

---

## What `/init` does

Run in a fresh session:

```bash
cairo -new
> /init
```

What follows is a conversation. Selene asks questions one at a time — pausing for each answer before moving to the next — and stores what you tell her as she goes. It takes 10-20 minutes if you engage fully, and produces a DB that knows:

- Your name (`config.user_name`)
- What you're working on (as memories)
- How you prefer to work (as prompt parts — "be terse," "avoid comments," etc.)
- What commands matter (build, test, deploy — stored in config or memories)
- Anything you've told her never to do (prompt parts with high priority)

And, if there's a codebase in the current directory, a tour of it:

- Architecture overview
- Key files and their purpose
- Coding conventions noticed
- Commands needed to build/test/run

Stored as memories, one per fact. Future sessions start with all of this already in the system prompt.

---

## The conversation shape

`/init` doesn't dump a giant form on you. It runs in phases:

**Phase 0 — Check.** Selene reads the current config to see what's already set. If your name is already stored, she won't re-ask it.

**Phase 1 — Meet.** "Hi, I'm Selene. I don't know you yet. What should I call you?" Stores `user_name`.

**Phase 2 — Project.** "What are we working on?" → "Is there an existing codebase here?" → if yes, explore it (ls, find, read key files) before asking further questions.

**Phase 3 — Working style.** "How do you prefer to work with me?" (pair programmer / executor / advisor / sounding board). "How direct should I be?" "What matters most in this work?"

**Phase 4 — Conventions.** "What coding conventions should I always follow?" "What commands should I know?" "Anything I should never do?"

Each answer is stored immediately — `config(action="set", ...)` for identity values, `memory(action="add", ...)` for facts, `prompt_part(action="add", ...)` for always-on behaviors.

**Phase 5 — Summary.** Selene reviews what she stored, flips `init_complete=true`, and says she's ready.

---

## If you'd rather not have the conversation

Three alternatives:

**Skip `/init` entirely.** Cairo works fine without it. You'll get a one-line hint on every startup until you set `init_complete=true` manually:

```
> use config to set init_complete to true
```

**Skip the personal questions, do the codebase tour only:**

```
> /init codebase
```

Runs just the exploration phase — no questions about you, just learns about the current working directory.

**Direct config.** Skip the skill, set identity values manually:

```
> use config to set user_name to Scot
> use config to set init_complete to true
> add a memory: this project is a Go-based AI coding harness using Ollama
```

Less warm, but if you want a laconic setup it works.

---

## What gets stored

You can inspect what `/init` stored via:

```
> show me my memories
> show me my config
> show me my prompt parts
```

Or directly from the DB:

```bash
sqlite3 ~/.cairo2/cairo.db "SELECT content FROM memories;"
sqlite3 ~/.cairo2/cairo.db "SELECT key, value FROM config;"
sqlite3 ~/.cairo2/cairo.db "SELECT key, content FROM prompt_parts WHERE trigger IS NULL;"
```

Good init conversations typically produce:

- 5-15 memories about the project
- 3-6 prompt parts for working style
- 2-3 config keys for identity (user_name, project_name, maybe preferred_language)

If you see dozens of tiny memories ("user's name is Scot", "Scot prefers terseness", "Scot works in Go", etc.), that's fine — Cairo will deduplicate via search over time, and you can merge memories manually (`memory(action="update", ...)`).

---

## Redoing `/init`

Run it again whenever your context shifts:

- Starting a new project → `/init codebase` in the new directory
- Major change in how you want to work → `/init` to recapture preferences

Both are non-destructive. `/init` uses `memory(action="add")` which creates new rows; old memories aren't overwritten. If you want a cleaner slate, delete the memories you don't want first:

```
> delete memories 3 through 8 — they're outdated
```

---

## What the being is actually doing

Under the hood, `/init` is a skill — a row in the `skills` table with a `content` field that reads roughly:

```
Phase 0: check config for what's set
Phase 1: ask for the user's name, store as config.user_name
Phase 2: ask about the project, explore the codebase if present
...
```

The skill is plain text. You can read it (`skill(action="read", name="init")`), edit it (`skill(action="update", ...)`), or write your own follow-up skill that picks up where `/init` leaves off.

See [Skills](../guides/skills.md) for how skills work.

---

## Common concerns

**"I don't want my name stored."** Don't tell it your name. You can cairo just fine as "the user" — Selene's prompts just won't address you by name.

**"I don't want it writing code memories."** `/init` asks what conventions matter; if you say "I don't want you memorizing my style," Selene won't. The skill is a guide, not a checklist.

**"I changed my mind — can I delete everything?"** Yes. `rm ~/.cairo2/cairo.db` is a clean reset. The next `cairo` run reseeds defaults. You lose nothing that wasn't in the DB to begin with.

---

## After `/init`

The hint line (`(Selene is here but hasn't met you yet…)`) stops appearing. Sessions open straight to the prompt. Selene addresses you by name. The being starts every session already knowing your project, your preferences, your working style.

From there, you're just having conversations with the being, and identity accretes naturally — memories added when you hit something worth keeping, soul revised when it drifts, prompt parts added when behavior needs correcting. Selene's self grows with use.
