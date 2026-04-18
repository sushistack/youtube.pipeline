package pipeline_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// TestIntegration_AntiProgressFlow_RecordsRetryReason exercises the full flow:
// detector.Check → detector.LastSimilarity → Recorder.RecordAntiProgress →
// real RunStore → runs row. Proves FR8 and the Dev-Notes-pinned invariants:
//   - Stage/Status are NOT mutated by the anti-progress path.
//   - retry_count accumulates; retry_reason is set to "anti_progress".
//   - slog emits one Warn + one Info line.
//   - ErrAntiProgress classifies to (422, "ANTI_PROGRESS", false).
func TestIntegration_AntiProgressFlow_RecordsRetryReason(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.LoadRunStateFixture(t, "running_at_write")
	runStore := db.NewRunStore(database)
	accumulator := pipeline.NewCostAccumulator(nil, 0)
	logger, logBuf := testutil.CaptureLog(t)
	recorder := pipeline.NewRecorder(runStore, accumulator, clock.RealClock{}, logger)

	detector, err := pipeline.NewAntiProgressDetector(0.92)
	if err != nil {
		t.Fatalf("constructor: %v", err)
	}

	// Simulated Critic retry loop: Writer produces near-identical output on
	// consecutive attempts despite Critic feedback — the detector must trip.
	outputs := []string{
		"scp-049 describes an entity with anomalous properties demanding specific containment procedures immediately.",
		"scp-049 describes an entity with anomalous properties demanding specific containment procedures immediately.",
	}

	const runID = "scp-049-run-1"
	ctx := context.Background()

	for i, out := range outputs {
		stop, sim := detector.Check(out)
		switch i {
		case 0:
			if stop {
				t.Fatalf("baseline should not trip: sim=%.6f", sim)
			}
		case 1:
			if !stop {
				t.Fatalf("near-duplicate should trip at threshold 0.92: sim=%.6f", sim)
			}
			if err := recorder.RecordAntiProgress(ctx, runID, domain.StageWrite, detector.LastSimilarity(), 0.92); err != nil {
				t.Fatalf("RecordAntiProgress: %v", err)
			}
		}
	}

	// Verify runs row state.
	got, err := runStore.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	testutil.AssertEqual(t, got.Stage, domain.StageWrite)
	testutil.AssertEqual(t, got.Status, domain.StatusRunning)
	testutil.AssertEqual(t, got.RetryCount, 1)
	if got.RetryReason == nil || *got.RetryReason != "anti_progress" {
		t.Errorf("RetryReason = %v, want \"anti_progress\"", got.RetryReason)
	}
	testutil.AssertEqual(t, got.CostUSD, 0.0)

	// Assert slog captured: one "anti-progress detected" Warn + one "stage observation" Info.
	logOut := logBuf.String()
	if strings.Count(logOut, `"msg":"anti-progress detected"`) != 1 {
		t.Errorf("expected exactly 1 anti-progress detected log line:\n%s", logOut)
	}
	if strings.Count(logOut, `"msg":"stage observation"`) != 1 {
		t.Errorf("expected exactly 1 stage observation log line:\n%s", logOut)
	}

	// Classify the escalation error the caller surfaces to the engine.
	escalated := fmt.Errorf("write stage: %w", domain.ErrAntiProgress)
	if !errors.Is(escalated, domain.ErrAntiProgress) {
		t.Error("errors.Is failed to unwrap ErrAntiProgress")
	}
	status, code, retryable := domain.Classify(escalated)
	testutil.AssertEqual(t, status, 422)
	testutil.AssertEqual(t, code, "ANTI_PROGRESS")
	testutil.AssertEqual(t, retryable, false)
}

// TestIntegration_AntiProgressFlow_PreservesPriorCost pins that a prior
// Writer Record (with real cost) is not clobbered by the follow-up
// RecordAntiProgress event. Anti-progress is a decision-only event with
// cost=0; the existing cost_usd column must accumulate monotonically.
func TestIntegration_AntiProgressFlow_PreservesPriorCost(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.LoadRunStateFixture(t, "running_at_write")
	runStore := db.NewRunStore(database)
	accumulator := pipeline.NewCostAccumulator(nil, 0)
	logger, _ := testutil.CaptureLog(t)
	recorder := pipeline.NewRecorder(runStore, accumulator, clock.RealClock{}, logger)

	const runID = "scp-049-run-1"
	ctx := context.Background()

	// Writer attempt: incurs real cost.
	writerObs := domain.StageObservation{
		Stage:      domain.StageWrite,
		DurationMs: 1200,
		TokenIn:    320,
		TokenOut:   410,
		CostUSD:    0.05,
	}
	if err := recorder.Record(ctx, runID, writerObs); err != nil {
		t.Fatalf("Writer Record: %v", err)
	}

	// Anti-progress fires on the NEXT attempt — convention is cost=0.
	if err := recorder.RecordAntiProgress(ctx, runID, domain.StageWrite, 0.98, 0.92); err != nil {
		t.Fatalf("RecordAntiProgress: %v", err)
	}

	got, err := runStore.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	// Cost, tokens, duration must be the Writer's ($0.05, 320, 410, 1200).
	testutil.AssertEqual(t, got.CostUSD, 0.05)
	testutil.AssertEqual(t, got.TokenIn, 320)
	testutil.AssertEqual(t, got.TokenOut, 410)
	testutil.AssertEqual(t, got.DurationMs, int64(1200))
	// retry_count accumulates only the anti-progress event (Writer sent 0).
	testutil.AssertEqual(t, got.RetryCount, 1)
	if got.RetryReason == nil || *got.RetryReason != "anti_progress" {
		t.Errorf("RetryReason = %v, want \"anti_progress\"", got.RetryReason)
	}
	// Stage/status invariant preserved.
	testutil.AssertEqual(t, got.Stage, domain.StageWrite)
	testutil.AssertEqual(t, got.Status, domain.StatusRunning)
}
