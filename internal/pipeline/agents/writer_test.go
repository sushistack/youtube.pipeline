package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// actIndexedTextGenerator routes Generate calls to per-act response queues
// based on the `[ACT:<id>]` marker the test asset injects into the rendered
// prompt. Per-act FIFO queues let tests drive both happy-path and retry
// scenarios deterministically. It also tracks max concurrent in-flight
// calls for the concurrency test.
type actIndexedTextGenerator struct {
	mu          sync.Mutex
	responses   map[string][]string
	calls       map[string]int
	prompts     map[string][]string
	finishReason map[string][]string
	model       string
	provider    string
	sleep       time.Duration
	inFlight    atomic.Int32
	maxInFlight atomic.Int32
}

func newActIndexedTextGenerator(perAct map[string][]string) *actIndexedTextGenerator {
	return &actIndexedTextGenerator{
		responses:    perAct,
		calls:        map[string]int{},
		prompts:      map[string][]string{},
		finishReason: map[string][]string{},
		model:        "writer-model",
		provider:     "openai",
	}
}

func (a *actIndexedTextGenerator) Generate(_ context.Context, req domain.TextRequest) (domain.TextResponse, error) {
	actID := extractActIDFromPrompt(req.Prompt)
	if actID == "" {
		return domain.TextResponse{}, fmt.Errorf("test gen: prompt missing [ACT:<id>] marker")
	}

	now := a.inFlight.Add(1)
	defer a.inFlight.Add(-1)
	for {
		cur := a.maxInFlight.Load()
		if now <= cur || a.maxInFlight.CompareAndSwap(cur, now) {
			break
		}
	}
	if a.sleep > 0 {
		time.Sleep(a.sleep)
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.prompts[actID] = append(a.prompts[actID], req.Prompt)
	queue, ok := a.responses[actID]
	if !ok || len(queue) == 0 {
		return domain.TextResponse{}, fmt.Errorf("test gen: no response queued for act %s", actID)
	}
	resp := queue[0]
	a.responses[actID] = queue[1:]
	a.calls[actID]++
	var finish string
	if frQueue, ok := a.finishReason[actID]; ok && len(frQueue) > 0 {
		finish = frQueue[0]
		a.finishReason[actID] = frQueue[1:]
	}
	return domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{
		Content:      resp,
		Model:        a.model,
		Provider:     a.provider,
		FinishReason: finish,
	}}, nil
}

