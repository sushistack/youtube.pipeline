package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestOpenDB_WALMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want %q", mode, "wal")
	}
}

func TestOpenDB_BusyTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	var timeout int
	if err := db.QueryRow("PRAGMA busy_timeout").Scan(&timeout); err != nil {
		t.Fatalf("query busy_timeout: %v", err)
	}
	if timeout != 5000 {
		t.Errorf("busy_timeout = %d, want %d", timeout, 5000)
	}
}

func TestOpenDB_FilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permissions not applicable on Windows")
	}

	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("file permissions = %o, want %o", perm, 0600)
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB (first): %v", err)
	}

	// Run migrate again on the same DB
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate (second run): %v", err)
	}

	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("query user_version: %v", err)
	}
	if version != 16 {
		t.Errorf("user_version = %d, want %d", version, 16)
	}

	db.Close()
}

func TestMigrate_UserVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("query user_version: %v", err)
	}
	if version != 16 {
		t.Errorf("user_version = %d, want %d", version, 16)
	}
}

func TestSchema_TablesExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	tables := []string{
		"runs",
		"decisions",
		"segments",
		"hitl_sessions",
		"critic_calibration_snapshots",
		"character_search_cache",
		"settings_versions",
		"settings_state",
	}
	for _, table := range tables {
		var name string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestSchema_SettingsStateSentinelRowExists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	var (
		id        int
		effective sql.NullInt64
	)
	err = db.QueryRow(`
SELECT id, effective_version
  FROM settings_state
 WHERE id = 1`).Scan(&id, &effective)
	if err != nil {
		t.Fatalf("sentinel settings_state row missing: %v", err)
	}
	if id != 1 {
		t.Errorf("sentinel id = %d, want 1", id)
	}
	if effective.Valid {
		t.Errorf("effective_version should start NULL; got %d", effective.Int64)
	}
}

func TestSchema_SettingsVersionsHasFingerprintNotEnvJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	// settings_versions must store env_fingerprint (audit-safe hash) and
	// must NOT store env_json (which would persist raw secrets into the DB).
	expected := map[string]string{
		"version":         "INTEGER",
		"config_json":     "TEXT",
		"env_fingerprint": "TEXT",
		"created_at":      "TEXT",
	}
	assertTableColumns(t, db, "settings_versions", expected)
}

func TestSchema_CriticCalibrationSnapshotsIndexExists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	var name string
	if err := db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='index' AND name='idx_critic_calibration_snapshots_window_computed_at'",
	).Scan(&name); err != nil {
		t.Fatalf("critic calibration snapshot index not found: %v", err)
	}
	if name != "idx_critic_calibration_snapshots_window_computed_at" {
		t.Fatalf("index name = %q", name)
	}
}

func TestSchema_HITLSessionsColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	expected := map[string]string{
		"run_id":                     "TEXT",
		"stage":                      "TEXT",
		"scene_index":                "INTEGER",
		"last_interaction_timestamp": "TEXT",
		"snapshot_json":              "TEXT",
		"created_at":                 "TEXT",
		"updated_at":                 "TEXT",
	}
	assertTableColumns(t, db, "hitl_sessions", expected)
}

func TestSchema_CharacterSearchCacheColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	expected := map[string]string{
		"query_key":   "TEXT",
		"query_text":  "TEXT",
		"result_json": "TEXT",
		"created_at":  "TEXT",
		"updated_at":  "TEXT",
	}
	assertTableColumns(t, db, "character_search_cache", expected)
}

func TestSchema_HITLSessionsUpdatedAtTrigger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	// Seed a run so the FK is satisfied.
	if _, err := db.Exec("INSERT INTO runs (id, scp_id) VALUES ('scp-049-run-1', '049')"); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO hitl_sessions (run_id, stage, scene_index, last_interaction_timestamp, snapshot_json, created_at, updated_at)
		VALUES ('scp-049-run-1', 'batch_review', 0, '2026-01-01T00:00:00Z', '{}', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert hitl_session: %v", err)
	}

	var before string
	if err := db.QueryRow("SELECT updated_at FROM hitl_sessions WHERE run_id = 'scp-049-run-1'").Scan(&before); err != nil {
		t.Fatalf("select before: %v", err)
	}
	// SQLite datetime resolution is 1 second — sleep to guarantee advancement.
	time.Sleep(1100 * time.Millisecond)
	if _, err := db.Exec("UPDATE hitl_sessions SET scene_index = 4 WHERE run_id = 'scp-049-run-1'"); err != nil {
		t.Fatalf("update scene_index: %v", err)
	}
	var after string
	if err := db.QueryRow("SELECT updated_at FROM hitl_sessions WHERE run_id = 'scp-049-run-1'").Scan(&after); err != nil {
		t.Fatalf("select after: %v", err)
	}
	if after == before {
		t.Errorf("updated_at did not advance: before=%q after=%q", before, after)
	}
}

