package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// fakeBeatDurationEstimator returns a per-beat duration override map keyed
// on the beat's StartOffset, with a fallback for beats not in the map. The
// v2 estimator is invoked once per beat anchor; tests that need per-beat
// duration variance pre-populate values keyed on the BeatAnchor.StartOffset
// they emit (offsets are stable across response generation).
type fakeBeatDurationEstimator struct {
	values   map[int]float64
	fallback float64
}

func (f fakeBeatDurationEstimator) Estimate(_ string, _ string, anchor domain.BeatAnchor) float64 {
	if v, ok := f.values[anchor.StartOffset]; ok {
		return v
	}
	if f.fallback > 0 {
		return f.fallback
	}
	return 7.0
}

func uniformEstimator(duration float64) fakeBeatDurationEstimator {
	return fakeBeatDurationEstimator{fallback: duration}
}

// shotResp builds one v2 visual_breakdowner shot JSON object that echoes a
// source BeatAnchor verbatim into its narration_anchor block. Keeping the
// helper isolated from the test bodies prevents drift between the response
// shape the LLM is contracted to emit and what the validator demands.
func shotResp(descriptor, transition string, anchor domain.BeatAnchor) string {
	raw, err := json.Marshal(visualBreakdownActResponseShot{
		VisualDescriptor: descriptor,
		Transition:       transition,
		NarrationAnchor:  anchor,
	})
	if err != nil {
		panic(fmt.Sprintf("shotResp marshal: %v", err))
	}
	return string(raw)
}

// actResp wraps an act_id + per-beat shotResp slice into the v2
// per-act response envelope visual_breakdowner consumes.
func actResp(actID string, shots ...string) string {
	return fmt.Sprintf(`{"act_id":%q,"shots":[%s]}`, actID, strings.Join(shots, ","))
}

// validActResponses generates one valid response per act in the supplied
// NarrationScript. Each shot's narration_anchor mirrors its source BeatAnchor
// byte-for-byte, so the anchor-equality validator passes.
func validActResponses(t *testing.T, script *domain.NarrationScript) map[string]string {
	t.Helper()
	out := make(map[string]string, len(script.Acts))
	for _, act := range script.Acts {
		shots := make([]string, len(act.Beats))
		for i, beat := range act.Beats {
			shots[i] = shotResp(
				fmt.Sprintf("%s shot %d descriptor", act.ActID, i+1),
				domain.TransitionKenBurns,
				beat,
			)
		}
		out[act.ActID] = actResp(act.ActID, shots...)
	}
	return out
}

func TestVisualBreakdowner_Run_Happy(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := sampleVisualBreakdownState4Acts()
	gen := &actQueueTextGenerator{responsesByAct: enqueueOnce(validActResponses(t, state.Narration))}
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if err != nil {
		t.Fatalf("VisualBreakdowner: %v", err)
	}
	if state.VisualScript == nil {
		t.Fatal("expected visual script output")
	}
	testutil.AssertEqual(t, state.VisualScript.SourceVersion, domain.VisualBreakdownSourceVersionV2)
	testutil.AssertEqual(t, state.VisualScript.Metadata.PromptTemplate, "03_5_visual_breakdown.md")
	testutil.AssertEqual(t, state.VisualScript.Metadata.ShotFormulaVersion, domain.ShotFormulaVersionV1)
	testutil.AssertEqual(t, state.VisualScript.Metadata.VisualBreakdownModel, sampleWriterConfig().Model)
	testutil.AssertEqual(t, state.VisualScript.Metadata.VisualBreakdownProvider, sampleWriterConfig().Provider)
	testutil.AssertEqual(t, len(state.VisualScript.Acts), 4)
}

