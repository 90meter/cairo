package tools

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/scotmcc/cairo/internal/agent"
	"github.com/scotmcc/cairo/internal/db"
)

// Agent is the consolidated agent tool — replaces agent_spawn, agent_wait,
// agent_log. Spawn launches a background cairo subprocess against a task
// (atomic claim, deps enforced). Wait blocks until a task reaches a terminal
// state. Log reads the task's stdout/stderr capture file.
type agentTool struct{ db *db.DB }

func Agent(database *db.DB) agent.Tool { return agentTool{db: database} }

func (agentTool) Name() string { return "agent" }
func (agentTool) Description() string {
	return `Control background agents — parallel threads of the being's own attention.
Actions:
- spawn: launch a subprocess for a task. Args: id (required). Task must exist with an
  assigned_role; deps must be done. Returns immediately — the subprocess runs in the
  background. Use action="wait" or task(action="list") to monitor.
- wait: block until a task reaches done/failed. Args: id (required); timeout (optional
  seconds, default 300, max 3600).
- log: read the captured stdout/stderr of a background task. Args: id (required);
  tail (optional — number of lines from the end).`
}
func (agentTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"spawn", "wait", "log"},
				"description": "Operation to perform.",
			},
			"id":      prop("integer", "Task ID — required for all actions."),
			"timeout": prop("integer", "Seconds to wait before giving up — optional for wait (default 300, max 3600)."),
			"tail":    prop("integer", "Lines from the end of the log — optional for log (default: all)."),
		},
		"required": []string{"action"},
	}
}

func (t agentTool) Execute(args map[string]any, ctx *agent.ToolContext) agent.ToolResult {
	switch strArg(args, "action") {
	case "spawn":
		return t.doSpawn(args)
	case "wait":
		return t.doWait(args, ctx)
	case "log":
		return t.doLog(args)
	case "":
		return agent.ToolResult{Content: "error: action is required (spawn|wait|log)", IsError: true}
	default:
		return agent.ToolResult{
			Content: fmt.Sprintf("error: unknown action %q — valid: spawn|wait|log", strArg(args, "action")),
			IsError: true,
		}
	}
}

func (t agentTool) doSpawn(args map[string]any) agent.ToolResult {
	taskID := int64(intArg(args, "id", 0))
	if taskID == 0 {
		return agent.ToolResult{Content: "error: id is required for spawn", IsError: true}
	}

	// Atomically claim the task — fails if already running, already done,
	// or deps aren't met. Prevents concurrent spawns double-advancing the
	// same task (§1.2).
	task, err := t.db.Tasks.ClaimForSpawn(taskID)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}

	logDir := filepath.Join(db.DefaultDataDir(), "logs")
	os.MkdirAll(logDir, 0755)
	logPath := filepath.Join(logDir, fmt.Sprintf("task_%d.log", taskID))
	t.db.Tasks.SetLogPath(taskID, logPath)

	exe, err := os.Executable()
	if err != nil {
		exe = "cairo"
	}

	logFile, err := os.Create(logPath)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error creating log: %v", err), IsError: true}
	}

	cmd := exec.Command(exe,
		fmt.Sprintf("-task=%d", taskID),
		"-background",
		"-new",
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = detached()

	if err := cmd.Start(); err != nil {
		logFile.Close()
		t.db.Tasks.SetStatus(taskID, "failed")
		t.db.Tasks.SetResult(taskID, fmt.Sprintf("spawn failed: %v", err))
		return agent.ToolResult{Content: fmt.Sprintf("error spawning process: %v", err), IsError: true}
	}

	logFile.Close()
	t.db.Tasks.SetPID(taskID, cmd.Process.Pid)
	cmd.Process.Release()

	return agent.ToolResult{
		Content: fmt.Sprintf("task %d spawned (pid %d, role: %s)\nlog: %s",
			taskID, cmd.Process.Pid, task.AssignedRole, logPath),
		Details: map[string]any{"task_id": taskID, "pid": cmd.Process.Pid, "log_path": logPath},
	}
}

func (t agentTool) doWait(args map[string]any, ctx *agent.ToolContext) agent.ToolResult {
	taskID := int64(intArg(args, "id", 0))
	if taskID == 0 {
		return agent.ToolResult{Content: "error: id is required for wait", IsError: true}
	}
	timeoutSec := intArg(args, "timeout", 300)
	if timeoutSec > 3600 {
		timeoutSec = 3600
	}

	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	tick := 2 * time.Second

	for time.Now().Before(deadline) {
		task, err := t.db.Tasks.Get(taskID)
		if err != nil {
			return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
		}

		switch task.Status {
		case "done":
			return agent.ToolResult{
				Content: fmt.Sprintf("task %d done\n\n%s", taskID, task.Result),
				Details: task,
			}
		case "failed":
			return agent.ToolResult{
				Content: fmt.Sprintf("task %d failed\n\n%s", taskID, task.Result),
				IsError: true,
				Details: task,
			}
		}

		if ctx.Bus != nil {
			ctx.Bus.Publish(agent.Event{
				Type:    agent.EventToolUpdate,
				Payload: agent.PayloadToolUpdate{Name: "agent", Output: fmt.Sprintf("task %d: %s", taskID, task.Status)},
			})
		}

		select {
		case <-time.After(tick):
		case <-ctx.Ctx.Done():
			return agent.ToolResult{Content: "wait cancelled", IsError: true}
		}
	}

	return agent.ToolResult{
		Content: fmt.Sprintf("timeout: task %d did not complete within %ds", taskID, timeoutSec),
		IsError: true,
	}
}

func (t agentTool) doLog(args map[string]any) agent.ToolResult {
	taskID := int64(intArg(args, "id", 0))
	if taskID == 0 {
		return agent.ToolResult{Content: "error: id is required for log", IsError: true}
	}
	task, err := t.db.Tasks.Get(taskID)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	if task.LogPath == "" {
		return agent.ToolResult{Content: fmt.Sprintf("no log for task %d", taskID)}
	}

	data, err := os.ReadFile(task.LogPath)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error reading log: %v", err), IsError: true}
	}

	const maxLog = 65536

	content := string(data)
	tail := intArg(args, "tail", 0)
	if tail > 0 {
		lines := strings.Split(content, "\n")
		if tail < len(lines) {
			lines = lines[len(lines)-tail:]
		}
		content = strings.Join(lines, "\n")
	}

	if len(content) > maxLog {
		total := len(content)
		content = content[len(content)-maxLog:]
		content += fmt.Sprintf("\n[log truncated — %d bytes total, showing last %d]", total, maxLog)
	}

	if content == "" {
		return agent.ToolResult{Content: "(empty log)"}
	}
	return agent.ToolResult{Content: content}
}
