package tools

// dbtools.go now holds only the tools that didn't fold into a consolidated
// entity-family tool: prompt_show (self-inspection) and tool_list_builtin
// (registry mirror).

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/scotmcc/cairo/internal/agent"
	"github.com/scotmcc/cairo/internal/providers"
)

// --- prompt_show ---

type promptShowTool struct{}

// PromptShow returns the assembled system prompt the being will see next turn.
// It takes no DB handle — everything it needs is on the ToolContext.
func PromptShow() agent.Tool { return promptShowTool{} }

func (promptShowTool) Name() string { return "prompt_show" }
func (promptShowTool) Description() string {
	return "Return the full assembled system prompt for the current turn — base + soul + " +
		"role addendum + tool addenda + conversation summaries + recent memories + date/cwd. " +
		"Use this to reason about what shape you're in or to debug prompt composition."
}
func (promptShowTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func (promptShowTool) Execute(_ map[string]any, ctx *agent.ToolContext) agent.ToolResult {
	if ctx.Session == nil {
		return agent.ToolResult{Content: "error: prompt_show requires a session context", IsError: true}
	}
	reg := ctx.Registry
	if reg == nil {
		reg = providers.Default()
	}
	msg, err := agent.BuildSystemPrompt(ctx.DB, ctx.Session.ID, ctx.Session.Role, ctx.WorkDir, ctx.Tools, time.Time{}, reg)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error building prompt: %v", err), IsError: true}
	}
	return agent.ToolResult{Content: msg.Content}
}

// --- tool_list_builtin ---

// toolListBuiltinTool returns the names of all built-in tools.
// The list is captured from the live registry at construction time so it
// stays in lockstep with tools.Default() without a hardcoded duplicate.
type toolListBuiltinTool struct {
	names []string
}

// ToolListBuiltin constructs the tool. Pass the list of registered built-in
// names — typically derived by iterating tools.Default() and reading Name().
func ToolListBuiltin(names []string) agent.Tool {
	sorted := make([]string, len(names))
	copy(sorted, names)
	sort.Strings(sorted)
	return toolListBuiltinTool{names: sorted}
}

func (toolListBuiltinTool) Name() string        { return "tool_list_builtin" }
func (toolListBuiltinTool) Description() string { return "List all built-in tools (not custom tools)." }
func (toolListBuiltinTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func (t toolListBuiltinTool) Execute(_ map[string]any, _ *agent.ToolContext) agent.ToolResult {
	if len(t.names) == 0 {
		return agent.ToolResult{Content: "no built-in tools"}
	}
	var b strings.Builder
	for _, name := range t.names {
		fmt.Fprintf(&b, "%s\n", name)
	}
	return agent.ToolResult{Content: strings.TrimSpace(b.String()), Details: t.names}
}
