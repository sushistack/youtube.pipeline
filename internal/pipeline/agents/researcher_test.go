package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
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

// queuedTextGenerator returns the next response in a FIFO queue per Generate
// call. Errors in the response queue propagate to the caller (transport
// failure simulation). When the queue is exhausted the call fails the test.
type queuedTextGenerator struct {
	mu        sync.Mutex
	responses []queuedResponse
	calls     int
	prompts   []string
}

type queuedResponse struct {
	content string
	err     error
}

func newQueuedGen(items ...queuedResponse) *queuedTextGenerator {
	return &queuedTextGenerator{responses: items}
}

func (q *queuedTextGenerator) Generate(_ context.Context, req domain.TextRequest) (domain.TextResponse, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.calls++
	q.prompts = append(q.prompts, req.Prompt)
	if len(q.responses) == 0 {
		return domain.TextResponse{}, fmt.Errorf("queuedTextGenerator: no response queued (call #%d)", q.calls)
	}
	next := q.responses[0]
	q.responses = q.responses[1:]
	if next.err != nil {
		return domain.TextResponse{}, next.err
	}
	return domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{
		Content:  next.content,
		Model:    "test-classifier-model",
		Provider: "openai",
	}}, nil
}

func happyClassifierResponse(roles ...string) string {
	type cls struct {
		Index int    `json:"index"`
		Role  string `json:"role"`
	}
	type wrapper struct {
		Classifications []cls `json:"classifications"`
	}
	w := wrapper{Classifications: make([]cls, len(roles))}
	for i, r := range roles {
		w.Classifications[i] = cls{Index: i, Role: r}
	}
	raw, _ := json.Marshal(w)
	return string(raw)
}

func sampleClassifierConfig() TextAgentConfig {
	return TextAgentConfig{Model: "test-classifier-model", Provider: "openai", MaxTokens: 1024, Temperature: 0.0}
}

func sampleClassifierAssets() PromptAssets {
	return PromptAssets{
		RoleClassifierTemplate: "scp={scp_id} count={beat_count}\nbeats:\n{beat_table}\n",
	}
}

// scpTestRoles matches the role assignment baked into the SCP-TEST sample
// fixture so the researcher's stubbed classifier can drive the deterministic
// chain through to the structurer fixture comparison.
var scpTestRoles = []string{
	domain.RoleHook,
	domain.RoleTension,
	domain.RoleTension,
	domain.RoleReveal,
	domain.RoleBridge,
}

func newResearcherForTest(t *testing.T, corpus CorpusReader, gen domain.TextGenerator) AgentFunc {
	t.Helper()
	return NewResearcher(
		corpus,
		mustValidator(t, "researcher_output.schema.json"),
		gen,
		sampleClassifierConfig(),
		sampleClassifierAssets(),
	)
}