func TestVisualBreakdowner_Run_MetadataFromConfigNotLastResponse(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := sampleVisualBreakdownState4Acts()
	drifting := &actDriftingTextGenerator{responsesByAct: validActResponses(t, state.Narration)}
	err := NewVisualBreakdowner(drifting, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if err != nil {
		t.Fatalf("VisualBreakdowner: %v", err)
	}
	testutil.AssertEqual(t, state.VisualScript.Metadata.VisualBreakdownModel, sampleWriterConfig().Model)
	testutil.AssertEqual(t, state.VisualScript.Metadata.VisualBreakdownProvider, sampleWriterConfig().Provider)
}

func TestVisualBreakdowner_Run_CallsGeneratorPerAct(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := sampleVisualBreakdownState4Acts()
	gen := &actQueueTextGenerator{responsesByAct: enqueueOnce(validActResponses(t, state.Narration))}
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if err != nil {
		t.Fatalf("VisualBreakdowner: %v", err)
	}
	// Per-act fan-out: 4 acts → exactly 4 generator calls.
	testutil.AssertEqual(t, gen.totalCalls(), 4)
}

// TestVisualBreakdowner_Run_ShotCountMatchesNarrationBeats was Skip-flagged in
// D1; reintroduced here against v2's 1:1 beat→shot invariant. Each act's
// emitted Shots length MUST equal the source act's Beats length (anchor
// equality enforces ordering and field equality on top of that).
func TestVisualBreakdowner_Run_ShotCountMatchesNarrationBeats(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := sampleVisualBreakdownState4Acts()
	gen := &actQueueTextGenerator{responsesByAct: enqueueOnce(validActResponses(t, state.Narration))}
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if err != nil {
		t.Fatalf("VisualBreakdowner: %v", err)
	}
	for i, act := range state.VisualScript.Acts {
		srcBeats := state.Narration.Acts[i].Beats
		if len(act.Shots) != len(srcBeats) {
			t.Fatalf("act %s: shot count=%d, want=%d (== len(BeatAnchors))", act.ActID, len(act.Shots), len(srcBeats))
		}
		for j, shot := range act.Shots {
			if !beatAnchorEqual(shot.NarrationAnchor, srcBeats[j]) {
				t.Fatalf("act %s shot %d: narration_anchor diverged from source beat", act.ActID, j+1)
			}
		}
	}
}

// TestVisualBreakdowner_Run_PassesDescriptorsThroughVerbatim was Skip-flagged
// in D1; reintroduced here against v2. The agent must NOT prepend the frozen
// descriptor onto each shot's visual_descriptor — the image-prompt builder
// (image_track.ComposeImagePrompt) is the canonical anchor site, and a
// double-prepend at the agent layer would corrupt that contract.
func TestVisualBreakdowner_Run_PassesDescriptorsThroughVerbatim(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := sampleVisualBreakdownState4Acts()
	gen := &actQueueTextGenerator{responsesByAct: enqueueOnce(validActResponses(t, state.Narration))}
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if err != nil {
		t.Fatalf("VisualBreakdowner: %v", err)
	}
	frozen := state.VisualScript.FrozenDescriptor
	totalShots := 0
	for _, act := range state.VisualScript.Acts {
		for _, shot := range act.Shots {
			totalShots++
			if frozen != "" && strings.HasPrefix(shot.VisualDescriptor, frozen) {
				t.Fatalf("act %s shot %d unexpectedly prepended with frozen descriptor: %q", act.ActID, shot.ShotIndex, shot.VisualDescriptor)
			}
		}
	}
	// 4 acts × 8 beats = 32 total shots (sample fixture).
	if totalShots != 32 {
		t.Fatalf("expected 32 total shots across 4 acts × 8 beats, got %d", totalShots)
	}
}

func TestVisualBreakdowner_Run_RejectsWrongShotCount(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := sampleVisualBreakdownState4Acts()
	// Build responses where act `incident` returns only 1 shot — anchor on the
	// first source beat — instead of the required 8. Validator rejects with
	// ErrValidation; retry exhausts because the bad response repeats.
	bad := actResp(domain.ActIncident, shotResp(
		"only one",
		domain.TransitionKenBurns,
		state.Narration.Acts[0].Beats[0],
	))
	queue := enqueueOnce(validActResponses(t, state.Narration))
	queue[domain.ActIncident] = []string{bad, bad}
	gen := &actQueueTextGenerator{responsesByAct: queue}
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestVisualBreakdowner_Run_RejectsInvalidTransition(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := sampleVisualBreakdownState4Acts()
	// Build per-act responses where the first act has an invalid transition
	// across both attempts so the retry budget is consumed and the run fails.
	shots := make([]string, len(state.Narration.Acts[0].Beats))
	for i, beat := range state.Narration.Acts[0].Beats {
		shots[i] = shotResp(fmt.Sprintf("shot %d", i+1), "zoom", beat)
	}
	bad := actResp(domain.ActIncident, shots...)
	queue := enqueueOnce(validActResponses(t, state.Narration))
	queue[domain.ActIncident] = []string{bad, bad}
	gen := &actQueueTextGenerator{responsesByAct: queue}
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

// TestVisualBreakdowner_Run_RejectsAnchorMismatch covers the load-bearing
// anchor-equality invariant: if any shot's narration_anchor diverges from
// its source BeatAnchor (offset / mood / etc.) the validator rejects with
// retryable ErrValidation. Two consecutive bad responses → retry exhaustion.
func TestVisualBreakdowner_Run_RejectsAnchorMismatch(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := sampleVisualBreakdownState4Acts()
	srcBeats := state.Narration.Acts[0].Beats
	shots := make([]string, len(srcBeats))
	for i, beat := range srcBeats {
		drifted := beat
		if i == 0 {
			// Drift the first shot's mood — anchor-equality validator rejects.
			drifted.Mood = "drifted-mood"
		}
		shots[i] = shotResp(fmt.Sprintf("shot %d", i+1), domain.TransitionKenBurns, drifted)
	}
	bad := actResp(domain.ActIncident, shots...)
	queue := enqueueOnce(validActResponses(t, state.Narration))
	queue[domain.ActIncident] = []string{bad, bad}
	gen := &actQueueTextGenerator{responsesByAct: queue}
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation on anchor mismatch, got %v", err)
	}
}

// TestVisualBreakdowner_Run_RetriesOnEmptyTransition exercises the cycle-C
// retry policy: a single bad-then-good attempt sequence on one act recovers
// without aborting the whole stage.
func TestVisualBreakdowner_Run_RetriesOnEmptyTransition(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := sampleVisualBreakdownState4Acts()
	srcBeats := state.Narration.Acts[0].Beats
	badShots := make([]string, len(srcBeats))
	for i, beat := range srcBeats {
		badShots[i] = shotResp(fmt.Sprintf("shot %d", i+1), "", beat)
	}
	bad := actResp(domain.ActIncident, badShots...)
	queue := enqueueOnce(validActResponses(t, state.Narration))
	queue[domain.ActIncident] = []string{bad, queue[domain.ActIncident][0]}
	gen := &actQueueTextGenerator{responsesByAct: queue}
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if err != nil {
		t.Fatalf("VisualBreakdowner: %v", err)
	}
	testutil.AssertEqual(t, gen.callsByAct[domain.ActIncident], 2)
	for _, actID := range []string{domain.ActMystery, domain.ActRevelation, domain.ActUnresolved} {
		testutil.AssertEqual(t, gen.callsByAct[actID], 1)
	}
	if state.VisualScript == nil || len(state.VisualScript.Acts) != 4 {
		t.Fatalf("expected 4 acts built from retry, got %#v", state.VisualScript)
	}
	testutil.AssertEqual(t, state.VisualScript.Acts[0].Shots[0].Transition, domain.TransitionKenBurns)
}

// TestVisualBreakdowner_Run_PropagatesTransportErrorWithoutRetry pins cycle-C
// transport-error policy: a non-ErrStageFailed error from the generator
// short-circuits without consuming the retry budget.
func TestVisualBreakdowner_Run_PropagatesTransportErrorWithoutRetry(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := sampleVisualBreakdownState4Acts()
	transportErr := errors.New("network: connection reset")
	valid := validActResponses(t, state.Narration)
	gen := &actErrorTextGenerator{
		errOnAct:       domain.ActIncident,
		err:            transportErr,
		validResponses: valid,
	}
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if err == nil {
		t.Fatal("expected transport error to propagate")
	}
	if !errors.Is(err, transportErr) {
		t.Fatalf("expected transport error to propagate verbatim, got %v", err)
	}
	testutil.AssertEqual(t, gen.callsByAct[domain.ActIncident], 1)
	if state.VisualScript != nil {
		t.Fatalf("expected state.VisualScript unchanged on transport failure, got %#v", state.VisualScript)
	}
}

// TestVisualBreakdowner_Run_RetriesEmptyContentAsErrStageFailed pins the
// cycle-C empty-content fix: provider transient ErrStageFailed signals are
// retryable per-act (a single empty-content blip recovers on retry).
func TestVisualBreakdowner_Run_RetriesEmptyContentAsErrStageFailed(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := sampleVisualBreakdownState4Acts()
	valid := validActResponses(t, state.Narration)
	gen := &actErrorThenSuccessGenerator{
		errOnAct:       domain.ActIncident,
		err:            domain.ErrStageFailed,
		successAfter:   1,
		validResponses: valid,
	}
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if err != nil {
		t.Fatalf("VisualBreakdowner: %v", err)
	}
	testutil.AssertEqual(t, gen.callsByAct[domain.ActIncident], 2)
}

// TestVisualBreakdowner_Run_HandlesMultipleConcurrentRetries proves the
// errgroup fan-out doesn't serialize retries across acts: incident AND
// revelation each fail-then-recover concurrently, final ordering preserved.
func TestVisualBreakdowner_Run_HandlesMultipleConcurrentRetries(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := sampleVisualBreakdownState4Acts()
	valid := validActResponses(t, state.Narration)

	badIncident := actResp(domain.ActIncident, makeBadShots(state.Narration.Acts[0].Beats, "")...)
	badRevelation := actResp(domain.ActRevelation, makeBadShots(state.Narration.Acts[2].Beats, "zoom")...)

	queue := map[string][]string{
		domain.ActIncident:   {badIncident, valid[domain.ActIncident]},
		domain.ActMystery:    {valid[domain.ActMystery]},
		domain.ActRevelation: {badRevelation, valid[domain.ActRevelation]},
		domain.ActUnresolved: {valid[domain.ActUnresolved]},
	}
	gen := &actQueueTextGenerator{responsesByAct: queue}
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if err != nil {
		t.Fatalf("VisualBreakdowner: %v", err)
	}
	testutil.AssertEqual(t, gen.callsByAct[domain.ActIncident], 2)
	testutil.AssertEqual(t, gen.callsByAct[domain.ActMystery], 1)
	testutil.AssertEqual(t, gen.callsByAct[domain.ActRevelation], 2)
	testutil.AssertEqual(t, gen.callsByAct[domain.ActUnresolved], 1)
	if state.VisualScript == nil || len(state.VisualScript.Acts) != 4 {
		t.Fatalf("expected 4 acts built, got %#v", state.VisualScript)
	}
	// Ordering preserved: result Acts mirror input narration.Acts order.
	for i, act := range state.VisualScript.Acts {
		if act.ActID != state.Narration.Acts[i].ActID {
			t.Fatalf("ordering broken: acts[%d].act_id=%s, want=%s", i, act.ActID, state.Narration.Acts[i].ActID)
		}
	}
}

// TestVisualBreakdowner_Run_GivesUpAfterOneRetry confirms the retry budget is
// const-pinned to 1: two failing attempts → retry exhausted → ErrValidation
// surfaced (the negative-budget guarantee is exercised separately in
// retry_test.go against the helper).
func TestVisualBreakdowner_Run_GivesUpAfterOneRetry(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := sampleVisualBreakdownState4Acts()
	bad1 := actResp(domain.ActIncident, makeBadShots(state.Narration.Acts[0].Beats, "")...)
	bad2 := actResp(domain.ActIncident, makeBadShots(state.Narration.Acts[0].Beats, "zoom")...)
	queue := enqueueOnce(validActResponses(t, state.Narration))
	queue[domain.ActIncident] = []string{bad1, bad2}
	gen := &actQueueTextGenerator{responsesByAct: queue}
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation after retry exhausted, got %v", err)
	}
	testutil.AssertEqual(t, gen.callsByAct[domain.ActIncident], 2)
	if state.VisualScript != nil {
		t.Fatalf("expected state.VisualScript unchanged on failure, got %#v", state.VisualScript)
	}
}

func TestVisualBreakdowner_Run_RejectsEmptyActs(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &fakeTextGenerator{}
	state := sampleVisualBreakdownState4Acts()
	state.Narration.Acts = nil
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	testutil.AssertEqual(t, gen.calls, 0)
}

func TestVisualBreakdowner_Run_RejectsActWithoutBeats(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &fakeTextGenerator{}
	state := sampleVisualBreakdownState4Acts()
	// Strip beats from the second act → invariant violation, before any LLM call.
	state.Narration.Acts[1].Beats = nil
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	testutil.AssertEqual(t, gen.calls, 0)
}

func TestVisualBreakdowner_Run_InitializesEmptyShotOverrides(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := sampleVisualBreakdownState4Acts()
	gen := &actQueueTextGenerator{responsesByAct: enqueueOnce(validActResponses(t, state.Narration))}
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if err != nil {
		t.Fatalf("VisualBreakdowner: %v", err)
	}
	if state.VisualScript.ShotOverrides == nil || len(state.VisualScript.ShotOverrides) != 0 {
		t.Fatalf("expected empty shot overrides map, got %#v", state.VisualScript.ShotOverrides)
	}
}

func TestVisualBreakdowner_Run_DoesNotMutateStateOnFailure(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &fakeTextGenerator{resp: domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{Content: "not-json"}}}
	state := sampleVisualBreakdownState4Acts()
	sentinel := &domain.VisualScript{SCPID: "SENTINEL"}
	state.VisualScript = sentinel
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if err == nil {
		t.Fatal("expected error")
	}
	if state.VisualScript != sentinel {
		t.Fatal("visual breakdowner mutated state on failure")
	}
	if state.VisualScript.SCPID != "SENTINEL" {
		t.Fatalf("sentinel mutated in place: %#v", state.VisualScript)
	}
}

