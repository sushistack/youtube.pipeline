package agents

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// --- test doubles ---

type polisherQueueGen struct {
	mu        sync.Mutex
	responses []string
	errs      []error
	calls     int
	model     string
	provider  string
}

func newPolisherGen(responses ...string) *polisherQueueGen {
	return &polisherQueueGen{responses: responses, model: "polisher-model", provider: "openai"}
}

func newPolisherGenWithErr(err error) *polisherQueueGen {
	return &polisherQueueGen{errs: []error{err, err}, model: "polisher-model", provider: "openai"}
}

// polisherGenStep scripts a single Generate call's outcome — either an
// error (transport / context) or a content string. Used by
// newPolisherGenSeq to build multi-attempt sequences such as
// "transient error then success".
type polisherGenStep struct {
	content string
	err     error
}

func newPolisherGenSeq(steps ...polisherGenStep) *polisherQueueGen {
	g := &polisherQueueGen{model: "polisher-model", provider: "openai"}
	for _, s := range steps {
		g.errs = append(g.errs, s.err)
		g.responses = append(g.responses, s.content)
	}
	return g
}

func (g *polisherQueueGen) Generate(_ context.Context, _ domain.TextRequest) (domain.TextResponse, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	i := g.calls
	g.calls++
	if i < len(g.errs) && g.errs[i] != nil {
		return domain.TextResponse{}, g.errs[i]
	}
	if i < len(g.responses) {
		return domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{
			Content:  g.responses[i],
			Model:    g.model,
			Provider: g.provider,
		}}, nil
	}
	return domain.TextResponse{}, errors.New("polisher test gen: no response queued")
}

type spyAuditLogger struct {
	mu      sync.Mutex
	entries []domain.AuditEntry
}

func (s *spyAuditLogger) Log(_ context.Context, entry domain.AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, entry)
	return nil
}

func (s *spyAuditLogger) all() []domain.AuditEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.AuditEntry, len(s.entries))
	copy(out, s.entries)
	return out
}

// --- helpers ---

func samplePolisherState(t *testing.T) *PipelineState {
	t.Helper()
	script := loadMergedSample(t)
	return &PipelineState{
		RunID:     "pol-run-1",
		SCPID:     "SCP-TEST",
		Narration: &script,
	}
}

func samplePolisherConfig() TextAgentConfig {
	return TextAgentConfig{Model: "polisher-model", Provider: "openai", MaxTokens: 8192, Temperature: 0.5}
}

func samplePolisherAssets() PromptAssets {
	return PromptAssets{PolisherTemplate: "Polish {scp_id}:\n{narration_script_json}"}
}

func newPolisherForTestT(t *testing.T, gen domain.TextGenerator, spy *spyAuditLogger) AgentFunc {
	t.Helper()
	cfg := samplePolisherConfig()
	if spy != nil {
		cfg.AuditLogger = spy
	}
	return NewPolisher(gen, cfg, samplePolisherAssets(), mustValidator(t, "writer_output.schema.json"), mustTerms(t))
}

// polishedScript returns a valid NarrationScript JSON where each scene's
// narration has been nudged by fewer than 25% runes (single char appended).
func polishedScriptJSON(t *testing.T, base domain.NarrationScript) string {
	t.Helper()
	for i := range base.Scenes {
		// Append one character — always well within the 25% budget.
		base.Scenes[i].Narration += "."
	}
	b, err := json.Marshal(base)
	if err != nil {
		t.Fatalf("marshal polished: %v", err)
	}
	return string(b)
}

