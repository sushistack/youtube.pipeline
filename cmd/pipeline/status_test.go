package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/testutil"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// seedStatusTestDB opens (or creates) a file DB at dbPath, runs migrations,
// and executes the given SQL. The caller is responsible for closing if needed.
func seedStatusTestDB(t *testing.T, dbPath, seedSQL string) {
	t.Helper()
	database, err := db.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	defer database.Close()
	if _, err := database.Exec(seedSQL); err != nil {
		t.Fatalf("seed db: %v", err)
	}
}

// writeStatusTestConfig writes a minimal config.yaml for status command tests.
func writeStatusTestConfig(t *testing.T, configPath, dbPath string) {
	t.Helper()
	cfg := "db_path: \"" + dbPath + "\"\noutput_dir: \"" + filepath.Dir(dbPath) + "/output\"\n"
	if err := os.WriteFile(configPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func TestStatusCmd_Human_PausedShowsChanges(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "pipeline.db")
	configPath := filepath.Join(tmp, "config.yaml")

	seedStatusTestDB(t, dbPath, `
		INSERT INTO runs (id, scp_id, stage, status, created_at, updated_at)
		VALUES ('scp-049-run-cmd', '049', 'batch_review', 'waiting', '2026-01-01T00:00:00Z', '2026-01-01T01:00:00Z');
		INSERT INTO segments (run_id, scene_index, narration, shot_count, status)
		VALUES ('scp-049-run-cmd', 0, 'Scene 0', 1, 'completed'),
		       ('scp-049-run-cmd', 1, 'Scene 1', 1, 'completed'),
		       ('scp-049-run-cmd', 2, 'Scene 2', 1, 'completed');
		INSERT INTO decisions (run_id, scene_id, decision_type, created_at)
		VALUES ('scp-049-run-cmd', '0', 'approve', '2026-01-01T00:10:00Z'),
		       ('scp-049-run-cmd', '1', 'approve', '2026-01-01T00:20:00Z'),
		       ('scp-049-run-cmd', '2', 'approve', '2026-01-01T00:50:00Z');
		INSERT INTO hitl_sessions (run_id, stage, scene_index, last_interaction_timestamp, snapshot_json, created_at, updated_at)
		VALUES ('scp-049-run-cmd', 'batch_review', 2, '2026-01-01T00:25:00Z',
		  '{"total_scenes":3,"approved_count":2,"rejected_count":0,"pending_count":1,"scene_statuses":{"0":"approved","1":"approved","2":"pending"}}',
		  '2026-01-01T00:00:00Z', '2026-01-01T00:25:00Z');
	`)
	writeStatusTestConfig(t, configPath, dbPath)

	prevCfg := cfgPath
	cfgPath = configPath
	t.Cleanup(func() { cfgPath = prevCfg })

	cmd := newStatusCmd()
	cmd.SetContext(context.Background())
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	if err := cmd.RunE(cmd, []string{"scp-049-run-cmd"}); err != nil {
		t.Fatalf("status failed: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "Summary:") {
		t.Errorf("expected Summary: in output, got: %s", out)
	}
	if !strings.Contains(out, "Changes since last interaction") {
		t.Errorf("expected 'Changes since last interaction' in output, got: %s", out)
	}
	if !strings.Contains(out, "scene 2: pending") {
		t.Errorf("expected scene 2 change in output, got: %s", out)
	}
}

func TestStatusCmd_Human_NotPausedOmitsSummary(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "pipeline.db")
	configPath := filepath.Join(tmp, "config.yaml")

	seedStatusTestDB(t, dbPath, `
		INSERT INTO runs (id, scp_id, stage, status, created_at, updated_at)
		VALUES ('r-running', '999', 'write', 'running', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
	`)
	writeStatusTestConfig(t, configPath, dbPath)

	prevCfg := cfgPath
	cfgPath = configPath
	t.Cleanup(func() { cfgPath = prevCfg })

	cmd := newStatusCmd()
	cmd.SetContext(context.Background())
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	if err := cmd.RunE(cmd, []string{"r-running"}); err != nil {
		t.Fatalf("status failed: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "Summary:") {
		t.Errorf("non-HITL run should not have Summary:, got: %s", out)
	}
	if strings.Contains(out, "Changes since") {
		t.Errorf("non-HITL run should not have 'Changes since', got: %s", out)
	}
}

func TestStatusCmd_JSON_Paused_GoldenMatch(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "pipeline.db")
	configPath := filepath.Join(tmp, "config.yaml")

	seedStatusTestDB(t, dbPath, `
		INSERT INTO runs (id, scp_id, stage, status, retry_count, cost_usd, token_in, token_out, duration_ms, human_override, created_at, updated_at)
		VALUES ('scp-049-run-json', '049', 'batch_review', 'waiting', 0, 1.25, 15000, 3000, 45000, 0, '2026-01-01T00:00:00Z', '2026-01-01T00:30:00Z');
		INSERT INTO segments (run_id, scene_index, status)
		VALUES ('scp-049-run-json', 0, 'completed'),
		       ('scp-049-run-json', 1, 'completed'),
		       ('scp-049-run-json', 2, 'pending');
		INSERT INTO decisions (run_id, scene_id, decision_type, created_at)
		VALUES ('scp-049-run-json', '0', 'approve', '2026-01-01T00:20:00Z'),
		       ('scp-049-run-json', '1', 'approve', '2026-01-01T00:25:00Z');
		INSERT INTO hitl_sessions (run_id, stage, scene_index, last_interaction_timestamp, snapshot_json, created_at, updated_at)
		VALUES ('scp-049-run-json', 'batch_review', 2, '2026-01-01T00:25:00Z',
		  '{"total_scenes":3,"approved_count":2,"rejected_count":0,"pending_count":1,"scene_statuses":{"0":"approved","1":"approved","2":"pending"}}',
		  '2026-01-01T00:00:00Z', '2026-01-01T00:25:00Z');
	`)
	writeStatusTestConfig(t, configPath, dbPath)

	prevCfg := cfgPath
	cfgPath = configPath
	prevJSON := jsonOutput
	jsonOutput = true
	t.Cleanup(func() {
		cfgPath = prevCfg
		jsonOutput = prevJSON
	})

	cmd := newStatusCmd()
	cmd.SetContext(context.Background())
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	if err := cmd.RunE(cmd, []string{"scp-049-run-json"}); err != nil {
		t.Fatalf("status --json failed: %v", err)
	}

	var env struct {
		Version int            `json:"version"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Version != 1 {
		t.Errorf("expected version=1, got %d", env.Version)
	}
	// CLI renders RunOutput flat (id, stage, etc. at top level of data).
	if _, ok := env.Data["id"]; !ok {
		t.Errorf("expected 'id' key in data, got %v", env.Data)
	}
	if _, ok := env.Data["paused_position"]; !ok {
		t.Errorf("expected 'paused_position' key in data, got %v", env.Data)
	}
	if _, ok := env.Data["decisions_summary"]; !ok {
		t.Errorf("expected 'decisions_summary' key in data, got %v", env.Data)
	}
	wantSummary := "Run scp-049-run-json: reviewing scene 3 of 3, 2 approved, 0 rejected"
	if got, _ := env.Data["summary"].(string); got != wantSummary {
		t.Errorf("summary = %q, want %q", got, wantSummary)
	}
}
