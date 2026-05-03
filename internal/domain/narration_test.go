package domain

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestNarrationScript_JSONRoundTrip(t *testing.T) {
	orig := sampleNarrationScript()
	raw, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var round NarrationScript
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(round, orig) {
		t.Fatalf("round-trip mismatch:\n got: %#v\nwant: %#v", round, orig)
	}
}

func TestNarrationScript_JSONTagsSnakeCase(t *testing.T) {
	assertSnakeCaseJSONTags(t, reflect.TypeOf(NarrationScript{}))
	assertSnakeCaseJSONTags(t, reflect.TypeOf(ActScript{}))
	assertSnakeCaseJSONTags(t, reflect.TypeOf(BeatAnchor{}))
	assertSnakeCaseJSONTags(t, reflect.TypeOf(NarrationScene{}))
	assertSnakeCaseJSONTags(t, reflect.TypeOf(FactTag{}))
	assertSnakeCaseJSONTags(t, reflect.TypeOf(NarrationMetadata{}))
}

// TestLegacyScenes_ProducesV1Shape verifies the bridge that downstream v1
// agents (visual_breakdowner, polisher v1 stub, scene_service) consume
// during the D1–D6 incremental migration.
func TestLegacyScenes_ProducesV1Shape(t *testing.T) {
	script := sampleNarrationScript()
	legacy := script.LegacyScenes()
	if got, want := len(legacy), 2; got != want {
		t.Fatalf("len=%d want %d", got, want)
	}
	if legacy[0].SceneNum != 1 || legacy[1].SceneNum != 2 {
		t.Fatalf("scene_num order: %d, %d", legacy[0].SceneNum, legacy[1].SceneNum)
	}
	if legacy[0].ActID != ActIncident {
		t.Fatalf("act_id: %s", legacy[0].ActID)
	}
	if legacy[0].Narration != "문이 닫히는 순간." {
		t.Fatalf("first narration: %q", legacy[0].Narration)
	}
	if legacy[1].Narration != "당신은 이미 늦었습니다." {
		t.Fatalf("second narration: %q", legacy[1].Narration)
	}
}

func sampleNarrationScript() NarrationScript {
	monologue := "문이 닫히는 순간." + "당신은 이미 늦었습니다."
	// rune offsets: "문이 닫히는 순간." = 10 runes; "당신은 이미 늦었습니다." = 12 runes
	return NarrationScript{
		SCPID: "SCP-TEST",
		Title: "SCP-TEST",
		Acts: []ActScript{
			{
				ActID:     ActIncident,
				Monologue: monologue,
				Mood:      "tense",
				KeyPoints: []string{"door closes", "observer late"},
				Beats: []BeatAnchor{
					{
						StartOffset:       0,
						EndOffset:         10,
						Mood:              "tense",
						Location:          "Site-19 containment chamber",
						CharactersPresent: []string{"SCP-TEST", "Observer-1"},
						EntityVisible:     true,
						ColorPalette:      "cold gray, alarm red",
						Atmosphere:        "claustrophobic dread",
						FactTags:          []FactTag{{Key: "containment", Content: "Three observers are required."}},
					},
					{
						StartOffset:       10,
						EndOffset:         23,
						Mood:              "tense",
						Location:          "Site-19 containment chamber",
						CharactersPresent: []string{"SCP-TEST", "Observer-1"},
						EntityVisible:     true,
						ColorPalette:      "cold gray, alarm red",
						Atmosphere:        "claustrophobic dread",
						FactTags:          []FactTag{},
					},
				},
			},
		},
		Metadata: NarrationMetadata{
			Language:              LanguageKorean,
			SceneCount:            2,
			WriterModel:           "qwen-max",
			WriterProvider:        "dashscope",
			PromptTemplate:        "03_writing.md",
			FormatGuideTemplate:   "format_guide.md",
			ForbiddenTermsVersion: "sha256",
		},
		SourceVersion: NarrationSourceVersionV2,
	}
}