// budgetBustingScriptJSON returns a NarrationScript JSON where scene 0's
// narration is expanded well beyond domain.PolisherMaxEditRatio (currently
// 0.40 — see narration.go calibration history).
func budgetBustingScriptJSON(t *testing.T, base domain.NarrationScript) string {
	t.Helper()
	origRunes := utf8.RuneCountInString(base.Scenes[0].Narration)
	// Add 50% more runes — guaranteed to exceed the cap regardless of
	// reasonable future tuning (0.25 ~ 0.50).
	extra := origRunes/2 + 1
	for i := 0; i < extra; i++ {
		base.Scenes[0].Narration += "X"
	}
	b, err := json.Marshal(base)
	if err != nil {
		t.Fatalf("marshal budget-busting: %v", err)
	}
	return string(b)
}

// forbiddenScriptJSON returns a valid NarrationScript where scene 0
// contains the "wiki" forbidden term (matches mustTerms pattern).
func forbiddenScriptJSON(t *testing.T, base domain.NarrationScript) string {
	t.Helper()
	base.Scenes[0].Narration = "wiki " + base.Scenes[0].Narration
	b, err := json.Marshal(base)
	if err != nil {
		t.Fatalf("marshal forbidden: %v", err)
	}
	return string(b)
}

// readOnlyMutatedScriptJSON returns a NarrationScript that is schema-valid
// and stays within the per-scene rune budget but mutates a non-text
// read-only field on scene[0] (location). Exercises
// checkPolisherReadOnlyInvariance — schema alone would not catch this.
func readOnlyMutatedScriptJSON(t *testing.T, base domain.NarrationScript) string {
	t.Helper()
	if len(base.Scenes) == 0 {
		t.Fatal("readOnlyMutatedScriptJSON needs ≥1 scene")
	}
	// Polish marker on every scene to keep budget happy.
	for i := range base.Scenes {
		base.Scenes[i].Narration += "."
	}
	// Mutate a non-text read-only field on scene[0]. location is a free-form
	// string so this stays schema-valid; only the explicit invariance check
	// catches it.
	base.Scenes[0].Location = "polisher-rewrote-this-location"
	b, err := json.Marshal(base)
	if err != nil {
		t.Fatalf("marshal readonly-mutated: %v", err)
	}
	return string(b)
}

// --- tests ---

func TestPolisher_HappyPath(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Build the polished response from a fresh load so we can compare it
	// against the post-run state byte-for-byte. (loadMergedSample returns
	// independent values each call — see loader.)
	polishedSource := loadMergedSample(t)
	polishedJSON := polishedScriptJSON(t, polishedSource)
	var wantScript domain.NarrationScript
	if err := json.Unmarshal([]byte(polishedJSON), &wantScript); err != nil {
		t.Fatalf("unmarshal expected polished: %v", err)
	}

	spy := &spyAuditLogger{}
	agent := newPolisherForTestT(t, newPolisherGen(polishedJSON), spy)
	state := samplePolisherState(t)

	if err := agent(context.Background(), state); err != nil {
		t.Fatalf("polisher: unexpected error: %v", err)
	}
	if state.Narration == nil {
		t.Fatal("state.Narration is nil after successful polish")
	}

	// Every scene's narration must end with "." (the polished marker) —
	// not just scene[0]. A regression that only updated the first scene
	// would have escaped the previous assertion.
	for i, scene := range state.Narration.Scenes {
		if len(scene.Narration) == 0 || scene.Narration[len(scene.Narration)-1] != '.' {
			t.Errorf("scene[%d] narration not polished: %q", i, scene.Narration)
		}
	}

	// state.Narration must equal the polished response byte-for-byte after
	// JSON round-trip. This catches any silent field drop / mutation that
	// happened between decode and assignment.
	gotJSON, err := json.Marshal(state.Narration)
	if err != nil {
		t.Fatalf("marshal got: %v", err)
	}
	wantJSON, err := json.Marshal(&wantScript)
	if err != nil {
		t.Fatalf("marshal want: %v", err)
	}
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("state.Narration does not match polished response.\n got: %s\nwant: %s", gotJSON, wantJSON)
	}

	// Audit ledger must contain exactly one text_generation entry with
	// stage="polisher" and zero polisher_failed entries.
	entries := spy.all()
	var textGen, failed int
	for _, e := range entries {
		switch e.EventType {
		case domain.AuditEventTextGeneration:
			textGen++
			if e.Stage != "polisher" {
				t.Errorf("text_generation stage = %q, want %q", e.Stage, "polisher")
			}
		case domain.AuditEventPolisherFailed:
			failed++
		}
	}
	if textGen != 1 {
		t.Errorf("text_generation entries = %d, want 1", textGen)
	}
	if failed != 0 {
		t.Errorf("polisher_failed entries = %d, want 0 on happy path", failed)
	}
}

