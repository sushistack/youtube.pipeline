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

	// Provider names (for Writer ≠ Critic enforcement)
	WriterProvider string `yaml:"writer_provider" mapstructure:"writer_provider"`
	CriticProvider string `yaml:"critic_provider" mapstructure:"critic_provider"`

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
}

// DefaultConfig returns a PipelineConfig with sensible defaults.
// Writer and Critic use different providers out of the box (FR46).
func DefaultConfig() PipelineConfig {
	home, _ := os.UserHomeDir()
	base := filepath.Join(home, ".youtube-pipeline")

	return PipelineConfig{
		WriterModel:    "deepseek-chat",
		CriticModel:    "gemini-2.0-flash",
		TTSModel:       "qwen3-tts-flash-2025-09-18",
		ImageModel:     "qwen-max-vl",
		WriterProvider: "deepseek",
		CriticProvider: "gemini",
		DashScopeRegion: "cn-beijing",
		DataDir:         "/mnt/data/raw",
		OutputDir:       filepath.Join(base, "output"),
		DBPath:          filepath.Join(base, "pipeline.db"),
		CostCapResearch: 0.50,
		CostCapWrite:    0.50,
		CostCapImage:    2.00,
		CostCapTTS:      1.00,
		CostCapAssemble: 0.10,
		CostCapPerRun:   5.00,
	}
}
