package tools

import (
	"fmt"
	"strings"

	"github.com/scotmcc/cairo/internal/agent"
	"github.com/scotmcc/cairo/internal/db"
)

// roleTool is the consolidated role tool — replaces role_list, role_model_set.
// A role is a focus mode with its own model, prompt addendum, and tool
// allowlist; this tool reads and edits role configuration.
type roleTool struct{ db *db.DB }

func Role(database *db.DB) agent.Tool { return roleTool{db: database} }

func (roleTool) Name() string { return "role" }
func (roleTool) Description() string {
	return `Inspect and configure agent roles — focus modes of the being (not separate identities).
Actions:
- list: return all defined roles with their current model assignment.
- model_set: change the Ollama model used by a role. Args: role, model (both required).
  Takes effect for new sessions and background tasks in that role.`
}
func (roleTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"list", "model_set"},
				"description": "Operation to perform.",
			},
			"role":  prop("string", "Role name — required for model_set."),
			"model": prop("string", "Ollama model name — required for model_set."),
		},
		"required": []string{"action"},
	}
}

func (t roleTool) Execute(args map[string]any, _ *agent.ToolContext) agent.ToolResult {
	switch strArg(args, "action") {
	case "list":
		return t.doList()
	case "model_set":
		return t.doModelSet(args)
	case "":
		return agent.ToolResult{Content: "error: action is required (list|model_set)", IsError: true}
	default:
		return agent.ToolResult{
			Content: fmt.Sprintf("error: unknown action %q — valid: list|model_set", strArg(args, "action")),
			IsError: true,
		}
	}
}

func (t roleTool) doList() agent.ToolResult {
	roles, err := t.db.Roles.List()
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	if len(roles) == 0 {
		return agent.ToolResult{Content: "no roles defined"}
	}
	var b strings.Builder
	for _, r := range roles {
		model := r.Model
		if model == "" {
			model = "(default)"
		}
		fmt.Fprintf(&b, "%s — %s [model: %s]\n", r.Name, r.Description, model)
	}
	return agent.ToolResult{Content: strings.TrimSpace(b.String()), Details: roles}
}

func (t roleTool) doModelSet(args map[string]any) agent.ToolResult {
	role := strArg(args, "role")
	model := strArg(args, "model")
	if role == "" || model == "" {
		return agent.ToolResult{Content: "error: role and model are required for model_set", IsError: true}
	}
	if err := t.db.Roles.SetModel(role, model); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	return agent.ToolResult{Content: fmt.Sprintf("role %q now uses model %q", role, model)}
}
