package agents

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestReviewer_Run_Happy(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &fakeTextGenerator{resp: domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{
		Content:  string(testutil.LoadFixture(t, filepath.Join("contracts", "reviewer_report.sample.json"))),
		Model:    "review-model",
		Provider: "anthropic",
	}}}
	state := sampleReviewerState()
	err := NewReviewer(gen, TextAgentConfig{Model: "review-model", Provider: "anthropic"}, sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), mustValidator(t, "reviewer_report.schema.json"))(context.Background(), state)
	if err != nil {
		t.Fatalf("Reviewer: %v", err)
	}
	if state.Review == nil {
		t.Fatal("expected review output")
	}
	testutil.AssertEqual(t, state.Review.ReviewerModel, "review-model")
}

func TestReviewer_Run_InvalidVisualScriptBlockedBeforeLLM(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &fakeTextGenerator{}
	state := sampleReviewerState()
	state.VisualScript.ShotOverrides = nil
	err := NewReviewer(gen, TextAgentConfig{Model: "review-model", Provider: "anthropic"}, sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), mustValidator(t, "reviewer_report.schema.json"))(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	testutil.AssertEqual(t, gen.calls, 0)
}

func TestReviewer_Run_InvalidJSON(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &fakeTextGenerator{resp: domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{Content: "not-json"}}}
	state := sampleReviewerState()
	err := NewReviewer(gen, TextAgentConfig{Model: "review-model", Provider: "anthropic"}, sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), mustValidator(t, "reviewer_report.schema.json"))(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestReviewer_Run_SchemaViolation(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &fakeTextGenerator{resp: domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{
		Content: `{"overall_pass":true,"coverage_pct":85,"issues":[{"scene_num":1,"type":"fact_error","severity":"bad","description":"x","correction":"y"}],"corrections":[],"reviewer_model":"x","reviewer_provider":"y","source_version":"v1-reviewer-fact-check"}`,
	}}}
	state := sampleReviewerState()
	err := NewReviewer(gen, TextAgentConfig{Model: "review-model", Provider: "anthropic"}, sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), mustValidator(t, "reviewer_report.schema.json"))(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestReviewer_Run_CriticalIssueForcesFail(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &fakeTextGenerator{resp: domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{
		Content: `{"overall_pass":true,"coverage_pct":50,"issues":[{"scene_num":3,"type":"fact_error","severity":"critical","description":"bad","correction":"fix"}],"corrections":[],"reviewer_model":"x","reviewer_provider":"y","source_version":"v1-reviewer-fact-check"}`,
	}}}
	state := sampleReviewerState()
	err := NewReviewer(gen, TextAgentConfig{Model: "review-model", Provider: "anthropic"}, sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), mustValidator(t, "reviewer_report.schema.json"))(context.Background(), state)
	if err != nil {
		t.Fatalf("Reviewer: %v", err)
	}
	if state.Review.OverallPass {
		t.Fatal("expected critical issue to force fail")
	}
	var syntheticCount int
	for _, issue := range state.Review.Issues {
		if issue.Severity == "info" && issue.Type == domain.ReviewIssueConsistencyIssue {
			syntheticCount++
			testutil.AssertEqual(t, issue.SceneNum, 3)
		}
	}
	if syntheticCount != 1 {
		t.Fatalf("expected exactly 1 synthetic audit issue, got %d", syntheticCount)
	}
}

func TestReviewer_Run_DoesNotMutateStateOnFailure(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &fakeTextGenerator{resp: domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{Content: "not-json"}}}
	state := sampleReviewerState()
	sentinel := &domain.ReviewReport{OverallPass: true, ReviewerModel: "SENTINEL"}
	state.Review = sentinel
	err := NewReviewer(gen, TextAgentConfig{Model: "review-model", Provider: "anthropic"}, sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), mustValidator(t, "reviewer_report.schema.json"))(context.Background(), state)
	if err == nil {
		t.Fatal("expected error")
	}
	if state.Review != sentinel {
		t.Fatal("reviewer mutated state on failure")
	}
	if state.Review.ReviewerModel != "SENTINEL" {
		t.Fatalf("sentinel fields were overwritten in place: %#v", state.Review)
	}
}

func sampleReviewerState() *PipelineState {
	state := sampleVisualBreakdownState4Acts()
	state.VisualScript = sampleVisualScriptForReview(state.Narration)
	return state
}

// sampleVisualScriptForReview builds a v2 VisualScript whose Acts/Shots
// 1:1-mirror the supplied NarrationScript's Acts/Beats with frozen-descriptor
// continuity and Korean-friendly Ken-Burns transitions. Used by reviewer +
// critic precheck tests that need a schema-valid v2 visual_script in state.
func sampleVisualScriptForReview(narration *domain.NarrationScript) *domain.VisualScript {
	acts := make([]domain.VisualAct, 0, len(narration.Acts))
	for _, srcAct := range narration.Acts {
		shots := make([]domain.VisualShot, 0, len(srcAct.Beats))
		for i, beat := range srcAct.Beats {
			shots = append(shots, domain.VisualShot{
				ShotIndex:          i + 1,
				VisualDescriptor:   "Appearance: Concrete sentinel; Distinguishing features: Obsidian eyes; Environment: Transit vault; Key visual moments: Blink; shot description",
				EstimatedDurationS: 7.0,
				Transition:         domain.TransitionKenBurns,
				NarrationAnchor:    beat,
			})
		}
		acts = append(acts, domain.VisualAct{ActID: srcAct.ActID, Shots: shots})
	}
	return &domain.VisualScript{
		SCPID:            "SCP-TEST",
		Title:            "SCP-TEST",
		FrozenDescriptor: "Appearance: Concrete sentinel; Distinguishing features: Obsidian eyes; Environment: Transit vault; Key visual moments: Blink",
		Acts:             acts,
		ShotOverrides:    map[int]domain.ShotOverride{},
		Metadata: domain.VisualBreakdownMetadata{
			VisualBreakdownModel:    "visual-model",
			VisualBreakdownProvider: "openai",
			PromptTemplate:          "03_5_visual_breakdown.md",
			ShotFormulaVersion:      domain.ShotFormulaVersionV1,
		},
		SourceVersion: domain.VisualBreakdownSourceVersionV2,
	}
}