// makeBadShots returns shotResp slices that drift the transition off the
// allowed enum, anchoring each to its source beat. Used by retry tests that
// drive validator rejection without anchor mismatch.
func makeBadShots(beats []domain.BeatAnchor, badTransition string) []string {
	out := make([]string, len(beats))
	for i, beat := range beats {
		out[i] = shotResp(fmt.Sprintf("shot %d", i+1), badTransition, beat)
	}
	return out
}

// enqueueOnce wraps a per-act single-response map into a per-act FIFO queue
// shape so actQueueTextGenerator can serve one valid response per act.
func enqueueOnce(byAct map[string]string) map[string][]string {
	out := make(map[string][]string, len(byAct))
	for k, v := range byAct {
		out[k] = []string{v}
	}
	return out
}

// actQueueTextGenerator pops responses from a per-act FIFO queue, so a single
// act can be served multiple distinct responses across retries. Routes by
// parsing "Act ID: `<id>`" from the rendered prompt.
type actQueueTextGenerator struct {
	mu             sync.Mutex
	responsesByAct map[string][]string
	callsByAct     map[string]int
}

func (q *actQueueTextGenerator) Generate(_ context.Context, req domain.TextRequest) (domain.TextResponse, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	actID, ok := parseActPromptID(req.Prompt)
	if !ok {
		return domain.TextResponse{}, fmt.Errorf("actQueueTextGenerator: act_id missing from prompt")
	}
	queue, ok := q.responsesByAct[actID]
	if !ok || len(queue) == 0 {
		return domain.TextResponse{}, fmt.Errorf("actQueueTextGenerator: no response queued for act_id=%q", actID)
	}
	resp := queue[0]
	q.responsesByAct[actID] = queue[1:]
	if q.callsByAct == nil {
		q.callsByAct = map[string]int{}
	}
	q.callsByAct[actID]++
	return domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{Content: resp, Model: req.Model, Provider: "openai"}}, nil
}

