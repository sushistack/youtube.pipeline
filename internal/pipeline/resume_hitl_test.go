package pipeline_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// TestEngine_Resume_RemovesHITLSession_WhenExitingHITL seeds a paused run
// and resumes it. Because the run stays at stage=batch_review (a HITL
// stage), StatusForStage returns StatusWaiting and the session row stays.
// So we seed a non-HITL stage (e.g. write) with status=waiting (artificial
// but valid test state) to force the exit path. Since resume requires
// status ∈ {failed, waiting}, write+waiting qualifies.
func TestEngine_Resume_RemovesHITLSession_WhenExitingHITL(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	runStore := db.NewRunStore(database)
	segStore := db.NewSegmentStore(database)
	decisions := db.NewDecisionStore(database)

	outDir := t.TempDir()
	runID := "scp-049-run-9"
	ctx := context.Background()

	// Seed a run at stage=write with status=waiting (artificial but matches
	// the Resume precondition). Resume will transition it to running (not
	// waiting), which exits HITL — session row must be dropped.
	if _, err := database.ExecContext(ctx,
		`INSERT INTO runs (id, scp_id, stage, status) VALUES (?, '049', 'write', 'waiting')`,
		runID); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	// Also create the run output dir to satisfy CheckConsistency.
	if err := os.MkdirAll(filepath.Join(outDir, runID), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Pre-seed a session row simulating a prior HITL pause that somehow
	// persisted past its invariant. Resume's cleanup should drop it.
	if err := decisions.UpsertSession(ctx, &domain.HITLSession{
		RunID: runID, Stage: domain.StageBatchReview, SceneIndex: 0,
		LastInteractionTimestamp: "2026-01-01T00:00:00Z", SnapshotJSON: `{}`,
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	engine := pipeline.NewEngine(runStore, segStore, decisions, clock.RealClock{}, outDir, logger)

	if _, err := engine.Resume(ctx, runID); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	if got, _ := decisions.GetSession(ctx, runID); got != nil {
		t.Fatalf("expected session removed after resume to running, got %+v", got)
	}
}

// TestEngine_Resume_KeepsHITLSession_WhenStillWaiting verifies that a run
// that remains at a HITL stage (batch_review) after resume keeps its
// session row intact — the operator is still paused, just with a retry_count
// bumped.
func TestEngine_Resume_KeepsHITLSession_WhenStillWaiting(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.LoadRunStateFixture(t, "paused_at_batch_review")
	runStore := db.NewRunStore(database)
	segStore := db.NewSegmentStore(database)
	decisions := db.NewDecisionStore(database)

	runID := "scp-049-run-1"
	outDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outDir, runID), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// The fixture has segments referencing tts_path (nil in this fixture)
	// so no extra artifacts are required; CheckConsistency passes.

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	engine := pipeline.NewEngine(runStore, segStore, decisions, clock.RealClock{}, outDir, logger)

	ctx := context.Background()
	if _, err := engine.Resume(ctx, runID); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	// StatusForStage(batch_review) == waiting, so session row should stay.
	if got, _ := decisions.GetSession(ctx, runID); got == nil {
		t.Fatalf("expected session row preserved when resume keeps waiting status, got nil")
	}
}
