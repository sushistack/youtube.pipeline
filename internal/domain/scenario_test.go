package domain_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestResearcherOutput_JSONRoundTrip(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	orig := domain.ResearcherOutput{
		SCPID:                 "SCP-TEST",
		Title:                 "SCP-TEST",
		ObjectClass:           "Euclid",
		PhysicalDescription:   "A test statue.",
		AnomalousProperties:   []string{"Moves when unwatched"},
		ContainmentProcedures: "Watch it closely.",
		BehaviorAndNature:     "Predatory.",
		OriginAndDiscovery:    "Recovered from a shuttered site.",
		VisualIdentity: domain.VisualIdentity{
			Appearance:             "Concrete idol",
			DistinguishingFeatures: []string{"Cracked face"},
			EnvironmentSetting:     "Cold chamber",
			KeyVisualMoments:       []string{"A blink", "A scrape"},
		},
		DramaticBeats: []domain.DramaticBeat{
			{Index: 0, Source: "visual_moment", Description: "A blink", EmotionalTone: "mystery"},
		},
		MainTextExcerpt: "Trimmed excerpt",
		Tags:            []string{"scp", "test"},
		SourceVersion:   domain.SourceVersionV1,
	}

	raw, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{
		`"scp_id"`, `"object_class"`, `"physical_description"`, `"visual_identity"`,
		`"dramatic_beats"`, `"main_text_excerpt"`, `"source_version"`,
	} {
		if !strings.Contains(string(raw), key) {
			t.Fatalf("missing key %s in %s", key, raw)
		}
	}

	var round domain.ResearcherOutput
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(round, orig) {
		t.Fatalf("round trip mismatch:\ngot:  %#v\nwant: %#v", round, orig)
	}
}

func TestStructurerOutput_JSONRoundTrip(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	orig := domain.StructurerOutput{
		SCPID: "SCP-TEST",
		Acts: []domain.Act{
			{
				ID:              domain.ActIncident,
				Name:            "Act 1 — Incident",
				Synopsis:        "Act 1 opens with a scrape. (1 beats; 15% of runtime.)",
				SceneBudget:     2,
				DurationRatio:   0.15,
				DramaticBeatIDs: []int{0},
				KeyPoints:       []string{"A scrape"},
			},
		},
		TargetSceneCount: 10,
		SourceVersion:    domain.SourceVersionV1,
	}

	raw, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{`"target_scene_count"`, `"dramatic_beat_ids"`, `"duration_ratio"`} {
		if !strings.Contains(string(raw), key) {
			t.Fatalf("missing key %s in %s", key, raw)
		}
	}

	var round domain.StructurerOutput
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(round, orig) {
		t.Fatalf("round trip mismatch:\ngot:  %#v\nwant: %#v", round, orig)
	}
}

func TestStructurerOutput_ActOrderConstant(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	if len(domain.ActOrder) != 4 {
		t.Fatalf("got %d acts, want 4", len(domain.ActOrder))
	}
	want := [4]string{domain.ActIncident, domain.ActMystery, domain.ActRevelation, domain.ActUnresolved}
	if domain.ActOrder != want {
		t.Fatalf("got %v, want %v", domain.ActOrder, want)
	}

	var sum float64
	for _, id := range domain.ActOrder {
		sum += domain.ActDurationRatio[id]
	}
	testutil.AssertFloatNear(t, sum, 1.0, 1e-9)
}

func TestResearcherOutput_NoOmitemptyOnRequired(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	typ := reflect.TypeOf(domain.ResearcherOutput{})
	for i := range typ.NumField() {
		tag := typ.Field(i).Tag.Get("json")
		if strings.Contains(tag, ",omitempty") {
			t.Fatalf("field %s unexpectedly has omitempty tag %q", typ.Field(i).Name, tag)
		}
	}
}