// TestPolisher_TransientRetryRecovers verifies that a single transient
// generator error on the first attempt is retried, and a successful
// second attempt is accepted with no fallback. This is the difference
// between "polisher works most of the time" and "one TCP reset
// permanently disables polisher".
func TestPolisher_TransientRetryRecovers(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	polishedSource := loadMergedSample(t)
	polishedJSON := polishedScriptJSON(t, polishedSource)

	gen := newPolisherGenSeq(
		polisherGenStep{err: errors.New("transient: connection reset by peer")},
		polisherGenStep{content: polishedJSON},
	)
	spy := &spyAuditLogger{}
	agent := newPolisherForTestT(t, gen, spy)
	state := samplePolisherState(t)

	if err := agent(context.Background(), state); err != nil {
		t.Fatalf("polisher: unexpected error: %v", err)
	}
	if state.Narration == nil {
		t.Fatal("state.Narration is nil after retry recovery")
	}
	// Scene[0] must reflect the polished payload (ends in ".").
	if got := state.Narration.Scenes[0].Narration; len(got) == 0 || got[len(got)-1] != '.' {
		t.Errorf("scene[0] not polished after retry: %q", got)
	}
	// Generator must have been called exactly twice (one fail + one success).
	if gen.calls != 2 {
		t.Errorf("generator calls = %d, want 2", gen.calls)
	}
	// Audit ledger: exactly one text_generation, zero polisher_failed.
	var textGen, failed int
	for _, e := range spy.all() {
		switch e.EventType {
		case domain.AuditEventTextGeneration:
			textGen++
		case domain.AuditEventPolisherFailed:
			failed++
		}
	}
	if textGen != 1 {
		t.Errorf("text_generation entries = %d, want 1 (one per successful run, not per attempt)", textGen)
	}
	if failed != 0 {
		t.Errorf("polisher_failed entries = %d, want 0 (transient retry recovered)", failed)
	}
}

// TestPolisher_ContextCancelledPropagates verifies that a cancelled ctx
// returns the ctx error to the runner — does NOT silently fall back.
// Falling back on cancellation would have the runner happily continue
// to the next stage after the operator asked the run to stop.
func TestPolisher_ContextCancelledPropagates(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	spy := &spyAuditLogger{}
	// Generator should never be invoked because we cancel ctx before calling.
	gen := newPolisherGen("should not be called")
	agent := newPolisherForTestT(t, gen, spy)
	state := samplePolisherState(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := agent(ctx, state)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("polisher: expected context.Canceled, got: %v", err)
	}

	// No fallback audit entry on cancellation — the run aborts cleanly.
	for _, e := range spy.all() {
		if e.EventType == domain.AuditEventPolisherFailed {
			t.Errorf("polisher_failed emitted on context cancellation: %+v", e)
		}
		if e.EventType == domain.AuditEventTextGeneration {
			t.Errorf("text_generation emitted on context cancellation: %+v", e)
		}
	}
}

