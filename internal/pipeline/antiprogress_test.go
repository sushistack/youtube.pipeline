package pipeline_test

import (
	"errors"
	"math"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestNewAntiProgressDetector_ValidThresholds(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	for _, th := range []float64{0.01, 0.5, 0.92, 0.999, 1.0} {
		d, err := pipeline.NewAntiProgressDetector(th)
		if err != nil {
			t.Errorf("threshold %.4f: unexpected error %v", th, err)
		}
		if d == nil {
			t.Errorf("threshold %.4f: detector is nil", th)
		}
	}
}

func TestNewAntiProgressDetector_InvalidThresholds(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	for _, th := range []float64{0.0, -0.1, 1.01, 2.0, -1.0} {
		d, err := pipeline.NewAntiProgressDetector(th)
		if err == nil {
			t.Errorf("threshold %.4f: expected error, got nil", th)
			continue
		}
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("threshold %.4f: expected ErrValidation, got %v", th, err)
		}
		if d != nil {
			t.Errorf("threshold %.4f: expected nil detector on error", th)
		}
	}
}

func TestDetector_FirstCallNeverTrips(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	d, err := pipeline.NewAntiProgressDetector(0.01)
	if err != nil {
		t.Fatalf("constructor: %v", err)
	}
	stop, sim := d.Check("some long writer output")
	if stop {
		t.Error("first Check should never trip")
	}
	if sim != 0.0 {
		t.Errorf("first Check similarity = %v, want 0.0", sim)
	}
}

func TestDetector_ThresholdCrossing(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	cases := []struct {
		name      string
		prev      string
		curr      string
		threshold float64
		wantStop  bool
	}{
		{"identical at 0.92", "the quick brown fox", "the quick brown fox", 0.92, true},
		{"4of5 at 0.92 below", "the quick brown fox", "the quick brown fox jumped", 0.92, false},
		{"repeat-scaling identical", "hello world", "hello world hello world", 0.92, true},
		{"dissimilar", "hello", "goodbye", 0.92, false},
		{"identical at threshold 1.0 strict >", "a b c d e", "a b c d e", 1.0, false},
		{"identical at threshold 0.999999", "a b c d e", "a b c d e", 0.999999, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := pipeline.NewAntiProgressDetector(tc.threshold)
			if err != nil {
				t.Fatalf("constructor: %v", err)
			}
			if stop, _ := d.Check(tc.prev); stop {
				t.Fatal("first Check should not trip")
			}
			stop, sim := d.Check(tc.curr)
			if stop != tc.wantStop {
				t.Errorf("Check(%q vs %q) stop=%v sim=%.6f want stop=%v", tc.prev, tc.curr, stop, sim, tc.wantStop)
			}
		})
	}
}

func TestDetector_ConfigurableThreshold(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	// Cosine of the pair ≈ 0.8944.
	a := "the quick brown fox"
	b := "the quick brown fox jumped"
	cases := []struct {
		threshold float64
		wantStop  bool
	}{
		{0.50, true},
		{0.80, true},
		{0.92, false},
		{0.95, false},
	}
	for _, tc := range cases {
		d, err := pipeline.NewAntiProgressDetector(tc.threshold)
		if err != nil {
			t.Fatalf("threshold %.2f constructor: %v", tc.threshold, err)
		}
		d.Check(a)
		stop, sim := d.Check(b)
		if stop != tc.wantStop {
			t.Errorf("threshold %.2f: stop=%v sim=%.6f, want stop=%v", tc.threshold, stop, sim, tc.wantStop)
		}
	}
}

