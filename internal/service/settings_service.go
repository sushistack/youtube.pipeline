package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// ErrSettingsConflict signals an ETag/If-Match mismatch on a concurrent save.
// Handlers should translate this to HTTP 409 so the client can refresh the
// snapshot and resubmit deliberately.
var ErrSettingsConflict = errors.New("settings: concurrent update detected")

type SettingsStore interface {
	LoadState(ctx context.Context) (db.SettingsStateRow, error)
	LoadVersion(ctx context.Context, version int64) (db.SettingsVersionRow, error)
	SaveSnapshot(ctx context.Context, files domain.SettingsFileSnapshot) (db.SettingsStateRow, int64, error)
	EnsureEffectiveVersion(ctx context.Context, files domain.SettingsFileSnapshot) (db.SettingsStateRow, int64, error)
	BudgetSourceRun(ctx context.Context) (*domain.SettingsBudgetRun, error)
}

type SettingsFileAccess interface {
	Load() (domain.SettingsFileSnapshot, error)
	Write(files domain.SettingsFileSnapshot) error
}

type SettingsService struct {
	store SettingsStore
	files SettingsFileAccess
	clk   clock.Clock
	// mu serializes Save against disk writes to avoid torn writes under two
	// simultaneous operator saves. DB ops inside the hold remain fast — we
	// only guard the read-modify-write envelope.
	mu sync.Mutex
}

func NewSettingsService(store SettingsStore, files SettingsFileAccess, clk clock.Clock) *SettingsService {
	if clk == nil {
		clk = clock.RealClock{}
	}
	return &SettingsService{store: store, files: files, clk: clk}
}

// Bootstrap seeds settings_state.effective_version from the current disk
// snapshot if no version has ever been recorded. Intended to be called once
// at server startup so LoadEffectiveRuntimeFiles never has to fall back to
// raw disk reads.
func (s *SettingsService) Bootstrap(ctx context.Context) error {
	files, err := s.files.Load()
	if err != nil {
		// Corrupted config.yaml on a cold start: seed from defaults so the
		// server still comes up. The operator can fix or reset from the UI.
		files = domain.SettingsFileSnapshot{Config: domain.DefaultConfig(), Env: map[string]string{}}
	}
	if _, _, err := s.store.EnsureEffectiveVersion(ctx, files); err != nil {
		return fmt.Errorf("settings bootstrap: %w", err)
	}
	return nil
}

// ResetToDefaults overwrites config.yaml with domain.DefaultConfig() and
// records a new effective version. .env is left untouched — secrets are
// never reset via the UI surface. Intended as the recovery action when
// config.yaml has become unreadable.
func (s *SettingsService) ResetToDefaults(ctx context.Context) (*SettingsSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// .env stays as-is so the operator doesn't lose API keys during recovery.
	disk, err := s.files.Load()
	if err != nil {
		// If even .env is unreadable, proceed with an empty env map — the
		// point of the reset action is to break out of a stuck state.
		disk = domain.SettingsFileSnapshot{Env: map[string]string{}}
	}
	next := domain.SettingsFileSnapshot{Config: domain.DefaultConfig(), Env: disk.Env}

	state, _, err := s.store.SaveSnapshot(ctx, next)
	if err != nil {
		return nil, fmt.Errorf("reset settings: persist: %w", err)
	}
	if err := s.files.Write(next); err != nil {
		return nil, fmt.Errorf("reset settings: write disk: %w", err)
	}
	return s.buildSnapshot(ctx, next, state)
}

func (s *SettingsService) Snapshot(ctx context.Context) (*SettingsSnapshot, error) {
	state, err := s.store.LoadState(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot settings: %w", err)
	}
	files, err := s.effectiveSnapshotFromState(ctx, state)
	if err != nil {
		return nil, err
	}
	return s.buildSnapshot(ctx, files, state)
}

