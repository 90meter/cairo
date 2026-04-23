package tools

import (
	"strings"
	"testing"

	"github.com/scotmcc/cairo/internal/agent"
	"github.com/scotmcc/cairo/internal/db"
)

// stubEmbedder implements the Embedder interface with a deterministic fake
// embedding so search-by-similarity tests don't need a live Ollama. Maps
// text → a 2-dim vector seeded by content length, which is enough to make
// cosine similarity order predictable for a handful of test strings.
type stubEmbedder struct{}

func (stubEmbedder) Embed(model, text string) ([]float32, error) {
	return []float32{float32(len(text)), 1.0}, nil
}

func execMemory(t *testing.T, d *db.DB, args map[string]any) agent.ToolResult {
	t.Helper()
	tool := Memory(d, stubEmbedder{}, "stub-embed-model")
	return tool.Execute(args, &agent.ToolContext{DB: d})
}

func TestMemoryTool_AddListDelete_RoundTrip(t *testing.T) {
	d := openTestDB(t)

	// add
	res := execMemory(t, d, map[string]any{"action": "add", "content": "first memory"})
	if res.IsError {
		t.Fatalf("add failed: %s", res.Content)
	}

	// list should see exactly one
	res = execMemory(t, d, map[string]any{"action": "list"})
	if res.IsError {
		t.Fatalf("list errored: %s", res.Content)
	}
	memories, ok := res.Details.([]*db.Memory)
	if !ok {
		t.Fatalf("list details: want []*db.Memory, got %T", res.Details)
	}
	if len(memories) != 1 {
		t.Fatalf("after one add, list length = %d, want 1", len(memories))
	}
	id := memories[0].ID

	// delete
	res = execMemory(t, d, map[string]any{"action": "delete", "id": id})
	if res.IsError {
		t.Fatalf("delete failed: %s", res.Content)
	}

	// list should be empty again
	res = execMemory(t, d, map[string]any{"action": "list"})
	if !strings.Contains(res.Content, "no memories") {
		t.Errorf("expected 'no memories' after delete, got: %s", res.Content)
	}
}

func TestMemoryTool_RequiresAction(t *testing.T) {
	d := openTestDB(t)
	res := execMemory(t, d, map[string]any{})
	if !res.IsError {
		t.Error("missing action should be an error")
	}
	if !strings.Contains(res.Content, "action is required") {
		t.Errorf("error text didn't mention action: %s", res.Content)
	}
}

func TestMemoryTool_UnknownAction(t *testing.T) {
	d := openTestDB(t)
	res := execMemory(t, d, map[string]any{"action": "explode"})
	if !res.IsError {
		t.Error("unknown action should be an error")
	}
	if !strings.Contains(res.Content, "unknown action") {
		t.Errorf("error text should mention 'unknown action': %s", res.Content)
	}
}

func TestMemoryTool_AddValidatesContent(t *testing.T) {
	d := openTestDB(t)
	res := execMemory(t, d, map[string]any{"action": "add"})
	if !res.IsError {
		t.Error("add without content should be an error")
	}
}

func TestMemoryTool_DeleteValidatesID(t *testing.T) {
	d := openTestDB(t)
	res := execMemory(t, d, map[string]any{"action": "delete"})
	if !res.IsError {
		t.Error("delete without id should be an error")
	}
}

func TestMemoryTool_UpdateRewritesContent(t *testing.T) {
	d := openTestDB(t)

	// seed
	res := execMemory(t, d, map[string]any{"action": "add", "content": "original"})
	if res.IsError {
		t.Fatalf("add: %s", res.Content)
	}
	memories, _ := d.Memories.All()
	if len(memories) == 0 {
		t.Fatal("seed add produced no rows")
	}
	id := memories[0].ID

	// update
	res = execMemory(t, d, map[string]any{"action": "update", "id": id, "content": "rewritten"})
	if res.IsError {
		t.Fatalf("update: %s", res.Content)
	}

	// verify via direct DB read — bypasses any list formatting
	got, err := d.Memories.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Content != "rewritten" {
		t.Errorf("update didn't rewrite; got %q", got.Content)
	}
}

func TestMemoryTool_SearchReturnsMatches(t *testing.T) {
	d := openTestDB(t)

	// Two memories of different lengths so the stub embedder gives them
	// different vectors; the query's vector is seeded by its own length,
	// and cosine puts the closest-length entry first.
	_ = execMemory(t, d, map[string]any{"action": "add", "content": "short"})
	_ = execMemory(t, d, map[string]any{"action": "add", "content": "a much much longer memory about nothing in particular"})

	res := execMemory(t, d, map[string]any{"action": "search", "query": "ss"})
	if res.IsError {
		t.Fatalf("search errored: %s", res.Content)
	}
	memories, ok := res.Details.([]*db.Memory)
	if !ok {
		t.Fatalf("search details: want []*db.Memory, got %T", res.Details)
	}
	if len(memories) == 0 {
		t.Fatal("search returned no memories despite seeded content")
	}
}