func (q *actQueueTextGenerator) totalCalls() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	total := 0
	for _, n := range q.callsByAct {
		total += n
	}
	return total
}

// actErrorTextGenerator returns errOnAct's queued error for any call to that
// act, and a single valid response for every other act.
type actErrorTextGenerator struct {
	mu             sync.Mutex
	errOnAct       string
	err            error
	validResponses map[string]string
	callsByAct     map[string]int
}

func (g *actErrorTextGenerator) Generate(_ context.Context, req domain.TextRequest) (domain.TextResponse, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	actID, ok := parseActPromptID(req.Prompt)
	if !ok {
		return domain.TextResponse{}, fmt.Errorf("actErrorTextGenerator: act_id missing from prompt")
	}
	if g.callsByAct == nil {
		g.callsByAct = map[string]int{}
	}
	g.callsByAct[actID]++
	if actID == g.errOnAct {
		return domain.TextResponse{}, g.err
	}
	resp, ok := g.validResponses[actID]
	if !ok {
		return domain.TextResponse{}, fmt.Errorf("actErrorTextGenerator: no response for act_id=%q", actID)
	}
	return domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{Content: resp, Model: req.Model, Provider: "openai"}}, nil
}

// actErrorThenSuccessGenerator errors on errOnAct's call(s) until successAfter
// is reached, after which it returns the queued valid response. Used to
// verify the cycle-C ErrStageFailed retry recovery path.
type actErrorThenSuccessGenerator struct {
	mu             sync.Mutex
	errOnAct       string
	err            error
	successAfter   int
	validResponses map[string]string
	callsByAct     map[string]int
}

