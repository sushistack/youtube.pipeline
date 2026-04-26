package domain

import (
	"os"
	"path/filepath"
)

// PipelineConfig holds non-secret pipeline configuration.
// Secrets (API keys) are read from .env via environment variables at runtime.
type PipelineConfig struct {
	// LLM Model IDs
	WriterModel string `yaml:"writer_model" mapstructure:"writer_model"`
	CriticModel string `yaml:"critic_model" mapstructure:"critic_model"`
	TTSModel    string `yaml:"tts_model"    mapstructure:"tts_model"`
	ImageModel  string `yaml:"image_model"  mapstructure:"image_model"`

	// TTSVoice is the DashScope qwen3-tts voice identifier. The voice preset
	// must be a Korean-capable voice; it is left as a config field so FR47
	// (blocked-voice-ID enforcement) can layer on as a pre-call guard later
	// without requiring a client refactor.
	TTSVoice string `yaml:"tts_voice" mapstructure:"tts_voice"`

	// TTSAudioFormat is the output codec for TTS audio ("wav" or "mp3").
	// Defaults to "wav" to match existing test fixtures.
	TTSAudioFormat string `yaml:"tts_audio_format" mapstructure:"tts_audio_format"`

	// Provider names (for Writer ≠ Critic enforcement)
	WriterProvider string `yaml:"writer_provider" mapstructure:"writer_provider"`
	CriticProvider string `yaml:"critic_provider" mapstructure:"critic_provider"`

	// ImageProvider is the provider for image generation ("dashscope" default).
	ImageProvider string `yaml:"image_provider" mapstructure:"image_provider"`
	// TTSProvider is the provider for TTS synthesis ("dashscope" default).
	TTSProvider string `yaml:"tts_provider" mapstructure:"tts_provider"`

	// DashScope
	DashScopeRegion string `yaml:"dashscope_region" mapstructure:"dashscope_region"`

	// Paths
	DataDir   string `yaml:"data_dir"   mapstructure:"data_dir"`
	OutputDir string `yaml:"output_dir" mapstructure:"output_dir"`
	DBPath    string `yaml:"db_path"    mapstructure:"db_path"`

	// Cost caps (USD per stage)
	CostCapResearch float64 `yaml:"cost_cap_research" mapstructure:"cost_cap_research"`
	CostCapWrite    float64 `yaml:"cost_cap_write"    mapstructure:"cost_cap_write"`
	CostCapImage    float64 `yaml:"cost_cap_image"    mapstructure:"cost_cap_image"`
	CostCapTTS      float64 `yaml:"cost_cap_tts"      mapstructure:"cost_cap_tts"`
	CostCapAssemble float64 `yaml:"cost_cap_assemble" mapstructure:"cost_cap_assemble"`

	// Per-run hard cap (USD). NFR-C2 backstop — if the sum of per-stage
	// spend for a single run exceeds this, the run is hard-stopped regardless
	// of which stage was running. Intentionally larger than the sum of
	// per-stage caps so per-stage caps remain the primary guardrail.
	CostCapPerRun float64 `yaml:"cost_cap_per_run" mapstructure:"cost_cap_per_run"`

	// Anti-progress cosine similarity threshold (FR8). When two consecutive
	// retry outputs exceed this threshold the retry loop is hard-stopped and
	// the operator is escalated via domain.ErrAntiProgress (NFR-R2).
	// Must be in (0.0, 1.0]; 1.0 disables the detector (no value can exceed).
	AntiProgressThreshold float64 `yaml:"anti_progress_threshold" mapstructure:"anti_progress_threshold"`

	// GoldenStalenessDays is the number of days after last_refreshed_at before
	// pipeline doctor emits a staleness warning for the Golden eval set (FR26).
	// Default is 30. Values < 1 are rejected as domain.ErrValidation.
	GoldenStalenessDays int `yaml:"golden_staleness_days" mapstructure:"golden_staleness_days"`

	// ShadowEvalWindow is the number of most recent passed runs the Shadow
	// runner replays (FR28). Default is 10. Values < 1 are rejected as
	// domain.ErrValidation.
	ShadowEvalWindow int `yaml:"shadow_eval_window" mapstructure:"shadow_eval_window"`

	// AutoApprovalThreshold is the strict scene-level critic_score cutoff
	// above which a scene can be system-auto-approved when no safeguards fire.
	AutoApprovalThreshold float64 `yaml:"auto_approval_threshold" mapstructure:"auto_approval_threshold"`

	// BlockedVoiceIDs lists TTS voice identifiers that the operator has
	// prohibited from use. When a run's TTSVoice matches an entry, the TTS
	// track rejects the request before any external API call and appends a
	// voice_blocked audit entry. Default is nil (no voices blocked).
	BlockedVoiceIDs []string `yaml:"blocked_voice_ids" mapstructure:"blocked_voice_ids"`

	// ArtifactRetentionDays is the Story 10.3 Soft Archive cutoff. Terminal
	// runs whose `updated_at` is older than `now - ArtifactRetentionDays` are
	// eligible for archive: artifact files are deleted from disk and DB path
	// references are nulled, while the run/segment/decision rows are retained
	// indefinitely per NFR-O2. Must be >= 1; 0 or negative is rejected as
	// domain.ErrValidation by the loader.
	ArtifactRetentionDays int `yaml:"artifact_retention_days" mapstructure:"artifact_retention_days"`
}

// DefaultConfig returns a PipelineConfig with sensible defaults.
// Writer and Critic use different providers out of the box (FR46).
func DefaultConfig() PipelineConfig {
	home, _ := os.UserHomeDir()
	base := filepath.Join(home, ".youtube-pipeline")

	return PipelineConfig{
		WriterModel:           "deepseek-chat",
		CriticModel:           "gemini-3.1-flash-lite-preview",
		TTSModel:              "qwen3-tts-flash-2025-09-18",
		TTSVoice:              "Ethan",
		TTSAudioFormat:        "wav",
		ImageModel:            "qwen-max-vl",
		WriterProvider:        "deepseek",
		CriticProvider:        "gemini",
		ImageProvider:         "dashscope",
		TTSProvider:           "dashscope",
		DashScopeRegion:       "cn-beijing",
		DataDir:               "/mnt/data/raw",
		OutputDir:             filepath.Join(base, "output"),
		DBPath:                filepath.Join(base, "pipeline.db"),
		CostCapResearch:       0.50,
		CostCapWrite:          0.50,
		CostCapImage:          2.00,
		CostCapTTS:            1.00,
		CostCapAssemble:       0.10,
		CostCapPerRun:         5.00,
		AntiProgressThreshold: 0.92,
		GoldenStalenessDays:   30,
		ShadowEvalWindow:      10,
		AutoApprovalThreshold: 0.85,
		ArtifactRetentionDays: 30,
	}
}
