package service

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/config"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestSettingsService_SaveQueuedWhenActiveRunExists(t *testing.T) {
	testDB := testutil.NewTestDB(t)
	insertActiveRun(t, testDB, "scp-049-run-1", "running")

	svc, configPath := newSettingsTestService(t, testDB)
	initialConfigBytes, _ := os.ReadFile(configPath)

	snapshot, err := svc.Save(context.Background(), validSettingsInput(), nil)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if snapshot.Application.Status != "queued" {
		t.Fatalf("application status = %q, want queued", snapshot.Application.Status)
	}

	// D1 guarantee: disk must NOT be rewritten while active runs exist.
	postSaveConfigBytes, _ := os.ReadFile(configPath)
	if string(initialConfigBytes) != string(postSaveConfigBytes) {
		t.Fatalf("config.yaml was rewritten during queued save (disk leak)")
	}

	cfg, err := svc.LoadEffectiveRuntimeConfig(context.Background())
	if err != nil {
		t.Fatalf("LoadEffectiveRuntimeConfig() error = %v", err)
	}
	if cfg.WriterModel == "deepseek-chat-v2" {
		t.Fatalf("effective runtime config unexpectedly promoted queued version")
	}

	promoted, err := svc.PromotePendingAtSafeSeam(context.Background())
	if err != nil {
		t.Fatalf("PromotePendingAtSafeSeam() error = %v", err)
	}
	if !promoted {
		t.Fatal("PromotePendingAtSafeSeam() = false, want true")
	}

	cfg, err = svc.LoadEffectiveRuntimeConfig(context.Background())
	if err != nil {
		t.Fatalf("LoadEffectiveRuntimeConfig() after promote error = %v", err)
	}
	if cfg.WriterModel != "deepseek-chat-v2" {
		t.Fatalf("effective writer_model = %q, want deepseek-chat-v2", cfg.WriterModel)
	}

	// After promotion, disk must reflect the promoted version.
	promotedBytes, _ := os.ReadFile(configPath)
	if !strings.Contains(string(promotedBytes), "deepseek-chat-v2") {
		t.Fatalf("config.yaml not materialized after promotion; contents=%s", string(promotedBytes))
	}
}

func TestSettingsService_SaveWritesDiskWhenNoActiveRuns(t *testing.T) {
	testDB := testutil.NewTestDB(t)
	svc, configPath := newSettingsTestService(t, testDB)

	snapshot, err := svc.Save(context.Background(), validSettingsInput(), nil)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if snapshot.Application.Status != "effective" {
		t.Fatalf("application status = %q, want effective", snapshot.Application.Status)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "deepseek-chat-v2") {
		t.Fatalf("config.yaml does not contain saved value; contents=%s", string(data))
	}
}

func TestSettingsService_SaveRejectsValidationErrors(t *testing.T) {
	testDB := testutil.NewTestDB(t)
	svc, _ := newSettingsTestService(t, testDB)

	_, err := svc.Save(context.Background(), SettingsUpdateInput{
		Config: SettingsConfigInput{
			WriterModel:     "writer",
			CriticModel:     "critic",
			ImageModel:      "image",
			TTSModel:        "tts",
			TTSVoice:        "voice",
			TTSAudioFormat:  "wav",
			WriterProvider:  "same",
			CriticProvider:  "same",
			ImageProvider:   "dashscope",
			TTSProvider:     "dashscope",
			DashScopeRegion: "cn-beijing",
			CostCapResearch: 0.5,
			CostCapWrite:    0.5,
			CostCapImage:    2,
			CostCapTTS:      1,
			CostCapAssemble: 0.1,
			CostCapPerRun:   1,
		},
		Env: map[string]*string{
			"UNSUPPORTED_KEY": ptr("x"),
		},
	}, nil)
	if err == nil {
		t.Fatal("Save() error = nil, want validation error")
	}

	var validationErr *SettingsValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("Save() error = %T, want SettingsValidationError", err)
	}
	if validationErr.FieldErrors["config.writer_provider"] == "" {
		t.Fatal("expected writer_provider field error")
	}
	if validationErr.FieldErrors["env.UNSUPPORTED_KEY"] == "" {
		t.Fatal("expected unsupported key field error")
	}

	if !errors.Is(err, domain.ErrValidation) {
		t.Fatal("validation error does not satisfy errors.Is(domain.ErrValidation)")
	}
}

