package agents

import (
	"math"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestNormalizeShotDurations_SumsToSceneDuration(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	cases := []struct {
		total     float64
		shotCount int
	}{
		{17.3, 3},
		{8.0, 1},
		{40.0, 4},
		{7.7, 2},
		{23.1, 3},
	}
	for _, tc := range cases {
		got := NormalizeShotDurations(tc.total, tc.shotCount)
		testutil.AssertEqual(t, len(got), tc.shotCount)
		for i, d := range got {
			if d < 0 {
				t.Fatalf("case total=%v count=%d: negative duration at shot %d: %v", tc.total, tc.shotCount, i, d)
			}
		}
		if diff := math.Abs(sumDurations(got) - tc.total); diff > 0.1 {
			t.Fatalf("case total=%v count=%d: sum drift %v exceeds ±0.1s tolerance", tc.total, tc.shotCount, diff)
		}
	}
}

func TestNormalizeShotDurations_SafeOnNonFiniteAndNegativeInput(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	for _, total := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -10.0} {
		got := NormalizeShotDurations(total, 3)
		testutil.AssertEqual(t, len(got), 3)
		for i, d := range got {
			if math.IsNaN(d) || math.IsInf(d, 0) || d < 0 {
				t.Fatalf("input=%v: shot %d produced unsafe value %v", total, i, d)
			}
		}
	}
}

func TestBuildFrozenDescriptor_Stable(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	got := BuildFrozenDescriptor(domain.VisualIdentity{
		Appearance:             "Concrete sentinel",
		DistinguishingFeatures: []string{"Obsidian eyes", "Red fractures"},
		EnvironmentSetting:     "Transit vault",
		KeyVisualMoments:       []string{"Blink", "Dust trail"},
	})
	want := "Appearance: Concrete sentinel; Distinguishing features: Obsidian eyes, Red fractures; Environment: Transit vault; Key visual moments: Blink, Dust trail"
	testutil.AssertEqual(t, got, want)
}

