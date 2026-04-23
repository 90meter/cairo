package agent

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/scotmcc/cairo/internal/db"
	"github.com/scotmcc/cairo/internal/llm"
)

// templateRe matches {{name}} where name is a simple identifier (letters,
// digits, underscore, starting with letter or underscore). Deliberately
// narrow so stray `{{` sequences in user content don't accidentally match.
var templateRe = regexp.MustCompile(`\{\{([a-zA-Z_][a-zA-Z0-9_]*)\}\}`)

// applyTemplates substitutes {{key}} occurrences in s with the matching value
// from vars. Unknown or empty-value keys are replaced with the empty string —
// the intent is that missing identity values disappear gracefully, and the
// init skill is responsible for capturing them conversationally.
func applyTemplates(s string, vars map[string]string) string {
	return templateRe.ReplaceAllStringFunc(s, func(match string) string {
		key := match[2 : len(match)-2]
		return vars[key]
	})
}

// BuildSystemPrompt assembles the system prompt fresh from DB state.
// It is called at the start of every turn so changes (soul updates, new memories,
// new prompt parts) take effect immediately without restarting the session.
//
// Structure:
//  1. Base parts (trigger IS NULL), ordered by load_order
//  2. Environment hints (wsh, VS Code, shell)
//  3. Soul — the AI's self-maintained persona (from config.soul_prompt)
//  4. Role addendum (trigger = "role:<roleName>")
//  5. Tool addenda (trigger = "tool:<toolName>") for each active tool
//  6. Custom tool prompt_addendum fields
//  7. Recent summaries
//  8. Recent memories (capped)
//  9. Date + cwd stamp
// 10. Template substitution ({{key}} → config values)
func BuildSystemPrompt(database *db.DB, sessionID int64, roleName, cwd string, tools []Tool) (llm.Message, error) {
	var b strings.Builder

	// 0. Detect environment.
	env := detectEnvironment()

	// 1. base parts
	base, err := database.Prompts.Base()
	if err != nil {
		return llm.Message{}, fmt.Errorf("prompt base: %w", err)
	}
	for _, p := range base {
		b.WriteString(p.Content)
		b.WriteString("\n\n")
	}

	// 2. Environment hints — detect wsh, VS Code, and shell, append directly.
	if env.IsWsh {
		b.WriteString("You are running within Wave Terminal (wsh). Use `wsh view <file>`, `wsh edit <file>`, or `wsh browser <url>` to collaborate. Run `wsh -h` to learn more about available commands.\n\n")
	}

	if env.IsVsCode {
		b.WriteString("You are running within VS Code. Use `code -r <file>` to open files, `code <dir>` to open folders. Run `code --help` to learn more.\n\n")
	}

	if env.Shell != "" {
		b.WriteString("You are running in " + env.Shell + ".\n\n")
	}

	// 3. soul — self-maintained persona
	soul, _ := database.Config.Get("soul_prompt")
	if soul != "" {
		b.WriteString("## My character\n\n")
		b.WriteString(soul)
		b.WriteString("\n\n")
	}

	// 3. role addendum
	if roleName != "" {
		parts, err := database.Prompts.ForTrigger("role:" + roleName)
		if err != nil {
			return llm.Message{}, fmt.Errorf("prompt role: %w", err)
		}
		for _, p := range parts {
			b.WriteString(p.Content)
			b.WriteString("\n\n")
		}
	}

	// 4. tool addenda (built-in triggers)
	seen := make(map[string]bool)
	for _, t := range tools {
		if seen[t.Name()] {
			continue
		}
		seen[t.Name()] = true
		parts, err := database.Prompts.ForTrigger("tool:" + t.Name())
		if err != nil {
			return llm.Message{}, fmt.Errorf("prompt tool %s: %w", t.Name(), err)
		}
		for _, p := range parts {
			b.WriteString(p.Content)
			b.WriteString("\n\n")
		}
	}

	// 5. custom tool addenda stored on the tool row itself
	customTools, err := database.Tools.Enabled()
	if err != nil {
		return llm.Message{}, fmt.Errorf("prompt custom tools: %w", err)
	}
	for _, ct := range customTools {
		if ct.PromptAddendum != "" {
			b.WriteString(ct.PromptAddendum)
			b.WriteString("\n\n")
		}
	}

	// 6. summaries — recent context from this and prior sessions
	contextCount := 4
	if cstr, _ := database.Config.Get("summary_context"); cstr != "" {
		if n, err := strconv.Atoi(cstr); err == nil && n > 0 {
			contextCount = n
		}
	}
	summaries, err := database.Summaries.LatestForSession(sessionID, contextCount)
	if err == nil && len(summaries) > 0 {
		b.WriteString("## Conversation context\n\n")
		for _, s := range summaries {
			fmt.Fprintf(&b, "[%s] %s\n\n", s.CreatedAt.Format("Jan 2 15:04"), s.Content)
		}
	}

	// 7. memories — capped to avoid prompt bloat
	limit := 15
	if lstr, _ := database.Config.Get("memory_limit"); lstr != "" {
		if n, err := strconv.Atoi(lstr); err == nil && n > 0 {
			limit = n
		}
	}

	memories, err := database.Memories.RecentContent(limit)
	if err != nil {
		return llm.Message{}, fmt.Errorf("prompt memories: %w", err)
	}
	if len(memories) > 0 {
		b.WriteString("## Memories\n\n")
		for _, c := range memories {
			b.WriteString("- ")
			b.WriteString(c)
			b.WriteByte('\n')
		}
		total, _ := database.Memories.Count()
		if overflow := total - len(memories); overflow > 0 {
			fmt.Fprintf(&b, "(%d more memories available via memory action=search)\n", overflow)
		}
		b.WriteByte('\n')
	}

	// 8. stamp
	b.WriteString(fmt.Sprintf("Date: %s\nWorking directory: %s\n",
		time.Now().Format("2006-01-02 15:04 MST"), cwd))

	// 9. template substitution — every config key is a {{key}} template var.
	// Runs once over the assembled prompt so it covers every section: base,
	// role, tool addenda, soul, summaries, memories, stamp. Cheap enough to
	// do per turn; keeps the source content clean and the mapping central.
	vars, _ := database.Config.All()
	return llm.Message{Role: "system", Content: applyTemplates(b.String(), vars)}, nil
}