// Save persists a settings edit by writing a new effective version to the DB
// and the new snapshot to disk in a single read-modify-write envelope.
//
// ifMatchVersion, when non-nil, is checked against the current effective
// version before the save is accepted. A mismatch returns ErrSettingsConflict.
// Callers that did not read a snapshot first (tests, CLI) may pass nil.
func (s *SettingsService) Save(
	ctx context.Context,
	input SettingsUpdateInput,
	ifMatchVersion *int64,
) (*SettingsSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.store.LoadState(ctx)
	if err != nil {
		return nil, fmt.Errorf("save settings: load state: %w", err)
	}

	if ifMatchVersion != nil {
		if !versionsMatch(state.EffectiveVersion, ifMatchVersion) {
			return nil, ErrSettingsConflict
		}
	}

	currentFiles, err := s.effectiveSnapshotFromState(ctx, state)
	if err != nil {
		return nil, fmt.Errorf("save settings: load effective snapshot: %w", err)
	}

	nextFiles, validationErr := s.mergeAndValidate(currentFiles, input)
	if validationErr != nil {
		return nil, fmt.Errorf("save settings: %w", validationErr)
	}

	newState, _, err := s.store.SaveSnapshot(ctx, nextFiles)
	if err != nil {
		return nil, fmt.Errorf("save settings: persist version state: %w", err)
	}
	if err := s.files.Write(nextFiles); err != nil {
		return nil, fmt.Errorf("save settings: persist files: %w", err)
	}
	return s.buildSnapshot(ctx, nextFiles, newState)
}

func (s *SettingsService) LoadEffectiveRuntimeConfig(ctx context.Context) (domain.PipelineConfig, error) {
	files, err := s.LoadEffectiveRuntimeFiles(ctx)
	if err != nil {
		return domain.PipelineConfig{}, err
	}
	return files.Config, nil
}

// LoadEffectiveRuntimeFiles returns the snapshot the pipeline should execute
// against right now. It reads from the DB effective_version — not from disk —
// so the in-memory snapshot taken by each phase executor is sourced from the
// authoritative DB state. If effective_version is unset (freshly migrated
// database before Bootstrap runs), it falls back to the on-disk snapshot for
// resilience; Bootstrap should run at startup to make this branch unreachable
// in normal operation.
func (s *SettingsService) LoadEffectiveRuntimeFiles(ctx context.Context) (domain.SettingsFileSnapshot, error) {
	state, err := s.store.LoadState(ctx)
	if err != nil {
		return domain.SettingsFileSnapshot{}, fmt.Errorf("load effective runtime config: %w", err)
	}
	return s.effectiveSnapshotFromState(ctx, state)
}

// EffectiveVersion returns the current effective version number, or 0 if
// none is set. Used by handlers building ETag headers for If-Match checks.
func (s *SettingsService) EffectiveVersion(ctx context.Context) (int64, error) {
	state, err := s.store.LoadState(ctx)
	if err != nil {
		return 0, err
	}
	if state.EffectiveVersion == nil {
		return 0, nil
	}
	return *state.EffectiveVersion, nil
}

// effectiveSnapshotFromState resolves the config+env tuple for a given state
// row. Config comes from the effective DB version when set; env is always
// loaded from disk (secrets are file-backed). When no effective version
// exists yet, both halves come from disk.
func (s *SettingsService) effectiveSnapshotFromState(ctx context.Context, state db.SettingsStateRow) (domain.SettingsFileSnapshot, error) {
	disk, err := s.files.Load()
	if err != nil {
		return domain.SettingsFileSnapshot{}, fmt.Errorf("read settings files: %w", err)
	}
	if state.EffectiveVersion == nil {
		return disk, nil
	}
	version, err := s.store.LoadVersion(ctx, *state.EffectiveVersion)
	if err != nil {
		return domain.SettingsFileSnapshot{}, fmt.Errorf("load effective version: %w", err)
	}
	return domain.SettingsFileSnapshot{Config: version.Config, Env: disk.Env}, nil
}

func (s *SettingsService) buildSnapshot(ctx context.Context, files domain.SettingsFileSnapshot, state db.SettingsStateRow) (*SettingsSnapshot, error) {
	budget, err := s.buildBudget(ctx, files.Config.CostCapPerRun)
	if err != nil {
		return nil, err
	}
	return &SettingsSnapshot{
		Config:      normalizeSettingsConfig(files.Config),
		Env:         normalizeSecretState(files.Env),
		Budget:      budget,
		Application: normalizeApplicationState(state),
	}, nil
}

