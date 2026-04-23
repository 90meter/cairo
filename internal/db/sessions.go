package db

import (
	"database/sql"
	"time"
)

type Session struct {
	ID         int64
	Name       string
	CWD        string
	Role       string
	CreatedAt  time.Time
	LastActive time.Time
}

type SessionQ struct{ db *sql.DB }

func (q *SessionQ) Create(name, cwd, role string) (*Session, error) {
	res, err := q.db.Exec(
		`INSERT INTO sessions(name, cwd, role) VALUES(?, ?, ?)`,
		nullStr(name), cwd, role,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return q.Get(id)
}

func (q *SessionQ) Get(id int64) (*Session, error) {
	row := q.db.QueryRow(
		`SELECT id, COALESCE(name,''), cwd, role, created_at, last_active FROM sessions WHERE id = ?`, id)
	return scanSession(row)
}

func (q *SessionQ) Latest() (*Session, error) {
	row := q.db.QueryRow(
		`SELECT id, COALESCE(name,''), cwd, role, created_at, last_active FROM sessions ORDER BY last_active DESC LIMIT 1`)
	s, err := scanSession(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return s, err
}

func (q *SessionQ) List() ([]*Session, error) {
	rows, err := q.db.Query(
		`SELECT id, COALESCE(name,''), cwd, role, created_at, last_active FROM sessions ORDER BY last_active DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (q *SessionQ) Touch(id int64) error {
	_, err := q.db.Exec(`UPDATE sessions SET last_active = unixepoch() WHERE id = ?`, id)
	return err
}

func (q *SessionQ) Rename(id int64, name string) error {
	_, err := q.db.Exec(`UPDATE sessions SET name = ? WHERE id = ?`, name, id)
	return err
}

// Delete removes a session and — via ON DELETE CASCADE declared on every
// table that references sessions(id) — sweeps its messages, summaries,
// facts, jobs, and transitively the jobs' tasks and task_artifacts.
// Requires PRAGMA foreign_keys=on (set in Open()) to take effect.
func (q *SessionQ) Delete(id int64) error {
	_, err := q.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanSession(row scanner) (*Session, error) {
	var s Session
	var createdAt, lastActive int64
	err := row.Scan(&s.ID, &s.Name, &s.CWD, &s.Role, &createdAt, &lastActive)
	if err != nil {
		return nil, err
	}
	s.CreatedAt = time.Unix(createdAt, 0)
	s.LastActive = time.Unix(lastActive, 0)
	return &s, nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
