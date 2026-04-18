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
	assertSnakeCaseJSONTags(t, reflect.TypeOf(NarrationScene{}))
	assertSnakeCaseJSONTags(t, reflect.TypeOf(FactTag{}))
	assertSnakeCaseJSONTags(t, reflect.TypeOf(NarrationMetadata{}))
}

func sampleNarrationScript() NarrationScript {
	return NarrationScript{
		SCPID: "SCP-TEST",
		Title: "SCP-TEST",
		Scenes: []NarrationScene{
			{
				SceneNum:          1,
				ActID:             ActIncident,
				Narration:         "문이 닫히는 순간, 당신은 이미 늦었습니다.",
				FactTags:          []FactTag{{Key: "containment", Content: "Three observers are required."}},
				Mood:              "tense",
				EntityVisible:     true,
				Location:          "Site-19 containment chamber",
				CharactersPresent: []string{"SCP-TEST", "Observer-1"},
				ColorPalette:      "cold gray, alarm red",
				Atmosphere:        "claustrophobic dread",
			},
		},
		Metadata: NarrationMetadata{
			Language:              LanguageKorean,
			SceneCount:            1,
			WriterModel:           "gpt-test",
			WriterProvider:        "openai",
			PromptTemplate:        "03_writing.md",
			FormatGuideTemplate:   "format_guide.md",
			ForbiddenTermsVersion: "sha256",
		},
		SourceVersion: NarrationSourceVersionV1,
	}
}
