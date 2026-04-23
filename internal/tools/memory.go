package tools

import (
	"fmt"
	"strings"

	"github.com/scotmcc/cairo/internal/agent"
	"github.com/scotmcc/cairo/internal/db"
)

// Embedder generates vector embeddings for text. The llm.Client satisfies this.
type Embedder interface {
	Embed(model, text string) ([]float32, error)
}

// memoryTool is the consolidated memory tool — replaces memory_add, memory_list,
// memory_search, memory_update, memory_delete. Actions are dispatched on the
// required "action" arg; the other args are conditionally relevant per action.
type memoryTool struct {
	db         *db.DB
	embedder   Embedder
	embedModel string
}

func Memory(database *db.DB, embedder Embedder, embedModel string) agent.Tool {
	return memoryTool{db: database, embedder: embedder, embedModel: embedModel}
}

func (memoryTool) Name() string { return "memory" }
func (memoryTool) Description() string {
	return `Manage persistent memories — a being-wide knowledge store.
Actions:
- add: store a new memory. Args: content (required), tags (optional).
- list: return all memories. No extra args.
- search: semantic similarity search. Args: query (required), limit (optional, default 5).
- update: replace a memory's content. Args: id (required), content (required).
- delete: remove a memory. Args: id (required).`
}
func (memoryTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"add", "list", "search", "update", "delete"},
				"description": "Operation to perform.",
			},
			"content": prop("string", "Memory content — required for add, update."),
			"tags":    prop("string", "Comma-separated tags — optional for add."),
			"query":   prop("string", "Natural-language query — required for search."),
			"id":      prop("integer", "Memory ID — required for update, delete."),
			"limit":   prop("integer", "Max results — optional for search (default 5)."),
		},
		"required": []string{"action"},
	}
}

func (t memoryTool) Execute(args map[string]any, _ *agent.ToolContext) agent.ToolResult {
	switch strArg(args, "action") {
	case "add":
		return t.doAdd(args)
	case "list":
		return t.doList()
	case "search":
		return t.doSearch(args)
	case "update":
		return t.doUpdate(args)
	case "delete":
		return t.doDelete(args)
	case "":
		return agent.ToolResult{Content: "error: action is required (add|list|search|update|delete)", IsError: true}
	default:
		return agent.ToolResult{
			Content: fmt.Sprintf("error: unknown action %q — valid: add|list|search|update|delete", strArg(args, "action")),
			IsError: true,
		}
	}
}

func (t memoryTool) doAdd(args map[string]any) agent.ToolResult {
	content := strArg(args, "content")
	if content == "" {
		return agent.ToolResult{Content: "error: content is required for add", IsError: true}
	}
	tags := formatTags(strArg(args, "tags"))

	var embedding []float32
	if t.embedder != nil && t.embedModel != "" {
		if vec, err := t.embedder.Embed(t.embedModel, content); err == nil {
			embedding = vec
		}
	}

	m, err := t.db.Memories.Add(content, tags, embedding)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	suffix := ""
	if len(embedding) > 0 {
		suffix = fmt.Sprintf(" (%d-dim embedding)", len(embedding))
	}
	return agent.ToolResult{Content: fmt.Sprintf("memory saved (id: %d)%s", m.ID, suffix)}
}

func (t memoryTool) doList() agent.ToolResult {
	memories, err := t.db.Memories.All()
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	if len(memories) == 0 {
		return agent.ToolResult{Content: "no memories stored"}
	}
	var b strings.Builder
	for _, m := range memories {
		fmt.Fprintf(&b, "[%d] %s\n", m.ID, m.Content)
	}
	return agent.ToolResult{Content: strings.TrimSpace(b.String()), Details: memories}
}

func (t memoryTool) doSearch(args map[string]any) agent.ToolResult {
	query := strArg(args, "query")
	if query == "" {
		return agent.ToolResult{Content: "error: query is required for search", IsError: true}
	}
	limit := intArg(args, "limit", 5)

	if t.embedder == nil || t.embedModel == "" {
		return agent.ToolResult{Content: "semantic search unavailable: no embed model configured", IsError: true}
	}

	vec, err := t.embedder.Embed(t.embedModel, query)
	if err != nil || len(vec) == 0 {
		return agent.ToolResult{Content: "failed to embed query — is the embed model running?", IsError: true}
	}

	memories, err := t.db.Memories.Search(vec, limit)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	if len(memories) == 0 {
		return agent.ToolResult{Content: "no matching memories found"}
	}

	var b strings.Builder
	for _, m := range memories {
		fmt.Fprintf(&b, "[%d] %s\n", m.ID, m.Content)
	}
	return agent.ToolResult{Content: strings.TrimSpace(b.String()), Details: memories}
}

func (t memoryTool) doUpdate(args map[string]any) agent.ToolResult {
	id := int64(intArg(args, "id", 0))
	content := strArg(args, "content")
	if id == 0 || content == "" {
		return agent.ToolResult{Content: "error: id and content are required for update", IsError: true}
	}
	if err := t.db.Memories.Update(id, content); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	return agent.ToolResult{Content: fmt.Sprintf("memory %d updated", id)}
}

func (t memoryTool) doDelete(args map[string]any) agent.ToolResult {
	id := int64(intArg(args, "id", 0))
	if id == 0 {
		return agent.ToolResult{Content: "error: id is required for delete", IsError: true}
	}
	if err := t.db.Memories.Delete(id); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	return agent.ToolResult{Content: fmt.Sprintf("memory %d deleted", id)}
}

func formatTags(raw string) string {
	if raw == "" {
		return "[]"
	}
	parts := strings.Split(raw, ",")
	quoted := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			quoted = append(quoted, fmt.Sprintf("%q", p))
		}
	}
	return "[" + strings.Join(quoted, ",") + "]"
}
