package db

import (
	"database/sql"
	"log"
	"time"
)

type Summary struct {
	ID            int64
	SessionID     int64
	Content       string
	Embedding     []float32
	CoversFrom    int64 // first message ID covered
	CoversThrough int64 // last message ID covered
	CreatedAt     time.Time
}

type Fact struct {
	ID        int64
	SessionID int64
	SummaryID int64
	Content   string
	Embedding []float32
	CreatedAt time.Time
}

type SummaryQ struct{ db *sql.DB }
type FactQ struct{ db *sql.DB }

// --- Summaries ---

func (q *SummaryQ) Add(sessionID, coversFrom, coversThrough int64, content string, embedding []float32) (*Summary, error) {
	res, err := q.db.Exec(
		`INSERT INTO summaries(session_id, content, embedding, covers_from, covers_through)
		 VALUES(?, ?, ?, ?, ?)`,
		sessionID, content, encodeEmbedding(embedding), coversFrom, coversThrough)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return q.Get(id)
}

func (q *SummaryQ) Get(id int64) (*Summary, error) {
	row := q.db.QueryRow(
		`SELECT id, session_id, content, embedding, covers_from, covers_through, created_at
		 FROM summaries WHERE id = ?`, id)
	return scanSummary(row)
}

// LatestForSession returns the most recent n summaries for a session.
// Use these for the context window — most recent = most relevant.
func (q *SummaryQ) LatestForSession(sessionID int64, n int) ([]*Summary, error) {
	rows, err := q.db.Query(
		`SELECT id, session_id, content, embedding, covers_from, covers_through, created_at
		 FROM summaries WHERE session_id = ?
		 ORDER BY created_at DESC LIMIT ?`, sessionID, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Summary
	for rows.Next() {
		s, err := scanSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	// Reverse so chronological order is preserved for the LLM
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

// Search finds the top-k summaries by semantic similarity across ALL sessions.
func (q *SummaryQ) Search(query []float32, k int) ([]*Summary, error) {
	rows, err := q.db.Query(
		`SELECT id, session_id, content, embedding, covers_from, covers_through, created_at
		 FROM summaries WHERE embedding IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type scored struct {
		s     *Summary
		score float32
	}
	var candidates []scored
	var skippedDimMismatch int
	for rows.Next() {
		s, err := scanSummary(rows)
		if err != nil {
			return nil, err
		}
		if len(s.Embedding) == 0 {
			continue
		}
		if len(s.Embedding) != len(query) {
			skippedDimMismatch++
			continue
		}
		candidates = append(candidates, scored{s, cosine(query, s.Embedding)})
	}
	if skippedDimMismatch > 0 {
		log.Printf("summary_search: skipped %d summaries with mismatched embedding dim (query dim=%d) — embed_model likely changed",
			skippedDimMismatch, len(query))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := 0; i < k && i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].score > candidates[i].score {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}
	if k > len(candidates) {
		k = len(candidates)
	}
	out := make([]*Summary, k)
	for i := range out {
		out[i] = candidates[i].s
	}
	return out, nil
}

func (q *SummaryQ) All() ([]*Summary, error) {
	rows, err := q.db.Query(
		`SELECT id, session_id, content, embedding, covers_from, covers_through, created_at
		 FROM summaries ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Summary
	for rows.Next() {
		s, err := scanSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (q *SummaryQ) Update(id int64, content string, embedding []float32) error {
	_, err := q.db.Exec(
		`UPDATE summaries SET content = ?, embedding = ? WHERE id = ?`,
		content, encodeEmbedding(embedding), id)
	return err
}

func scanSummary(row scanner) (*Summary, error) {
	var s Summary
	var createdAt int64
	var embBlob []byte
	err := row.Scan(&s.ID, &s.SessionID, &s.Content, &embBlob, &s.CoversFrom, &s.CoversThrough, &createdAt)
	if err != nil {
		return nil, err
	}
	s.Embedding = decodeEmbedding(embBlob)
	s.CreatedAt = time.Unix(createdAt, 0)
	return &s, nil
}

// --- Facts ---

func (q *FactQ) Add(sessionID, summaryID int64, content string, embedding []float32) (*Fact, error) {
	res, err := q.db.Exec(
		`INSERT INTO facts(session_id, summary_id, content, embedding) VALUES(?, ?, ?, ?)`,
		sessionID, summaryID, content, encodeEmbedding(embedding))
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return q.GetFact(id)
}

func (q *FactQ) GetFact(id int64) (*Fact, error) {
	row := q.db.QueryRow(
		`SELECT id, session_id, summary_id, content, embedding, created_at FROM facts WHERE id = ?`, id)
	return scanFact(row)
}

func (q *FactQ) ForSession(sessionID int64) ([]*Fact, error) {
	rows, err := q.db.Query(
		`SELECT id, session_id, summary_id, content, embedding, created_at
		 FROM facts WHERE session_id = ? ORDER BY created_at DESC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Fact
	for rows.Next() {
		f, err := scanFact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (q *FactQ) All() ([]*Fact, error) {
	rows, err := q.db.Query(
		`SELECT id, session_id, summary_id, content, embedding, created_at FROM facts ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Fact
	for rows.Next() {
		f, err := scanFact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (q *FactQ) Delete(id int64) error {
	_, err := q.db.Exec(`DELETE FROM facts WHERE id = ?`, id)
	return err
}

func scanFact(row scanner) (*Fact, error) {
	var f Fact
	var createdAt int64
	var embBlob []byte
	err := row.Scan(&f.ID, &f.SessionID, &f.SummaryID, &f.Content, &embBlob, &createdAt)
	if err != nil {
		return nil, err
	}
	f.Embedding = decodeEmbedding(embBlob)
	f.CreatedAt = time.Unix(createdAt, 0)
	return &f, nil
}
