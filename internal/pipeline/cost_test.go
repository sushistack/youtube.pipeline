package pipeline_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestCostAccumulator_NoCap_NoError(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	acc := pipeline.NewCostAccumulator(nil, 0)
	for i := 0; i < 100; i++ {
		if err := acc.Add(domain.StageWrite, 0.10); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}
	if acc.Tripped() {
		t.Fatal("expected not tripped")
	}
}

func TestCostAccumulator_PerStageCap_TripsOnOverrun(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	acc := pipeline.NewCostAccumulator(map[domain.Stage]float64{
		domain.StageWrite: 0.50,
	}, 0)

	// Under cap: clean.
	if err := acc.Add(domain.StageWrite, 0.20); err != nil {
		t.Fatalf("Add 1: %v", err)
	}
	if err := acc.Add(domain.StageWrite, 0.20); err != nil {
		t.Fatalf("Add 2: %v", err)
	}
	// Push past $0.50 cap.
	err := acc.Add(domain.StageWrite, 0.20)
	if err == nil {
		t.Fatal("expected ErrCostCapExceeded, got nil")
	}
	if !errors.Is(err, domain.ErrCostCapExceeded) {
		t.Fatalf("expected errors.Is ErrCostCapExceeded, got %v", err)
	}
	if !acc.Tripped() {
		t.Fatal("expected Tripped=true")
	}
	stage, reason := acc.TripReason()
	testutil.AssertEqual(t, stage, domain.StageWrite)
	testutil.AssertEqual(t, reason, "stage_cap")
	// NFR-C3: the over-cap cost is still recorded.
	if got := acc.StageTotal(domain.StageWrite); got < 0.5999 || got > 0.6001 {
		t.Errorf("StageTotal: got %.4f want ~0.60", got)
	}
}

func TestCostAccumulator_PerRunCap_TripsOnOverrun(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	acc := pipeline.NewCostAccumulator(nil, 1.00)
	if err := acc.Add(domain.StageResearch, 0.40); err != nil {
		t.Fatalf("Add 1: %v", err)
	}
	if err := acc.Add(domain.StageWrite, 0.40); err != nil {
		t.Fatalf("Add 2: %v", err)
	}
	err := acc.Add(domain.StageImage, 0.30)
	if !errors.Is(err, domain.ErrCostCapExceeded) {
		t.Fatalf("expected ErrCostCapExceeded, got %v", err)
	}
	_, reason := acc.TripReason()
	testutil.AssertEqual(t, reason, "run_cap")
	if got := acc.RunTotal(); got < 1.0999 || got > 1.1001 {
		t.Errorf("RunTotal: got %.4f want ~1.10", got)
	}
}

func TestCostAccumulator_NegativeCost_ValidationError(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	acc := pipeline.NewCostAccumulator(nil, 0)
	err := acc.Add(domain.StageWrite, -0.01)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	testutil.AssertEqual(t, acc.RunTotal(), 0.0)
}

func TestCostAccumulator_TrippedStaysTripped(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	acc := pipeline.NewCostAccumulator(map[domain.Stage]float64{
		domain.StageWrite: 0.10,
	}, 0)

	// Trip on first over-cap add.
	err := acc.Add(domain.StageWrite, 0.20)
	if !errors.Is(err, domain.ErrCostCapExceeded) {
		t.Fatalf("expected trip, got %v", err)
	}

	// Subsequent Add still records AND returns ErrCostCapExceeded.
	err = acc.Add(domain.StageWrite, 0.05)
	if !errors.Is(err, domain.ErrCostCapExceeded) {
		t.Fatalf("expected still tripped, got %v", err)
	}
	// NFR-C3: the post-trip cost is persisted.
	if got := acc.StageTotal(domain.StageWrite); got < 0.2499 || got > 0.2501 {
		t.Errorf("StageTotal after trip: got %.4f want ~0.25", got)
	}
}

func TestCostAccumulator_StageNotInCapMap_NoEnforcement(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	acc := pipeline.NewCostAccumulator(map[domain.Stage]float64{
		domain.StageWrite: 0.10,
	}, 0)

	// StageImage is not in the cap map: no per-stage cap applies.
	for i := 0; i < 100; i++ {
		if err := acc.Add(domain.StageImage, 0.05); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}
	if acc.Tripped() {
		t.Fatal("expected not tripped for unmapped stage")
	}
}

func TestCostAccumulator_ConcurrentAdds(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	acc := pipeline.NewCostAccumulator(map[domain.Stage]float64{
		domain.StageImage: 0.05,
	}, 0)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = acc.Add(domain.StageImage, 0.001)
		}()
	}
	wg.Wait()

	if !acc.Tripped() {
		t.Fatal("expected Tripped=true after 100 concurrent adds of $0.001 over $0.05 cap")
	}
	// Every addition must be reflected (no lost updates).
	if got := acc.StageTotal(domain.StageImage); got < 0.0999 || got > 0.1001 {
		t.Errorf("StageTotal: got %.6f want 0.100 (100 * 0.001)", got)
	}
}

func TestCostAccumulator_ErrorMessageFormat(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	acc := pipeline.NewCostAccumulator(map[domain.Stage]float64{
		domain.StageWrite: 0.50,
	}, 0)
	_ = acc.Add(domain.StageWrite, 0.60)
	err := acc.Add(domain.StageWrite, 0.01)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	// Must include stage, reason, actual, cap for operator diagnosis.
	for _, want := range []string{"write", "stage_cap", "$0.61", "$0.50"} {
		if !containsSubstring(msg, want) {
			t.Errorf("error message missing %q: %s", want, msg)
		}
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
