package pipeline

import (
	"fmt"
	"strings"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// AntiProgressDetector tracks consecutive retry outputs and fires when two
// successive outputs have cosine similarity above the configured threshold.
// Not goroutine-safe: one detector instance per stage-retry-loop per run.
// Epic 3's Critic loop will own the lifetime.
//
// Comparison semantic: current vs immediately previous (not vs first) —
// the baseline rotates after every non-empty Check call. The threshold
// comparison is strict > (FR8 "exceeds"); at threshold 1.0 identical
// inputs do NOT trip because no value can exceed 1.0.
type AntiProgressDetector struct {
	threshold   float64
	previous    string
	hasPrevious bool
	lastSim     float64
}

// NewAntiProgressDetector constructs a detector. threshold must be in
// (0.0, 1.0]; otherwise returns a wrapped ErrValidation. The detector
// starts with no prior output; the first Check call records the baseline
// and always returns (false, 0.0) because there is nothing to compare
// against.
func NewAntiProgressDetector(threshold float64) (*AntiProgressDetector, error) {
	if threshold <= 0 || threshold > 1.0 {
		return nil, fmt.Errorf("anti-progress detector: threshold %.4f out of range (0, 1]: %w", threshold, domain.ErrValidation)
	}
	return &AntiProgressDetector{threshold: threshold}, nil
}

// Check compares output against the previous output and decides whether
// the retry loop should stop early:
//
//   - First call: stores output as the baseline, returns (false, 0.0).
//   - Subsequent call: computes CosineSimilarity(prev, output); if
//     similarity > threshold returns (true, similarity); otherwise returns
//     (false, similarity). The latest output replaces the baseline
//     regardless of the decision.
//   - Empty or whitespace-only output: returns (false, 0.0) and does NOT
//     rotate the baseline (protects against false positives where the model
//     returns nothing usable; a whitespace-only string tokenizes to zero
//     tokens and would otherwise replace the baseline with dead state that
//     makes every subsequent comparison return 0).
//
// stop==true is the caller's signal to break the retry loop and surface
// domain.ErrAntiProgress.
func (d *AntiProgressDetector) Check(output string) (stop bool, similarity float64) {
	if strings.TrimSpace(output) == "" {
		return false, 0.0
	}
	if !d.hasPrevious {
		d.previous = output
		d.hasPrevious = true
		d.lastSim = 0.0
		return false, 0.0
	}
	sim := CosineSimilarity(d.previous, output)
	d.previous = output
	d.lastSim = sim
	return sim > d.threshold, sim
}

// LastSimilarity returns the similarity from the most recent Check call,
// or 0 if never called. Callers pair this with config.AntiProgressThreshold
// when recording an anti-progress event.
func (d *AntiProgressDetector) LastSimilarity() float64 {
	return d.lastSim
}

// Reset clears the baseline so the detector can be reused for a fresh
// retry loop (e.g., when the caller discards the current loop for an
// unrelated reason).
func (d *AntiProgressDetector) Reset() {
	d.previous = ""
	d.hasPrevious = false
	d.lastSim = 0.0
}
