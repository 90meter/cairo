package db

import (
	"os"
	"path/filepath"
)

// DataDirName is the name of the cairo data directory within the user's home.
const DataDirName = ".cairo2"

// DefaultDataDir returns the default cairo data directory (~/.cairo2).
func DefaultDataDir() string {
	return filepath.Join(os.Getenv("HOME"), DataDirName)
}

// busyTimeoutMs is the SQLite busy_timeout value in milliseconds.
const busyTimeoutMs = 15000

// Role name constants — match the seeded role names in seed.go.
const (
	RoleThinkingPartner = "thinking_partner"
	RoleOrchestrator    = "orchestrator"
	RoleCoder           = "coder"
	RolePlanner         = "planner"
	RoleReviewer        = "reviewer"
	RoleDream           = "dream"
)

// Job and task status constants.
const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusDone      = "done"
	StatusFailed    = "failed"
	StatusBlocked   = "blocked"
	StatusCancelled = "cancelled"
)