func (s *SettingsService) buildBudget(ctx context.Context, hardCap float64) (SettingsBudgetSummary, error) {
	softCap := roundUSD(hardCap * settingsSoftCapRatio)
	run, err := s.store.BudgetSourceRun(ctx)
	if err != nil {
		return SettingsBudgetSummary{}, fmt.Errorf("build settings budget: %w", err)
	}
	if run == nil {
		return SettingsBudgetSummary{
			Source: SettingsBudgetSource{
				Kind:  "none",
				Label: "No run telemetry available yet",
			},
			CurrentSpendUSD: 0,
			SoftCapUSD:      softCap,
			HardCapUSD:      hardCap,
			ProgressRatio:   0,
			Status:          "safe",
		}, nil
	}

	progress := 0.0
	if hardCap > 0 {
		progress = run.CostUSD / hardCap
	}
	status := "safe"
	if run.CostUSD >= hardCap && hardCap > 0 {
		status = "exceeded"
	} else if run.CostUSD >= softCap && softCap > 0 {
		status = "near_cap"
	}
	statusValue := run.Status
	return SettingsBudgetSummary{
		Source: SettingsBudgetSource{
			Kind:   resolveBudgetSourceKind(run.Status),
			Label:  resolveBudgetSourceLabel(run.Status),
			RunID:  &run.ID,
			Status: &statusValue,
		},
		CurrentSpendUSD: roundUSD(run.CostUSD),
		SoftCapUSD:      softCap,
		HardCapUSD:      hardCap,
		ProgressRatio:   progress,
		Status:          status,
	}, nil
}

func (s *SettingsService) mergeAndValidate(current domain.SettingsFileSnapshot, input SettingsUpdateInput) (domain.SettingsFileSnapshot, *SettingsValidationError) {
	nextConfig := current.Config
	applyEditableConfig(&nextConfig, input.Config)

	nextEnv := cloneSecretMap(current.Env)
	fieldErrors := map[string]string{}

	for key := range input.Env {
		if !isSupportedSettingsSecret(key) {
			fieldErrors["env."+key] = "unsupported secret key"
		}
	}

	for _, key := range supportedSettingsSecrets {
		value, ok := input.Env[key]
		if !ok {
			continue
		}
		if value == nil {
			// Explicit null clears a secret from .env. Validation below
			// rejects clearing a required key unless the operator has set
			// a replacement.
			delete(nextEnv, key)
			continue
		}
		nextEnv[key] = *value
	}

	validateSettingsConfig(nextConfig, nextEnv, fieldErrors)
	if len(fieldErrors) > 0 {
		return domain.SettingsFileSnapshot{}, &SettingsValidationError{FieldErrors: fieldErrors}
	}
	return domain.SettingsFileSnapshot{Config: nextConfig, Env: nextEnv}, nil
}

func validateSettingsConfig(cfg domain.PipelineConfig, env map[string]string, fieldErrors map[string]string) {
	requiredStrings := map[string]string{
		"config.writer_provider":  cfg.WriterProvider,
		"config.writer_model":     cfg.WriterModel,
		"config.critic_provider":  cfg.CriticProvider,
		"config.critic_model":     cfg.CriticModel,
		"config.image_provider":   cfg.ImageProvider,
		"config.image_model":      cfg.ImageModel,
		"config.image_edit_model": cfg.ImageEditModel,
		"config.tts_provider":     cfg.TTSProvider,
		"config.tts_model":        cfg.TTSModel,
		"config.tts_voice":        cfg.TTSVoice,
		"config.tts_audio_format": cfg.TTSAudioFormat,
	}
	for field, value := range requiredStrings {
		if value == "" {
			fieldErrors[field] = "required"
		}
	}
	if cfg.DashScopeRegion == "" {
		fieldErrors["config.dashscope_region"] = "required"
	}
	if cfg.WriterProvider == cfg.CriticProvider && cfg.WriterProvider != "" {
		fieldErrors["config.writer_provider"] = "Writer and Critic must use different LLM providers"
		fieldErrors["config.critic_provider"] = "Writer and Critic must use different LLM providers"
	}

	stageCaps := map[string]float64{
		"config.cost_cap_research": cfg.CostCapResearch,
		"config.cost_cap_write":    cfg.CostCapWrite,
		"config.cost_cap_image":    cfg.CostCapImage,
		"config.cost_cap_tts":      cfg.CostCapTTS,
		"config.cost_cap_assemble": cfg.CostCapAssemble,
	}
	maxStageCap := 0.0
	for field, value := range stageCaps {
		if value < 0 {
			fieldErrors[field] = "must be non-negative"
		}
		maxStageCap = math.Max(maxStageCap, value)
	}
	if cfg.CostCapPerRun < 0 {
		fieldErrors["config.cost_cap_per_run"] = "must be non-negative"
	} else if cfg.CostCapPerRun < maxStageCap {
		fieldErrors["config.cost_cap_per_run"] = "must be greater than or equal to the highest stage cap"
	}

	for _, key := range supportedSettingsSecrets {
		if value, ok := env[key]; ok && strings.TrimSpace(value) == "" {
			fieldErrors["env."+key] = "API key cannot be blank when explicitly cleared"
		}
	}
}

