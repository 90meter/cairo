package db

import (
	"database/sql"
	"time"
)

type CustomTool struct {
	ID             int64
	Name           string
	Description    string
	Parameters     string // JSON Schema object
	Implementation string // script body
	ImplType       string // bash | python
	PromptAddendum string // auto-loaded into system prompt when tool is active
	Enabled        bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type CustomToolQ struct{ db *sql.DB }

func (q *CustomToolQ) Get(name string) (*CustomTool, error) {
	row := q.db.QueryRow(
		`SELECT id, name, description, parameters, implementation, impl_type, prompt_addendum, enabled, created_at, updated_at
		 FROM custom_tools WHERE name = ?`, name)
	return scanCustomTool(row)
}

func (q *CustomToolQ) List() ([]*CustomTool, error) {
	rows, err := q.db.Query(
		`SELECT id, name, description, parameters, implementation, impl_type, prompt_addendum, enabled, created_at, updated_at
		 FROM custom_tools ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*CustomTool
	for rows.Next() {
		t, err := scanCustomTool(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (q *CustomToolQ) Enabled() ([]*CustomTool, error) {
	rows, err := q.db.Query(
		`SELECT id, name, description, parameters, implementation, impl_type, prompt_addendum, enabled, created_at, updated_at
		 FROM custom_tools WHERE enabled = 1 ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*CustomTool
	for rows.Next() {
		t, err := scanCustomTool(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (q *CustomToolQ) Create(name, description, parameters, implementation, implType, promptAddendum string) error {
	_, err := q.db.Exec(
		`INSERT INTO custom_tools(name, description, parameters, implementation, impl_type, prompt_addendum)
		 VALUES(?, ?, ?, ?, ?, ?)`,
		name, description, parameters, implementation, implType, promptAddendum)
	return err
}

func (q *CustomToolQ) SetEnabled(name string, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	_, err := q.db.Exec(
		`UPDATE custom_tools SET enabled = ?, updated_at = unixepoch() WHERE name = ?`, v, name)
	return err
}

func (q *CustomToolQ) Delete(name string) error {
	_, err := q.db.Exec(`DELETE FROM custom_tools WHERE name = ?`, name)
	return err
}

func scanCustomTool(row scanner) (*CustomTool, error) {
	var t CustomTool
	var createdAt, updatedAt int64
	var enabled int
	err := row.Scan(
		&t.ID, &t.Name, &t.Description, &t.Parameters,
		&t.Implementation, &t.ImplType, &t.PromptAddendum,
		&enabled, &createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}
	t.Enabled = enabled == 1
	t.CreatedAt = time.Unix(createdAt, 0)
	t.UpdatedAt = time.Unix(updatedAt, 0)
	return &t, nil
}
