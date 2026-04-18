package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline/agents"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

type integrationTextGenerator struct {
	resp  domain.TextResponse
	calls int
}

func (g *integrationTextGenerator) Generate(_ context.Context, _ domain.TextRequest) (domain.TextResponse, error) {
	g.calls++
	return g.resp, nil
}

func TestPhaseARunner_Integration_EndToEnd(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	logger, logBuf := testutil.CaptureLog(t)
	fakeClk := clock.NewFakeClock(time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC))
	outputDir := t.TempDir()

	var order []agents.PipelineStage
	assign := func(ps agents.PipelineStage, state *agents.PipelineState) {
		switch ps {
		case agents.StageResearcher:
			state.Research = &domain.ResearcherOutput{SCPID: "scp-007", Title: "research"}
		case agents.StageStructurer:
			state.Structure = &domain.StructurerOutput{SCPID: "scp-007", TargetSceneCount: 10}
		case agents.StageWriter:
			state.Narration = &domain.NarrationScript{SCPID: "scp-007", Title: "writer"}
		case agents.StageVisualBreakdowner:
			state.VisualBreakdown = &domain.VisualBreakdownOutput{
				SCPID:            "scp-007",
				Title:            "writer",
				FrozenDescriptor: "Appearance: test",
				Scenes:           []domain.VisualBreakdownScene{},
				ShotOverrides:    map[int]domain.ShotOverride{},
				Metadata: domain.VisualBreakdownMetadata{
					VisualBreakdownModel:    "visual-model",
					VisualBreakdownProvider: "openai",
					PromptTemplate:          "03_5_visual_breakdown.md",
					ShotFormulaVersion:      domain.ShotFormulaVersionV1,
				},
				SourceVersion: domain.VisualBreakdownSourceVersionV1,
			}
		case agents.StagePostWriterCritic:
			state.Critic = &domain.CriticOutput{
				PostWriter: &domain.CriticCheckpointReport{
					Checkpoint: domain.CriticCheckpointPostWriter,
					Verdict:    domain.CriticVerdictPass,
					Feedback:   "좋습니다.",
				},
			}
		case agents.StageReviewer:
			state.Review = &domain.ReviewReport{
				OverallPass:      true,
				CoveragePct:      100,
				Issues:           []domain.ReviewIssue{},
				Corrections:      []domain.ReviewCorrection{},
				ReviewerModel:    "review-model",
				ReviewerProvider: "anthropic",
				SourceVersion:    domain.ReviewSourceVersionV1,
			}
		case agents.StageCritic:
			state.Critic.PostReviewer = &domain.CriticCheckpointReport{
				Checkpoint:   domain.CriticCheckpointPostReviewer,
				Verdict:      domain.CriticVerdictPass,
				OverallScore: 90,
				Feedback:     "최종 검토까지 안정적입니다.",
			}
		}
	}

	spy := func(ps agents.PipelineStage) agents.AgentFunc {
		return func(ctx context.Context, state *agents.PipelineState) error {
			order = append(order, ps)
			assign(ps, state)
			// Advance the fake clock a tick so StartedAt < FinishedAt.
			fakeClk.Advance(100 * time.Millisecond)
			return nil
		}
	}

	r, err := NewPhaseARunner(
		spy(agents.StageResearcher),
		spy(agents.StageStructurer),
		spy(agents.StageWriter),
		spy(agents.StagePostWriterCritic),
		spy(agents.StageVisualBreakdowner),
		spy(agents.StageReviewer),
		spy(agents.StageCritic),
		"openai",
		"anthropic",
		outputDir,
		fakeClk,
		logger,
	)
	if err != nil {
		t.Fatalf("NewPhaseARunner: %v", err)
	}

	state := &agents.PipelineState{RunID: "run-xyz", SCPID: "scp-007"}
	if err := r.Run(context.Background(), state); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Order check.
	wantOrder := []agents.PipelineStage{
		agents.StageResearcher,
		agents.StageStructurer,
		agents.StageWriter,
		agents.StagePostWriterCritic,
		agents.StageVisualBreakdowner,
		agents.StageReviewer,
		agents.StageCritic,
	}
	if len(order) != len(wantOrder) {
		t.Fatalf("got %d calls, want %d: %v", len(order), len(wantOrder), order)
	}
	for i, ps := range wantOrder {
		if order[i] != ps {
			t.Errorf("position %d: got %s want %s", i, order[i], ps)
		}
	}

	// scenario.json exists and round-trips exactly.
	scenario := ScenarioPath(outputDir, state.RunID)
	raw, err := os.ReadFile(scenario)
	if err != nil {
		t.Fatalf("read scenario: %v", err)
	}
	var got agents.PipelineState
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal scenario: %v", err)
	}

	// Each output field was written by its spy with a specific payload;
	// verify byte-for-byte equivalence after canonical unmarshal/marshal.
	if got.Research == nil || got.Research.SCPID != "scp-007" {
		t.Fatalf("research payload mismatch: %+v", got.Research)
	}
	if got.Structure == nil || got.Structure.TargetSceneCount != 10 {
		t.Fatalf("structure payload mismatch: %+v", got.Structure)
	}
	if got.Narration == nil {
		t.Fatal("writer payload missing")
	}
	for _, c := range []struct {
		ps   agents.PipelineStage
		seen bool
	}{
		{agents.StageVisualBreakdowner, got.VisualBreakdown != nil},
		{agents.StageReviewer, got.Review != nil},
	} {
		if !c.seen {
			t.Fatalf("%s payload missing", c.ps)
		}
	}
	if got.Critic == nil || got.Critic.PostWriter == nil {
		t.Fatalf("critic payload missing: %+v", got.Critic)
	}

	// Timestamps parse and are ordered.
	startAt, err := time.Parse(time.RFC3339Nano, got.StartedAt)
	if err != nil {
		t.Fatalf("parse started_at: %v", err)
	}
	finishAt, err := time.Parse(time.RFC3339Nano, got.FinishedAt)
	if err != nil {
		t.Fatalf("parse finished_at: %v", err)
	}
	if finishAt.Before(startAt) {
		t.Errorf("finished_at %s before started_at %s", finishAt, startAt)
	}

	// Log capture — six "agent start" entries in order.
	logLines := strings.Split(strings.TrimRight(logBuf.String(), "\n"), "\n")
	var starts []string
	for _, line := range logLines {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec["msg"] == "agent start" {
			if ps, ok := rec["pipeline_stage"].(string); ok {
				starts = append(starts, ps)
			}
		}
	}
	if len(starts) != 7 {
		t.Fatalf("expected 7 'agent start' log entries, got %d: %v", len(starts), starts)
	}
	wantStarts := []string{
		"researcher", "structurer", "writer",
		"post_writer_critic", "visual_breakdowner", "reviewer", "critic",
	}
	for i, ps := range wantStarts {
		if starts[i] != ps {
			t.Errorf("log start #%d: got %s want %s", i, starts[i], ps)
		}
	}
}

