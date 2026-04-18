package service

import (
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestCohensKappa_PerfectAgreement(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	got, ok, reason := CohensKappa(8, 0, 0, 12)
	if !ok {
		t.Fatalf("ok = false, reason = %q", reason)
	}
	testutil.AssertFloatNear(t, got, 1.0, 1e-9)
}

func TestCohensKappa_Chance(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	got, ok, reason := CohensKappa(5, 5, 5, 5)
	if !ok {
		t.Fatalf("ok = false, reason = %q", reason)
	}
	testutil.AssertFloatNear(t, got, 0.0, 1e-9)
}

func TestCohensKappa_KnownTextbookExample(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	// Canonical 2-rater / 50-subject example frequently used in Cohen's
	// kappa references: a=20, b=5, c=10, d=15 -> kappa = 0.4.
	got, ok, reason := CohensKappa(20, 5, 10, 15)
	if !ok {
		t.Fatalf("ok = false, reason = %q", reason)
	}
	testutil.AssertFloatNear(t, got, 0.4, 1e-9)
}

func TestCohensKappa_Degenerate_AllOneClass(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	_, ok, reason := CohensKappa(10, 0, 0, 0)
	if ok {
		t.Fatal("expected ok=false for degenerate matrix")
	}
	testutil.AssertEqual(t, reason, "degenerate — no variance to calibrate against")
}

func TestCohensKappa_Empty(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	_, ok, reason := CohensKappa(0, 0, 0, 0)
	if ok {
		t.Fatal("expected ok=false for empty matrix")
	}
	testutil.AssertEqual(t, reason, "no paired observations")
}

func TestCohensKappa_NegativeAgreement(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	got, ok, reason := CohensKappa(0, 10, 10, 0)
	if !ok {
		t.Fatalf("ok = false, reason = %q", reason)
	}
	testutil.AssertFloatNear(t, got, -1.0, 1e-9)
}
