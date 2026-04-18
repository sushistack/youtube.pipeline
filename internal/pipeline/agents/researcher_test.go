package agents

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

type fakeCorpusReader struct {
	doc CorpusDocument
	err error
}

func (f fakeCorpusReader) Read(ctx context.Context, scpID string) (CorpusDocument, error) {
	return f.doc, f.err
}

func TestResearcher_Run_SCPTest_Happy(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{SCPID: "SCP-TEST"}
	researcher := NewResearcher(
		NewFilesystemCorpus(filepath.Join(testutil.ProjectRoot(t), "testdata", "fixtures", "corpus")),
		mustValidator(t, "researcher_output.schema.json"),
	)
	if err := researcher(context.Background(), state); err != nil {
		t.Fatalf("Researcher: %v", err)
	}
	if state.Research == nil {
		t.Fatal("expected research output")
	}
	testutil.AssertEqual(t, state.Research.Title, "SCP-TEST")
	if len(state.Research.DramaticBeats) < 3 {
		t.Fatalf("expected at least 3 beats, got %d", len(state.Research.DramaticBeats))
	}
	testutil.AssertEqual(t, state.Research.SourceVersion, domain.SourceVersionV1)
	if err := mustValidator(t, "researcher_output.schema.json").Validate(state.Research); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestResearcher_Run_Validates_SampleFixture(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{SCPID: "SCP-TEST"}
	researcher := NewResearcher(
		NewFilesystemCorpus(filepath.Join(testutil.ProjectRoot(t), "testdata", "fixtures", "corpus")),
		mustValidator(t, "researcher_output.schema.json"),
	)
	if err := researcher(context.Background(), state); err != nil {
		t.Fatalf("Researcher: %v", err)
	}
	raw, err := json.Marshal(state.Research)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	testutil.AssertJSONEq(t, string(raw), string(testutil.LoadFixture(t, filepath.Join("contracts", "researcher_output.sample.json"))))
}

func TestResearcher_Run_EmptySCPID(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{}
	err := NewResearcher(fakeCorpusReader{}, mustValidator(t, "researcher_output.schema.json"))(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if state.Research != nil {
		t.Fatalf("unexpected research: %+v", state.Research)
	}
}

func TestResearcher_Run_MissingCorpus(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{SCPID: "SCP-404"}
	err := NewResearcher(fakeCorpusReader{err: ErrCorpusNotFound}, mustValidator(t, "researcher_output.schema.json"))(context.Background(), state)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if state.Research != nil {
		t.Fatalf("unexpected research: %+v", state.Research)
	}
}

func TestResearcher_Run_SparseCorpus(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{SCPID: "SCP-TEST"}
	doc := sampleCorpusDocument()
	doc.Facts.AnomalousProperties = nil
	doc.Facts.VisualElements.KeyVisualMoments = nil
	err := NewResearcher(fakeCorpusReader{doc: doc}, mustValidator(t, "researcher_output.schema.json"))(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "sparse corpus") {
		t.Fatalf("expected sparse corpus error, got %v", err)
	}
}

func TestResearcher_Run_MainTextTruncation(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{SCPID: "SCP-TEST"}
	doc := sampleCorpusDocument()
	doc.MainText = strings.Repeat("가", 10000)
	err := NewResearcher(fakeCorpusReader{doc: doc}, mustValidator(t, "researcher_output.schema.json"))(context.Background(), state)
	if err != nil {
		t.Fatalf("Researcher: %v", err)
	}
	if utf8.RuneCountInString(state.Research.MainTextExcerpt) > 4000 {
		t.Fatalf("excerpt too long: %d runes", utf8.RuneCountInString(state.Research.MainTextExcerpt))
	}
	if err := mustValidator(t, "researcher_output.schema.json").Validate(state.Research); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestResearcher_Run_TagsFallback_FromMeta(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{SCPID: "SCP-TEST"}
	doc := sampleCorpusDocument()
	doc.Facts.Tags = nil
	doc.Meta.Tags = []string{"a", "b"}
	err := NewResearcher(fakeCorpusReader{doc: doc}, mustValidator(t, "researcher_output.schema.json"))(context.Background(), state)
	if err != nil {
		t.Fatalf("Researcher: %v", err)
	}
	if !reflect.DeepEqual(state.Research.Tags, []string{"a", "b"}) {
		t.Fatalf("got %v, want [a b]", state.Research.Tags)
	}
}

func TestResearcher_Run_TagsFallback_BothEmpty(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{SCPID: "SCP-TEST"}
	doc := sampleCorpusDocument()
	doc.Facts.Tags = nil
	doc.Meta.Tags = nil
	err := NewResearcher(fakeCorpusReader{doc: doc}, mustValidator(t, "researcher_output.schema.json"))(context.Background(), state)
	if err != nil {
		t.Fatalf("Researcher: %v", err)
	}
	if !reflect.DeepEqual(state.Research.Tags, []string{}) {
		t.Fatalf("got %#v, want empty slice", state.Research.Tags)
	}
	raw, err := json.Marshal(state.Research)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"tags":[]`) {
		t.Fatalf("expected tags array, got %s", raw)
	}
}

func TestResearcher_Run_SliceIsolation(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{SCPID: "SCP-TEST"}
	doc := sampleCorpusDocument()
	err := NewResearcher(fakeCorpusReader{doc: doc}, mustValidator(t, "researcher_output.schema.json"))(context.Background(), state)
	if err != nil {
		t.Fatalf("Researcher: %v", err)
	}
	doc.Facts.AnomalousProperties[0] = "Mutated"
	if state.Research.AnomalousProperties[0] == "Mutated" {
		t.Fatal("research output aliased input slice")
	}
}

func TestResearcher_BeatTones_Rotate(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	beats := buildDramaticBeats(sampleCorpusDocument().Facts)
	for i := 1; i < len(beats); i++ {
		if beats[i-1].EmotionalTone == beats[i].EmotionalTone {
			t.Fatalf("adjacent beats share tone %q", beats[i].EmotionalTone)
		}
	}
}

func TestResearcher_Run_CallsBlockExternalHTTP(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{SCPID: "SCP-TEST"}
	err := NewResearcher(fakeCorpusReader{doc: sampleCorpusDocument()}, mustValidator(t, "researcher_output.schema.json"))(context.Background(), state)
	if err != nil {
		t.Fatalf("Researcher: %v", err)
	}
}

func sampleCorpusDocument() CorpusDocument {
	return CorpusDocument{
		SCPID: "SCP-TEST",
		Facts: SCPFacts{
			SCPID:                 "SCP-TEST",
			Title:                 "SCP-TEST",
			ObjectClass:           "Euclid",
			PhysicalDescription:   "A concrete figure with fractured obsidian inlays.",
			AnomalousProperties:   []string{"It advances when every witness blinks.", "Its shadow lingers after it moves."},
			ContainmentProcedures: "Keep three observers in the chamber at all times.",
			BehaviorAndNature:     "It stalks the nearest isolated observer.",
			OriginAndDiscovery:    "Recovered from an abandoned Seoul transit spur.",
			VisualElements: SCPVisualElements{
				Appearance:             "A gaunt concrete sentinel with obsidian seams.",
				DistinguishingFeatures: []string{"Obsidian eye sockets", "Hairline fractures that glow red"},
				EnvironmentSetting:     "A dim transit-platform containment vault.",
				KeyVisualMoments: []string{
					"The sentinel stands motionless beneath a flickering platform light.",
					"A security monitor catches it one step closer after a blink.",
					"Red light leaks from the fractures as alarms begin to pulse.",
				},
			},
			Tags: []string{"scp", "horror", "urban", "test"},
		},
		Meta: SCPMeta{
			SCPID:       "SCP-TEST",
			Tags:        []string{"scp", "horror", "urban", "test"},
			RelatedDocs: []string{},
		},
		MainText: "SCP-TEST waited at the end of the abandoned platform, silent except for the tiny grit that slid from its shoulders. Investigators logged each blink in pairs, yet every relay review found the figure closer than before. 관측 기록은 일치하지 않았고, each witness described the same impossible sensation: the platform felt shorter every time they looked away. By the time the emergency shutters sealed, a second trail of dust already marked the path behind them.",
	}
}