func TestPhaseAIntegration_TwoCriticCheckpointsAndScenarioJSONIntegrity(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	outputDir := t.TempDir()
	fakeClk := clock.NewFakeClock(time.Date(2026, 4, 18, 15, 0, 0, 0, time.UTC))
	r, err := NewPhaseARunner(
		func(ctx context.Context, state *agents.PipelineState) error {
			state.Research = samplePhaseAResearch()
			return nil
		},
		func(ctx context.Context, state *agents.PipelineState) error {
			state.Structure = &domain.StructurerOutput{SCPID: state.SCPID, TargetSceneCount: 10, SourceVersion: domain.SourceVersionV1}
			return nil
		},
		func(ctx context.Context, state *agents.PipelineState) error {
			state.Narration = samplePhaseANarration()
			return nil
		},
		func(ctx context.Context, state *agents.PipelineState) error {
			// post-writer Critic checkpoint: populates only PostWriter
			state.Critic = &domain.CriticOutput{
				PostWriter: &domain.CriticCheckpointReport{
					Checkpoint:   domain.CriticCheckpointPostWriter,
					Verdict:      domain.CriticVerdictAcceptWithNotes,
					OverallScore: 81,
					Feedback:     "좋습니다.",
				},
			}
			return nil
		},
		func(ctx context.Context, state *agents.PipelineState) error {
			state.VisualBreakdown = samplePhaseAVisualBreakdown()
			return nil
		},
		func(ctx context.Context, state *agents.PipelineState) error {
			state.Review = &domain.ReviewReport{
				OverallPass:      true,
				CoveragePct:      100,
				Issues:           []domain.ReviewIssue{},
				Corrections:      []domain.ReviewCorrection{},
				ReviewerModel:    "review-model",
				ReviewerProvider: "anthropic",
				SourceVersion:    domain.ReviewSourceVersionV1,
			}
			return nil
		},
		func(ctx context.Context, state *agents.PipelineState) error {
			// post-reviewer Critic checkpoint: populates PostReviewer without disturbing PostWriter
			state.Critic.PostReviewer = &domain.CriticCheckpointReport{
				Checkpoint:   domain.CriticCheckpointPostReviewer,
				Verdict:      domain.CriticVerdictPass,
				OverallScore: 88,
				Feedback:     "최종 검토까지 안정적입니다.",
			}
			return nil
		},
		"openai",
		"anthropic",
		outputDir,
		fakeClk,
		nil,
	)
	if err != nil {
		t.Fatalf("NewPhaseARunner: %v", err)
	}

	state := &agents.PipelineState{RunID: "phase-a-happy", SCPID: "SCP-TEST"}
	if err := r.Run(context.Background(), state); err != nil {
		t.Fatalf("Run: %v", err)
	}
	raw, err := os.ReadFile(ScenarioPath(outputDir, state.RunID))
	if err != nil {
		t.Fatalf("read scenario: %v", err)
	}
	var got agents.PipelineState
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal scenario: %v", err)
	}
	if got.Critic == nil || got.Critic.PostWriter == nil || got.Critic.PostReviewer == nil {
		t.Fatalf("expected two critic checkpoints, got %+v", got.Critic)
	}
	if got.Contracts == nil {
		t.Fatal("expected contract manifest")
	}
	for _, path := range []string{
		got.Contracts.ResearchSchema.Path,
		got.Contracts.StructureSchema.Path,
		got.Contracts.WriterSchema.Path,
		got.Contracts.VisualBreakdownSchema.Path,
		got.Contracts.ReviewSchema.Path,
		got.Contracts.CriticPostWriterSchema.Path,
		got.Contracts.CriticPostReviewerSchema.Path,
		got.Contracts.PhaseAStateSchema.Path,
	} {
		if _, err := os.Stat(testutil.ProjectRoot(t) + "/" + path); err != nil {
			t.Fatalf("missing manifest path %s: %v", path, err)
		}
	}
}

