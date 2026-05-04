package agents

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestPostWriterCritic_Run_Happy(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	report := string(testutil.LoadFixture(t, filepath.Join("contracts", "critic_post_writer.sample.json")))
	gen := &fakeTextGenerator{resp: domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{
		Content:  report,
		Model:    "critic-model",
		Provider: "anthropic",
	}}}
	state := sampleCriticState(t)
	err := NewPostWriterCritic(gen, TextAgentConfig{Model: "critic-model", Provider: "anthropic"}, sampleWriterAssets(), mustValidator(t, "writer_output.schema.json"), mustValidator(t, "critic_post_writer.schema.json"), mustTerms(t), "openai")(context.Background(), state)
	if err != nil {
		t.Fatalf("Critic: %v", err)
	}
	if state.Critic == nil || state.Critic.PostWriter == nil {
		t.Fatal("expected post_writer report")
	}
	testutil.AssertEqual(t, gen.calls, 1)
}

func TestPostWriterCritic_Run_SameProviderBlocked(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &fakeTextGenerator{}
	state := sampleCriticState(t)
	err := NewPostWriterCritic(gen, TextAgentConfig{Model: "critic-model", Provider: "openai"}, sampleWriterAssets(), mustValidator(t, "writer_output.schema.json"), mustValidator(t, "critic_post_writer.schema.json"), mustTerms(t), "openai")(context.Background(), state)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "Writer and Critic must use different LLM providers") {
		t.Fatalf("expected message substring, got %q", err.Error())
	}
	testutil.AssertEqual(t, gen.calls, 0)
}

func TestPostWriterCritic_Run_PrecheckRetryWithoutLLMCall(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := sampleCriticState(t)
	state.Narration.Acts[0].Monologue = "이건 wiki 문체입니다."
	gen := &fakeTextGenerator{}
	err := NewPostWriterCritic(gen, TextAgentConfig{Model: "critic-model", Provider: "anthropic"}, sampleWriterAssets(), mustValidator(t, "writer_output.schema.json"), mustValidator(t, "critic_post_writer.schema.json"), mustTerms(t), "openai")(context.Background(), state)
	if err != nil {
		t.Fatalf("Critic: %v", err)
	}
	if state.Critic == nil || state.Critic.PostWriter == nil {
		t.Fatal("expected short-circuit report")
	}
	testutil.AssertEqual(t, state.Critic.PostWriter.RetryReason, "forbidden_terms_detected")
	testutil.AssertEqual(t, gen.calls, 0)
}

func TestPostWriterCritic_Run_FillsRetryReasonFromRubric(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &fakeTextGenerator{resp: domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{
		Content: `{"checkpoint":"post_writer","verdict":"retry","overall_score":42,"rubric":{"hook":10,"fact_accuracy":60,"emotional_variation":70,"immersion":80},"feedback":"후크가 약합니다.","scene_notes":[]}`,
		Model:   "critic-model", Provider: "anthropic",
	}}}
	state := sampleCriticState(t)
	err := NewPostWriterCritic(gen, TextAgentConfig{Model: "critic-model", Provider: "anthropic"}, sampleWriterAssets(), mustValidator(t, "writer_output.schema.json"), mustValidator(t, "critic_post_writer.schema.json"), mustTerms(t), "openai")(context.Background(), state)
	if err != nil {
		t.Fatalf("Critic: %v", err)
	}
	testutil.AssertEqual(t, state.Critic.PostWriter.RetryReason, "weak_hook")
}

func TestPostWriterCritic_Run_PreservesPostReviewerNil(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := sampleCriticState(t)
	gen := &fakeTextGenerator{resp: domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{
		Content:  string(testutil.LoadFixture(t, filepath.Join("contracts", "critic_post_writer.sample.json"))),
		Model:    "critic-model",
		Provider: "anthropic",
	}}}
	err := NewPostWriterCritic(gen, TextAgentConfig{Model: "critic-model", Provider: "anthropic"}, sampleWriterAssets(), mustValidator(t, "writer_output.schema.json"), mustValidator(t, "critic_post_writer.schema.json"), mustTerms(t), "openai")(context.Background(), state)
	if err != nil {
		t.Fatalf("Critic: %v", err)
	}
	if state.Critic.PostReviewer != nil {
		t.Fatalf("expected post_reviewer nil, got %+v", state.Critic.PostReviewer)
	}
}

