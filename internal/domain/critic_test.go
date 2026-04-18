package domain

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestCriticOutput_JSONRoundTrip_BothCheckpoints(t *testing.T) {
	orig := CriticOutput{
		PostWriter: &CriticCheckpointReport{
			Checkpoint:   CriticCheckpointPostWriter,
			Verdict:      CriticVerdictAcceptWithNotes,
			OverallScore: 83,
			Rubric: CriticRubricScores{
				Hook: 85, FactAccuracy: 82, EmotionalVariation: 80, Immersion: 85,
			},
			Feedback: "도입은 강하지만 장면 전환의 감정 대비를 더 키우세요.",
			SceneNotes: []CriticSceneNote{
				{SceneNum: 1, Issue: "후크가 조금 길다", Suggestion: "첫 문장을 더 짧게 줄이세요."},
			},
			Precheck: CriticPrecheck{
				SchemaValid:       true,
				ForbiddenTermHits: []string{},
				ShortCircuited:    false,
			},
			CriticModel:    "critic-model",
			CriticProvider: "anthropic",
			SourceVersion:  CriticSourceVersionV1,
		},
		PostReviewer: &CriticCheckpointReport{
			Checkpoint:   CriticCheckpointPostReviewer,
			Verdict:      CriticVerdictPass,
			OverallScore: 91,
			Rubric: CriticRubricScores{
				Hook: 91, FactAccuracy: 92, EmotionalVariation: 90, Immersion: 91,
			},
			Feedback: "최종 검토까지 안정적입니다.",
			SceneNotes: []CriticSceneNote{
				{SceneNum: 2, Issue: "리듬이 약간 느리다", Suggestion: "장면 전환을 반 박자 더 빠르게 정리하세요."},
			},
			Precheck: CriticPrecheck{
				SchemaValid:       true,
				ForbiddenTermHits: []string{},
				ShortCircuited:    false,
			},
			CriticModel:    "critic-model-2",
			CriticProvider: "openai",
			SourceVersion:  CriticSourceVersionV1,
		},
	}
	raw, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var round CriticOutput
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(round, orig) {
		t.Fatalf("round-trip mismatch:\n got: %#v\nwant: %#v", round, orig)
	}
}

func TestCriticTypes_JSONTagsSnakeCase(t *testing.T) {
	assertSnakeCaseJSONTags(t, reflect.TypeOf(CriticOutput{}))
	assertSnakeCaseJSONTags(t, reflect.TypeOf(CriticCheckpointReport{}))
	assertSnakeCaseJSONTags(t, reflect.TypeOf(CriticRubricScores{}))
	assertSnakeCaseJSONTags(t, reflect.TypeOf(CriticSceneNote{}))
	assertSnakeCaseJSONTags(t, reflect.TypeOf(CriticPrecheck{}))
}

func TestCriticRubricWeights_SumToOne(t *testing.T) {
	var sum float64
	for _, weight := range CriticRubricWeights {
		sum += weight
	}
	if diff := sum - 1.0; diff < -1e-9 || diff > 1e-9 {
		t.Fatalf("sum = %0.12f, want 1.0", sum)
	}
}

func TestCriticOutput_JSONOmitEmptyPostReviewer(t *testing.T) {
	raw, err := json.Marshal(CriticOutput{
		PostWriter: &CriticCheckpointReport{
			Checkpoint:     CriticCheckpointPostWriter,
			Verdict:        CriticVerdictPass,
			OverallScore:   90,
			Rubric:         CriticRubricScores{Hook: 90, FactAccuracy: 90, EmotionalVariation: 90, Immersion: 90},
			Feedback:       "좋습니다.",
			SceneNotes:     []CriticSceneNote{},
			Precheck:       CriticPrecheck{SchemaValid: true, ForbiddenTermHits: []string{}, ShortCircuited: false},
			CriticModel:    "critic",
			CriticProvider: "provider",
			SourceVersion:  CriticSourceVersionV1,
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "post_reviewer") {
		t.Fatalf("expected post_reviewer to be omitted, got %s", raw)
	}
}

func TestCriticCheckpointPostReviewer_Constant(t *testing.T) {
	if CriticCheckpointPostReviewer != "post_reviewer" {
		t.Fatalf("got %q, want post_reviewer", CriticCheckpointPostReviewer)
	}
}

func TestCriticCheckpointReport_JSONRoundTrip_MinorPolicyFindings(t *testing.T) {
	orig := CriticCheckpointReport{
		Checkpoint:   CriticCheckpointPostReviewer,
		Verdict:      CriticVerdictPass,
		OverallScore: 90,
		Rubric:       CriticRubricScores{Hook: 90, FactAccuracy: 90, EmotionalVariation: 90, Immersion: 90},
		Feedback:     "좋습니다.",
		SceneNotes:   []CriticSceneNote{},
		MinorPolicyFindings: []MinorPolicyFinding{
			{SceneNum: 2, Reason: "미성년자가 폭력에 노출됩니다."},
		},
		Precheck:       CriticPrecheck{SchemaValid: true},
		CriticModel:    "critic",
		CriticProvider: "provider",
		SourceVersion:  CriticSourceVersionPostReviewerV1,
	}
	raw, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round CriticCheckpointReport
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(round, orig) {
		t.Fatalf("round-trip mismatch:\n got: %#v\nwant: %#v", round, orig)
	}
}
