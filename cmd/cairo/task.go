package main

import (
	"context"
	"fmt"
	"os"

	"github.com/scotmcc/cairo/internal/agent"
	"github.com/scotmcc/cairo/internal/cli"
	"github.com/scotmcc/cairo/internal/db"
	"github.com/scotmcc/cairo/internal/llm"
	"github.com/scotmcc/cairo/internal/tools"
)

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
