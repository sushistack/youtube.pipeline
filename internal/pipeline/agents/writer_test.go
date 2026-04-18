package agents

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

type fakeTextGenerator struct {
	resp  domain.TextResponse
	err   error
	calls int
	last  domain.TextRequest
}

func (f *fakeTextGenerator) Generate(_ context.Context, req domain.TextRequest) (domain.TextResponse, error) {
	f.calls++
	f.last = req
	return f.resp, f.err
}

func TestWriter_Run_Happy(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &fakeTextGenerator{resp: domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{
		Content:  string(testutil.LoadFixture(t, filepath.Join("contracts", "writer_output.sample.json"))),
		Model:    "writer-model",
		Provider: "openai",
	}}}
	state := sampleWriterState()
	err := newWriterForTest(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if err != nil {
		t.Fatalf("Writer: %v", err)
	}
	if state.Narration == nil {
		t.Fatal("expected narration output")
	}
	testutil.AssertEqual(t, gen.calls, 1)
}

func TestWriter_Run_StripsCodeFence(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	fixture := strings.TrimSpace(string(testutil.LoadFixture(t, filepath.Join("contracts", "writer_output.sample.json"))))
	gen := &fakeTextGenerator{resp: domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{
		Content: "```json\n" + fixture + "\n```",
	}}}
	state := sampleWriterState()
	err := newWriterForTest(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if err != nil {
		t.Fatalf("Writer: %v", err)
	}
}

func TestWriter_Run_InvalidJSON(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &fakeTextGenerator{resp: domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{Content: "not-json"}}}
	state := sampleWriterState()
	err := newWriterForTest(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestWriter_Run_NilStructure(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &fakeTextGenerator{}
	state := sampleWriterState()
	state.Structure = nil
	err := newWriterForTest(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestWriter_Run_SchemaViolation(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &fakeTextGenerator{resp: domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{
		Content: `{"scp_id":"SCP-TEST","title":"bad","scenes":[],"metadata":{},"source_version":"v1-llm-writer"}`,
	}}}
	state := sampleWriterState()
	err := newWriterForTest(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestWriter_Run_ForbiddenTermsRejected(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	var script map[string]any
	if err := json.Unmarshal(testutil.LoadFixture(t, filepath.Join("contracts", "writer_output.sample.json")), &script); err != nil {
		t.Fatalf("unmarshal sample: %v", err)
	}
	scenes := script["scenes"].([]any)
	scene1 := scenes[0].(map[string]any)
	scene1["narration"] = "이건 wiki 문체입니다."
	raw, err := json.Marshal(script)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	gen := &fakeTextGenerator{resp: domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{Content: string(raw)}}}
	state := sampleWriterState()
	err = newWriterForTest(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if state.Narration != nil {
		t.Fatalf("state mutated on forbidden terms: %+v", state.Narration)
	}
}

func TestWriter_Run_MetadataFilled(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &fakeTextGenerator{resp: domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{
		Content:  string(testutil.LoadFixture(t, filepath.Join("contracts", "writer_output.sample.json"))),
		Model:    "writer-model",
		Provider: "openai",
	}}}
	state := sampleWriterState()
	err := newWriterForTest(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if err != nil {
		t.Fatalf("Writer: %v", err)
	}
	testutil.AssertEqual(t, state.Narration.Metadata.WriterModel, "writer-model")
	testutil.AssertEqual(t, state.Narration.Metadata.WriterProvider, "openai")
	testutil.AssertEqual(t, state.Narration.Metadata.SceneCount, len(state.Narration.Scenes))
}

func TestWriter_Run_DoesNotMutateStateOnFailure(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &fakeTextGenerator{resp: domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{Content: "not-json"}}}
	state := sampleWriterState()
	sentinel := &domain.NarrationScript{SCPID: "SENTINEL"}
	state.Narration = sentinel
	err := newWriterForTest(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if err == nil {
		t.Fatal("expected error")
	}
	if state.Narration != sentinel {
		t.Fatal("writer mutated state on failure")
	}
	if state.Narration.SCPID != "SENTINEL" {
		t.Fatalf("sentinel fields were overwritten in place: %#v", state.Narration)
	}
}

func sampleWriterState() *PipelineState {
	return &PipelineState{
		RunID:     "run-1",
		SCPID:     "SCP-TEST",
		Research:  sampleResearchForStructurer(),
		Structure: sampleStructurerOutput(),
	}
}

func sampleWriterConfig() TextAgentConfig {
	return TextAgentConfig{Model: "gpt-test-writer", Provider: "openai", MaxTokens: 4096, Temperature: 0.7}
}

func sampleWriterAssets() PromptAssets {
	return PromptAssets{
		WriterTemplate:          "Write {scp_id}\n{scene_structure}\n{scp_visual_reference}\n{format_guide}\n{forbidden_terms_section}\n{glossary_section}\n{quality_feedback}",
		CriticTemplate:          "unused",
		VisualBreakdownTemplate: "unused",
		ReviewerTemplate:        "unused",
		FormatGuide:             "guide",
	}
}

func mustTerms(t *testing.T) *ForbiddenTerms {
	t.Helper()
	root := policyRoot(t, "# comment\nwiki\n")
	terms, err := LoadForbiddenTerms(root)
	if err != nil {
		t.Fatalf("LoadForbiddenTerms: %v", err)
	}
	return terms
}

func sampleStructurerOutput() *domain.StructurerOutput {
	return &domain.StructurerOutput{
		SCPID: "SCP-TEST",
		Acts: []domain.Act{
			{ID: domain.ActIncident, Name: "Act 1", Synopsis: "Incident", SceneBudget: 2, DurationRatio: 0.15, DramaticBeatIDs: []int{0, 4}, KeyPoints: []string{"Beat 0"}},
			{ID: domain.ActMystery, Name: "Act 2", Synopsis: "Mystery", SceneBudget: 3, DurationRatio: 0.30, DramaticBeatIDs: []int{1}, KeyPoints: []string{"Beat 1"}},
			{ID: domain.ActRevelation, Name: "Act 3", Synopsis: "Revelation", SceneBudget: 4, DurationRatio: 0.40, DramaticBeatIDs: []int{2}, KeyPoints: []string{"Beat 2"}},
			{ID: domain.ActUnresolved, Name: "Act 4", Synopsis: "Unresolved", SceneBudget: 1, DurationRatio: 0.15, DramaticBeatIDs: []int{3}, KeyPoints: []string{"Beat 3"}},
		},
		TargetSceneCount: 10,
		SourceVersion:    domain.SourceVersionV1,
	}
}

func newWriterForTest(gen domain.TextGenerator, validator *Validator, terms *ForbiddenTerms) AgentFunc {
	return NewWriter(gen, sampleWriterConfig(), sampleWriterAssets(), validator, terms)
}
