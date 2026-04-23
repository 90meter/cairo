package db

import (
	"database/sql"
	"time"
)

type PromptPart struct {
	ID        int64
	Key       string
	Content   string
	Trigger   string // empty = always load; "tool:bash" = load when bash active; "role:coder" = load for coder role
	LoadOrder int
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type PromptQ struct{ db *sql.DB }

// Base returns all always-on prompt parts, ordered by load_order.
func (q *PromptQ) Base() ([]*PromptPart, error) {
	rows, err := q.db.Query(
		`SELECT id, key, content, COALESCE(trigger,''), load_order, enabled, created_at, updated_at
		 FROM prompt_parts WHERE trigger IS NULL AND enabled = 1
		 ORDER BY load_order ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPromptParts(rows)
}

// ForTrigger returns prompt parts that fire for a given trigger string.
func (q *PromptQ) ForTrigger(trigger string) ([]*PromptPart, error) {
	rows, err := q.db.Query(
		`SELECT id, key, content, COALESCE(trigger,''), load_order, enabled, created_at, updated_at
		 FROM prompt_parts WHERE trigger = ? AND enabled = 1
		 ORDER BY load_order ASC`, trigger)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPromptParts(rows)
}

func (q *PromptQ) All() ([]*PromptPart, error) {
	rows, err := q.db.Query(
		`SELECT id, key, content, COALESCE(trigger,''), load_order, enabled, created_at, updated_at
		 FROM prompt_parts ORDER BY load_order ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPromptParts(rows)
}

func (q *PromptQ) Add(key, content, trigger string, loadOrder int) error {
	_, err := q.db.Exec(
		`INSERT INTO prompt_parts(key, content, trigger, load_order) VALUES(?, ?, ?, ?)`,
		key, content, nullStr(trigger), loadOrder)
	return err
}

func (q *PromptQ) SetEnabled(id int64, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	_, err := q.db.Exec(`UPDATE prompt_parts SET enabled = ?, updated_at = unixepoch() WHERE id = ?`, v, id)
	return err
}

func (q *PromptQ) Update(id int64, content string) error {
	_, err := q.db.Exec(
		`UPDATE prompt_parts SET content = ?, updated_at = unixepoch() WHERE id = ?`, content, id)
	return err
}

func (q *PromptQ) Delete(id int64) error {
	_, err := q.db.Exec(`DELETE FROM prompt_parts WHERE id = ?`, id)
	return err
}

func scanPromptParts(rows *sql.Rows) ([]*PromptPart, error) {
	var out []*PromptPart
	for rows.Next() {
		var p PromptPart
		var createdAt, updatedAt int64
		var enabled int
		if err := rows.Scan(&p.ID, &p.Key, &p.Content, &p.Trigger, &p.LoadOrder, &enabled, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		p.Enabled = enabled == 1
		p.CreatedAt = time.Unix(createdAt, 0)
		p.UpdatedAt = time.Unix(updatedAt, 0)
		out = append(out, &p)
	}
	return out, rows.Err()
}

// DeleteByKeyPrefix removes all prompt parts whose key starts with prefix.
func (q *PromptQ) DeleteByKeyPrefix(prefix string) error {
	_, err := q.db.Exec(`DELETE FROM prompt_parts WHERE key LIKE ?`, prefix+"%")
	return err
}