// TestPolisher_ContextCancelMidGeneratorPropagates verifies that a
// generator returning ctx.Canceled (simulating mid-call cancellation)
// also propagates rather than falling back.
func TestPolisher_ContextCancelMidGeneratorPropagates(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	spy := &spyAuditLogger{}
	// Generator returns context.Canceled directly; ctx itself is alive.
	gen := newPolisherGenWithErr(context.Canceled)
	agent := newPolisherForTestT(t, gen, spy)
	state := samplePolisherState(t)

	err := agent(context.Background(), state)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("polisher: expected context.Canceled propagation, got: %v", err)
	}
	for _, e := range spy.all() {
		if e.EventType == domain.AuditEventPolisherFailed {
			t.Errorf("polisher_failed emitted on cancellation: %+v", e)
		}
	}
}

// TestPolisher_ReadOnlyFieldMutation verifies that a polished script
// which keeps the schema valid but mutates a non-text field (e.g.,
// swaps act_id) triggers fallback. JSON schema alone is too permissive.
func TestPolisher_ReadOnlyFieldMutation(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	original := loadMergedSample(t)
	mutated := readOnlyMutatedScriptJSON(t, original)

	spy := &spyAuditLogger{}
	agent := newPolisherForTestT(t, newPolisherGen(mutated), spy)
	state := samplePolisherState(t)
	origSnapshot := *state.Narration

	if err := agent(context.Background(), state); err != nil {
		t.Fatalf("polisher: expected nil (fallback), got: %v", err)
	}
	assertPolisherFallback(t, state, origSnapshot, spy)
}

