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
	assertSnakeCaseJSONTags(t, reflect.TypeOf(FactTag{}))
	assertSnakeCaseJSONTags(t, reflect.TypeOf(NarrationMetadata{}))
}

// TestFlatBeats_ResolvesPerBeatTextAndOrder verifies the v2 helper that
// downstream consumers (segment seeding, image_track, scene_service) use to
// walk the script as a flat ordered sequence with parent-act context.
func TestFlatBeats_ResolvesPerBeatTextAndOrder(t *testing.T) {
	script := sampleNarrationScript()
	flat := script.FlatBeats()
	if got, want := len(flat), 2; got != want {
		t.Fatalf("len=%d want %d", got, want)
	}
	if flat[0].Index != 1 || flat[1].Index != 2 {
		t.Fatalf("index order: %d, %d", flat[0].Index, flat[1].Index)
	}
	if flat[0].ActID != ActIncident {
		t.Fatalf("act_id: %s", flat[0].ActID)
	}
	if flat[0].Text != "문이 닫히는 순간." {
		t.Fatalf("first text: %q", flat[0].Text)
	}
	if flat[1].Text != "당신은 이미 늦었습니다." {
		t.Fatalf("second text: %q", flat[1].Text)
	}
	if flat[0].ActMood != "tense" {
		t.Fatalf("act_mood fallthrough: %q", flat[0].ActMood)
	}
}

// TestFlatBeats_ClampsOutOfRangeOffsets verifies defensive slicing — a beat
// whose EndOffset exceeds the monologue rune length yields empty/clamped
// Text rather than a panic. Out-of-range offsets are a programmer/upstream
// bug, not a runtime fatal.
func TestFlatBeats_ClampsOutOfRangeOffsets(t *testing.T) {
	script := NarrationScript{
		Acts: []ActScript{{
			ActID:     ActIncident,
			Monologue: "짧은 문장.",
			Beats: []BeatAnchor{
				{StartOffset: -1, EndOffset: 999},
				{StartOffset: 100, EndOffset: 50},
			},
		}},
	}
	flat := script.FlatBeats()
	if len(flat) != 2 {
		t.Fatalf("len=%d want 2", len(flat))
	}
	if flat[0].Text != "짧은 문장." {
		t.Fatalf("clamped slice: %q", flat[0].Text)
	}
	if flat[1].Text != "" {
		t.Fatalf("inverted/oob slice: %q", flat[1].Text)
	}
}

// TestBeatIndexAt_TranslatesActOffsetToFlatIndex covers the act_id +
// rune_offset → flat scene_index translation that critic-finding consumers
// rely on (review_gate, segment store).
func TestBeatIndexAt_TranslatesActOffsetToFlatIndex(t *testing.T) {
	script := sampleNarrationScript()
	if got := script.BeatIndexAt(ActIncident, 0); got != 1 {
		t.Errorf("offset 0 → index %d, want 1", got)
	}
	if got := script.BeatIndexAt(ActIncident, 5); got != 1 {
		t.Errorf("offset 5 (mid first beat) → index %d, want 1", got)
	}
	if got := script.BeatIndexAt(ActIncident, 10); got != 2 {
		t.Errorf("offset 10 (start of second beat) → index %d, want 2", got)
	}
	if got := script.BeatIndexAt(ActIncident, 22); got != 2 {
		t.Errorf("offset 22 (last rune of second beat) → index %d, want 2", got)
	}
	if got := script.BeatIndexAt(ActIncident, 999); got != 0 {
		t.Errorf("offset 999 (oob) → index %d, want 0", got)
	}
	if got := script.BeatIndexAt("nonexistent_act", 0); got != 0 {
		t.Errorf("missing act → index %d, want 0", got)
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
