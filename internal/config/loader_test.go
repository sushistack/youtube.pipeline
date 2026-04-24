package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

func TestLoad_DefaultsWhenNoFiles(t *testing.T) {
	cfg, err := Load("", "")
	if err != nil {
		t.Fatalf("Load with no files: %v", err)
	}
	if cfg.WriterProvider != "deepseek" {
		t.Errorf("WriterProvider = %q, want %q", cfg.WriterProvider, "deepseek")
	}
	if cfg.CriticProvider != "gemini" {
		t.Errorf("CriticProvider = %q, want %q", cfg.CriticProvider, "gemini")
	}
	if cfg.CostCapResearch != 0.50 {
		t.Errorf("CostCapResearch = %f, want 0.50", cfg.CostCapResearch)
	}
}

func TestLoad_YAMLOverridesDefaults(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yaml")

	yaml := `writer_provider: "openai"
critic_provider: "anthropic"
cost_cap_research: 1.25
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	cfg, err := Load(cfgPath, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.WriterProvider != "openai" {
		t.Errorf("WriterProvider = %q, want %q", cfg.WriterProvider, "openai")
	}
	if cfg.CriticProvider != "anthropic" {
		t.Errorf("CriticProvider = %q, want %q", cfg.CriticProvider, "anthropic")
	}
	if cfg.CostCapResearch != 1.25 {
		t.Errorf("CostCapResearch = %f, want 1.25", cfg.CostCapResearch)
	}
	// Unset fields keep defaults.
	if cfg.DashScopeRegion != "cn-beijing" {
		t.Errorf("DashScopeRegion = %q, want default %q", cfg.DashScopeRegion, "cn-beijing")
	}
}

func TestLoad_EnvOverridesYAML(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yaml")
	envPath := filepath.Join(tmp, ".env")

	yaml := `writer_provider: "openai"
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	env := `WRITER_PROVIDER=overridden-provider
`
	if err := os.WriteFile(envPath, []byte(env), 0644); err != nil {
		t.Fatalf("write env: %v", err)
	}

	// Clean up env var after test.
	t.Cleanup(func() {
		os.Unsetenv("WRITER_PROVIDER")
	})

	cfg, err := Load(cfgPath, envPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.WriterProvider != "overridden-provider" {
		t.Errorf("WriterProvider = %q, want %q", cfg.WriterProvider, "overridden-provider")
	}
}

func TestLoad_MissingEnvFileIgnored(t *testing.T) {
	cfg, err := Load("", "/nonexistent/.env")
	if err != nil {
		t.Fatalf("Load with missing .env should not error: %v", err)
	}
	if cfg.WriterProvider == "" {
		t.Error("should still have defaults")
	}
}

func TestLoad_MissingConfigFileIgnored(t *testing.T) {
	cfg, err := Load("/nonexistent/config.yaml", "")
	if err != nil {
		t.Fatalf("Load with missing config.yaml should not error: %v", err)
	}
	if cfg.WriterProvider == "" {
		t.Error("should still have defaults")
	}
}

func TestLoad_AntiProgressThresholdDefault(t *testing.T) {
	cfg, err := Load("", "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AntiProgressThreshold != 0.92 {
		t.Errorf("AntiProgressThreshold = %v, want 0.92 default", cfg.AntiProgressThreshold)
	}
}

func TestLoad_AntiProgressThresholdOverride(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yaml")
	yaml := `anti_progress_threshold: 0.85
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	cfg, err := Load(cfgPath, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AntiProgressThreshold != 0.85 {
		t.Errorf("AntiProgressThreshold = %v, want 0.85 (from yaml)", cfg.AntiProgressThreshold)
	}
}

func TestLoad_GoldenStalenessDaysDefault(t *testing.T) {
	cfg, err := Load("", "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.GoldenStalenessDays != 30 {
		t.Errorf("GoldenStalenessDays = %d, want 30 default", cfg.GoldenStalenessDays)
	}
}

func TestLoad_GoldenStalenessDaysOverride(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yaml")
	yaml := `golden_staleness_days: 7
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	cfg, err := Load(cfgPath, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.GoldenStalenessDays != 7 {
		t.Errorf("GoldenStalenessDays = %d, want 7", cfg.GoldenStalenessDays)
	}
}

func TestLoad_GoldenStalenessDaysRejectsZero(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yaml")
	yaml := `golden_staleness_days: 0
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	_, err := Load(cfgPath, "")
	if err == nil {
		t.Fatal("expected validation error for golden_staleness_days=0")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}
}

func TestLoad_GoldenStalenessDaysRejectsNegative(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yaml")
	yaml := `golden_staleness_days: -5
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	_, err := Load(cfgPath, "")
	if err == nil {
		t.Fatal("expected validation error for negative golden_staleness_days")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}
}

func TestLoadConfig_ShadowEvalWindowDefault(t *testing.T) {
	cfg, err := Load("", "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ShadowEvalWindow != 10 {
		t.Errorf("ShadowEvalWindow = %d, want 10 default", cfg.ShadowEvalWindow)
	}
}

func TestLoadConfig_ShadowEvalWindowOverride(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yaml")
	yaml := `shadow_eval_window: 25
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	cfg, err := Load(cfgPath, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ShadowEvalWindow != 25 {
		t.Errorf("ShadowEvalWindow = %d, want 25", cfg.ShadowEvalWindow)
	}
}

func TestLoadConfig_ShadowEvalWindowRejectsZero(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yaml")
	yaml := `shadow_eval_window: 0
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	_, err := Load(cfgPath, "")
	if err == nil {
		t.Fatal("expected validation error for shadow_eval_window=0")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}
}

func TestLoadConfig_ShadowEvalWindowRejectsNegative(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yaml")
	yaml := `shadow_eval_window: -3
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	_, err := Load(cfgPath, "")
	if err == nil {
		t.Fatal("expected validation error for negative shadow_eval_window")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}
}

func TestConfigLoad_AutoApprovalThresholdFromYAML(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yaml")
	yaml := `auto_approval_threshold: 0.91
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	cfg, err := Load(cfgPath, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AutoApprovalThreshold != 0.91 {
		t.Errorf("AutoApprovalThreshold = %v, want 0.91", cfg.AutoApprovalThreshold)
	}
}

func TestConfigLoad_RejectsOutOfRangeAutoApprovalThreshold(t *testing.T) {
	for _, tc := range []string{"0", "1.0", "-0.2"} {
		t.Run(tc, func(t *testing.T) {
			tmp := t.TempDir()
			cfgPath := filepath.Join(tmp, "config.yaml")
			yaml := "auto_approval_threshold: " + tc + "\n"
			if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
				t.Fatalf("write yaml: %v", err)
			}
			_, err := Load(cfgPath, "")
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !errors.Is(err, domain.ErrValidation) {
				t.Fatalf("expected ErrValidation, got %v", err)
			}
		})
	}
}

func TestLoadConfig_ArtifactRetentionDaysDefault(t *testing.T) {
	cfg, err := Load("", "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ArtifactRetentionDays != 30 {
		t.Errorf("ArtifactRetentionDays = %d, want 30 default", cfg.ArtifactRetentionDays)
	}
}

func TestLoadConfig_ArtifactRetentionDaysOverride(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yaml")
	yaml := `artifact_retention_days: 7
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	cfg, err := Load(cfgPath, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ArtifactRetentionDays != 7 {
		t.Errorf("ArtifactRetentionDays = %d, want 7", cfg.ArtifactRetentionDays)
	}
}

func TestLoadConfig_ArtifactRetentionDaysRejectsInvalid(t *testing.T) {
	for _, tc := range []string{"0", "-1"} {
		t.Run(tc, func(t *testing.T) {
			tmp := t.TempDir()
			cfgPath := filepath.Join(tmp, "config.yaml")
			yaml := "artifact_retention_days: " + tc + "\n"
			if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
				t.Fatalf("write yaml: %v", err)
			}
			_, err := Load(cfgPath, "")
			if err == nil {
				t.Fatalf("expected validation error for artifact_retention_days=%s", tc)
			}
			if !errors.Is(err, domain.ErrValidation) {
				t.Fatalf("expected ErrValidation, got %v", err)
			}
		})
	}
}

func TestDefaultConfigDir(t *testing.T) {
	dir := DefaultConfigDir()
	if dir == "" {
		t.Error("DefaultConfigDir returned empty")
	}
	if filepath.Base(dir) != ".youtube-pipeline" {
		t.Errorf("DefaultConfigDir = %q, want suffix .youtube-pipeline", dir)
	}
}
