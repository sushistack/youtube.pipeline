package config

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// Check is the interface for doctor preflight checks.
type Check interface {
	Name() string
	Run(cfg domain.PipelineConfig) error
}

// Result holds the outcome of a single check.
type Result struct {
	Name    string
	Passed  bool
	Message string
}

// Registry holds registered checks and runs them in order.
type Registry struct {
	checks []Check
}

// NewRegistry returns an empty check registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a check to the registry.
func (r *Registry) Register(c Check) {
	r.checks = append(r.checks, c)
}

// RunAll executes all registered checks and returns results.
func (r *Registry) RunAll(cfg domain.PipelineConfig) []Result {
	results := make([]Result, 0, len(r.checks))
	for _, c := range r.checks {
		res := Result{Name: c.Name(), Passed: true}
		if err := c.Run(cfg); err != nil {
			res.Passed = false
			res.Message = err.Error()
		}
		results = append(results, res)
	}
	return results
}

// DefaultRegistry returns a registry with all standard checks pre-registered.
func DefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register(&APIKeyCheck{})
	r.Register(&FSWritableCheck{})
	r.Register(&FFmpegCheck{})
	r.Register(&WriterCriticCheck{})
	return r
}

// APIKeyCheck verifies that required API key environment variables are set.
type APIKeyCheck struct{}

func (c *APIKeyCheck) Name() string { return "API Keys" }

func (c *APIKeyCheck) Run(_ domain.PipelineConfig) error {
	keys := []string{"DASHSCOPE_API_KEY", "DEEPSEEK_API_KEY", "GEMINI_API_KEY"}
	var missing []string
	for _, k := range keys {
		if os.Getenv(k) == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing API keys: %v — set them in .env file", missing)
	}
	return nil
}

// FSWritableCheck verifies that output and data directories are writable.
type FSWritableCheck struct{}

func (c *FSWritableCheck) Name() string { return "Filesystem Paths" }

func (c *FSWritableCheck) Run(cfg domain.PipelineConfig) error {
	dirs := map[string]string{
		"output_dir": cfg.OutputDir,
		"data_dir":   cfg.DataDir,
	}
	for label, dir := range dirs {
		if err := checkDirWritable(dir); err != nil {
			return fmt.Errorf("%s (%s): %w — ensure the directory exists and is writable", label, dir, err)
		}
	}
	return nil
}

func checkDirWritable(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("does not exist")
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory")
	}
	tmp, err := os.CreateTemp(dir, ".doctor-probe-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	tmp.Close()
	os.Remove(name)
	return nil
}

// FFmpegCheck verifies that the ffmpeg binary is available.
type FFmpegCheck struct {
	// LookPath allows injection for testing.
	LookPath func(file string) (string, error)
}

func (c *FFmpegCheck) Name() string { return "FFmpeg" }

func (c *FFmpegCheck) Run(_ domain.PipelineConfig) error {
	lookPath := c.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	path, err := lookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("ffmpeg not found in PATH — install FFmpeg: https://ffmpeg.org/download.html")
	}
	cmd := exec.Command(path, "-version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg found at %s but failed to run: %w", path, err)
	}
	return nil
}

// WriterCriticCheck verifies that writer and critic use different LLM providers.
type WriterCriticCheck struct{}

func (c *WriterCriticCheck) Name() string { return "Writer ≠ Critic" }

func (c *WriterCriticCheck) Run(cfg domain.PipelineConfig) error {
	if cfg.WriterProvider == cfg.CriticProvider {
		return fmt.Errorf("Writer and Critic must use different LLM providers")
	}
	return nil
}

// AllPassed returns true if every result in the slice passed.
func AllPassed(results []Result) bool {
	for _, r := range results {
		if !r.Passed {
			return false
		}
	}
	return true
}

// FormatResults returns a human-readable summary of check results.
func FormatResults(results []Result) string {
	var out string
	for _, r := range results {
		if r.Passed {
			out += fmt.Sprintf("  ✓ %s\n", r.Name)
		} else {
			out += fmt.Sprintf("  ✗ %s: %s\n", r.Name, r.Message)
		}
	}

	passed := 0
	for _, r := range results {
		if r.Passed {
			passed++
		}
	}
	out += fmt.Sprintf("\n%d/%d checks passed", passed, len(results))
	if !AllPassed(results) {
		out += " — fix failing checks before running the pipeline"
	}
	return out
}
