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

// TestIntegration_Resume_FailedAtTTS exercises the full Resume path
// against real SQLite stores + on-disk artifacts, using the
// failed_at_tts.sql fixture.
func TestIntegration_Resume_FailedAtTTS(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	database := testutil.LoadRunStateFixture(t, "failed_at_tts")
	runStore := db.NewRunStore(database)
	segStore := db.NewSegmentStore(database)

	// Materialize the tts/*.wav files the DB claims exist.
	outDir := t.TempDir()
	runDir := filepath.Join(outDir, "scp-049-run-1")
	mustWrite(t, filepath.Join(runDir, "tts", "scene_01.wav"), "w1")
	mustWrite(t, filepath.Join(runDir, "tts", "scene_02.wav"), "w2")
	mustWrite(t, filepath.Join(runDir, "tts", "scene_03.wav"), "w3")
	// Preserve scenario.json (completed Phase A work).
	mustWrite(t, filepath.Join(runDir, "scenario.json"), `{"scenes":[]}`)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	engine := pipeline.NewEngine(runStore, segStore, clock.RealClock{}, outDir, logger)

	ctx := context.Background()

	before, err := runStore.Get(ctx, "scp-049-run-1")
	if err != nil {
		t.Fatalf("Get before: %v", err)
	}
	testutil.AssertEqual(t, before.Status, domain.StatusFailed)
	testutil.AssertEqual(t, before.RetryCount, 1)
	beforeUpdated := before.UpdatedAt

	if _, err := engine.Resume(ctx, "scp-049-run-1"); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	// Phase B clean slate — all segments deleted.
	segs, _ := segStore.ListByRunID(ctx, "scp-049-run-1")
	if len(segs) != 0 {
		t.Errorf("segments not cleared; got %d", len(segs))
	}
	// tts/ removed entirely.
	if _, err := os.Stat(filepath.Join(runDir, "tts")); !os.IsNotExist(err) {
		t.Errorf("tts dir still present; stat err = %v", err)
	}
	// scenario.json preserved.
	if _, err := os.Stat(filepath.Join(runDir, "scenario.json")); err != nil {
		t.Errorf("scenario.json removed unexpectedly: %v", err)
	}
	// Status reset to running (automated stage); retry_reason cleared.
	after, _ := runStore.Get(ctx, "scp-049-run-1")
	testutil.AssertEqual(t, after.Status, domain.StatusRunning)
	if after.RetryReason != nil {
		t.Errorf("RetryReason = %v, want nil (cleared)", *after.RetryReason)
	}
	// retry_count incremented.
	testutil.AssertEqual(t, after.RetryCount, 2)
	// updated_at advanced (Migration 002 trigger fired).
	if after.UpdatedAt <= beforeUpdated {
		t.Errorf("updated_at did not advance: before=%q after=%q",
			beforeUpdated, after.UpdatedAt)
	}
}

// TestIntegration_Resume_IdempotentAgainstRealStores covers NFR-R1 at the
// integration layer: two sequential Resumes against a real SQLite DB and
// a real on-disk tree produce the same terminal state (stage, status,
// segments count, file tree). retry_count legitimately drifts +1 per
// resume and is NOT part of the NFR-R1 invariant.
func TestIntegration_Resume_IdempotentAgainstRealStores(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	database := testutil.LoadRunStateFixture(t, "failed_at_tts")
	runStore := db.NewRunStore(database)
	segStore := db.NewSegmentStore(database)

	outDir := t.TempDir()
	runDir := filepath.Join(outDir, "scp-049-run-1")
	mustWrite(t, filepath.Join(runDir, "tts", "scene_01.wav"), "w1")
	mustWrite(t, filepath.Join(runDir, "tts", "scene_02.wav"), "w2")
	mustWrite(t, filepath.Join(runDir, "tts", "scene_03.wav"), "w3")
	mustWrite(t, filepath.Join(runDir, "scenario.json"), `{"scenes":[]}`)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	engine := pipeline.NewEngine(runStore, segStore, clock.RealClock{}, outDir, logger)

	ctx := context.Background()

	// First resume.
	if _, err := engine.Resume(ctx, "scp-049-run-1"); err != nil {
		t.Fatalf("first Resume: %v", err)
	}
	after1Run, _ := runStore.Get(ctx, "scp-049-run-1")
	after1Segs, _ := segStore.ListByRunID(ctx, "scp-049-run-1")
	after1Tree := snapshotFileTree(t, runDir)
	after1RetryCount := after1Run.RetryCount

	// Force status back to failed so the second Resume has a legal entry.
	// This simulates the run failing again (e.g., retry exhausted after the
	// resume re-queued the stage).
	if _, err := database.ExecContext(ctx,
		`UPDATE runs SET status = 'failed' WHERE id = ?`, "scp-049-run-1"); err != nil {
		t.Fatalf("re-seed status=failed: %v", err)
	}

	// Second resume.
	if _, err := engine.Resume(ctx, "scp-049-run-1"); err != nil {
		t.Fatalf("second Resume: %v", err)
	}
	after2Run, _ := runStore.Get(ctx, "scp-049-run-1")
	after2Segs, _ := segStore.ListByRunID(ctx, "scp-049-run-1")
	after2Tree := snapshotFileTree(t, runDir)

	// NFR-R1 invariants: stage, status, segment count, file tree match.
	testutil.AssertEqual(t, after2Run.Stage, after1Run.Stage)
	testutil.AssertEqual(t, after2Run.Status, after1Run.Status)
	testutil.AssertEqual(t, len(after2Segs), len(after1Segs))
	if !slicesEqual(after1Tree, after2Tree) {
		t.Errorf("file tree drift:\n  first:  %v\n  second: %v", after1Tree, after2Tree)
	}
	// retry_count bumps each resume, outside NFR-R1.
	if after2Run.RetryCount != after1RetryCount+1 {
		t.Errorf("retry_count = %d, want %d (first + 1)",
			after2Run.RetryCount, after1RetryCount+1)
	}
}

// TestIntegration_Resume_FailedAtWrite covers Phase A failure — no on-disk
// cleanup, no segments delete, status reset only.
func TestIntegration_Resume_FailedAtWrite(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	database := testutil.LoadRunStateFixture(t, "failed_at_write")
	runStore := db.NewRunStore(database)
	segStore := db.NewSegmentStore(database)

	outDir := t.TempDir()
	// No on-disk artifacts seeded — Phase A is in-memory.

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	engine := pipeline.NewEngine(runStore, segStore, clock.RealClock{}, outDir, logger)

	ctx := context.Background()
	if _, err := engine.Resume(ctx, "scp-049-run-1"); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	// No segments to delete.
	segs, _ := segStore.ListByRunID(ctx, "scp-049-run-1")
	if len(segs) != 0 {
		t.Errorf("unexpected segments for Phase A resume: %d", len(segs))
	}
	// Status reset.
	after, _ := runStore.Get(ctx, "scp-049-run-1")
	testutil.AssertEqual(t, after.Status, domain.StatusRunning)
	testutil.AssertEqual(t, after.RetryCount, 1) // was 0 in fixture.
}
