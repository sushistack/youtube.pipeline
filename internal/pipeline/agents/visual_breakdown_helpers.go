package agents

import (
	"fmt"
	"math"
	"strings"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// BeatDurationEstimator computes the per-beat audio-driven duration in
// seconds for one BeatAnchor of an act. Implementations receive the parent
// ActScript context (ActID, Monologue) plus the anchor itself so the
// estimator can read the rune-slice text without re-slicing.
type BeatDurationEstimator interface {
	Estimate(actID string, monologue string, anchor domain.BeatAnchor) float64
}

type heuristicDurationEstimator struct{}

// NewHeuristicDurationEstimator returns the v2 word-count heuristic
// estimator: ~0.8s per word with a 4.0s floor. Production wires the
// schema-aware estimator instead; this is the legacy default surfaced for
// tests and offline preview tooling.
func NewHeuristicDurationEstimator() BeatDurationEstimator {
	return heuristicDurationEstimator{}
}

func (heuristicDurationEstimator) Estimate(_ string, monologue string, anchor domain.BeatAnchor) float64 {
	text := beatRuneSlice(monologue, anchor)
	words := len(strings.Fields(text))
	if words == 0 {
		return 4.0
	}
	seconds := float64(words) * 0.8
	if seconds < 4.0 {
		return 4.0
	}
	return roundToTenth(seconds)
}

// beatRuneSlice returns the rune-slice text of a beat anchor with defensive
// clamping. Out-of-range or inverted offsets yield "" rather than panicking
// — the validator at runVisualBreakdowner entry catches malformed offsets,
// and a panic here would surface as bridge corruption rather than a clear
// validation error.
func beatRuneSlice(monologue string, anchor domain.BeatAnchor) string {
	if monologue == "" {
		return ""
	}
	runes := []rune(monologue)
	runeLen := len(runes)
	start := anchor.StartOffset
	end := anchor.EndOffset
	if start < 0 {
		start = 0
	}
	if start > runeLen {
		start = runeLen
	}
	if end > runeLen {
		end = runeLen
	}
	if end < start {
		end = start
	}
	return string(runes[start:end])
}

// NormalizeShotDurations splits a scene's total duration evenly across
// shotCount visual shots. Shot count is now driven by len(scene.NarrationBeats)
// (1:1 narration↔shot alignment) rather than by the deprecated duration-tier
// formula — see docs/prompts/scenario/03_5_visual_breakdown.md and
// internal/pipeline/agents/visual_breakdowner.go for the wiring.
func NormalizeShotDurations(totalSeconds float64, shotCount int) []float64 {
	if shotCount <= 0 {
		return nil
	}
	if math.IsNaN(totalSeconds) || math.IsInf(totalSeconds, 0) || totalSeconds < 0 {
		totalSeconds = 0
	}
	total := roundToTenth(totalSeconds)
	if shotCount == 1 {
		return []float64{total}
	}
	base := roundToTenth(total / float64(shotCount))
	durations := make([]float64, shotCount)
	var assigned float64
	for i := 0; i < shotCount-1; i++ {
		durations[i] = base
		assigned += durations[i]
	}
	durations[shotCount-1] = roundToTenth(total - assigned)
	return durations
}

func BuildFrozenDescriptor(v domain.VisualIdentity) string {
	return fmt.Sprintf(
		"Appearance: %s; Distinguishing features: %s; Environment: %s; Key visual moments: %s",
		v.Appearance,
		strings.Join(v.DistinguishingFeatures, ", "),
		v.EnvironmentSetting,
		strings.Join(v.KeyVisualMoments, ", "),
	)
}

func roundToTenth(v float64) float64 {
	return math.Round(v*10) / 10
}

func sumDurations(values []float64) float64 {
	var sum float64
	for _, value := range values {
		sum += value
	}
	return roundToTenth(sum)
}
