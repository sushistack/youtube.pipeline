package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/testutil"

	_ "github.com/ncruces/go-sqlite3/driver"
)

func TestMetricsCmd_HumanOutput_Golden(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	dbPath, cfg := seedMetricsCommandDB(t)
	_ = dbPath
	prevCfgPath, prevJSON, prevClock := cfgPath, jsonOutput, metricsClock
	cfgPath, jsonOutput, metricsClock = cfg, false, clock.NewFakeClock(time.Date(2026, 4, 18, 12, 34, 56, 0, time.UTC))
	defer func() {
		cfgPath, jsonOutput, metricsClock = prevCfgPath, prevJSON, prevClock
		resetMetricsGlobals()
	}()

	cmd := newMetricsCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--window", "25", "--regression-rate", writeFloatFile(t, "regression.txt", "0.82"), "--idempotency-rate", writeFloatFile(t, "idempotency.txt", "1.0")})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	want := string(testutil.LoadFixture(t, filepath.Join("golden", "cli_metrics_human.txt")))
	if buf.String() != want {
		t.Fatalf("human output mismatch\n--- got ---\n%s\n--- want ---\n%s", buf.String(), want)
	}
}

func TestMetricsCmd_JSONOutput_Golden(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	_, cfg := seedMetricsCommandDB(t)
	prevCfgPath, prevJSON, prevClock := cfgPath, jsonOutput, metricsClock
	cfgPath, jsonOutput, metricsClock = cfg, true, clock.NewFakeClock(time.Date(2026, 4, 18, 12, 34, 56, 0, time.UTC))
	defer func() {
		cfgPath, jsonOutput, metricsClock = prevCfgPath, prevJSON, prevClock
		resetMetricsGlobals()
	}()

	cmd := newMetricsCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--window", "25", "--regression-rate", writeFloatFile(t, "regression.txt", "0.82"), "--idempotency-rate", writeFloatFile(t, "idempotency.txt", "1.0")})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	want := string(testutil.LoadFixture(t, filepath.Join("golden", "cli_metrics_json.json")))
	testutil.AssertJSONEq(t, buf.String(), want)

	var env struct {
		Version int `json:"version"`
		Data    struct {
			Window  int `json:"window"`
			Metrics []struct {
				ID          string   `json:"id"`
				Value       *float64 `json:"value"`
				Reason      string   `json:"reason"`
				Unavailable bool     `json:"unavailable"`
			} `json:"metrics"`
		} `json:"data"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	testutil.AssertEqual(t, env.Version, 1)
	testutil.AssertEqual(t, env.Data.Window, 25)
	testutil.AssertEqual(t, len(env.Data.Metrics), 5)
	testutil.AssertEqual(t, env.Data.Metrics[0].ID, "automation_rate")
	testutil.AssertEqual(t, env.Data.Metrics[4].ID, "resume_idempotency")
}

func TestMetricsCmd_ValidationErrorOnZeroWindow(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	_, cfg := seedMetricsCommandDB(t)
	prevCfgPath, prevJSON, prevClock := cfgPath, jsonOutput, metricsClock
	cfgPath, jsonOutput, metricsClock = cfg, false, clock.NewFakeClock(time.Date(2026, 4, 18, 12, 34, 56, 0, time.UTC))
	defer func() {
		cfgPath, jsonOutput, metricsClock = prevCfgPath, prevJSON, prevClock
		resetMetricsGlobals()
	}()

	cmd := newMetricsCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--window", "0"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for zero window")
	}
	if !strings.Contains(buf.String(), "window 0 must be > 0") {
		t.Fatalf("expected validation error in output, got: %s", buf.String())
	}
}

func TestMetricsCmd_RegressionFile_ParsedAndPassed(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	_, cfg := seedMetricsCommandDB(t)
	prevCfgPath, prevJSON, prevClock := cfgPath, jsonOutput, metricsClock
	cfgPath, jsonOutput, metricsClock = cfg, true, clock.NewFakeClock(time.Date(2026, 4, 18, 12, 34, 56, 0, time.UTC))
	defer func() {
		cfgPath, jsonOutput, metricsClock = prevCfgPath, prevJSON, prevClock
		resetMetricsGlobals()
	}()

	cmd := newMetricsCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--regression-rate", writeFloatFile(t, "regression.txt", "0.91")})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), `"critic_regression_detection"`) || !strings.Contains(buf.String(), `"value":0.91`) {
		t.Fatalf("expected regression value in JSON output, got: %s", buf.String())
	}
}

func TestMetricsCmd_IdempotencyFile_ParsedAndPassed(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	_, cfg := seedMetricsCommandDB(t)
	prevCfgPath, prevJSON, prevClock := cfgPath, jsonOutput, metricsClock
	cfgPath, jsonOutput, metricsClock = cfg, true, clock.NewFakeClock(time.Date(2026, 4, 18, 12, 34, 56, 0, time.UTC))
	defer func() {
		cfgPath, jsonOutput, metricsClock = prevCfgPath, prevJSON, prevClock
		resetMetricsGlobals()
	}()

	cmd := newMetricsCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--idempotency-rate", writeFloatFile(t, "idempotency.txt", "1.0")})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var env struct {
		Data struct {
			Metrics []struct {
				ID    string   `json:"id"`
				Value *float64 `json:"value"`
			} `json:"metrics"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(buf.String()), &env); err != nil {
		t.Fatalf("unmarshal JSON: %v", err)
	}
	var found bool
	for _, m := range env.Data.Metrics {
		if m.ID == "resume_idempotency" {
			if m.Value == nil || *m.Value != 1.0 {
				t.Fatalf("resume_idempotency value: want 1.0, got %v", m.Value)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("resume_idempotency metric not found in JSON output")
	}
}

func TestMetricsCmd_RegressionFileMissing_PropagatesError(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	_, cfg := seedMetricsCommandDB(t)
	prevCfgPath, prevJSON, prevClock := cfgPath, jsonOutput, metricsClock
	cfgPath, jsonOutput, metricsClock = cfg, false, clock.NewFakeClock(time.Date(2026, 4, 18, 12, 34, 56, 0, time.UTC))
	defer func() {
		cfgPath, jsonOutput, metricsClock = prevCfgPath, prevJSON, prevClock
		resetMetricsGlobals()
	}()

	cmd := newMetricsCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--regression-rate", filepath.Join(t.TempDir(), "missing.txt")})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing regression file")
	}
	var silent *silentErr
	if !errors.As(err, &silent) {
		t.Fatalf("expected silentErr, got %T", err)
	}
	if !strings.Contains(buf.String(), "missing.txt") {
		t.Fatalf("expected missing-file path in output, got: %s", buf.String())
	}
}

