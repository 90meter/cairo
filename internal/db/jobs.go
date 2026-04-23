package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type Job struct {
	ID               int64
	Title            string
	Description      string
	Status           string // pending | running | done | failed | cancelled
	OrchestratorRole string
	SessionID        *int64
	Result           string
	CreatedAt        time.Time
	StartedAt        *time.Time
	CompletedAt      *time.Time
}

type Task struct {
	ID           int64
	JobID        int64
	Title        string
	Description  string
	Status       string // pending | running | done | failed | blocked
	AssignedRole string
	DependsOn    string // JSON array of task IDs
	Result       string
	PID          *int
	LogPath      string
	CreatedAt    time.Time
	StartedAt    *time.Time
	CompletedAt  *time.Time
}

type JobQ struct{ db *sql.DB }
type TaskQ struct{ db *sql.DB }

// --- Jobs ---

func (q *JobQ) Create(title, description, orchestratorRole string, sessionID *int64) (*Job, error) {
	res, err := q.db.Exec(
		`INSERT INTO jobs(title, description, orchestrator_role, session_id) VALUES(?, ?, ?, ?)`,
		title, description, orchestratorRole, sessionID)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return q.Get(id)
}

func (q *JobQ) Get(id int64) (*Job, error) {
	row := q.db.QueryRow(
		`SELECT id, title, description, status, orchestrator_role, session_id, result,
		        created_at, started_at, completed_at
		 FROM jobs WHERE id = ?`, id)
	return scanJob(row)
}

