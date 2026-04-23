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
