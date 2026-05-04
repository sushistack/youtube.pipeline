package eval

import (
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestComputeCalibration_PerfectAgreement(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	pairs := []PairResult{
		{Index: 1, NegVerdict: "retry", PosVerdict: "pass"},
		{Index: 2, NegVerdict: "retry", PosVerdict: "pass"},
		{Index: 3, NegVerdict: "retry", PosVerdict: "pass"},
	}
	snap := computeCalibration(pairs)
	testutil.AssertEqual(t, 6, snap.Observations)
	testutil.AssertEqual(t, 3, snap.AgreementPassPass)
	testutil.AssertEqual(t, 3, snap.AgreementRetryRetry)
	testutil.AssertEqual(t, 0, snap.DisagreementPassRetry)
	testutil.AssertEqual(t, 0, snap.DisagreementRetryPass)
	if snap.Kappa == nil {
		t.Fatal("expected non-nil kappa for perfect agreement")
	}
	if *snap.Kappa != 1.0 {
		t.Errorf("expected kappa=1.0 for perfect agreement, got %v", *snap.Kappa)
	}
	if !snap.FloorOK {
		t.Error("expected FloorOK=true for kappa=1.0")
	}
}

func TestComputeCalibration_FloorBreach(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	// Construct a 2x2 with kappa < 0.6 by simulating mixed agreement.
	// 5 fixture pairs (10 rows): pos pass=3, pos retry=2, neg retry=3, neg pass=2.
	// a=3, b=2, c=2, d=3 -> po=0.6, pe=0.5 -> kappa=0.2 (well below 0.6)
	pairs := []PairResult{
		{Index: 1, NegVerdict: "retry", PosVerdict: "pass"},
		{Index: 2, NegVerdict: "retry", PosVerdict: "pass"},
		{Index: 3, NegVerdict: "retry", PosVerdict: "pass"},
		{Index: 4, NegVerdict: "pass", PosVerdict: "retry"},
		{Index: 5, NegVerdict: "pass", PosVerdict: "retry"},
	}
	snap := computeCalibration(pairs)
	if snap.Kappa == nil {
		t.Fatal("expected non-nil kappa for mixed agreement")
	}
	if *snap.Kappa >= CalibrationFloor {
		t.Errorf("expected kappa < %v, got %v", CalibrationFloor, *snap.Kappa)
	}
	if snap.FloorOK {
		t.Error("expected FloorOK=false when kappa < floor")
	}
}

func TestComputeCalibration_Empty(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	snap := computeCalibration(nil)
	testutil.AssertEqual(t, 0, snap.Observations)
	if snap.Kappa != nil {
		t.Errorf("expected nil kappa for empty input, got %v", *snap.Kappa)
	}
	if snap.FloorOK {
		t.Error("expected FloorOK=false when no pairs")
	}
	if snap.Reason == "" {
		t.Error("expected non-empty Reason when kappa is nil")
	}
}

func TestComputeCalibration_AllSameActualVerdict_KappaZero(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	// When every fixture's actual verdict is "pass" (negatives miss, positives
	// pass), variance exists in the expected dimension but not in the actual
	// dimension. Cohen's kappa: po = (a+d)/n; the actual-pass column carries
	// all observations, so po-pe collapses but pe < 1 — kappa is computable
	// at zero, not flagged as degenerate.
	pairs := []PairResult{
		{Index: 1, NegVerdict: "pass", PosVerdict: "pass"},
		{Index: 2, NegVerdict: "pass", PosVerdict: "pass"},
	}
	snap := computeCalibration(pairs)
	if snap.Kappa == nil {
		t.Fatal("expected non-nil kappa for this configuration")
	}
	if *snap.Kappa != 0 {
		t.Errorf("expected kappa=0 when po==pe, got %v", *snap.Kappa)
	}
	if snap.FloorOK {
		t.Error("expected FloorOK=false when kappa=0")
	}
}

func TestComputeCalibration_DegenerateAllOneCell(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	// True degeneracy: feed a contingency that pins all weight in a single
	// cell so pe == 1.0. The fastest way to construct this from PairResult
	// is to issue zero pair rows except a synthetic row that lands all
	// observations in (a) — which we cannot do via real pair shape. Verify
	// the cohensKappa primitive directly for this corner.
	kappa, ok, reason := cohensKappa(5, 0, 0, 0)
	if ok {
		t.Errorf("expected ok=false (degenerate single-cell), got kappa=%v", kappa)
	}
	if reason == "" {
		t.Error("expected non-empty degenerate reason")
	}
}

func TestNormalizeBinaryVerdict(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	cases := []struct {
		in       string
		expected string
	}{
		{"retry", "retry"},
		{"pass", "pass"},
		{"accept_with_notes", "pass"},
		{"", "pass"},
		{"unknown_taxonomy", "pass"},
	}
	for _, c := range cases {
		got := normalizeBinaryVerdict(c.in)
		if got != c.expected {
			t.Errorf("normalizeBinaryVerdict(%q) = %q, want %q", c.in, got, c.expected)
		}
	}
}
