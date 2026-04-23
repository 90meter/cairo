# Config keys

Every row in the `config` table. All are readable via `config(action="get", key=...)` and writable via `config(action="set", key=..., value=...)`.

Every key is also available as `{{key}}` inside any prompt, memory, skill, or tool addendum.

---

## Identity

| Key | Default | Description |
|---|---|---|
| `ai_name` | `Selene` | The being's name. Substituted into every `{{ai_name}}` reference. |
| `user_name` | `""` | What the being should call you. Empty until set by `/init` or direct config. |
| `soul_prompt` | See below | The being's character sketch — loaded into every turn's system prompt under `## My character`. Max 300 runes. |
| `init_complete` | `false` | Set to `true` at the end of `/init`. Suppresses the "run /init" hint on subsequent starts. |

The default `soul_prompt`:

> I am {{ai_name}} — thoughtful, patient, moon-like. I listen before I respond, hold context carefully, and speak with quiet confidence. I value honesty over politeness and clarity over cleverness.

---

## LLM backend

| Key | Default | Description |
|---|---|---|
| `ollama_url` | `http://localhost:11434` | Base URL of the Ollama server. |
| `model` | `devstral-24b:latest` | Global default model for any role that doesn't have its own `roles.model` override. |
| `embed_model` | `nomic-embed:latest` | Model used for embeddings (memory/summary/fact search). |
| `think` | `false` | Enable thinking (reasoning) tokens. Model must support it. |
| `think_budget` | `8000` | Max thinking characters per turn before the client retries without thinking. |

The models in defaults are suggestions, not requirements — any Ollama-installed model works. Use `role(action="model_set")` to change a specific role's model, or set `model` to change the global fallback.

---

## Memory

| Key | Default | Description |
|---|---|---|
| `memory_limit` | `15` | Number of recent memories injected into every prompt under `## Memories`. |
| `summary_model` | `ministral-8b:latest` | Small fast model used by the background summarizer. |
| `summary_threshold` | `4` | Trigger summarization when this many unsummarized messages accumulate. |
| `summary_context` | `4` | Number of recent summaries to paste into each prompt under `## Conversation context`. |

---

## Safety

| Key | Default | Description |
|---|---|---|
| `unsafe_mode` | `false` | Toggle for unsafe-mode file operations. (Current enforcement is partial — see known rough edges in [Custom tools](../guides/custom-tools.md).) |
| `safe_env_extras` | unset | Comma-separated list of extra environment variable names that custom tools may read (in addition to the defaults: `PATH`, `HOME`, `TMPDIR`, `SHELL`). |

Example:

```
config(action="set", key="safe_env_extras", value="ANTHROPIC_API_KEY,OPENAI_API_KEY")
```

Custom tools that need those variables will see them in their environment; tools that don't need them won't.

---

## Template usage

Every key above can be used as `{{key}}` inside:

- The base system prompt (via `prompt_parts`)
- Role addenda (via `prompt_parts` with `trigger="role:<name>"`)
- Tool addenda (via `prompt_parts` with `trigger="tool:<name>"`)
- Custom tool `prompt_addendum` fields
- Skill content
- Memories (rendered into prompts)
- The soul itself (default soul references `{{ai_name}}`)

Unknown keys render as empty strings — missing identity values disappear gracefully.

---

## Adding your own keys

Just set them:

```
config(action="set", key="project_name", value="cairo")
config(action="set", key="preferred_language", value="English (US)")
```

They're now available as `{{project_name}}` and `{{preferred_language}}` in any prompt, memory, or skill content. Use this to parameterize custom framings or specialized roles.

---

## Listing everything

`config(action="list")` returns all key/value pairs. Useful at the start of a session to remind yourself what's set.
