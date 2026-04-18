package domain

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestVisualBreakdownOutput_JSONRoundTrip(t *testing.T) {
	orig := sampleVisualBreakdownOutput()
	raw, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var round VisualBreakdownOutput
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(round, orig) {
		t.Fatalf("round-trip mismatch:\n got: %#v\nwant: %#v", round, orig)
	}
	if !strings.Contains(string(raw), `"shot_overrides":{`) {
		t.Fatalf("expected shot_overrides object key in %s", raw)
	}
}

func TestVisualBreakdownOutput_JSONTagsSnakeCase(t *testing.T) {
	assertSnakeCaseJSONTags(t, reflect.TypeOf(VisualBreakdownOutput{}))
	assertSnakeCaseJSONTags(t, reflect.TypeOf(VisualBreakdownScene{}))
	assertSnakeCaseJSONTags(t, reflect.TypeOf(VisualShot{}))
	assertSnakeCaseJSONTags(t, reflect.TypeOf(ShotOverride{}))
	assertSnakeCaseJSONTags(t, reflect.TypeOf(VisualBreakdownMetadata{}))
}

func TestVisualBreakdownOutput_TransitionConstantsAllowed(t *testing.T) {
	allowed := map[string]bool{
		TransitionKenBurns:      true,
		TransitionCrossDissolve: true,
		TransitionHardCut:       true,
	}
	expected := map[string]string{
		"ken_burns":      TransitionKenBurns,
		"cross_dissolve": TransitionCrossDissolve,
		"hard_cut":       TransitionHardCut,
	}
	for literal, constant := range expected {
		if constant != literal {
			t.Errorf("constant %q drifted from wire literal %q", constant, literal)
		}
		if !allowed[literal] {
			t.Errorf("wire literal %q missing from allowed set", literal)
		}
	}
	if len(allowed) != 3 {
		t.Fatalf("expected exactly 3 transition constants, got %d", len(allowed))
	}
}

func sampleVisualBreakdownOutput() VisualBreakdownOutput {
	return VisualBreakdownOutput{
		SCPID:            "SCP-TEST",
		Title:            "SCP-TEST",
		FrozenDescriptor: "Appearance: Concrete sentinel; Distinguishing features: cracks; Environment: chamber; Key visual moments: blink",
		Scenes: []VisualBreakdownScene{
			{
				SceneNum:              1,
				ActID:                 ActIncident,
				Narration:             "문이 닫히는 순간, 당신은 이미 늦었습니다.",
				EstimatedTTSDurationS: 7.2,
				ShotCount:             1,
				Shots: []VisualShot{{
					ShotIndex:          1,
					VisualDescriptor:   "Appearance: Concrete sentinel; Distinguishing features: cracks; Environment: chamber; Key visual moments: blink; close shot of the sentinel beside the closing door",
					EstimatedDurationS: 7.2,
					Transition:         TransitionKenBurns,
				}},
			},
		},
		ShotOverrides: map[int]ShotOverride{},
		Metadata: VisualBreakdownMetadata{
			VisualBreakdownModel:    "gpt-test",
			VisualBreakdownProvider: "openai",
			PromptTemplate:          "03_5_visual_breakdown.md",
			ShotFormulaVersion:      ShotFormulaVersionV1,
		},
		SourceVersion: VisualBreakdownSourceVersionV1,
	}
}