func (q *JobQ) List() ([]*Job, error) {
	rows, err := q.db.Query(
		`SELECT id, title, description, status, orchestrator_role, session_id, result,
		        created_at, started_at, completed_at
		 FROM jobs ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (q *JobQ) SetStatus(id int64, status string) error {
	switch status {
	case "running":
		_, err := q.db.Exec(
			`UPDATE jobs SET status = ?, started_at = unixepoch() WHERE id = ?`, status, id)
		return err
	case "done", "failed", "cancelled":
		_, err := q.db.Exec(
			`UPDATE jobs SET status = ?, completed_at = unixepoch() WHERE id = ?`, status, id)
		return err
	default:
		_, err := q.db.Exec(`UPDATE jobs SET status = ? WHERE id = ?`, status, id)
		return err
	}
}

func (q *JobQ) SetResult(id int64, result string) error {
	_, err := q.db.Exec(`UPDATE jobs SET result = ? WHERE id = ?`, result, id)
	return err
}

func (q *JobQ) Delete(id int64) error {
	_, err := q.db.Exec(`DELETE FROM jobs WHERE id = ?`, id)
	return err
}

// --- Tasks ---

func (q *TaskQ) Create(jobID int64, title, description, assignedRole, dependsOn string) (*Task, error) {
	res, err := q.db.Exec(
		`INSERT INTO tasks(job_id, title, description, assigned_role, depends_on) VALUES(?, ?, ?, ?, ?)`,
		jobID, title, description, assignedRole, dependsOn)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return q.Get(id)
}

func (q *TaskQ) Get(id int64) (*Task, error) {
	row := q.db.QueryRow(
		`SELECT id, job_id, title, description, status, assigned_role, depends_on, result,
		        pid, COALESCE(log_path,''), created_at, started_at, completed_at
		 FROM tasks WHERE id = ?`, id)
	return scanTask(row)
}

func (q *TaskQ) ForJob(jobID int64) ([]*Task, error) {
	rows, err := q.db.Query(
		`SELECT id, job_id, title, description, status, assigned_role, depends_on, result,
		        pid, COALESCE(log_path,''), created_at, started_at, completed_at
		 FROM tasks WHERE job_id = ? ORDER BY created_at ASC`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (q *TaskQ) SetStatus(id int64, status string) error {
	// Enforce dependency ordering: a task can only run when all its deps are done.
	if status == "running" {
		if err := q.checkDeps(id); err != nil {
			return err
		}
	}

	switch status {
	case "running":
		_, err := q.db.Exec(
			`UPDATE tasks SET status = ?, started_at = unixepoch() WHERE id = ?`, status, id)
		return err
	case "done", "failed":
		_, err := q.db.Exec(
			`UPDATE tasks SET status = ?, completed_at = unixepoch() WHERE id = ?`, status, id)
		return err
	default:
		_, err := q.db.Exec(`UPDATE tasks SET status = ? WHERE id = ?`, status, id)
		return err
	}
}

// ClaimForSpawn atomically transitions a task from pending/blocked to running
// when all its dependencies are already done. Prevents the TOCTOU race where
// two concurrent agent_spawn callers both pass a stale Tasks.Get read and
// then both write status='running' via SetStatus, launching two subprocesses
// against the same task (and clobbering each other's log file).
//
// On failure, runs a diagnostic query to return a useful reason —
// "already running", "dep X not done", or "not found".
func (q *TaskQ) ClaimForSpawn(id int64) (*Task, error) {
	res, err := q.db.Exec(
		`UPDATE tasks SET status='running', started_at=unixepoch()
		 WHERE id = ?
		   AND status IN ('pending','blocked')
		   AND NOT EXISTS (
		       SELECT 1 FROM json_each(depends_on) je
		       WHERE (SELECT status FROM tasks WHERE id = je.value) != 'done'
		   )`, id)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		return q.Get(id)
	}

	// Claim failed — diagnose.
	task, gerr := q.Get(id)
	if gerr != nil {
		return nil, fmt.Errorf("task %d not found: %w", id, gerr)
	}
	if task.Status != "pending" && task.Status != "blocked" {
		return nil, fmt.Errorf("task %d is already %s", id, task.Status)
	}
	if err := q.checkDeps(id); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("task %d: claim failed", id)
}

func (q *TaskQ) SetResult(id int64, result string) error {
	_, err := q.db.Exec(`UPDATE tasks SET result = ? WHERE id = ?`, result, id)
	return err
}

func (q *TaskQ) SetPID(id int64, pid int) error {
	_, err := q.db.Exec(`UPDATE tasks SET pid = ? WHERE id = ?`, pid, id)
	return err
}

func (q *TaskQ) SetLogPath(id int64, path string) error {
	_, err := q.db.Exec(`UPDATE tasks SET log_path = ? WHERE id = ?`, path, id)
	return err
}

func (q *TaskQ) Delete(id int64) error {
	_, err := q.db.Exec(`DELETE FROM tasks WHERE id = ?`, id)
	return err
}

// CountRunning returns the number of tasks currently in status='running'.
// Used by the TUI's status bar to show a live pulse of Selene's parallel
// attention threads.
func (q *TaskQ) CountRunning() (int, error) {
	var n int
	err := q.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE status = 'running'`).Scan(&n)
	return n, err
}

