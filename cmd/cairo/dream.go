package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

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

	// Snapshot the live DB before any maintenance begins. If the backup fails,
	// abort — do not run the dream agent against a DB we couldn't checkpoint.
	backupDir := filepath.Join(db.DefaultDataDir(), "backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return fmt.Errorf("create backup dir: %w", err)
	}
	backupPath := filepath.Join(backupDir, time.Now().Format("dream-2006-01-02-15-04.cairo"))

	src := cairoDBPath()
	tmp, err := os.CreateTemp("", "cairo-dream-backup-*.db")
	if err != nil {
		return fmt.Errorf("create backup temp: %w", err)
	}
	tmpPath := tmp.Name()
	tmp.Close()
	os.Remove(tmpPath) // vacuumInto refuses an existing target

	if err := vacuumInto(src, tmpPath); err != nil {
		return fmt.Errorf("backup vacuum: %w", err)
	}
	defer os.Remove(tmpPath)

	counts, err := countEntities(tmpPath)
	if err != nil {
		return fmt.Errorf("backup count: %w", err)
	}
	manifest := bundleManifest{
		Version:         manifestVersion,
		ExportedAt:      time.Now().UTC(),
		IncludesHistory: true,
		Counts:          counts,
	}
	if err := writeBundle(backupPath, tmpPath, manifest); err != nil {
		return fmt.Errorf("write backup bundle: %w", err)
	}
	fmt.Printf("backup saved to %s\n", backupPath)

	ollamaURL := resolveOllamaURL(database)
	embedModel, _ := database.Config.Get("embed_model")

	llmClient := llm.New(ollamaURL)
	if err := llmClient.Ping(); err != nil {
		return fmt.Errorf("ollama unreachable: %v", err)
	}

	cwd, _ := os.Getwd()
	session, err := database.Sessions.Create(db.RoleDream, cwd, db.RoleDream)
	if err != nil {
		return fmt.Errorf("create session: %v", err)
	}

	model, err := db.ResolveModel(database, db.RoleDream, "qwen3.6:35b-a3b-mlx-bf16")
	if err != nil {
		return fmt.Errorf("resolve model: %v", err)
	}

	builtins := tools.Default(database, llmClient, embedModel)
	if allowed, _ := database.Roles.AllowedTools(db.RoleDream); len(allowed) > 0 {
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
