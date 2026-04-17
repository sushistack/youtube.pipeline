package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorCmd_PassesWithValidSetup(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")

	cfg := `writer_provider: deepseek
critic_provider: gemini
output_dir: "` + filepath.Join(tmp, "output") + `"
data_dir: "` + filepath.Join(tmp, "data") + `"
`
	if err := os.WriteFile(configPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Create the directories doctor will check.
	os.MkdirAll(filepath.Join(tmp, "output"), 0755)
	os.MkdirAll(filepath.Join(tmp, "data"), 0755)

	// Set required API keys.
	t.Setenv("DASHSCOPE_API_KEY", "test-key")
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	t.Setenv("GEMINI_API_KEY", "test-key")

	cfgPath = configPath
	cmd := newDoctorCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	// Note: doctor calls os.Exit(1) on failure. We can't test that directly.
	// Instead, we verify the output contains expected content.
	err := cmd.RunE(cmd, nil)
	if err != nil {
		t.Fatalf("doctor failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "API Keys") {
		t.Error("output should mention API Keys check")
	}
}

func TestDoctorCmd_FailsOnMissingKey(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")

	cfg := `writer_provider: deepseek
critic_provider: gemini
output_dir: "` + filepath.Join(tmp, "output") + `"
data_dir: "` + filepath.Join(tmp, "data") + `"
`
	if err := os.WriteFile(configPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	os.MkdirAll(filepath.Join(tmp, "output"), 0755)
	os.MkdirAll(filepath.Join(tmp, "data"), 0755)

	// Only set one key — two are missing.
	t.Setenv("DASHSCOPE_API_KEY", "test-key")
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")

	cfgPath = configPath
	cmd := newDoctorCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected error for missing keys")
	}

	out := buf.String()
	if !strings.Contains(out, "missing API keys") {
		t.Errorf("output should mention missing API keys, got: %s", out)
	}
	if !strings.Contains(out, "DEEPSEEK_API_KEY") {
		t.Errorf("should mention DEEPSEEK_API_KEY, got: %s", out)
	}
}

func TestDoctorCmd_FailsOnWriterEqualsCritic(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")

	cfg := `writer_provider: deepseek
critic_provider: deepseek
output_dir: "` + filepath.Join(tmp, "output") + `"
data_dir: "` + filepath.Join(tmp, "data") + `"
`
	if err := os.WriteFile(configPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	os.MkdirAll(filepath.Join(tmp, "output"), 0755)
	os.MkdirAll(filepath.Join(tmp, "data"), 0755)

	t.Setenv("DASHSCOPE_API_KEY", "test-key")
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	t.Setenv("GEMINI_API_KEY", "test-key")

	cfgPath = configPath
	cmd := newDoctorCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected error for same provider")
	}

	out := buf.String()
	if !strings.Contains(out, "Writer and Critic must use different LLM providers") {
		t.Errorf("should report Writer=Critic error, got: %s", out)
	}
}
