package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/testutil"
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

// buildDoctorSetup creates a minimal valid doctor config and returns config path.
func buildDoctorSetup(t *testing.T) (string, string) {
	t.Helper()
	tmp := t.TempDir()
	cfg := `writer_provider: deepseek
critic_provider: gemini
output_dir: "` + filepath.Join(tmp, "output") + `"
data_dir: "` + filepath.Join(tmp, "data") + `"
`
	configPath := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(configPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	os.MkdirAll(filepath.Join(tmp, "output"), 0755)
	os.MkdirAll(filepath.Join(tmp, "data"), 0755)
	t.Setenv("DASHSCOPE_API_KEY", "test-key")
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	t.Setenv("GEMINI_API_KEY", "test-key")
	return tmp, configPath
}

func buildStaleGoldenRoot(t *testing.T, refreshedAt time.Time) string {
	t.Helper()
	realRoot := testutil.ProjectRoot(t)
	tmp := t.TempDir()
	for _, rel := range []string{"testdata/contracts", "testdata/golden/eval", "docs/prompts/scenario"} {
		os.MkdirAll(filepath.Join(tmp, rel), 0o755)
	}
	for _, name := range []string{"golden_eval_fixture.schema.json", "writer_output.schema.json", "golden_eval_manifest.schema.json"} {
		data, _ := os.ReadFile(filepath.Join(realRoot, "testdata", "contracts", name))
		os.WriteFile(filepath.Join(tmp, "testdata", "contracts", name), data, 0o644)
	}
	promptData, _ := os.ReadFile(filepath.Join(realRoot, "docs", "prompts", "scenario", "critic_agent.md"))
	os.WriteFile(filepath.Join(tmp, "docs", "prompts", "scenario", "critic_agent.md"), promptData, 0o644)
	m := map[string]interface{}{
		"version":           1,
		"next_index":        1,
		"last_refreshed_at": refreshedAt.UTC().Format(time.RFC3339),
		"pairs":             []interface{}{},
	}
	data, _ := json.MarshalIndent(m, "", "  ")
	data = append(data, '\n')
	os.WriteFile(filepath.Join(tmp, "testdata", "golden", "eval", "manifest.json"), data, 0o644)
	return tmp
}

func TestDoctorCmd_WarnsOnGoldenStaleness(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	_, configPath := buildDoctorSetup(t)
	// Build a stale golden root (100 days ago).
	staleRoot := buildStaleGoldenRoot(t, time.Now().UTC().Add(-100*24*time.Hour))

	prevCfgPath, prevRoot := cfgPath, goldenProjectRoot
	cfgPath, goldenProjectRoot = configPath, staleRoot
	defer func() { cfgPath, goldenProjectRoot = prevCfgPath, prevRoot }()

	cmd := newDoctorCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	// Doctor passes hard checks but should include staleness warning.
	_ = cmd.RunE(cmd, nil)
	out := buf.String()
	if !strings.Contains(out, "Staleness Warning:") {
		t.Errorf("expected 'Staleness Warning:' in doctor output, got:\n%s", out)
	}
}

func TestDoctorCmd_WarningsDoNotFailExitCode(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	_, configPath := buildDoctorSetup(t)
	staleRoot := buildStaleGoldenRoot(t, time.Now().UTC().Add(-100*24*time.Hour))

	prevCfgPath, prevRoot := cfgPath, goldenProjectRoot
	cfgPath, goldenProjectRoot = configPath, staleRoot
	defer func() { cfgPath, goldenProjectRoot = prevCfgPath, prevRoot }()

	cmd := newDoctorCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.RunE(cmd, nil)
	// All hard checks pass (env set up correctly), so error should be nil
	// even with staleness warning present.
	if err != nil {
		t.Errorf("expected nil error when only warnings present, got: %v\noutput:\n%s", err, buf.String())
	}
}