func TestResearcher_Run_SCPTest_Happy(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{SCPID: "SCP-TEST"}
	gen := newQueuedGen(queuedResponse{content: happyClassifierResponse(scpTestRoles...)})
	researcher := NewResearcher(
		NewFilesystemCorpus(filepath.Join(testutil.ProjectRoot(t), "testdata", "fixtures", "corpus")),
		mustValidator(t, "researcher_output.schema.json"),
		gen,
		sampleClassifierConfig(),
		sampleClassifierAssets(),
	)
	if err := researcher(context.Background(), state); err != nil {
		t.Fatalf("Researcher: %v", err)
	}
	if state.Research == nil {
		t.Fatal("expected research output")
	}
	testutil.AssertEqual(t, state.Research.Title, "SCP-TEST")
	if len(state.Research.DramaticBeats) < 4 {
		t.Fatalf("expected at least 4 beats, got %d", len(state.Research.DramaticBeats))
	}
	for i, beat := range state.Research.DramaticBeats {
		if beat.RoleSuggestion != scpTestRoles[i] {
			t.Fatalf("beat[%d] role=%q want=%q", i, beat.RoleSuggestion, scpTestRoles[i])
		}
	}
	testutil.AssertEqual(t, state.Research.SourceVersion, domain.SourceVersionV1)
	testutil.AssertEqual(t, gen.calls, 1)
	if err := mustValidator(t, "researcher_output.schema.json").Validate(state.Research); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestResearcher_Run_Validates_SampleFixture(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{SCPID: "SCP-TEST"}
	gen := newQueuedGen(queuedResponse{content: happyClassifierResponse(scpTestRoles...)})
	researcher := NewResearcher(
		NewFilesystemCorpus(filepath.Join(testutil.ProjectRoot(t), "testdata", "fixtures", "corpus")),
		mustValidator(t, "researcher_output.schema.json"),
		gen,
		sampleClassifierConfig(),
		sampleClassifierAssets(),
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
	err := newResearcherForTest(t, fakeCorpusReader{}, newQueuedGen())(context.Background(), state)
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
	err := newResearcherForTest(t, fakeCorpusReader{err: ErrCorpusNotFound}, newQueuedGen())(context.Background(), state)
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
	err := newResearcherForTest(t, fakeCorpusReader{doc: doc}, newQueuedGen())(context.Background(), state)
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
	gen := newQueuedGen(queuedResponse{content: happyClassifierResponse(scpTestRoles...)})
	err := newResearcherForTest(t, fakeCorpusReader{doc: doc}, gen)(context.Background(), state)
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
	gen := newQueuedGen(queuedResponse{content: happyClassifierResponse(scpTestRoles...)})
	err := newResearcherForTest(t, fakeCorpusReader{doc: doc}, gen)(context.Background(), state)
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
	gen := newQueuedGen(queuedResponse{content: happyClassifierResponse(scpTestRoles...)})
	err := newResearcherForTest(t, fakeCorpusReader{doc: doc}, gen)(context.Background(), state)
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
	gen := newQueuedGen(queuedResponse{content: happyClassifierResponse(scpTestRoles...)})
	err := newResearcherForTest(t, fakeCorpusReader{doc: doc}, gen)(context.Background(), state)
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
	gen := newQueuedGen(queuedResponse{content: happyClassifierResponse(scpTestRoles...)})
	err := newResearcherForTest(t, fakeCorpusReader{doc: sampleCorpusDocument()}, gen)(context.Background(), state)
	if err != nil {
		t.Fatalf("Researcher: %v", err)
	}
}

// TestResearcher_Classifier_HappyAfterOneBadAttempt verifies the retry budget
// covers transient flakes: first response is malformed, second returns a
// valid balanced classification, run succeeds.
func TestResearcher_Classifier_HappyAfterOneBadAttempt(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{SCPID: "SCP-TEST"}
	gen := newQueuedGen(
		queuedResponse{content: "not-json"},
		queuedResponse{content: happyClassifierResponse(scpTestRoles...)},
	)
	err := newResearcherForTest(t, fakeCorpusReader{doc: sampleCorpusDocument()}, gen)(context.Background(), state)
	if err != nil {
		t.Fatalf("Researcher: %v", err)
	}
	testutil.AssertEqual(t, gen.calls, 2)
	if state.Research.DramaticBeats[0].RoleSuggestion != domain.RoleHook {
		t.Fatalf("expected hook on beat 0, got %q", state.Research.DramaticBeats[0].RoleSuggestion)
	}
}

// TestResearcher_Classifier_AllAttemptsMalformedJSON exhausts the retry budget
// with garbage responses and asserts ErrStageFailed (no degraded output).
func TestResearcher_Classifier_AllAttemptsMalformedJSON(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{SCPID: "SCP-TEST"}
	gen := newQueuedGen(
		queuedResponse{content: "garbage1"},
		queuedResponse{content: "garbage2"},
		queuedResponse{content: "garbage3"},
	)
	err := newResearcherForTest(t, fakeCorpusReader{doc: sampleCorpusDocument()}, gen)(context.Background(), state)
	if !errors.Is(err, domain.ErrStageFailed) {
		t.Fatalf("expected ErrStageFailed, got %v", err)
	}
	testutil.AssertEqual(t, gen.calls, roleClassifierAttempts)
	if state.Research != nil {
		t.Fatalf("state mutated on classifier failure: %+v", state.Research)
	}
}

// TestResearcher_Classifier_AllAttemptsMissingRole rejects responses where
// any of hook/tension/reveal/bridge is absent, even if the JSON is well
// formed and the indices line up.
func TestResearcher_Classifier_AllAttemptsMissingRole(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{SCPID: "SCP-TEST"}
	// 5 beats, all classified as hook → bridge/reveal/tension missing.
	imbalanced := happyClassifierResponse(domain.RoleHook, domain.RoleHook, domain.RoleHook, domain.RoleHook, domain.RoleHook)
	gen := newQueuedGen(
		queuedResponse{content: imbalanced},
		queuedResponse{content: imbalanced},
		queuedResponse{content: imbalanced},
	)
	err := newResearcherForTest(t, fakeCorpusReader{doc: sampleCorpusDocument()}, gen)(context.Background(), state)
	if !errors.Is(err, domain.ErrStageFailed) {
		t.Fatalf("expected ErrStageFailed, got %v", err)
	}
	testutil.AssertEqual(t, gen.calls, roleClassifierAttempts)
	if !strings.Contains(err.Error(), "missing role") {
		t.Fatalf("expected 'missing role' in error, got %v", err)
	}
}

// TestResearcher_Classifier_DuplicateIndices rejects responses where two
// classifications target the same beat index.
func TestResearcher_Classifier_DuplicateIndices(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{SCPID: "SCP-TEST"}
	dupe := `{"classifications":[{"index":0,"role":"hook"},{"index":0,"role":"tension"},{"index":2,"role":"reveal"},{"index":3,"role":"bridge"},{"index":4,"role":"tension"}]}`
	gen := newQueuedGen(
		queuedResponse{content: dupe},
		queuedResponse{content: dupe},
		queuedResponse{content: dupe},
	)
	err := newResearcherForTest(t, fakeCorpusReader{doc: sampleCorpusDocument()}, gen)(context.Background(), state)
	if !errors.Is(err, domain.ErrStageFailed) {
		t.Fatalf("expected ErrStageFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "duplicate index") {
		t.Fatalf("expected 'duplicate index' in error, got %v", err)
	}
}

// TestResearcher_Classifier_CountMismatch rejects responses whose
// classification count doesn't equal the beat count.
func TestResearcher_Classifier_CountMismatch(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{SCPID: "SCP-TEST"}
	// Sample corpus has 5 beats; respond with only 3.
	short := `{"classifications":[{"index":0,"role":"hook"},{"index":1,"role":"tension"},{"index":2,"role":"reveal"}]}`
	gen := newQueuedGen(
		queuedResponse{content: short},
		queuedResponse{content: short},
		queuedResponse{content: short},
	)
	err := newResearcherForTest(t, fakeCorpusReader{doc: sampleCorpusDocument()}, gen)(context.Background(), state)
	if !errors.Is(err, domain.ErrStageFailed) {
		t.Fatalf("expected ErrStageFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "want 5") {
		t.Fatalf("expected count mismatch error, got %v", err)
	}
}

// TestResearcher_Classifier_TransportError exhausts retries on transport
// errors (network failures) and returns ErrStageFailed.
func TestResearcher_Classifier_TransportError(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{SCPID: "SCP-TEST"}
	netErr := errors.New("simulated network failure")
	gen := newQueuedGen(
		queuedResponse{err: netErr},
		queuedResponse{err: netErr},
		queuedResponse{err: netErr},
	)
	err := newResearcherForTest(t, fakeCorpusReader{doc: sampleCorpusDocument()}, gen)(context.Background(), state)
	if !errors.Is(err, domain.ErrStageFailed) {
		t.Fatalf("expected ErrStageFailed, got %v", err)
	}
	testutil.AssertEqual(t, gen.calls, roleClassifierAttempts)
}

// TestResearcher_Classifier_StripsCodeFence accepts responses wrapped in a
// markdown ```json fence, mirroring the writer's JSON-fence tolerance.
func TestResearcher_Classifier_StripsCodeFence(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{SCPID: "SCP-TEST"}
	fenced := "```json\n" + happyClassifierResponse(scpTestRoles...) + "\n```"
	gen := newQueuedGen(queuedResponse{content: fenced})
	err := newResearcherForTest(t, fakeCorpusReader{doc: sampleCorpusDocument()}, gen)(context.Background(), state)
	if err != nil {
		t.Fatalf("Researcher: %v", err)
	}
}

// TestResearcher_Classifier_RejectsTrailingContent guards against the LLM
// appending prose after the JSON object. json.Decoder.Decode reads one
// value and stops; without an explicit drain check, trailing prose would
// silently pass.
func TestResearcher_Classifier_RejectsTrailingContent(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{SCPID: "SCP-TEST"}
	withTrailing := happyClassifierResponse(scpTestRoles...) + "\n\nNote: I'm uncertain about beat 3."
	gen := newQueuedGen(
		queuedResponse{content: withTrailing},
		queuedResponse{content: withTrailing},
		queuedResponse{content: withTrailing},
	)
	err := newResearcherForTest(t, fakeCorpusReader{doc: sampleCorpusDocument()}, gen)(context.Background(), state)
	if !errors.Is(err, domain.ErrStageFailed) {
		t.Fatalf("expected ErrStageFailed for trailing content, got %v", err)
	}
	if !strings.Contains(err.Error(), "trailing content") {
		t.Fatalf("expected trailing-content error, got %v", err)
	}
}

// TestResearcher_Classifier_StripsBOM tolerates a UTF-8 BOM prefix on the
// response — some providers/proxies inject U+FEFF into multilingual output.
func TestResearcher_Classifier_StripsBOM(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{SCPID: "SCP-TEST"}
	withBOM := "\ufeff" + happyClassifierResponse(scpTestRoles...)
	gen := newQueuedGen(queuedResponse{content: withBOM})
	err := newResearcherForTest(t, fakeCorpusReader{doc: sampleCorpusDocument()}, gen)(context.Background(), state)
	if err != nil {
		t.Fatalf("BOM-prefixed classifier response rejected: %v", err)
	}
	testutil.AssertEqual(t, gen.calls, 1)
}

// TestResearcher_Classifier_PromptIncludesAllBeats verifies the rendered
// prompt actually lists every beat's index/source/description so the LLM
// has the context required to classify correctly.
func TestResearcher_Classifier_PromptIncludesAllBeats(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{SCPID: "SCP-TEST"}
	gen := newQueuedGen(queuedResponse{content: happyClassifierResponse(scpTestRoles...)})
	err := newResearcherForTest(t, fakeCorpusReader{doc: sampleCorpusDocument()}, gen)(context.Background(), state)
	if err != nil {
		t.Fatalf("Researcher: %v", err)
	}
	if len(gen.prompts) == 0 {
		t.Fatal("no prompt captured")
	}
	prompt := gen.prompts[0]
	for i, beat := range state.Research.DramaticBeats {
		if !strings.Contains(prompt, fmt.Sprintf("index=%d", i)) {
			t.Fatalf("prompt missing index=%d marker", i)
		}
		if !strings.Contains(prompt, beat.Description) {
			t.Fatalf("prompt missing beat[%d] description", i)
		}
	}
	if !strings.Contains(prompt, "count=5") {
		t.Fatalf("prompt missing beat count: %s", prompt)
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
