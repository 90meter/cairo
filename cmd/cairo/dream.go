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
