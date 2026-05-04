package pipeline_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
)

func TestFileTraceWriter_PerAttemptFile(t *testing.T) {
	dir := t.TempDir()
	w := pipeline.NewFileTraceWriter(dir)

	runID := "scp-049-run-1"
	ctx := pipeline.WithTraceRunID(context.Background(), runID)

	entry := domain.TraceEntry{
		Stage:         "writer",
		PromptRendered: "test prompt",
		Provider:      "dashscope",
		Model:         "qwen-plus",
	}

	// First write → writer.001.json
	if err := w.Write(ctx, entry); err != nil {
		t.Fatalf("Write 1: %v", err)
	}
	// Second write (same stage) → writer.002.json
	if err := w.Write(ctx, entry); err != nil {
		t.Fatalf("Write 2: %v", err)
	}

	tracesDir := filepath.Join(dir, runID, "traces")
	for _, wantFile := range []string{"writer.001.json", "writer.002.json"} {
		path := filepath.Join(tracesDir, wantFile)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected trace file %s: %v", wantFile, err)
		}
	}
}

func TestFileTraceWriter_DifferentStagesIndependentCounters(t *testing.T) {
	dir := t.TempDir()
	w := pipeline.NewFileTraceWriter(dir)
	runID := "run-1"
	ctx := pipeline.WithTraceRunID(context.Background(), runID)

	_ = w.Write(ctx, domain.TraceEntry{Stage: "writer"})
	_ = w.Write(ctx, domain.TraceEntry{Stage: "critic"})
	_ = w.Write(ctx, domain.TraceEntry{Stage: "writer"})

	tracesDir := filepath.Join(dir, runID, "traces")
	// writer gets .001 and .002; critic gets .001 independently
	for _, wantFile := range []string{"writer.001.json", "writer.002.json", "critic.001.json"} {
		if _, err := os.Stat(filepath.Join(tracesDir, wantFile)); err != nil {
			t.Errorf("expected %s: %v", wantFile, err)
		}
	}
}

func TestFileTraceWriter_EmptyStage_Error(t *testing.T) {
	dir := t.TempDir()
	w := pipeline.NewFileTraceWriter(dir)
	err := w.Write(context.Background(), domain.TraceEntry{Stage: ""})
	if err == nil {
		t.Error("expected error for empty stage, got nil")
	}
}

func TestNoopTraceWriter_NoDiskWrite(t *testing.T) {
	dir := t.TempDir()
	w := pipeline.NoopTraceWriter{}
	ctx := pipeline.WithTraceRunID(context.Background(), "run-1")
	if err := w.Write(ctx, domain.TraceEntry{Stage: "writer"}); err != nil {
		t.Fatalf("NoopTraceWriter.Write: %v", err)
	}
	// No files should exist
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no files in dir, got %d entries", len(entries))
	}
}

func TestWithTraceRunID_RoundTrip(t *testing.T) {
	ctx := pipeline.WithTraceRunID(context.Background(), "my-run")
	// FileTraceWriter.Write uses the context key; we test it indirectly by
	// checking that Write produces a file in the correct run subdirectory.
	dir := t.TempDir()
	w := pipeline.NewFileTraceWriter(dir)
	_ = w.Write(ctx, domain.TraceEntry{Stage: "critic"})
	path := filepath.Join(dir, "my-run", "traces", "critic.001.json")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("trace file not at expected path %s: %v", path, err)
	}
}
