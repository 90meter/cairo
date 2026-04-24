package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/scotmcc/cairo/internal/agent"
	"github.com/scotmcc/cairo/internal/cli"
	"github.com/scotmcc/cairo/internal/db"
	"github.com/scotmcc/cairo/internal/llm"
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
		sessionRole = "thinking_partner"
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

// runTask is the background worker path: load task, create session in task's role,
// run the task description through the agent, store result, mark done.
func runTask(database *db.DB, taskID int64, background bool) error {
	task, err := database.Tasks.Get(taskID)
	if err != nil {
		return fmt.Errorf("get task: %w", err)
	}

	ollamaURL := resolveOllamaURL(database)
	embedModel, _ := database.Config.Get("embed_model")

	llmClient := llm.New(ollamaURL)
	if err := llmClient.Ping(); err != nil {
		database.Tasks.SetStatus(taskID, "failed")
		database.Tasks.SetResult(taskID, fmt.Sprintf("ollama unreachable: %v", err))
		return err
	}

	// create a dedicated session for this task
	cwd, _ := os.Getwd()
	session, err := database.Sessions.Create(
		fmt.Sprintf("task-%d", taskID),
		cwd,
		task.AssignedRole,
	)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}

	// Use the role's configured model (e.g. coder → qwen35-35b-coding)
	model, err := db.ResolveModel(database, task.AssignedRole, "qwen3.6:35b-a3b-mlx-bf16")
	if err != nil {
		return fmt.Errorf("resolve model: %w", err)
	}

	builtins := tools.Default(database, llmClient, embedModel)
	if allowed, _ := database.Roles.AllowedTools(task.AssignedRole); len(allowed) > 0 {
		builtins = tools.FilterByAllowlist(builtins, allowed)
	}
	custom, _ := tools.LoadCustom(database)
	allTools := append(builtins, custom...)

	a, err := agent.New(agent.Config{
		DB:      database,
		LLM:     llmClient,
		Model:   model,
		Session: session,
		Tools:   allTools,
	})
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}

	// set up renderer — logs to file in background mode, stdout otherwise
	var stopRenderer func()
	if background && task.LogPath != "" {
		logFile, err := cli.OpenTaskLog(task.LogPath)
		if err == nil {
			defer logFile.Close()
			stopRenderer = cli.BackgroundRenderer(a.Bus(), logFile)
		}
	}
	if stopRenderer == nil {
		stopRenderer = cli.BackgroundRenderer(a.Bus(), os.Stdout)
	}
	defer stopRenderer()

	// collect artifacts by observing tool events on the bus
	// tools are sequential so we track last-start to correlate with end
	artifactCh, stopArtifacts := a.Bus().Subscribe()
	defer stopArtifacts()
	go collectArtifacts(artifactCh, database, taskID)

	// run the task
	runErr := a.Prompt(context.Background(), task.Description)

	// Capture result from in-memory history — avoids the stale-read that
	// happened when reading LastAssistantMessage from DB after a partial
	// failure (R2.2.9). The in-memory view is what the loop just emitted.
	result := a.LastAssistantText()
	if result == "" && runErr != nil {
		result = fmt.Sprintf("error: %v", runErr)
	}

	if runErr != nil {
		database.Tasks.SetStatus(taskID, "failed")
	} else {
		database.Tasks.SetStatus(taskID, "done")
	}
	database.Tasks.SetResult(taskID, result)

	return runErr
}

// runDream runs a headless maintenance session in the dream role.
// It reviews and consolidates memories, facts, and summaries, then exits.
func runDream(_ []string) error {
	database, err := db.Open()
	if err != nil {
		return fmt.Errorf("open db: %v", err)
	}
	defer database.Close()

	ollamaURL := resolveOllamaURL(database)
	embedModel, _ := database.Config.Get("embed_model")

	llmClient := llm.New(ollamaURL)
	if err := llmClient.Ping(); err != nil {
		return fmt.Errorf("ollama unreachable: %v", err)
	}

	cwd, _ := os.Getwd()
	session, err := database.Sessions.Create("dream", cwd, "dream")
	if err != nil {
		return fmt.Errorf("create session: %v", err)
	}

	model, err := db.ResolveModel(database, "dream", "qwen3.6:35b-a3b-mlx-bf16")
	if err != nil {
		return fmt.Errorf("resolve model: %v", err)
	}

	builtins := tools.Default(database, llmClient, embedModel)
	if allowed, _ := database.Roles.AllowedTools("dream"); len(allowed) > 0 {
		builtins = tools.FilterByAllowlist(builtins, allowed)
	}
	custom, _ := tools.LoadCustom(database)
	allTools := append(builtins, custom...)

	a, err := agent.New(agent.Config{
		DB:      database,
		LLM:     llmClient,
		Model:   model,
		Session: session,
		Tools:   allTools,
	})
	if err != nil {
		return fmt.Errorf("create agent: %v", err)
	}

	stopRenderer := cli.BackgroundRenderer(a.Bus(), os.Stdout)
	defer stopRenderer()

	return a.Prompt(context.Background(), "Begin your maintenance cycle. Review and consolidate all memories, facts, and summaries.")
}

