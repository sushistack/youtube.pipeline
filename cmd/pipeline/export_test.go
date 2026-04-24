package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/testutil"

	_ "github.com/ncruces/go-sqlite3/driver"
)

func seedExportCommandDB(t *testing.T, dbPath, outputDir string) string {
	t.Helper()
	database, err := db.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	runID := "scp-049-run-3"
	runDir := filepath.Join(outputDir, runID)
	for _, rel := range []string{
		"scenario.json",
		"metadata.json",
		"manifest.json",
		filepath.Join("images", "scene_02", "shot_01.png"),
		filepath.Join("tts", "scene_02.wav"),
	} {
		path := filepath.Join(runDir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	if _, err := database.ExecContext(context.Background(), `
		INSERT INTO runs (id, scp_id, scenario_path, output_path)
		VALUES ('scp-049-run-3', '049', 'scenario.json', ?)`,
		filepath.Join(runDir, "output.mp4"),
	); err != nil {
		t.Fatalf("seed export run: %v", err)
	}
	if _, err := database.ExecContext(context.Background(), `
		INSERT INTO decisions (id, run_id, scene_id, decision_type, note, created_at, superseded_by) VALUES
		  (41, 'scp-049-run-3', '0', 'reject', 'needs rewrite', '2026-04-24T12:34:56Z', 42),
		  (42, 'scp-049-run-3', '0', 'approve', NULL, '2026-04-24T12:35:56Z', NULL)`); err != nil {
		t.Fatalf("seed export decisions: %v", err)
	}
	if _, err := database.ExecContext(context.Background(), `
		INSERT INTO segments (run_id, scene_index, shot_count, shots, tts_path, status) VALUES
		  ('scp-049-run-3', 1, 1, ?, 'tts/scene_02.wav', 'completed')`,
		`[{"image_path":"images/scene_02/shot_01.png","duration_s":3,"transition":"cut","visual_descriptor":"desc"}]`,
	); err != nil {
		t.Fatalf("seed export db: %v", err)
	}
	return runID
}

func writeExportTestConfig(t *testing.T, configPath, dbPath, outputDir string) {
	t.Helper()
	cfg := "db_path: \"" + dbPath + "\"\noutput_dir: \"" + outputDir + "\"\n"
	if err := os.WriteFile(configPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func TestExportCmd_RejectsMissingRunID(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "pipeline.db")
	cfg := filepath.Join(tmp, "config.yaml")
	writeExportTestConfig(t, cfg, dbPath, filepath.Join(tmp, "output"))

	prevCfg, prevJSON := cfgPath, jsonOutput
	cfgPath, jsonOutput = cfg, false
	t.Cleanup(func() { cfgPath, jsonOutput = prevCfg, prevJSON })

	cmd := newExportCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--type", "decisions"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	// Cobra's MarkFlagRequired fires before config/DB load; the error message
	// from Cobra is "required flag(s) \"run-id\" not set".
	if !strings.Contains(err.Error(), `"run-id"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExportCmd_RejectsMissingType(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "pipeline.db")
	cfg := filepath.Join(tmp, "config.yaml")
	writeExportTestConfig(t, cfg, dbPath, filepath.Join(tmp, "output"))

	prevCfg, prevJSON := cfgPath, jsonOutput
	cfgPath, jsonOutput = cfg, false
	t.Cleanup(func() { cfgPath, jsonOutput = prevCfg, prevJSON })

	cmd := newExportCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--run-id", "scp-049-run-3"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `"type"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExportCmd_RejectsNonexistentRunID(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	tmp := t.TempDir()
	outputDir := filepath.Join(tmp, "output")
	dbPath := filepath.Join(tmp, "pipeline.db")
	cfg := filepath.Join(tmp, "config.yaml")
	_ = seedExportCommandDB(t, dbPath, outputDir)
	writeExportTestConfig(t, cfg, dbPath, outputDir)

	prevCfg, prevJSON := cfgPath, jsonOutput
	cfgPath, jsonOutput = cfg, false
	t.Cleanup(func() { cfgPath, jsonOutput = prevCfg, prevJSON })

	cmd := newExportCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--run-id", "ghost-run", "--type", "decisions"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for non-existent run")
	}
	if !strings.Contains(buf.String(), "ghost-run") && !strings.Contains(err.Error(), "ghost-run") {
		t.Fatalf("expected error mentioning run id; got buf=%q err=%v", buf.String(), err)
	}
}

func TestExportCmd_RejectsInvalidType(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "pipeline.db")
	cfg := filepath.Join(tmp, "config.yaml")
	writeExportTestConfig(t, cfg, dbPath, filepath.Join(tmp, "output"))

	prevCfg, prevJSON := cfgPath, jsonOutput
	cfgPath, jsonOutput = cfg, false
	t.Cleanup(func() { cfgPath, jsonOutput = prevCfg, prevJSON })

	cmd := newExportCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--run-id", "scp-049-run-3", "--type", "bogus"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(buf.String(), "invalid --type") {
		t.Fatalf("unexpected output: %s", buf.String())
	}
}

func TestExportCmd_RejectsInvalidFormat(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "pipeline.db")
	cfg := filepath.Join(tmp, "config.yaml")
	writeExportTestConfig(t, cfg, dbPath, filepath.Join(tmp, "output"))

	prevCfg, prevJSON := cfgPath, jsonOutput
	cfgPath, jsonOutput = cfg, false
	t.Cleanup(func() { cfgPath, jsonOutput = prevCfg, prevJSON })

	cmd := newExportCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--run-id", "scp-049-run-3", "--type", "decisions", "--format", "yaml"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(buf.String(), "invalid --format") {
		t.Fatalf("unexpected output: %s", buf.String())
	}
}