func TestSettingsService_SaveRejectsIfMatchMismatch(t *testing.T) {
	testDB := testutil.NewTestDB(t)
	svc, _ := newSettingsTestService(t, testDB)

	// Bootstrap sets effective_version=1, so a caller passing version=99 as
	// If-Match should see a conflict.
	if err := svc.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	bogus := int64(99)
	_, err := svc.Save(context.Background(), validSettingsInput(), &bogus)
	if !errors.Is(err, ErrSettingsConflict) {
		t.Fatalf("Save with stale If-Match = %v, want ErrSettingsConflict", err)
	}
}

func TestSettingsService_SnapshotBuildsBudgetSummary(t *testing.T) {
	testDB := testutil.NewTestDB(t)
	insertActiveRun(t, testDB, "scp-049-run-2", "running")
	if _, err := testDB.Exec(`UPDATE runs SET cost_usd = 4.25 WHERE id = ?`, "scp-049-run-2"); err != nil {
		t.Fatalf("seed budget cost: %v", err)
	}

	svc, _ := newSettingsTestService(t, testDB)
	snapshot, err := svc.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.Budget.Status != "near_cap" {
		t.Fatalf("budget status = %q, want near_cap", snapshot.Budget.Status)
	}
	if snapshot.Budget.Source.Kind != "active_run" {
		t.Fatalf("budget source kind = %q, want active_run", snapshot.Budget.Source.Kind)
	}
}

func TestSettingsService_BudgetPrefersRunningOverFailed(t *testing.T) {
	testDB := testutil.NewTestDB(t)
	// Older running run — should still win over a recently-failed run.
	insertActiveRunAt(t, testDB, "run-running", "running", 4.0, "2026-01-01T00:00:00Z")
	insertActiveRunAt(t, testDB, "run-failed", "failed", 9.0, "2026-04-01T00:00:00Z")

	svc, _ := newSettingsTestService(t, testDB)
	snapshot, err := svc.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snapshot.Budget.Source.RunID == nil || *snapshot.Budget.Source.RunID != "run-running" {
		t.Fatalf("budget should prefer running run over failed run; got %+v", snapshot.Budget.Source)
	}
}

func TestSettingsService_LoadRuntimeFilesForRun_UsesPinnedVersion(t *testing.T) {
	testDB := testutil.NewTestDB(t)
	svc, _ := newSettingsTestService(t, testDB)

	// Save an initial effective version (v1).
	if _, err := svc.Save(context.Background(), validSettingsInput(), nil); err != nil {
		t.Fatalf("initial save: %v", err)
	}
	// Simulate a run being created against v1.
	insertActiveRun(t, testDB, "pinned-run", "running")
	if err := svc.AssignRunToEffectiveVersion(context.Background(), "pinned-run"); err != nil {
		t.Fatalf("assign: %v", err)
	}

	// A second save while the run is active is queued (v2 = pending).
	second := validSettingsInput()
	second.Config.WriterModel = "writer-v2"
	if _, err := svc.Save(context.Background(), second, nil); err != nil {
		t.Fatalf("second save: %v", err)
	}

	// The pinned run must still resolve to v1 even though the most-recent
	// version is v2 (queued pending). Engine-level promotion would normally
	// fire at the next stage boundary; until then, active stages MUST keep
	// the snapshot they started with.
	files, err := svc.LoadRuntimeFilesForRun(context.Background(), "pinned-run")
	if err != nil {
		t.Fatalf("LoadRuntimeFilesForRun: %v", err)
	}
	if files.Config.WriterModel == "writer-v2" {
		t.Fatalf("pinned run adopted queued version v2; want v1 writer-v2 only after promotion")
	}
}

