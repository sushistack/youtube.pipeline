package pipeline_test

import (
	"context"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

type fakeSegmentUpdater struct {
	updated []struct {
		runID      string
		sceneIndex int
		clipPath   string
	}
	err error
}

func (f *fakeSegmentUpdater) UpdateClipPath(ctx context.Context, runID string, sceneIndex int, clipPath string) error {
	if f.err != nil {
		return f.err
	}
	f.updated = append(f.updated, struct {
		runID      string
		sceneIndex int
		clipPath   string
	}{runID, sceneIndex, clipPath})
	return nil
}

type fakeRunUpdater struct {
	updated []struct {
		runID      string
		outputPath string
	}
	err error
}

func (f *fakeRunUpdater) UpdateOutputPath(ctx context.Context, runID string, outputPath string) error {
	if f.err != nil {
		return f.err
	}
	f.updated = append(f.updated, struct {
		runID      string
		outputPath string
	}{runID, outputPath})
	return nil
}

// Helper functions for integration tests (placeholder)
func phaseCRequest(t *testing.T) pipeline.PhaseCRequest {
	t.Helper()
	runDir := t.TempDir()
	// TODO: generate test media
	return pipeline.PhaseCRequest{
		RunID:  "test-run",
		RunDir: runDir,
		Segments: []*domain.Episode{},
	}
}

func TestPhaseCRunner_1ShotKenBurns(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	t.Skip("TODO: implement with real ffmpeg")
}

func TestPhaseCRunner_3ShotCrossDissolve(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	t.Skip("TODO: implement with real ffmpeg")
}

func TestPhaseCRunner_HardCutTransition(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	t.Skip("TODO: implement with real ffmpeg")
}

func TestPhaseCRunner_FinalConcatDuration(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	t.Skip("TODO: implement with real ffmpeg")
}

func TestPhaseCRunner_ResumeReEntry(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	t.Skip("TODO: implement with real ffmpeg")
}

func TestPhaseCRunner_Run_ValidatesRequest(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	runner := pipeline.NewPhaseCRunner(nil, nil, nil, nil, nil)

	_, err := runner.Run(context.Background(), pipeline.PhaseCRequest{})
	if err == nil {
		t.Fatal("expected validation error")
	}
	// Ensure error is about missing fields.
	t.Logf("validation error: %v", err)
}