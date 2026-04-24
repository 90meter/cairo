package agent

import (
	"encoding/json"

	"github.com/scotmcc/cairo/internal/llm"
)

func (a *Agent) loadHistory() error {
	// Only load unsummarized messages as active context.
	// Summarized messages are represented in the context via summary blocks
	// injected into the system prompt by BuildSystemPrompt.
	msgs, err := a.db.Messages.UnsummarizedForSession(a.session.ID)
	if err != nil {
		return err
	}
	a.history = make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case "assistant":
			if m.ToolCalls != "" {
				// Reconstruct assistant tool-call request messages from persisted JSON.
				toolCalls, err := unmarshalToolCalls(m.ToolCalls)
				if err == nil && len(toolCalls) > 0 {
					a.history = append(a.history, llm.Message{Role: "assistant", ToolCalls: toolCalls})
					continue
				}
			}
			if m.Content != "" {
				a.history = append(a.history, llm.Message{Role: "assistant", Content: m.Content})
			}
			// skip empty-content assistant rows with no tool calls (shouldn't happen, but defensive)
		case "tool":
			a.history = append(a.history, llm.Message{Role: "tool", Content: m.Content})
		default:
			// user, system
			a.history = append(a.history, llm.Message{Role: m.Role, Content: m.Content})
		}
	}

	// Detect incomplete turns: if the process crashed mid-turn, the DB may
	// contain an assistant tool-call row followed by fewer tool-result rows
	// than there are tool calls. This produces an invalid message sequence
	// for the LLM (mismatched call/result counts). Strip the incomplete turn
	// and inject a system note so the resumed session starts from a clean state.
	a.history = repairIncompleteTurn(a.history)

	return nil
}

// repairIncompleteTurn scans the tail of history for a partially-executed
// tool-call turn and strips it if found. A turn is incomplete when the last
// assistant message has N tool calls but is followed by fewer than N tool
// results. In that case the incomplete turn is removed and a system note is
// appended so the LLM knows the previous session was interrupted.
func repairIncompleteTurn(history []llm.Message) []llm.Message {
	n := len(history)
	if n == 0 {
		return history
	}

	// Count trailing tool-result messages.
	trailingTools := 0
	for i := n - 1; i >= 0; i-- {
		if history[i].Role == "tool" {
			trailingTools++
		} else {
			break
		}
	}

	// The message immediately before the trailing tool results must be an
	// assistant message with tool calls for there to be anything to repair.
	assistantIdx := n - 1 - trailingTools
	if assistantIdx < 0 {
		return history
	}
	asst := history[assistantIdx]
	if asst.Role != "assistant" || len(asst.ToolCalls) == 0 {
		return history
	}

	// If all tool calls have corresponding results, the turn is complete.
	if trailingTools >= len(asst.ToolCalls) {
		return history
	}

	// Incomplete: strip the assistant tool-call row and any partial results,
	// then append a system note so the LLM resumes with clean context.
	repaired := history[:assistantIdx]
	repaired = append(repaired, llm.Message{
		Role:    "system",
		Content: "[system] Note: the previous session was interrupted mid-turn. The last tool call sequence did not complete. Please acknowledge and ask how to proceed.",
	})
	return repaired
}

// persistMessage is called by runLoop for every message produced during a turn.
// In-memory history is updated here too, keeping it in sync with what's sent to the LLM.
func (a *Agent) persistMessage(role, content, toolCallsJSON, toolName, toolID string) {
	a.db.Messages.Add(a.session.ID, role, content, toolCallsJSON, toolName, toolID)
	switch role {
	case "assistant":
		if toolCallsJSON != "" {
			if toolCalls, err := unmarshalToolCalls(toolCallsJSON); err == nil && len(toolCalls) > 0 {
				a.history = append(a.history, llm.Message{Role: "assistant", ToolCalls: toolCalls})
				return
			}
		}
		if content != "" {
			a.history = append(a.history, llm.Message{Role: "assistant", Content: content})
		}
	case "tool":
		a.history = append(a.history, llm.Message{Role: "tool", Content: content})
	}
}

// unmarshalToolCalls reconstructs llm.ToolCall slice from the JSON stored in the DB.
// The stored format is [{id, name, args}] — we map back to llm.ToolCall's shape.
func unmarshalToolCalls(raw string) ([]llm.ToolCall, error) {
	var stored []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Args any    `json:"args"`
	}
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return nil, err
	}
	out := make([]llm.ToolCall, len(stored))
	for i, s := range stored {
		out[i].Function.Name = s.Name
		out[i].Function.Arguments = s.Args
	}
	return out, nil
}
