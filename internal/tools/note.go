package tools

import (
	"fmt"
	"strings"

	"github.com/scotmcc/cairo/internal/agent"
	"github.com/scotmcc/cairo/internal/db"
)

// noteTool is the consolidated note tool — replaces note_create, note_list,
// note_read, note_update, note_delete.
type noteTool struct{ db *db.DB }

func Note(database *db.DB) agent.Tool { return noteTool{db: database} }

func (noteTool) Name() string { return "note" }
func (noteTool) Description() string {
	return `Manage notes — freeform scratch-pad entries, separate from memories.
Actions:
- create: save a new note. Args: content (required), title (optional), tags (optional).
- list: return all notes (titles only). No extra args.
- read: return the full body of a note. Args: id (required).
- update: edit a note's title and/or content. Args: id (required), title (optional), content (optional).
- delete: remove a note. Args: id (required).`
}
func (noteTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"create", "list", "read", "update", "delete"},
				"description": "Operation to perform.",
			},
			"id":      prop("integer", "Note ID — required for read, update, delete."),
			"title":   prop("string", "Optional title — used for create, update."),
			"content": prop("string", "Note body — required for create, optional for update."),
			"tags":    prop("string", "Comma-separated tags — optional for create."),
		},
		"required": []string{"action"},
	}
}

func (t noteTool) Execute(args map[string]any, _ *agent.ToolContext) agent.ToolResult {
	switch strArg(args, "action") {
	case "create":
		return t.doCreate(args)
	case "list":
		return t.doList()
	case "read":
		return t.doRead(args)
	case "update":
		return t.doUpdate(args)
	case "delete":
		return t.doDelete(args)
	case "":
		return agent.ToolResult{Content: "error: action is required (create|list|read|update|delete)", IsError: true}
	default:
		return agent.ToolResult{
			Content: fmt.Sprintf("error: unknown action %q — valid: create|list|read|update|delete", strArg(args, "action")),
			IsError: true,
		}
	}
}

func (t noteTool) doCreate(args map[string]any) agent.ToolResult {
	content := strArg(args, "content")
	if content == "" {
		return agent.ToolResult{Content: "error: content is required for create", IsError: true}
	}
	n, err := t.db.Notes.Create(strArg(args, "title"), content, formatTags(strArg(args, "tags")))
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	return agent.ToolResult{Content: fmt.Sprintf("note saved (id: %d)", n.ID)}
}

func (t noteTool) doList() agent.ToolResult {
	notes, err := t.db.Notes.List()
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	if len(notes) == 0 {
		return agent.ToolResult{Content: "no notes"}
	}
	var b strings.Builder
	for _, n := range notes {
		title := n.Title
		if title == "" {
			title = "(untitled)"
		}
		fmt.Fprintf(&b, "[%d] %s\n", n.ID, title)
	}
	return agent.ToolResult{Content: strings.TrimSpace(b.String()), Details: notes}
}

func (t noteTool) doRead(args map[string]any) agent.ToolResult {
	id := int64(intArg(args, "id", 0))
	if id == 0 {
		return agent.ToolResult{Content: "error: id is required for read", IsError: true}
	}
	n, err := t.db.Notes.Get(id)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	title := n.Title
	if title == "" {
		title = "(untitled)"
	}
	return agent.ToolResult{
		Content: fmt.Sprintf("# %s\n\n%s", title, n.Content),
		Details: n,
	}
}

func (t noteTool) doUpdate(args map[string]any) agent.ToolResult {
	id := int64(intArg(args, "id", 0))
	if id == 0 {
		return agent.ToolResult{Content: "error: id is required for update", IsError: true}
	}
	if err := t.db.Notes.Update(id, strArg(args, "title"), strArg(args, "content")); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	return agent.ToolResult{Content: fmt.Sprintf("note %d updated", id)}
}

func (t noteTool) doDelete(args map[string]any) agent.ToolResult {
	id := int64(intArg(args, "id", 0))
	if id == 0 {
		return agent.ToolResult{Content: "error: id is required for delete", IsError: true}
	}
	if err := t.db.Notes.Delete(id); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	return agent.ToolResult{Content: fmt.Sprintf("note %d deleted", id)}
}
