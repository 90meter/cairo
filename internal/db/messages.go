package db

import (
	"database/sql"
	"time"
)

type Message struct {
	ID        int64
	SessionID int64
	Role      string // user | assistant | tool
	Content   string // raw text or JSON
	ToolCalls string // JSON array, non-empty for assistant messages that call tools
	ToolName  string // non-empty for tool result messages
	ToolID    string // non-empty for tool result messages
	CreatedAt time.Time
}

type MessageQ struct{ db *sql.DB }

func (q *MessageQ) Add(sessionID int64, role, content, toolCalls, toolName, toolID string) (*Message, error) {
	res, err := q.db.Exec(
		`INSERT INTO messages(session_id, role, content, tool_calls, tool_name, tool_id)
		 VALUES(?, ?, ?, ?, ?, ?)`,
		sessionID, role, content,
		nullStr(toolCalls), nullStr(toolName), nullStr(toolID),
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return q.Get(id)
}

func (q *MessageQ) Get(id int64) (*Message, error) {
	row := q.db.QueryRow(
		`SELECT id, session_id, role, content,
		        COALESCE(tool_calls,''), COALESCE(tool_name,''), COALESCE(tool_id,''),
		        created_at
		 FROM messages WHERE id = ?`, id)
	return scanMessage(row)
}

// UnsummarizedForSession returns messages not yet included in any summary.
// These form the "hot" context window sent to the LLM.
func (q *MessageQ) UnsummarizedForSession(sessionID int64) ([]*Message, error) {
	rows, err := q.db.Query(
		`SELECT id, session_id, role, content,
		        COALESCE(tool_calls,''), COALESCE(tool_name,''), COALESCE(tool_id,''),
		        created_at
		 FROM messages WHERE session_id = ? AND summarized = 0 ORDER BY id ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CountForSession returns the total number of messages in a session (all
// roles, summarized or not). Used by the session browser UI for at-a-glance
// "how much has been said here" display.
func (q *MessageQ) CountForSession(sessionID int64) (int, error) {
	var n int
	err := q.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE session_id = ?`, sessionID).Scan(&n)
	return n, err
}

// CountUnsummarized returns the number of unsummarized user/assistant messages.
// Tool-call and tool-result rows are not counted — they're part of a turn, not a turn themselves.
func (q *MessageQ) CountUnsummarized(sessionID int64) (int, error) {
	var n int
	err := q.db.QueryRow(
		`SELECT COUNT(*) FROM messages
		 WHERE session_id = ? AND summarized = 0 AND role IN ('user','assistant') AND content != ''`,
		sessionID).Scan(&n)
	return n, err
}

// MarkSummarized marks a range of messages as summarized by ID.
func (q *MessageQ) MarkSummarized(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	// Build placeholder list
	args := make([]any, len(ids))
	placeholders := make([]byte, 0, len(ids)*2)
	for i, id := range ids {
		args[i] = id
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
	}
	_, err := q.db.Exec(
		"UPDATE messages SET summarized = 1 WHERE id IN ("+string(placeholders)+")",
		args...)
	return err
}

// OldestUnsummarized returns up to n oldest unsummarized user/assistant messages.
// The summarizer processes these as one batch.
func (q *MessageQ) OldestUnsummarized(sessionID int64, n int) ([]*Message, error) {
	rows, err := q.db.Query(
		`SELECT id, session_id, role, content,
		        COALESCE(tool_calls,''), COALESCE(tool_name,''), COALESCE(tool_id,''),
		        created_at
		 FROM messages
		 WHERE session_id = ? AND summarized = 0 AND role IN ('user','assistant') AND content != ''
		 ORDER BY id ASC LIMIT ?`, sessionID, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (q *MessageQ) ForSession(sessionID int64) ([]*Message, error) {
	rows, err := q.db.Query(
		`SELECT id, session_id, role, content,
		        COALESCE(tool_calls,''), COALESCE(tool_name,''), COALESCE(tool_id,''),
		        created_at
		 FROM messages WHERE session_id = ? ORDER BY id ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func scanMessage(row scanner) (*Message, error) {
	var m Message
	var createdAt int64
	err := row.Scan(
		&m.ID, &m.SessionID, &m.Role, &m.Content,
		&m.ToolCalls, &m.ToolName, &m.ToolID,
		&createdAt,
	)
	if err != nil {
		return nil, err
	}
	m.CreatedAt = time.Unix(createdAt, 0)
	return &m, nil
}
