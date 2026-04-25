package pipeline

import (
	"errors"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// White-box unit coverage for the helpers that codify the Story 11-5
// hardening contracts (R-09 short-shot xfade, R-10 zero-duration probe).

func TestComputeCrossDissolveOffset_AboveDissolveWindow(t *testing.T) {
	got := computeCrossDissolveOffset(2.0, 0.5)
	if got != 1.5 {
		t.Errorf("offset = %.3f, want 1.500 (= 2.0 − 0.5)", got)
	}
}

func TestComputeCrossDissolveOffset_AtDissolveWindow(t *testing.T) {
	got := computeCrossDissolveOffset(0.5, 0.5)
	if got != 0 {
		t.Errorf("offset = %.3f, want 0.000 at the boundary", got)
	}
}

// TestComputeCrossDissolveOffset_BelowDissolveWindow codifies the R-09 short-
// shot invariant: the offset MUST be clamped to >= 0 so the generated xfade
// filter never relies on FFmpeg undefined behavior.
func TestComputeCrossDissolveOffset_BelowDissolveWindow(t *testing.T) {
	cases := []float64{0.0, 0.1, 0.3, 0.499}
	for _, streamDur := range cases {
		got := computeCrossDissolveOffset(streamDur, 0.5)
		if got < 0 {
			t.Errorf("streamDur=%.3f: offset = %.3f, want >= 0 (R-09 invariant)", streamDur, got)
		}
		if got != 0 {
			t.Errorf("streamDur=%.3f: offset = %.3f, want 0.000 (clamped)", streamDur, got)
		}
	}
}

func TestValidateProbedDuration_Positive(t *testing.T) {
	if err := validateProbedDuration("clip.mp4", 0.001); err != nil {
		t.Errorf("expected nil for positive duration, got %v", err)
	}
	if err := validateProbedDuration("clip.mp4", 1.0); err != nil {
		t.Errorf("expected nil for 1.0s duration, got %v", err)
	}
}

func TestValidateProbedDuration_Zero(t *testing.T) {
	err := validateProbedDuration("clip.mp4", 0.0)
	if err == nil {
		t.Fatal("expected error for zero duration, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected error chain to contain domain.ErrValidation, got %v", err)
	}
}

func TestValidateProbedDuration_Negative(t *testing.T) {
	err := validateProbedDuration("clip.mp4", -1.5)
	if err == nil {
		t.Fatal("expected error for negative duration, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected error chain to contain domain.ErrValidation, got %v", err)
	}
}