// collectArtifacts observes the bus and records write/edit/bash results
// as task artifacts. Runs in its own goroutine until the channel closes.
func collectArtifacts(ch <-chan agent.Event, database *db.DB, taskID int64) {
	var pendingName string
	var pendingPath string

	for event := range ch {
		switch event.Type {
		case agent.EventToolStart:
			p := event.Payload.(agent.PayloadToolStart)
			pendingName = p.Name
			pendingPath = ""
			if p.Name == "write" || p.Name == "edit" {
				if path, ok := p.Args["path"].(string); ok {
					pendingPath = path
				}
			}

		case agent.EventToolEnd:
			p := event.Payload.(agent.PayloadToolEnd)
			if p.IsError {
				pendingName = ""
				continue
			}
			switch pendingName {
			case "write", "edit":
				if pendingPath != "" {
					database.TaskArtifacts.Add(taskID, "file", pendingPath, "", pendingName)
				}
			case "bash":
				if p.Result != "" {
					database.TaskArtifacts.Add(taskID, "output", "", p.Result, "bash")
				}
			}
			pendingName = ""
			pendingPath = ""
		}
	}
}

func resolveSession(database *db.DB, llmClient *llm.Client, wg *sync.WaitGroup, forceNew bool, id int64, name, role string) (*db.Session, error) {
	cwd, _ := os.Getwd()

	if id != 0 {
		return database.Sessions.Get(id)
	}

	// When starting a new session (explicit or because none exists),
	// drain any unsummarized messages from the previous session first.
	// This ensures context is never lost at session boundaries.
	if forceNew {
		prev, _ := database.Sessions.Latest()
		if prev != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				agent.SummarizeAll(database, llmClient, prev.ID)
			}()
		}
		return database.Sessions.Create(name, cwd, role)
	}

	s, err := database.Sessions.Latest()
	if err != nil {
		return nil, err
	}
	if s == nil {
		return database.Sessions.Create(name, cwd, role)
	}
	return s, nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "cairo: "+format+"\n", args...)
	os.Exit(1)
}

// resolveOllamaURL returns the Ollama URL, preferring the OLLAMA_URL env var
// over the stored DB config value. Env wins so headless/CI/Docker setups can
// override without mutating the DB.
func resolveOllamaURL(database *db.DB) string {
	if env := strings.TrimSpace(os.Getenv("OLLAMA_URL")); env != "" {
		return env
	}
	url, _ := database.Config.Get("ollama_url")
	return url
}

// connectOllama pings Ollama and, on failure, prompts the user for a new URL
// on stdin. URLs entered at the prompt are persisted to the config table so
// they stick across runs. Loops until ping succeeds or stdin closes.
func connectOllama(database *db.DB, url string) (*llm.Client, error) {
	reader := bufio.NewReader(os.Stdin)
	for {
		client := llm.New(url)
		if err := client.Ping(); err == nil {
			return client, nil
		} else {
			fmt.Fprintf(os.Stderr, "cairo: ollama unreachable at %s: %v\n", url, err)
		}
		fmt.Fprint(os.Stderr, "Enter Ollama URL (blank to retry, Ctrl+C to quit): ")
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		line = strings.TrimSpace(line)
		if line != "" {
			url = line
			if err := database.Config.Set("ollama_url", url); err != nil {
				fmt.Fprintf(os.Stderr, "cairo: warning: failed to save URL to config: %v\n", err)
			}
		}
	}
}
