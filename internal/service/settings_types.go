package service

import (
	"sort"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

const (
	settingsSoftCapRatio = 0.8
)

var supportedSettingsSecrets = []string{
	domain.SettingsSecretDashScope,
	domain.SettingsSecretDeepSeek,
	domain.SettingsSecretGemini,
}

type SettingsConfigInput struct {
	WriterModel     string `json:"writer_model"`
	CriticModel     string `json:"critic_model"`
	ImageModel      string `json:"image_model"`
	ImageEditModel  string `json:"image_edit_model"`
	TTSModel        string `json:"tts_model"`
	TTSVoice        string `json:"tts_voice"`
	TTSAudioFormat  string `json:"tts_audio_format"`
	WriterProvider  string `json:"writer_provider"`
	CriticProvider  string `json:"critic_provider"`
	ImageProvider   string `json:"image_provider"`
	TTSProvider     string `json:"tts_provider"`
	DashScopeRegion string `json:"dashscope_region"`
	ComfyUIEndpoint string `json:"comfyui_endpoint"`

	CostCapResearch float64 `json:"cost_cap_research"`
	CostCapWrite    float64 `json:"cost_cap_write"`
	CostCapImage    float64 `json:"cost_cap_image"`
	CostCapTTS      float64 `json:"cost_cap_tts"`
	CostCapAssemble float64 `json:"cost_cap_assemble"`
	CostCapPerRun   float64 `json:"cost_cap_per_run"`

	DryRun bool `json:"dry_run"`
}

type SettingsSecretState struct {
	Configured bool `json:"configured"`
}

type SettingsBudgetSource struct {
	Kind   string  `json:"kind"`
	Label  string  `json:"label"`
	RunID  *string `json:"run_id,omitempty"`
	Status *string `json:"status,omitempty"`
}

type SettingsBudgetSummary struct {
	Source          SettingsBudgetSource `json:"source"`
	CurrentSpendUSD float64              `json:"current_spend_usd"`
	SoftCapUSD      float64              `json:"soft_cap_usd"`
	HardCapUSD      float64              `json:"hard_cap_usd"`
	ProgressRatio   float64              `json:"progress_ratio"`
	Status          string               `json:"status"`
}

type SettingsApplicationState struct {
	EffectiveVersion *int64 `json:"effective_version,omitempty"`
}

// SettingsNoneVersion is sentinel used in ETag / If-Match headers when no
// effective version exists yet (fresh install prior to the first save).
// Clients surface it as the literal string "0".
const SettingsNoneVersion = 0

type SettingsSnapshot struct {
	Config      SettingsConfigInput            `json:"config"`
	Env         map[string]SettingsSecretState `json:"env"`
	Budget      SettingsBudgetSummary          `json:"budget"`
	Application SettingsApplicationState       `json:"application"`
}

type SettingsUpdateInput struct {
	Config SettingsConfigInput `json:"config"`
	Env    map[string]*string  `json:"env"`
}

type SettingsVersionRecord struct {
	Version int64
	Files   domain.SettingsFileSnapshot
}

type SettingsStateRecord struct {
	EffectiveVersion *int64
}

type SettingsValidationError struct {
	FieldErrors map[string]string
}

func (e *SettingsValidationError) Error() string {
	if e == nil || len(e.FieldErrors) == 0 {
		return "settings validation failed"
	}
	for _, key := range []string{
		"config.writer_provider",
		"config.critic_provider",
		"config.writer_model",
		"config.critic_model",
		"config.image_provider",
		"config.image_model",
		"config.tts_provider",
		"config.tts_model",
		"config.tts_voice",
		"config.tts_audio_format",
		"config.cost_cap_per_run",
		"env.DASHSCOPE_API_KEY",
		"env.DEEPSEEK_API_KEY",
		"env.GEMINI_API_KEY",
	} {
		if msg, ok := e.FieldErrors[key]; ok {
			return msg
		}
	}
	// Deterministic fallback: sort field names so identical inputs produce
	// identical error messages across runs (map iteration order is random).
	keys := make([]string, 0, len(e.FieldErrors))
	for key := range e.FieldErrors {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return e.FieldErrors[keys[0]]
}

// Is lets handlers check `errors.Is(err, domain.ErrValidation)` without
// double-wrapping at call sites. Every SettingsValidationError IS a
// validation error by construction.
func (e *SettingsValidationError) Is(target error) bool {
	return target == domain.ErrValidation
}
