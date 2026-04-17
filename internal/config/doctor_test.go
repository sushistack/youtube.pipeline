package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

func TestAPIKeyCheck_AllPresent(t *testing.T) {
	t.Setenv("DASHSCOPE_API_KEY", "key1")
	t.Setenv("DEEPSEEK_API_KEY", "key2")
	t.Setenv("GEMINI_API_KEY", "key3")

	c := &APIKeyCheck{}
	if err := c.Run(domain.PipelineConfig{}); err != nil {
		t.Errorf("expected pass, got error: %v", err)
	}
}

func TestAPIKeyCheck_MissingKeys(t *testing.T) {
	t.Setenv("DASHSCOPE_API_KEY", "key1")
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")

	c := &APIKeyCheck{}
	err := c.Run(domain.PipelineConfig{})
	if err == nil {
		t.Fatal("expected error for missing keys")
	}
	if !strings.Contains(err.Error(), "DEEPSEEK_API_KEY") {
		t.Errorf("error should mention DEEPSEEK_API_KEY, got: %s", err)
	}
	if !strings.Contains(err.Error(), "GEMINI_API_KEY") {
		t.Errorf("error should mention GEMINI_API_KEY, got: %s", err)
	}
}

func TestFSWritableCheck_WritableDirs(t *testing.T) {
	tmp := t.TempDir()
	outDir := filepath.Join(tmp, "output")
	dataDir := filepath.Join(tmp, "data")
	os.MkdirAll(outDir, 0755)
	os.MkdirAll(dataDir, 0755)

	cfg := domain.PipelineConfig{
		OutputDir: outDir,
		DataDir:   dataDir,
	}

	c := &FSWritableCheck{}
	if err := c.Run(cfg); err != nil {
		t.Errorf("expected pass, got error: %v", err)
	}
}

func TestFSWritableCheck_NonexistentDir(t *testing.T) {
	cfg := domain.PipelineConfig{
		OutputDir: filepath.Join(t.TempDir(), "nonexistent"),
		DataDir:   t.TempDir(),
	}

	c := &FSWritableCheck{}
	err := c.Run(cfg)
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error should mention 'does not exist', got: %s", err)
	}
}

func TestFSWritableCheck_UnwritablePath(t *testing.T) {
	cfg := domain.PipelineConfig{
		OutputDir: "/proc/nonexistent-probe",
		DataDir:   t.TempDir(),
	}

	c := &FSWritableCheck{}
	err := c.Run(cfg)
	if err == nil {
		t.Fatal("expected error for unwritable path")
	}
	if !strings.Contains(err.Error(), "output_dir") {
		t.Errorf("error should mention output_dir, got: %s", err)
	}
}

func TestFFmpegCheck_Found(t *testing.T) {
	c := &FFmpegCheck{
		LookPath: func(file string) (string, error) {
			return "/usr/bin/ffmpeg", nil
		},
	}
	// This will try to actually run /usr/bin/ffmpeg -version.
	// Skip if ffmpeg isn't actually there.
	if _, err := os.Stat("/usr/bin/ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed, skipping")
	}
	if err := c.Run(domain.PipelineConfig{}); err != nil {
		t.Errorf("expected pass: %v", err)
	}
}

func TestFFmpegCheck_NotFound(t *testing.T) {
	c := &FFmpegCheck{
		LookPath: func(file string) (string, error) {
			return "", fmt.Errorf("not found")
		},
	}
	err := c.Run(domain.PipelineConfig{})
	if err == nil {
		t.Fatal("expected error for missing ffmpeg")
	}
	if !strings.Contains(err.Error(), "ffmpeg not found") {
		t.Errorf("error should mention ffmpeg not found, got: %s", err)
	}
}

func TestWriterCriticCheck_DifferentProviders(t *testing.T) {
	cfg := domain.PipelineConfig{
		WriterProvider: "deepseek",
		CriticProvider: "gemini",
	}
	c := &WriterCriticCheck{}
	if err := c.Run(cfg); err != nil {
		t.Errorf("expected pass: %v", err)
	}
}

func TestWriterCriticCheck_SameProvider(t *testing.T) {
	cfg := domain.PipelineConfig{
		WriterProvider: "deepseek",
		CriticProvider: "deepseek",
	}
	c := &WriterCriticCheck{}
	err := c.Run(cfg)
	if err == nil {
		t.Fatal("expected error for same provider")
	}
	want := "Writer and Critic must use different LLM providers"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestRegistry_RunAll(t *testing.T) {
	r := NewRegistry()
	r.Register(&WriterCriticCheck{})

	cfg := domain.PipelineConfig{
		WriterProvider: "deepseek",
		CriticProvider: "gemini",
	}
	results := r.RunAll(cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Passed {
		t.Error("expected check to pass")
	}
}

func TestRegistry_Extensible(t *testing.T) {
	r := NewRegistry()

	// Register a custom check.
	r.Register(&customCheck{name: "Custom", fail: false})
	r.Register(&customCheck{name: "Failing", fail: true})

	results := r.RunAll(domain.PipelineConfig{})
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if !results[0].Passed {
		t.Error("custom check should pass")
	}
	if results[1].Passed {
		t.Error("failing check should fail")
	}
}

func TestDefaultRegistry_Has4Checks(t *testing.T) {
	r := DefaultRegistry()
	if len(r.checks) != 4 {
		t.Errorf("DefaultRegistry should have 4 checks, got %d", len(r.checks))
	}
}

func TestAllPassed(t *testing.T) {
	t.Run("all passed", func(t *testing.T) {
		results := []Result{{Passed: true}, {Passed: true}}
		if !AllPassed(results) {
			t.Error("expected AllPassed = true")
		}
	})
	t.Run("one failed", func(t *testing.T) {
		results := []Result{{Passed: true}, {Passed: false}}
		if AllPassed(results) {
			t.Error("expected AllPassed = false")
		}
	})
	t.Run("empty", func(t *testing.T) {
		if !AllPassed(nil) {
			t.Error("empty results should be all passed")
		}
	})
}

// customCheck is a test helper implementing Check.
type customCheck struct {
	name string
	fail bool
}

func (c *customCheck) Name() string { return c.name }

func (c *customCheck) Run(_ domain.PipelineConfig) error {
	if c.fail {
		return fmt.Errorf("custom check failed")
	}
	return nil
}