func TestMetricsCmd_PersistsCalibrationSnapshot(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	dbPath, cfg := seedMetricsCommandDB(t)
	prevCfgPath, prevJSON, prevClock := cfgPath, jsonOutput, metricsClock
	cfgPath, jsonOutput, metricsClock = cfg, false, clock.NewFakeClock(time.Date(2026, 4, 18, 12, 34, 56, 0, time.UTC))
	defer func() {
		cfgPath, jsonOutput, metricsClock = prevCfgPath, prevJSON, prevClock
		resetMetricsGlobals()
	}()

	cmd := newMetricsCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--window", "25", "--regression-rate", writeFloatFile(t, "regression.txt", "0.82"), "--idempotency-rate", writeFloatFile(t, "idempotency.txt", "1.0")})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	database, err := db.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer database.Close()

	var (
		windowSize       int
		windowCount      int
		provisional      int
		threshold        float64
		kappa            sql.NullFloat64
		reason           sql.NullString
		latestDecisionID int
		computedAt       string
	)
	if err := database.QueryRow(`
		SELECT window_size, window_count, provisional, calibration_threshold, kappa, reason, latest_decision_id, computed_at
		  FROM critic_calibration_snapshots`,
	).Scan(&windowSize, &windowCount, &provisional, &threshold, &kappa, &reason, &latestDecisionID, &computedAt); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	testutil.AssertEqual(t, windowSize, 25)
	testutil.AssertEqual(t, windowCount, 25)
	testutil.AssertEqual(t, provisional, 0)
	testutil.AssertFloatNear(t, threshold, 0.70, 1e-9)
	if !kappa.Valid {
		t.Fatal("expected persisted kappa value")
	}
	testutil.AssertFloatNear(t, kappa.Float64, 0.714828897338403, 1e-9)
	testutil.AssertEqual(t, reason.Valid, false)
	testutil.AssertEqual(t, latestDecisionID > 0, true)
	testutil.AssertEqual(t, computedAt, "2026-04-18T12:34:56Z")
}

func seedMetricsCommandDB(t *testing.T) (string, string) {
	t.Helper()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "pipeline.db")
	database, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	database.SetMaxOpenConns(1)
	defer database.Close()
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
	if _, err := database.Exec(string(testutil.LoadFixture(t, filepath.Join("fixtures", "metrics_seed.sql")))); err != nil {
		t.Fatalf("seed fixture: %v", err)
	}
	cfgPath := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("db_path: \""+dbPath+"\"\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return dbPath, cfgPath
}

func writeFloatFile(t *testing.T, name, value string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(value), 0644); err != nil {
		t.Fatalf("write float file: %v", err)
	}
	return path
}

func resetMetricsGlobals() {
	metricsWindow = 25
	metricsCalibrationThreshold = 0.70
	metricsRegressionFile = ""
	metricsIdempotencyFile = ""
}
