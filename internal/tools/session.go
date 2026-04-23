package tools

import (
	"fmt"
	"strings"

	"github.com/scotmcc/cairo/internal/agent"
	"github.com/scotmcc/cairo/internal/db"
)

// sessionTool is the consolidated session tool — replaces session_list and
// adds session deletion. Delete is guarded: it requires an explicit
// confirm=true, and refuses to delete the currently-active session (which
// would leave the runtime in a bad state).
type sessionTool struct{ db *db.DB }

func Session(database *db.DB) agent.Tool { return sessionTool{db: database} }

func (sessionTool) Name() string { return "session" }
func (sessionTool) Description() string {
	return `Manage conversation sessions — each is an independent thread with its own role and message history.
Actions:
- list: return all sessions with their last-active time.
- delete: remove a session and all its messages/summaries/facts/jobs. Args:
  id (required); confirm=true (required — guard against accidental wipes).
  Cannot delete the session you are currently running in.`
}
func (sessionTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"list", "delete"},
				"description": "Operation to perform.",
			},
			"id":      prop("integer", "Session ID — required for delete."),
			"confirm": prop("boolean", "Must be true for delete to proceed."),
		},
		"required": []string{"action"},
	}
}

func (t sessionTool) Execute(args map[string]any, ctx *agent.ToolContext) agent.ToolResult {
	switch strArg(args, "action") {
	case "list":
		return t.doList()
	case "delete":
		return t.doDelete(args, ctx)
	case "":
		return agent.ToolResult{Content: "error: action is required (list|delete)", IsError: true}
	default:
		return agent.ToolResult{
			Content: fmt.Sprintf("error: unknown action %q — valid: list|delete", strArg(args, "action")),
			IsError: true,
		}
	}
}

func (t sessionTool) doList() agent.ToolResult {
	sessions, err := t.db.Sessions.List()
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	if len(sessions) == 0 {
		return agent.ToolResult{Content: "no sessions"}
	}
	var b strings.Builder
	for _, s := range sessions {
		name := s.Name
		if name == "" {
			name = "(unnamed)"
		}
		fmt.Fprintf(&b, "[%d] %s — role: %s — last active: %s\n",
			s.ID, name, s.Role, s.LastActive.Format("2006-01-02 15:04"))
	}
	return agent.ToolResult{Content: strings.TrimSpace(b.String()), Details: sessions}
}

func (t sessionTool) doDelete(args map[string]any, ctx *agent.ToolContext) agent.ToolResult {
	id := int64(intArg(args, "id", 0))
	if id == 0 {
		return agent.ToolResult{Content: "error: id is required for delete", IsError: true}
	}
	if !boolArg(args, "confirm") {
		return agent.ToolResult{
			Content: "error: delete requires confirm=true — this wipes the session's messages, summaries, facts, and any jobs (with their tasks).",
			IsError: true,
		}
	}
	if ctx.Session != nil && ctx.Session.ID == id {
		return agent.ToolResult{
			Content: fmt.Sprintf("error: cannot delete session %d — it's the session you're running in. Switch to a different session first.", id),
			IsError: true,
		}
	}

	// Verify the target exists first so we can return a useful message.
	if _, err := t.db.Sessions.Get(id); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: session %d not found: %v", id, err), IsError: true}
	}

	if err := t.db.Sessions.Delete(id); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	return agent.ToolResult{Content: fmt.Sprintf("session %d deleted (messages, summaries, facts, jobs cascaded)", id)}
}
