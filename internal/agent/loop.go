package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/scotmcc/cairo/internal/db"
	"github.com/scotmcc/cairo/internal/llm"
)

type loopConfig struct {
	model         string
	history       []llm.Message // user/assistant/tool only — no system prompt
	tools         []Tool
	llm           *llm.Client
	bus           *Bus
	db            *db.DB // passed to ToolContext for tools that need DB access (e.g. unsafe_mode check)
	session       *db.Session // threaded into ToolContext so self-inspection tools can rebuild prompt
	persist       func(role, content, toolCallsJSON, toolName, toolID string)
	workDir       string
	buildPrompt   func() (llm.Message, error)
	drainSteering func() []llm.Message
	drainFollowUp func() []llm.Message
}

// runLoop is the pure functional core — no UI coupling.
//
// The tool-call loop now lives here (not inside llm.Chat), so every
// intermediate message — assistant tool-call requests and tool results —
// is visible, persisted to the DB, and threaded correctly through history.
//
// Structure:
//
//	outer loop: re-runs while steering or follow-up messages are queued
//	  system prompt rebuilt fresh each outer iteration
//	  inner loop: re-runs while the model requests tool calls
//	    StreamOnce → if tool calls, execute + persist, loop
//	    if final text, persist, break inner
func runLoop(ctx context.Context, cfg loopConfig) error {
	cfg.bus.Publish(Event{Type: EventAgentStart})
	defer cfg.bus.Publish(Event{Type: EventAgentEnd})

	// conversation history — system prompt is NOT stored here
	msgs := make([]llm.Message, len(cfg.history))
	copy(msgs, cfg.history)

	toolDefs := make([]llm.ToolDef, len(cfg.tools))
	for i, t := range cfg.tools {
		toolDefs[i] = ToLLM(t)
	}
	toolMap := make(map[string]Tool, len(cfg.tools))
	for _, t := range cfg.tools {
		toolMap[t.Name()] = t
	}
	tc := &ToolContext{
		WorkDir: cfg.workDir,
		DB:      cfg.db,
		Bus:     cfg.bus,
		Session: cfg.session,
		Tools:   cfg.tools,
	}

	callSeq := 0 // for synthetic tool call IDs

	for { // outer: steering / follow-up
		cfg.bus.Publish(Event{Type: EventTurnStart})

		// Rebuild system prompt fresh — picks up soul/memory/prompt changes made this session
		sendMsgs := buildSendMsgs(cfg.buildPrompt, msgs)

		callbacks := llm.ChatCallbacks{
			Content: func(token string) {
				cfg.bus.Publish(Event{Type: EventTokens, Payload: PayloadTokens{Token: token}})
			},
			Thinking: func(token string) {
				cfg.bus.Publish(Event{Type: EventThinking, Payload: PayloadThinking{Token: token}})
			},
		}

		// inner: tool call loop
		var finalText string
		for {
			text, toolCalls, _, err := cfg.llm.StreamOnce(
				ctx, cfg.model, sendMsgs, toolDefs, llm.ChatOptions{}, callbacks,
			)
			if err != nil {
				// Distinguish a user-initiated cancel from a real error. On
				// cancel we persist whatever partial text arrived (tagged so
				// the transcript doesn't read as if Selene finished the
				// thought) and return cleanly — not as a failure.
				if ctx.Err() != nil {
					if text != "" {
						cfg.persist("assistant", text+"\n\n(interrupted)", "", "", "")
					}
					cfg.bus.Publish(Event{Type: EventTurnEnd, Payload: PayloadTurnEnd{}})
					return nil
				}
				cfg.bus.Publish(Event{Type: EventError, Payload: PayloadError{Err: err}})
				cfg.bus.Publish(Event{Type: EventTurnEnd, Payload: PayloadTurnEnd{}})
				return err
			}

			if len(toolCalls) == 0 {
				// Final text response — exit inner loop
				finalText = text
				break
			}

			// --- execute tool calls ---
			// Persist the assistant's tool-call request message
			toolCallsJSON := marshalToolCalls(toolCalls, callSeq)
			cfg.persist("assistant", "", toolCallsJSON, "", "")

			// Append to sendMsgs so the next StreamOnce has full context
			sendMsgs = append(sendMsgs, llm.Message{Role: "assistant", ToolCalls: toolCalls})

			for i, tc_call := range toolCalls {
				callSeq++
				callID := tc_call.CallID(callSeq)
				args := tc_call.Args()
				name := tc_call.Function.Name

				cfg.bus.Publish(Event{Type: EventToolStart, Payload: PayloadToolStart{Name: name, Args: args}})

				tool, ok := toolMap[name]
				var result ToolResult
				if !ok {
					result = ToolResult{
						Content: fmt.Sprintf("unknown tool: %s", name),
						IsError: true,
					}
				} else {
					result = tool.Execute(args, tc)
				}

				cfg.bus.Publish(Event{Type: EventToolEnd, Payload: PayloadToolEnd{
					Name:    name,
					Result:  result.Content,
					IsError: result.IsError,
				}})

				// Persist tool result to DB
				cfg.persist("tool", result.Content, "", name, callID)

				// Append tool result to sendMsgs for next iteration
				_ = i
				sendMsgs = append(sendMsgs, llm.Message{
					Role:    "tool",
					Content: result.Content,
				})

				// Honour cancellation between tool calls too — a long
				// chain of tools shouldn't ignore a Ctrl-C just because
				// Selene hasn't paused to stream text.
				if ctx.Err() != nil {
					cfg.bus.Publish(Event{Type: EventTurnEnd, Payload: PayloadTurnEnd{}})
					return nil
				}
			}
			// continue inner loop — model sees tool results and responds
		}

		// Persist final assistant text and update in-memory history
		if finalText != "" {
			cfg.persist("assistant", finalText, "", "", "")
			msgs = append(msgs, llm.Message{Role: "assistant", Content: finalText})
		}

		// Steering: messages injected while we were running
		steered := cfg.drainSteering()
		if len(steered) > 0 {
			cfg.bus.Publish(Event{Type: EventTurnEnd, Payload: PayloadTurnEnd{HasMore: true}})
			for _, m := range steered {
				cfg.persist(m.Role, m.Content, "", "", "")
			}
			msgs = append(msgs, steered...)
			continue
		}

		// Follow-ups: only run after agent would otherwise be idle
		followUps := cfg.drainFollowUp()
		if len(followUps) > 0 {
			cfg.bus.Publish(Event{Type: EventTurnEnd, Payload: PayloadTurnEnd{HasMore: true}})
			for _, m := range followUps {
				cfg.persist(m.Role, m.Content, "", "", "")
			}
			msgs = append(msgs, followUps...)
			continue
		}

		cfg.bus.Publish(Event{Type: EventTurnEnd, Payload: PayloadTurnEnd{HasMore: false}})
		return nil
	}
}

func buildSendMsgs(buildPrompt func() (llm.Message, error), msgs []llm.Message) []llm.Message {
	if buildPrompt == nil {
		return msgs
	}
	sys, err := buildPrompt()
	if err != nil || sys.Content == "" {
		return msgs
	}
	out := make([]llm.Message, 0, len(msgs)+1)
	out = append(out, sys)
	out = append(out, msgs...)
	return out
}

func marshalToolCalls(calls []llm.ToolCall, seqStart int) string {
	type entry struct {
		ID   string         `json:"id"`
		Name string         `json:"name"`
		Args map[string]any `json:"args"`
	}
	entries := make([]entry, len(calls))
	for i, c := range calls {
		entries[i] = entry{
			ID:   c.CallID(seqStart + i + 1),
			Name: c.Function.Name,
			Args: c.Args(),
		}
	}
	b, _ := json.Marshal(entries)
	return string(b)
}

// ContentPreview truncates tool output for readability in logs.
func contentPreview(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("… [%d bytes truncated]", len(s)-max)
}

func init() {
	_ = contentPreview // suppress unused warning — used in future logging
	_ = strings.Builder{}
}