func extractActIDFromPrompt(prompt string) string {
	const marker = "[ACT:"
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

func TestWriter_PerAct_Happy_Merges10Scenes_InOrder(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	perAct := perActFixturesFromMergedSample(t)
	gen := newActIndexedTextGenerator(perAct)
	state := sampleWriterState()
	err := newWriterForTest(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if err != nil {
		t.Fatalf("Writer: %v", err)
	}
	if state.Narration == nil {
		t.Fatal("expected narration output")
	}
	if got := len(state.Narration.Scenes); got != 10 {
		t.Fatalf("scene count = %d, want 10", got)
	}
	for i, scene := range state.Narration.Scenes {
		if scene.SceneNum != i+1 {
			t.Fatalf("scene[%d].scene_num=%d, want %d", i, scene.SceneNum, i+1)
		}
	}
	wantAct := map[int]string{
		1: domain.ActIncident, 2: domain.ActIncident,
		3: domain.ActMystery, 4: domain.ActMystery, 5: domain.ActMystery,
		6: domain.ActRevelation, 7: domain.ActRevelation, 8: domain.ActRevelation,
		9: domain.ActUnresolved, 10: domain.ActUnresolved,
	}
	for _, scene := range state.Narration.Scenes {
		if scene.ActID != wantAct[scene.SceneNum] {
			t.Fatalf("scene_num=%d act_id=%q want=%q", scene.SceneNum, scene.ActID, wantAct[scene.SceneNum])
		}
	}
	// Each act called exactly once on the happy path.
	for _, id := range []string{domain.ActIncident, domain.ActMystery, domain.ActRevelation, domain.ActUnresolved} {
		if gen.calls[id] != 1 {
			t.Fatalf("act %s call count = %d, want 1", id, gen.calls[id])
		}
	}
}

func TestWriter_PerAct_RetriesOnlyFailedAct(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	perAct := perActFixturesFromMergedSample(t)
	// Inject a schema-violating first response for Act 2 (mystery): wrong scene count (2, want 3).
	bad := mustEncodeActResponse(t, domain.ActMystery, []domain.NarrationScene{
		fillRequiredSceneFields(domain.NarrationScene{SceneNum: 3, ActID: domain.ActMystery, Narration: "broken"}),
		fillRequiredSceneFields(domain.NarrationScene{SceneNum: 4, ActID: domain.ActMystery, Narration: "broken"}),
	})
	perAct[domain.ActMystery] = append([]string{bad}, perAct[domain.ActMystery]...)

	gen := newActIndexedTextGenerator(perAct)
	state := sampleWriterState()
	err := newWriterForTest(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if err != nil {
		t.Fatalf("Writer: %v", err)
	}
	if gen.calls[domain.ActMystery] != 2 {
		t.Fatalf("mystery call count = %d, want 2 (bad + retry)", gen.calls[domain.ActMystery])
	}
	for _, id := range []string{domain.ActIncident, domain.ActRevelation, domain.ActUnresolved} {
		if gen.calls[id] != 1 {
			t.Fatalf("act %s call count = %d, want 1", id, gen.calls[id])
		}
	}
}

func TestWriter_PerAct_PriorActSummaryInjected(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	perAct := perActFixturesFromMergedSample(t)
	gen := newActIndexedTextGenerator(perAct)
	state := sampleWriterState()
	err := newWriterForTest(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if err != nil {
		t.Fatalf("Writer: %v", err)
	}
	// Pull the sample's Act 1 last scene narration and assert it appears in
	// every Acts 2/3/4 prompt.
	merged := loadMergedSample(t)
	var act1Last domain.NarrationScene
	for _, scene := range merged.Scenes {
		if scene.ActID == domain.ActIncident {
			act1Last = scene
		}
	}
	if act1Last.Narration == "" {
		t.Fatal("sample fixture has no incident scene; cannot test prior-summary injection")
	}
	for _, id := range []string{domain.ActMystery, domain.ActRevelation, domain.ActUnresolved} {
		prompts := gen.prompts[id]
		if len(prompts) == 0 {
			t.Fatalf("no prompt captured for act %s", id)
		}
		if !strings.Contains(prompts[0], strings.TrimSpace(act1Last.Narration)) {
			t.Fatalf("act %s prompt missing prior-act summary; want substring %q", id, act1Last.Narration)
		}
	}
	// Act 1 prompt should NOT contain the prior-summary phrase.
	if got := gen.prompts[domain.ActIncident]; len(got) > 0 && strings.Contains(got[0], "Previous act ended") {
		t.Fatalf("act 1 prompt unexpectedly contains prior-summary phrase: %s", got[0])
	}
}

// TestWriter_PerAct_ExemplarInjectedPerAct asserts the per-act inject
// guarantee from the spec's Code Map: each act's prompt contains the
// exemplar narration for ITS OWN act and none of the others. Mechanism
// is constructionally per-act (single map lookup), but pin it explicitly
// so a future refactor that accidentally concatenates all acts breaks the
// build.
func TestWriter_PerAct_ExemplarInjectedPerAct(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	perAct := perActFixturesFromMergedSample(t)
	gen := newActIndexedTextGenerator(perAct)
	state := sampleWriterState()
	err := newWriterForTest(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if err != nil {
		t.Fatalf("Writer: %v", err)
	}
	wantStubs := map[string]string{
		domain.ActIncident:   "[exemplar incident stub]",
		domain.ActMystery:    "[exemplar mystery stub]",
		domain.ActRevelation: "[exemplar revelation stub]",
		domain.ActUnresolved: "[exemplar unresolved stub]",
	}
	for actID, ownStub := range wantStubs {
		prompts := gen.prompts[actID]
		if len(prompts) == 0 {
			t.Fatalf("no prompt captured for act %s", actID)
		}
		if !strings.Contains(prompts[0], ownStub) {
			t.Fatalf("act %s prompt missing own exemplar stub %q", actID, ownStub)
		}
		for otherActID, otherStub := range wantStubs {
			if otherActID == actID {
				continue
			}
			if strings.Contains(prompts[0], otherStub) {
				t.Fatalf("act %s prompt leaked other-act exemplar %q (per-act inject violated)", actID, otherStub)
			}
		}
	}
}

func TestWriter_PerAct_Concurrency(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	perAct := perActFixturesFromMergedSample(t)
	gen := newActIndexedTextGenerator(perAct)
	gen.sleep = 30 * time.Millisecond

	cfg := sampleWriterConfig()
	cfg.Concurrency = 2
	state := sampleWriterState()
	writer := NewWriter(gen, cfg, sampleWriterAssets(), mustValidator(t, "writer_output.schema.json"), mustTerms(t))
	if err := writer(context.Background(), state); err != nil {
		t.Fatalf("Writer: %v", err)
	}
	if got := gen.maxInFlight.Load(); got > 2 {
		t.Fatalf("max in-flight = %d, want <= 2 (cfg.Concurrency=2)", got)
	}
	// Acts 2/3/4 are fan-out, so we should have observed >=2 to confirm they truly ran in parallel.
	if got := gen.maxInFlight.Load(); got < 2 {
		t.Fatalf("max in-flight = %d; expected acts 2/3/4 to overlap (>=2)", got)
	}
}

func TestWriter_PerAct_DefaultConcurrency(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	perAct := perActFixturesFromMergedSample(t)
	gen := newActIndexedTextGenerator(perAct)
	gen.sleep = 30 * time.Millisecond

	cfg := sampleWriterConfig() // Concurrency unset → falls back to writerDefaultConcurrency=2.
	state := sampleWriterState()
	writer := NewWriter(gen, cfg, sampleWriterAssets(), mustValidator(t, "writer_output.schema.json"), mustTerms(t))
	if err := writer(context.Background(), state); err != nil {
		t.Fatalf("Writer: %v", err)
	}
	if got := gen.maxInFlight.Load(); got > writerDefaultConcurrency {
		t.Fatalf("max in-flight = %d, want <= %d (default fallback)", got, writerDefaultConcurrency)
	}
}

func TestWriter_PerAct_ContextCanceledMidFanout(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	perAct := perActFixturesFromMergedSample(t)
	gen := newActIndexedTextGenerator(perAct)
	gen.sleep = 80 * time.Millisecond // give the canceller time to fire while Acts 2/3/4 are mid-flight

	state := sampleWriterState()
	sentinel := &domain.NarrationScript{SCPID: "SENTINEL"}
	state.Narration = sentinel

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond) // after Act 1 completes, before Acts 2/3/4 finish
		cancel()
	}()
	defer cancel()

	writer := NewWriter(gen, sampleWriterConfig(), sampleWriterAssets(), mustValidator(t, "writer_output.schema.json"), mustTerms(t))
	err := writer(ctx, state)
	if err == nil {
		t.Fatal("expected ctx cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if state.Narration != sentinel {
		t.Fatalf("state.Narration mutated on ctx cancel: %#v", state.Narration)
	}
}

func TestWriter_Run_Happy(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	perAct := perActFixturesFromMergedSample(t)
	gen := newActIndexedTextGenerator(perAct)
	state := sampleWriterState()
	err := newWriterForTest(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if err != nil {
		t.Fatalf("Writer: %v", err)
	}
	if state.Narration == nil {
		t.Fatal("expected narration output")
	}
	totalCalls := 0
	for _, n := range gen.calls {
		totalCalls += n
	}
	testutil.AssertEqual(t, totalCalls, 4)
}

func TestWriter_Run_StripsCodeFence(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	perAct := perActFixturesFromMergedSample(t)
	for id, list := range perAct {
		for i, raw := range list {
			perAct[id][i] = "```json\n" + strings.TrimSpace(raw) + "\n```"
		}
	}
	gen := newActIndexedTextGenerator(perAct)
	state := sampleWriterState()
	err := newWriterForTest(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if err != nil {
		t.Fatalf("Writer: %v", err)
	}
}

func TestWriter_Run_InvalidJSON(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	perAct := perActFixturesFromMergedSample(t)
	// Force invalid JSON for Act 1; retry budget is 1 so two bad responses
	// exhaust the budget and the writer fails.
	perAct[domain.ActIncident] = []string{"not-json", "still-not-json"}
	gen := newActIndexedTextGenerator(perAct)
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

	perAct := perActFixturesFromMergedSample(t)
	// Act 1 returns wrong scene count both attempts → exhausts retry budget.
	bad := mustEncodeActResponse(t, domain.ActIncident, []domain.NarrationScene{
		fillRequiredSceneFields(domain.NarrationScene{SceneNum: 1, ActID: domain.ActIncident, Narration: "x"}),
	})
	perAct[domain.ActIncident] = []string{bad, bad}
	gen := newActIndexedTextGenerator(perAct)
	state := sampleWriterState()
	err := newWriterForTest(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	// Act 1 hard failure must short-circuit before Acts 2/3/4 are launched
	// (spec I/O matrix: "Acts 2–4 not launched").
	for _, id := range []string{domain.ActMystery, domain.ActRevelation, domain.ActUnresolved} {
		if gen.calls[id] != 0 {
			t.Fatalf("act %s called %d times after Act 1 failure; want 0", id, gen.calls[id])
		}
	}
}

// TestWriter_Run_NarrationPerActCap exercises the per-act narration rune
// cap lookup (domain.ActNarrationRuneCap) inside validateWriterActResponse.
// At-cap narration must pass; cap+1 must fail with ErrValidation. Each act
// is asserted independently because the caps differ
// (incident=100, mystery=220, revelation=320, unresolved=180).
func TestWriter_Run_NarrationPerActCap(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	cases := []struct {
		actID    string
		sceneIdx int // index into the merged sample (scene_num - 1)
	}{
		{domain.ActIncident, 0},   // scene 1
		{domain.ActMystery, 2},    // scene 3
		{domain.ActRevelation, 5}, // scene 6
		{domain.ActUnresolved, 8}, // scene 9
	}
	for _, tc := range cases {
		tc := tc
		cap := domain.ActNarrationRuneCap[tc.actID]
		t.Run(tc.actID+"_at_cap_passes", func(t *testing.T) {
			testutil.BlockExternalHTTP(t)
			merged := loadMergedSample(t)
			merged.Scenes[tc.sceneIdx].Narration = strings.Repeat("가", cap)
			gen := newActIndexedTextGenerator(splitMergedByAct(t, merged))
			state := sampleWriterState()
			if err := newWriterForTest(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state); err != nil {
				t.Fatalf("writer rejected %s narration at cap=%d: %v", tc.actID, cap, err)
			}
		})
		t.Run(tc.actID+"_over_cap_rejected", func(t *testing.T) {
			testutil.BlockExternalHTTP(t)
			merged := loadMergedSample(t)
			merged.Scenes[tc.sceneIdx].Narration = strings.Repeat("가", cap+1)
			perAct := splitMergedByAct(t, merged)
			// Re-encode the offending act with both attempts identical so the retry
			// budget is exhausted on the same broken shape.
			perAct[tc.actID] = []string{perAct[tc.actID][0], perAct[tc.actID][0]}
			gen := newActIndexedTextGenerator(perAct)
			state := sampleWriterState()
			err := newWriterForTest(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
			if !errors.Is(err, domain.ErrValidation) {
				t.Fatalf("expected ErrValidation for %s narration over cap=%d, got %v", tc.actID, cap, err)
			}
			if !strings.Contains(err.Error(), "exceeds cap") {
				t.Fatalf("expected exceeds-cap error, got %v", err)
			}
			if state.Narration != nil {
				t.Fatalf("state mutated on cap violation: %+v", state.Narration)
			}
		})
	}
}

func TestWriter_Run_ForbiddenTermsRejected(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	merged := loadMergedSample(t)
	merged.Scenes[0].Narration = "이건 wiki 문체입니다."
	perAct := splitMergedByAct(t, merged)
	gen := newActIndexedTextGenerator(perAct)
	state := sampleWriterState()
	err := newWriterForTest(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if state.Narration != nil {
		t.Fatalf("state mutated on forbidden terms: %+v", state.Narration)
	}
}

func TestWriter_Run_MetadataFilled(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	perAct := perActFixturesFromMergedSample(t)
	gen := newActIndexedTextGenerator(perAct)
	state := sampleWriterState()
	err := newWriterForTest(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if err != nil {
		t.Fatalf("Writer: %v", err)
	}
	testutil.AssertEqual(t, state.Narration.Metadata.WriterModel, "writer-model")
	testutil.AssertEqual(t, state.Narration.Metadata.WriterProvider, "openai")
	testutil.AssertEqual(t, state.Narration.Metadata.SceneCount, len(state.Narration.Scenes))
}

// TestWriter_PerAct_FailsFastOnTruncatedFinishReason verifies the
// finish_reason="length" early-fail guard ported from the dogfood-era
// single-call writer. When the provider truncates the response, decoding
// the half-written JSON would just produce a generic parse error and
// burn the per-act retry budget on the same broken shape — better to
// surface the truncation directly.
func TestWriter_PerAct_FailsFastOnTruncatedFinishReason(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	perAct := perActFixturesFromMergedSample(t)
	gen := newActIndexedTextGenerator(perAct)
	// Mark Act 1's response as truncated. The act's actual JSON is fine
	// but the finish_reason guard must trip before decode.
	gen.finishReason[domain.ActIncident] = []string{"length"}

	state := sampleWriterState()
	err := newWriterForTest(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "provider truncated completion") {
		t.Fatalf("expected truncation error, got %v", err)
	}
	if gen.calls[domain.ActIncident] != 1 {
		t.Fatalf("act incident call count = %d, want 1 (no retry on truncation)", gen.calls[domain.ActIncident])
	}
	if state.Narration != nil {
		t.Fatalf("state mutated on truncation: %+v", state.Narration)
	}
}

// TestWriter_PerAct_PriorCriticFeedbackInjected verifies the wiring
// from state.PriorCriticFeedback into the {quality_feedback} prompt
// placeholder. Same value is injected into every act's prompt today
// (per spec §4 Critic feedback).
func TestWriter_PerAct_PriorCriticFeedbackInjected(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	perAct := perActFixturesFromMergedSample(t)
	gen := newActIndexedTextGenerator(perAct)
	state := sampleWriterState()
	state.PriorCriticFeedback = "Q-FEEDBACK-MARKER: avoid generic narration"

	err := newWriterForTest(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if err != nil {
		t.Fatalf("Writer: %v", err)
	}
	for _, id := range []string{domain.ActIncident, domain.ActMystery, domain.ActRevelation, domain.ActUnresolved} {
		prompts := gen.prompts[id]
		if len(prompts) == 0 {
			t.Fatalf("no prompt captured for act %s", id)
		}
		if !strings.Contains(prompts[0], state.PriorCriticFeedback) {
			t.Fatalf("act %s prompt missing prior critic feedback marker", id)
		}
	}
}

// TestWriter_Run_PropagatesNarrationBeats pins the writer→narration
// beats wiring: the LLM response's `narration_beats` per scene must land
// on every NarrationScene.NarrationBeats slice (1:1, in order).
// Visual_breakdowner depends on this for its 1-shot-per-beat contract;
// dropping the wiring would silently make every scene incident-shaped.
func TestWriter_Run_PropagatesNarrationBeats(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	perAct := perActFixturesFromMergedSample(t)
	gen := newActIndexedTextGenerator(perAct)
	state := sampleWriterState()
	err := newWriterForTest(gen, mustValidator(t, "writer_output.schema.json"), mustTerms(t))(context.Background(), state)
	if err != nil {
		t.Fatalf("Writer: %v", err)
	}
	if state.Narration == nil {
		t.Fatal("expected narration output")
	}
	for _, scene := range state.Narration.Scenes {
		if len(scene.NarrationBeats) < 1 {
			t.Fatalf("scene_num=%d: NarrationBeats empty (writer must emit ≥1 beat per scene)", scene.SceneNum)
		}
		for i, beat := range scene.NarrationBeats {
			if strings.TrimSpace(beat) == "" {
				t.Fatalf("scene_num=%d beat[%d]: blank string", scene.SceneNum, i)
			}
		}
	}
}

func TestWriter_Run_DoesNotMutateStateOnFailure(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	perAct := perActFixturesFromMergedSample(t)
	perAct[domain.ActIncident] = []string{"not-json", "still-not-json"}
	gen := newActIndexedTextGenerator(perAct)
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

// fakeTextGenerator is retained for legacy shape-mismatch tests where act
// routing isn't relevant (e.g. nil-state guards). The resps/reqs slices
// support tests that need to drive multi-call sequences with distinct
// responses; resp is the fallback for single-shot callers.
//
// Mutex gates Generate() so the visual_breakdowner errgroup fan-out does
// not race on `calls`/`last`/`reqs`. Direct field reads from the test
// goroutine remain valid only AFTER the agent's Run returns (no
// concurrent writers at that point).
type fakeTextGenerator struct {
	mu    sync.Mutex
	resp  domain.TextResponse
	resps []domain.TextResponse
	err   error
	calls int
	last  domain.TextRequest
	reqs  []domain.TextRequest
}

func (f *fakeTextGenerator) Generate(_ context.Context, req domain.TextRequest) (domain.TextResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.last = req
	f.reqs = append(f.reqs, req)
	if len(f.resps) > 0 {
		idx := f.calls - 1
		if idx >= len(f.resps) {
			idx = len(f.resps) - 1
		}
		return f.resps[idx], f.err
	}
	return f.resp, f.err
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

// sampleWriterAssets is the per-act test template. The `[ACT:{act_id}]`
// marker is what actIndexedTextGenerator uses to route per-act responses.
// {prior_act_summary} is included so the prior-summary injection test can
// assert on its presence in Acts 2/3/4 prompts. {exemplar_scenes} is
// populated from a per-act stub so renderWriterActPrompt's exemplar guard
// passes — assets_test.go covers the real-file load path separately.
func sampleWriterAssets() PromptAssets {
	return PromptAssets{
		WriterTemplate:          "[ACT:{act_id}]\nWrite {scp_id}\nrange={scene_num_range} budget={scene_budget}\nsynopsis={act_synopsis}\nkey_points={act_key_points}\nprior={prior_act_summary}\n{scp_visual_reference}\n{format_guide}\n{forbidden_terms_section}\n{glossary_section}\n{quality_feedback}\nexemplar={exemplar_scenes}",
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
		// SceneBudget aligns with testdata/contracts/writer_output.sample.json
		// scene→act distribution (2/3/3/2). Writer per-act validator demands
		// exact match, so the fixtures must agree.
		Acts: []domain.Act{
			{ID: domain.ActIncident, Name: "Act 1", Synopsis: "Incident", SceneBudget: 2, DurationRatio: 0.15, DramaticBeatIDs: []int{0, 4}, KeyPoints: []string{"Beat 0"}, Role: domain.RoleHook},
			{ID: domain.ActMystery, Name: "Act 2", Synopsis: "Mystery", SceneBudget: 3, DurationRatio: 0.30, DramaticBeatIDs: []int{1}, KeyPoints: []string{"Beat 1"}, Role: domain.RoleTension},
			{ID: domain.ActRevelation, Name: "Act 3", Synopsis: "Revelation", SceneBudget: 3, DurationRatio: 0.40, DramaticBeatIDs: []int{2}, KeyPoints: []string{"Beat 2"}, Role: domain.RoleReveal},
			{ID: domain.ActUnresolved, Name: "Act 4", Synopsis: "Unresolved", SceneBudget: 2, DurationRatio: 0.15, DramaticBeatIDs: []int{3}, KeyPoints: []string{"Beat 3"}, Role: domain.RoleBridge},
		},
		TargetSceneCount: 10,
		SourceVersion:    domain.SourceVersionV1,
	}
}

func newWriterForTest(gen domain.TextGenerator, validator *Validator, terms *ForbiddenTerms) AgentFunc {
	return NewWriter(gen, sampleWriterConfig(), sampleWriterAssets(), validator, terms)
}

// loadMergedSample reads the canonical 10-scene sample as a NarrationScript.
func loadMergedSample(t *testing.T) domain.NarrationScript {
	t.Helper()
	var script domain.NarrationScript
	if err := json.Unmarshal(testutil.LoadFixture(t, filepath.Join("contracts", "writer_output.sample.json")), &script); err != nil {
		t.Fatalf("unmarshal sample: %v", err)
	}
	return script
}

// splitMergedByAct groups the merged sample's scenes by act_id and encodes
// one writerActResponse JSON per act, in domain.ActOrder.
func splitMergedByAct(t *testing.T, script domain.NarrationScript) map[string][]string {
	t.Helper()
	byAct := map[string][]domain.NarrationScene{}
	for _, scene := range script.Scenes {
		byAct[scene.ActID] = append(byAct[scene.ActID], scene)
	}
	out := map[string][]string{}
	for _, id := range domain.ActOrder {
		out[id] = []string{mustEncodeActResponse(t, id, byAct[id])}
	}
	return out
}

func perActFixturesFromMergedSample(t *testing.T) map[string][]string {
	t.Helper()
	return splitMergedByAct(t, loadMergedSample(t))
}

func mustEncodeActResponse(t *testing.T, actID string, scenes []domain.NarrationScene) string {
	t.Helper()
	resp := writerActResponse{ActID: actID, Scenes: scenes}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("encode act response: %v", err)
	}
	return string(raw)
}

// fillRequiredSceneFields ensures every required NarrationScene field is
// non-empty so tests that intentionally violate ONE rule (e.g. wrong scene
// count) don't accidentally trip a different validator on the way through.
func fillRequiredSceneFields(scene domain.NarrationScene) domain.NarrationScene {
	if scene.Narration == "" {
		scene.Narration = "filler"
	}
	if scene.Mood == "" {
		scene.Mood = "tense"
	}
	if scene.Location == "" {
		scene.Location = "Site-19"
	}
	if scene.CharactersPresent == nil {
		scene.CharactersPresent = []string{"SCP-TEST"}
	}
	if scene.ColorPalette == "" {
		scene.ColorPalette = "gray"
	}
	if scene.Atmosphere == "" {
		scene.Atmosphere = "tense"
	}
	if scene.FactTags == nil {
		scene.FactTags = []domain.FactTag{}
	}
	return scene
}
