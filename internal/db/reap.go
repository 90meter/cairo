package db

import (
	"fmt"
	"log"
	"os"
	"syscall"
)

// isProcessAlive reports whether a Unix process with the given PID exists.
// Uses the classic signal(0) probe: a successful send proves the process
// is still owned by someone; ESRCH means it's gone. Does not distinguish
// "our task subprocess" from "a recycled PID" — false positives delay
// cleanup by one startup cycle but don't cause wrong state.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 is a no-op that still triggers permission + existence checks.
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

// ReapOrphanedTasks sweeps tasks stuck in status='running' whose subprocess
// is no longer alive, marking them failed so dependents and the UI aren't
// stuck waiting forever. Called once on DB open — the subprocesses spawned
// by agent_spawn detach, so a cairo crash leaves no cleanup path otherwise.
// Tasks with no recorded pid are left alone (they may be mid-spawn or are
// running inside this process, not a child).
func (db *DB) ReapOrphanedTasks() (int, error) {
	rows, err := db.sql.Query(
		`SELECT id, COALESCE(pid, 0) FROM tasks WHERE status = 'running' AND pid IS NOT NULL`)
	if err != nil {
		return 0, err
	}
	type rec struct{ id int64; pid int }
	var stale []rec
	for rows.Next() {
		var id int64
		var pid int
		if err := rows.Scan(&id, &pid); err != nil {
			rows.Close()
			return 0, err
		}
		if pid > 0 && !isProcessAlive(pid) {
			stale = append(stale, rec{id, pid})
		}
	}
	rows.Close()

	for _, r := range stale {
		msg := fmt.Sprintf("reaped: subprocess (pid %d) no longer alive on cairo startup", r.pid)
		if _, err := db.sql.Exec(
			`UPDATE tasks SET status='failed', result=?, completed_at=unixepoch() WHERE id=?`,
			msg, r.id,
		); err != nil {
			return len(stale), err
		}
	}
	if len(stale) > 0 {
		log.Printf("reap: marked %d orphaned task(s) as failed", len(stale))
	}
	return len(stale), nil
}
