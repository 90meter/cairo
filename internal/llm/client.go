package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const DefaultURL = "http://localhost:11434"

type Client struct {
	url  string
	http *http.Client
}

func New(url string) *Client {
	if url == "" {
		url = DefaultURL
	}
	return &Client{
		url: url,
		http: &http.Client{
			Timeout: 10 * time.Minute, // generous for long generations; prevents forever-hangs
		},
	}
}

func (c *Client) Ping() error {
	resp, err := c.http.Get(c.url + "/api/version")
	if err != nil {
		return fmt.Errorf("ollama unreachable at %s: %w", c.url, err)
	}
	resp.Body.Close()
	return nil
}

func (c *Client) post(path string, body any) (*http.Response, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return c.http.Post(c.url+path, "application/json", bytes.NewReader(b))
}

// ListModels returns the names of models currently installed on the Ollama
// server (the equivalent of `ollama list`).
func (c *Client) ListModels() ([]string, error) {
	resp, err := c.http.Get(c.url + "/api/tags")
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	defer resp.Body.Close()

	var body struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode models: %w", err)
	}
	names := make([]string, 0, len(body.Models))
	for _, m := range body.Models {
		names = append(names, m.Name)
	}
	return names, nil
}