func TestSchema_RunsColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	expected := map[string]string{
		"id":                    "TEXT",
		"scp_id":                "TEXT",
		"stage":                 "TEXT",
		"status":                "TEXT",
		"retry_count":           "INTEGER",
		"retry_reason":          "TEXT",
		"critic_score":          "REAL",
		"cost_usd":              "REAL",
		"token_in":              "INTEGER",
		"token_out":             "INTEGER",
		"duration_ms":           "INTEGER",
		"human_override":        "INTEGER",
		"scenario_path":         "TEXT",
		"character_query_key":   "TEXT",
		"selected_character_id": "TEXT",
		"frozen_descriptor":     "TEXT",
		"output_path":           "TEXT",
		"critic_prompt_version": "TEXT",
		"critic_prompt_hash":    "TEXT",
		"created_at":            "TEXT",
		"updated_at":            "TEXT",
	}

	assertTableColumns(t, db, "runs", expected)
}

func TestSchema_DecisionsColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	expected := map[string]string{
		"id":               "INTEGER",
		"run_id":           "TEXT",
		"scene_id":         "TEXT",
		"decision_type":    "TEXT",
		"context_snapshot": "TEXT",
		"outcome_link":     "TEXT",
		"tags":             "TEXT",
		"feedback_source":  "TEXT",
		"external_ref":     "TEXT",
		"feedback_at":      "TEXT",
		"superseded_by":    "INTEGER",
		"note":             "TEXT",
		"created_at":       "TEXT",
	}

	assertTableColumns(t, db, "decisions", expected)
}

func TestSchema_SegmentsColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	expected := map[string]string{
		"id":              "INTEGER",
		"run_id":          "TEXT",
		"scene_index":     "INTEGER",
		"narration":       "TEXT",
		"shot_count":      "INTEGER",
		"shots":           "TEXT",
		"tts_path":        "TEXT",
		"tts_duration_ms": "INTEGER",
		"clip_path":       "TEXT",
		"critic_score":    "REAL",
		"critic_sub":      "TEXT",
		"status":          "TEXT",
		"review_status":   "TEXT",
		"safeguard_flags": "TEXT",
		"created_at":      "TEXT",
	}

	assertTableColumns(t, db, "segments", expected)
}

func TestMigration006_AddsSceneReviewGateColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	for _, tc := range []struct {
		name string
		typ  string
	}{
		{name: "review_status", typ: "TEXT"},
		{name: "safeguard_flags", typ: "TEXT"},
	} {
		var got string
		if err := db.QueryRow("SELECT type FROM pragma_table_info('segments') WHERE name = ?", tc.name).Scan(&got); err != nil {
			t.Fatalf("column %s not found: %v", tc.name, err)
		}
		if got != tc.typ {
			t.Fatalf("column %s type = %q, want %q", tc.name, got, tc.typ)
		}
	}

	var name string
	if err := db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='index' AND name='idx_segments_run_review_status'",
	).Scan(&name); err != nil {
		t.Fatalf("scene review gate index not found: %v", err)
	}
	if name != "idx_segments_run_review_status" {
		t.Fatalf("index name = %q", name)
	}
}

func TestSchema_SegmentsUniqueConstraint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	// Insert a run first (FK target)
	_, err = db.Exec("INSERT INTO runs (id, scp_id) VALUES ('run-1', 'scp-049')")
	if err != nil {
		t.Fatalf("insert run: %v", err)
	}

	// First segment insert should succeed
	_, err = db.Exec("INSERT INTO segments (run_id, scene_index) VALUES ('run-1', 0)")
	if err != nil {
		t.Fatalf("insert first segment: %v", err)
	}

	// Duplicate (run_id, scene_index) should fail
	_, err = db.Exec("INSERT INTO segments (run_id, scene_index) VALUES ('run-1', 0)")
	if err == nil {
		t.Error("expected UNIQUE constraint violation, got nil error")
	}
}

func TestOpenDB_ForeignKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	var fk int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("query foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d, want 1", fk)
	}
}

func TestOpenDB_ForeignKeyEnforcement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	// Insert segment with non-existent run_id should fail
	_, err = db.Exec("INSERT INTO segments (run_id, scene_index) VALUES ('nonexistent', 0)")
	if err == nil {
		t.Error("expected FK violation for non-existent run_id, got nil error")
	}
}

// assertTableColumns verifies that a table has exactly the expected columns with matching types.
func assertTableColumns(t *testing.T, db *sql.DB, table string, expected map[string]string) {
	t.Helper()

	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatalf("table_info(%s): %v", table, err)
	}
	defer rows.Close()

	found := make(map[string]string)
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan table_info row: %v", err)
		}
		found[name] = colType
	}

	for name, wantType := range expected {
		gotType, ok := found[name]
		if !ok {
			t.Errorf("table %s: missing column %q", table, name)
			continue
		}
		if gotType != wantType {
			t.Errorf("table %s column %q: type = %q, want %q", table, name, gotType, wantType)
		}
	}

	for name := range found {
		if _, ok := expected[name]; !ok {
			t.Errorf("table %s: unexpected column %q", table, name)
		}
	}
}
