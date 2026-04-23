package tools

import (
	"path/filepath"
	"testing"

	"github.com/scotmcc/cairo/internal/agent"
	"github.com/scotmcc/cairo/internal/db"
)

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := db.OpenAt(path)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// fakeTool is a minimal agent.Tool for FilterByAllowlist tests.
type fakeTool struct{ name string }

func (f fakeTool) Name() string                     { return f.name }
func (f fakeTool) Description() string              { return "fake" }
func (f fakeTool) Parameters() map[string]any       { return map[string]any{} }
func (f fakeTool) Execute(map[string]any, *agent.ToolContext) agent.ToolResult {
	return agent.ToolResult{}
}

func TestFilterByAllowlist_EmptyAllowlistIsUnrestricted(t *testing.T) {
	tools := []agent.Tool{fakeTool{"a"}, fakeTool{"b"}, fakeTool{"c"}}

	got := FilterByAllowlist(tools, nil)
	if len(got) != 3 {
		t.Errorf("nil allowlist: expected unrestricted (3 tools), got %d", len(got))
	}

	got = FilterByAllowlist(tools, []string{})
	if len(got) != 3 {
		t.Errorf("empty allowlist: expected unrestricted (3 tools), got %d", len(got))
	}
}

func TestFilterByAllowlist_Intersects(t *testing.T) {
	tools := []agent.Tool{fakeTool{"a"}, fakeTool{"b"}, fakeTool{"c"}}

	got := FilterByAllowlist(tools, []string{"a", "c"})
	if len(got) != 2 {
		t.Fatalf("expected 2 tools after filter, got %d", len(got))
	}
	names := []string{got[0].Name(), got[1].Name()}
	if names[0] != "a" || names[1] != "c" {
		t.Errorf("expected order [a c], got %v", names)
	}
}

func TestFilterByAllowlist_UnknownNamesIgnored(t *testing.T) {
	tools := []agent.Tool{fakeTool{"a"}, fakeTool{"b"}}

	got := FilterByAllowlist(tools, []string{"a", "does_not_exist"})
	if len(got) != 1 {
		t.Errorf("expected 1 matching tool, got %d", len(got))
	}
	if got[0].Name() != "a" {
		t.Errorf("expected 'a', got %q", got[0].Name())
	}
}

// TestDefault_AllNamesUnique is a regression guard: after the consolidation
// pass, every built-in tool must have a unique Name(). Two tools with the
// same name would be an easy mistake and would silently shadow each other
// in the loop's toolMap lookup.
func TestDefault_AllNamesUnique(t *testing.T) {
	d := openTestDB(t)

	tools := Default(d, nil, "")
	seen := make(map[string]bool, len(tools))
	for _, tool := range tools {
		name := tool.Name()
		if seen[name] {
			t.Errorf("duplicate tool name in Default(): %q", name)
		}
		seen[name] = true
	}
}

// TestDefault_RespectsSeededRoleAllowlists asserts that every tool name
// referenced in a seeded role's allowlist actually exists in Default().
// This catches the drift class where a seed string references a tool that
// got renamed or deleted — the filter would silently drop it.
func TestDefault_RespectsSeededRoleAllowlists(t *testing.T) {
	d := openTestDB(t)

	tools := Default(d, nil, "")
	known := make(map[string]bool, len(tools))
	for _, tool := range tools {
		known[tool.Name()] = true
	}

	roles, err := d.Roles.List()
	if err != nil {
		t.Fatalf("list roles: %v", err)
	}
	for _, r := range roles {
		allowed, err := d.Roles.AllowedTools(r.Name)
		if err != nil {
			t.Fatalf("AllowedTools(%s): %v", r.Name, err)
		}
		for _, name := range allowed {
			if !known[name] {
				t.Errorf("role %q lists unknown tool %q (not in Default())", r.Name, name)
			}
		}
	}
}