func TestPostReviewerCritic_Run_Happy(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	report := string(testutil.LoadFixture(t, filepath.Join("contracts", "critic_post_reviewer.sample.json")))
	gen := &fakeTextGenerator{resp: domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{
		Content:  report,
		Model:    "critic-model",
		Provider: "anthropic",
	}}}
	state := samplePostReviewerCriticState(t)
	err := NewPostReviewerCritic(gen, TextAgentConfig{Model: "critic-model", Provider: "anthropic"}, sampleWriterAssets(), mustValidator(t, "writer_output.schema.json"), mustValidator(t, "visual_breakdown.schema.json"), mustValidator(t, "reviewer_report.schema.json"), mustValidator(t, "critic_post_reviewer.schema.json"), mustTerms(t), "openai")(context.Background(), state)
	if err != nil {
		t.Fatalf("Critic: %v", err)
	}
	if state.Critic == nil || state.Critic.PostReviewer == nil {
		t.Fatal("expected post_reviewer report")
	}
	testutil.AssertEqual(t, gen.calls, 1)
	testutil.AssertEqual(t, state.Critic.PostWriter.Checkpoint, domain.CriticCheckpointPostWriter)
}

func TestPostReviewerCritic_Run_PrecheckReviewFailureShortCircuits(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := samplePostReviewerCriticState(t)
	state.Review.OverallPass = false
	gen := &fakeTextGenerator{}
	err := NewPostReviewerCritic(gen, TextAgentConfig{Model: "critic-model", Provider: "anthropic"}, sampleWriterAssets(), mustValidator(t, "writer_output.schema.json"), mustValidator(t, "visual_breakdown.schema.json"), mustValidator(t, "reviewer_report.schema.json"), mustValidator(t, "critic_post_reviewer.schema.json"), mustTerms(t), "openai")(context.Background(), state)
	if err != nil {
		t.Fatalf("Critic: %v", err)
	}
	testutil.AssertEqual(t, gen.calls, 0)
	testutil.AssertEqual(t, state.Critic.PostReviewer.RetryReason, "review_failed")
	if !state.Critic.PostReviewer.Precheck.ShortCircuited {
		t.Fatalf("expected short-circuited precheck")
	}
}

func TestPostReviewerCritic_Run_PrecheckSchemaFailureShortCircuits(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := samplePostReviewerCriticState(t)
	state.VisualScript.Acts = nil
	gen := &fakeTextGenerator{}
	err := NewPostReviewerCritic(gen, TextAgentConfig{Model: "critic-model", Provider: "anthropic"}, sampleWriterAssets(), mustValidator(t, "writer_output.schema.json"), mustValidator(t, "visual_breakdown.schema.json"), mustValidator(t, "reviewer_report.schema.json"), mustValidator(t, "critic_post_reviewer.schema.json"), mustTerms(t), "openai")(context.Background(), state)
	if err != nil {
		t.Fatalf("Critic: %v", err)
	}
	testutil.AssertEqual(t, gen.calls, 0)
	testutil.AssertEqual(t, state.Critic.PostReviewer.RetryReason, "schema_validation_failed")
}

func TestPostReviewerCritic_Run_PrecheckForbiddenTermsShortCircuits(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := samplePostReviewerCriticState(t)
	state.Narration.Acts[0].Monologue = "이건 wiki 문체입니다."
	gen := &fakeTextGenerator{}
	err := NewPostReviewerCritic(gen, TextAgentConfig{Model: "critic-model", Provider: "anthropic"}, sampleWriterAssets(), mustValidator(t, "writer_output.schema.json"), mustValidator(t, "visual_breakdown.schema.json"), mustValidator(t, "reviewer_report.schema.json"), mustValidator(t, "critic_post_reviewer.schema.json"), mustTerms(t), "openai")(context.Background(), state)
	if err != nil {
		t.Fatalf("Critic: %v", err)
	}
	testutil.AssertEqual(t, gen.calls, 0)
	testutil.AssertEqual(t, state.Critic.PostReviewer.RetryReason, "forbidden_terms_detected")
}

