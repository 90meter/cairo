package db

import (
	"testing"
)

func TestSessionsDeleteCascade(t *testing.T) {
	database := openTest(t)

	// Create a session and populate every table that references it directly
	// (messages, summaries, facts, jobs) or transitively (tasks via jobs,
	// task_artifacts via tasks).
	sess, err := database.Sessions.Create("test", "/tmp", "thinking_partner")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	if _, err := database.Messages.Add(sess.ID, "user", "hello", "", "", ""); err != nil {
		t.Fatalf("add message: %v", err)
	}
	sum, err := database.Summaries.Add(sess.ID, 0, 0, "some summary", nil)
	if err != nil {
		t.Fatalf("add summary: %v", err)
	}
	if _, err := database.Facts.Add(sess.ID, sum.ID, "fact1", nil); err != nil {
		t.Fatalf("add fact: %v", err)
	}
	sid := sess.ID
	job, err := database.Jobs.Create("job1", "desc", "orchestrator", &sid)
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	task, err := database.Tasks.Create(job.ID, "task1", "desc", "coder", "[]")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := database.TaskArtifacts.Add(task.ID, "file", "/tmp/x", "", "write"); err != nil {
		t.Fatalf("add artifact: %v", err)
	}

	// Sanity: all tables have at least one row for this session's subtree.
	for _, tc := range []struct {
		table string
		want  int
	}{
		{"sessions", 1}, {"messages", 1}, {"summaries", 1}, {"facts", 1},
		{"jobs", 1}, {"tasks", 1}, {"task_artifacts", 1},
	} {
		if got := count(t, database, tc.table); got != tc.want {
			t.Fatalf("pre-delete %s: want %d, got %d", tc.table, tc.want, got)
		}
	}

	if err := database.Sessions.Delete(sess.ID); err != nil {
		t.Fatalf("delete session: %v", err)
	}

	// The delete should have cascaded through the whole subtree.
	for _, table := range []string{"sessions", "messages", "summaries", "facts", "jobs", "tasks", "task_artifacts"} {
		if got := count(t, database, table); got != 0 {
			t.Errorf("post-delete %s: want 0, got %d (cascade didn't sweep it)", table, got)
		}
	}
}

func count(t *testing.T, d *DB, table string) int {
	t.Helper()
	var n int
	if err := d.sql.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}