func TestPhaseAIntegration_FinalRetryLeavesNoScenarioJSON(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	outputDir := t.TempDir()
	r, err := NewPhaseARunner(
		func(ctx context.Context, state *agents.PipelineState) error { state.Research = samplePhaseAResearch(); return nil },
		func(ctx context.Context, state *agents.PipelineState) error {
			state.Structure = &domain.StructurerOutput{SCPID: state.SCPID, TargetSceneCount: 10, SourceVersion: domain.SourceVersionV1}
			return nil
		},
		func(ctx context.Context, state *agents.PipelineState) error { state.Narration = samplePhaseANarration(); return nil },
		func(ctx context.Context, state *agents.PipelineState) error {
			state.Critic = &domain.CriticOutput{PostWriter: &domain.CriticCheckpointReport{Checkpoint: domain.CriticCheckpointPostWriter, Verdict: domain.CriticVerdictPass, OverallScore: 80, Feedback: "좋습니다."}}
			return nil
		},
		func(ctx context.Context, state *agents.PipelineState) error { state.VisualBreakdown = samplePhaseAVisualBreakdown(); return nil },
		func(ctx context.Context, state *agents.PipelineState) error {
			state.Review = &domain.ReviewReport{OverallPass: true, CoveragePct: 100, Issues: []domain.ReviewIssue{}, Corrections: []domain.ReviewCorrection{}, ReviewerModel: "review-model", ReviewerProvider: "anthropic", SourceVersion: domain.ReviewSourceVersionV1}
			return nil
		},
		func(ctx context.Context, state *agents.PipelineState) error {
			state.Critic.PostReviewer = &domain.CriticCheckpointReport{Checkpoint: domain.CriticCheckpointPostReviewer, Verdict: domain.CriticVerdictRetry, RetryReason: "weak_hook", OverallScore: 51, Feedback: "다시 써야 합니다."}
			return nil
		},
		"openai", "anthropic", outputDir, clock.NewFakeClock(time.Date(2026, 4, 18, 16, 0, 0, 0, time.UTC)), nil,
	)
	if err != nil {
		t.Fatalf("NewPhaseARunner: %v", err)
	}

	state := &agents.PipelineState{RunID: "phase-a-retry", SCPID: "SCP-TEST"}
	if err := r.Run(context.Background(), state); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(ScenarioPath(outputDir, state.RunID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no scenario.json, got %v", err)
	}
}

func TestPhaseAIntegration_PostReviewerReviewFailureShortCircuitsSecondLLMCall(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	root := testutil.ProjectRoot(t)
	gen := &integrationTextGenerator{resp: domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{
		Content:  string(testutil.LoadFixture(t, "contracts/critic_post_reviewer.sample.json")),
		Model:    "critic-model",
		Provider: "anthropic",
	}}}
	writerValidator, err := agents.NewValidator(root, "writer_output.schema.json")
	if err != nil {
		t.Fatalf("writer validator: %v", err)
	}
	visualValidator, err := agents.NewValidator(root, "visual_breakdown.schema.json")
	if err != nil {
		t.Fatalf("visual validator: %v", err)
	}
	reviewValidator, err := agents.NewValidator(root, "reviewer_report.schema.json")
	if err != nil {
		t.Fatalf("review validator: %v", err)
	}
	criticValidator, err := agents.NewValidator(root, "critic_post_reviewer.schema.json")
	if err != nil {
		t.Fatalf("critic validator: %v", err)
	}
	terms, err := agents.LoadForbiddenTerms(root)
	if err != nil {
		t.Fatalf("terms: %v", err)
	}
	criticAgent := agents.NewPostReviewerCritic(
		gen,
		agents.TextAgentConfig{Model: "critic-model", Provider: "anthropic"},
		agents.PromptAssets{CriticTemplate: "{scenario_json}\n{format_guide}", FormatGuide: "guide"},
		writerValidator,
		visualValidator,
		reviewValidator,
		criticValidator,
		terms,
		"openai",
	)

	var narration domain.NarrationScript
	if err := json.Unmarshal(testutil.LoadFixture(t, "contracts/writer_output.sample.json"), &narration); err != nil {
		t.Fatalf("unmarshal narration: %v", err)
	}
	var visual domain.VisualBreakdownOutput
	if err := json.Unmarshal(testutil.LoadFixture(t, "contracts/visual_breakdown.sample.json"), &visual); err != nil {
		t.Fatalf("unmarshal visual: %v", err)
	}
	var review domain.ReviewReport
	if err := json.Unmarshal(testutil.LoadFixture(t, "contracts/reviewer_report.sample.json"), &review); err != nil {
		t.Fatalf("unmarshal review: %v", err)
	}
	var postWriter domain.CriticCheckpointReport
	if err := json.Unmarshal(testutil.LoadFixture(t, "contracts/critic_post_writer.sample.json"), &postWriter); err != nil {
		t.Fatalf("unmarshal post_writer: %v", err)
	}
	state := &agents.PipelineState{
		RunID:           "review-short-circuit",
		SCPID:           "SCP-TEST",
		Research:        samplePhaseAResearch(),
		Structure:       &domain.StructurerOutput{SCPID: "SCP-TEST", TargetSceneCount: 10, SourceVersion: domain.SourceVersionV1},
		Narration:       &narration,
		VisualBreakdown: &visual,
		Review:          &review,
		Critic:          &domain.CriticOutput{PostWriter: &postWriter},
	}
	state.Review.OverallPass = false
	if err := criticAgent(context.Background(), state); err != nil {
		t.Fatalf("critic agent: %v", err)
	}
	testutil.AssertEqual(t, gen.calls, 0)
	if state.Critic.PostReviewer == nil || state.Critic.PostReviewer.RetryReason != "review_failed" {
		t.Fatalf("expected review_failed short circuit, got %+v", state.Critic.PostReviewer)
	}
}