func TestPostReviewerCritic_Run_SameProviderBlocked(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &fakeTextGenerator{}
	state := samplePostReviewerCriticState(t)
	err := NewPostReviewerCritic(gen, TextAgentConfig{Model: "critic-model", Provider: "openai"}, sampleWriterAssets(), mustValidator(t, "writer_output.schema.json"), mustValidator(t, "visual_breakdown.schema.json"), mustValidator(t, "reviewer_report.schema.json"), mustValidator(t, "critic_post_reviewer.schema.json"), mustTerms(t), "openai")(context.Background(), state)
	if err == nil || err.Error() != "critic: validation error: Writer and Critic must use different LLM providers" {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertEqual(t, gen.calls, 0)
}

func TestPostReviewerCritic_Run_FillsRetryReasonFromRubric(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &fakeTextGenerator{resp: domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{
		Content: `{"checkpoint":"post_reviewer","verdict":"retry","overall_score":42,"rubric":{"hook":60,"fact_accuracy":20,"emotional_variation":70,"immersion":80},"feedback":"사실 검증 보강이 더 필요합니다.","scene_notes":[],"precheck":{"schema_valid":true,"forbidden_term_hits":[],"short_circuited":false},"critic_model":"critic-model","critic_provider":"anthropic","source_version":"v1-critic-post-writer"}`,
		Model:   "critic-model", Provider: "anthropic",
	}}}
	state := samplePostReviewerCriticState(t)
	err := NewPostReviewerCritic(gen, TextAgentConfig{Model: "critic-model", Provider: "anthropic"}, sampleWriterAssets(), mustValidator(t, "writer_output.schema.json"), mustValidator(t, "visual_breakdown.schema.json"), mustValidator(t, "reviewer_report.schema.json"), mustValidator(t, "critic_post_reviewer.schema.json"), mustTerms(t), "openai")(context.Background(), state)
	if err != nil {
		t.Fatalf("Critic: %v", err)
	}
	testutil.AssertEqual(t, state.Critic.PostReviewer.RetryReason, "fact_accuracy")
}

func TestPostReviewerCritic_Run_PreservesPostWriter(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := samplePostReviewerCriticState(t)
	orig := *state.Critic.PostWriter
	gen := &fakeTextGenerator{resp: domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{
		Content:  string(testutil.LoadFixture(t, filepath.Join("contracts", "critic_post_reviewer.sample.json"))),
		Model:    "critic-model",
		Provider: "anthropic",
	}}}
	err := NewPostReviewerCritic(gen, TextAgentConfig{Model: "critic-model", Provider: "anthropic"}, sampleWriterAssets(), mustValidator(t, "writer_output.schema.json"), mustValidator(t, "visual_breakdown.schema.json"), mustValidator(t, "reviewer_report.schema.json"), mustValidator(t, "critic_post_reviewer.schema.json"), mustTerms(t), "openai")(context.Background(), state)
	if err != nil {
		t.Fatalf("Critic: %v", err)
	}
	if state.Critic.PostWriter == nil || !reflect.DeepEqual(*state.Critic.PostWriter, orig) {
		t.Fatalf("expected post_writer preserved, got %+v", state.Critic.PostWriter)
	}
}