// RecentAll returns the most recent tasks across all jobs, ordered newest
// first by created_at. Used by the TUI's threads panel to render the
// being's parallel-attention surface — what has run lately, what's running
// now. Pass limit to cap.
func (q *TaskQ) RecentAll(limit int) ([]*Task, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := q.db.Query(
		`SELECT id, job_id, title, description, status, assigned_role, depends_on, result,
		        pid, COALESCE(log_path,''), created_at, started_at, completed_at
		 FROM tasks ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// UnreportedCompleted returns tasks that have reached a terminal status
// (done, failed) but whose completion hasn't yet been surfaced to the parent
// session as a background-activity note. Caller should format them into a
// "[background] ..." inbox message and then call MarkReported.
func (q *TaskQ) UnreportedCompleted() ([]*Task, error) {
	rows, err := q.db.Query(
		`SELECT id, job_id, title, description, status, assigned_role, depends_on, result,
		        pid, COALESCE(log_path,''), created_at, started_at, completed_at
		 FROM tasks
		 WHERE status IN ('done','failed') AND reported_at IS NULL
		 ORDER BY completed_at ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// MarkReported stamps reported_at so the given tasks won't show up in the
// inbox again. Takes a slice of IDs to batch the update in one statement.
func (q *TaskQ) MarkReported(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := make([]byte, 0, len(ids)*2)
	args := make([]any, len(ids))
	for i, id := range ids {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args[i] = id
	}
	_, err := q.db.Exec(
		"UPDATE tasks SET reported_at = unixepoch() WHERE id IN ("+string(placeholders)+")",
		args...,
	)
	return err
}

// ReadyTasks returns all pending/blocked tasks in a job whose dependencies are
// all done — i.e. tasks the orchestrator can start right now.
func (q *TaskQ) ReadyTasks(jobID int64) ([]*Task, error) {
	all, err := q.ForJob(jobID)
	if err != nil {
		return nil, err
	}

	// index status by id
	statusOf := make(map[int64]string, len(all))
	for _, t := range all {
		statusOf[t.ID] = t.Status
	}

	var ready []*Task
	for _, t := range all {
		if t.Status != "pending" && t.Status != "blocked" {
			continue
		}
		deps, err := parseDeps(t.DependsOn)
		if err != nil {
			continue
		}
		allDone := true
		for _, depID := range deps {
			if statusOf[depID] != "done" {
				allDone = false
				break
			}
		}
		if allDone {
			ready = append(ready, t)
		}
	}
	return ready, nil
}

// checkDeps returns an error if any dependency of task id is not yet done.
func (q *TaskQ) checkDeps(id int64) error {
	task, err := q.Get(id)
	if err != nil {
		return err
	}
	deps, err := parseDeps(task.DependsOn)
	if err != nil {
		return fmt.Errorf("task %d: invalid depends_on: %w", id, err)
	}
	for _, depID := range deps {
		dep, err := q.Get(depID)
		if err != nil {
			return fmt.Errorf("task %d: dependency %d not found", id, depID)
		}
		if dep.Status != "done" {
			return fmt.Errorf("task %d cannot run: depends on task %d (%q) which is %s",
				id, depID, dep.Title, dep.Status)
		}
	}
	return nil
}

func parseDeps(raw string) ([]int64, error) {
	if raw == "" || raw == "[]" {
		return nil, nil
	}
	var ids []int64
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil, err
	}
	return ids, nil
}

// --- Scanners ---

func scanJob(row scanner) (*Job, error) {
	var j Job
	var createdAt int64
	var startedAt, completedAt sql.NullInt64
	var sessionID sql.NullInt64
	err := row.Scan(
		&j.ID, &j.Title, &j.Description, &j.Status,
		&j.OrchestratorRole, &sessionID, &j.Result,
		&createdAt, &startedAt, &completedAt,
	)
	if err != nil {
		return nil, err
	}
	j.CreatedAt = time.Unix(createdAt, 0)
	if sessionID.Valid {
		j.SessionID = &sessionID.Int64
	}
	if startedAt.Valid {
		t := time.Unix(startedAt.Int64, 0)
		j.StartedAt = &t
	}
	if completedAt.Valid {
		t := time.Unix(completedAt.Int64, 0)
		j.CompletedAt = &t
	}
	return &j, nil
}

func scanTask(row scanner) (*Task, error) {
	var t Task
	var createdAt int64
	var startedAt, completedAt sql.NullInt64
	var pid sql.NullInt64
	err := row.Scan(
		&t.ID, &t.JobID, &t.Title, &t.Description, &t.Status,
		&t.AssignedRole, &t.DependsOn, &t.Result,
		&pid, &t.LogPath,
		&createdAt, &startedAt, &completedAt,
	)
	if err != nil {
		return nil, err
	}
	if pid.Valid {
		p := int(pid.Int64)
		t.PID = &p
	}
	t.CreatedAt = time.Unix(createdAt, 0)
	if startedAt.Valid {
		ts := time.Unix(startedAt.Int64, 0)
		t.StartedAt = &ts
	}
	if completedAt.Valid {
		ts := time.Unix(completedAt.Int64, 0)
		t.CompletedAt = &ts
	}
	return &t, nil
}