func normalizeSettingsConfig(cfg domain.PipelineConfig) SettingsConfigInput {
	return SettingsConfigInput{
		WriterModel:     cfg.WriterModel,
		CriticModel:     cfg.CriticModel,
		ImageModel:      cfg.ImageModel,
		ImageEditModel:  cfg.ImageEditModel,
		TTSModel:        cfg.TTSModel,
		TTSVoice:        cfg.TTSVoice,
		TTSAudioFormat:  cfg.TTSAudioFormat,
		WriterProvider:  cfg.WriterProvider,
		CriticProvider:  cfg.CriticProvider,
		ImageProvider:   cfg.ImageProvider,
		TTSProvider:     cfg.TTSProvider,
		DashScopeRegion: cfg.DashScopeRegion,
		CostCapResearch: cfg.CostCapResearch,
		CostCapWrite:    cfg.CostCapWrite,
		CostCapImage:    cfg.CostCapImage,
		CostCapTTS:      cfg.CostCapTTS,
		CostCapAssemble: cfg.CostCapAssemble,
		CostCapPerRun:   cfg.CostCapPerRun,
	}
}

func normalizeSecretState(env map[string]string) map[string]SettingsSecretState {
	state := make(map[string]SettingsSecretState, len(supportedSettingsSecrets))
	for _, key := range supportedSettingsSecrets {
		state[key] = SettingsSecretState{Configured: env[key] != ""}
	}
	return state
}

func normalizeApplicationState(state db.SettingsStateRow) SettingsApplicationState {
	return SettingsApplicationState{
		EffectiveVersion: state.EffectiveVersion,
	}
}

func applyEditableConfig(cfg *domain.PipelineConfig, input SettingsConfigInput) {
	cfg.WriterModel = input.WriterModel
	cfg.CriticModel = input.CriticModel
	cfg.ImageModel = input.ImageModel
	cfg.ImageEditModel = input.ImageEditModel
	cfg.TTSModel = input.TTSModel
	cfg.TTSVoice = input.TTSVoice
	cfg.TTSAudioFormat = input.TTSAudioFormat
	cfg.WriterProvider = input.WriterProvider
	cfg.CriticProvider = input.CriticProvider
	cfg.ImageProvider = input.ImageProvider
	cfg.TTSProvider = input.TTSProvider
	cfg.DashScopeRegion = input.DashScopeRegion
	cfg.CostCapResearch = input.CostCapResearch
	cfg.CostCapWrite = input.CostCapWrite
	cfg.CostCapImage = input.CostCapImage
	cfg.CostCapTTS = input.CostCapTTS
	cfg.CostCapAssemble = input.CostCapAssemble
	cfg.CostCapPerRun = input.CostCapPerRun
}

func cloneSecretMap(values map[string]string) map[string]string {
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		if isSupportedSettingsSecret(key) {
			cloned[key] = value
		}
	}
	return cloned
}

func resolveBudgetSourceKind(status string) string {
	switch status {
	case string(domain.StatusRunning), string(domain.StatusWaiting):
		return "active_run"
	case string(domain.StatusFailed):
		return "failed_run"
	default:
		return "latest_run"
	}
}

func resolveBudgetSourceLabel(status string) string {
	switch resolveBudgetSourceKind(status) {
	case "active_run":
		return "Active run spend"
	case "failed_run":
		return "Last failed run spend"
	default:
		return "Latest run spend"
	}
}

func isSupportedSettingsSecret(key string) bool {
	for _, candidate := range supportedSettingsSecrets {
		if candidate == key {
			return true
		}
	}
	return false
}

func roundUSD(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return math.Round(value*100) / 100
}

func versionsMatch(actual, expected *int64) bool {
	if actual == nil && expected == nil {
		return true
	}
	if actual == nil || expected == nil {
		return false
	}
	return *actual == *expected
}
