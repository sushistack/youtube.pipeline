package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// twoStageFakeGen routes Generate calls to per-stage per-act response queues,
// distinguishing stage-1 (writer) vs. stage-2 (segmenter) by the request
// Model field. Each stage has its own FIFO of responses keyed by act_id.
//
// Mirrors the contract of v1's actIndexedTextGenerator but covers the
// two-stage writer the spec demands.
type twoStageFakeGen struct {
	mu sync.Mutex
	// stageQueues[stageKey][actID] is a FIFO of response Content strings.
	stageQueues map[string]map[string][]string
	// finishReason[stageKey][actID] is a parallel FIFO of finish_reason
	// strings. Same length as stageQueues[stageKey][actID]; "" means stop.
	finishReason map[string]map[string][]string
	calls        map[string]map[string]int
	prompts      map[string]map[string][]string
}

const (
	stageKeyWriter    = "writer"
	stageKeySegmenter = "segmenter"

	testWriterModel    = "writer-model"
	testSegmenterModel = "segmenter-model"
)

func newTwoStageFakeGen() *twoStageFakeGen {
	return &twoStageFakeGen{
		stageQueues:  map[string]map[string][]string{stageKeyWriter: {}, stageKeySegmenter: {}},
		finishReason: map[string]map[string][]string{stageKeyWriter: {}, stageKeySegmenter: {}},
		calls:        map[string]map[string]int{stageKeyWriter: {}, stageKeySegmenter: {}},
		prompts:      map[string]map[string][]string{stageKeyWriter: {}, stageKeySegmenter: {}},
	}
}

func (f *twoStageFakeGen) Generate(_ context.Context, req domain.TextRequest) (domain.TextResponse, error) {
	stageKey, marker := stageOf(req.Model)
	if stageKey == "" {
		return domain.TextResponse{}, fmt.Errorf("test gen: unknown model %q", req.Model)
	}
	actID := extractMarkerFromPrompt(req.Prompt, marker)
	if actID == "" {
		return domain.TextResponse{}, fmt.Errorf("test gen: prompt missing %s<id>] marker", marker)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prompts[stageKey][actID] = append(f.prompts[stageKey][actID], req.Prompt)
	queue, ok := f.stageQueues[stageKey][actID]
	if !ok || len(queue) == 0 {
		return domain.TextResponse{}, fmt.Errorf("test gen: no %s response queued for act %s", stageKey, actID)
	}
	resp := queue[0]
	f.stageQueues[stageKey][actID] = queue[1:]
	f.calls[stageKey][actID]++
	finish := ""
	if frQueue, ok := f.finishReason[stageKey][actID]; ok && len(frQueue) > 0 {
		finish = frQueue[0]
		f.finishReason[stageKey][actID] = frQueue[1:]
	}
	model := req.Model
	provider := "openai"
	if stageKey == stageKeySegmenter {
		provider = "dashscope"
	}
	return domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{
		Content:      resp,
		Model:        model,
		Provider:     provider,
		FinishReason: finish,
	}}, nil
}

func stageOf(model string) (string, string) {
	switch model {
	case testWriterModel:
		return stageKeyWriter, "[ACT:"
	case testSegmenterModel:
		return stageKeySegmenter, "[SEG:"
	default:
		return "", ""
	}
}

func extractMarkerFromPrompt(prompt, marker string) string {
	i := strings.Index(prompt, marker)
	if i < 0 {
		return ""
	}
	rest := prompt[i+len(marker):]
	j := strings.Index(rest, "]")
	if j < 0 {
		return ""
	}
	return rest[:j]
}

func (f *twoStageFakeGen) enqueue(stageKey, actID string, content, finish string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stageQueues[stageKey][actID] == nil {
		f.stageQueues[stageKey][actID] = []string{}
		f.finishReason[stageKey][actID] = []string{}
	}
	f.stageQueues[stageKey][actID] = append(f.stageQueues[stageKey][actID], content)
	f.finishReason[stageKey][actID] = append(f.finishReason[stageKey][actID], finish)
}

func (f *twoStageFakeGen) callCount(stageKey, actID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[stageKey][actID]
}

// --- prompt assets ------------------------------------------------------

func sampleWriterAssets() PromptAssets {
	return PromptAssets{
		WriterTemplate: "[ACT:{act_id}]\nrune_cap={monologue_rune_cap}\n" +
			"scp={scp_id} synopsis={act_synopsis}\nprior={prior_act_summary}\nkp={act_key_points}\n" +
			"ref={scp_visual_reference}\ncont={containment_constraints}\nfg={format_guide}\n" +
			"forbidden={forbidden_terms_section}\nglossary={glossary_section}\n" +
			"qf={quality_feedback}\nrf={retry_feedback}\nexemplar={exemplar_scenes}",
		SegmenterTemplate: "[SEG:{act_id}]\nmood={act_mood}\nkp={act_key_points}\n" +
			"monologue={monologue}\nrunes={monologue_rune_count}\nref={scp_visual_reference}\n" +
			"facts={fact_tag_catalog}",
		CriticTemplate:          "unused",
		VisualBreakdownTemplate: "unused",
		ReviewerTemplate:        "unused",
		FormatGuide:             "guide",
		ExemplarsByAct: map[string]string{
			domain.ActIncident:   "[exemplar incident stub]",
			domain.ActMystery:    "[exemplar mystery stub]",
			domain.ActRevelation: "[exemplar revelation stub]",
			domain.ActUnresolved: "[exemplar unresolved stub]",
		},
	}
}

func sampleWriterCfg() TextAgentConfig {
	return TextAgentConfig{Model: testWriterModel, Provider: "openai", MaxTokens: 4096, Temperature: 0.7}
}

// sampleWriterConfig is the legacy alias retained for downstream agent tests
// (visual_breakdowner_test.go, etc.) that constructed agents with the v1
// writer config shape. Agents that don't drive the two-stage writer can use
// this single TextAgentConfig as before.
func sampleWriterConfig() TextAgentConfig {
	return TextAgentConfig{Model: "gpt-test-writer", Provider: "openai", MaxTokens: 4096, Temperature: 0.7}
}

