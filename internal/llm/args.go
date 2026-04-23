package llm

import "encoding/json"

// normalizeArgs handles both map[string]any and JSON-string argument forms
// that different Ollama versions emit.
func normalizeArgs(raw any) map[string]any {
	if m, ok := raw.(map[string]any); ok {
		return m
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(b, &m) == nil {
		return m
	}
	// double-encoded string
	var s string
	if json.Unmarshal(b, &s) == nil {
		json.Unmarshal([]byte(s), &m)
	}
	return m
}
