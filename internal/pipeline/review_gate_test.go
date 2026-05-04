package pipeline_test

import (
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/pipeline/agents"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestDecideSceneGate_MinorsOverrideHighScore(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	score := 0.99
	got, err := pipeline.DecideSceneGate(pipeline.SceneGateInput{
		SceneIndex:      0,
		CriticScore:     &score,
		RegexTriggered:  true,
		CriticTriggered: false,
	}, 0.85)
	if err != nil {
		t.Fatalf("DecideSceneGate: %v", err)
	}
	testutil.AssertEqual(t, got.ReviewStatus, domain.ReviewStatusWaitingForReview)
	testutil.AssertEqual(t, got.AutoApproved, false)
	testutil.AssertEqual(t, got.SafeguardFlags[0], domain.SafeguardFlagMinors)
}

func TestDecideSceneGate_AutoApprovesOnlyWhenStrictlyAboveThreshold(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	scoreEqual := 0.85
	got, err := pipeline.DecideSceneGate(pipeline.SceneGateInput{CriticScore: &scoreEqual}, 0.85)
	if err != nil {
		t.Fatalf("DecideSceneGate equal: %v", err)
	}
	testutil.AssertEqual(t, got.ReviewStatus, domain.ReviewStatusWaitingForReview)
	scoreAbove := 0.86
	got, err = pipeline.DecideSceneGate(pipeline.SceneGateInput{CriticScore: &scoreAbove}, 0.85)
	if err != nil {
		t.Fatalf("DecideSceneGate above: %v", err)
	}
	testutil.AssertEqual(t, got.ReviewStatus, domain.ReviewStatusAutoApproved)
	testutil.AssertEqual(t, got.AutoApproved, true)
}

func TestDecideSceneGate_MissingScoreFallsBackToWaitingForReview(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	got, err := pipeline.DecideSceneGate(pipeline.SceneGateInput{}, 0.85)
	if err != nil {
		t.Fatalf("DecideSceneGate: %v", err)
	}
	testutil.AssertEqual(t, got.ReviewStatus, domain.ReviewStatusWaitingForReview)
}

// TestMergeMinorSignals_UnionByScene covers the v2 (act_id, rune_offset) →
// flat segments.scene_index translation. A two-act script with two beats
// each yields scene_index 0..3; hits anchored to ActMystery (the 2nd act)
// inside its first beat must resolve to scene_index 2.
func TestMergeMinorSignals_UnionByScene(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	script := &domain.NarrationScript{
		Acts: []domain.ActScript{
			{
				ActID:     domain.ActIncident,
				Monologue: "ABCDEFGHIJ", // 10 runes
				Beats: []domain.BeatAnchor{
					{StartOffset: 0, EndOffset: 5},
					{StartOffset: 5, EndOffset: 10},
				},
			},
			{
				ActID:     domain.ActMystery,
				Monologue: "KLMNOPQRST", // 10 runes
				Beats: []domain.BeatAnchor{
					{StartOffset: 0, EndOffset: 5}, // flat scene_index = 2
					{StartOffset: 5, EndOffset: 10}, // flat scene_index = 3
				},
			},
		},
	}
	got := pipeline.MergeMinorSignals(
		script,
		[]agents.MinorRegexHit{
			{ActID: domain.ActMystery, RuneOffset: 2, Pattern: "미성년자.{0,12}폭행"},
			// Duplicate hit deduped by (scene_index, value).
			{ActID: domain.ActMystery, RuneOffset: 3, Pattern: "미성년자.{0,12}폭행"},
		},
		[]domain.MinorPolicyFinding{
			{ActID: domain.ActMystery, RuneOffset: 1, Reason: "미성년자가 폭력에 노출됩니다."},
			{ActID: domain.ActMystery, RuneOffset: 6, Reason: "미성년자가 성적 맥락에 놓입니다."},
		},
	)
	// scene_index 2 (mystery beat[0]): regex pattern + first finding reason.
	testutil.AssertEqual(t, len(got[2]), 2)
	// scene_index 3 (mystery beat[1]): second finding only.
	testutil.AssertEqual(t, len(got[3]), 1)
}

// TestMergeMinorSignals_NilScriptIsSafe ensures the helper does not panic
// when the script is nil — callers wired before the script is loaded
// (test scaffolds, edge-case error paths) get an empty map back.
func TestMergeMinorSignals_NilScriptIsSafe(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	got := pipeline.MergeMinorSignals(
		nil,
		[]agents.MinorRegexHit{{ActID: domain.ActMystery, RuneOffset: 2, Pattern: "x"}},
		[]domain.MinorPolicyFinding{{ActID: domain.ActMystery, RuneOffset: 1, Reason: "y"}},
	)
	if len(got) != 0 {
		t.Fatalf("nil script should yield empty map, got %+v", got)
	}
}
