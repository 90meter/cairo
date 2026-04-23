package agent

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/scotmcc/cairo/internal/db"
	"github.com/scotmcc/cairo/internal/llm"
)

const summarizePrompt = `You are summarizing a conversation segment. Be concise and precise.

OUTPUT FORMAT (use exactly this structure):
SUMMARY: <2-3 sentences capturing what was discussed, decided, or accomplished>
FACTS:
- <one atomic fact per line, e.g. "User prefers tabs over spaces" or "Project uses PostgreSQL 15">
- <focus on durable facts, not transient state>
- <3-6 facts, omit if the segment is trivial>

Do not add commentary. Do not repeat the conversation. Just the summary and facts.`

// Summarize reads the oldest batch of unsummarized messages for a session,
// calls the summary model, stores the summary + facts, and marks messages done.
// Safe to call from a goroutine — errors are logged, not returned.
func Summarize(database *db.DB, llmClient *llm.Client, sessionID int64) {
	// load config
	model, _ := database.Config.Get("summary_model")
	if model == "" {
		model = "ministral-8b:latest"
	}
	thresholdStr, _ := database.Config.Get("summary_threshold")
	threshold, _ := strconv.Atoi(thresholdStr)
	if threshold <= 0 {
		threshold = 4
	}
	embedModel, _ := database.Config.Get("embed_model")

	// check how many unsummarized there are
	count, err := database.Messages.CountUnsummarized(sessionID)
	if err != nil || count < threshold {
		return
	}

	// grab the oldest batch (up to threshold messages)
	msgs, err := database.Messages.OldestUnsummarized(sessionID, threshold)
	if err != nil || len(msgs) == 0 {
		return
	}

	// build transcript for the summarizer
	var transcript strings.Builder
	for _, m := range msgs {
		role := m.Role
		if role == "assistant" {
			role = "Cairo"
		} else {
			role = "User"
		}
		fmt.Fprintf(&transcript, "%s: %s\n\n", role, m.Content)
	}

	// call the summary model
	systemMsg := llm.Message{Role: "system", Content: summarizePrompt}
	userMsg := llm.Message{Role: "user", Content: transcript.String()}

	var response strings.Builder
	// Summarizer runs in the background; a Background context is fine since
	// there's no user-facing cancel for it.
	_, _, _, err = llmClient.StreamOnce(context.Background(), model, []llm.Message{systemMsg, userMsg}, nil, llm.ChatOptions{}, llm.ChatCallbacks{
		Content: func(token string) { response.WriteString(token) },
	})
	if err != nil {
		log.Printf("summarizer: llm error for session %d: %v", sessionID, err)
		return
	}

	raw := strings.TrimSpace(response.String())
	summary, facts := parseSummaryResponse(raw)
	if summary == "" {
		log.Printf("summarizer: empty summary for session %d", sessionID)
		return
	}

	// embed the summary
	var embedding []float32
	if embedModel != "" {
		embedding, _ = llmClient.Embed(embedModel, summary)
	}

	// store summary
	firstID := msgs[0].ID
	lastID := msgs[len(msgs)-1].ID
	stored, err := database.Summaries.Add(sessionID, firstID, lastID, summary, embedding)
	if err != nil {
		log.Printf("summarizer: store error for session %d: %v", sessionID, err)
		return
	}

	// store facts
	for _, fact := range facts {
		fact = strings.TrimSpace(strings.TrimPrefix(fact, "- "))
		if fact == "" {
			continue
		}
		var factEmb []float32
		if embedModel != "" {
			factEmb, _ = llmClient.Embed(embedModel, fact)
		}
		database.Facts.Add(sessionID, stored.ID, fact, factEmb)
	}

	// mark messages as summarized
	ids := make([]int64, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	if err := database.Messages.MarkSummarized(ids); err != nil {
		log.Printf("summarizer: mark error for session %d: %v", sessionID, err)
	}
}

// SummarizeAll drains all unsummarized messages from a session in batches.
// Used when closing a session or starting a new one to avoid losing context.
func SummarizeAll(database *db.DB, llmClient *llm.Client, sessionID int64) {
	for {
		count, err := database.Messages.CountUnsummarized(sessionID)
		if err != nil || count == 0 {
			return
		}
		Summarize(database, llmClient, sessionID)
	}
}

func parseSummaryResponse(raw string) (summary string, facts []string) {
	lines := strings.Split(raw, "\n")
	inFacts := false

	for _, line := range lines {
		if strings.HasPrefix(line, "SUMMARY:") {
			summary = strings.TrimSpace(strings.TrimPrefix(line, "SUMMARY:"))
			inFacts = false
			continue
		}
		if strings.TrimSpace(line) == "FACTS:" {
			inFacts = true
			continue
		}
		if inFacts && strings.HasPrefix(strings.TrimSpace(line), "-") {
			fact := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
			if fact != "" {
				facts = append(facts, fact)
			}
		}
	}
	return
}
