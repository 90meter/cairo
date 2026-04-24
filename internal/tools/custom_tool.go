package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/scotmcc/cairo/internal/agent"
	"github.com/scotmcc/cairo/internal/db"
)

// customTool adapts a db.CustomTool to agent.Tool at runtime.
type customTool struct {
	name           string
	description    string
	parameters     map[string]any
	implementation string
	implType       string
	db             *db.DB
}

func newCustomTool(ct *db.CustomTool, database *db.DB) agent.Tool {
	var params map[string]any
	json.Unmarshal([]byte(ct.Parameters), &params)
	if params == nil {
		params = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return &customTool{
		name:           ct.Name,
		description:    ct.Description,
		parameters:     params,
		implementation: ct.Implementation,
		implType:       ct.ImplType,
		db:             database,
	}
}

func (t *customTool) Name() string               { return t.name }
func (t *customTool) Description() string        { return t.description }
func (t *customTool) Parameters() map[string]any { return t.parameters }

func (t *customTool) Execute(args map[string]any, tc *agent.ToolContext) agent.ToolResult {
	// Load config fresh each call so mid-session changes are visible.
	config, _ := t.db.Config.All()

	// Build minimal environment
	env := []string{
		fmt.Sprintf("PATH=%s", os.Getenv("PATH")),
		fmt.Sprintf("HOME=%s", os.Getenv("HOME")),
		fmt.Sprintf("TMPDIR=%s", os.Getenv("TMPDIR")),
		fmt.Sprintf("SHELL=%s", os.Getenv("SHELL")),
	}

	// Inject args as environment variables prefixed with CAIRO_ARG_
	for k, v := range args {
		env = append(env, fmt.Sprintf("CAIRO_ARG_%s=%v", strings.ToUpper(k), v))
	}

	// Also pass as JSON in CAIRO_ARGS
	argsJSON, _ := json.Marshal(args)
	env = append(env, fmt.Sprintf("CAIRO_ARGS=%s", argsJSON))

	// Get safe_env_extras config and add those vars
	if extrasConfig, ok := config["safe_env_extras"]; ok && extrasConfig != "" {
		for _, extra := range strings.Split(extrasConfig, ",") {
			extra = strings.TrimSpace(extra)
			if extra != "" {
				if val := os.Getenv(extra); val != "" {
					env = append(env, fmt.Sprintf("%s=%s", extra, val))
				}
			}
		}
	}

	cmdCtx, cancel := context.WithTimeout(tc.Ctx, 60*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	switch t.implType {
	case "python":
		cmd = exec.CommandContext(cmdCtx, "python3", "-c", t.implementation)
	default:
		cmd = exec.CommandContext(cmdCtx, "bash", "-c", t.implementation)
	}

	cmd.Dir = tc.WorkDir
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	if cmdCtx.Err() != nil && cmd.Process != nil {
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	output := out.String()

	if cmdCtx.Err() == context.DeadlineExceeded {
		return agent.ToolResult{Content: output + "\n[timed out]", IsError: true}
	}
	if err != nil {
		return agent.ToolResult{Content: output, IsError: true}
	}
	return agent.ToolResult{Content: output}
}

// customToolTool is the consolidated custom-tool tool — replaces tool_list,
// tool_create, tool_delete. Named "custom_tool" instead of "tool" to avoid
// the (misleading) implication that it operates on built-in tools as well.
type customToolTool struct{ db *db.DB }

func CustomTool(database *db.DB) agent.Tool { return customToolTool{db: database} }

func (customToolTool) Name() string { return "custom_tool" }
func (customToolTool) Description() string {
	return `Manage AI-authored custom tools — scripts wrapped as callable tools, stored in the DB.
Actions:
- list: return all custom tools with enabled state.
- create: author a new tool. Args: name, description, parameters (JSON Schema string), implementation (script body); impl_type (optional: bash|python, default bash); prompt_addendum (optional — text appended to system prompt when this tool is active).
- delete: remove a custom tool by name. Args: name (required).

Built-in tools are managed by the cairo binary, not through this tool.`
}
func (customToolTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"list", "create", "delete"},
				"description": "Operation to perform.",
			},
			"name":            prop("string", "Tool name — required for create, delete."),
			"description":     prop("string", "What the tool does — required for create."),
			"parameters":      prop("string", "JSON Schema object as a string — required for create."),
			"implementation":  prop("string", "Script body — required for create."),
			"impl_type":       prop("string", "bash (default) or python — optional for create."),
			"prompt_addendum": prop("string", "System prompt text appended when this tool is active — optional for create."),
		},
		"required": []string{"action"},
	}
}

func (t customToolTool) Execute(args map[string]any, _ *agent.ToolContext) agent.ToolResult {
	switch strArg(args, "action") {
	case "list":
		return t.doList()
	case "create":
		return t.doCreate(args)
	case "delete":
		return t.doDelete(args)
	case "":
		return agent.ToolResult{Content: "error: action is required (list|create|delete)", IsError: true}
	default:
		return agent.ToolResult{
			Content: fmt.Sprintf("error: unknown action %q — valid: list|create|delete", strArg(args, "action")),
			IsError: true,
		}
	}
}

func (t customToolTool) doList() agent.ToolResult {
	tools, err := t.db.Tools.List()
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	if len(tools) == 0 {
		return agent.ToolResult{Content: "no custom tools defined"}
	}
	var b strings.Builder
	for _, tool := range tools {
		status := "enabled"
		if !tool.Enabled {
			status = "disabled"
		}
		fmt.Fprintf(&b, "[%s] %s — %s (%s)\n", status, tool.Name, tool.Description, tool.ImplType)
	}
	return agent.ToolResult{Content: strings.TrimSpace(b.String()), Details: tools}
}

func (t customToolTool) doCreate(args map[string]any) agent.ToolResult {
	name := strArg(args, "name")
	description := strArg(args, "description")
	parameters := strArg(args, "parameters")
	implementation := strArg(args, "implementation")
	if name == "" || description == "" || parameters == "" || implementation == "" {
		return agent.ToolResult{Content: "error: name, description, parameters, and implementation are all required for create", IsError: true}
	}
	implType := strArg(args, "impl_type")
	if implType == "" {
		implType = "bash"
	}
	promptAddendum := strArg(args, "prompt_addendum")

	var schema map[string]any
	if err := json.Unmarshal([]byte(parameters), &schema); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: parameters is not valid JSON: %v", err), IsError: true}
	}

	if err := t.db.Tools.Create(name, description, parameters, implementation, implType, promptAddendum); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	return agent.ToolResult{Content: fmt.Sprintf("tool %q created — will be available on next session start", name)}
}

func (t customToolTool) doDelete(args map[string]any) agent.ToolResult {
	name := strArg(args, "name")
	if name == "" {
		return agent.ToolResult{Content: "error: name is required for delete", IsError: true}
	}
	if err := t.db.Tools.Delete(name); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	return agent.ToolResult{Content: fmt.Sprintf("tool %q deleted", name)}
}
