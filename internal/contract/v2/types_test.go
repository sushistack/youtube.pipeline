package contractv2_test

import (
	"encoding/json"
	"reflect"
	"testing"

	contractv2 "github.com/sushistack/youtube.pipeline/internal/contract/v2"
)

func TestVersionConstant(t *testing.T) {
	t.Parallel()
	if contractv2.Version != "v2" {
		t.Fatalf("Version = %q, want v2", contractv2.Version)
	}
}

func TestRoundtripScriptOutput(t *testing.T) {
	t.Parallel()
	original := contractv2.ScriptOutput{
		TitleCandidates: []string{"제목 A", "제목 B"},
		Scenes: []contractv2.Scene{
			{SceneID: 1, Section: "incident", DurationSeconds: 12, NarrationKO: "검은 액체가 흘러내렸죠.", VisualDirection: "dim corridor", EmotionCurve: "tense", SFXHint: "low rumble"},
			{SceneID: 2, Section: "mystery", DurationSeconds: 24, NarrationKO: "재단 요원들이 모였습니다.", VisualDirection: "containment cell", EmotionCurve: "calm", SFXHint: "footsteps"},
		},
		OutroHookKO: "그것은 아직 그곳에 있다.",
		SourceAttribution: contractv2.Attribution{
			SCPNumber:  "SCP-173",
			Author:     "Moto42",
			WikiURL:    "http://www.scpwiki.com/scp-173",
			License:    "CC BY-SA 3.0",
			RenderedKO: "본 영상은 …",
		},
	}
	bytes, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded contractv2.ScriptOutput
	if err := json.Unmarshal(bytes, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, decoded) {
		t.Fatalf("roundtrip mismatch:\nwant=%#v\ngot=%#v", original, decoded)
	}
}

func TestRoundtripCriticReport(t *testing.T) {
	t.Parallel()
	report := contractv2.CriticReport{
		OverallScore: 82,
		Passed:       true,
		RubricScores: map[string]int{
			"hook_under_15s":   9,
			"information_drip": 8,
		},
		Failures: []contractv2.Failure{
			{
				Criterion:      "sentence_rhythm",
				Score:          6,
				FailureQuote:   "긴 문장이 너무 많아 긴장감이 무너졌습니다.",
				Recommendation: "tense 구간 평균을 18자 이하로 줄이세요.",
			},
		},
		RevisionPriority: "tense pacing",
	}
	bytes, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var decoded contractv2.CriticReport
	if err := json.Unmarshal(bytes, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(report, decoded) {
		t.Fatalf("roundtrip mismatch:\nwant=%#v\ngot=%#v", report, decoded)
	}
}

func TestCriterionKeysCompleteness(t *testing.T) {
	t.Parallel()
	if got := len(contractv2.CriterionKeys); got != 10 {
		t.Fatalf("CriterionKeys length=%d, want 10", got)
	}
	seen := map[string]bool{}
	for _, k := range contractv2.CriterionKeys {
		if seen[k] {
			t.Fatalf("duplicate criterion key: %q", k)
		}
		seen[k] = true
	}
}
