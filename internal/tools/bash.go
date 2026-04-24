package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"syscall"
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
	_ = unsafeMode

	timeoutSec := intArg(args, "timeout", 30)
	if timeoutSec > 120 {
		timeoutSec = 120
	}
	timeout := time.Duration(timeoutSec) * time.Second

	cmdCtx, cancel := context.WithTimeout(ctx.Ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "bash", "-c", command)
	cmd.Dir = ctx.WorkDir
	// Put bash and all its children in a new process group so we can kill
	// the whole tree on timeout (catches grandchildren like git credential helpers).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()

	// Kill the entire process group to clean up any grandchildren.
	if cmdCtx.Err() != nil && cmd.Process != nil {
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}

	output := out.String()
	originalSize := len(output)

	if cmdCtx.Err() != nil {
		msg := fmt.Sprintf("\n[timed out after %s]", timeout)
		if ctx.Ctx.Err() != nil {
			msg = "\n[cancelled]"
		}
		return agent.ToolResult{
			Content: output + msg,
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