func TestExportCmd_DecisionsJSON_WritesEnvelope(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	tmp := t.TempDir()
	outputDir := filepath.Join(tmp, "output")
	dbPath := filepath.Join(tmp, "pipeline.db")
	cfg := filepath.Join(tmp, "config.yaml")
	runID := seedExportCommandDB(t, dbPath, outputDir)
	writeExportTestConfig(t, cfg, dbPath, outputDir)

	prevCfg, prevJSON := cfgPath, jsonOutput
	cfgPath, jsonOutput = cfg, false
	t.Cleanup(func() { cfgPath, jsonOutput = prevCfg, prevJSON })

	cmd := newExportCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--run-id", runID, "--type", "decisions"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	exportPath := filepath.Join(outputDir, runID, "export", "decisions.json")
	raw, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	var env struct {
		Version int `json:"version"`
		Data    []struct {
			DecisionID   int64  `json:"decision_id"`
			TargetItem   string `json:"target_item"`
			SupersededBy *int64 `json:"superseded_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal export: %v", err)
	}
	if env.Version != 1 {
		t.Fatalf("version = %d, want 1", env.Version)
	}
	if len(env.Data) != 2 {
		t.Fatalf("rows = %d, want 2", len(env.Data))
	}
	if env.Data[0].TargetItem != "scene:0" {
		t.Fatalf("target_item = %q", env.Data[0].TargetItem)
	}
	if env.Data[0].SupersededBy == nil || *env.Data[0].SupersededBy != 42 {
		t.Fatalf("superseded_by missing: %+v", env.Data[0])
	}
}

func TestExportCmd_ArtifactsJSON_WritesRelativePaths(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	tmp := t.TempDir()
	outputDir := filepath.Join(tmp, "output")
	dbPath := filepath.Join(tmp, "pipeline.db")
	cfg := filepath.Join(tmp, "config.yaml")
	runID := seedExportCommandDB(t, dbPath, outputDir)
	writeExportTestConfig(t, cfg, dbPath, outputDir)

	prevCfg, prevJSON := cfgPath, jsonOutput
	cfgPath, jsonOutput = cfg, false
	t.Cleanup(func() { cfgPath, jsonOutput = prevCfg, prevJSON })

	cmd := newExportCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--run-id", runID, "--type", "artifacts"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	exportPath := filepath.Join(outputDir, runID, "export", "artifacts.json")
	raw, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	var env struct {
		Version int `json:"version"`
		Data    []struct {
			Path string `json:"path"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal export: %v", err)
	}
	if env.Version != 1 {
		t.Fatalf("version = %d, want 1", env.Version)
	}
	for _, row := range env.Data {
		if filepath.IsAbs(row.Path) {
			t.Fatalf("expected relative path, got %q", row.Path)
		}
	}
}

func TestExportCmd_CSV_WritesStableFilesForBothTypes(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	tmp := t.TempDir()
	outputDir := filepath.Join(tmp, "output")
	dbPath := filepath.Join(tmp, "pipeline.db")
	cfg := filepath.Join(tmp, "config.yaml")
	runID := seedExportCommandDB(t, dbPath, outputDir)
	writeExportTestConfig(t, cfg, dbPath, outputDir)

	prevCfg, prevJSON := cfgPath, jsonOutput
	cfgPath, jsonOutput = cfg, false
	t.Cleanup(func() { cfgPath, jsonOutput = prevCfg, prevJSON })

	for _, tc := range []struct {
		exportType string
		filename   string
		header     []string
	}{
		{"decisions", "decisions.csv", []string{"decision_id", "run_id", "scene_id", "target_item", "decision_type", "created_at", "note", "superseded_by"}},
		{"artifacts", "artifacts.csv", []string{"artifact_type", "scene_index", "shot_index", "path"}},
	} {
		cmd := newExportCmd()
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		cmd.SetArgs([]string{"--run-id", runID, "--type", tc.exportType, "--format", "csv"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute %s: %v", tc.exportType, err)
		}

		f, err := os.Open(filepath.Join(outputDir, runID, "export", tc.filename))
		if err != nil {
			t.Fatalf("open %s: %v", tc.filename, err)
		}
		rows, err := csv.NewReader(f).ReadAll()
		f.Close()
		if err != nil {
			t.Fatalf("read %s: %v", tc.filename, err)
		}
		if len(rows) < 2 {
			t.Fatalf("%s missing data rows", tc.filename)
		}
		for i, want := range tc.header {
			if rows[0][i] != want {
				t.Fatalf("%s header[%d] = %q, want %q", tc.filename, i, rows[0][i], want)
			}
		}
	}
}
