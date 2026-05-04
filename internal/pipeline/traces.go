package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// FileTraceWriter persists per-attempt LLM traces to disk. Each call
// produces one JSON file at {outputDir}/{runID}/traces/{stage}.{NNN}.json
// where NNN is a zero-padded monotonic counter scoped to the (runID,
// stage) tuple.
//
// The (runID, stage) counter is held in-memory under a mutex; restarts
// reset counters to zero, which is fine because each restart writes to
// a fresh run directory. Atomic writes (tmp → rename) mirror the
// scenario.json pattern so a partial JSON file never appears under
// traces/.
//
// Implements domain.TraceWriter so agents can emit traces via
// TextAgentConfig.TraceWriter without an import cycle through pipeline.
type FileTraceWriter struct {
	outputDir string

	mu       sync.Mutex
	counters map[string]int // key = runID + "\x00" + stage
}

// NewFileTraceWriter creates a writer rooted at outputDir. Per-run
// trace files land in {outputDir}/{runID}/traces/.
func NewFileTraceWriter(outputDir string) *FileTraceWriter {
	return &FileTraceWriter{
		outputDir: outputDir,
		counters:  map[string]int{},
	}
}

// Write persists entry to a new per-attempt file. AttemptNum on the
// passed-in entry is OVERRIDDEN with the writer's monotonic counter for
// (runID, entry.Stage) — callers cannot pre-assign an attempt number.
//
// runID is sourced from the context-scoped key set by WithTraceRunID;
// when missing the writer falls back to "unknown" so a misconfigured
// caller still produces a debuggable file rather than crashing.
func (w *FileTraceWriter) Write(ctx context.Context, entry domain.TraceEntry) error {
	if entry.Stage == "" {
		return fmt.Errorf("trace writer: stage is empty")
	}
	runID := traceRunIDFromContext(ctx)
	if runID == "" {
		runID = "unknown"
	}

	w.mu.Lock()
	key := runID + "\x00" + entry.Stage
	w.counters[key]++
	attempt := w.counters[key]
	w.mu.Unlock()

	entry.AttemptNum = attempt

	tracesDir := filepath.Join(w.outputDir, runID, "traces")
	if err := os.MkdirAll(tracesDir, 0o755); err != nil {
		return fmt.Errorf("trace writer: mkdir: %w", err)
	}
	filename := fmt.Sprintf("%s.%03d.json", entry.Stage, attempt)
	path := filepath.Join(tracesDir, filename)

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("trace writer: marshal: %w", err)
	}
	// tmp → fsync → rename matches finalize_phase_a.go:55-86. Skipping
	// fsync risks a zero-length trace file surviving a crash; for debug
	// data that's mostly cosmetic, but the cache write follows the same
	// policy and consistency outweighs the syscall cost.
	tmp, err := os.CreateTemp(tracesDir, filename+".*.tmp")
	if err != nil {
		return fmt.Errorf("trace writer: create temp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("trace writer: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("trace writer: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("trace writer: close: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		cleanup()
		return fmt.Errorf("trace writer: chmod: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("trace writer: rename: %w", err)
	}
	return nil
}

// NoopTraceWriter discards every entry. Used when debug_traces=false so
// the production hot path makes no disk syscalls.
type NoopTraceWriter struct{}

func (NoopTraceWriter) Write(_ context.Context, _ domain.TraceEntry) error { return nil }

// traceRunIDKey is the unexported context key for the current run ID.
// Using an unexported type prevents accidental key collision with
// other packages.
type traceRunIDKey struct{}

// WithTraceRunID returns a context tagged with the current run ID so
// downstream code (TraceWriter implementations) can route entries to
// the right per-run traces/ directory without an extra parameter on
// every Write call. PhaseARunner.Run sets this once at the top.
func WithTraceRunID(ctx context.Context, runID string) context.Context {
	return context.WithValue(ctx, traceRunIDKey{}, runID)
}

// traceRunIDFromContext extracts the run ID set by WithTraceRunID.
// Returns "" when the context was not tagged.
func traceRunIDFromContext(ctx context.Context) string {
	v := ctx.Value(traceRunIDKey{})
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
