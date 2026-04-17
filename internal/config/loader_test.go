package config

import (
	"os"
	"path/filepath"
	"testing"
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

func TestDefaultConfigDir(t *testing.T) {
	dir := DefaultConfigDir()
	if dir == "" {
		t.Error("DefaultConfigDir returned empty")
	}
	if filepath.Base(dir) != ".youtube-pipeline" {
		t.Errorf("DefaultConfigDir = %q, want suffix .youtube-pipeline", dir)
	}
}
