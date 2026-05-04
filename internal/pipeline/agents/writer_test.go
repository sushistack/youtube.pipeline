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
			"qf={quality_feedback}\nexemplar={exemplar_scenes}",
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

// validMonologueForAct returns a placeholder monologue exactly at the per-act
// rune cap floor so tests stay green even after rune-cap rescales. The string
// is composed of 8 nine-syllable Korean clauses each terminated with `.`,
// totalling 80 runes — predictable for offset math, and every 10-rune
// boundary lands on a `.` so the segmenter sentence-boundary validator
// accepts the canonical 8-beat × 10-rune fixture.
func validMonologueForAct(actID string) string {
	// Each clause is `가가가가가가가가가.` = 10 runes ending in a sentence
	// terminal. 8 clauses × 10 runes = 80 runes total.
	return strings.Repeat(strings.Repeat("가", 9)+".", 8)
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
	beats := []map[string]any{}
	for i := 0; i < 8; i++ {
		beats = append(beats, map[string]any{
			"start_offset":       i * 10,
			"end_offset":         (i + 1) * 10,
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

// --- I/O matrix: stage-2 mid-sentence cut ------------------------------

func TestWriter_Stage2_MidSentenceCut_RetriesThenFails(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Build a beats response whose end_offsets land mid-syllable (i*10 + 5)
	// — every cut lands on `가`, never on `.`. The sentence-boundary
	// validator must reject every attempt.
	mkBadCuts := func(actID string) string {
		beats := []map[string]any{}
		for i := 0; i < 8; i++ {
			start := i * 10
			end := i*10 + 5
			if i == 7 {
				end = 80
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
	gen.enqueue(stageKeyWriter, domain.ActIncident, validMonologueResponse(domain.ActIncident), "")
	bad := mkBadCuts(domain.ActIncident)
	// budget=2 → up to 3 attempts.
	gen.enqueue(stageKeySegmenter, domain.ActIncident, bad, "")
	gen.enqueue(stageKeySegmenter, domain.ActIncident, bad, "")
	gen.enqueue(stageKeySegmenter, domain.ActIncident, bad, "")

	state := freshWriterState()
	err := newTestWriter(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if err == nil {
		t.Fatal("expected mid-sentence cut failure, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("error chain must include ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "mid-sentence") {
		t.Fatalf("error should mention mid-sentence cut, got: %v", err)
	}
	if state.Narration != nil {
		t.Fatalf("state.Narration must remain unset on failure: %+v", state.Narration)
	}
	// Retry budget = 2 → 3 total attempts.
	if got := gen.callCount(stageKeySegmenter, domain.ActIncident); got != 3 {
		t.Fatalf("segmenter call count=%d, want 3 (1 + 2 retries)", got)
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
	// Both end_offsets land on sentence-terminal `.` boundaries (10, 20)
	// so the sentence-boundary validator passes; the overlap is the first
	// failure mode and the only one this fixture is intended to exercise.
	// Beats: (0,20), (10,30), (20,40), (30,50), (40,60), (50,70), (60,80), (70,80)
	// — beat[1].start_offset (10) < beat[0].end_offset (20) → overlap on beat[1].
	beats := []map[string]any{}
	for i := 0; i < 8; i++ {
		end := i*10 + 20
		if end > 80 {
			end = 80
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
