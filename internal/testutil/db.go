package testutil

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/db"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// NewTestDB creates a temporary SQLite database with all migrations applied.
// The database is automatically closed at the end of the test via t.Cleanup.
// Uses the same settings as production: WAL mode, foreign_keys=ON, MaxOpenConns=1.
func NewTestDB(t testing.TB) *sql.DB {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "test.db")
	testDB, err := sql.Open("sqlite3", tmp)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	testDB.SetMaxOpenConns(1)

	var mode string
	if err := testDB.QueryRow("PRAGMA journal_mode=wal").Scan(&mode); err != nil || mode != "wal" {
		testDB.Close()
		t.Fatalf("enable WAL mode: got %q, err %v", mode, err)
	}
	if _, err := testDB.Exec("PRAGMA busy_timeout=5000"); err != nil {
		testDB.Close()
		t.Fatalf("set busy_timeout: %v", err)
	}
	if _, err := testDB.Exec("PRAGMA foreign_keys=ON"); err != nil {
		testDB.Close()
		t.Fatalf("enable foreign keys: %v", err)
	}

	if err := db.Migrate(testDB); err != nil {
		testDB.Close()
		t.Fatalf("migrate test db: %v", err)
	}

	t.Cleanup(func() { testDB.Close() })
	return testDB
}
