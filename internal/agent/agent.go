package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/scotmcc/cairo/internal/db"
	"github.com/scotmcc/cairo/internal/llm"
	"github.com/scotmcc/cairo/internal/providers"
)

// Agent is the stateful wrapper around the agent loop.
type Agent struct {
	db       *db.DB
	llm      *llm.Client
	model    string
	session  *db.Session
	tools    []Tool
	bus      *Bus
	registry *providers.Registry

	lastActiveBeforeTurn time.Time // captured before Touch() each turn — used for temporal awareness

	mu          sync.Mutex
	history     []llm.Message // user/assistant/tool only — system prompt is NOT stored here
	streaming   bool
	steerQueue  []llm.Message
	followQueue []llm.Message
	wg          sync.WaitGroup // tracks background goroutines (summarizer)
}

// Config is passed to New.
type Config struct {
	DB       *db.DB
	LLM      *llm.Client
	Model    string
	Session  *db.Session
	Tools    []Tool
	Registry *providers.Registry // nil falls back to providers.Default()
	// SystemPrompt removed — the prompt is now rebuilt dynamically each turn
}

// New creates an Agent and loads the session's message history from the DB.
func New(cfg Config) (*Agent, error) {
	reg := cfg.Registry
	if reg == nil {
		reg = providers.Default()
	}
	a := &Agent{
		db:       cfg.DB,
		llm:      cfg.LLM,
		model:    cfg.Model,
		session:  cfg.Session,
		tools:    cfg.Tools,
		bus:      &Bus{},
		registry: reg,
	}
	if err := a.loadHistory(); err != nil {
		return nil, err
	}
	return a, nil
}

// Bus returns the event bus. Subscribe before calling Prompt.
func (a *Agent) Bus() *Bus { return a.bus }

// Prompt submits a user message and runs the agent loop to completion.
func (a *Agent) Prompt(ctx context.Context, text string) error {
	a.mu.Lock()
	if a.streaming {
		a.steerQueue = append(a.steerQueue, llm.Message{Role: "user", Content: text})
		a.mu.Unlock()
		return nil
	}
	a.streaming = true
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.streaming = false
		a.mu.Unlock()
		// After the turn completes, check if we need to summarize.
		// Tracked via WaitGroup so Close() can drain it before process exit.
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			Summarize(a.db, a.llm, a.session.ID)
		}()
	}()

	// Drain the background inbox: any tasks that completed while we were idle
	// get surfaced once, as a synthetic "system"-role message. This converts
	// async workers from "user must remember to poll" into "my other threads
	// tell me what they did." Stored in messages so the log reads naturally
	// in a transcript replay; marked reported so each event shows at most once.
	if note := a.drainBackgroundInbox(); note != "" {
		a.db.Messages.Add(a.session.ID, "system", note, "", "", "")
		a.history = append(a.history, llm.Message{Role: "system", Content: note})
	}

	if _, err := a.db.Messages.Add(a.session.ID, "user", text, "", "", ""); err != nil {
		return err
	}
	a.history = append(a.history, llm.Message{Role: "user", Content: text})
	a.lastActiveBeforeTurn = a.session.LastActive
	_ = a.db.Sessions.Touch(a.session.ID)

	return runLoop(ctx, loopConfig{
		model:         a.model,
		history:       a.history,
		tools:         a.tools,
		llm:           a.llm,
		bus:           a.bus,
		db:            a.db,
		session:       a.session,
		registry:      a.registry,
		persist:       a.persistMessage,
		workDir:       a.session.CWD,
		buildPrompt:   a.buildSystemPrompt,
		drainSteering: a.drainSteering,
		drainFollowUp: a.drainFollowUp,
	})
}

// Steer injects a message at the next turn boundary; if idle, runs it immediately.
func (a *Agent) Steer(ctx context.Context, text string) error {
	a.mu.Lock()
	if a.streaming {
		a.steerQueue = append(a.steerQueue, llm.Message{Role: "user", Content: text})
		a.mu.Unlock()
		return nil
	}
	a.mu.Unlock()
	return a.Prompt(ctx, text)
}

// FollowUp queues a message to run only after the agent is fully idle.
func (a *Agent) FollowUp(text string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.followQueue = append(a.followQueue, llm.Message{Role: "user", Content: text})
}

// Close waits for all background goroutines (summarizer etc.) to finish.
// Call this before the process exits.
func (a *Agent) Close() { a.wg.Wait() }

// drainBackgroundInbox formats any unreported completed background tasks as
// a single "[background]" note and marks them reported so they surface only
// once. Returns "" if the inbox is empty.
func (a *Agent) drainBackgroundInbox() string {
	tasks, err := a.db.Tasks.UnreportedCompleted()
	if err != nil || len(tasks) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("[background] while you were idle, these tasks reached a terminal state:\n")
	ids := make([]int64, 0, len(tasks))
	for _, t := range tasks {
		ids = append(ids, t.ID)
		result := t.Result
		// Trim long results — the full text is still in the DB for anyone
		// who wants to read it via task(action="artifacts") or agent(action="log").
		if len(result) > 300 {
			result = result[:300] + "…"
		}
		if result == "" {
			fmt.Fprintf(&b, "- task %d [%s] %q (role: %s)\n", t.ID, t.Status, t.Title, t.AssignedRole)
		} else {
			fmt.Fprintf(&b, "- task %d [%s] %q (role: %s): %s\n", t.ID, t.Status, t.Title, t.AssignedRole, result)
		}
	}
	b.WriteString("\nWeave into your response if relevant, or just acknowledge and continue.")

	if err := a.db.Tasks.MarkReported(ids); err != nil {
		// If we can't mark reported, don't surface the note — otherwise it'd
		// repeat on every turn. Logging is fine to fail silently here; the
		// next turn will retry.
		return ""
	}
	return b.String()
}

// Embed returns a vector embedding for text using the session's configured
// embed model. Exposed so UI surfaces (e.g. the TUI memory spotlight) can
// reuse the same embedder the summarizer and memory_search tool use, without
// needing their own llm.Client handle. Returns an empty slice + error if
// no embed_model is configured.
func (a *Agent) Embed(text string) ([]float32, error) {
	model, _ := a.db.Config.Get("embed_model")
	if model == "" {
		return nil, nil
	}
	return a.llm.Embed(model, text)
}

// LastAssistantText returns the most recent assistant message's content from
// the in-memory history. Used by background task workers to capture a task's
// canonical output without a second DB round-trip — the in-memory history
// is the authoritative view of what the loop just produced, avoiding the
// stale-read that LastAssistantMessage exposed on partial failure.
// Returns "" when the turn produced only tool calls or errored before emitting text.
func (a *Agent) LastAssistantText() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := len(a.history) - 1; i >= 0; i-- {
		m := a.history[i]
		if m.Role == "assistant" && m.Content != "" {
			return m.Content
		}
	}
	return ""
}

// IsStreaming reports whether the agent is mid-turn.
func (a *Agent) IsStreaming() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.streaming
}

// buildSystemPrompt is the closure passed to loopConfig.buildPrompt.
// Called fresh at the start of every outer loop iteration.
func (a *Agent) buildSystemPrompt() (llm.Message, error) {
	return BuildSystemPrompt(a.db, a.session.ID, a.session.Role, a.session.CWD, a.tools, a.lastActiveBeforeTurn, a.registry)
}

func (a *Agent) drainSteering() []llm.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	msgs := a.steerQueue
	a.steerQueue = nil
	return msgs
}

func (a *Agent) drainFollowUp() []llm.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	msgs := a.followQueue
	a.followQueue = nil
	return msgs
}