func TestSettingsService_DBDoesNotStoreRawSecrets(t *testing.T) {
	testDB := testutil.NewTestDB(t)
	svc, _ := newSettingsTestService(t, testDB)

	input := validSettingsInput()
	input.Env[domain.SettingsSecretDashScope] = ptr("SUPER-SECRET-AKIA")
	if _, err := svc.Save(context.Background(), input, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var configJSON string
	var fingerprint string
	row := testDB.QueryRow(`SELECT config_json, env_fingerprint FROM settings_versions ORDER BY version DESC LIMIT 1`)
	if err := row.Scan(&configJSON, &fingerprint); err != nil {
		t.Fatalf("scan version row: %v", err)
	}
	if strings.Contains(configJSON, "SUPER-SECRET-AKIA") {
		t.Fatalf("raw secret leaked into config_json: %s", configJSON)
	}
	if strings.Contains(fingerprint, "SUPER-SECRET-AKIA") {
		t.Fatalf("raw secret leaked into fingerprint: %s", fingerprint)
	}
	if len(fingerprint) != 64 { // hex-encoded SHA-256
		t.Fatalf("fingerprint length = %d, want 64", len(fingerprint))
	}
}

func validSettingsInput() SettingsUpdateInput {
	return SettingsUpdateInput{
		Config: SettingsConfigInput{
			WriterModel:     "deepseek-chat-v2",
			CriticModel:     "gemini-2.0-flash",
			ImageModel:      "qwen-max-vl",
			TTSModel:        "qwen3-tts",
			TTSVoice:        "longhua",
			TTSAudioFormat:  "wav",
			WriterProvider:  "deepseek",
			CriticProvider:  "gemini",
			ImageProvider:   "dashscope",
			TTSProvider:     "dashscope",
			DashScopeRegion: "cn-beijing",
			CostCapResearch: 0.5,
			CostCapWrite:    0.5,
			CostCapImage:    2,
			CostCapTTS:      1,
			CostCapAssemble: 0.1,
			CostCapPerRun:   5,
		},
		Env: map[string]*string{
			domain.SettingsSecretDashScope: ptr("dashscope-secret"),
			domain.SettingsSecretDeepSeek:  ptr("deepseek-secret"),
			domain.SettingsSecretGemini:    ptr("gemini-secret"),
		},
	}
}

func newSettingsTestService(t *testing.T, database *sql.DB) (*SettingsService, string) {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	envPath := filepath.Join(filepath.Dir(configPath), ".env")

	initial := domain.SettingsFileSnapshot{
		Config: domain.DefaultConfig(),
		Env: map[string]string{
			domain.SettingsSecretDashScope: "existing-dashscope",
			domain.SettingsSecretGemini:    "existing-gemini",
		},
	}
	manager := config.NewSettingsFileManager(configPath, envPath)
	if err := manager.Write(initial); err != nil {
		t.Fatalf("seed settings files: %v", err)
	}

	store := db.NewSettingsStore(database)
	if _, _, err := store.EnsureEffectiveVersion(context.Background(), initial); err != nil {
		t.Fatalf("seed settings store: %v", err)
	}
	return NewSettingsService(store, manager, clock.RealClock{}), configPath
}

func insertActiveRun(t *testing.T, database *sql.DB, id, status string) {
	t.Helper()
	if _, err := database.Exec(`
INSERT INTO runs (id, scp_id, stage, status, created_at, updated_at)
VALUES (?, '049', 'write', ?, datetime('now'), datetime('now'))`,
		id, status,
	); err != nil {
		t.Fatalf("insert active run: %v", err)
	}
}

func insertActiveRunAt(t *testing.T, database *sql.DB, id, status string, costUSD float64, updatedAt string) {
	t.Helper()
	if _, err := database.Exec(`
INSERT INTO runs (id, scp_id, stage, status, cost_usd, created_at, updated_at)
VALUES (?, '049', 'write', ?, ?, datetime('now'), ?)`,
		id, status, costUSD, updatedAt,
	); err != nil {
		t.Fatalf("insert active run: %v", err)
	}
}

func ptr(value string) *string {
	return &value
}