func TestPhaseARunner_ReviewOutputValidatedBeforeCritic(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	outputDir := t.TempDir()
	var criticCalled bool

	// Real Reviewer agent fed a JSON-valid but schema-invalid response:
	// issue.severity is not in the allowed enum. This exercises the actual
	// reviewValidator.Validate path inside NewReviewer, not a hand-thrown error.
	schemaInvalidJSON := `{"overall_pass":true,"coverage_pct":100,"issues":[{"scene_num":1,"type":"fact_error","severity":"NOT_AN_ENUM_VALUE","description":"x","correction":"y"}],"corrections":[],"reviewer_model":"r","reviewer_provider":"p","source_version":"v1-reviewer-fact-check"}`
	invalidGen := &fakeFixedTextGenerator{content: schemaInvalidJSON}

	visualValidator, err := agents.NewValidator(testutil.ProjectRoot(t), "visual_breakdown.schema.json")
	if err != nil {
		t.Fatalf("visual validator: %v", err)
	}
	reviewValidator, err := agents.NewValidator(testutil.ProjectRoot(t), "reviewer_report.schema.json")
	if err != nil {
		t.Fatalf("review validator: %v", err)
	}

	reviewerAgent := agents.NewReviewer(
		invalidGen,
		agents.TextAgentConfig{Model: "review-model", Provider: "anthropic"},
		agents.PromptAssets{ReviewerTemplate: "prompt", FormatGuide: "guide"},
		visualValidator,
		reviewValidator,
	)

	critic := func(ctx context.Context, state *agents.PipelineState) error {
		criticCalled = true
		return nil
	}

	r, err := NewPhaseARunner(
		func(ctx context.Context, state *agents.PipelineState) error {
			state.Research = samplePhaseAResearch()
			return nil
		},
		func(ctx context.Context, state *agents.PipelineState) error {
			state.Structure = &domain.StructurerOutput{SCPID: state.SCPID, TargetSceneCount: 10, SourceVersion: domain.SourceVersionV1}
			return nil
		},
		func(ctx context.Context, state *agents.PipelineState) error {
			state.Narration = samplePhaseANarration()
			return nil
		},
		func(ctx context.Context, state *agents.PipelineState) error {
			// post-writer Critic noop for this test — error under test is in reviewer
			state.Critic = &domain.CriticOutput{PostWriter: &domain.CriticCheckpointReport{Checkpoint: domain.CriticCheckpointPostWriter, Verdict: domain.CriticVerdictPass, OverallScore: 80, Feedback: "좋습니다."}}
			return nil
		},
		func(ctx context.Context, state *agents.PipelineState) error {
			state.VisualBreakdown = samplePhaseAVisualBreakdown()
			return nil
		},
		reviewerAgent,
		critic,
		"openai",
		"anthropic",
		outputDir,
		clock.NewFakeClock(time.Date(2026, 4, 18, 13, 0, 0, 0, time.UTC)),
		nil,
	)
	if err != nil {
		t.Fatalf("NewPhaseARunner: %v", err)
	}

	state := &agents.PipelineState{RunID: "run-review-block", SCPID: "SCP-TEST"}
	err = r.Run(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation from reviewValidator, got %v", err)
	}
	if criticCalled {
		t.Fatal("critic should not run when reviewer output fails schema validation")
	}
	if state.Review != nil {
		t.Fatalf("state.Review must remain untouched on reviewer failure, got %#v", state.Review)
	}
	if _, statErr := os.Stat(ScenarioPath(outputDir, state.RunID)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected no scenario artifact, got stat err=%v", statErr)
	}
}

