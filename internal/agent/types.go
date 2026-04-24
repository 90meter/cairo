package agent

import (
	"context"

	"github.com/scotmcc/cairo/internal/db"
	"github.com/scotmcc/cairo/internal/llm"
)

// Tool is the interface every built-in and custom tool must satisfy.
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]any // JSON Schema object
	Execute(args map[string]any, ctx *ToolContext) ToolResult
}

// ToolResult carries both the model-visible content and UI-only detail.
type ToolResult struct {
	Content string // returned to the model as the tool result
	Details any    // structured data for the TUI renderer (never sent to model)
	IsError bool
}

// ToolContext is passed to every tool Execute call.
// Session and Tools are populated by the agent loop; they let tools reason
// about the being's current state — e.g. prompt_show rebuilds the system
// prompt, which needs the same session/role/tools the loop is using.
// Both may be nil in contexts that don't have an Agent (e.g. future
// standalone tool invocation); tools must guard for that.
type ToolContext struct {
	Ctx     context.Context
	WorkDir string
	DB      *db.DB
	Bus     *Bus // tools can publish progress updates
	Session *db.Session
	Tools   []Tool
}

// ToLLM converts a Tool to the llm.ToolDef wire format.
func ToLLM(t Tool) llm.ToolDef {
	var def llm.ToolDef
	def.Type = "function"
	def.Function.Name = t.Name()
	def.Function.Description = t.Description()
	def.Function.Parameters = t.Parameters()
	return def
}
