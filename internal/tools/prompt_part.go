package tools

import (
	"fmt"
	"strings"

	"github.com/scotmcc/cairo/internal/agent"
	"github.com/scotmcc/cairo/internal/db"
)

// promptPartTool is the consolidated prompt-part tool — replaces
// prompt_part_add, prompt_part_list, prompt_part_update, prompt_part_delete,
// prompt_part_toggle.
type promptPartTool struct{ db *db.DB }

func PromptPart(database *db.DB) agent.Tool { return promptPartTool{db: database} }

func (promptPartTool) Name() string { return "prompt_part" }
func (promptPartTool) Description() string {
	return `Manage modular system-prompt parts.
Actions:
- add: insert a new part. Args: key, content (required); trigger (optional — empty=always, "role:NAME", "tool:NAME"); load_order (optional, default 100).
- list: return all parts with enabled state, trigger, and load_order.
- update: replace a part's content. Args: id, content (required).
- delete: remove a part. Args: id (required).
- toggle: flip a part's enabled state. Args: id (required).`
}
func (promptPartTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"add", "list", "update", "delete", "toggle"},
				"description": "Operation to perform.",
			},
			"id":         prop("integer", "Prompt-part ID — required for update, delete, toggle."),
			"key":        prop("string", "Identifier — required for add."),
			"content":    prop("string", "Prompt text — required for add, update."),
			"trigger":    prop("string", "When to load — optional for add (empty=always, 'role:NAME', 'tool:NAME')."),
			"load_order": prop("integer", "Lower = earlier — optional for add (default 100)."),
		},
		"required": []string{"action"},
	}
}

func (t promptPartTool) Execute(args map[string]any, _ *agent.ToolContext) agent.ToolResult {
	switch strArg(args, "action") {
	case "add":
		return t.doAdd(args)
	case "list":
		return t.doList()
	case "update":
		return t.doUpdate(args)
	case "delete":
		return t.doDelete(args)
	case "toggle":
		return t.doToggle(args)
	case "":
		return agent.ToolResult{Content: "error: action is required (add|list|update|delete|toggle)", IsError: true}
	default:
		return agent.ToolResult{
			Content: fmt.Sprintf("error: unknown action %q — valid: add|list|update|delete|toggle", strArg(args, "action")),
			IsError: true,
		}
	}
}

func (t promptPartTool) doAdd(args map[string]any) agent.ToolResult {
	key := strArg(args, "key")
	content := strArg(args, "content")
	if key == "" || content == "" {
		return agent.ToolResult{Content: "error: key and content are required for add", IsError: true}
	}
	trigger := strArg(args, "trigger")
	order := intArg(args, "load_order", 100)
	if err := t.db.Prompts.Add(key, content, trigger, order); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	return agent.ToolResult{Content: fmt.Sprintf("prompt part %q added (trigger: %q, order: %d)", key, trigger, order)}
}

func (t promptPartTool) doList() agent.ToolResult {
	parts, err := t.db.Prompts.All()
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	if len(parts) == 0 {
		return agent.ToolResult{Content: "no prompt parts"}
	}
	var b strings.Builder
	for _, p := range parts {
		status := "enabled"
		if !p.Enabled {
			status = "disabled"
		}
		trigger := p.Trigger
		if trigger == "" {
			trigger = "(always)"
		}
		fmt.Fprintf(&b, "[%d %s] %s — trigger: %s — order: %d\n", p.ID, status, p.Key, trigger, p.LoadOrder)
	}
	return agent.ToolResult{Content: strings.TrimSpace(b.String()), Details: parts}
}

func (t promptPartTool) doUpdate(args map[string]any) agent.ToolResult {
	id := int64(intArg(args, "id", 0))
	content := strArg(args, "content")
	if id == 0 || content == "" {
		return agent.ToolResult{Content: "error: id and content are required for update", IsError: true}
	}
	if err := t.db.Prompts.Update(id, content); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	return agent.ToolResult{Content: fmt.Sprintf("prompt part %d updated", id)}
}

func (t promptPartTool) doDelete(args map[string]any) agent.ToolResult {
	id := int64(intArg(args, "id", 0))
	if id == 0 {
		return agent.ToolResult{Content: "error: id is required for delete", IsError: true}
	}
	if err := t.db.Prompts.Delete(id); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	return agent.ToolResult{Content: fmt.Sprintf("prompt part %d deleted", id)}
}

func (t promptPartTool) doToggle(args map[string]any) agent.ToolResult {
	id := int64(intArg(args, "id", 0))
	if id == 0 {
		return agent.ToolResult{Content: "error: id is required for toggle", IsError: true}
	}
	parts, err := t.db.Prompts.All()
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	var currentEnabled bool
	var found bool
	for _, p := range parts {
		if p.ID == id {
			currentEnabled = p.Enabled
			found = true
			break
		}
	}
	if !found {
		return agent.ToolResult{Content: fmt.Sprintf("error: prompt part %d not found", id), IsError: true}
	}
	newEnabled := !currentEnabled
	if err := t.db.Prompts.SetEnabled(id, newEnabled); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	status := "enabled"
	if !newEnabled {
		status = "disabled"
	}
	return agent.ToolResult{Content: fmt.Sprintf("prompt part %d → %s", id, status)}
}
