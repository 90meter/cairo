# Identity

The being's identity is assembled fresh at the start of every turn. Nothing is baked into the binary. A small number of config rows, prompt parts, and memory rows compose into a single system prompt — and changing any of them changes the being's next response.

---

## The default being: Selene

On first run Cairo seeds an identity:

- `ai_name = "Selene"` — the name the being answers to
- `soul_prompt = "I am {{ai_name}} — thoughtful, patient, moon-like. I listen before I respond, hold context carefully, and speak with quiet confidence. I value honesty over politeness and clarity over cleverness."`
- `user_name = ""` — empty until you tell the being who you are (usually via `/init`)

Selene is a default, not a fixture. Every one of these is a row in the `config` table. To build a different default being, change the rows; to preserve yours across machines, export the DB (`cairo export`).

You can rename the being at any time:

```
cairo
> use config to set ai_name to "Kai"
```

The model will call `config(action="set", key="ai_name", value="Kai")`, and from the next turn on every mention of `{{ai_name}}` throughout the prompt substitutes the new name.

---

## Prompt composition order

At the start of every turn, `BuildSystemPrompt` (`internal/agent/prompt.go`) assembles the system prompt in this order:

1. **Base prompt parts** — rows in `prompt_parts` with `trigger IS NULL`, ordered by `load_order`. These are the always-on instructions.
2. **Soul** — the current `soul_prompt` config value, rendered under a `## My character` header.
3. **Role addendum** — rows in `prompt_parts` with `trigger = "role:<current_role>"`. Mode-specific framing.
4. **Tool addenda** — rows with `trigger = "tool:<tool_name>"` for every tool the current role has access to.
5. **Custom-tool addenda** — the `prompt_addendum` field of every enabled row in `custom_tools`.
6. **Conversation context** — the most recent `summary_context` rows from the `summaries` table (default 4), under `## Conversation context`.
7. **Memories** — up to `memory_limit` rows from `memories` (default 15), newest first, under `## Memories`. Overflow count is shown so the being knows to search when it needs more.
8. **Stamp** — current date and working directory.
9. **Template substitution** — `{{key}}` tokens anywhere in the assembled prompt are replaced with the matching `config` value.

The whole thing is assembled from scratch on every turn. That means a soul edit, a new memory, a new tool — none of them need a restart. The next turn picks them up.

---

## Template substitution

Every row in `config` is available as a `{{key}}` template variable inside prompts, soul, memories, skills, and tool addenda. The common ones:

- `{{ai_name}}` — the name the being answers to
- `{{user_name}}` — the name the being calls the user
- Any other config key you've set: `{{project_name}}`, `{{preferred_language}}`, etc.

Unknown or empty keys render as empty strings, not as `{{...}}` literals. Missing identity values disappear gracefully rather than leaking "I am {{ai_name}}" into a response.

This is why `/init` is a conversation and not a wizard. The being asks your name, stores it as `user_name`, and from the next turn on it addresses you by name in every prompt that references `{{user_name}}`.

---

## The soul

The soul is a single row in `config` keyed `soul_prompt`. It's the character sketch the being carries into every turn. Short by design — 300 characters in the default. Long enough to have a voice, short enough to stay out of the way.

The soul is self-writable. The `soul` tool (`internal/tools/soul.go`) has two actions, `get` and `set`. The being can reshape its own character in response to the user's feedback. That sounds risky — in practice the being is conservative because the soul is loaded into its own next prompt; careless edits produce turns the being itself finds jarring.

---

## Roles as overlays

A role is not a separate identity. It's an overlay on top of the same being.

Every session has a `role` field. At prompt-composition time, the base + soul are loaded unchanged; then the `role:<name>` prompt part is appended. That part is usually a short paragraph — "You are operating as a coder — one of {{ai_name}}'s focused attention modes. Your job is to implement..." — enough to tilt the turn without replacing the underlying voice.

See [Roles](roles.md) for the five built-in roles and how to add your own.

---

## What *isn't* identity

To be clear about the boundary:

- **Conversation history** is not identity. Messages are scoped to a session; they don't persist into the self. What the being remembers from a conversation becomes a memory, summary, or fact — those are identity.
- **The working directory** is not identity. Cairo stamps the cwd into every prompt as context, but it's just orientation.
- **Hardware, machine, OS** are all irrelevant. A `.cairo` bundle on a new machine is the same being.

Identity is what survives the process terminating.
