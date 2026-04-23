package tools

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/scotmcc/cairo/internal/agent"
	"github.com/scotmcc/cairo/internal/db"
)

const soulMaxChars = 300

// soulTool is the consolidated soul tool — replaces soul_get, soul_set.
// The soul is the AI's self-maintained persona, stored in config.soul_prompt.
type soulTool struct{ db *db.DB }

func Soul(database *db.DB) agent.Tool { return soulTool{db: database} }

func (soulTool) Name() string { return "soul" }
func (soulTool) Description() string {
	return fmt.Sprintf(`Read or rewrite your soul — the short self-description that shapes every session.
Actions:
- get: return the current soul text.
- set: replace the soul entirely. Args: soul (required, max %d characters).
  Keep it tight: voice, tendencies, what you care about. Takes effect next message.`, soulMaxChars)
}
func (soulTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"get", "set"},
				"description": "Operation to perform.",
			},
			"soul": prop("string", fmt.Sprintf("Your persona, max %d characters — required for set.", soulMaxChars)),
		},
		"required": []string{"action"},
	}
}

func (t soulTool) Execute(args map[string]any, _ *agent.ToolContext) agent.ToolResult {
	switch strArg(args, "action") {
	case "get":
		return t.doGet()
	case "set":
		return t.doSet(args)
	case "":
		return agent.ToolResult{Content: "error: action is required (get|set)", IsError: true}
	default:
		return agent.ToolResult{
			Content: fmt.Sprintf("error: unknown action %q — valid: get|set", strArg(args, "action")),
			IsError: true,
		}
	}
}

func (t soulTool) doGet() agent.ToolResult {
	soul, err := t.db.Config.Get("soul_prompt")
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	if soul == "" {
		return agent.ToolResult{Content: "(soul not set)"}
	}
	return agent.ToolResult{Content: soul}
}

func (t soulTool) doSet(args map[string]any) agent.ToolResult {
	soul := strings.TrimSpace(strArg(args, "soul"))
	if soul == "" {
		return agent.ToolResult{Content: "error: soul is required for set", IsError: true}
	}
	if utf8.RuneCountInString(soul) > soulMaxChars {
		return agent.ToolResult{
			Content: fmt.Sprintf("error: soul is %d characters, max is %d. Trim it down.", utf8.RuneCountInString(soul), soulMaxChars),
			IsError: true,
		}
	}
	if err := t.db.Config.Set("soul_prompt", soul); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	return agent.ToolResult{Content: fmt.Sprintf("soul updated (%d chars). Takes effect next message.", utf8.RuneCountInString(soul))}
}