func TestDetector_NoFalsePositiveOnDissimilar(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	// Five thematically adjacent but lexically varied SCP-style summaries.
	outputs := []string{
		"SCP-049 presents as a medieval plague doctor exhibiting anomalous surgical behavior toward subjects.",
		"Containment procedures require Class-D personnel rotation under Site-19 supervision every seventy-two hours.",
		"Cross-testing between Keter entities remains prohibited following the catastrophic breach of Area-14.",
		"Foundation researchers observe subject cognition drifts sharply when exposed to memetic stimulus vectors.",
		"Standard amnestic protocols are ineffective on entities demonstrating recursive memory implantation.",
	}
	d, err := pipeline.NewAntiProgressDetector(0.92)
	if err != nil {
		t.Fatalf("constructor: %v", err)
	}
	for i, out := range outputs {
		stop, sim := d.Check(out)
		if stop {
			t.Errorf("output %d unexpectedly tripped detector (sim=%.4f): %q", i, sim, out)
		}
	}
}

func TestDetector_EmptyInputDoesNotRotateBaseline(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	d, err := pipeline.NewAntiProgressDetector(0.92)
	if err != nil {
		t.Fatalf("constructor: %v", err)
	}
	// Baseline set by the first non-empty Check.
	if stop, _ := d.Check("hello world"); stop {
		t.Fatal("baseline Check tripped")
	}
	// Empty does not rotate.
	stop, sim := d.Check("")
	if stop || sim != 0.0 {
		t.Errorf("empty Check: stop=%v sim=%v, want false/0.0", stop, sim)
	}
	// Next Check compares to the ORIGINAL baseline ("hello world"), not "".
	stop, sim = d.Check("hello world")
	if !stop {
		t.Errorf("expected trip on identical baseline restore; got stop=%v sim=%.6f", stop, sim)
	}
	if math.Abs(sim-1.0) > 1e-9 {
		t.Errorf("expected sim≈1.0, got %v", sim)
	}
}

func TestDetector_ResetClearsBaseline(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	d, err := pipeline.NewAntiProgressDetector(0.92)
	if err != nil {
		t.Fatalf("constructor: %v", err)
	}
	d.Check("hello world")
	// Establish a non-zero lastSim so the post-Reset assertion is meaningful.
	d.Check("hello world")
	if d.LastSimilarity() == 0 {
		t.Fatal("precondition: LastSimilarity should be non-zero before Reset")
	}
	d.Reset()
	if got := d.LastSimilarity(); got != 0 {
		t.Errorf("Reset did not clear LastSimilarity; got %v, want 0", got)
	}
	// After reset, the next Check acts as "first call".
	stop, sim := d.Check("hello world")
	if stop {
		t.Errorf("after Reset, first Check should not trip; got stop=%v sim=%v", stop, sim)
	}
	if sim != 0.0 {
		t.Errorf("after Reset, first Check similarity = %v, want 0.0", sim)
	}
}

func TestDetector_WhitespaceOnlyDoesNotRotateBaseline(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	d, err := pipeline.NewAntiProgressDetector(0.92)
	if err != nil {
		t.Fatalf("constructor: %v", err)
	}
	// Baseline set.
	if stop, _ := d.Check("hello world"); stop {
		t.Fatal("baseline should not trip")
	}
	// Whitespace-only outputs must NOT rotate the baseline.
	for _, ws := range []string{"   ", "\n\t", "\r\n", "      \t\n"} {
		stop, sim := d.Check(ws)
		if stop || sim != 0.0 {
			t.Errorf("whitespace-only %q: stop=%v sim=%v, want false/0.0", ws, stop, sim)
		}
	}
	// Next real comparison must go against the ORIGINAL baseline.
	stop, _ := d.Check("hello world")
	if !stop {
		t.Error("expected trip on identical baseline after whitespace-only interruptions")
	}
}

func TestDetector_LastSimilarityMatches(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	d, err := pipeline.NewAntiProgressDetector(0.5)
	if err != nil {
		t.Fatalf("constructor: %v", err)
	}
	d.Check("the quick brown fox")
	_, sim := d.Check("the quick brown fox jumped")
	if got := d.LastSimilarity(); got != sim {
		t.Errorf("LastSimilarity()=%v, want %v", got, sim)
	}
}
