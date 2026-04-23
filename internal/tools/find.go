package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/scotmcc/cairo/internal/agent"
)

type findTool struct{}

func Find() agent.Tool { return findTool{} }

func (findTool) Name() string        { return "find" }
func (findTool) Description() string { return "Find files matching a glob pattern." }
func (findTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": prop("string", "Glob pattern, e.g. '**/*.go'"),
			"path":    prop("string", "Root directory to search (default: working directory)"),
			"limit":   prop("integer", "Maximum results (default 100)"),
		},
		"required": []string{"pattern"},
	}
}

func (findTool) Execute(args map[string]any, ctx *agent.ToolContext) agent.ToolResult {
	pattern := strArg(args, "pattern")
	root := strArg(args, "path")
	if root == "" {
		root = ctx.WorkDir
	} else {
		root = resolvePath(root, ctx.WorkDir)
	}
	limit := intArg(args, "limit", 100)

	var matches []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		matched, err := filepath.Match(pattern, filepath.Base(path))
		if err != nil {
			return err
		}
		if matched {
			rel, _ := filepath.Rel(root, path)
			matches = append(matches, rel)
			if len(matches) >= limit {
				return filepath.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	if len(matches) == 0 {
		return agent.ToolResult{Content: "no files found"}
	}
	return agent.ToolResult{Content: strings.Join(matches, "\n")}
}
