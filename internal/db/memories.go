package db

import (
	"database/sql"
	"encoding/binary"
	"log"
	"math"
	"time"
)

type Memory struct {
	ID        int64
	Content   string
	Tags      string // JSON array
	Embedding []float32
	CreatedAt time.Time
	UpdatedAt time.Time
}

type MemoryQ struct{ db *sql.DB }

func (q *MemoryQ) Add(content, tags string, embedding []float32) (*Memory, error) {
	res, err := q.db.Exec(
		`INSERT INTO memories(content, tags, embedding) VALUES(?, ?, ?)`,
		content, tags, encodeEmbedding(embedding),
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return q.Get(id)
}

func (q *MemoryQ) Get(id int64) (*Memory, error) {
	row := q.db.QueryRow(
		`SELECT id, content, tags, embedding, created_at, updated_at FROM memories WHERE id = ?`, id)
	return scanMemory(row)
}

// RecentContent returns just the content strings for the N most recent memories.
// Skips embedding BLOB decoding entirely — the prompt builder only needs content,
// and decoding every embedding on every turn was a hot-path cost flagged in review.
func (q *MemoryQ) RecentContent(limit int) ([]string, error) {
	rows, err := q.db.Query(
		`SELECT content FROM memories ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Count returns the total number of stored memories.
func (q *MemoryQ) Count() (int, error) {
	var n int
	err := q.db.QueryRow(`SELECT COUNT(*) FROM memories`).Scan(&n)
	return n, err
}

// DimBreakdown reports how many stored memories exist at each embedding
// dimension, derived cheaply from blob byte length (float32 => 4 bytes).
// Useful for diagnosing a silent embed_model swap — if the map has more
// than one non-zero key, cross-dim cosine won't work.
func (q *MemoryQ) DimBreakdown() (map[int]int, error) {
	rows, err := q.db.Query(
		`SELECT COALESCE(length(embedding),0)/4 AS dim, COUNT(*)
		 FROM memories GROUP BY dim`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int]int)
	for rows.Next() {
		var dim, count int
		if err := rows.Scan(&dim, &count); err != nil {
			return nil, err
		}
		out[dim] = count
	}
	return out, rows.Err()
}

// AllContent returns all memories with content and metadata but without
// embedding BLOBs — lighter than All() for listing and display purposes.
func (q *MemoryQ) AllContent() ([]*Memory, error) {
	rows, err := q.db.Query(
		`SELECT id, content, tags, NULL as embedding, created_at, updated_at FROM memories ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Memory
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (q *MemoryQ) All() ([]*Memory, error) {
	rows, err := q.db.Query(
		`SELECT id, content, tags, embedding, created_at, updated_at FROM memories ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Memory
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (q *MemoryQ) Delete(id int64) error {
	_, err := q.db.Exec(`DELETE FROM memories WHERE id = ?`, id)
	return err
}

func (q *MemoryQ) Update(id int64, content string) error {
	_, err := q.db.Exec(`UPDATE memories SET content = ?, updated_at = unixepoch() WHERE id = ?`, content, id)
	return err
}

func (q *MemoryQ) UpdateWithEmbedding(id int64, content string, embedding []float32) error {
	_, err := q.db.Exec(
		`UPDATE memories SET content = ?, embedding = ?, updated_at = unixepoch() WHERE id = ?`,
		content, encodeEmbedding(embedding), id)
	return err
}

// Search returns the top-k memories by cosine similarity to the query embedding.
// Pure Go — no CGO required. Fine for hundreds to low-thousands of memories.
func (q *MemoryQ) Search(query []float32, k int) ([]*Memory, error) {
	all, err := q.All()
	if err != nil {
		return nil, err
	}

	type scored struct {
		m     *Memory
		score float32
	}
	var candidates []scored
	var skippedDimMismatch int
	for _, m := range all {
		if len(m.Embedding) == 0 {
			continue
		}
		if len(m.Embedding) != len(query) {
			skippedDimMismatch++
			continue
		}
		candidates = append(candidates, scored{m, cosine(query, m.Embedding)})
	}
	if skippedDimMismatch > 0 {
		log.Printf("memory_search: skipped %d memories with mismatched embedding dim (query dim=%d) — embed_model likely changed; re-embed or restore the prior model",
			skippedDimMismatch, len(query))
	}

	// partial sort: bubble top-k to front
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
	out := make([]*Memory, k)
	for i := range out {
		out[i] = candidates[i].m
	}
	return out, nil
}

func scanMemory(row scanner) (*Memory, error) {
	var m Memory
	var createdAt, updatedAt int64
	var embBlob []byte
	err := row.Scan(&m.ID, &m.Content, &m.Tags, &embBlob, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	m.Embedding = decodeEmbedding(embBlob)
	m.CreatedAt = time.Unix(createdAt, 0)
	m.UpdatedAt = time.Unix(updatedAt, 0)
	return &m, nil
}

// encodeEmbedding serializes float32 slice to little-endian bytes.
func encodeEmbedding(v []float32) []byte {
	if len(v) == 0 {
		return nil
	}
	b := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

func decodeEmbedding(b []byte) []float32 {
	if len(b) == 0 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

func cosine(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}
