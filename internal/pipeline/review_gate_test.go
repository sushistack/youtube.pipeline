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

func TestMergeMinorSignals_UnionByScene(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	got := pipeline.MergeMinorSignals(
		[]agents.MinorRegexHit{
			{SceneNum: 2, Pattern: "미성년자.{0,12}폭행"},
			{SceneNum: 2, Pattern: "미성년자.{0,12}폭행"},
		},
		[]domain.MinorPolicyFinding{
			{SceneNum: 2, Reason: "미성년자가 폭력에 노출됩니다."},
			{SceneNum: 3, Reason: "미성년자가 성적 맥락에 놓입니다."},
		},
	)
	testutil.AssertEqual(t, len(got[1]), 2)
	testutil.AssertEqual(t, len(got[2]), 1)
}
