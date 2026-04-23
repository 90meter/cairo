package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/scotmcc/cairo/internal/agent"
)

const (
	defaultMaxLines = 500
	maxBytes        = 1 << 20 // 1 MB
)

type readTool struct{}

func Read() agent.Tool { return readTool{} }

func (readTool) Name() string        { return "read" }
func (readTool) Description() string { return "Read a file from the filesystem. Large files are truncated." }
func (readTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":   prop("string", "Absolute or relative path to the file"),
			"offset": prop("integer", "Line number to start reading from (1-based, optional)"),
			"limit":  prop("integer", "Maximum number of lines to return (default 500)"),
		},
		"required": []string{"path"},
	}
}

func (readTool) Execute(args map[string]any, ctx *agent.ToolContext) agent.ToolResult {
	path := resolvePath(strArg(args, "path"), ctx.WorkDir)
	offset := intArg(args, "offset", 1)
	limit := intArg(args, "limit", defaultMaxLines)
	if offset < 1 {
		offset = 1
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	if len(data) > maxBytes {
		data = data[:maxBytes]
	}

	lines := strings.Split(string(data), "\n")
	start := offset - 1
	if start >= len(lines) {
		start = len(lines) - 1
	}
	end := start + limit
	if end > len(lines) {
		end = len(lines)
	}

	var b strings.Builder
	for i, line := range lines[start:end] {
		fmt.Fprintf(&b, "%d\t%s\n", start+i+1, line)
	}
	if end < len(lines) {
		fmt.Fprintf(&b, "\n[truncated — %d lines total; use offset/limit to read more]", len(lines))
	}

	return agent.ToolResult{Content: b.String()}
}

func resolvePath(path, workDir string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(workDir, path)
}
