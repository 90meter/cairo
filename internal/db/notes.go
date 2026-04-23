package db

import (
	"database/sql"
	"time"
)

type Note struct {
	ID        int64
	Title     string
	Content   string
	Tags      string // JSON array
	CreatedAt time.Time
	UpdatedAt time.Time
}

type NoteQ struct{ db *sql.DB }

func (q *NoteQ) Create(title, content, tags string) (*Note, error) {
	res, err := q.db.Exec(
		`INSERT INTO notes(title, content, tags) VALUES(?, ?, ?)`, title, content, tags)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return q.Get(id)
}

func (q *NoteQ) Get(id int64) (*Note, error) {
	row := q.db.QueryRow(
		`SELECT id, title, content, tags, created_at, updated_at FROM notes WHERE id = ?`, id)
	return scanNote(row)
}

func (q *NoteQ) List() ([]*Note, error) {
	rows, err := q.db.Query(
		`SELECT id, title, content, tags, created_at, updated_at FROM notes ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Note
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (q *NoteQ) Update(id int64, title, content string) error {
	_, err := q.db.Exec(
		`UPDATE notes SET title = ?, content = ?, updated_at = unixepoch() WHERE id = ?`,
		title, content, id)
	return err
}

func (q *NoteQ) Delete(id int64) error {
	_, err := q.db.Exec(`DELETE FROM notes WHERE id = ?`, id)
	return err
}

func scanNote(row scanner) (*Note, error) {
	var n Note
	var createdAt, updatedAt int64
	err := row.Scan(&n.ID, &n.Title, &n.Content, &n.Tags, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	n.CreatedAt = time.Unix(createdAt, 0)
	n.UpdatedAt = time.Unix(updatedAt, 0)
	return &n, nil
}
