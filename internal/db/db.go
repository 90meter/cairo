package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// DB wraps sql.DB and owns all query methods via embedded sub-types.
type DB struct {
	sql *sql.DB

	Config        *ConfigQ
	Sessions      *SessionQ
	Messages      *MessageQ
	Memories      *MemoryQ
	Roles         *RoleQ
	Prompts       *PromptQ
	Tools         *CustomToolQ
	Skills        *SkillQ
	Notes         *NoteQ
	Jobs          *JobQ
	Tasks         *TaskQ
	TaskArtifacts *TaskArtifactQ
	Summaries     *SummaryQ
	Facts         *FactQ
}

// Open opens (or creates) the cairo database at ~/.cairo2/cairo.db.
// It's a thin wrapper over OpenAt that resolves the default production path;
// tests call OpenAt directly with a tempdir path to stay isolated.
func Open() (*DB, error) {
	dir := filepath.Join(os.Getenv("HOME"), ".cairo2")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return OpenAt(filepath.Join(dir, "cairo.db"))
}

// OpenAt opens (or creates) a cairo DB at the given path.
func OpenAt(path string) (*DB, error) {
	sqldb, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	sqldb.SetMaxOpenConns(1)

	// Wait up to 15s for the write lock before giving up.
	// Prevents SQLITE_BUSY when multiple subprocesses open the DB at the same time.
	if _, err := sqldb.Exec("PRAGMA busy_timeout = 15000"); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("set busy_timeout: %w", err)
	}

	// Belt-and-suspenders: the _foreign_keys=on DSN parameter has turned out
	// to be unreliable with modernc.org/sqlite in some code paths (discovered
	// while writing export strip-history). Set it explicitly on the pinned
	// single connection so cascade deletes fire.
	if _, err := sqldb.Exec("PRAGMA foreign_keys = ON"); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("enable foreign_keys: %w", err)
	}

	if err := execSchema(sqldb, schema); err != nil {
		sqldb.Close()
		return nil, err
	}
	applyMigrations(sqldb)

	db := &DB{sql: sqldb}
	db.Config = &ConfigQ{db: sqldb}
	db.Sessions = &SessionQ{db: sqldb}
	db.Messages = &MessageQ{db: sqldb}
	db.Memories = &MemoryQ{db: sqldb}
	db.Roles = &RoleQ{db: sqldb}
	db.Prompts = &PromptQ{db: sqldb}
	db.Tools = &CustomToolQ{db: sqldb}
	db.Skills = &SkillQ{db: sqldb}
	db.Notes = &NoteQ{db: sqldb}
	db.Jobs          = &JobQ{db: sqldb}
	db.Tasks         = &TaskQ{db: sqldb}
	db.TaskArtifacts = &TaskArtifactQ{db: sqldb}
	db.Summaries     = &SummaryQ{db: sqldb}
	db.Facts         = &FactQ{db: sqldb}

	if err := db.seed(); err != nil {
		sqldb.Close()
		return nil, err
	}

	// Sweep any tasks that were "running" when cairo last crashed —
	// their PID is no longer alive, so mark them failed now instead of
	// leaving job_list reporting them as in-flight forever.
	// Errors are logged-and-ignored: a reap failure shouldn't block startup.
	if _, err := db.ReapOrphanedTasks(); err != nil {
		fmt.Fprintf(os.Stderr, "reap: %v\n", err)
	}

	return db, nil
}

func (db *DB) Close() error { return db.sql.Close() }

func execSchema(sqldb *sql.DB, s string) error {
	for _, stmt := range strings.Split(s, ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := sqldb.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func applyMigrations(sqldb *sql.DB) {
	for _, m := range migrations {
		sqldb.Exec(m)
	}
}

// seed inserts default data on first run (idempotent).
func (db *DB) seed() error {
	return db.seedDefaults()
}
