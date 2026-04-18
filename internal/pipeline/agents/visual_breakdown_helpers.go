package agents

import (
	"fmt"
	"math"
	"strings"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

type SceneDurationEstimator interface {
	Estimate(scene domain.NarrationScene) float64
}

type heuristicDurationEstimator struct{}

func NewHeuristicDurationEstimator() SceneDurationEstimator {
	return heuristicDurationEstimator{}
}

func (heuristicDurationEstimator) Estimate(scene domain.NarrationScene) float64 {
	words := len(strings.Fields(scene.Narration))
	if words == 0 {
		return 4.0
	}
	seconds := float64(words) * 0.8
	if seconds < 4.0 {
		return 4.0
	}
	return roundToTenth(seconds)
}

func ShotCountForDuration(seconds float64) int {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 {
		return 1
	}
	switch {
	case seconds <= 8:
		return 1
	case seconds <= 15:
		return 2
	case seconds <= 25:
		return 3
	case seconds <= 40:
		return 4
	default:
		return 5
	}
}

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

func EnsureFrozenPrefix(frozen, descriptor string) string {
	frozen = strings.TrimSpace(frozen)
	trimmed := strings.TrimSpace(descriptor)
	if frozen == "" {
		return trimmed
	}
	if trimmed == "" {
		return frozen
	}
	if strings.HasPrefix(trimmed, frozen) {
		return trimmed
	}
	return frozen + "; " + trimmed
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
