package pipeline_test

import (
	"errors"
	"math"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// TestSMOKE_04_CostCapCircuitBreaker exercises the run-level cost cap
// trip from a near-cap baseline. It is the smoke-test counterpart to
// the unit tests in cost_test.go: those drive Add() with synthetic
// inputs in isolation, while this test sets up a deterministic DB
// baseline (via seedRunAtCost) so the trip path mirrors how a
// production Phase A→B run would approach the cap mid-flight.
//
// NFR-C3 invariant covered: cost is recorded on the in-memory
// accumulator even when the cap trips (RunTotal post-Add reflects the
// over-cap delta). The original Step 3 §4 SMOKE-04 wording said
// "cost_usd reflects pre-call value", which contradicts NFR-C3 and the
// live cost.go behavior — this test follows the code, not the stale spec.
//
// The seeded runs row is intentionally a passive baseline: the cap
// accumulator does not currently consume runs.cost_usd at construction
// time (Phase B/C cost-recording wiring is CP-1 territory, deferred to
// Step 6 SMOKE-01/03). When that wiring lands, this test will swap
// `acc.Add(StageWrite, preCallCost)` for a real load-from-DB path; the
// seed is the contract anchor that future expansion targets.
//
// Runtime budget: ≤ 5 s. No ffmpeg, no external HTTP.
func TestSMOKE_04_CostCapCircuitBreaker(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	const (
		runID         = "scp-049-run-1"
		perRunCap     = 0.50
		preCallCost   = 0.499
		writerDelta   = 0.01
		expectedTotal = preCallCost + writerDelta // = 0.509, breaches 0.50
	)

	database := testutil.NewTestDB(t)
	seedRunAtCost(t, database, runID, preCallCost)

	acc := pipeline.NewCostAccumulator(nil, perRunCap)
	if err := acc.Add(domain.StageWrite, preCallCost); err != nil {
		t.Fatalf("seed accumulator: %v", err)
	}
	if acc.Tripped() {
		t.Fatal("accumulator tripped before the over-cap call — seed cost too high")
	}

	// Trip: a writer-stage cost delta pushes the run total over perRunCap.
	err := acc.Add(domain.StageWrite, writerDelta)
	if err == nil {
		t.Fatal("expected ErrCostCapExceeded, got nil")
	}
	if !errors.Is(err, domain.ErrCostCapExceeded) {
		t.Fatalf("expected errors.Is(domain.ErrCostCapExceeded), got %v", err)
	}
	if !acc.Tripped() {
		t.Error("Tripped() = false after over-cap Add")
	}
	if stage, reason := acc.TripReason(); stage != domain.StageWrite || reason != "run_cap" {
		t.Errorf("TripReason() = (%q, %q), want (write, run_cap)", stage, reason)
	}
	// NFR-C3: cost is recorded on the accumulator even when the cap trips.
	if got := acc.RunTotal(); math.Abs(got-expectedTotal) > 1e-9 {
		t.Errorf("RunTotal() = %.4f, want %.4f (NFR-C3: cost recorded on trip)", got, expectedTotal)
	}

	// Subsequent Add must keep returning the trip error so callers can
	// fail fast without re-incrementing the trip state.
	err = acc.Add(domain.StageImage, 0.001)
	if !errors.Is(err, domain.ErrCostCapExceeded) {
		t.Errorf("post-trip Add: expected ErrCostCapExceeded, got %v", err)
	}
}
