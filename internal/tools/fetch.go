package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"

	"github.com/scotmcc/cairo/internal/agent"
)

type fetchTool struct{}

func Fetch() agent.Tool { return fetchTool{} }

func (fetchTool) Name() string        { return "fetch" }
func (fetchTool) Description() string { return "Fetch a web page and return its content as clean markdown." }
func (fetchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url":        prop("string", "The URL to fetch"),
			"max_length": prop("integer", "Character cap on returned content (default 8000, max 32000)"),
		},
		"required": []string{"url"},
	}
}

func (fetchTool) Execute(args map[string]any, ctx *agent.ToolContext) agent.ToolResult {
	rawURL := strArg(args, "url")
	if rawURL == "" {
		return agent.ToolResult{Content: "error: url is required", IsError: true}
	}

	maxLength := intArg(args, "max_length", 8000)
	if maxLength < 1 {
		maxLength = 8000
	}
	if maxLength > 32000 {
		maxLength = 32000
	}

	httpCtx, cancel := context.WithTimeout(ctx.Ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(httpCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error building request: %v", err), IsError: true}
	}
	req.Header.Set("User-Agent", "cairo/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error fetching URL: %v", err), IsError: true}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return agent.ToolResult{
			Content: fmt.Sprintf("fetch failed: HTTP %d", resp.StatusCode),
			IsError: true,
		}
	}

	htmlBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error reading response body: %v", err), IsError: true}
	}

	markdown, err := htmltomarkdown.ConvertString(string(htmlBytes))
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error converting HTML to markdown: %v", err), IsError: true}
	}

	if len(markdown) > maxLength {
		markdown = markdown[:maxLength] + "\n[truncated]"
	}

	return agent.ToolResult{Content: markdown}
}
