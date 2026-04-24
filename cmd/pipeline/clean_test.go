package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// seedCleanCommandDB creates a DB + config.yaml under t.TempDir and seeds
// one terminal run old enough to be eligible for Soft Archive, plus one
// recent run that must be preserved. Returns the cfg.yaml path, the DB
// path, and the run IDs in order [archivable, recent].
func seedCleanCommandDB(t *testing.T, outputDir string, retentionDays int) (string, string, []string) {
	t.Helper()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "pipeline.db")
	database, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	database.SetMaxOpenConns(1)

	if _, err := database.Exec("PRAGMA journal_mode=wal"); err != nil {
		t.Fatalf("wal: %v", err)
	}
	if _, err := database.Exec("PRAGMA busy_timeout=5000"); err != nil {
		t.Fatalf("busy_timeout: %v", err)
	}
	if _, err := database.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("foreign_keys: %v", err)
	}
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	archivableID := "scp-049-run-1"
	recentID := "scp-050-run-1"

	// Seed both runs with scenario_path and output_path populated so the
	// archive count has something to null.
	ctx := context.Background()
	for _, row := range []struct {
		id, scpID, status, updatedAt string
	}{
		{archivableID, "049", string(domain.StatusCompleted), "2026-01-01 00:00:00"},
		{recentID, "050", string(domain.StatusCompleted), "2026-04-23 00:00:00"},
	} {
		if _, err := database.ExecContext(ctx,
			`INSERT INTO runs (id, scp_id, stage, status, scenario_path, output_path, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			row.id, row.scpID, string(domain.StageComplete), row.status,
			"scenario.json", "output.mp4", row.updatedAt,
		); err != nil {
			t.Fatalf("seed run %s: %v", row.id, err)
		}
	}
	database.Close()

	// Create artifact files for both runs under outputDir.
	for _, id := range []string{archivableID, recentID} {
		mustWriteCleanFile(t, filepath.Join(outputDir, id, "scenario.json"), "{}")
		mustWriteCleanFile(t, filepath.Join(outputDir, id, "output.mp4"), "video")
		mustWriteCleanFile(t, filepath.Join(outputDir, id, "images", "scene_01", "shot_01.png"), "img")
	}

	cfgPath := filepath.Join(tmp, "config.yaml")
	cfgYaml := "db_path: \"" + dbPath + "\"\n" +
		"output_dir: \"" + outputDir + "\"\n" +
		"artifact_retention_days: " + itoa(retentionDays) + "\n"
	if err := os.WriteFile(cfgPath, []byte(cfgYaml), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath, dbPath, []string{archivableID, recentID}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	buf := ""
	for n > 0 {
		d := n % 10
		buf = string(rune('0'+d)) + buf
		n /= 10
	}
	return sign + buf
}

func mustWriteCleanFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestCleanCmd_JSONEnvelope_Golden(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	outputDir := t.TempDir()
	cfg, dbPath, ids := seedCleanCommandDB(t, outputDir, 30)
	_ = dbPath
	archivableID, recentID := ids[0], ids[1]

	prevCfg, prevJSON, prevClock := cfgPath, jsonOutput, cleanClock
	cfgPath, jsonOutput, cleanClock = cfg, true, clock.NewFakeClock(time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC))
	defer func() {
		cfgPath, jsonOutput, cleanClock = prevCfg, prevJSON, prevClock
	}()

	cmd := newCleanCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var env struct {
		Version int `json:"version"`
		Data    struct {
			RetentionDays int    `json:"retention_days"`
			CutoffUTC     string `json:"cutoff_utc"`
			RunsScanned   int    `json:"runs_scanned"`
			RunsArchived  int    `json:"runs_archived"`
			FilesDeleted  int    `json:"files_deleted"`
			DBRefsCleared int    `json:"db_refs_cleared"`
			Vacuum        string `json:"vacuum"`
			ArchivedRuns  []struct {
				ID string `json:"id"`
			} `json:"archived_runs"`
		} `json:"data"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v\nbuf=%s", err, buf.String())
	}

	testutil.AssertEqual(t, env.Version, 1)
	testutil.AssertEqual(t, env.Data.RetentionDays, 30)
	testutil.AssertEqual(t, env.Data.RunsScanned, 1)
	testutil.AssertEqual(t, env.Data.RunsArchived, 1)
	testutil.AssertEqual(t, env.Data.Vacuum, "ran")
	if len(env.Data.ArchivedRuns) != 1 || env.Data.ArchivedRuns[0].ID != archivableID {
		t.Fatalf("archived_runs: got %v want [%s]", env.Data.ArchivedRuns, archivableID)
	}
	// Recent run's artifacts must survive.
	if _, err := os.Stat(filepath.Join(outputDir, recentID, "scenario.json")); err != nil {
		t.Errorf("recent run's scenario.json was removed: %v", err)
	}
	// Archived run's artifacts must be gone.
	if _, err := os.Stat(filepath.Join(outputDir, archivableID, "scenario.json")); !os.IsNotExist(err) {
		t.Errorf("archived run's scenario.json still present: %v", err)
	}
}

func TestCleanCmd_HumanOutputIncludesSummary(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	outputDir := t.TempDir()
	cfg, _, _ := seedCleanCommandDB(t, outputDir, 30)

	prevCfg, prevJSON, prevClock := cfgPath, jsonOutput, cleanClock
	cfgPath, jsonOutput, cleanClock = cfg, false, clock.NewFakeClock(time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC))
	defer func() {
		cfgPath, jsonOutput, cleanClock = prevCfg, prevJSON, prevClock
	}()

	cmd := newCleanCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := buf.String()
	for _, want := range []string{
		"Soft archive complete",
		"Retention:",
		"Runs archived:",
		"VACUUM:",
	} {
		if !contains(got, want) {
			t.Errorf("human output missing %q\n---\n%s", want, got)
		}
	}
}

func TestCleanCmd_RejectsInvalidRetentionConfig(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "pipeline.db")

	// Construct config.yaml with artifact_retention_days=0 and an otherwise
	// valid DB path. loader.Load must fail with ErrValidation before the
	// command ever calls the service.
	cfg := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(cfg, []byte(
		"db_path: \""+dbPath+"\"\n"+
			"artifact_retention_days: 0\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	prevCfg, prevJSON := cfgPath, jsonOutput
	cfgPath, jsonOutput = cfg, false
	defer func() {
		cfgPath, jsonOutput = prevCfg, prevJSON
	}()

	cmd := newCleanCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid retention config")
	}
}

func contains(hay, needle string) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
