package db

import (
	"database/sql"
	"encoding/json"
	"time"
)

type Role struct {
	ID             int64
	Name           string
	Description    string
	Model          string
	BasePromptKey  string
	Tools          string // JSON array of tool names
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type RoleQ struct{ db *sql.DB }

func (q *RoleQ) Get(name string) (*Role, error) {
	row := q.db.QueryRow(
		`SELECT id, name, description, model, base_prompt_key, tools, created_at, updated_at
		 FROM roles WHERE name = ?`, name)
	return scanRole(row)
}

func (q *RoleQ) List() ([]*Role, error) {
	rows, err := q.db.Query(
		`SELECT id, name, description, model, base_prompt_key, tools, created_at, updated_at
		 FROM roles ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Role
	for rows.Next() {
		r, err := scanRole(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AllowedTools returns the parsed tool whitelist for a role.
// Empty (nil) means unrestricted — the role may call any built-in tool.
// A non-empty slice is a whitelist intersected against registered tools.
// If the role doesn't exist or the tools column is empty/malformed, returns nil.
func (q *RoleQ) AllowedTools(roleName string) ([]string, error) {
	if roleName == "" {
		return nil, nil
	}
	var raw string
	err := q.db.QueryRow(`SELECT COALESCE(tools,'') FROM roles WHERE name = ?`, roleName).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return nil, nil
	}
	var names []string
	if err := json.Unmarshal([]byte(raw), &names); err != nil {
		return nil, nil // malformed → treat as unrestricted
	}
	return names, nil
}

// ModelFor returns the model configured for the given role, falling back to
// the global config model if the role has no model set.
func (q *RoleQ) ModelFor(roleName string) (string, error) {
	if roleName == "" {
		return "", nil
	}
	var model string
	err := q.db.QueryRow(`SELECT model FROM roles WHERE name = ?`, roleName).Scan(&model)
	if err != nil || model == "" {
		return "", nil // caller falls back to global config
	}
	return model, nil
}

func (q *RoleQ) Upsert(name, description, model, basePromptKey, tools string) error {
	_, err := q.db.Exec(`
		INSERT INTO roles(name, description, model, base_prompt_key, tools, updated_at)
		VALUES(?, ?, ?, ?, ?, unixepoch())
		ON CONFLICT(name) DO UPDATE SET
			description     = CASE WHEN excluded.description    != '' THEN excluded.description    ELSE description    END,
			model           = CASE WHEN excluded.model          != '' THEN excluded.model          ELSE model          END,
			base_prompt_key = CASE WHEN excluded.base_prompt_key != '' THEN excluded.base_prompt_key ELSE base_prompt_key END,
			tools           = CASE WHEN excluded.tools          != '' THEN excluded.tools          ELSE tools          END,
			updated_at      = unixepoch()`,
		name, description, model, basePromptKey, tools)
	return err
}

// SetModel updates just the model for an existing role.
func (q *RoleQ) SetModel(name, model string) error {
	_, err := q.db.Exec(`UPDATE roles SET model = ?, updated_at = unixepoch() WHERE name = ?`, model, name)
	return err
}

func scanRole(row scanner) (*Role, error) {
	var r Role
	var createdAt, updatedAt int64
	err := row.Scan(&r.ID, &r.Name, &r.Description, &r.Model, &r.BasePromptKey, &r.Tools, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	r.CreatedAt = time.Unix(createdAt, 0)
	r.UpdatedAt = time.Unix(updatedAt, 0)
	return &r, nil
}
