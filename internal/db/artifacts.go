package db

import (
	"database/sql"
	"time"
)

type TaskArtifact struct {
	ID        int64
	TaskID    int64
	Type      string // file | output | error
	Path      string // non-empty for file artifacts
	Content   string
	ToolName  string
	CreatedAt time.Time
}

type TaskArtifactQ struct{ db *sql.DB }

func (q *TaskArtifactQ) Add(taskID int64, artifactType, path, content, toolName string) error {
	_, err := q.db.Exec(
		`INSERT INTO task_artifacts(task_id, type, path, content, tool_name) VALUES(?,?,?,?,?)`,
		taskID, artifactType, path, content, toolName)
	return err
}

func (q *TaskArtifactQ) ForTask(taskID int64) ([]*TaskArtifact, error) {
	rows, err := q.db.Query(
		`SELECT id, task_id, type, path, content, tool_name, created_at
		 FROM task_artifacts WHERE task_id = ? ORDER BY created_at ASC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*TaskArtifact
	for rows.Next() {
		var a TaskArtifact
		var createdAt int64
		if err := rows.Scan(&a.ID, &a.TaskID, &a.Type, &a.Path, &a.Content, &a.ToolName, &createdAt); err != nil {
			return nil, err
		}
		a.CreatedAt = time.Unix(createdAt, 0)
		out = append(out, &a)
	}
	return out, rows.Err()
}
