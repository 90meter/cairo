package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/scotmcc/cairo/internal/agent"
	"github.com/scotmcc/cairo/internal/db"
	"github.com/scotmcc/cairo/internal/llm"
)

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
