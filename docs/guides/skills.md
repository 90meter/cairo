# Skills

A skill is a **reusable instruction** — a block of prose the being can invoke to run a specific flow. Unlike tools (which execute code), skills are pure content: they're pasted into a user turn, and the model responds to them like any other instruction.

Skills are how Cairo packages repeatable workflows without needing to hard-code them.

---

## Two built-in skills

Seeded into every new DB:

### `/init` — guided setup
The long version. Introduces the being, asks the user's name, learns about the project, asks about working preferences, explores the codebase if present, and stores everything as memories + prompt parts. Runs ~15-30 minutes of conversation.

Trigger: `/init` (slash command) or `skill(action="read", name="init")` then paste the content as a user turn.

### `/init codebase` — codebase exploration only
The shorter version. Skips personal questions, just surveys the current working directory, reads key files, and stores findings as memories. Useful when cairo already knows you and you're just pointing it at a new project.

Trigger: `/init codebase` or `skill(action="read", name="init_codebase")`.

---

## Skill shape

A skill is a row in the `skills` table:

```
skills (
    name, description, content, tags
)
```

- **`name`** — short identifier (e.g., `init`, `code_review`, `spike`)
- **`description`** — one-line what-it-does
- **`content`** — the instruction text the model receives as a user turn
- **`tags`** — JSON array of strings for filtering

Content is plain markdown. Template substitution works — `{{ai_name}}`, `{{user_name}}`, any config key.

---

## Invoking a skill

The `/init` CLI command is a special case (it triggers the `init` skill plus a storage follow-up). In general you invoke a skill by reading its content and feeding it back as a user turn:

```
> use the code_review skill on the changes in cmd/cairo/
  [model calls skill(action="read", name="code_review")]
  [skill content appears as the next user turn, executed immediately]
```

Or more concisely, the model itself can dispatch a skill if the situation calls for it — "you should run the code_review skill" is enough.

For a slash-command shortcut (like `/init`), the CLI has a hardcoded list:

```go
case "/init":
    return false, buildInitPrompt(arg, a, database, session)
```

Adding your own slash shortcut requires a code change. For now, the model-dispatched path is the general mechanism.

---

## Writing a skill

```
> write a skill called "spike" that starts a quick exploration — read the
  relevant files, form a hypothesis, propose the smallest experiment that would
  disprove it, and wait for my OK before doing anything.

  [model calls skill(action="create", name="spike", ...)]
```

The stored content might look like:

```markdown
# Spike

Goal: explore a hypothesis quickly and cheaply.

Steps:
1. Read the files the user referenced. If none are named, ask.
2. Form a one-sentence hypothesis: "I think X because Y."
3. Propose the smallest experiment that would disprove the hypothesis.
   Not the full solution — the cheapest test.
4. State the proposal explicitly: "The experiment is: run Z, and if A
   happens, the hypothesis is wrong."
5. Wait for the user's OK before running Z.

Do not implement the full solution in this turn. This is reconnaissance.
```

Next time the user says "spike this idea," the model can call `skill(action="read", name="spike")` and execute the instruction.

---

## Skills vs tools vs prompt parts

Three closely-related concepts that do different things:

| | When loaded | Shape | Best for |
|---|---|---|---|
| **Skill** | On explicit invocation | Full instruction text | Multi-step workflows, named flows |
| **Tool** | Every turn | Function the model can call | Imperative capabilities (read file, add memory) |
| **Prompt part** | Every turn (if trigger matches) | Prose appended to system prompt | Background framing, always-on constraints |

Rule of thumb:
- If it's a named thing the user will ask for by name ("run `/init`," "do a code review"), it's a **skill**.
- If the model calls it repeatedly to do a thing, it's a **tool**.
- If it's a rule that should always be in effect (like "prefer terse responses"), it's a **prompt part**.

---

## Skills in `.cairo` bundles

Skills are part of identity. `cairo export` ships them; `cairo import` installs them. A bundle's skills can differ from yours — see `cairo diff` output.

This is the "skills marketplace" seed — see [ROADMAP](../../ROADMAP.md), far horizon. Today skills travel with full identity bundles; selective skill-only bundles are a plausible future.

---

## Listing and editing

```
> show me the skills I have
  [skill(action="list")]

> read the init_codebase skill
  [skill(action="read", name="init_codebase")]

> update the spike skill — add a step about cleaning up any test files
  [skill(action="update", name="spike", content="...")]

> delete the old one
  [skill(action="delete", name="...")]
```

---

## Known rough edges

- **No slash-command registration from inside cairo.** New skills don't automatically get a `/my_skill` shortcut in the line CLI — that requires editing `internal/cli/cli.go`. The TUI slash drawer registers a fixed command list too; skill invocation goes through natural-language for now.
- **No parameters.** A skill is just text. If you want a parameterized workflow ("spike on topic X") you include the topic in the user turn before the skill call, or you write a tool instead.
- **Template substitution is turn-time.** `{{ai_name}}` in a skill's content is substituted when the skill is dispatched, not when it's stored. That means skill content survives name changes.
