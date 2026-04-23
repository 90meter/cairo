package tools

import (
	"fmt"
	"strings"

	"github.com/scotmcc/cairo/internal/agent"
	"github.com/scotmcc/cairo/internal/db"
)

// skillTool is the consolidated skill tool — replaces skill_list, skill_create,
// skill_read, skill_update, skill_delete.
type skillTool struct{ db *db.DB }

func Skill(database *db.DB) agent.Tool { return skillTool{db: database} }

func (skillTool) Name() string { return "skill" }
func (skillTool) Description() string {
	return `Manage skills — reusable prompt patterns, workflows, and reference material keyed by name.
Actions:
- list: return all skills (name + one-line description).
- read: return the full body of a skill. Args: name (required).
- create: save a new skill. Args: name, description, content (all required); tags (optional).
- update: replace a skill's content. Args: name, content (both required).
- delete: remove a skill. Args: name (required).`
}
func (skillTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"list", "read", "create", "update", "delete"},
				"description": "Operation to perform.",
			},
			"name":        prop("string", "Skill name — required for read, create, update, delete."),
			"description": prop("string", "Short summary — required for create."),
			"content":     prop("string", "Skill body — required for create, update."),
			"tags":        prop("string", "Comma-separated tags — optional for create."),
		},
		"required": []string{"action"},
	}
}

func (t skillTool) Execute(args map[string]any, _ *agent.ToolContext) agent.ToolResult {
	switch strArg(args, "action") {
	case "list":
		return t.doList()
	case "read":
		return t.doRead(args)
	case "create":
		return t.doCreate(args)
	case "update":
		return t.doUpdate(args)
	case "delete":
		return t.doDelete(args)
	case "":
		return agent.ToolResult{Content: "error: action is required (list|read|create|update|delete)", IsError: true}
	default:
		return agent.ToolResult{
			Content: fmt.Sprintf("error: unknown action %q — valid: list|read|create|update|delete", strArg(args, "action")),
			IsError: true,
		}
	}
}

func (t skillTool) doList() agent.ToolResult {
	skills, err := t.db.Skills.List()
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	if len(skills) == 0 {
		return agent.ToolResult{Content: "no skills defined"}
	}
	var b strings.Builder
	for _, s := range skills {
		fmt.Fprintf(&b, "%s — %s\n", s.Name, s.Description)
	}
	return agent.ToolResult{Content: strings.TrimSpace(b.String()), Details: skills}
}

func (t skillTool) doRead(args map[string]any) agent.ToolResult {
	name := strArg(args, "name")
	if name == "" {
		return agent.ToolResult{Content: "error: name is required for read", IsError: true}
	}
	s, err := t.db.Skills.Get(name)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	return agent.ToolResult{
		Content: fmt.Sprintf("# %s\n\n%s\n\n%s", s.Name, s.Description, s.Content),
		Details: s,
	}
}

func (t skillTool) doCreate(args map[string]any) agent.ToolResult {
	name := strArg(args, "name")
	description := strArg(args, "description")
	content := strArg(args, "content")
	if name == "" || description == "" || content == "" {
		return agent.ToolResult{Content: "error: name, description, and content are all required for create", IsError: true}
	}
	if err := t.db.Skills.Create(name, description, content, formatTags(strArg(args, "tags"))); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	return agent.ToolResult{Content: fmt.Sprintf("skill %q saved", name)}
}

func (t skillTool) doUpdate(args map[string]any) agent.ToolResult {
	name := strArg(args, "name")
	content := strArg(args, "content")
	if name == "" || content == "" {
		return agent.ToolResult{Content: "error: name and content are required for update", IsError: true}
	}
	if err := t.db.Skills.Update(name, content); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	return agent.ToolResult{Content: fmt.Sprintf("skill %q updated", name)}
}

func (t skillTool) doDelete(args map[string]any) agent.ToolResult {
	name := strArg(args, "name")
	if name == "" {
		return agent.ToolResult{Content: "error: name is required for delete", IsError: true}
	}
	if err := t.db.Skills.Delete(name); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	return agent.ToolResult{Content: fmt.Sprintf("skill %q deleted", name)}
}