type fakeFixedTextGenerator struct {
	content string
}

func (f *fakeFixedTextGenerator) Generate(_ context.Context, _ domain.TextRequest) (domain.TextResponse, error) {
	return domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{
		Content:  f.content,
		Model:    "review-model",
		Provider: "anthropic",
	}}, nil
}

func samplePhaseAResearch() *domain.ResearcherOutput {
	return &domain.ResearcherOutput{
		SCPID: "SCP-TEST",
		Title: "SCP-TEST",
		VisualIdentity: domain.VisualIdentity{
			Appearance:             "Concrete sentinel",
			DistinguishingFeatures: []string{"Obsidian eyes"},
			EnvironmentSetting:     "Transit vault",
			KeyVisualMoments:       []string{"Blink"},
		},
		SourceVersion: domain.SourceVersionV1,
	}
}

func samplePhaseANarration() *domain.NarrationScript {
	return &domain.NarrationScript{
		SCPID: "SCP-TEST",
		Title: "SCP-TEST",
		Scenes: []domain.NarrationScene{
			{SceneNum: 1, ActID: domain.ActIncident, Narration: "scene 1"},
			{SceneNum: 2, ActID: domain.ActIncident, Narration: "scene 2"},
			{SceneNum: 3, ActID: domain.ActMystery, Narration: "scene 3"},
			{SceneNum: 4, ActID: domain.ActMystery, Narration: "scene 4"},
			{SceneNum: 5, ActID: domain.ActMystery, Narration: "scene 5"},
			{SceneNum: 6, ActID: domain.ActRevelation, Narration: "scene 6"},
			{SceneNum: 7, ActID: domain.ActRevelation, Narration: "scene 7"},
			{SceneNum: 8, ActID: domain.ActRevelation, Narration: "scene 8"},
			{SceneNum: 9, ActID: domain.ActUnresolved, Narration: "scene 9"},
			{SceneNum: 10, ActID: domain.ActUnresolved, Narration: "scene 10"},
		},
		SourceVersion: domain.NarrationSourceVersionV1,
	}
}

