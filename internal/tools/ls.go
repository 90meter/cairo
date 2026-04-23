package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/scotmcc/cairo/internal/agent"
)

type lsTool struct{}

func Ls() agent.Tool { return lsTool{} }

func (lsTool) Name() string        { return "ls" }
func (lsTool) Description() string { return "List files and directories." }
func (lsTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":  prop("string", "Directory to list (default: working directory)"),
			"limit": prop("integer", "Maximum entries (default 200)"),
		},
	}
}

func (lsTool) Execute(args map[string]any, ctx *agent.ToolContext) agent.ToolResult {
	path := strArg(args, "path")
	if path == "" {
		path = ctx.WorkDir
	} else {
		path = resolvePath(path, ctx.WorkDir)
	}
	limit := intArg(args, "limit", 200)

	entries, err := os.ReadDir(path)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}

	var b strings.Builder
	count := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		b.WriteString(name)
		b.WriteByte('\n')
		count++
		if count >= limit {
			fmt.Fprintf(&b, "[truncated — %d more entries]", len(entries)-count)
			break
		}
	}

	abs, _ := filepath.Abs(path)
	return agent.ToolResult{Content: fmt.Sprintf("%s\n%s", abs, b.String())}
}
