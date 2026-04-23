package db

import (
	"database/sql"
	"time"
)

type Skill struct {
	ID          int64
	Name        string
	Description string
	Content     string
	Tags        string // JSON array
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type SkillQ struct{ db *sql.DB }

func (q *SkillQ) Get(name string) (*Skill, error) {
	row := q.db.QueryRow(
		`SELECT id, name, description, content, tags, created_at, updated_at FROM skills WHERE name = ?`, name)
	return scanSkill(row)
}

func (q *SkillQ) List() ([]*Skill, error) {
	rows, err := q.db.Query(
		`SELECT id, name, description, content, tags, created_at, updated_at FROM skills ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Skill
	for rows.Next() {
		s, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (q *SkillQ) Create(name, description, content, tags string) error {
	_, err := q.db.Exec(
		`INSERT INTO skills(name, description, content, tags) VALUES(?, ?, ?, ?)`,
		name, description, content, tags)
	return err
}

func (q *SkillQ) Update(name, content string) error {
	_, err := q.db.Exec(
		`UPDATE skills SET content = ?, updated_at = unixepoch() WHERE name = ?`, content, name)
	return err
}

func (q *SkillQ) Delete(name string) error {
	_, err := q.db.Exec(`DELETE FROM skills WHERE name = ?`, name)
	return err
}

func scanSkill(row scanner) (*Skill, error) {
	var s Skill
	var createdAt, updatedAt int64
	err := row.Scan(&s.ID, &s.Name, &s.Description, &s.Content, &s.Tags, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	s.CreatedAt = time.Unix(createdAt, 0)
	s.UpdatedAt = time.Unix(updatedAt, 0)
	return &s, nil
}
