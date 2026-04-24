package tools

import (
	"github.com/scotmcc/cairo/internal/agent"
	"github.com/scotmcc/cairo/internal/db"
)

// Default returns the full set of built-in tools wired to the given DB.
// embedder and embedModel are optional — pass nil/empty to skip embedding.
//
// tool_list_builtin is appended last and receives the derived name list so
// it stays in sync automatically when tools are added or removed above.
func Default(database *db.DB, embedder Embedder, embedModel string) []agent.Tool {
	tools := []agent.Tool{
			// filesystem
		Read(),
		Write(),
		Edit(),
		Bash(),
		Grep(),
		Find(),
		Ls(),
			// memory (consolidated — add/list/search/update/delete)
		Memory(database, embedder, embedModel),
			// summaries + facts
		SummarySearch(database, embedder, embedModel),
		FactPromote(database, embedder, embedModel),
		FactList(database),
		SummaryRewrite(database, embedder, embedModel),
			// custom tools (consolidated — list/create/delete)
		CustomTool(database),
			// skills (consolidated — list/read/create/update/delete)
		Skill(database),
			// notes (consolidated — create/list/read/update/delete)
		Note(database),
			// roles (consolidated — list/model_set)
		Role(database),
			// jobs + tasks (consolidated — create/list/update/delete; task also has ready, artifacts)
		Job(database),
		Task(database),
			// background agents (consolidated — spawn/wait/log)
		Agent(database),
			// sessions (consolidated — list/delete)
		Session(database),
			// prompt parts (consolidated — add/list/update/delete/toggle)
		PromptPart(database),
			// soul (consolidated — get/set)
		Soul(database),
			// config (consolidated — get/set/list; absorbs unsafe_mode_enable/disable via set)
		Config(database),
			// web tools
		Search(database),
		Fetch(),
			// self-inspection
		PromptShow(),
	}

	names := make([]string, 0, len(tools)+1)
	for _, t := range tools {
		names = append(names, t.Name())
	}
	names = append(names, "tool_list_builtin")
	tools = append(tools, ToolListBuiltin(names))
	return tools
}

// FilterByAllowlist returns the subset of tools whose names appear in allowed.
// An empty (nil) allowlist means unrestricted — all tools pass through.
// Unknown names in allowed are silently ignored; filtering is intersective.
func FilterByAllowlist(tools []agent.Tool, allowed []string) []agent.Tool {
	if len(allowed) == 0 {
		return tools
	}
	allow := make(map[string]struct{}, len(allowed))
	for _, n := range allowed {
		allow[n] = struct{}{}
	}
	out := make([]agent.Tool, 0, len(tools))
	for _, t := range tools {
		if _, ok := allow[t.Name()]; ok {
			out = append(out, t)
		}
	}
	return out
}

// LoadCustom loads enabled custom tools from the DB and returns them as agent.Tools.
// Each custom tool wraps its implementation script as an executable.
func LoadCustom(database *db.DB) ([]agent.Tool, error) {
	customs, err := database.Tools.Enabled()
	if err != nil {
		return nil, err
	}
	out := make([]agent.Tool, 0, len(customs))
	for _, ct := range customs {
		out = append(out, newCustomTool(ct, database))
	}
	return out, nil
}

