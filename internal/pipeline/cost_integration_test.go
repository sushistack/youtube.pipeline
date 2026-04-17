package pipeline_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// TestRecorder_PerStageCap_HardStop asserts NFR-C1 (per-stage cap) and NFR-C3
// (over-cap cost is still persisted — no truncation).
func TestRecorder_PerStageCap_HardStop(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	outDir := t.TempDir()
	ctx := context.Background()

	run, _ := store.Create(ctx, "049", outDir)

	acc := pipeline.NewCostAccumulator(map[domain.Stage]float64{
		domain.StageWrite: 0.50,
	}, 5.00)
	logger, _ := testutil.CaptureLog(t)
	rec := pipeline.NewRecorder(store, acc, clock.RealClock{}, logger)

	if err := rec.Record(ctx, run.ID, domain.StageObservation{Stage: domain.StageWrite, CostUSD: 0.20}); err != nil {
		t.Fatalf("record 1: %v", err)
	}
	if err := rec.Record(ctx, run.ID, domain.StageObservation{Stage: domain.StageWrite, CostUSD: 0.20}); err != nil {
		t.Fatalf("record 2: %v", err)
	}
	// Third call pushes past $0.50 cap.
	err := rec.Record(ctx, run.ID, domain.StageObservation{Stage: domain.StageWrite, CostUSD: 0.20})
	if !errors.Is(err, domain.ErrCostCapExceeded) {
		t.Fatalf("expected ErrCostCapExceeded, got %v", err)
	}

	// NFR-C3: persisted cost includes the over-cap spend.
	got, _ := store.Get(ctx, run.ID)
	if got.CostUSD < 0.5999 || got.CostUSD > 0.6001 {
		t.Errorf("CostUSD: got %.4f want ~0.60", got.CostUSD)
	}

	// domain.Classify → (402, COST_CAP_EXCEEDED, false).
	status, code, retryable := domain.Classify(err)
	testutil.AssertEqual(t, status, 402)
	testutil.AssertEqual(t, code, "COST_CAP_EXCEEDED")
	testutil.AssertEqual(t, retryable, false)

	// Subsequent Record still records (NFR-C3) AND still errors (Tripped).
	err = rec.Record(ctx, run.ID, domain.StageObservation{Stage: domain.StageWrite, CostUSD: 0.05})
	if !errors.Is(err, domain.ErrCostCapExceeded) {
		t.Fatalf("expected still tripped, got %v", err)
	}
	got, _ = store.Get(ctx, run.ID)
	if got.CostUSD < 0.6499 || got.CostUSD > 0.6501 {
		t.Errorf("post-trip CostUSD: got %.4f want ~0.65", got.CostUSD)
	}
}

// TestRecorder_PerRunCap_HardStop asserts NFR-C2 (per-run cap) triggers on
// cumulative spend across multiple stages.
func TestRecorder_PerRunCap_HardStop(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	outDir := t.TempDir()
	ctx := context.Background()

	run, _ := store.Create(ctx, "049", outDir)

	// Per-stage caps effectively disabled; per-run cap at $1.00.
	acc := pipeline.NewCostAccumulator(map[domain.Stage]float64{
		domain.StageResearch: 999,
		domain.StageWrite:    999,
		domain.StageImage:    999,
	}, 1.00)
	logger, _ := testutil.CaptureLog(t)
	rec := pipeline.NewRecorder(store, acc, clock.RealClock{}, logger)

	if err := rec.Record(ctx, run.ID, domain.StageObservation{Stage: domain.StageResearch, CostUSD: 0.40}); err != nil {
		t.Fatalf("record 1: %v", err)
	}
	if err := rec.Record(ctx, run.ID, domain.StageObservation{Stage: domain.StageWrite, CostUSD: 0.40}); err != nil {
		t.Fatalf("record 2: %v", err)
	}
	err := rec.Record(ctx, run.ID, domain.StageObservation{Stage: domain.StageImage, CostUSD: 0.30})
	if !errors.Is(err, domain.ErrCostCapExceeded) {
		t.Fatalf("expected ErrCostCapExceeded, got %v", err)
	}
	_, reason := acc.TripReason()
	testutil.AssertEqual(t, reason, "run_cap")

	got, _ := store.Get(ctx, run.ID)
	if got.CostUSD < 1.0999 || got.CostUSD > 1.1001 {
		t.Errorf("CostUSD: got %.4f want ~1.10", got.CostUSD)
	}
}

// TestRecorder_PerStageCap_StatusNotAdvanced pins that the cost-cap path
// (like the retry path) never calls SetStatus on its own. Status transitions
// are the engine's job; Recorder is observability only.
func TestRecorder_PerStageCap_StatusNotAdvanced(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	outDir := t.TempDir()
	ctx := context.Background()

	run, _ := store.Create(ctx, "049", outDir)

	acc := pipeline.NewCostAccumulator(map[domain.Stage]float64{
		domain.StageWrite: 0.05,
	}, 0)
	logger, _ := testutil.CaptureLog(t)
	rec := pipeline.NewRecorder(store, acc, clock.RealClock{}, logger)

	_ = rec.Record(ctx, run.ID, domain.StageObservation{Stage: domain.StageWrite, CostUSD: 0.10})

	got, _ := store.Get(ctx, run.ID)
	testutil.AssertEqual(t, got.Stage, domain.StagePending) // Create sets pending; unchanged.
	testutil.AssertEqual(t, got.Status, domain.StatusPending)
}
