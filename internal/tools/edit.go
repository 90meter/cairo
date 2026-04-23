package tools

import (
	"fmt"
	"os"
	"strings"

	"github.com/scotmcc/cairo/internal/agent"
)

type editTool struct{}

func Edit() agent.Tool { return editTool{} }

func (editTool) Name() string        { return "edit" }
func (editTool) Description() string { return "Replace exact text in a file. The old_text must match exactly (including whitespace)." }
func (editTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":     prop("string", "Path to the file"),
			"old_text": prop("string", "Exact text to replace"),
			"new_text": prop("string", "Replacement text"),
		},
		"required": []string{"path", "old_text", "new_text"},
	}
}

func (editTool) Execute(args map[string]any, ctx *agent.ToolContext) agent.ToolResult {
	path := resolvePath(strArg(args, "path"), ctx.WorkDir)
	oldText := strArg(args, "old_text")
	newText := strArg(args, "new_text")

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

	data, err := os.ReadFile(path)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error reading file: %v", err), IsError: true}
	}

	content := string(data)
	count := strings.Count(content, oldText)
	if count == 0 {
		return agent.ToolResult{Content: "error: old_text not found in file", IsError: true}
	}
	if count > 1 {
		return agent.ToolResult{
			Content: fmt.Sprintf("error: old_text matches %d locations — be more specific", count),
			IsError: true,
		}
	}

	updated := strings.Replace(content, oldText, newText, 1)
	if err := os.WriteFile(path, []byte(updated), 0644); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error writing file: %v", err), IsError: true}
	}
	return agent.ToolResult{Content: fmt.Sprintf("edited %s", path)}
}
