package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// ChatCallbacks receives streaming events. ToolCall is removed — tool execution
// is now the caller's responsibility (runLoop handles the tool loop directly).
type ChatCallbacks struct {
	Thinking func(token string)
	Content  func(token string)
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
	Think    bool      `json:"think"`
	Tools    []ToolDef `json:"tools,omitempty"`
}

type chatChunk struct {
	Message struct {
		Role      string     `json:"role"`
		Content   string     `json:"content"`
		Thinking  string     `json:"thinking"`
		ToolCalls []ToolCall `json:"tool_calls"`
	} `json:"message"`
	Done bool `json:"done"`
}

// StreamOnce makes a single LLM request and streams the response. Returns
// the accumulated text and any tool calls the model requested.
//
// The ctx is propagated into the HTTP request — cancelling it aborts the
// in-flight stream. On cancellation we return whatever text was accumulated
// up to that point along with ctx.Err(), so callers can persist the partial
// response rather than discarding it. budgetExceeded is reported separately
// so the caller can retry without thinking enabled.
func (c *Client) StreamOnce(ctx context.Context, model string, messages []Message, tools []ToolDef, opts ChatOptions, cb ChatCallbacks) (text string, toolCalls []ToolCall, budgetExceeded bool, err error) {
	body, err := json.Marshal(chatRequest{
		Model:    model,
		Messages: messages,
		Stream:   true,
		Think:    opts.Think,
		Tools:    tools,
	})
	if err != nil {
		return "", nil, false, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", nil, false, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		// Distinguish cancellation from other network errors so the caller
		// can handle it differently (persist partial state, emit a signal).
		if ctx.Err() != nil {
			return "", nil, false, ctx.Err()
		}
		return "", nil, false, fmt.Errorf("ollama: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var e struct{ Error string `json:"error"` }
		json.NewDecoder(resp.Body).Decode(&e)
		return "", nil, false, fmt.Errorf("ollama %d: %s", resp.StatusCode, e.Error)
	}

	var textBuf []byte
	thinkChars := 0

	// 4MB line buffer — handles large tool_call payloads (write tool with big files)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 4<<20), 4<<20)

	for scanner.Scan() {
		// Cheap check between chunks — if the user cancelled, stop reading
		// and return what we've got. The HTTP context will also have
		// poisoned resp.Body, which would surface the same error via
		// scanner.Err() on the next iteration.
		if ctx.Err() != nil {
			return string(textBuf), toolCalls, false, ctx.Err()
		}

		var chunk chatChunk
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			continue
		}

		if len(chunk.Message.ToolCalls) > 0 {
			toolCalls = append(toolCalls, chunk.Message.ToolCalls...)
		}

		if chunk.Message.Thinking != "" {
			thinkChars += len(chunk.Message.Thinking)
			if opts.ThinkBudget > 0 && thinkChars > opts.ThinkBudget {
				return "", nil, true, nil
			}
			if cb.Thinking != nil {
				cb.Thinking(chunk.Message.Thinking)
			}
		} else if chunk.Message.Content != "" {
			textBuf = append(textBuf, chunk.Message.Content...)
			if cb.Content != nil {
				cb.Content(chunk.Message.Content)
			}
		}

		if chunk.Done {
			return string(textBuf), toolCalls, false, nil
		}
	}
	if err := scanner.Err(); err != nil {
		// Bubble ctx cancellation as such rather than a generic read error.
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			return string(textBuf), toolCalls, false, ctx.Err()
		}
		return string(textBuf), toolCalls, false, err
	}
	return string(textBuf), toolCalls, false, nil
}
