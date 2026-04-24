package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/scotmcc/cairo/internal/agent"
	"github.com/scotmcc/cairo/internal/db"
)

type searchTool struct {
	db *db.DB
}

func Search(database *db.DB) agent.Tool { return searchTool{db: database} }

func (searchTool) Name() string        { return "search" }
func (searchTool) Description() string { return "Search the web via a SearXNG instance. Returns titles, URLs, and snippets." }
func (searchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": prop("string", "The search query"),
			"limit": prop("integer", "Number of results to return (default 10, max 20)"),
		},
		"required": []string{"query"},
	}
}

func (t searchTool) Execute(args map[string]any, ctx *agent.ToolContext) agent.ToolResult {
	query := strArg(args, "query")
	if query == "" {
		return agent.ToolResult{Content: "error: query is required", IsError: true}
	}

	limit := intArg(args, "limit", 10)
	if limit < 1 {
		limit = 10
	}
	if limit > 20 {
		limit = 20
	}

	baseURL, err := t.db.Config.Get("searxng_url")
	if err != nil || baseURL == "" {
		return agent.ToolResult{
			Content: "searxng_url not configured — run: config set searxng_url http://your-searxng-host",
			IsError: true,
		}
	}

	searchURL := fmt.Sprintf("%s/search?q=%s&format=json&categories=general",
		strings.TrimRight(baseURL, "/"),
		url.QueryEscape(query),
	)

	httpCtx, cancel := context.WithTimeout(ctx.Ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(httpCtx, http.MethodGet, searchURL, nil)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error building request: %v", err), IsError: true}
	}
	req.Header.Set("User-Agent", "cairo/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error fetching search results: %v", err), IsError: true}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return agent.ToolResult{
			Content: fmt.Sprintf("search request failed: HTTP %d", resp.StatusCode),
			IsError: true,
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error reading response: %v", err), IsError: true}
	}

	var parsed struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error parsing response: %v", err), IsError: true}
	}

	if len(parsed.Results) == 0 {
		return agent.ToolResult{Content: "No results found."}
	}

	results := parsed.Results
	if len(results) > limit {
		results = results[:limit]
	}

	var sb strings.Builder
	for i, r := range results {
		fmt.Fprintf(&sb, "%d. %s\n   %s\n   %s\n\n", i+1, r.Title, r.URL, r.Content)
	}

	out := sb.String()
	const maxBytes = 8000
	if len(out) > maxBytes {
		out = out[:maxBytes] + "\n[truncated]"
	}

	return agent.ToolResult{Content: out}
}
