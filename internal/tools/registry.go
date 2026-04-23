package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

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

// customTool adapts a db.CustomTool to agent.Tool at runtime.
type customTool struct {
	name           string
	description    string
	parameters     map[string]any
	implementation string
	implType       string
	config         map[string]string
	db             *db.DB
}

func newCustomTool(ct *db.CustomTool, db *db.DB) agent.Tool {
	var params map[string]any
	json.Unmarshal([]byte(ct.Parameters), &params)
	if params == nil {
		params = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	cfg, _ := db.Config.All()
	return &customTool{
		name:           ct.Name,
		description:    ct.Description,
		parameters:     params,
		implementation: ct.Implementation,
		implType:       ct.ImplType,
		config:         cfg,
		db:             db,
	}
}

func (t *customTool) Name() string               { return t.name }
func (t *customTool) Description() string        { return t.description }
func (t *customTool) Parameters() map[string]any { return t.parameters }

func (t *customTool) Execute(args map[string]any, ctx *agent.ToolContext) agent.ToolResult {
	// Build minimal environment
	env := []string{
		fmt.Sprintf("PATH=%s", os.Getenv("PATH")),
		fmt.Sprintf("HOME=%s", os.Getenv("HOME")),
		fmt.Sprintf("TMPDIR=%s", os.Getenv("TMPDIR")),
		fmt.Sprintf("SHELL=%s", os.Getenv("SHELL")),
	}

	// Inject args as environment variables prefixed with CAIRO_ARG_
	for k, v := range args {
		env = append(env, fmt.Sprintf("CAIRO_ARG_%s=%v", strings.ToUpper(k), v))
	}

	// Also pass as JSON in CAIRO_ARGS
	argsJSON, _ := json.Marshal(args)
	env = append(env, fmt.Sprintf("CAIRO_ARGS=%s", argsJSON))

	// Get safe_env_extras config and add those vars
	if extrasConfig, ok := t.config["safe_env_extras"]; ok && extrasConfig != "" {
		for _, extra := range strings.Split(extrasConfig, ",") {
			extra = strings.TrimSpace(extra)
			if extra != "" {
				if val := os.Getenv(extra); val != "" {
					env = append(env, fmt.Sprintf("%s=%s", extra, val))
				}
			}
		}
	}

	cmdCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	switch t.implType {
	case "python":
		cmd = exec.CommandContext(cmdCtx, "python3", "-c", t.implementation)
	default:
		cmd = exec.CommandContext(cmdCtx, "bash", "-c", t.implementation)
	}

	cmd.Dir = ctx.WorkDir
	cmd.Env = env

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	output := out.String()

	if cmdCtx.Err() == context.DeadlineExceeded {
		return agent.ToolResult{Content: output + "\n[timed out]", IsError: true}
	}
	if err != nil {
		return agent.ToolResult{Content: output, IsError: true}
	}
	return agent.ToolResult{Content: output}
}
