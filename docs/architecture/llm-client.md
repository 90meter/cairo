# LLM client

`internal/llm` is the boundary between Cairo and Ollama. Two endpoints (`/api/chat`, `/api/embeddings`), one HTTP client, no SDK dependency.

The surface is narrow on purpose. If a future backend needs to be slotted in, these are the four functions that matter: `New`, `Ping`, `StreamOnce`, `Embed`.

---

## The client

`llm.Client` is a thin struct around an `http.Client`:

```go
type Client struct {
    url  string
    http *http.Client
}

func New(url string) *Client {
    if url == "" {
        url = "http://localhost:11434"
    }
    return &Client{
        url:  url,
        http: &http.Client{Timeout: 10 * time.Minute},
    }
}
```

**10-minute timeout** covers long generations without hanging forever. For a local model producing 50 tokens/s, that's roughly 30k tokens of output before the timeout fires — enough for a long reasoning trace.

**`Ping()`** hits `/api/version` on startup. A fast check that Ollama is reachable before the first `StreamOnce` call — produces a cleaner error message than a mid-stream connection failure.

---

## `StreamOnce`

The main function. One HTTP POST to `/api/chat`, streaming response.

```go
func (c *Client) StreamOnce(
    ctx context.Context,
    model string,
    messages []Message,
    tools []ToolDef,
    opts ChatOptions,
    cb ChatCallbacks,
) (text string, toolCalls []ToolCall, budgetExceeded bool, err error)
```

**One call, one response.** Not a loop. If the model emits tool calls, that's what `toolCalls` will contain; the caller (`runLoop` in `internal/agent/loop.go`) decides what to do with them.

**Streaming.** Ollama emits one JSON object per line. `StreamOnce` parses chunks with a 4MB line buffer (large enough for tool calls whose arguments include big file contents), accumulates content bytes, and fires `cb.Content(token)` / `cb.Thinking(token)` for each chunk so UIs can render live.

**Context cancellation.** The context is propagated into `http.NewRequestWithContext`. Cancelling aborts the stream; whatever text accumulated is returned along with `ctx.Err()`. This is what lets Ctrl-C in the TUI produce "partial text + (interrupted)" rather than "turn lost."

**Thinking budget.** `ChatOptions.ThinkBudget` caps reasoning tokens. If exceeded, `StreamOnce` returns `budgetExceeded=true` without an error; the caller can retry without thinking enabled. (The retry logic currently lives in the caller — see `internal/agent/loop.go`.)

---

## Message shape

```go
type Message struct {
    Role      string     `json:"role"`         // system | user | assistant | tool
    Content   string     `json:"content,omitempty"`
    ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type ToolCall struct {
    Function struct {
        Name      string `json:"name"`
        Arguments any    `json:"arguments"`    // map[string]any or raw JSON string
    } `json:"function"`
}

type ToolDef struct {
    Type     string `json:"type"`              // always "function"
    Function struct {
        Name        string `json:"name"`
        Description string `json:"description"`
        Parameters  any    `json:"parameters"` // JSON Schema
    } `json:"function"`
}
```

This shape is **Ollama/OpenAI-compatible**. It works with Ollama directly; it would likely work with other providers with minor endpoint/path adjustments. The format is intentionally the common-denominator.

---

## Tool call argument normalization

Models are inconsistent about whether they emit `arguments` as a JSON object or a JSON string containing an object. `ToolCall.Args()` handles both:

```go
func (tc *ToolCall) Args() map[string]any {
    return normalizeArgs(tc.Function.Arguments)
}
```

The normalize helper inspects the runtime type: if it's already a `map[string]any` it returns it directly; if it's a `string` it unmarshals. This is cheap insurance against a model that learned the wrong convention.

---

## Synthetic call IDs

Ollama doesn't emit stable tool-call IDs the way OpenAI does. Cairo synthesizes them:

```go
func (tc *ToolCall) CallID(seq int) string {
    return fmt.Sprintf("call_%s_%d", tc.Function.Name, seq)
}
```

The sequence number is maintained by `runLoop` across the turn. IDs are stable within a turn (so tool requests can be correlated with tool results in `messages`), but re-synthesized on resume. This is a known imperfection — real IDs would be more robust — documented in [Agent loop](agent-loop.md#known-rough-edges).

---

## Embeddings

`internal/llm/embed.go` handles `POST /api/embeddings`:

```go
func (c *Client) Embed(model, text string) ([]float32, error)
```

One request, one vector. Used by the memory tool on add, the memory/summary search tools on query, and the summarizer on write.

No streaming — embeddings are small and fast. No caching — the call surface is simple and the caller owns whether to repeat work.

---

## Error handling philosophy

The client's job is to:

1. Round-trip the HTTP call cleanly.
2. Distinguish connection errors, HTTP errors, and context cancellation.
3. Return partial progress on cancel rather than discarding it.

What it does *not* do:

- Retry failed requests (caller decides)
- Tolerate model-specific JSON quirks (normalization helpers like `Args()` exist, but the client doesn't paper over every malformed response)
- Log anything (silent — callers subscribe to the event bus for visibility)

---

## What's required from the server

To target a non-Ollama backend, the minimum needed:

- A streaming chat endpoint that accepts `{model, messages, stream: true, tools}` and emits one JSON object per line with `message.content`, optional `message.thinking`, optional `message.tool_calls`, and a `done: true` terminator.
- An embeddings endpoint that accepts `{model, prompt}` and returns `{embedding: []float}`.

The llama.cpp server, LM Studio, and several commercial APIs already match or come close. Adapters would be small. This is not a current priority (see [ROADMAP](../../ROADMAP.md) — far horizon) but the surface is deliberately narrow enough to allow it.

---

## Known rough edges

- **Synthetic call IDs.** Documented above. Real IDs (from a backend that emits them) would improve resume fidelity.
- **No explicit retry on transient network errors.** A blip hitting Ollama fails the turn. In practice Ollama is reliable enough on localhost that retries haven't been worth the added complexity.
- **Budget-exceeded retry is caller's problem.** When `budgetExceeded=true` is returned, whatever tokens had already streamed to the UI stayed on screen. The retry starts a fresh generation, and the tokens visually concatenate. The right fix is callback-side — know you're in a retry — but it hasn't been written.