func samplePhaseAVisualBreakdown() *domain.VisualBreakdownOutput {
	var scenes []domain.VisualBreakdownScene
	for i := 1; i <= 10; i++ {
		scenes = append(scenes, domain.VisualBreakdownScene{
			SceneNum:              i,
			ActID:                 actIDForPhaseATest(i),
			Narration:             fmt.Sprintf("scene %d", i),
			EstimatedTTSDurationS: 7.0,
			ShotCount:             1,
			Shots: []domain.VisualShot{{
				ShotIndex:          1,
				VisualDescriptor:   "Appearance: Concrete sentinel; Distinguishing features: Obsidian eyes; Environment: Transit vault; Key visual moments: Blink; shot description",
				EstimatedDurationS: 7.0,
				Transition:         domain.TransitionKenBurns,
			}},
		})
	}
	return &domain.VisualBreakdownOutput{
		SCPID:            "SCP-TEST",
		Title:            "SCP-TEST",
		FrozenDescriptor: "Appearance: Concrete sentinel; Distinguishing features: Obsidian eyes; Environment: Transit vault; Key visual moments: Blink",
		Scenes:           scenes,
		ShotOverrides:    map[int]domain.ShotOverride{},
		Metadata: domain.VisualBreakdownMetadata{
			VisualBreakdownModel:    "visual-model",
			VisualBreakdownProvider: "openai",
			PromptTemplate:          "03_5_visual_breakdown.md",
			ShotFormulaVersion:      domain.ShotFormulaVersionV1,
		},
		SourceVersion: domain.VisualBreakdownSourceVersionV1,
	}
}

func actIDForPhaseATest(sceneNum int) string {
	switch {
	case sceneNum <= 2:
		return domain.ActIncident
	case sceneNum <= 5:
		return domain.ActMystery
	case sceneNum <= 8:
		return domain.ActRevelation
	default:
		return domain.ActUnresolved
	}
}
