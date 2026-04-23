package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/scotmcc/cairo/internal/agent"
)

type bashTool struct{}

func Bash() agent.Tool { return bashTool{} }

func (bashTool) Name() string        { return "bash" }
func (bashTool) Description() string { return "Run a shell command. Returns combined stdout and stderr." }
func (bashTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": prop("string", "Shell command to execute"),
			"timeout": prop("integer", "Timeout in seconds (default 30, max 120)"),
		},
		"required": []string{"command"},
	}
}

func (bashTool) Execute(args map[string]any, ctx *agent.ToolContext) agent.ToolResult {
	command := strArg(args, "command")
	if command == "" {
		return agent.ToolResult{Content: "error: command is required", IsError: true}
	}

	// Check unsafe_mode
	unsafeMode := "false"
	if val, err := ctx.DB.Config.Get("unsafe_mode"); err == nil && val != "" {
		unsafeMode = val
	}
	// bash is always allowed — shell commands cannot be reliably path-scoped.
	// unsafe_mode=false still permits bash; it only restricts file write/edit paths.
	_ = unsafeMode

	timeoutSec := intArg(args, "timeout", 30)
	if timeoutSec > 120 {
		timeoutSec = 120
	}
	timeout := time.Duration(timeoutSec) * time.Second

	cmdCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "bash", "-c", command)
	cmd.Dir = ctx.WorkDir

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	output := out.String()
	originalSize := len(output)

	if cmdCtx.Err() == context.DeadlineExceeded {
		return agent.ToolResult{
			Content: output + fmt.Sprintf("\n[timed out after %s]", timeout),
			IsError: true,
		}
	}
	if err != nil {
		return agent.ToolResult{
			Content: output,
			IsError: true,
			Details: err.Error(),
		}
	}

	// Cap output at 65536 bytes
	const maxBytes = 65536
	if originalSize > maxBytes {
		output = string([]byte(output)[:maxBytes])
		output += fmt.Sprintf("\n[truncated: original size was %d bytes]", originalSize)
	}

	return agent.ToolResult{Content: output}
}
