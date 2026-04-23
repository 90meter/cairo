package tools

import (
	"fmt"
	"strings"

	"github.com/scotmcc/cairo/internal/agent"
	"github.com/scotmcc/cairo/internal/db"
)

// configTool is the consolidated config tool — replaces config_get, config_set,
// config_list, unsafe_mode_enable, unsafe_mode_disable. The two unsafe_mode
// helpers collapsed into config(action="set", key="unsafe_mode", value="true|false").
type configTool struct{ db *db.DB }

func Config(database *db.DB) agent.Tool { return configTool{db: database} }

func (configTool) Name() string { return "config" }
func (configTool) Description() string {
	return `Read and write being-level configuration (ollama_url, model, memory_limit,
unsafe_mode, summary_*, soul_prompt, init_complete, etc).
Actions:
- get: return a single value. Args: key (required).
- set: write a value. Args: key, value (both required).
- list: return all config keys and values.

Common keys:
  unsafe_mode (true|false) — permits file writes outside session CWD
  memory_limit — max memories injected into the system prompt
  summary_threshold — unsummarized messages before summarizer fires
  summary_model, embed_model — Ollama model names`
}
func (configTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"get", "set", "list"},
				"description": "Operation to perform.",
			},
			"key":   prop("string", "Config key — required for get, set."),
			"value": prop("string", "Config value — required for set."),
		},
		"required": []string{"action"},
	}
}

func (t configTool) Execute(args map[string]any, _ *agent.ToolContext) agent.ToolResult {
	switch strArg(args, "action") {
	case "get":
		return t.doGet(args)
	case "set":
		return t.doSet(args)
	case "list":
		return t.doList()
	case "":
		return agent.ToolResult{Content: "error: action is required (get|set|list)", IsError: true}
	default:
		return agent.ToolResult{
			Content: fmt.Sprintf("error: unknown action %q — valid: get|set|list", strArg(args, "action")),
			IsError: true,
		}
	}
}

func (t configTool) doGet(args map[string]any) agent.ToolResult {
	key := strArg(args, "key")
	if key == "" {
		return agent.ToolResult{Content: "error: key is required for get", IsError: true}
	}
	val, err := t.db.Config.Get(key)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	if val == "" {
		return agent.ToolResult{Content: fmt.Sprintf("%s = (not set)", key)}
	}
	return agent.ToolResult{Content: fmt.Sprintf("%s = %s", key, val)}
}

func (t configTool) doSet(args map[string]any) agent.ToolResult {
	key := strArg(args, "key")
	value := strArg(args, "value")
	if key == "" {
		return agent.ToolResult{Content: "error: key is required for set", IsError: true}
	}
	if err := t.db.Config.Set(key, value); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	return agent.ToolResult{Content: fmt.Sprintf("set %s = %s", key, value)}
}

func (t configTool) doList() agent.ToolResult {
	cfg, err := t.db.Config.All()
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	if len(cfg) == 0 {
		return agent.ToolResult{Content: "no config keys"}
	}
	var b strings.Builder
	for k, v := range cfg {
		fmt.Fprintf(&b, "%s = %s\n", k, v)
	}
	return agent.ToolResult{Content: strings.TrimSpace(b.String()), Details: cfg}
}
