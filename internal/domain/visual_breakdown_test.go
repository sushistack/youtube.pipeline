package domain

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestVisualScript_JSONRoundTrip(t *testing.T) {
	orig := sampleVisualScript()
	raw, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var round VisualScript
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(round, orig) {
		t.Fatalf("round-trip mismatch:\n got: %#v\nwant: %#v", round, orig)
	}
	if !strings.Contains(string(raw), `"shot_overrides":{`) {
		t.Fatalf("expected shot_overrides object key in %s", raw)
	}
	if !strings.Contains(string(raw), `"acts":[`) {
		t.Fatalf("expected acts array key in %s", raw)
	}
	if !strings.Contains(string(raw), `"narration_anchor":{`) {
		t.Fatalf("expected narration_anchor object key in %s", raw)
	}
}

func TestVisualScript_JSONTagsSnakeCase(t *testing.T) {
	assertSnakeCaseJSONTags(t, reflect.TypeOf(VisualScript{}))
	assertSnakeCaseJSONTags(t, reflect.TypeOf(VisualAct{}))
	assertSnakeCaseJSONTags(t, reflect.TypeOf(VisualShot{}))
	assertSnakeCaseJSONTags(t, reflect.TypeOf(VisualBreakdownScene{}))
	assertSnakeCaseJSONTags(t, reflect.TypeOf(LegacyShotV1{}))
	assertSnakeCaseJSONTags(t, reflect.TypeOf(ShotOverride{}))
	assertSnakeCaseJSONTags(t, reflect.TypeOf(VisualBreakdownMetadata{}))
}

func TestVisualScript_TransitionConstantsAllowed(t *testing.T) {
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

func TestVisualScript_LegacyScenesBridgeRuneSlicing(t *testing.T) {
	// Korean monologue with a multi-byte rune slice anchor — bridge MUST
	// rune-slice (not byte-slice) the act monologue when reconstructing
	// VisualBreakdownScene.Narration.
	monologue := "문이 닫힙니다. 석상이 한 발짝 다가왔습니다."
	narration := &NarrationScript{
		Acts: []ActScript{{
			ActID:     ActIncident,
			Monologue: monologue,
		}},
	}
	visual := &VisualScript{
		Acts: []VisualAct{{
			ActID: ActIncident,
			Shots: []VisualShot{{
				ShotIndex:        1,
				VisualDescriptor: "wide shot",
				Transition:       TransitionKenBurns,
				NarrationAnchor: BeatAnchor{
					StartOffset: 0,
					EndOffset:   8, // first 8 runes — "문이 닫힙니다."
				},
			}},
		}},
	}
	scenes := visual.LegacyScenes(narration)
	if len(scenes) != 1 {
		t.Fatalf("expected 1 bridged scene, got %d", len(scenes))
	}
	want := "문이 닫힙니다."
	if scenes[0].Narration != want {
		t.Fatalf("rune slicing wrong: got %q, want %q", scenes[0].Narration, want)
	}
	if scenes[0].SceneNum != 1 {
		t.Fatalf("SceneNum got %d, want 1", scenes[0].SceneNum)
	}
}

func sampleVisualScript() VisualScript {
	return VisualScript{
		SCPID:            "SCP-TEST",
		Title:            "SCP-TEST",
		FrozenDescriptor: "Appearance: Concrete sentinel; Distinguishing features: cracks; Environment: chamber; Key visual moments: blink",
		Acts: []VisualAct{{
			ActID: ActIncident,
			Shots: []VisualShot{{
				ShotIndex:          1,
				VisualDescriptor:   "close shot of the sentinel beside the closing door",
				EstimatedDurationS: 7.2,
				Transition:         TransitionKenBurns,
				NarrationAnchor: BeatAnchor{
					StartOffset:       0,
					EndOffset:         12,
					Mood:              "tense",
					Location:          "platform",
					CharactersPresent: []string{"SCP-TEST"},
					EntityVisible:     true,
					ColorPalette:      "alarm red, cold gray",
					Atmosphere:        "low hum",
					FactTags:          []FactTag{},
				},
			}},
		}},
		ShotOverrides: map[int]ShotOverride{},
		Metadata: VisualBreakdownMetadata{
			VisualBreakdownModel:    "gpt-test",
			VisualBreakdownProvider: "openai",
			PromptTemplate:          "03_5_visual_breakdown.md",
			ShotFormulaVersion:      ShotFormulaVersionV1,
		},
		SourceVersion: VisualBreakdownSourceVersionV2,
	}
}
