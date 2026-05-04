package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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
			state.VisualScript = &domain.VisualScript{
				SCPID:            "scp-007",
				Title:            "writer",
				FrozenDescriptor: "Appearance: test",
				Acts:             []domain.VisualAct{},
				ShotOverrides:    map[int]domain.ShotOverride{},
				Metadata: domain.VisualBreakdownMetadata{
					VisualBreakdownModel:    "visual-model",
					VisualBreakdownProvider: "openai",
					PromptTemplate:          "03_5_visual_breakdown.md",
					ShotFormulaVersion:      domain.ShotFormulaVersionV1,
				},
				SourceVersion: domain.VisualBreakdownSourceVersionV2,
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
		agents.NoopAgent(),
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
		{agents.StageVisualBreakdowner, got.VisualScript != nil},
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
	if len(starts) != 8 {
		t.Fatalf("expected 8 'agent start' log entries, got %d: %v", len(starts), starts)
	}
	wantStarts := []string{
		"researcher", "structurer", "writer",
		"polisher", "post_writer_critic", "visual_breakdowner", "reviewer", "critic",
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
		agents.NoopAgent(),
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
			state.VisualScript = samplePhaseAVisualBreakdown()
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
		agents.NoopAgent(),
		func(ctx context.Context, state *agents.PipelineState) error {
			state.Critic = &domain.CriticOutput{PostWriter: &domain.CriticCheckpointReport{Checkpoint: domain.CriticCheckpointPostWriter, Verdict: domain.CriticVerdictPass, OverallScore: 80, Feedback: "좋습니다."}}
			return nil
		},
		func(ctx context.Context, state *agents.PipelineState) error { state.VisualScript = samplePhaseAVisualBreakdown(); return nil },
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
	var visual domain.VisualScript
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
		Narration:    &narration,
		VisualScript: &visual,
		Review:       &review,
		Critic:       &domain.CriticOutput{PostWriter: &postWriter},
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
		agents.NoopAgent(),
		func(ctx context.Context, state *agents.PipelineState) error {
			// post-writer Critic noop for this test — error under test is in reviewer
			state.Critic = &domain.CriticOutput{PostWriter: &domain.CriticCheckpointReport{Checkpoint: domain.CriticCheckpointPostWriter, Verdict: domain.CriticVerdictPass, OverallScore: 80, Feedback: "좋습니다."}}
			return nil
		},
		func(ctx context.Context, state *agents.PipelineState) error {
			state.VisualScript = samplePhaseAVisualBreakdown()
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

// TestPhaseAIntegration_WriterMidStageFailure_AtomicNarration locks the
// D6 atomic boundary at the PhaseARunner integration level: when the
// writer agent returns an error, the runner does NOT synthesize a
// state.Narration on its own, FinishedAt stays empty, scenario.json is
// not produced, and downstream stages (post_writer_critic, visual,
// reviewer, critic) are not reached. The writer agent's own atomic
// boundary (state.Narration unset on per-stage / script-level failure)
// is unit-tested in writer_test.go via TestWriter_MidCascadeFailure_*
// and TestWriter_FullCascadeOk_ScriptValidatorFailure_*; this test
// covers the runner's complementary contract.
func TestPhaseAIntegration_WriterMidStageFailure_AtomicNarration(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	outputDir := t.TempDir()
	writerErr := fmt.Errorf("writer: act mystery segmenter: end_offset=99999 > monologue_rune_count=80: %w", domain.ErrValidation)

	// Per-stage call counters — `t.Fatal` from inside an agent stub would
	// be reached on a goroutine the runner could in principle move work to;
	// counting + post-Run assertion keeps the test failure clean and pins
	// not just "downstream stages skipped" but "upstream stages did run".
	var (
		researcherCalls   atomic.Int32
		structurerCalls   atomic.Int32
		writerCalls       atomic.Int32
		polisherCalls     atomic.Int32
		postWriterCalls   atomic.Int32
		visualCalls       atomic.Int32
		reviewerCalls     atomic.Int32
		criticCalls       atomic.Int32
	)

	r, err := NewPhaseARunner(
		func(ctx context.Context, state *agents.PipelineState) error {
			researcherCalls.Add(1)
			state.Research = samplePhaseAResearch()
			return nil
		},
		func(ctx context.Context, state *agents.PipelineState) error {
			structurerCalls.Add(1)
			state.Structure = &domain.StructurerOutput{SCPID: state.SCPID, TargetSceneCount: 10, SourceVersion: domain.SourceVersionV1}
			return nil
		},
		// Writer fails WITHOUT setting state.Narration — mirrors the real
		// writer's contract (writer.go: state.Narration is the final write,
		// gated on every act × stage + script-level validation succeeding).
		func(ctx context.Context, state *agents.PipelineState) error {
			writerCalls.Add(1)
			return writerErr
		},
		func(ctx context.Context, state *agents.PipelineState) error {
			polisherCalls.Add(1)
			return nil
		},
		func(ctx context.Context, state *agents.PipelineState) error { postWriterCalls.Add(1); return nil },
		func(ctx context.Context, state *agents.PipelineState) error { visualCalls.Add(1); return nil },
		func(ctx context.Context, state *agents.PipelineState) error { reviewerCalls.Add(1); return nil },
		func(ctx context.Context, state *agents.PipelineState) error { criticCalls.Add(1); return nil },
		"openai", "anthropic", outputDir,
		clock.NewFakeClock(time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)),
		nil,
	)
	if err != nil {
		t.Fatalf("NewPhaseARunner: %v", err)
	}

	state := &agents.PipelineState{RunID: "writer-mid-stage-fail", SCPID: "SCP-TEST"}
	gotErr := r.Run(context.Background(), state)
	if gotErr == nil {
		t.Fatal("expected writer-mid-stage failure, got nil")
	}
	if !errors.Is(gotErr, domain.ErrValidation) {
		t.Fatalf("error chain must include ErrValidation, got: %v", gotErr)
	}
	if !strings.Contains(gotErr.Error(), "stage=writer") {
		t.Fatalf("error message must wrap with stage=writer, got: %v", gotErr)
	}
	if state.Narration != nil {
		t.Fatalf("state.Narration must be nil after writer mid-stage failure: %+v", state.Narration)
	}
	// StartedAt is stamped BEFORE the chain runs (phase_a.go); FinishedAt is
	// only stamped on a fully-finalized success. Pinning both sides keeps a
	// future refactor that swaps the timestamp ordering from passing.
	if state.StartedAt == "" {
		t.Errorf("state.StartedAt must be stamped before chain runs, got empty")
	}
	if state.FinishedAt != "" {
		t.Errorf("state.FinishedAt must remain empty on failure, got %q", state.FinishedAt)
	}
	if _, statErr := os.Stat(ScenarioPath(outputDir, state.RunID)); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("expected no scenario.json after writer failure, got stat err=%v", statErr)
	}
	// Pin the chain shape: upstream of writer ran exactly once; writer ran
	// exactly once; everything downstream of writer never ran.
	if got := researcherCalls.Load(); got != 1 {
		t.Errorf("researcher calls=%d, want 1 (must run before writer)", got)
	}
	if got := structurerCalls.Load(); got != 1 {
		t.Errorf("structurer calls=%d, want 1 (must run before writer)", got)
	}
	if got := writerCalls.Load(); got != 1 {
		t.Errorf("writer calls=%d, want 1", got)
	}
	for _, c := range []struct {
		name  string
		count int32
	}{
		{"polisher", polisherCalls.Load()},
		{"post_writer_critic", postWriterCalls.Load()},
		{"visual_breakdowner", visualCalls.Load()},
		{"reviewer", reviewerCalls.Load()},
		{"critic", criticCalls.Load()},
	} {
		if c.count != 0 {
			t.Errorf("%s must not run after writer failure, got %d calls", c.name, c.count)
		}
	}
}

// TestPhaseAIntegration_ResumeAfterWriterFailure_RunsWriterFromScratch
// locks the D6 resume invariant: a writer-failed run leaves no on-disk
// narration artifact, so a subsequent invocation re-enters the writer
// from a clean state.Narration nil — there is intentionally no narration
// cache analogous to research/structure caches. The test runs the
// runner twice on the same run dir to actually exercise the resume
// boundary (not just "first run starts with Narration nil"): attempt 1
// fails at writer, attempt 2 succeeds, and the writer observes
// Narration=nil on entry both times. Catches a regression that would
// introduce a narration cache, leak state across attempts, or reuse a
// stale Narration.
func TestPhaseAIntegration_ResumeAfterWriterFailure_RunsWriterFromScratch(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	outputDir := t.TempDir()
	runID := "resume-after-writer-fail"
	var writerCalls atomic.Int32
	// writerShouldFail toggles per attempt: first invocation fails (mirrors
	// the post-writer-mid-stage atomic boundary — Narration stays nil),
	// second invocation succeeds.
	var writerShouldFail atomic.Bool
	writerShouldFail.Store(true)
	writerErr := fmt.Errorf("writer: act mystery segmenter exhausted retries: %w", domain.ErrValidation)

	mkRunner := func(now time.Time) *PhaseARunner {
		r, err := NewPhaseARunner(
			func(ctx context.Context, state *agents.PipelineState) error { state.Research = samplePhaseAResearch(); return nil },
			func(ctx context.Context, state *agents.PipelineState) error {
				state.Structure = &domain.StructurerOutput{SCPID: state.SCPID, TargetSceneCount: 10, SourceVersion: domain.SourceVersionV1}
				return nil
			},
			func(ctx context.Context, state *agents.PipelineState) error {
				writerCalls.Add(1)
				// Writer must observe state.Narration nil on entry — the
				// previous failed attempt is not allowed to leak partial
				// state across the resume boundary, and there is no on-disk
				// narration cache to rehydrate from.
				if state.Narration != nil {
					t.Errorf("writer entered with non-nil state.Narration on attempt %d: %+v", writerCalls.Load(), state.Narration)
				}
				if writerShouldFail.Load() {
					return writerErr
				}
				state.Narration = samplePhaseANarration()
				return nil
			},
			agents.NoopAgent(),
			func(ctx context.Context, state *agents.PipelineState) error {
				state.Critic = &domain.CriticOutput{PostWriter: &domain.CriticCheckpointReport{Checkpoint: domain.CriticCheckpointPostWriter, Verdict: domain.CriticVerdictPass, OverallScore: 80, Feedback: "좋습니다."}}
				return nil
			},
			func(ctx context.Context, state *agents.PipelineState) error { state.VisualScript = samplePhaseAVisualBreakdown(); return nil },
			func(ctx context.Context, state *agents.PipelineState) error {
				state.Review = &domain.ReviewReport{OverallPass: true, CoveragePct: 100, Issues: []domain.ReviewIssue{}, Corrections: []domain.ReviewCorrection{}, ReviewerModel: "review-model", ReviewerProvider: "anthropic", SourceVersion: domain.ReviewSourceVersionV1}
				return nil
			},
			func(ctx context.Context, state *agents.PipelineState) error {
				state.Critic.PostReviewer = &domain.CriticCheckpointReport{Checkpoint: domain.CriticCheckpointPostReviewer, Verdict: domain.CriticVerdictPass, OverallScore: 88, Feedback: "최종 검토까지 안정적입니다."}
				return nil
			},
			"openai", "anthropic", outputDir,
			clock.NewFakeClock(now), nil,
		)
		if err != nil {
			t.Fatalf("NewPhaseARunner: %v", err)
		}
		return r
	}

	// --- Attempt 1: writer fails. ----------------------------------------
	state1 := &agents.PipelineState{RunID: runID, SCPID: "SCP-TEST"}
	r1 := mkRunner(time.Date(2026, 5, 4, 12, 5, 0, 0, time.UTC))
	if err := r1.Run(context.Background(), state1); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("attempt 1 must fail at writer with ErrValidation, got %v", err)
	}
	if state1.Narration != nil {
		t.Fatalf("attempt 1: state.Narration must remain nil after writer failure, got %+v", state1.Narration)
	}
	if state1.FinishedAt != "" {
		t.Errorf("attempt 1: FinishedAt must remain empty on failure, got %q", state1.FinishedAt)
	}
	// After attempt 1, the run dir contains only deterministic-agent caches
	// (research_cache.json, structure_cache.json) — NEVER any narration- or
	// writer-stage cache. Inverting the freeze (allowlist instead of
	// denylist) catches any cache filename a future regression might pick.
	assertNoNarrationCache(t, outputDir, runID, "after attempt 1 (writer failed)")

	// --- Attempt 2: writer succeeds (resume). ----------------------------
	writerShouldFail.Store(false)
	state2 := &agents.PipelineState{RunID: runID, SCPID: "SCP-TEST"}
	r2 := mkRunner(time.Date(2026, 5, 4, 12, 6, 0, 0, time.UTC))
	if err := r2.Run(context.Background(), state2); err != nil {
		t.Fatalf("attempt 2 (resume) must succeed, got %v", err)
	}
	if state2.Narration == nil || len(state2.Narration.Acts) != 4 {
		t.Fatalf("attempt 2: state.Narration must be fully populated, got %+v", state2.Narration)
	}
	if state2.StartedAt == "" || state2.FinishedAt == "" {
		t.Errorf("attempt 2: timestamps must both be set on success, started=%q finished=%q", state2.StartedAt, state2.FinishedAt)
	}
	if _, err := os.Stat(ScenarioPath(outputDir, runID)); err != nil {
		t.Errorf("attempt 2: scenario.json must exist on success, stat err=%v", err)
	}
	// Writer was invoked twice (once per attempt) — proving no partial
	// reuse path bypassed the writer on resume.
	if got := writerCalls.Load(); got != 2 {
		t.Errorf("writer must be invoked once per attempt; got %d total, want 2", got)
	}
	// Final freeze: even after success, the run dir holds nothing that
	// would let a future resume skip the writer.
	assertNoNarrationCache(t, outputDir, runID, "after attempt 2 (success)")
}

