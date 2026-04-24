package tools

// knowledge.go — read-only queries into the being's knowledge base:
// summary semantic search, fact promotion, note reading, skill reading,
// role listing, task artifact inspection.

import (
	"fmt"
	"strings"

	"github.com/scotmcc/cairo/internal/agent"
	"github.com/scotmcc/cairo/internal/db"
)

// --- summary_search ---

type summarySearchTool struct {
	db         *db.DB
	embedder   Embedder
	embedModel string
}

func SummarySearch(database *db.DB, embedder Embedder, embedModel string) agent.Tool {
	return summarySearchTool{db: database, embedder: embedder, embedModel: embedModel}
}

func (summarySearchTool) Name() string { return "summary_search" }
func (summarySearchTool) Description() string {
	return "Search across all session summaries by semantic similarity. " +
		"Summaries are older conversation segments — use this to recall what a prior session covered."
}
func (summarySearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": prop("string", "Natural language query to search for"),
			"limit": prop("integer", "Maximum results to return (default 5)"),
		},
		"required": []string{"query"},
	}
}

func (t summarySearchTool) Execute(args map[string]any, _ *agent.ToolContext) agent.ToolResult {
	query := strArg(args, "query")
	limit := intArg(args, "limit", 5)

	if t.embedder == nil || t.embedModel == "" {
		return agent.ToolResult{Content: "semantic search unavailable: no embed model configured", IsError: true}
	}

	vec, err := t.embedder.Embed(t.embedModel, query)
	if err != nil || len(vec) == 0 {
		return agent.ToolResult{Content: "failed to embed query — is the embed model running?", IsError: true}
	}

	summaries, err := t.db.Summaries.Search(vec, limit)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	if len(summaries) == 0 {
		return agent.ToolResult{Content: "no matching summaries found"}
	}

	var b strings.Builder
	for _, s := range summaries {
		fmt.Fprintf(&b, "[%d] (session %d, %s) %s\n",
			s.ID, s.SessionID, s.CreatedAt.Format("Jan 2 15:04"), s.Content)
	}
	return agent.ToolResult{Content: strings.TrimSpace(b.String()), Details: summaries}
}

// --- fact_promote ---

type factPromoteTool struct {
	db         *db.DB
	embedder   Embedder
	embedModel string
}

func FactPromote(database *db.DB, embedder Embedder, embedModel string) agent.Tool {
	return factPromoteTool{db: database, embedder: embedder, embedModel: embedModel}
}

func (factPromoteTool) Name() string { return "fact_promote" }
func (factPromoteTool) Description() string {
	return "Promote a fact to a permanent memory. Facts are atomic observations extracted during " +
		"summarization; promoting one copies it into the global memories store with a fresh embedding " +
		"so it surfaces in future prompts and semantic search. The original fact row is preserved."
}
func (factPromoteTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"fact_id": prop("integer", "ID of the fact to promote"),
			"tags":    prop("string", "Comma-separated tags to attach to the new memory (optional)"),
		},
		"required": []string{"fact_id"},
	}
}

func (t factPromoteTool) Execute(args map[string]any, _ *agent.ToolContext) agent.ToolResult {
	id := int64(intArg(args, "fact_id", 0))
	if id == 0 {
		return agent.ToolResult{Content: "error: fact_id is required", IsError: true}
	}

	fact, err := t.db.Facts.GetFact(id)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: fact %d not found: %v", id, err), IsError: true}
	}

	tags := formatTags(strArg(args, "tags"))

	// Re-embed at promotion time against the current embed model. The fact
	// may have been embedded with an older model; the memory should be
	// embedded with whatever is current so semantic search stays coherent.
	var embedding []float32
	if t.embedder != nil && t.embedModel != "" {
		if vec, err := t.embedder.Embed(t.embedModel, fact.Content); err == nil {
			embedding = vec
		}
	}

	m, err := t.db.Memories.Add(fact.Content, tags, embedding)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	suffix := ""
	if len(embedding) > 0 {
		suffix = fmt.Sprintf(" (%d-dim embedding)", len(embedding))
	}
	return agent.ToolResult{
		Content: fmt.Sprintf("fact %d promoted to memory %d%s", id, m.ID, suffix),
		Details: m,
	}
}

// --- fact_list ---

type factListTool struct {
	db *db.DB
}

func FactList(database *db.DB) agent.Tool {
	return factListTool{db: database}
}

func (factListTool) Name() string { return "fact_list" }
func (factListTool) Description() string {
	return "List all facts across all sessions. Facts are atomic observations extracted during summarization."
}
func (factListTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t factListTool) Execute(_ map[string]any, _ *agent.ToolContext) agent.ToolResult {
	facts, err := t.db.Facts.All()
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	if len(facts) == 0 {
		return agent.ToolResult{Content: "no facts stored"}
	}
	var b strings.Builder
	for _, f := range facts {
		fmt.Fprintf(&b, "[%d] (session %d) %s\n", f.ID, f.SessionID, f.Content)
	}
	return agent.ToolResult{Content: strings.TrimSpace(b.String()), Details: facts}
}

// --- summary_rewrite ---

type summaryRewriteTool struct {
	db         *db.DB
	embedder   Embedder
	embedModel string
}

func SummaryRewrite(database *db.DB, embedder Embedder, embedModel string) agent.Tool {
	return summaryRewriteTool{db: database, embedder: embedder, embedModel: embedModel}
}

func (summaryRewriteTool) Name() string { return "summary_rewrite" }
func (summaryRewriteTool) Description() string {
	return "Rewrite a session summary's content and re-embed it. Use this during maintenance to consolidate or correct old summaries."
}
func (summaryRewriteTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":      prop("integer", "ID of the summary to rewrite"),
			"content": prop("string", "New content for the summary"),
		},
		"required": []string{"id", "content"},
	}
}

func (t summaryRewriteTool) Execute(args map[string]any, _ *agent.ToolContext) agent.ToolResult {
	id := int64(intArg(args, "id", 0))
	content := strArg(args, "content")
	if id == 0 || content == "" {
		return agent.ToolResult{Content: "error: id and content are required", IsError: true}
	}

	var embedding []float32
	if t.embedder != nil && t.embedModel != "" {
		if vec, err := t.embedder.Embed(t.embedModel, content); err == nil {
			embedding = vec
		}
	}

	if err := t.db.Summaries.Update(id, content, embedding); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	suffix := ""
	if len(embedding) > 0 {
		suffix = fmt.Sprintf(" (%d-dim embedding)", len(embedding))
	}
	return agent.ToolResult{Content: fmt.Sprintf("summary %d rewritten%s", id, suffix)}
}

