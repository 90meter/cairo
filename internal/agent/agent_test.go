package agent

import (
	"strings"
	"testing"

	"github.com/scotmcc/cairo/internal/llm"
)

// tc is a helper to build an assistant message carrying tool calls.
func tcMsg(names ...string) llm.Message {
	calls := make([]llm.ToolCall, len(names))
	for i, n := range names {
		calls[i].Function.Name = n
	}
	return llm.Message{Role: "assistant", ToolCalls: calls}
}

func toolResult(content string) llm.Message {
	return llm.Message{Role: "tool", Content: content}
}

func userMsg(s string) llm.Message {
	return llm.Message{Role: "user", Content: s}
}

func assistantMsg(s string) llm.Message {
	return llm.Message{Role: "assistant", Content: s}
}

// TestRepairIncompleteTurn_NoOp verifies that a complete turn is not modified.
func TestRepairIncompleteTurn_NoOp(t *testing.T) {
	history := []llm.Message{
		userMsg("do two things"),
		tcMsg("tool_a", "tool_b"),
		toolResult("result_a"),
		toolResult("result_b"),
		assistantMsg("done"),
	}
	got := repairIncompleteTurn(history)
	if len(got) != len(history) {
		t.Errorf("expected history unchanged (len %d), got len %d", len(history), len(got))
	}
}

// TestRepairIncompleteTurn_Empty verifies no panic on empty history.
func TestRepairIncompleteTurn_Empty(t *testing.T) {
	got := repairIncompleteTurn(nil)
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

// TestRepairIncompleteTurn_ZeroResults strips and injects note when process
// died immediately after persisting the assistant tool-call row.
func TestRepairIncompleteTurn_ZeroResults(t *testing.T) {
	history := []llm.Message{
		userMsg("run something"),
		tcMsg("tool_a", "tool_b", "tool_c"),
		// no tool results — crash immediately after persisting the call row
	}
	got := repairIncompleteTurn(history)
	// assistant tool-call row should be stripped; system note injected
	if len(got) != 2 {
		t.Fatalf("expected 2 messages (user + system note), got %d: %v", len(got), got)
	}
	if got[0].Role != "user" {
		t.Errorf("expected first message to be user, got %q", got[0].Role)
	}
	if got[1].Role != "system" {
		t.Errorf("expected second message to be system note, got %q", got[1].Role)
	}
	if !strings.Contains(got[1].Content, "interrupted") {
		t.Errorf("system note should mention 'interrupted', got: %q", got[1].Content)
	}
}

// TestRepairIncompleteTurn_PartialResults strips and injects note when only
// some tool results were persisted before the crash.
func TestRepairIncompleteTurn_PartialResults(t *testing.T) {
	history := []llm.Message{
		userMsg("run three tools"),
		tcMsg("tool_a", "tool_b", "tool_c"),
		toolResult("result_a"), // only first result made it
	}
	got := repairIncompleteTurn(history)
	// assistant tool-call row + partial result should be stripped
	if len(got) != 2 {
		t.Fatalf("expected 2 messages (user + system note), got %d", len(got))
	}
	if got[0].Role != "user" {
		t.Errorf("expected user, got %q", got[0].Role)
	}
	if got[1].Role != "system" {
		t.Errorf("expected system note, got %q", got[1].Role)
	}
}

// TestRepairIncompleteTurn_ExactMatch verifies no repair when results == calls.
func TestRepairIncompleteTurn_ExactMatch(t *testing.T) {
	history := []llm.Message{
		userMsg("run two tools"),
		tcMsg("tool_a", "tool_b"),
		toolResult("result_a"),
		toolResult("result_b"),
		// turn ended but no final assistant text yet — still complete
	}
	got := repairIncompleteTurn(history)
	if len(got) != len(history) {
		t.Errorf("expected history unchanged (len %d), got %d", len(history), len(got))
	}
}

// TestRepairIncompleteTurn_NoToolCallsAtTail verifies that trailing tool
// results after a plain assistant text message are not treated as incomplete.
func TestRepairIncompleteTurn_NoToolCallsAtTail(t *testing.T) {
	// This shouldn't occur in practice (tool results always follow tool calls)
	// but the repair function must not corrupt such a history.
	history := []llm.Message{
		userMsg("hello"),
		assistantMsg("hi there"),
	}
	got := repairIncompleteTurn(history)
	if len(got) != len(history) {
		t.Errorf("expected history unchanged, got len %d", len(got))
	}
}