func sampleSegmenterCfg() TextAgentConfig {
	return TextAgentConfig{Model: testSegmenterModel, Provider: "dashscope", MaxTokens: 2048, Temperature: 0.0}
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

// --- response builders --------------------------------------------------

// fixtureClauses returns the number of 10-rune clauses to use in this act's
// monologue fixture: a multiple of 8 (so an 8-beat segmentation with equal
// slices ends each beat on a `.`) just above the per-act
// domain.ActMonologueRuneFloor. Sized minimally to keep tests fast while
// satisfying the floor validator; per-act sizes:
//
//	incident   floor 288  → 32 clauses (320 runes)
//	mystery    floor 960  → 104 clauses (1040 runes)
//	revelation floor 1248 → 128 clauses (1280 runes)
//	unresolved floor 672  → 72 clauses (720 runes)
func fixtureClauses(actID string) int {
	floor := domain.ActMonologueRuneFloor[actID]
	// Clauses needed to cover the floor (ceil), rounded up to the next
	// multiple of 8.
	c := (floor + 9) / 10
	c = ((c + 7) / 8) * 8
	if c*10 == floor { // ensure strictly above floor
		c += 8
	}
	return c
}

// fixtureBeatRunes returns the per-beat rune count for the canonical 8-beat
// fixture sized to this act's monologue (= fixtureClauses(actID)*10 / 8).
// Each beat covers a clean multiple of 10 runes so end_offsets land on `.`.
func fixtureBeatRunes(actID string) int {
	return (fixtureClauses(actID) / 8) * 10
}

// validMonologueForAct returns a placeholder monologue sized just above the
// per-act floor (domain.ActMonologueRuneFloor). The string is composed of N
// nine-syllable Korean clauses each terminated with `.`, where N is a
// multiple of 8 so an 8-beat segmentation lands every end_offset on a `.`.
func validMonologueForAct(actID string) string {
	return strings.Repeat(strings.Repeat("가", 9)+".", fixtureClauses(actID))
}

func validMonologueResponse(actID string) string {
	resp, _ := json.Marshal(map[string]any{
		"act_id":     actID,
		"monologue":  validMonologueForAct(actID),
		"mood":       "tense",
		"key_points": []string{"first", "second"},
	})
	return string(resp)
}

func validBeatsResponse(actID string) string {
	beatRunes := fixtureBeatRunes(actID)
	beats := []map[string]any{}
	for i := 0; i < 8; i++ {
		beats = append(beats, map[string]any{
			"start_offset":       i * beatRunes,
			"end_offset":         (i + 1) * beatRunes,
			"mood":               "tense",
			"location":           "site-19",
			"characters_present": []string{"SCP-TEST"},
			"entity_visible":     i%2 == 0,
			"color_palette":      "gray",
			"atmosphere":         "subdued",
			"fact_tags":          []map[string]string{},
		})
	}
	resp, _ := json.Marshal(map[string]any{"act_id": actID, "beats": beats})
	return string(resp)
}

// --- happy path ---------------------------------------------------------

func TestWriter_HappyPath_Builds4ActsEachWith8Beats(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := newTwoStageFakeGen()
	for _, a := range domain.ActOrder {
		gen.enqueue(stageKeyWriter, a, validMonologueResponse(a), "")
		gen.enqueue(stageKeySegmenter, a, validBeatsResponse(a), "")
	}
	state := freshWriterState()
	err := newTestWriter(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if err != nil {
		t.Fatalf("writer: %v", err)
	}
	if state.Narration == nil {
		t.Fatal("state.Narration is nil")
	}
	if state.Narration.SourceVersion != domain.NarrationSourceVersionV2 {
		t.Fatalf("source_version=%q want %q", state.Narration.SourceVersion, domain.NarrationSourceVersionV2)
	}
	if got, want := len(state.Narration.Acts), 4; got != want {
		t.Fatalf("acts=%d want %d", got, want)
	}
	for i, act := range state.Narration.Acts {
		if act.ActID != domain.ActOrder[i] {
			t.Fatalf("acts[%d].ActID=%q want %q", i, act.ActID, domain.ActOrder[i])
		}
		if act.Monologue == "" {
			t.Fatalf("acts[%d] empty monologue", i)
		}
		if got := len(act.Beats); got < 8 || got > 10 {
			t.Fatalf("acts[%d] beat count=%d, want [8, 10]", i, got)
		}
	}
	// Total beats = 4 acts × 8 = 32; metadata.scene_count should match.
	if got, want := state.Narration.Metadata.SceneCount, 32; got != want {
		t.Fatalf("metadata.scene_count=%d want %d", got, want)
	}
}

// --- I/O matrix: stage-2 offsets out of range / overlapping ------------

func TestWriter_Stage2_OffsetsOutOfRange_RetriesThenFails(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := newTwoStageFakeGen()
	// Act 1 stage-1 OK
	gen.enqueue(stageKeyWriter, domain.ActIncident, validMonologueResponse(domain.ActIncident), "")
	// Act 1 stage-2 produces an out-of-range end_offset on every attempt.
	// budget=2 → up to 3 attempts.
	bad := badBeatsResponse(domain.ActIncident, 99999)
	gen.enqueue(stageKeySegmenter, domain.ActIncident, bad, "")
	gen.enqueue(stageKeySegmenter, domain.ActIncident, bad, "")
	gen.enqueue(stageKeySegmenter, domain.ActIncident, bad, "")

	state := freshWriterState()
	err := newTestWriter(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if err == nil {
		t.Fatal("expected validation failure, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("error=%v, want ErrValidation chain", err)
	}
	if !strings.Contains(err.Error(), "end_offset=99999") && !strings.Contains(err.Error(), "out of range") && !strings.Contains(err.Error(), "monologue_rune_count") {
		t.Fatalf("error message lacks offset detail: %v", err)
	}
	if state.Narration != nil {
		t.Fatalf("state.Narration must remain unset on failure: %+v", state.Narration)
	}
	// Retry budget = 2 → 3 total attempts.
	if got := gen.callCount(stageKeySegmenter, domain.ActIncident); got != 3 {
		t.Fatalf("segmenter call count=%d, want 3 (1 + 2 retries)", got)
	}
}

func TestWriter_Stage2_OverlappingOffsets_RetriesThenFails(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := newTwoStageFakeGen()
	gen.enqueue(stageKeyWriter, domain.ActIncident, validMonologueResponse(domain.ActIncident), "")
	bad := overlappingBeatsResponse(domain.ActIncident)
	// budget=2 → up to 3 attempts.
	gen.enqueue(stageKeySegmenter, domain.ActIncident, bad, "")
	gen.enqueue(stageKeySegmenter, domain.ActIncident, bad, "")
	gen.enqueue(stageKeySegmenter, domain.ActIncident, bad, "")

	state := freshWriterState()
	err := newTestWriter(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if err == nil {
		t.Fatal("expected validation failure, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("error=%v, want ErrValidation", err)
	}
	if !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("error should mention overlap: %v", err)
	}
}

// --- I/O matrix: stage-2 beat count outside [8, 10] --------------------

func TestWriter_Stage2_BeatCountOutOfRange_FailsAfterRetry(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := newTwoStageFakeGen()
	gen.enqueue(stageKeyWriter, domain.ActIncident, validMonologueResponse(domain.ActIncident), "")
	bad := nBeatsResponse(domain.ActIncident, 5) // 5 beats — below the 8 floor
	// budget=2 → up to 3 attempts; queue one bad response per attempt.
	gen.enqueue(stageKeySegmenter, domain.ActIncident, bad, "")
	gen.enqueue(stageKeySegmenter, domain.ActIncident, bad, "")
	gen.enqueue(stageKeySegmenter, domain.ActIncident, bad, "")

	state := freshWriterState()
	err := newTestWriter(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if err == nil {
		t.Fatal("expected validation failure, got nil")
	}
	if !strings.Contains(err.Error(), "beat count=5") {
		t.Fatalf("error should report count: %v", err)
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("error=%v, want ErrValidation", err)
	}
}

// --- I/O matrix: finish_reason=length both stages ----------------------

func TestWriter_Stage1_TruncationRetried(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := newTwoStageFakeGen()
	// Act 1 stage-1: first attempt truncates, second attempt succeeds.
	gen.enqueue(stageKeyWriter, domain.ActIncident, "{garbage}", "length")
	gen.enqueue(stageKeyWriter, domain.ActIncident, validMonologueResponse(domain.ActIncident), "")
	gen.enqueue(stageKeySegmenter, domain.ActIncident, validBeatsResponse(domain.ActIncident), "")
	for _, a := range []string{domain.ActMystery, domain.ActRevelation, domain.ActUnresolved} {
		gen.enqueue(stageKeyWriter, a, validMonologueResponse(a), "")
		gen.enqueue(stageKeySegmenter, a, validBeatsResponse(a), "")
	}

	state := freshWriterState()
	err := newTestWriter(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if err != nil {
		t.Fatalf("writer should recover on retry: %v", err)
	}
	if got := gen.callCount(stageKeyWriter, domain.ActIncident); got != 2 {
		t.Fatalf("writer act incident call count=%d, want 2 (1 truncation + 1 success)", got)
	}
	if state.Narration == nil {
		t.Fatal("state.Narration nil on recovered run")
	}
}

func TestWriter_Stage2_TruncationRetryExhausted_AtomicFailure(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := newTwoStageFakeGen()
	gen.enqueue(stageKeyWriter, domain.ActIncident, validMonologueResponse(domain.ActIncident), "")
	// budget=2 → up to 3 attempts.
	gen.enqueue(stageKeySegmenter, domain.ActIncident, "{garbage}", "length")
	gen.enqueue(stageKeySegmenter, domain.ActIncident, "{garbage}", "length")
	gen.enqueue(stageKeySegmenter, domain.ActIncident, "{garbage}", "length")

	state := freshWriterState()
	err := newTestWriter(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if err == nil {
		t.Fatal("expected truncation failure, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("error=%v, want ErrValidation", err)
	}
	if state.Narration != nil {
		t.Fatalf("state.Narration must remain unset on atomic failure: %+v", state.Narration)
	}
}

// --- I/O matrix: stage 1 OK, stage 2 fails after retries (atomicity) ---

func TestWriter_Stage1OkThenStage2Fails_AtomicFailure(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := newTwoStageFakeGen()
	gen.enqueue(stageKeyWriter, domain.ActIncident, validMonologueResponse(domain.ActIncident), "")
	gen.enqueue(stageKeySegmenter, domain.ActIncident, `{"act_id":"incident","beats":[]}`, "")
	gen.enqueue(stageKeySegmenter, domain.ActIncident, `{"act_id":"incident","beats":[]}`, "")

	state := freshWriterState()
	err := newTestWriter(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if err == nil {
		t.Fatal("expected stage-2 failure, got nil")
	}
	if state.Narration != nil {
		t.Fatalf("state.Narration must remain unset: %+v", state.Narration)
	}
}

// --- D6: mid-cascade atomicity (writer fails after partial work) -------

// TestWriter_MidCascadeFailure_LeavesNarrationNil locks the v2 writer
// atomicity invariant (D6 spec: state.Narration is set ONLY after all 4
// acts × 2 stages succeed). Act 1 succeeds end-to-end; Act 2 stage 2
// exhausts its retry budget. The writer must abort the cascade — Acts 3
// and 4 never run — and must NOT persist Act 1's monologue/beats into
// state.Narration. Without this freeze, a future refactor that
// optimistically pre-fills state.Narration.Acts inside the per-act loop
// would silently break resume semantics by leaving a half-populated
// NarrationScript on disk.
func TestWriter_MidCascadeFailure_LeavesNarrationNil(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := newTwoStageFakeGen()
	// Act 1 (incident): both stages succeed.
	gen.enqueue(stageKeyWriter, domain.ActIncident, validMonologueResponse(domain.ActIncident), "")
	gen.enqueue(stageKeySegmenter, domain.ActIncident, validBeatsResponse(domain.ActIncident), "")
	// Act 2 (mystery): stage 1 succeeds; stage 2 exhausts its retry budget
	// (writerPerStageRetryBudget+1 total attempts) with out-of-range offsets.
	// Sourcing the count from the production constant means a future budget
	// tweak does not silently break this atomicity freeze.
	gen.enqueue(stageKeyWriter, domain.ActMystery, validMonologueResponse(domain.ActMystery), "")
	bad := badBeatsResponse(domain.ActMystery, 99999)
	for i := 0; i < writerPerStageRetryBudget+1; i++ {
		gen.enqueue(stageKeySegmenter, domain.ActMystery, bad, "")
	}
	// Acts 3/4: NO responses queued. The cascade must not reach them; if
	// it does, twoStageFakeGen.Generate returns an error mentioning the
	// missing queue, which would surface as a different error string.

	state := freshWriterState()
	err := newTestWriter(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if err == nil {
		t.Fatal("expected mid-cascade failure, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("error=%v, want ErrValidation chain", err)
	}
	if state.Narration != nil {
		t.Fatalf("state.Narration must remain unset on mid-cascade failure (Act 1 succeeded but Act 2 stage 2 failed): %+v", state.Narration)
	}
	// Acts 1 and 2 are reached; Acts 3 and 4 are NOT.
	if got := gen.callCount(stageKeyWriter, domain.ActIncident); got != 1 {
		t.Errorf("incident stage-1 calls=%d, want 1", got)
	}
	if got := gen.callCount(stageKeySegmenter, domain.ActIncident); got != 1 {
		t.Errorf("incident stage-2 calls=%d, want 1", got)
	}
	if got := gen.callCount(stageKeyWriter, domain.ActMystery); got != 1 {
		t.Errorf("mystery stage-1 calls=%d, want 1", got)
	}
	if got, want := gen.callCount(stageKeySegmenter, domain.ActMystery), writerPerStageRetryBudget+1; got != want {
		t.Errorf("mystery stage-2 calls=%d, want %d (retry exhausted: budget=%d ⇒ %d total)", got, want, writerPerStageRetryBudget, want)
	}
	for _, a := range []string{domain.ActRevelation, domain.ActUnresolved} {
		if got := gen.callCount(stageKeyWriter, a); got != 0 {
			t.Errorf("act %s stage-1 must not be reached after Act 2 failure, got %d calls", a, got)
		}
		if got := gen.callCount(stageKeySegmenter, a); got != 0 {
			t.Errorf("act %s stage-2 must not be reached after Act 2 failure, got %d calls", a, got)
		}
	}
}

// TestWriter_FullCascadeOk_ScriptValidatorFailure_LeavesNarrationNil
// closes the second post-cascade atomicity branch the mid-cascade test
// can't reach: every per-act per-stage call succeeds, but the assembled
// NarrationScript is rejected by the script-level forbidden-terms check
// (writer.go: terms.MatchNarration(&script) at line 168). The atomic
// invariant says state.Narration is set ONLY after BOTH per-stage AND
// script-level validation pass — moving the assignment above the
// terms-check or validator.Validate would silently leak a forbidden
// monologue into resume's "valid stage artifact" pathway. Without this
// test, that regression would pass the per-stage atomicity test (which
// fails before assembly) and the integration test (which uses a stub
// agent that never assigns Narration).
func TestWriter_FullCascadeOk_ScriptValidatorFailure_LeavesNarrationNil(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Craft a stage-1 response for Act 1 whose monologue contains the
	// forbidden term "wiki" (loaded into mustTerms via policyRoot's
	// "# comment\nwiki\n" file). Other acts get clean monologues. The
	// per-stage validators do NOT scan forbidden terms — that check fires
	// at script level, after all 4 acts complete.
	mkPoisonedMonologueResponse := func(actID string) string {
		// Append " wiki " to a sentence-terminated monologue. Still well under
		// any per-act rune cap (validMonologueForAct = 80 runes; minimum cap
		// is 480). Reuses the canonical 8-clause-with-period pattern so the
		// segmenter sentence-boundary validator accepts the standard 8-beat
		// fixture.
		raw := []byte(`{"act_id":"` + actID + `","monologue":"` + validMonologueForAct(actID) + ` wiki ","mood":"tense","key_points":["first","second"]}`)
		return string(raw)
	}

	gen := newTwoStageFakeGen()
	for i, a := range domain.ActOrder {
		if i == 0 {
			gen.enqueue(stageKeyWriter, a, mkPoisonedMonologueResponse(a), "")
		} else {
			gen.enqueue(stageKeyWriter, a, validMonologueResponse(a), "")
		}
		gen.enqueue(stageKeySegmenter, a, validBeatsResponse(a), "")
	}

	state := freshWriterState()
	err := newTestWriter(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if err == nil {
		t.Fatal("expected forbidden-term failure after full cascade, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("error chain must include ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "wiki") {
		t.Errorf("error should surface the offending forbidden term, got: %v", err)
	}
	if state.Narration != nil {
		t.Fatalf("state.Narration must remain unset on script-level rejection (post-cascade atomicity branch): %+v", state.Narration)
	}
}

// --- cascade serial behavior + prior-tail injection ---------------------

func TestWriter_Stage1Cascade_SerialAndInjectsPriorTail(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := newTwoStageFakeGen()
	for _, a := range domain.ActOrder {
		gen.enqueue(stageKeyWriter, a, validMonologueResponse(a), "")
		gen.enqueue(stageKeySegmenter, a, validBeatsResponse(a), "")
	}
	state := freshWriterState()
	if err := newTestWriter(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state); err != nil {
		t.Fatalf("writer: %v", err)
	}
	// Acts 2/3/4 prompts must contain a non-empty prior= block referencing
	// the previous act's tail (we use a single repeated 가 — its tail is
	// "가가가...").
	for _, a := range []string{domain.ActMystery, domain.ActRevelation, domain.ActUnresolved} {
		prompts := gen.prompts[stageKeyWriter][a]
		if len(prompts) == 0 {
			t.Fatalf("no stage-1 prompt recorded for act %s", a)
		}
		if !strings.Contains(prompts[0], "이전 act 의 마지막 부분") {
			t.Fatalf("act %s stage-1 prompt missing prior-tail block", a)
		}
	}
	// Act 1 prior block is the synthetic "first act" placeholder, not a
	// prior-tail summary.
	prompts := gen.prompts[stageKeyWriter][domain.ActIncident]
	if len(prompts) == 0 || strings.Contains(prompts[0], "이전 act 의 마지막 부분") {
		t.Fatalf("act incident stage-1 prompt should NOT carry a prior-tail block")
	}
}

// --- structural input validation ----------------------------------------

func TestWriter_RejectsNilStructure(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := freshWriterState()
	state.Structure = nil
	err := newTestWriter(newTwoStageFakeGen(), mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if err == nil || !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestWriter_RejectsMissingSegmenterModel(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	bad := sampleSegmenterCfg()
	bad.Model = ""
	state := freshWriterState()
	gen := newTwoStageFakeGen()
	err := NewWriter(gen, gen, sampleWriterCfg(), bad, sampleWriterAssets(),
		mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if err == nil || !strings.Contains(err.Error(), "stage-2 model") {
		t.Fatalf("expected stage-2 model error, got %v", err)
	}
}

// --- I/O matrix: stage-2 mid-sentence cut → snap ----------------------

// TestWriter_Stage2_MidSentenceCut_SnappedAndAccepted verifies the
// snapBeatBoundariesToSentences post-process: when the LLM emits beat
// offsets that land mid-syllable, the runner shifts each inter-beat
// boundary to the nearest sentence terminal within ±25 runes and accepts
// the result on the first attempt instead of burning retries.
func TestWriter_Stage2_MidSentenceCut_SnappedAndAccepted(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// validMonologueForAct ends every 10th rune with `.` (positions
	// 9, 19, 29, ..., last). Our bad cuts land 5 runes off the nearest
	// terminal at each beat boundary — well within the snap radius.
	mkBadCuts := func(actID string) string {
		beatRunes := fixtureBeatRunes(actID)
		runeCount := beatRunes * 8
		beats := []map[string]any{}
		for i := 0; i < 8; i++ {
			start := i * beatRunes
			end := i*beatRunes + 5
			if i == 7 {
				end = runeCount
			}
			beats = append(beats, map[string]any{
				"start_offset":       start,
				"end_offset":         end,
				"mood":               "tense",
				"location":           "site-19",
				"characters_present": []string{"SCP-TEST"},
				"entity_visible":     true,
				"color_palette":      "gray",
				"atmosphere":         "subdued",
				"fact_tags":          []map[string]string{},
			})
		}
		resp, _ := json.Marshal(map[string]any{"act_id": actID, "beats": beats})
		return string(resp)
	}

	gen := newTwoStageFakeGen()
	for _, a := range domain.ActOrder {
		gen.enqueue(stageKeyWriter, a, validMonologueResponse(a), "")
		gen.enqueue(stageKeySegmenter, a, mkBadCuts(a), "")
	}

	state := freshWriterState()
	if err := newTestWriter(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state); err != nil {
		t.Fatalf("expected snap to recover bad cuts on first attempt, got: %v", err)
	}
	if state.Narration == nil || len(state.Narration.Acts) != 4 {
		t.Fatalf("state.Narration not populated: %+v", state.Narration)
	}
	// Each act's first inter-beat boundary should have snapped from 5 → 10.
	for _, act := range state.Narration.Acts {
		if len(act.Beats) < 2 {
			continue
		}
		if got := act.Beats[0].EndOffset; got != 10 {
			t.Errorf("act %s beat[0].end_offset=%d, want 10 (snapped from 5)", act.ActID, got)
		}
		if got := act.Beats[1].StartOffset; got != 10 {
			t.Errorf("act %s beat[1].start_offset=%d, want 10 (snapped from 5)", act.ActID, got)
		}
	}
	// Snap converges in one attempt — no retries burned.
	for _, a := range domain.ActOrder {
		if got := gen.callCount(stageKeySegmenter, a); got != 1 {
			t.Errorf("act %s segmenter call count=%d, want 1 (snap should accept on first try)", a, got)
		}
	}
}

// --- helpers ------------------------------------------------------------

func freshWriterState() *PipelineState {
	return &PipelineState{
		RunID:     "run-1",
		SCPID:     "SCP-TEST",
		Research:  sampleResearchForStructurer(),
		Structure: sampleStructurerOutput(),
	}
}

// sampleWriterState is the legacy alias used by downstream-agent tests
// (reviewer_test.go, visual_breakdowner_test.go) that need a state with a
// populated Narration. Returns a state whose Narration is the canonical v2
// sample, hard-coded so callers don't need a *testing.T to read a fixture.
func sampleWriterState() *PipelineState {
	state := freshWriterState()
	script := hardcodedV2Sample()
	state.Narration = &script
	return state
}

// hardcodedV2Sample mirrors testdata/contracts/writer_output.sample.json
// (4 acts × 8 beats, 80-rune monologues using a single repeating Korean
// syllable). Hard-coded because legacy callers can't take *testing.T.
func hardcodedV2Sample() domain.NarrationScript {
	mkAct := func(actID string) domain.ActScript {
		monologue := strings.Repeat("가", 80)
		beats := make([]domain.BeatAnchor, 0, 8)
		for i := 0; i < 8; i++ {
			beats = append(beats, domain.BeatAnchor{
				StartOffset:       i * 10,
				EndOffset:         (i + 1) * 10,
				Mood:              "tense",
				Location:          "site-19",
				CharactersPresent: []string{"SCP-TEST"},
				EntityVisible:     i%2 == 0,
				ColorPalette:      "gray",
				Atmosphere:        "subdued",
				FactTags:          []domain.FactTag{},
			})
		}
		return domain.ActScript{ActID: actID, Monologue: monologue, Beats: beats, Mood: "tense", KeyPoints: []string{}}
	}
	return domain.NarrationScript{
		SCPID: "SCP-TEST", Title: "SCP-TEST",
		Acts: []domain.ActScript{
			mkAct(domain.ActIncident),
			mkAct(domain.ActMystery),
			mkAct(domain.ActRevelation),
			mkAct(domain.ActUnresolved),
		},
		Metadata: domain.NarrationMetadata{
			Language: domain.LanguageKorean, SceneCount: 32,
			WriterModel: "qwen-max", WriterProvider: "dashscope",
			PromptTemplate: "03_writing.md", FormatGuideTemplate: "format_guide.md",
			ForbiddenTermsVersion: "test",
		},
		SourceVersion: domain.NarrationSourceVersionV2,
	}
}

func newTestWriter(gen domain.TextGenerator, validator *Validator, terms *ForbiddenTerms) AgentFunc {
	// Same gen for both stages — production splits when WriterProvider !=
	// SegmenterProvider, tests don't exercise cross-provider routing.
	return NewWriter(gen, gen, sampleWriterCfg(), sampleSegmenterCfg(), sampleWriterAssets(), validator, terms)
}

func sampleStructurerOutput() *domain.StructurerOutput {
	return &domain.StructurerOutput{
		SCPID: "SCP-TEST",
		Acts: []domain.Act{
			{ID: domain.ActIncident, Name: "Act 1", Synopsis: "Incident", SceneBudget: 1, DurationRatio: 0.15, DramaticBeatIDs: []int{0}, KeyPoints: []string{"Beat 0"}, Role: domain.RoleHook},
			{ID: domain.ActMystery, Name: "Act 2", Synopsis: "Mystery", SceneBudget: 1, DurationRatio: 0.30, DramaticBeatIDs: []int{1}, KeyPoints: []string{"Beat 1"}, Role: domain.RoleTension},
			{ID: domain.ActRevelation, Name: "Act 3", Synopsis: "Revelation", SceneBudget: 1, DurationRatio: 0.40, DramaticBeatIDs: []int{2}, KeyPoints: []string{"Beat 2"}, Role: domain.RoleReveal},
			{ID: domain.ActUnresolved, Name: "Act 4", Synopsis: "Unresolved", SceneBudget: 1, DurationRatio: 0.15, DramaticBeatIDs: []int{3}, KeyPoints: []string{"Beat 3"}, Role: domain.RoleBridge},
		},
		TargetSceneCount: 32,
		SourceVersion:    domain.SourceVersionV1,
	}
}

func badBeatsResponse(actID string, badEnd int) string {
	beats := []map[string]any{}
	for i := 0; i < 8; i++ {
		end := (i + 1) * 10
		if i == 7 {
			end = badEnd // last beat has out-of-range end_offset
		}
		beats = append(beats, map[string]any{
			"start_offset":       i * 10,
			"end_offset":         end,
			"mood":               "tense",
			"location":           "site-19",
			"characters_present": []string{"SCP-TEST"},
			"entity_visible":     true,
			"color_palette":      "gray",
			"atmosphere":         "subdued",
			"fact_tags":          []map[string]string{},
		})
	}
	resp, _ := json.Marshal(map[string]any{"act_id": actID, "beats": beats})
	return string(resp)
}

func overlappingBeatsResponse(actID string) string {
	// Each end_offset lands on a `.` (multiples of beatRunes, since clauses
	// are 10 runes and beatRunes is a multiple of 10) so the sentence-
	// boundary validator passes; the overlap is the first failure mode and
	// the only one this fixture exercises. starts at i*beatRunes, ends at
	// (i+2)*beatRunes (capped at total) — beat[1].start (= beatRunes) <
	// beat[0].end (= 2*beatRunes) → overlap on beat[1].
	beatRunes := fixtureBeatRunes(actID)
	runeCount := beatRunes * 8
	beats := []map[string]any{}
	for i := 0; i < 8; i++ {
		end := (i + 2) * beatRunes
		if end > runeCount {
			end = runeCount
		}
		beats = append(beats, map[string]any{
			"start_offset":       i * beatRunes,
			"end_offset":         end,
			"mood":               "tense",
			"location":           "site-19",
			"characters_present": []string{"SCP-TEST"},
			"entity_visible":     true,
			"color_palette":      "gray",
			"atmosphere":         "subdued",
			"fact_tags":          []map[string]string{},
		})
	}
	resp, _ := json.Marshal(map[string]any{"act_id": actID, "beats": beats})
	return string(resp)
}

func nBeatsResponse(actID string, n int) string {
	beats := []map[string]any{}
	for i := 0; i < n; i++ {
		beats = append(beats, map[string]any{
			"start_offset":       i * 5,
			"end_offset":         (i + 1) * 5,
			"mood":               "tense",
			"location":           "site-19",
			"characters_present": []string{"SCP-TEST"},
			"entity_visible":     true,
			"color_palette":      "gray",
			"atmosphere":         "subdued",
			"fact_tags":          []map[string]string{},
		})
	}
	resp, _ := json.Marshal(map[string]any{"act_id": actID, "beats": beats})
	return string(resp)
}

// loadMergedV2Sample reads the canonical v2 4-act sample as a NarrationScript.
func loadMergedV2Sample(t *testing.T) domain.NarrationScript {
	t.Helper()
	var script domain.NarrationScript
	if err := json.Unmarshal(testutil.LoadFixture(t, filepath.Join("contracts", "writer_output.sample.json")), &script); err != nil {
		t.Fatalf("unmarshal sample: %v", err)
	}
	return script
}

// fakeTextGenerator stays here for tests in other files (critic_test.go, etc.)
// that reference it. It returns a fixed TextResponse for any prompt.
type fakeTextGenerator struct {
	mu    sync.Mutex
	calls int
	resp  domain.TextResponse
	err   error
}

func (f *fakeTextGenerator) Generate(_ context.Context, _ domain.TextRequest) (domain.TextResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return domain.TextResponse{}, f.err
	}
	return f.resp, nil
}

// --- stage-1 sentence-terminal floor -----------------------------------

// TestWriter_Stage1_RejectsSparseMonologue verifies that a monologue with
// fewer than beatCountMin sentence terminals is rejected at stage 1, before
// stage 2 ever sees it. This catches the unsegmentable-input case at the
// right layer instead of letting stage 2 burn retries on something the
// snap step cannot fix (adjacent boundaries cannot share a terminal).
func TestWriter_Stage1_RejectsSparseMonologue(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Monologue with only 5 sentence terminals — below the 8 floor required
	// for stage 2's [8, 10] beat range. Each clause is 59 runes + `.` so the
	// total (300 runes) clears the per-act monologue floor (incident=288)
	// and isolates the sentence-terminal check as the failing condition.
	sparseMono := strings.Repeat(strings.Repeat("가", 59)+".", 5) // 300 runes, 5 terminals
	sparseResp, _ := json.Marshal(map[string]any{
		"act_id":     domain.ActIncident,
		"monologue":  sparseMono,
		"mood":       "tense",
		"key_points": []string{"first", "second"},
	})

	gen := newTwoStageFakeGen()
	// Three attempts queued (budget=2 → 3 total). All return the same sparse
	// monologue, so all three should hit the floor and the run hard-fails
	// at stage 1 without ever calling stage 2.
	for i := 0; i < 3; i++ {
		gen.enqueue(stageKeyWriter, domain.ActIncident, string(sparseResp), "")
	}

	state := freshWriterState()
	err := newTestWriter(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if err == nil {
		t.Fatal("expected sparse-monologue failure, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("error chain must include ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "sentence terminals") {
		t.Fatalf("error should mention sentence terminals, got: %v", err)
	}
	// Stage 2 must NOT run when stage 1 keeps failing.
	if got := gen.callCount(stageKeySegmenter, domain.ActIncident); got != 0 {
		t.Errorf("segmenter called %d times despite stage-1 floor failure", got)
	}
}

// TestWriter_Stage1_BelowFloor_RetriesThenFails verifies that a monologue
// shorter than the per-act ActMonologueRuneFloor is rejected at stage 1
// with retries burned. Length under-utilization was the dogfood regression
// (gold ~3080 runes vs. observed ~1900) that motivated the floor — a writer
// that returns 200 runes for a 480-cap incident act must fail loudly so
// downstream stages don't get a too-thin monologue silently passed through.
func TestWriter_Stage1_BelowFloor_RetriesThenFails(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Monologue at 200 runes — has 20 sentence terminals (passes the 8
	// terminals floor) but is below the incident-act monologue floor
	// (domain.ActMonologueRuneFloor[ActIncident] = 288). Isolates the floor
	// check as the failing condition.
	shortMono := strings.Repeat(strings.Repeat("가", 9)+".", 20) // 200 runes, 20 terminals
	shortResp, _ := json.Marshal(map[string]any{
		"act_id":     domain.ActIncident,
		"monologue":  shortMono,
		"mood":       "tense",
		"key_points": []string{"first", "second"},
	})

	gen := newTwoStageFakeGen()
	// budget=2 → 3 attempts total. All return below-floor; run hard-fails.
	for i := 0; i < writerPerStageRetryBudget+1; i++ {
		gen.enqueue(stageKeyWriter, domain.ActIncident, string(shortResp), "")
	}

	state := freshWriterState()
	err := newTestWriter(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if err == nil {
		t.Fatal("expected below-floor failure, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("error chain must include ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "below floor") {
		t.Fatalf("error should mention below floor, got: %v", err)
	}
	if got, want := gen.callCount(stageKeyWriter, domain.ActIncident), writerPerStageRetryBudget+1; got != want {
		t.Errorf("stage-1 calls=%d, want %d (retry budget exhausted)", got, want)
	}
	// Stage 2 must NOT run — atomicity: stage-1 failure stops the cascade.
	if got := gen.callCount(stageKeySegmenter, domain.ActIncident); got != 0 {
		t.Errorf("segmenter called %d times despite stage-1 floor failure", got)
	}
}

// TestCountSentenceTerminals_TableDriven locks the helper's behavior across
// the terminal-rune set, including paragraph breaks and ellipsis.
func TestCountSentenceTerminals_TableDriven(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"empty", "", 0},
		{"no_terminals", "안녕하세요 반갑습니다", 0},
		{"period_only", "한 문장. 두 문장.", 2},
		{"mixed", "정말? 그렇구나! 음… 그래.", 4},
		{"paragraph_breaks", "첫째\n둘째\n셋째", 2}, // 2 \n between 3 lines
		{"all_terminals", ".?!…\n", 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := countSentenceTerminals(tc.in); got != tc.want {
				t.Errorf("countSentenceTerminals(%q)=%d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

// --- snapBeatBoundariesToSentences edge cases --------------------------

func TestSnapBeatBoundariesToSentences_TableDriven(t *testing.T) {
	mkBeats := func(offsets ...int) []domain.BeatAnchor {
		out := make([]domain.BeatAnchor, len(offsets)-1)
		for i := 0; i < len(offsets)-1; i++ {
			out[i] = domain.BeatAnchor{StartOffset: offsets[i], EndOffset: offsets[i+1]}
		}
		return out
	}

	cases := []struct {
		name      string
		monologue string
		input     []domain.BeatAnchor
		radius    int
		wantEnds  []int // expected EndOffset for each beat (last beat unchanged)
	}{
		{
			// "abc. def." → terminal at index 3, snap from 5 → 4 (just after `.`).
			name:      "snap_to_nearest_terminal",
			monologue: "abc. def.",
			input:     mkBeats(0, 5, 9),
			radius:    10,
			wantEnds:  []int{5, 9}, // 5 stays — `.` at idx 3, post-skip-space lands at 5.
		},
		{
			// "abcdef." — boundary 3 with no terminal in [1,6]. Last beat ends at 7.
			// Boundary 3 has no terminal in range → not snapped.
			name:      "no_terminal_in_range_left_alone",
			monologue: "abcdef.",
			input:     mkBeats(0, 3, 7),
			radius:    2,
			wantEnds:  []int{3, 7}, // boundary 3 unchanged
		},
		{
			// Already-clean boundary stays: "abc. def." — boundary at 5 already
			// after `.` (skipping space). Should not move.
			name:      "already_clean_boundary",
			monologue: "abc. def.",
			input:     mkBeats(0, 5, 9),
			radius:    10,
			wantEnds:  []int{5, 9},
		},
		{
			// Two-beat input: only beats[0].end is an inter-beat boundary; last
			// beat's end is anchored to monologue length and never moves.
			name:      "anchored_last_end_never_moves",
			monologue: "abc. def.",
			input:     mkBeats(0, 7, 9),
			radius:    10,
			// boundary 7 → snap back to 5 (just after `.` at idx 3, +space).
			wantEnds: []int{5, 9},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			beats := append([]domain.BeatAnchor(nil), tc.input...)
			snapBeatBoundariesToSentences(beats, []rune(tc.monologue), tc.radius)
			for i, want := range tc.wantEnds {
				if got := beats[i].EndOffset; got != want {
					t.Errorf("beat[%d].EndOffset=%d, want %d", i, got, want)
				}
				// Adjacency invariant: beats[i+1].start must equal beats[i].end.
				if i+1 < len(beats) && beats[i+1].StartOffset != beats[i].EndOffset {
					t.Errorf("adjacency broken: beat[%d].end=%d, beat[%d].start=%d",
						i, beats[i].EndOffset, i+1, beats[i+1].StartOffset)
				}
			}
		})
	}
}

// TestFormatMonologueLengthRetryFeedback locks the within-stage retry
// feedback formatter behavior: under-floor / over-cap messages must include
// the actual count, the band middle as a numeric target, and the action verb
// the LLM should follow. In-band and degenerate-band inputs must return ""
// so the caller never injects a useless "previous attempt was fine" line.
func TestFormatMonologueLengthRetryFeedback(t *testing.T) {
	cases := []struct {
		name        string
		actual      int
		floor       int
		capV        int
		wantEmpty   bool
		mustContain []string
		mustNotHave []string
	}{
		{
			name:   "under_floor_revelation",
			actual: 800, floor: 900, capV: 2080,
			mustContain: []string{
				"PREVIOUS ATTEMPT FAILED",
				"800 runes",
				"BELOW the floor of 900",
				"[900, 2080]",
				"~1490 runes", // (900+2080)/2 = 1490
				"expand",
				"Do NOT pad with filler",
			},
			mustNotHave: []string{"OVER the cap", "Tighten"},
		},
		{
			name:   "over_cap_unresolved",
			actual: 1500, floor: 672, capV: 1400,
			mustContain: []string{
				"PREVIOUS ATTEMPT FAILED",
				"1500 runes",
				"OVER the cap of 1400",
				"[672, 1400]",
				"~1036 runes", // (672+1400)/2 = 1036
				"Tighten",
			},
			mustNotHave: []string{"BELOW the floor", "expand", "filler"},
		},
		{
			name:      "in_band_returns_empty",
			actual:    500, floor: 288, capV: 720,
			wantEmpty: true,
		},
		{
			name:      "exactly_at_floor_is_in_band",
			actual:    288, floor: 288, capV: 720,
			wantEmpty: true,
		},
		{
			name:      "exactly_at_cap_is_in_band",
			actual:    720, floor: 288, capV: 720,
			wantEmpty: true,
		},
		{
			name:      "zero_floor_is_degenerate",
			actual:    100, floor: 0, capV: 720,
			wantEmpty: true,
		},
		{
			name:      "zero_cap_is_degenerate",
			actual:    100, floor: 288, capV: 0,
			wantEmpty: true,
		},
		{
			name:      "inverted_band_is_degenerate",
			actual:    500, floor: 720, capV: 288,
			wantEmpty: true,
		},
		{
			name:      "negative_floor_is_degenerate",
			actual:    100, floor: -1, capV: 720,
			wantEmpty: true,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := formatMonologueLengthRetryFeedback(tc.actual, tc.floor, tc.capV)
			if tc.wantEmpty {
				if got != "" {
					t.Fatalf("expected empty string, got %q", got)
				}
				return
			}
			for _, want := range tc.mustContain {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q\n  full:\n%s", want, got)
				}
			}
			for _, banned := range tc.mustNotHave {
				if strings.Contains(got, banned) {
					t.Errorf("output unexpectedly contains %q\n  full:\n%s", banned, got)
				}
			}
		})
	}
}

// queueAllValidActsExcept enqueues valid stage-1 + stage-2 fixture responses
// for every act EXCEPT the ones listed in skip. Used by retry-feedback tests
// that pre-seed the failing act's queue manually before letting the rest of
// the pipeline complete.
func queueAllValidActsExcept(gen *twoStageFakeGen, skip ...string) {
	skipped := map[string]bool{}
	for _, a := range skip {
		skipped[a] = true
	}
	for _, a := range domain.ActOrder {
		if skipped[a] {
			continue
		}
		gen.enqueue(stageKeyWriter, a, validMonologueResponse(a), "")
		gen.enqueue(stageKeySegmenter, a, validBeatsResponse(a), "")
	}
}

// TestWriter_Stage1_RetryFeedback_InjectedOnUnderFloor verifies that when
// stage-1 attempt 0 emits a monologue below the per-act floor and attempt 1
// emits an in-band monologue, attempt 1's prompt carries the structured
// `PREVIOUS ATTEMPT FAILED ... BELOW the floor of N` block while attempt 0's
// prompt does not. This is the SCP-049 dogfood bug (2026-05-05): without
// feedback the LLM repeated the under-floor miss verbatim across retries.
func TestWriter_Stage1_RetryFeedback_InjectedOnUnderFloor(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// 200 runes, 20 sentence terminals: under incident floor (288) but well
	// above the sentence-terminal floor (8). Isolates the rune-floor check.
	underFloorMono := strings.Repeat(strings.Repeat("가", 9)+".", 20)
	underFloorResp, _ := json.Marshal(map[string]any{
		"act_id":     domain.ActIncident,
		"monologue":  underFloorMono,
		"mood":       "tense",
		"key_points": []string{"first", "second"},
	})

	gen := newTwoStageFakeGen()
	gen.enqueue(stageKeyWriter, domain.ActIncident, string(underFloorResp), "")
	gen.enqueue(stageKeyWriter, domain.ActIncident, validMonologueResponse(domain.ActIncident), "")
	gen.enqueue(stageKeySegmenter, domain.ActIncident, validBeatsResponse(domain.ActIncident), "")
	queueAllValidActsExcept(gen, domain.ActIncident)

	state := freshWriterState()
	if err := newTestWriter(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state); err != nil {
		t.Fatalf("writer should succeed on retry, got: %v", err)
	}

	gen.mu.Lock()
	prompts := append([]string(nil), gen.prompts[stageKeyWriter][domain.ActIncident]...)
	gen.mu.Unlock()
	if len(prompts) != 2 {
		t.Fatalf("expected 2 stage-1 prompts for incident, got %d", len(prompts))
	}
	if strings.Contains(prompts[0], "PREVIOUS ATTEMPT FAILED") {
		t.Errorf("attempt 0 prompt unexpectedly carries retry feedback")
	}
	for _, want := range []string{
		"PREVIOUS ATTEMPT FAILED",
		"200 runes",
		"BELOW the floor of 288",
		"expand",
	} {
		if !strings.Contains(prompts[1], want) {
			t.Errorf("attempt 1 prompt missing %q", want)
		}
	}
}

// TestWriter_Stage1_RetryFeedback_InjectedOnOverCap mirrors the under-floor
// test for the symmetric over-cap branch — the unresolved act dogfood case
// where 1286 > 1120 caused retry exhaustion before this loop existed.
func TestWriter_Stage1_RetryFeedback_InjectedOnOverCap(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// 800 runes, 80 terminals: over incident cap (720), valid otherwise.
	overCapMono := strings.Repeat(strings.Repeat("가", 9)+".", 80)
	overCapResp, _ := json.Marshal(map[string]any{
		"act_id":     domain.ActIncident,
		"monologue":  overCapMono,
		"mood":       "tense",
		"key_points": []string{"first", "second"},
	})

	gen := newTwoStageFakeGen()
	gen.enqueue(stageKeyWriter, domain.ActIncident, string(overCapResp), "")
	gen.enqueue(stageKeyWriter, domain.ActIncident, validMonologueResponse(domain.ActIncident), "")
	gen.enqueue(stageKeySegmenter, domain.ActIncident, validBeatsResponse(domain.ActIncident), "")
	queueAllValidActsExcept(gen, domain.ActIncident)

	state := freshWriterState()
	if err := newTestWriter(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state); err != nil {
		t.Fatalf("writer should succeed on retry, got: %v", err)
	}

	gen.mu.Lock()
	prompts := append([]string(nil), gen.prompts[stageKeyWriter][domain.ActIncident]...)
	gen.mu.Unlock()
	if len(prompts) != 2 {
		t.Fatalf("expected 2 stage-1 prompts for incident, got %d", len(prompts))
	}
	for _, want := range []string{
		"PREVIOUS ATTEMPT FAILED",
		"800 runes",
		"OVER the cap of 720",
		"Tighten",
	} {
		if !strings.Contains(prompts[1], want) {
			t.Errorf("attempt 1 prompt missing %q", want)
		}
	}
}

// TestWriter_Stage1_RetryFeedback_NotInjectedOnEmptyMood mirrors I/O matrix
// row 4 verbatim: an in-band monologue with empty `mood` fails validation
// for a non-length reason and must NOT cause retry feedback to appear in
// the next attempt's prompt.
func TestWriter_Stage1_RetryFeedback_NotInjectedOnEmptyMood(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	emptyMoodResp, _ := json.Marshal(map[string]any{
		"act_id":     domain.ActIncident,
		"monologue":  validMonologueForAct(domain.ActIncident),
		"mood":       "",
		"key_points": []string{"first", "second"},
	})

	gen := newTwoStageFakeGen()
	gen.enqueue(stageKeyWriter, domain.ActIncident, string(emptyMoodResp), "")
	gen.enqueue(stageKeyWriter, domain.ActIncident, validMonologueResponse(domain.ActIncident), "")
	gen.enqueue(stageKeySegmenter, domain.ActIncident, validBeatsResponse(domain.ActIncident), "")
	queueAllValidActsExcept(gen, domain.ActIncident)

	state := freshWriterState()
	if err := newTestWriter(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state); err != nil {
		t.Fatalf("writer should succeed on retry, got: %v", err)
	}

	gen.mu.Lock()
	prompts := append([]string(nil), gen.prompts[stageKeyWriter][domain.ActIncident]...)
	gen.mu.Unlock()
	if len(prompts) != 2 {
		t.Fatalf("expected 2 stage-1 prompts for incident, got %d", len(prompts))
	}
	if strings.Contains(prompts[1], "PREVIOUS ATTEMPT FAILED") {
		t.Errorf("attempt 1 prompt unexpectedly carries retry feedback for an empty-mood failure\n  prompt:\n%s", prompts[1])
	}
}

// TestWriter_Stage1_RetryFeedback_NotInjectedOnNonLengthMiss verifies that a
// validation failure unrelated to rune count (here: too few sentence
// terminals) does NOT populate retry feedback — the feedback signal is
// length-specific and must not falsely claim a "previous length miss"
// when the LLM's actual error was something else.
func TestWriter_Stage1_RetryFeedback_NotInjectedOnNonLengthMiss(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// 320 runes (above incident floor 288, below cap 720) but only 5
	// sentence terminals — fails the stage-1 sentence-terminal floor (≥ 8).
	// Each clause: 63 '가' syllable runes + '.' = 64 runes; × 5 = 320 runes.
	sparseMono := strings.Repeat(strings.Repeat("가", 63)+".", 5)
	sparseResp, _ := json.Marshal(map[string]any{
		"act_id":     domain.ActIncident,
		"monologue":  sparseMono,
		"mood":       "tense",
		"key_points": []string{"first", "second"},
	})

	gen := newTwoStageFakeGen()
	gen.enqueue(stageKeyWriter, domain.ActIncident, string(sparseResp), "")
	gen.enqueue(stageKeyWriter, domain.ActIncident, validMonologueResponse(domain.ActIncident), "")
	gen.enqueue(stageKeySegmenter, domain.ActIncident, validBeatsResponse(domain.ActIncident), "")
	queueAllValidActsExcept(gen, domain.ActIncident)

	state := freshWriterState()
	if err := newTestWriter(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state); err != nil {
		t.Fatalf("writer should succeed on retry, got: %v", err)
	}

	gen.mu.Lock()
	prompts := append([]string(nil), gen.prompts[stageKeyWriter][domain.ActIncident]...)
	gen.mu.Unlock()
	if len(prompts) != 2 {
		t.Fatalf("expected 2 stage-1 prompts for incident, got %d", len(prompts))
	}
	if strings.Contains(prompts[1], "PREVIOUS ATTEMPT FAILED") {
		t.Errorf("attempt 1 prompt unexpectedly carries retry feedback for a non-length miss\n  prompt:\n%s", prompts[1])
	}
}

// TestWriter_Stage1_RetryFeedback_CoexistsWithQualityFeedback verifies I/O
// matrix row 5: when both cross-stage `{quality_feedback}` (set via
// state.PriorCriticFeedback before the writer runs) and within-stage
// `{retry_feedback}` (populated by an attempt-0 length miss) are non-empty,
// attempt 1's prompt carries both signals in their distinct sections.
func TestWriter_Stage1_RetryFeedback_CoexistsWithQualityFeedback(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	const sentinelCritic = "CRITIC_FEEDBACK_SENTINEL_XYZZY"

	underFloorMono := strings.Repeat(strings.Repeat("가", 9)+".", 20) // 200 runes, 20 terminals
	underFloorResp, _ := json.Marshal(map[string]any{
		"act_id":     domain.ActIncident,
		"monologue":  underFloorMono,
		"mood":       "tense",
		"key_points": []string{"first", "second"},
	})

	gen := newTwoStageFakeGen()
	gen.enqueue(stageKeyWriter, domain.ActIncident, string(underFloorResp), "")
	gen.enqueue(stageKeyWriter, domain.ActIncident, validMonologueResponse(domain.ActIncident), "")
	gen.enqueue(stageKeySegmenter, domain.ActIncident, validBeatsResponse(domain.ActIncident), "")
	queueAllValidActsExcept(gen, domain.ActIncident)

	state := freshWriterState()
	state.PriorCriticFeedback = sentinelCritic
	if err := newTestWriter(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state); err != nil {
		t.Fatalf("writer should succeed on retry, got: %v", err)
	}

	gen.mu.Lock()
	prompts := append([]string(nil), gen.prompts[stageKeyWriter][domain.ActIncident]...)
	gen.mu.Unlock()
	if len(prompts) != 2 {
		t.Fatalf("expected 2 stage-1 prompts for incident, got %d", len(prompts))
	}
	// Attempt 0: critic feedback populated, retry feedback empty.
	if !strings.Contains(prompts[0], sentinelCritic) {
		t.Errorf("attempt 0 prompt missing critic-feedback sentinel")
	}
	if strings.Contains(prompts[0], "PREVIOUS ATTEMPT FAILED") {
		t.Errorf("attempt 0 prompt unexpectedly carries retry feedback")
	}
	// Attempt 1: BOTH signals present.
	if !strings.Contains(prompts[1], sentinelCritic) {
		t.Errorf("attempt 1 prompt missing critic-feedback sentinel — quality_feedback should persist alongside retry_feedback")
	}
	if !strings.Contains(prompts[1], "PREVIOUS ATTEMPT FAILED") || !strings.Contains(prompts[1], "BELOW the floor of 288") {
		t.Errorf("attempt 1 prompt missing retry feedback alongside critic feedback")
	}
}

// TestWriter_Stage1_RetryFeedback_ClearedOnSubsequentNonLengthMiss verifies
// that a length-miss followed by a non-length validator failure does NOT
// leak the stale length-miss banner into the next attempt's prompt. Without
// this guard the LLM would be told "BELOW the floor" referring to two
// attempts ago even after the in-between attempt was in-band on length.
func TestWriter_Stage1_RetryFeedback_ClearedOnSubsequentNonLengthMiss(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Attempt 0: 200 runes, under floor (288). 20 terminals.
	underFloorMono := strings.Repeat(strings.Repeat("가", 9)+".", 20)
	underFloorResp, _ := json.Marshal(map[string]any{
		"act_id":     domain.ActIncident,
		"monologue":  underFloorMono,
		"mood":       "tense",
		"key_points": []string{"first", "second"},
	})
	// Attempt 1: 320 runes (in-band) but only 5 sentence terminals (fails
	// the sentence-terminal floor — non-length validator failure).
	sparseInBandMono := strings.Repeat(strings.Repeat("가", 63)+".", 5)
	sparseResp, _ := json.Marshal(map[string]any{
		"act_id":     domain.ActIncident,
		"monologue":  sparseInBandMono,
		"mood":       "tense",
		"key_points": []string{"first", "second"},
	})

	gen := newTwoStageFakeGen()
	gen.enqueue(stageKeyWriter, domain.ActIncident, string(underFloorResp), "")
	gen.enqueue(stageKeyWriter, domain.ActIncident, string(sparseResp), "")
	gen.enqueue(stageKeyWriter, domain.ActIncident, validMonologueResponse(domain.ActIncident), "")
	gen.enqueue(stageKeySegmenter, domain.ActIncident, validBeatsResponse(domain.ActIncident), "")
	queueAllValidActsExcept(gen, domain.ActIncident)

	state := freshWriterState()
	if err := newTestWriter(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state); err != nil {
		t.Fatalf("writer should succeed on third attempt, got: %v", err)
	}

	gen.mu.Lock()
	prompts := append([]string(nil), gen.prompts[stageKeyWriter][domain.ActIncident]...)
	gen.mu.Unlock()
	if len(prompts) != 3 {
		t.Fatalf("expected 3 stage-1 prompts, got %d", len(prompts))
	}
	// Attempt 1 should carry the length-miss feedback from attempt 0.
	if !strings.Contains(prompts[1], "BELOW the floor of 288") {
		t.Errorf("attempt 1 prompt missing length-miss feedback from attempt 0")
	}
	// Attempt 2 must NOT carry stale length-miss feedback — attempt 1's
	// monologue was in-band, so the length signal no longer applies.
	if strings.Contains(prompts[2], "PREVIOUS ATTEMPT FAILED") {
		t.Errorf("attempt 2 prompt unexpectedly carries stale length-miss banner from attempt 0\n  prompt:\n%s", prompts[2])
	}
}
