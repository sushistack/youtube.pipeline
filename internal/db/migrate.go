package db

import (
	"database/sql"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/sushistack/youtube.pipeline/migrations"
)

// Migrate applies all pending SQL migrations from the embedded migrations.FS.
// It uses PRAGMA user_version to track which migrations have been applied.
func Migrate(db *sql.DB) error {
	var current int
	if err := db.QueryRow("PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}

	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		ver, err := parseMigrationVersion(e.Name())
		if err != nil {
			return fmt.Errorf("parse migration %s: %w", e.Name(), err)
		}
		if ver <= current {
			continue
		}

		data, err := fs.ReadFile(migrations.FS, e.Name())
		if err != nil {
			return fmt.Errorf("read %s: %w", e.Name(), err)
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", e.Name(), err)
		}
		if _, err := tx.Exec(string(data)); err != nil {
			tx.Rollback()
			return fmt.Errorf("exec %s: %w", e.Name(), err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", e.Name(), err)
		}
		// PRAGMA user_version cannot run inside a transaction — silently ignored.
		if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", ver)); err != nil {
			return fmt.Errorf("set user_version to %d: %w", ver, err)
		}
	}
	return nil
}

func parseMigrationVersion(filename string) (int, error) {
	parts := strings.SplitN(filename, "_", 2)
	if len(parts) == 0 {
		return 0, fmt.Errorf("invalid migration filename: %s", filename)
	}
	return strconv.Atoi(parts[0])
}
