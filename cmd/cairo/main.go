package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/scotmcc/cairo/internal/agent"
	"github.com/scotmcc/cairo/internal/cli"
	"github.com/scotmcc/cairo/internal/db"
	"github.com/scotmcc/cairo/internal/tools"
	"github.com/scotmcc/cairo/internal/tui"
)

func main() {
	// Subcommand dispatch — must happen before flag.Parse() so the subcommand
	// name isn't consumed as a positional arg. Each subcommand owns its own
	// flag parsing.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "export":
			if err := runExport(os.Args[2:]); err != nil {
				fatalf("export: %v", err)
			}
			return
		case "import":
			if err := runImport(os.Args[2:]); err != nil {
				fatalf("import: %v", err)
			}
			return
		case "diff":
			if err := runDiff(os.Args[2:]); err != nil {
				fatalf("diff: %v", err)
			}
			return
		case "dream":
			if err := runDream(os.Args[2:]); err != nil {
				fatalf("dream: %v", err)
			}
			return
		}
	}

	var (
		newSession  = flag.Bool("new", false, "start a new session")
		sessionID   = flag.Int64("session", 0, "resume a specific session by ID")
		sessionName = flag.String("name", "", "name for a new session")
		roleFlag    = flag.String("role", "", "role for a new session (default: thinking_partner)")
		taskFlag    = flag.Int64("task", 0, "run as a background task worker for this task ID")
		background  = flag.Bool("background", false, "background mode: plain log output, no banner")
		tuiFlag     = flag.Bool("tui", false, "use the Bubble Tea TUI instead of the line CLI")
	)
	flag.Parse()

	singleMessage := strings.Join(flag.Args(), " ")

	// --- open DB ---
	database, err := db.Open()
	if err != nil {
		fatalf("open db: %v", err)
	}
	defer database.Close()
	var bgWg sync.WaitGroup
	defer bgWg.Wait()

	// --- background task mode ---
	// When -task is set, we are a subprocess worker. Read the task from the DB,
	// run its description as the instruction, write the result back, then exit.
	if *taskFlag != 0 {
		if err := runTask(database, *taskFlag, *background); err != nil {
			fatalf("task %d failed: %v", *taskFlag, err)
		}
		return
	}

	// --- interactive / single-message mode ---
	ollamaURL := resolveOllamaURL(database)
	embedModel, _ := database.Config.Get("embed_model")

	llmClient, err := connectOllama(database, ollamaURL)
	if err != nil {
		fatalf("ollama: %v", err)
	}

	if err := runFirstRunWizard(database, llmClient); err != nil {
		fatalf("setup: %v", err)
	}

	sessionRole := *roleFlag
	if sessionRole == "" {
		sessionRole = db.RoleThinkingPartner
	}
	session, err := resolveSession(database, llmClient, &bgWg, *newSession, *sessionID, *sessionName, sessionRole)
	if err != nil {
		fatalf("session: %v", err)
	}

	// Resolve model: role model → global config → fallback
	model, err := db.ResolveModel(database, session.Role, "qwen3.6:35b-a3b-mlx-bf16")
	if err != nil {
		fatalf("resolve model: %v", err)
	}

	builtins := tools.Default(database, llmClient, embedModel)
	// Role-scoped tool filtering. Empty allowlist = unrestricted.
	// Custom tools are always available (they're the AI's own work).
	if allowed, _ := database.Roles.AllowedTools(session.Role); len(allowed) > 0 {
		builtins = tools.FilterByAllowlist(builtins, allowed)
	}
	custom, err := tools.LoadCustom(database)
	if err != nil {
		fatalf("load custom tools: %v", err)
	}
	allTools := append(builtins, custom...)

	a, err := agent.New(agent.Config{
		DB:      database,
		LLM:     llmClient,
		Model:   model,
		Session: session,
		Tools:   allTools,
	})
	if err != nil {
		fatalf("create agent: %v", err)
	}

	if singleMessage != "" {
		if err := cli.RunOnce(a, singleMessage); err != nil {
			fatalf("run: %v", err)
		}
		return
	}

	if *tuiFlag {
		if err := tui.Run(a, database, session); err != nil {
			fatalf("tui: %v", err)
		}
		return
	}

	if err := cli.Run(a, database, session); err != nil {
		fatalf("cli: %v", err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "cairo: "+format+"\n", args...)
	os.Exit(1)
}
