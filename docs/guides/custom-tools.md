# Custom tools

The being can write its own tools at runtime. A custom tool is a bash script or Python snippet stored in the DB that appears to the model like any built-in tool — same interface, same JSON-schema parameters, same result shape.

This is the most powerful (and riskiest) extension point in Cairo. It's how the being grows capability without needing a Go recompile.

---

## Lifecycle

```
1. Model calls custom_tool(action="create", ...) to register a new tool
   └─ writes a row to custom_tools
2. On the NEXT turn, the tool appears in the registry
   └─ LoadCustom() reads custom_tools.enabled=1 rows at agent startup
3. Model calls the new tool like any other
   └─ args flow in, stdout comes back
4. Optional: custom_tool(action="delete", name=...) removes it
```

The "next turn" part matters: a tool created mid-turn isn't yet loaded into the current tool set. The model sees it the turn after.

---

## What a custom tool looks like

When the model calls `custom_tool(action="create", ...)`:

```json
{
  "name": "wc_lines",
  "description": "Count lines in a file.",
  "parameters": {
    "type": "object",
    "properties": {
      "path": { "type": "string" }
    },
    "required": ["path"]
  },
  "implementation": "wc -l \"$CAIRO_ARG_PATH\"",
  "impl_type": "bash",
  "prompt_addendum": "When asked to count lines, use wc_lines — it's cheaper than reading the file."
}
```

On the next turn, the model sees `wc_lines` in its tool list and can call it with `wc_lines(path="/some/file")`.

---

## The execution environment

When a custom tool runs, it gets a **restricted environment**:

Always present:
- `PATH` — inherited from the cairo process
- `HOME`
- `TMPDIR`
- `SHELL`
- `CAIRO_ARG_<UPPERCASE_KEY>=<value>` — one per argument the model passed
- `CAIRO_ARGS=<json>` — all args as a single JSON string

Optionally present (if set via `safe_env_extras`):
- Any extra vars listed comma-separated in `config.safe_env_extras`

Not present: everything else in the cairo process environment. The parent env is *not* inherited wholesale.

This is the security boundary. A custom tool written by the AI can't exfiltrate your shell's `AWS_SECRET_ACCESS_KEY` unless you explicitly added that name to `safe_env_extras`.

---

## Receiving arguments

Three ways to read arguments in your script:

**Named env vars** — `CAIRO_ARG_<UPPERCASE_KEY>`:

```bash
echo "Got path: $CAIRO_ARG_PATH"
```

**JSON bundle** — `CAIRO_ARGS` carries all args:

```bash
echo "$CAIRO_ARGS" | jq -r '.path'
```

For Python tools (`impl_type: "python"`), same env:

```python
import json, os
args = json.loads(os.environ["CAIRO_ARGS"])
path = args["path"]
```

---

## Returning a result

Whatever the tool writes to stdout (or stderr — they're merged) becomes the tool result string seen by the model. Exit code matters:

- **Exit 0** → `ToolResult.IsError = false`
- **Non-zero exit** → `ToolResult.IsError = true` (but the output is still included)

So:

```bash
# A tool that might fail
if [ ! -f "$CAIRO_ARG_PATH" ]; then
  echo "file not found: $CAIRO_ARG_PATH"
  exit 1
fi
wc -l "$CAIRO_ARG_PATH"
```

The model sees the error message and the IsError signal.

---

## Timeout

Custom tools run under a **60-second deadline**. No override. If your tool needs longer, that's probably a job for `bash` (with its explicit 120s max) or a background task (no timeout — see [Background work](background-work.md)).

Hitting the deadline is treated as an error and the tool result appends `[timed out]`.

---

## `prompt_addendum`

Optional field. Text appended to the system prompt on every turn while the tool is enabled. Use it to teach the model when to reach for this tool vs. alternatives.

Example: a custom `git_status` tool might have:

```
When the user mentions uncommitted changes or the state of the tree, prefer
git_status over `bash("git status")` — it returns a structured summary instead
of raw output.
```

The addendum is loaded into the prompt composition just like a built-in tool's addendum would be. See [Identity](../concepts/identity.md) for the composition order.

---

## `impl_type`: bash vs python

Two supported types:

- **`bash`** (default) — `bash -c "<implementation>"`
- **`python`** — `python3 -c "<implementation>"`

Both run via `exec.CommandContext`. Python is available if `python3` is on PATH; Cairo doesn't bundle an interpreter.

---

## When to prefer custom tools over just `bash`

- **When the model will reach for it often.** A custom tool with a name shows up in the tool list and the model can learn to use it. A `bash` incantation is rediscovered each time.
- **When you want a `prompt_addendum` to teach usage.** Can't attach prose to a bash invocation.
- **When typed parameters matter.** A custom tool with a JSON schema rejects bad args before execution; bash just runs whatever you give it.
- **When the task is reusable across sessions.** Tools persist in the DB; they survive restarts and move with `cairo export`.

When to *not* prefer a custom tool: one-off work, exploratory scripting, anything that doesn't want the ceremony.

---

## Managing custom tools

**List them.** `custom_tool(action="list")` returns name, description, enabled state for every custom tool.

**Delete one.** `custom_tool(action="delete", name=...)` removes the row. The tool disappears from the next turn's registry.

There's no `toggle` or `disable` action today — delete is the only soft removal. The `enabled` column exists in the schema for future use; tools today only create rows with `enabled=1`.

---

## Safety notes

**The model writes these tools.** That means the model is responsible for what it executes on your machine. Cairo runs custom tools the same way you'd run any shell script — there's no sandbox, no seccomp profile, no chroot. The restricted environment is the only mitigation.

In practice this is acceptable because:

1. Cairo is **single-user, local-first**. You're the only one whose identity is asking the model to write tools.
2. The environment is **whitelisted, not inherited**. Secrets in your shell env don't leak unless you explicitly allow them via `safe_env_extras`.
3. Every tool is visible — `custom_tool(action="list")` and `sqlite3` both show what's registered.

It is **not** acceptable if you're:

- Running Cairo on a shared machine where the model could read other users' files
- Storing machine-identity credentials in `HOME` (some people do; check `~/.aws/credentials`, `~/.ssh/`, `~/.netrc`)
- Treating `safe_env_extras` casually — "sure, let the tool have `AWS_SECRET_ACCESS_KEY`" is a real commitment

Treat custom tools with the same skepticism you'd treat any shell script the AI proposed.

---

## Known rough edges

- **No way to edit a custom tool** — only create/delete. To change an implementation, delete and recreate. (A `custom_tool(action="update", ...)` is plausible future work.)
- **Tools appear in the registry only at agent startup / session load.** Mid-session creation requires a new session or at minimum a `loadHistory`-equivalent reload path.
- **Enable/disable is not exposed** — the column exists but no action toggles it. Delete is the only off switch today.
