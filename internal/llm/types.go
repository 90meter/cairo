package llm

import "fmt"

// Message is a single turn in a chat conversation.
type Message struct {
	Role      string     `json:"role"` // system | user | assistant | tool
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall is a single tool invocation requested by the model.
type ToolCall struct {
	Function struct {
		Name      string `json:"name"`
		Arguments any    `json:"arguments"` // map[string]any or raw JSON
	} `json:"function"`
}

// Args parses tool call arguments — handles both map and JSON-string forms.
func (tc *ToolCall) Args() map[string]any {
	return normalizeArgs(tc.Function.Arguments)
}

// CallID returns a stable synthetic ID for this tool call.
// Ollama doesn't emit call IDs; we derive one from name+args for DB persistence.
func (tc *ToolCall) CallID(seq int) string {
	return fmt.Sprintf("call_%s_%d", tc.Function.Name, seq)
}

// ToolDef is an Ollama/OpenAI-compatible tool definition.
type ToolDef struct {
	Type     string `json:"type"` // always "function"
	Function struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parameters  any    `json:"parameters"` // JSON Schema object
	} `json:"function"`
}

// ChatOptions controls optional model behaviour.
type ChatOptions struct {
	Think       bool
	ThinkBudget int // max thinking chars before budget-exceeded retry
}
