package db

import (
	"path/filepath"
	"testing"
)

// openTest returns a fully-migrated, seeded DB backed by a tempdir file.
// Each test gets its own DB so concurrent tests don't interfere. The file
// is cleaned up via t.TempDir's automatic teardown.
func openTest(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	database, err := OpenAt(path)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}
