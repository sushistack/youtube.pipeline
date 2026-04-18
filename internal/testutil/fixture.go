package testutil

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/db"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// LoadFixture reads a fixture file from testdata/{path} at the project root.
func LoadFixture(t testing.TB, path string) []byte {
	t.Helper()
	root := ProjectRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "testdata", path))
	if err != nil {
		t.Fatalf("load fixture %s: %v", path, err)
	}
	return data
}

// ProjectRoot resolves the repository root by walking up to the nearest go.mod.
func ProjectRoot(t testing.TB) string {
	t.Helper()
	return findProjectRoot(t)
}

// LoadRunStateFixture creates a temporary SQLite DB pre-seeded with data from
// testdata/fixtures/{name}.sql. The DB has all migrations applied and matches
// production settings (WAL mode, foreign keys, MaxOpenConns=1).
func LoadRunStateFixture(t testing.TB, name string) *sql.DB {
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

	seed := LoadFixture(t, filepath.Join("fixtures", name+".sql"))
	if _, err := testDB.Exec(string(seed)); err != nil {
		testDB.Close()
		t.Fatalf("seed fixture %s: %v", name, err)
	}

	t.Cleanup(func() { testDB.Close() })
	return testDB
}

// findProjectRoot walks up from the current working directory until it finds go.mod.
func findProjectRoot(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("project root not found: no go.mod in any parent directory")
		}
		dir = parent
	}
}
