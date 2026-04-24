package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/scotmcc/cairo/internal/agent"
)

type grepTool struct{}

func Grep() agent.Tool { return grepTool{} }

func (grepTool) Name() string        { return "grep" }
func (grepTool) Description() string { return "Search for a pattern in files. Uses ripgrep if available, falls back to grep." }
func (grepTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern":     prop("string", "Search pattern (regex)"),
			"path":        prop("string", "Directory or file to search (default: working directory)"),
			"glob":        prop("string", "File glob filter, e.g. '*.go'"),
			"ignore_case": prop("boolean", "Case-insensitive search"),
			"context":     prop("integer", "Lines of context around each match (default 0)"),
			"limit":       prop("integer", "Maximum number of results (default 100)"),
		},
		"required": []string{"pattern"},
	}
}

func (grepTool) Execute(args map[string]any, ctx *agent.ToolContext) agent.ToolResult {
	pattern := strArg(args, "pattern")
	path := strArg(args, "path")
	if path == "" {
		path = ctx.WorkDir
	} else {
		path = resolvePath(path, ctx.WorkDir)
	}
	glob := strArg(args, "glob")
	ignoreCase := boolArg(args, "ignore_case")
	ctxLines := intArg(args, "context", 0)
	limit := intArg(args, "limit", 100)

	// prefer rg
	if _, err := exec.LookPath("rg"); err == nil {
		return runRg(ctx.Ctx, pattern, path, glob, ignoreCase, ctxLines, limit)
	}
	return runGrep(ctx.Ctx, pattern, path, glob, ignoreCase, ctxLines, limit, ctx.WorkDir)
}

const grepMaxBytes = 500 * 1024

func runRg(gctx context.Context, pattern, path, glob string, ignoreCase bool, ctxLines, limit int) agent.ToolResult {
	argv := []string{"--no-heading", "--line-number", fmt.Sprintf("--max-count=%d", limit)}
	if ignoreCase {
		argv = append(argv, "-i")
	}
	if ctxLines > 0 {
		argv = append(argv, fmt.Sprintf("-C%d", ctxLines))
	}
	if glob != "" {
		argv = append(argv, "--glob", glob)
	}
	argv = append(argv, pattern, path)

	var out bytes.Buffer
	cmd := exec.CommandContext(gctx, "rg", argv...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.Run()
	result := strings.TrimSpace(out.String())
	if result == "" {
		return agent.ToolResult{Content: "no matches found"}
	}
	if len(result) > grepMaxBytes {
		result = result[:grepMaxBytes] + "\n[output truncated at 500KB]"
	}
	return agent.ToolResult{Content: result}
}

func runGrep(gctx context.Context, pattern, path, glob string, ignoreCase bool, ctxLines, limit int, workDir string) agent.ToolResult {
	argv := []string{"-rn", fmt.Sprintf("--max-count=%d", limit)}
	if ignoreCase {
		argv = append(argv, "-i")
	}
	if ctxLines > 0 {
		argv = append(argv, fmt.Sprintf("-C%d", ctxLines))
	}
	if glob != "" {
		argv = append(argv, "--include="+glob)
	}
	argv = append(argv, pattern, path)

	var out bytes.Buffer
	cmd := exec.CommandContext(gctx, "grep", argv...)
	cmd.Dir = workDir
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.Run()
	result := strings.TrimSpace(out.String())
	if result == "" {
		return agent.ToolResult{Content: "no matches found"}
	}
	if len(result) > grepMaxBytes {
		result = result[:grepMaxBytes] + "\n[output truncated at 500KB]"
	}
	return agent.ToolResult{Content: result}
}