func TestPostWriterCritic_Run_InvalidCriticJSON(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &fakeTextGenerator{resp: domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{Content: "not-json"}}}
	state := sampleCriticState(t)
	err := NewPostWriterCritic(gen, TextAgentConfig{Model: "critic-model", Provider: "anthropic"}, sampleWriterAssets(), mustValidator(t, "writer_output.schema.json"), mustValidator(t, "critic_post_writer.schema.json"), mustTerms(t), "openai")(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestPostWriterCritic_Run_CriticSchemaViolation(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &fakeTextGenerator{resp: domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{
		Content: `{"checkpoint":"post_writer","verdict":"pass","overall_score":101,"rubric":{"hook":10},"feedback":"좋습니다.","scene_notes":[]}`,
	}}}
	state := sampleCriticState(t)
	err := NewPostWriterCritic(gen, TextAgentConfig{Model: "critic-model", Provider: "anthropic"}, sampleWriterAssets(), mustValidator(t, "writer_output.schema.json"), mustValidator(t, "critic_post_writer.schema.json"), mustTerms(t), "openai")(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestPostWriterPrecheck_SchemaFailureShortCircuits(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := sampleCriticState(t)
	state.Narration.Acts = nil
	precheck, err := runPostWriterPrecheck(state.Narration, mustValidator(t, "writer_output.schema.json"), mustTerms(t))
	if err != nil {
		t.Fatalf("precheck: %v", err)
	}
	if !precheck.ShortCircuited || precheck.SchemaValid {
		t.Fatalf("unexpected precheck: %+v", precheck)
	}
}

func TestPostWriterPrecheck_ForbiddenTermsShortCircuits(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := sampleCriticState(t)
	state.Narration.Acts[0].Monologue = "이건 wiki 문체입니다."
	precheck, err := runPostWriterPrecheck(state.Narration, mustValidator(t, "writer_output.schema.json"), mustTerms(t))
	if err != nil {
		t.Fatalf("precheck: %v", err)
	}
	if !precheck.ShortCircuited || len(precheck.ForbiddenTermHits) == 0 {
		t.Fatalf("unexpected precheck: %+v", precheck)
	}
}

func TestDeriveRetryReason_LowestWins(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	testutil.AssertEqual(t, DeriveRetryReason(domain.CriticRubricScores{Hook: 80, FactAccuracy: 70, EmotionalVariation: 60, Immersion: 90}), "emotional_variation")
}

func TestDeriveRetryReason_TieBreakOrder(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	testutil.AssertEqual(t, DeriveRetryReason(domain.CriticRubricScores{Hook: 10, FactAccuracy: 10, EmotionalVariation: 10, Immersion: 10}), "weak_hook")
}

func TestPostWriterCritic_Run_PrecheckReportIsSchemaValidated(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := sampleCriticState(t)
	state.Narration.Acts[0].Monologue = "이건 wiki 문체입니다."
	gen := &fakeTextGenerator{}
	err := NewPostWriterCritic(gen, TextAgentConfig{Model: "critic-model", Provider: "anthropic"}, sampleWriterAssets(), mustValidator(t, "writer_output.schema.json"), mustValidator(t, "critic_post_writer.schema.json"), mustTerms(t), "openai")(context.Background(), state)
	if err != nil {
		t.Fatalf("Critic: %v", err)
	}
	if state.Critic == nil || state.Critic.PostWriter == nil {
		t.Fatal("expected short-circuit report")
	}
	// Independently re-validate the stored report against the critic schema —
	// the guard inside the agent should have already gated this, so any
	// passing run here confirms the report is schema-valid.
	if err := mustValidator(t, "critic_post_writer.schema.json").Validate(*state.Critic.PostWriter); err != nil {
		t.Fatalf("precheck report failed schema: %v", err)
	}
}

func TestDecodeJSONResponse_StripsBOM(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	var dst struct {
		OK bool `json:"ok"`
	}
	if err := decodeJSONResponse("\ufeff{\"ok\":true}", &dst); err != nil {
		t.Fatalf("decodeJSONResponse: %v", err)
	}
	testutil.AssertEqual(t, dst.OK, true)

	// With surrounding whitespace the BOM must still be stripped.
	dst.OK = false
	if err := decodeJSONResponse("  \ufeff  {\"ok\":true}", &dst); err != nil {
		t.Fatalf("decodeJSONResponse padded: %v", err)
	}
	testutil.AssertEqual(t, dst.OK, true)
}

func TestDecodeJSONResponse_FencedUppercaseLabel(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	var dst struct {
		OK bool `json:"ok"`
	}
	if err := decodeJSONResponse("```JSON\n{\"ok\":true}\n```", &dst); err != nil {
		t.Fatalf("uppercase JSON fence: %v", err)
	}
	testutil.AssertEqual(t, dst.OK, true)

	dst.OK = false
	if err := decodeJSONResponse("```Json\n{\"ok\":true}\n```", &dst); err != nil {
		t.Fatalf("mixed-case Json fence: %v", err)
	}
	testutil.AssertEqual(t, dst.OK, true)

	dst.OK = false
	if err := decodeJSONResponse("```python\n{\"ok\":true}\n```", &dst); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation on python label, got %v", err)
	}
}

func TestDecodeJSONResponse_CRLFLineEndings(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	var dst struct {
		OK bool `json:"ok"`
	}
	if err := decodeJSONResponse("```json\r\n{\"ok\":true}\r\n```\r\n", &dst); err != nil {
		t.Fatalf("CRLF fence: %v", err)
	}
	testutil.AssertEqual(t, dst.OK, true)

	// Bare CRLF (no fence) should also work since BOM-strip runs and body is
	// detected by the leading '{'.
	dst.OK = false
	if err := decodeJSONResponse("{\"ok\":true}\r\n", &dst); err != nil {
		t.Fatalf("bare CRLF: %v", err)
	}
	testutil.AssertEqual(t, dst.OK, true)
}

func TestContainsHangul_CoversJamoRanges(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Hangul Syllables (U+AC00–U+D7A3).
	testutil.AssertEqual(t, containsHangul("한글"), true)
	// Hangul Jamo (U+1100–U+11FF): U+1100 ᄀ.
	testutil.AssertEqual(t, containsHangul("\u1100"), true)
	// Hangul Compatibility Jamo (U+3130–U+318F): U+3131 ㄱ.
	testutil.AssertEqual(t, containsHangul("\u3131"), true)
	// Non-Hangul.
	testutil.AssertEqual(t, containsHangul("hello"), false)
	testutil.AssertEqual(t, containsHangul("日本語"), false)
}

func TestScoreRubric_RoundsNotTruncates(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// 99.25 — nearest integer is 99 (round-half-to-even or away-from-zero
	// both yield 99 here).
	testutil.AssertEqual(t, scoreRubric(domain.CriticRubricScores{Hook: 100, FactAccuracy: 99, EmotionalVariation: 99, Immersion: 99}), 99)
	// 99.5 — math.Round rounds halves away from zero, so 100. Under the old
	// int-truncation scheme this was 99.
	testutil.AssertEqual(t, scoreRubric(domain.CriticRubricScores{Hook: 100, FactAccuracy: 100, EmotionalVariation: 99, Immersion: 99}), 100)
}

func sampleCriticState(t *testing.T) *PipelineState {
	t.Helper()
	var script domain.NarrationScript
	if err := json.Unmarshal(testutil.LoadFixture(t, filepath.Join("contracts", "writer_output.sample.json")), &script); err != nil {
		t.Fatalf("unmarshal narration: %v", err)
	}
	return &PipelineState{
		RunID:     "run-1",
		SCPID:     "SCP-TEST",
		Research:  sampleResearchForStructurer(),
		Structure: sampleStructurerOutput(),
		Narration: &script,
	}
}

func samplePostReviewerCriticState(t *testing.T) *PipelineState {
	t.Helper()
	state := sampleCriticState(t)

	var visual domain.VisualScript
	if err := json.Unmarshal(testutil.LoadFixture(t, filepath.Join("contracts", "visual_breakdown.sample.json")), &visual); err != nil {
		t.Fatalf("unmarshal visual: %v", err)
	}
	var review domain.ReviewReport
	if err := json.Unmarshal(testutil.LoadFixture(t, filepath.Join("contracts", "reviewer_report.sample.json")), &review); err != nil {
		t.Fatalf("unmarshal review: %v", err)
	}
	var postWriter domain.CriticCheckpointReport
	if err := json.Unmarshal(testutil.LoadFixture(t, filepath.Join("contracts", "critic_post_writer.sample.json")), &postWriter); err != nil {
		t.Fatalf("unmarshal post_writer: %v", err)
	}

	state.VisualScript = &visual
	state.Review = &review
	state.Critic = &domain.CriticOutput{PostWriter: &postWriter}
	return state
}
