package llm

import (
	"encoding/json"
	"fmt"
)

type embedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type embedResponse struct {
	Embedding []float32 `json:"embedding"`
}

// Embed returns a vector embedding for the given text.
// Returns nil (no error) if the embed model is not available — callers
// should treat nil embeddings as "no semantic search for this entry."
func (c *Client) Embed(model, text string) ([]float32, error) {
	resp, err := c.post("/api/embeddings", embedRequest{Model: model, Prompt: text})
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, nil // model not available, degrade gracefully
	}

	var r embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("embed decode: %w", err)
	}
	return r.Embedding, nil
}