// TestPolisher_ForbiddenTermsInherited verifies that a forbidden term
// already present in the writer's pre-polish narration does NOT trigger
// fallback when the polisher inherits it (no new hits introduced). The
// writer is responsible for its own pre-existing matches.
func TestPolisher_ForbiddenTermsInherited(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Prime the input narration to already contain a forbidden term ("wiki"
	// matches mustTerms pattern).
	primed := loadMergedSample(t)
	primed.Scenes[0].Narration = "wiki " + primed.Scenes[0].Narration

	// Polished version: keep the inherited term, append "." for polish.
	polished := primed
	polishedJSON := polishedScriptJSON(t, polished)

	spy := &spyAuditLogger{}
	agent := newPolisherForTestT(t, newPolisherGen(polishedJSON), spy)
	state := &PipelineState{
		RunID:     "pol-run-inherited",
		SCPID:     "SCP-TEST",
		Narration: &primed,
	}

	if err := agent(context.Background(), state); err != nil {
		t.Fatalf("polisher: unexpected error: %v", err)
	}
	// state.Narration should be REPLACED (not fallback), because the
	// inherited "wiki" was already in the writer's output.
	if state.Narration.Scenes[0].Narration[len(state.Narration.Scenes[0].Narration)-1] != '.' {
		t.Errorf("scene[0] not polished — inherited forbidden term unfairly triggered fallback")
	}
	for _, e := range spy.all() {
		if e.EventType == domain.AuditEventPolisherFailed {
			t.Errorf("polisher_failed emitted for inherited (not new) forbidden term: %+v", e)
		}
	}
}

func TestPolisher_SchemaViolation(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	spy := &spyAuditLogger{}
	agent := newPolisherForTestT(t, newPolisherGen(`{"invalid": true}`), spy)
	state := samplePolisherState(t)
	original := *state.Narration

	if err := agent(context.Background(), state); err != nil {
		t.Fatalf("polisher: expected nil (fallback), got: %v", err)
	}
	assertPolisherFallback(t, state, original, spy)
}

func TestPolisher_EditBudgetViolation(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	original := loadMergedSample(t)
	busted := budgetBustingScriptJSON(t, original)

	spy := &spyAuditLogger{}
	agent := newPolisherForTestT(t, newPolisherGen(busted), spy)
	state := samplePolisherState(t)
	origSnapshot := *state.Narration

	if err := agent(context.Background(), state); err != nil {
		t.Fatalf("polisher: expected nil (fallback), got: %v", err)
	}
	assertPolisherFallback(t, state, origSnapshot, spy)
}

func TestPolisher_ForbiddenTerms(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	original := loadMergedSample(t)
	forbidden := forbiddenScriptJSON(t, original)

	spy := &spyAuditLogger{}
	agent := newPolisherForTestT(t, newPolisherGen(forbidden), spy)
	state := samplePolisherState(t)
	origSnapshot := *state.Narration

	if err := agent(context.Background(), state); err != nil {
		t.Fatalf("polisher: expected nil (fallback), got: %v", err)
	}
	assertPolisherFallback(t, state, origSnapshot, spy)
}

func TestPolisher_LLMFailFallback(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	genErr := errors.New("network error")
	spy := &spyAuditLogger{}
	agent := newPolisherForTestT(t, newPolisherGenWithErr(genErr), spy)
	state := samplePolisherState(t)
	origSnapshot := *state.Narration

	if err := agent(context.Background(), state); err != nil {
		t.Fatalf("polisher: expected nil (fallback), got: %v", err)
	}
	assertPolisherFallback(t, state, origSnapshot, spy)
}

func TestPolisher_DoesNotMutateOnFailure(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Use a JSON-decode failure (empty response) as the failure trigger.
	spy := &spyAuditLogger{}
	agent := newPolisherForTestT(t, newPolisherGen(""), spy)
	state := samplePolisherState(t)

	// Deep-copy the original scenes for comparison.
	origBytes, _ := json.Marshal(state.Narration)

	if err := agent(context.Background(), state); err != nil {
		t.Fatalf("polisher: expected nil (fallback), got: %v", err)
	}

	afterBytes, _ := json.Marshal(state.Narration)
	if string(origBytes) != string(afterBytes) {
		t.Error("state.Narration was mutated despite fallback")
	}
}

func TestPolisher_NilNarration_NoOp(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	spy := &spyAuditLogger{}
	agent := newPolisherForTestT(t, newPolisherGen("should not be called"), spy)
	state := &PipelineState{RunID: "pol-run-nil", SCPID: "SCP-TEST", Narration: nil}

	if err := agent(context.Background(), state); err != nil {
		t.Fatalf("polisher with nil narration: unexpected error: %v", err)
	}
	if len(spy.all()) != 0 {
		t.Error("expected no audit entries when narration is nil")
	}
}

// assertPolisherFallback verifies the fallback contract end-to-end:
//   - state.Narration is unchanged byte-for-byte vs original.
//   - exactly ONE polisher_failed audit entry was emitted, with stage="polisher".
//   - ZERO text_generation audit entries were emitted — the audit ledger's
//     contract is "the only observable difference between fallback and happy
//     run is the event type", which means a fallback run must not also
//     emit a success-flavored entry.
func assertPolisherFallback(t *testing.T, state *PipelineState, original domain.NarrationScript, spy *spyAuditLogger) {
	t.Helper()

	// state.Narration must point to the original (shallow pointer check is
	// insufficient — compare JSON for deep equality).
	origBytes, _ := json.Marshal(original)
	gotBytes, _ := json.Marshal(state.Narration)
	if string(origBytes) != string(gotBytes) {
		t.Error("state.Narration was mutated despite fallback path")
	}

	entries := spy.all()
	var failed, textGen []domain.AuditEntry
	for _, e := range entries {
		switch e.EventType {
		case domain.AuditEventPolisherFailed:
			failed = append(failed, e)
		case domain.AuditEventTextGeneration:
			textGen = append(textGen, e)
		}
	}
	if len(failed) != 1 {
		t.Errorf("polisher_failed audit entries = %d, want 1 (all entries: %v)", len(failed), entries)
	}
	if len(failed) > 0 && failed[0].Stage != "polisher" {
		t.Errorf("polisher_failed stage = %q, want %q", failed[0].Stage, "polisher")
	}
	if len(textGen) != 0 {
		t.Errorf("text_generation audit entries on fallback = %d, want 0 (paired-event leak — audit was emitted before validation)", len(textGen))
	}
}
