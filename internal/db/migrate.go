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

	// Reject duplicate version prefixes up front: the runner records progress
	// in PRAGMA user_version, so two files sharing a number means whichever
	// sorts second is silently skipped on every install (the bug that left
	// 004_hitl_sessions.sql un-applied while 004_anti_progress_index.sql
	// stamped user_version=4). Failing loud here is cheap and prevents the
	// next contributor from re-introducing the same hazard.
	seen := map[int]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		ver, err := parseMigrationVersion(e.Name())
		if err != nil {
			return fmt.Errorf("parse migration %s: %w", e.Name(), err)
		}
		if prev, ok := seen[ver]; ok {
			return fmt.Errorf("duplicate migration version %d: %s and %s", ver, prev, e.Name())
		}
		seen[ver] = e.Name()
	}

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
			// "duplicate column name" means an idempotent ADD COLUMN migration
			// ran on a database that already has the column (fresh install path
			// vs. migration-drift path). Treat as already applied.
			if !strings.Contains(err.Error(), "duplicate column name") {
				return fmt.Errorf("exec %s: %w", e.Name(), err)
			}
		} else if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", e.Name(), err)
		}
		// PRAGMA user_version cannot run inside a transaction — silently ignored.
		if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", ver)); err != nil {
			return fmt.Errorf("set user_version to %d: %w", ver, err)
		}
	}

	// fixSettingsVersionsSchema repairs migration-drift on settings_versions:
	// the column was renamed from env_json → env_fingerprint in the 012 SQL
	// after some databases had already applied 012 with the old name. Any DB
	// that still has env_json must have it dropped (env_fingerprint was added
	// by migration 014). This runs after all migrations so user_version is
	// already correct; it is safe to repeat because column presence is checked
	// first.
	if err := fixSettingsVersionsSchema(db); err != nil {
		return fmt.Errorf("fix settings_versions schema: %w", err)
	}
	return nil
}

// fixSettingsVersionsSchema drops the legacy env_json column from
// settings_versions if it still exists. It is called after all SQL migrations
// have run and is intentionally idempotent: it is a no-op when env_json is
// already absent.
func fixSettingsVersionsSchema(db *sql.DB) error {
	cols, err := tableColumns(db, "settings_versions")
	if err != nil {
		// Table may not exist yet on a brand-new DB before migrations run.
		return nil
	}
	hasEnvJSON := false
	for _, c := range cols {
		if c == "env_json" {
			hasEnvJSON = true
			break
		}
	}
	if !hasEnvJSON {
		return nil
	}
	if _, err := db.Exec("ALTER TABLE settings_versions DROP COLUMN env_json"); err != nil {
		return fmt.Errorf("drop env_json: %w", err)
	}
	return nil
}

// tableColumns returns the column names of the named table via PRAGMA table_info.
// Returns nil, nil if the table does not exist.
func tableColumns(db *sql.DB, table string) ([]string, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols = append(cols, name)
	}
	return cols, rows.Err()
}

func parseMigrationVersion(filename string) (int, error) {
	parts := strings.SplitN(filename, "_", 2)
	if len(parts) == 0 {
		return 0, fmt.Errorf("invalid migration filename: %s", filename)
	}
	return strconv.Atoi(parts[0])
}
