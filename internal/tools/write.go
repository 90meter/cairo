package tools

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/scotmcc/cairo/internal/agent"
)

type writeTool struct{}

func Write() agent.Tool { return writeTool{} }

func (writeTool) Name() string        { return "write" }
func (writeTool) Description() string { return "Write content to a file, creating it and any parent directories if needed." }
func (writeTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    prop("string", "Absolute or relative path to the file"),
			"content": prop("string", "Full content to write"),
		},
		"required": []string{"path", "content"},
	}
}

func (writeTool) Execute(args map[string]any, ctx *agent.ToolContext) agent.ToolResult {
	path := resolvePath(strArg(args, "path"), ctx.WorkDir)
	content := strArg(args, "content")

	// Check unsafe_mode
	unsafeMode := "false"
	if val, err := ctx.DB.Config.Get("unsafe_mode"); err == nil && val != "" {
		unsafeMode = val
	}
	if unsafeMode == "false" {
		if err := requireUnderCWD(path, ctx.WorkDir); err != nil {
			return agent.ToolResult{Content: err.Error(), IsError: true}
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error creating directories: %v", err), IsError: true}
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error writing file: %v", err), IsError: true}
	}
	return agent.ToolResult{Content: fmt.Sprintf("wrote %d bytes to %s", len(content), path)}
}