func (g *actErrorThenSuccessGenerator) Generate(_ context.Context, req domain.TextRequest) (domain.TextResponse, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	actID, ok := parseActPromptID(req.Prompt)
	if !ok {
		return domain.TextResponse{}, fmt.Errorf("actErrorThenSuccessGenerator: act_id missing from prompt")
	}
	if g.callsByAct == nil {
		g.callsByAct = map[string]int{}
	}
	g.callsByAct[actID]++
	if actID == g.errOnAct && g.callsByAct[actID] <= g.successAfter {
		return domain.TextResponse{}, g.err
	}
	resp, ok := g.validResponses[actID]
	if !ok {
		return domain.TextResponse{}, fmt.Errorf("actErrorThenSuccessGenerator: no response for act_id=%q", actID)
	}
	return domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{Content: resp, Model: req.Model, Provider: "openai"}}, nil
}

// actDriftingTextGenerator returns the queued response for the act parsed
// from the prompt, but stamps a drift-suffixed model/provider on each
// reply so the test can verify metadata is sourced from cfg rather than
// the LLM response.
type actDriftingTextGenerator struct {
	mu             sync.Mutex
	responsesByAct map[string]string
	calls          int
}

func (d *actDriftingTextGenerator) Generate(_ context.Context, req domain.TextRequest) (domain.TextResponse, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	driftN := d.calls
	actID, ok := parseActPromptID(req.Prompt)
	if !ok {
		return domain.TextResponse{}, fmt.Errorf("actDriftingTextGenerator: act_id missing from prompt")
	}
	resp, ok := d.responsesByAct[actID]
	if !ok {
		return domain.TextResponse{}, fmt.Errorf("actDriftingTextGenerator: no response for act_id=%q", actID)
	}
	return domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{
		Content:  resp,
		Model:    fmt.Sprintf("drift-model-%d", driftN),
		Provider: fmt.Sprintf("drift-provider-%d", driftN),
	}}, nil
}

