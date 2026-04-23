package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/scotmcc/cairo/internal/db"
	"github.com/scotmcc/cairo/internal/llm"
)

// runFirstRunWizard performs first-run "base config" setup before the CLI/TUI
// launches. Triggered when config.setup_complete != "true". Each step is
// skipped if its value is already valid, so re-runs are short and the wizard
// disappears once everything is set.
func runFirstRunWizard(database *db.DB, client *llm.Client) error {
	if configValue(database, "setup_complete") == "true" {
		return nil
	}

	models, err := client.ListModels()
	if err != nil {
		return fmt.Errorf("list ollama models: %w", err)
	}
	if len(models) == 0 {
		fmt.Fprintln(os.Stderr, "cairo: no models installed on Ollama. Run 'ollama pull <model>' first (e.g. 'ollama pull devstral-24b:latest'), then relaunch cairo.")
		os.Exit(1)
	}

	needModel := !contains(models, configValue(database, "model"))
	needEmbed := !contains(models, configValue(database, "embed_model"))
	needUser := configValue(database, "user_name") == ""

	if !needModel && !needEmbed && !needUser {
		// Nothing to ask — silently mark complete so we never re-enter.
		return database.Config.Set("setup_complete", "true")
	}

	reader := bufio.NewReader(os.Stdin)

	fmt.Println()
	fmt.Println("─── First-run setup ───")
	fmt.Println("Quick base config before you meet Selene.")

	if needModel {
		choice, err := pickFromList(reader, "Default chat model", models)
		if err != nil {
			return wizardErr(err)
		}
		if err := database.Config.Set("model", choice); err != nil {
			return fmt.Errorf("save model: %w", err)
		}
	}

	if needEmbed {
		display := filterEmbedModels(models)
		if len(display) == 0 {
			fmt.Println("\n(no obvious embedding models found — pick the right one for embeddings)")
			display = models
		}
		choice, err := pickFromList(reader, "Embedding model", display)
		if err != nil {
			return wizardErr(err)
		}
		if err := database.Config.Set("embed_model", choice); err != nil {
			return fmt.Errorf("save embed_model: %w", err)
		}
	}

	if needUser {
		fmt.Print("\nWhat should Selene call you? (blank to skip): ")
		line, err := reader.ReadString('\n')
		if err != nil {
			return wizardErr(err)
		}
		if name := strings.TrimSpace(line); name != "" {
			if err := database.Config.Set("user_name", name); err != nil {
				return fmt.Errorf("save user_name: %w", err)
			}
		}
	}

	currentAI := configValue(database, "ai_name")
	if currentAI == "" {
		currentAI = "Selene"
	}
	fmt.Printf("\nCairo's identity name [%s] (Enter to keep): ", currentAI)
	line, err := reader.ReadString('\n')
	if err != nil {
		return wizardErr(err)
	}
	if name := strings.TrimSpace(line); name != "" && name != currentAI {
		if err := database.Config.Set("ai_name", name); err != nil {
			return fmt.Errorf("save ai_name: %w", err)
		}
	}

	if err := database.Config.Set("setup_complete", "true"); err != nil {
		return fmt.Errorf("save setup_complete: %w", err)
	}

	fmt.Println()
	fmt.Println("Setup done. Once you're in, run /init to introduce yourself")
	fmt.Println("to Selene and have her learn about your project.")
	fmt.Println()

	return nil
}

func configValue(database *db.DB, key string) string {
	v, _ := database.Config.Get(key)
	return v
}

func contains(haystack []string, needle string) bool {
	if needle == "" {
		return false
	}
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func filterEmbedModels(all []string) []string {
	var out []string
	for _, m := range all {
		lower := strings.ToLower(m)
		if strings.Contains(lower, "embed") || strings.Contains(lower, "nomic") {
			out = append(out, m)
		}
	}
	return out
}

func pickFromList(reader *bufio.Reader, label string, items []string) (string, error) {
	for {
		fmt.Printf("\n%s — pick one:\n", label)
		for i, item := range items {
			fmt.Printf("  %d) %s\n", i+1, item)
		}
		fmt.Print("Enter number, or type an exact model name: ")
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if n, err := strconv.Atoi(line); err == nil {
			if n >= 1 && n <= len(items) {
				return items[n-1], nil
			}
			fmt.Printf("Number out of range (1-%d).\n", len(items))
			continue
		}
		return line, nil
	}
}

// wizardErr wraps stdin EOF as a friendly aborted-setup exit, otherwise
// returns the error to be reported by the caller.
func wizardErr(err error) error {
	if errors.Is(err, io.EOF) {
		fmt.Fprintln(os.Stderr, "\nSetup aborted. Rerun cairo when ready.")
		os.Exit(0)
	}
	return err
}