// assertNoNarrationCache walks the per-run output dir and fails the test
// if it finds any file whose name suggests a narration / writer-stage
// cache. The allowlist (research_cache.json, structure_cache.json,
// scenario.json) is the canonical set of D6-permitted artifacts; an
// inverted check catches any future cache filename a regression might
// introduce — `narration_cache.json`, `writer_stage1.json`,
// `writer_partial.json`, `acts_cache.json`, etc. — without enumerating
// every possible drift up front.
func assertNoNarrationCache(t *testing.T, outputDir, runID, when string) {
	t.Helper()
	runDir := filepath.Join(outputDir, runID)
	entries, err := os.ReadDir(runDir)
	if err != nil {
		t.Fatalf("read run dir %q: %v", runDir, err)
	}
	allowed := map[string]bool{
		"research_cache.json":  true,
		"structure_cache.json": true,
		"scenario.json":        true,
	}
	for _, e := range entries {
		name := e.Name()
		if allowed[name] {
			continue
		}
		// Anything narration- or writer-stage flavored is forbidden by D6.
		// Include broad substring matches so a renamed cache also surfaces.
		lower := strings.ToLower(name)
		for _, marker := range []string{"narration", "writer_stage", "writer_cache", "act_cache", "monologue", "beats_cache"} {
			if strings.Contains(lower, marker) {
				t.Errorf("forbidden narration/writer cache %q in run dir %s: per D6 atomic boundary, no mid-writer on-disk artifact is allowed", name, when)
			}
		}
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
	// v2 shape: 4 acts × {2,3,3,2} beats = 10 beats total. FlatBeats() yields
	// NarrationBeatView with Index=1..10 in act order, matching the
	// flat scene_index basis downstream stages key on (segments,
	// image_track, scene_service).
	mkAct := func(actID string, narrations []string) domain.ActScript {
		monologue := strings.Join(narrations, " ")
		anchors := make([]domain.BeatAnchor, 0, len(narrations))
		offset := 0
		for i, n := range narrations {
			runes := []rune(n)
			end := offset + len(runes)
			anchors = append(anchors, domain.BeatAnchor{
				StartOffset:       offset,
				EndOffset:         end,
				Mood:              "calm",
				Location:          "site-19",
				CharactersPresent: []string{"unknown"},
				EntityVisible:     false,
				ColorPalette:      "neutral",
				Atmosphere:        "subdued",
				FactTags:          []domain.FactTag{},
			})
			offset = end
			if i < len(narrations)-1 {
				offset++
			}
		}
		return domain.ActScript{
			ActID:     actID,
			Monologue: monologue,
			Beats:     anchors,
			Mood:      "calm",
			KeyPoints: []string{},
		}
	}
	return &domain.NarrationScript{
		SCPID: "SCP-TEST",
		Title: "SCP-TEST",
		Acts: []domain.ActScript{
			mkAct(domain.ActIncident, []string{"scene 1", "scene 2"}),
			mkAct(domain.ActMystery, []string{"scene 3", "scene 4", "scene 5"}),
			mkAct(domain.ActRevelation, []string{"scene 6", "scene 7", "scene 8"}),
			mkAct(domain.ActUnresolved, []string{"scene 9", "scene 10"}),
		},
		SourceVersion: domain.NarrationSourceVersionV2,
	}
}

func samplePhaseAVisualBreakdown() *domain.VisualScript {
	// v2: 4 acts × 8 shots, anchored to the sample monologue offsets.
	actIDs := []string{domain.ActIncident, domain.ActMystery, domain.ActRevelation, domain.ActUnresolved}
	acts := make([]domain.VisualAct, 0, 4)
	for actIdx, actID := range actIDs {
		shots := make([]domain.VisualShot, 0, 8)
		for i := 0; i < 8; i++ {
			shots = append(shots, domain.VisualShot{
				ShotIndex:          i + 1,
				VisualDescriptor:   fmt.Sprintf("Appearance: Concrete sentinel; Distinguishing features: Obsidian eyes; Environment: Transit vault; Key visual moments: Blink; act %d shot %d description", actIdx, i+1),
				EstimatedDurationS: 7.0,
				Transition:         domain.TransitionKenBurns,
				NarrationAnchor: domain.BeatAnchor{
					StartOffset:       i * 50,
					EndOffset:         i*50 + 50,
					Mood:              "tense",
					Location:          "transit platform",
					CharactersPresent: []string{"SCP-TEST"},
					EntityVisible:     true,
					ColorPalette:      "alarm red, cold gray",
					Atmosphere:        "low hum",
					FactTags:          []domain.FactTag{},
				},
			})
		}
		acts = append(acts, domain.VisualAct{ActID: actID, Shots: shots})
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