// parseActPromptID extracts the rendered Act ID from the v2 visual_breakdowner
// prompt. The template renders "Act ID: `<id>`" as a literal line. Returns
// (actID, true) on success.
func parseActPromptID(prompt string) (string, bool) {
	const marker = "Act ID: `"
	idx := strings.Index(prompt, marker)
	if idx < 0 {
		return "", false
	}
	rest := prompt[idx+len(marker):]
	end := strings.IndexByte(rest, '`')
	if end <= 0 {
		return "", false
	}
	return rest[:end], true
}

// sampleVisualBreakdownState4Acts builds a v2 PipelineState whose narration
// has 4 acts × 8 beats — minimum size that satisfies the v2 contract schema
// (acts minItems=4, shots minItems=8 per act).
func sampleVisualBreakdownState4Acts() *PipelineState {
	state := sampleWriterState()
	state.Narration = sampleNarration4Acts()
	return state
}

// sampleNarration4Acts produces 4 acts × 8 beats. Each act's monologue is
// long enough that 8 contiguous non-overlapping rune slices fit, and each
// beat carries a unique rune offset slice.
func sampleNarration4Acts() *domain.NarrationScript {
	actIDs := []string{domain.ActIncident, domain.ActMystery, domain.ActRevelation, domain.ActUnresolved}
	script := &domain.NarrationScript{
		SCPID:         "SCP-TEST",
		Title:         "SCP-TEST",
		SourceVersion: domain.NarrationSourceVersionV2,
	}
	for actIdx, actID := range actIDs {
		// Each beat is 50 runes wide × 8 beats → 400-rune monologue per act.
		beats := make([]domain.BeatAnchor, 8)
		var monologue strings.Builder
		for i := 0; i < 8; i++ {
			start := i * 50
			end := start + 50
			beat := domain.BeatAnchor{
				StartOffset:       start,
				EndOffset:         end,
				Mood:              fmt.Sprintf("mood-%d-%d", actIdx, i),
				Location:          fmt.Sprintf("location-%d-%d", actIdx, i),
				CharactersPresent: []string{fmt.Sprintf("Char-%d-%d", actIdx, i)},
				EntityVisible:     i%2 == 0,
				ColorPalette:      "alarm red, cold gray",
				Atmosphere:        "low hum",
				FactTags:          []domain.FactTag{},
			}
			beats[i] = beat
			// Build monologue text: 50 runes per beat. Use ASCII so rune count
			// equals byte count for simplicity in the test fixture.
			beatText := strings.Repeat(string(rune('a'+i)), 50)
			monologue.WriteString(beatText)
		}
		script.Acts = append(script.Acts, domain.ActScript{
			ActID:     actID,
			Monologue: monologue.String(),
			Mood:      fmt.Sprintf("act-%d-mood", actIdx),
			KeyPoints: []string{},
			Beats:     beats,
		})
	}
	return script
}

func sampleVisualAssets() PromptAssets {
	return PromptAssets{
		// Mirror the v2 placeholder set the agent's renderer substitutes —
		// must include "Act ID: `{act_id}`" so parseActPromptID can route.
		VisualBreakdownTemplate: "Act ID: `{act_id}`\nMood: {act_mood}\nMonologue: {monologue}\nBeats: {beats_table}\nIdentity: {scp_visual_reference}\nFrozen: {frozen_descriptor}\nShots: {shot_count}\n",
		ReviewerTemplate:        "unused",
		WriterTemplate:          "unused",
		CriticTemplate:          "unused",
		FormatGuide:             "guide",
	}
}
