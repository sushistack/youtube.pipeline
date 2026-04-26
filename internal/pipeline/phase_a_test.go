package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline/agents"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// fixedTime is the canonical "now" used by most phase_a tests. FakeClock
// holds it without auto-advance; tests that care about StartedAt <
// FinishedAt call clock.Advance manually between the two stamps.
var fixedTime = time.Date(2026, 4, 18, 10, 0, 0, 0, time.UTC)

// newRunnerForTest wires seven NoopAgents by default and returns a runner
// with FakeClock + temp output dir. Callers override individual agents
// as needed via the returned builder.
type runnerBuilder struct {
	researcher        agents.AgentFunc
	structurer        agents.AgentFunc
	writer            agents.AgentFunc
	postWriterCritic  agents.AgentFunc
	visualBreakdowner agents.AgentFunc
	reviewer          agents.AgentFunc
	critic            agents.AgentFunc

	writerProvider string
	criticProvider string
	outputDir      string
	clock          clock.Clock
	logger         *slog.Logger
}

func defaultRunnerBuilder(t *testing.T) *runnerBuilder {
	t.Helper()
	logger, _ := testutil.CaptureLog(t)
	return &runnerBuilder{
		researcher:        agents.NoopAgent(),
		structurer:        agents.NoopAgent(),
		writer:            agents.NoopAgent(),
		postWriterCritic:  agents.NoopAgent(),
		visualBreakdowner: agents.NoopAgent(),
		reviewer:          agents.NoopAgent(),
		critic:            agents.NoopAgent(),
		writerProvider:    "openai",
		criticProvider:    "anthropic",
		outputDir:         t.TempDir(),
		clock:             clock.NewFakeClock(fixedTime),
		logger:            logger,
	}
}

func (b *runnerBuilder) build(t *testing.T) *PhaseARunner {
	t.Helper()
	r, err := NewPhaseARunner(
		b.researcher, b.structurer, b.writer,
		b.postWriterCritic, b.visualBreakdowner, b.reviewer, b.critic,
		b.writerProvider, b.criticProvider,
		b.outputDir, b.clock, b.logger,
	)
	if err != nil {
		t.Fatalf("NewPhaseARunner: %v", err)
	}
	return r
}

// newState returns a minimal valid PipelineState for Run invocations.
func newState() *agents.PipelineState {
	return &agents.PipelineState{RunID: "run-1", SCPID: "scp-1"}
}

// --- Constructor validation ------------------------------------------------

func TestNewPhaseARunner_NilAgent_ReturnsValidation(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	cases := []struct {
		name   string
		mutate func(*runnerBuilder)
		want   string
	}{
		{"nil researcher", func(b *runnerBuilder) { b.researcher = nil }, "researcher"},
		{"nil structurer", func(b *runnerBuilder) { b.structurer = nil }, "structurer"},
		{"nil writer", func(b *runnerBuilder) { b.writer = nil }, "writer"},
		{"nil post_writer_critic", func(b *runnerBuilder) { b.postWriterCritic = nil }, "post_writer_critic"},
		{"nil visual_breakdowner", func(b *runnerBuilder) { b.visualBreakdowner = nil }, "visual_breakdowner"},
		{"nil reviewer", func(b *runnerBuilder) { b.reviewer = nil }, "reviewer"},
		{"nil critic", func(b *runnerBuilder) { b.critic = nil }, "critic"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := defaultRunnerBuilder(t)
			tc.mutate(b)
			_, err := NewPhaseARunner(
				b.researcher, b.structurer, b.writer,
				b.postWriterCritic, b.visualBreakdowner, b.reviewer, b.critic,
				b.writerProvider, b.criticProvider,
				b.outputDir, b.clock, b.logger,
			)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, domain.ErrValidation) {
				t.Errorf("expected ErrValidation, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q missing %q", err.Error(), tc.want)
			}
		})
	}
}

func TestNewPhaseARunner_NilClock_ReturnsValidation(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	b := defaultRunnerBuilder(t)
	b.clock = nil
	_, err := NewPhaseARunner(
		b.researcher, b.structurer, b.writer,
		b.postWriterCritic, b.visualBreakdowner, b.reviewer, b.critic,
		b.writerProvider, b.criticProvider,
		b.outputDir, b.clock, b.logger,
	)
	if err == nil || !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "clock") {
		t.Errorf("error %q missing 'clock'", err.Error())
	}
}

func TestNewPhaseARunner_EmptyOutputDir_ReturnsValidation(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	b := defaultRunnerBuilder(t)
	b.outputDir = ""
	_, err := NewPhaseARunner(
		b.researcher, b.structurer, b.writer,
		b.postWriterCritic, b.visualBreakdowner, b.reviewer, b.critic,
		b.writerProvider, b.criticProvider,
		b.outputDir, b.clock, b.logger,
	)
	if err == nil || !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "outputDir") {
		t.Errorf("error %q missing 'outputDir'", err.Error())
	}
}

func TestNewPhaseARunner_EmptyWriterProvider_ReturnsValidation(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	b := defaultRunnerBuilder(t)
	b.writerProvider = ""
	_, err := NewPhaseARunner(
		b.researcher, b.structurer, b.writer,
		b.postWriterCritic, b.visualBreakdowner, b.reviewer, b.critic,
		b.writerProvider, b.criticProvider,
		b.outputDir, b.clock, b.logger,
	)
	if err == nil || !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "writerProvider") {
		t.Errorf("error %q missing 'writerProvider'", err.Error())
	}
}

func TestNewPhaseARunner_EmptyCriticProvider_ReturnsValidation(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	b := defaultRunnerBuilder(t)
	b.criticProvider = ""
	_, err := NewPhaseARunner(
		b.researcher, b.structurer, b.writer,
		b.postWriterCritic, b.visualBreakdowner, b.reviewer, b.critic,
		b.writerProvider, b.criticProvider,
		b.outputDir, b.clock, b.logger,
	)
	if err == nil || !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "criticProvider") {
		t.Errorf("error %q missing 'criticProvider'", err.Error())
	}
}

func TestNewPhaseARunner_NilLogger_DefaultsToSlogDefault(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	b := defaultRunnerBuilder(t)
	b.logger = nil
	r, err := NewPhaseARunner(
		b.researcher, b.structurer, b.writer,
		b.postWriterCritic, b.visualBreakdowner, b.reviewer, b.critic,
		b.writerProvider, b.criticProvider,
		b.outputDir, b.clock, b.logger,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.logger == nil {
		t.Fatal("expected non-nil logger fallback")
	}
}

// --- Run() input validation -----------------------------------------------

func TestPhaseARunner_Run_NilState_ReturnsValidation(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	r := defaultRunnerBuilder(t).build(t)
	err := r.Run(context.Background(), nil)
	if err == nil || !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestPhaseARunner_Run_EmptyRunID_ReturnsValidation(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	r := defaultRunnerBuilder(t).build(t)
	err := r.Run(context.Background(), &agents.PipelineState{SCPID: "scp-1"})
	if err == nil || !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "RunID") {
		t.Errorf("error %q missing 'RunID'", err.Error())
	}
}

func TestPhaseARunner_Run_EmptySCPID_ReturnsValidation(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	r := defaultRunnerBuilder(t).build(t)
	err := r.Run(context.Background(), &agents.PipelineState{RunID: "run-1"})
	if err == nil || !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "SCPID") {
		t.Errorf("error %q missing 'SCPID'", err.Error())
	}
}

func TestPhaseARunner_Run_RunIDPathTraversal_ReturnsValidation(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	cases := []struct {
		name  string
		runID string
	}{
		{"contains forward slash", "run/1"},
		{"contains backslash", `run\1`},
		{"leading double-dot", "../etc/passwd"},
		{"trailing double-dot", "run-1/.."},
		{"standalone dot", "."},
		{"embedded double-dot", "run..1"},
		{"nul byte", "run\x00inject"},
		{"newline", "run\ninject"},
		{"space", "run id"},
		{"tab", "run\tinject"},
	}

	// Use a spy chain that would count invocations, so if validation
	// ever regressed the spy tally would reveal it.
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls int
			tally := func(ctx context.Context, state *agents.PipelineState) error {
				calls++
				return nil
			}
			b := defaultRunnerBuilder(t)
			b.researcher = tally
			b.structurer = tally
			b.writer = tally
			b.visualBreakdowner = tally
			b.reviewer = tally
			b.critic = tally
			r := b.build(t)

			err := r.Run(context.Background(), &agents.PipelineState{
				RunID: tc.runID,
				SCPID: "scp-1",
			})
			if err == nil {
				t.Fatalf("expected error for RunID %q", tc.runID)
			}
			if !errors.Is(err, domain.ErrValidation) {
				t.Errorf("expected ErrValidation for %q, got %v", tc.runID, err)
			}
			if !strings.Contains(err.Error(), "RunID") {
				t.Errorf("expected %q in error, got %q", "RunID", err.Error())
			}
			testutil.AssertEqual(t, calls, 0)
		})
	}
}

// --- Execution order (AC-CHAIN-ORDER-INVARIANT) --------------------------

func TestPhaseARunner_ExecutionOrder(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	var order []agents.PipelineStage
	spy := func(ps agents.PipelineStage) agents.AgentFunc {
		return func(ctx context.Context, state *agents.PipelineState) error {
			order = append(order, ps)
			return nil
		}
	}

	b := defaultRunnerBuilder(t)
	b.researcher = spy(agents.StageResearcher)
	b.structurer = spy(agents.StageStructurer)
	b.writer = spy(agents.StageWriter)
	b.visualBreakdowner = spy(agents.StageVisualBreakdowner)
	b.reviewer = spy(agents.StageReviewer)
	b.critic = spy(agents.StageCritic)

	r := b.build(t)
	if err := r.Run(context.Background(), newState()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := []agents.PipelineStage{
		agents.StageResearcher,
		agents.StageStructurer,
		agents.StageWriter,
		agents.StageVisualBreakdowner,
		agents.StageReviewer,
		agents.StageCritic,
	}
	if len(order) != len(want) {
		t.Fatalf("got %d invocations, want %d: %v", len(order), len(want), order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("position %d: got %s want %s", i, order[i], want[i])
		}
	}
}

func TestPhaseARunner_StageCountIs6(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Direct behavioral guard against a future refactor silently adding
	// or removing an agent: wire six tallying spies and assert the
	// aggregate invocation count is exactly 6 after a successful Run.
	// The per-agent identity check lives in TestPhaseARunner_ExecutionOrder;
	// this test pins the *count* specifically so the "len(chain)==6"
	// invariant inside Run is load-bearing.
	var calls int
	tally := func(ctx context.Context, state *agents.PipelineState) error {
		calls++
		return nil
	}

	b := defaultRunnerBuilder(t)
	b.researcher = tally
	b.structurer = tally
	b.writer = tally
	b.visualBreakdowner = tally
	b.reviewer = tally
	b.critic = tally

	r := b.build(t)
	if err := r.Run(context.Background(), newState()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	testutil.AssertEqual(t, calls, 6)
}

// --- Fail-fast wrapping (AC-FAIL-FAST-WRAPPING) --------------------------

func TestPhaseARunner_StopsOnFirstError(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	sentinel := errors.New("writer boom")
	var calls []agents.PipelineStage
	spy := func(ps agents.PipelineStage) agents.AgentFunc {
		return func(ctx context.Context, state *agents.PipelineState) error {
			calls = append(calls, ps)
			return nil
		}
	}

	b := defaultRunnerBuilder(t)
	b.researcher = spy(agents.StageResearcher)
	b.structurer = spy(agents.StageStructurer)
	b.writer = func(ctx context.Context, state *agents.PipelineState) error {
		calls = append(calls, agents.StageWriter)
		return sentinel
	}
	b.visualBreakdowner = spy(agents.StageVisualBreakdowner)
	b.reviewer = spy(agents.StageReviewer)
	b.critic = spy(agents.StageCritic)

	r := b.build(t)
	err := r.Run(context.Background(), newState())
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("errors.Is(err, sentinel) == false: %v", err)
	}
	if !strings.Contains(err.Error(), "stage=writer") {
		t.Errorf("missing stage=writer in %q", err.Error())
	}

	// Only 3 stages executed: researcher, structurer, writer.
	want := []agents.PipelineStage{
		agents.StageResearcher,
		agents.StageStructurer,
		agents.StageWriter,
	}
	if len(calls) != len(want) {
		t.Fatalf("got %d calls, want %d: %v", len(calls), len(want), calls)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Errorf("position %d: got %s want %s", i, calls[i], want[i])
		}
	}
}

func TestPhaseARunner_StopsOnFirstError_ClassifiesDomainError(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	b := defaultRunnerBuilder(t)
	b.writer = func(ctx context.Context, state *agents.PipelineState) error {
		return fmt.Errorf("writer: %w: bad structure", domain.ErrValidation)
	}
	r := b.build(t)
	err := r.Run(context.Background(), newState())
	if err == nil {
		t.Fatal("expected error")
	}
	status, code, retryable := domain.Classify(err)
	testutil.AssertEqual(t, status, 400)
	testutil.AssertEqual(t, code, "VALIDATION_ERROR")
	testutil.AssertEqual(t, retryable, false)
}

// --- No artifact on failure (AC-FAIL-NO-ARTIFACT) ------------------------

func TestPhaseARunner_NoArtifactOnFailure(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	b := defaultRunnerBuilder(t)
	b.writer = func(ctx context.Context, state *agents.PipelineState) error {
		return errors.New("writer boom")
	}
	r := b.build(t)
	state := newState()
	if err := r.Run(context.Background(), state); err == nil {
		t.Fatal("expected error")
	}
	scenario := ScenarioPath(b.outputDir, state.RunID)
	_, err := os.Stat(scenario)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected scenario.json missing, stat err=%v", err)
	}
	if state.FinishedAt != "" {
		t.Errorf("FinishedAt must be empty on failure, got %q", state.FinishedAt)
	}
}

// --- Context cancellation (AC-CTX-CANCEL) --------------------------------

func TestPhaseARunner_ContextAlreadyCanceled(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	var calls int
	b := defaultRunnerBuilder(t)
	tally := func(ctx context.Context, state *agents.PipelineState) error {
		calls++
		return nil
	}
	b.researcher = tally
	b.structurer = tally
	b.writer = tally
	b.visualBreakdowner = tally
	b.reviewer = tally
	b.critic = tally

	r := b.build(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := r.Run(ctx, newState())
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if !strings.Contains(err.Error(), "stage=researcher") {
		t.Errorf("expected stage=researcher, got %q", err.Error())
	}
	testutil.AssertEqual(t, calls, 0)
}

func TestPhaseARunner_ContextCancelBetweenAgents(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	ctx, cancel := context.WithCancel(context.Background())

	var calls []agents.PipelineStage
	b := defaultRunnerBuilder(t)
	b.researcher = func(ctx context.Context, state *agents.PipelineState) error {
		calls = append(calls, agents.StageResearcher)
		return nil
	}
	b.structurer = func(ctx context.Context, state *agents.PipelineState) error {
		calls = append(calls, agents.StageStructurer)
		cancel() // cancel after structurer returns, before writer runs
		return nil
	}
	b.writer = func(ctx context.Context, state *agents.PipelineState) error {
		calls = append(calls, agents.StageWriter)
		return nil
	}
	b.visualBreakdowner = func(ctx context.Context, state *agents.PipelineState) error {
		calls = append(calls, agents.StageVisualBreakdowner)
		return nil
	}
	b.reviewer = func(ctx context.Context, state *agents.PipelineState) error {
		calls = append(calls, agents.StageReviewer)
		return nil
	}
	b.critic = func(ctx context.Context, state *agents.PipelineState) error {
		calls = append(calls, agents.StageCritic)
		return nil
	}

	r := b.build(t)

	err := r.Run(ctx, newState())
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if !strings.Contains(err.Error(), "stage=writer") {
		t.Errorf("expected stage=writer (the aborted stage), got %q", err.Error())
	}

	// Only researcher and structurer actually ran.
	want := []agents.PipelineStage{
		agents.StageResearcher,
		agents.StageStructurer,
	}
	if len(calls) != len(want) {
		t.Fatalf("got %d calls, want %d: %v", len(calls), len(want), calls)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Errorf("position %d: got %s want %s", i, calls[i], want[i])
		}
	}
}

// --- scenario.json write (AC-SCENARIO-JSON) ------------------------------

func TestPhaseARunner_WritesScenarioJSON(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	fakeClk := clock.NewFakeClock(fixedTime)

	b := defaultRunnerBuilder(t)
	b.clock = fakeClk
	b.researcher = func(ctx context.Context, state *agents.PipelineState) error {
		state.Research = &domain.ResearcherOutput{SCPID: "scp-1", Title: "payload"}
		return nil
	}
	b.structurer = func(ctx context.Context, state *agents.PipelineState) error {
		state.Structure = &domain.StructurerOutput{SCPID: "scp-1", TargetSceneCount: 10}
		return nil
	}
	// Advance the fake clock mid-chain (inside the writer spy) so that
	// StartedAt (stamped at the top of Run) and FinishedAt (stamped
	// after writeScenario) resolve to distinct timestamps.
	b.writer = func(ctx context.Context, state *agents.PipelineState) error {
		state.Narration = &domain.NarrationScript{SCPID: "scp-1", Title: "payload"}
		fakeClk.Advance(5 * time.Second)
		return nil
	}
	b.visualBreakdowner = func(ctx context.Context, state *agents.PipelineState) error {
		state.VisualBreakdown = &domain.VisualBreakdownOutput{
			SCPID:            "scp-1",
			Title:            "payload",
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
		return nil
	}
	b.reviewer = func(ctx context.Context, state *agents.PipelineState) error {
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
	}
	b.critic = func(ctx context.Context, state *agents.PipelineState) error {
		state.Critic = &domain.CriticOutput{
			PostWriter: &domain.CriticCheckpointReport{
				Checkpoint: domain.CriticCheckpointPostWriter,
				Verdict:    domain.CriticVerdictPass,
				Feedback:   "좋습니다.",
			},
			PostReviewer: &domain.CriticCheckpointReport{
				Checkpoint:   domain.CriticCheckpointPostReviewer,
				Verdict:      domain.CriticVerdictPass,
				OverallScore: 91,
				Feedback:     "최종 검토까지 안정적입니다.",
			},
		}
		return nil
	}

	r := b.build(t)
	state := newState()

	if err := r.Run(context.Background(), state); err != nil {
		t.Fatalf("Run: %v", err)
	}

	scenario := ScenarioPath(b.outputDir, state.RunID)
	raw, err := os.ReadFile(scenario)
	if err != nil {
		t.Fatalf("read scenario: %v", err)
	}

	var got agents.PipelineState
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal scenario: %v", err)
	}

	// All 6 fields populated.
	if got.Research == nil || got.Structure == nil || got.Narration == nil ||
		got.VisualBreakdown == nil || got.Review == nil || got.Critic == nil {
		t.Errorf("expected all 6 output fields populated, got %+v", got)
	}

	// StartedAt < FinishedAt.
	startAt, err := time.Parse(time.RFC3339Nano, got.StartedAt)
	if err != nil {
		t.Fatalf("parse started_at: %v", err)
	}
	finishAt, err := time.Parse(time.RFC3339Nano, got.FinishedAt)
	if err != nil {
		t.Fatalf("parse finished_at: %v", err)
	}
	if !startAt.Before(finishAt) {
		t.Errorf("expected started_at < finished_at, got %s >= %s", startAt, finishAt)
	}
}

func TestPhaseARunner_AtomicWrite(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	b := defaultRunnerBuilder(t)
	b.researcher = func(ctx context.Context, state *agents.PipelineState) error {
		state.Research = samplePhaseAResearch()
		return nil
	}
	b.structurer = func(ctx context.Context, state *agents.PipelineState) error {
		state.Structure = &domain.StructurerOutput{SCPID: state.SCPID, TargetSceneCount: 10, SourceVersion: domain.SourceVersionV1}
		return nil
	}
	b.writer = func(ctx context.Context, state *agents.PipelineState) error {
		state.Narration = samplePhaseANarration()
		return nil
	}
	b.visualBreakdowner = func(ctx context.Context, state *agents.PipelineState) error {
		state.VisualBreakdown = samplePhaseAVisualBreakdown()
		return nil
	}
	b.reviewer = func(ctx context.Context, state *agents.PipelineState) error {
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
	}
	b.critic = func(ctx context.Context, state *agents.PipelineState) error {
		state.Critic = &domain.CriticOutput{
			PostWriter: &domain.CriticCheckpointReport{
				Checkpoint:   domain.CriticCheckpointPostWriter,
				Verdict:      domain.CriticVerdictPass,
				OverallScore: 82,
				Feedback:     "좋습니다.",
			},
			PostReviewer: &domain.CriticCheckpointReport{
				Checkpoint:   domain.CriticCheckpointPostReviewer,
				Verdict:      domain.CriticVerdictPass,
				OverallScore: 88,
				Feedback:     "최종 검토까지 안정적입니다.",
			},
		}
		return nil
	}
	r := b.build(t)
	state := newState()
	if err := r.Run(context.Background(), state); err != nil {
		t.Fatalf("Run: %v", err)
	}
	runDir := filepath.Join(b.outputDir, state.RunID)
	entries, err := os.ReadDir(runDir)
	if err != nil {
		t.Fatalf("read run dir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "scenario-") && strings.HasSuffix(name, ".json") {
			t.Errorf("temp file leaked: %s", name)
		}
	}
	// And scenario.json must exist.
	if _, err := os.Stat(filepath.Join(runDir, "scenario.json")); err != nil {
		t.Fatalf("scenario.json missing: %v", err)
	}
}

func TestPhaseARunner_IdempotentOverwrite(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	var callCount int
	b := defaultRunnerBuilder(t)
	b.researcher = func(ctx context.Context, state *agents.PipelineState) error {
		state.Research = samplePhaseAResearch()
		return nil
	}
	b.structurer = func(ctx context.Context, state *agents.PipelineState) error {
		state.Structure = &domain.StructurerOutput{SCPID: state.SCPID, TargetSceneCount: 10, SourceVersion: domain.SourceVersionV1}
		return nil
	}
	b.writer = func(ctx context.Context, state *agents.PipelineState) error {
		state.Narration = samplePhaseANarration()
		return nil
	}
	b.visualBreakdowner = func(ctx context.Context, state *agents.PipelineState) error {
		state.VisualBreakdown = samplePhaseAVisualBreakdown()
		return nil
	}
	b.reviewer = func(ctx context.Context, state *agents.PipelineState) error {
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
	}
	b.critic = func(ctx context.Context, state *agents.PipelineState) error {
		callCount++
		state.Critic = &domain.CriticOutput{
			PostWriter: &domain.CriticCheckpointReport{
				Checkpoint:   domain.CriticCheckpointPostWriter,
				Verdict:      domain.CriticVerdictPass,
				Feedback:     fmt.Sprintf("호출 %d", callCount),
				OverallScore: callCount,
			},
			PostReviewer: &domain.CriticCheckpointReport{
				Checkpoint:   domain.CriticCheckpointPostReviewer,
				Verdict:      domain.CriticVerdictPass,
				Feedback:     fmt.Sprintf("최종 %d", callCount),
				OverallScore: 80 + callCount,
			},
		}
		return nil
	}
	r := b.build(t)
	state := newState()

	if err := r.Run(context.Background(), state); err != nil {
		t.Fatalf("Run #1: %v", err)
	}
	state2 := newState()
	if err := r.Run(context.Background(), state2); err != nil {
		t.Fatalf("Run #2: %v", err)
	}

	scenario := ScenarioPath(b.outputDir, state2.RunID)
	raw, err := os.ReadFile(scenario)
	if err != nil {
		t.Fatalf("read scenario: %v", err)
	}
	var loaded agents.PipelineState
	if err := json.Unmarshal(raw, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if loaded.Critic == nil || loaded.Critic.PostWriter == nil {
		t.Fatalf("critic payload missing: %+v", loaded.Critic)
	}
	if loaded.Critic.PostWriter.OverallScore != 2 {
		t.Errorf("expected overwritten scenario.json to contain score=2, got %+v", loaded.Critic.PostWriter)
	}
}

// --- MkdirAll failure (AC-MKDIR-FAILURE) ---------------------------------

func TestPhaseARunner_MkdirFailure_ReturnsError(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Point outputDir at a regular file so MkdirAll(outputDir/runID) fails.
	root := t.TempDir()
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("setup blocker: %v", err)
	}

	var calls int
	tally := func(ctx context.Context, state *agents.PipelineState) error {
		calls++
		return nil
	}
	b := defaultRunnerBuilder(t)
	b.outputDir = blocker
	b.researcher = tally
	b.structurer = tally
	b.writer = tally
	b.visualBreakdowner = tally
	b.reviewer = tally
	b.critic = tally

	r := b.build(t)
	state := newState()
	err := r.Run(context.Background(), state)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "create run dir") {
		t.Errorf("expected 'create run dir' substring, got %q", err.Error())
	}
	// AC-MKDIR-FAILURE: no agents must run when the per-run directory
	// cannot be created. The chain bails out BEFORE any agent is invoked
	// so we never burn LLM cost on output that cannot be persisted.
	testutil.AssertEqual(t, calls, 0)
	// FinishedAt must remain empty so the (FinishedAt != "") success
	// predicate still holds.
	testutil.AssertEqual(t, state.FinishedAt, "")
}

// --- Phase A agent caching (researcher / structurer) ----------------------

func TestPhaseARunner_ResearcherCacheHit(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	b := defaultRunnerBuilder(t)

	// Write a valid research_cache.json before Run() is called.
	runDir := filepath.Join(b.outputDir, "run-1")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cached := &domain.ResearcherOutput{SCPID: "scp-1", Title: "from-cache"}
	writeTestCacheJSON(t, filepath.Join(runDir, "research_cache.json"), cached)

	var researcherCalls int
	b.researcher = func(_ context.Context, _ *agents.PipelineState) error {
		researcherCalls++
		return nil
	}

	r := b.build(t)
	state := newState()
	if err := r.Run(context.Background(), state); err != nil {
		t.Fatalf("Run: %v", err)
	}

	testutil.AssertEqual(t, researcherCalls, 0)
	if state.Research == nil {
		t.Fatal("state.Research must be populated from cache")
	}
	testutil.AssertEqual(t, state.Research.Title, "from-cache")
}

func TestPhaseARunner_StructurerCacheHit(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	b := defaultRunnerBuilder(t)

	// Write a valid structure_cache.json before Run() is called.
	runDir := filepath.Join(b.outputDir, "run-1")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cached := &domain.StructurerOutput{SCPID: "scp-1", TargetSceneCount: 42}
	writeTestCacheJSON(t, filepath.Join(runDir, "structure_cache.json"), cached)

	var structurerCalls int
	b.structurer = func(_ context.Context, _ *agents.PipelineState) error {
		structurerCalls++
		return nil
	}

	r := b.build(t)
	state := newState()
	if err := r.Run(context.Background(), state); err != nil {
		t.Fatalf("Run: %v", err)
	}

	testutil.AssertEqual(t, structurerCalls, 0)
	if state.Structure == nil {
		t.Fatal("state.Structure must be populated from cache")
	}
	testutil.AssertEqual(t, state.Structure.TargetSceneCount, 42)
}

func TestPhaseARunner_CacheMissWritesFile(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	b := defaultRunnerBuilder(t)

	b.researcher = func(_ context.Context, state *agents.PipelineState) error {
		state.Research = &domain.ResearcherOutput{SCPID: "scp-1", Title: "fresh"}
		return nil
	}
	b.structurer = func(_ context.Context, state *agents.PipelineState) error {
		state.Structure = &domain.StructurerOutput{SCPID: "scp-1", TargetSceneCount: 10}
		return nil
	}

	r := b.build(t)
	state := newState()
	if err := r.Run(context.Background(), state); err != nil {
		t.Fatalf("Run: %v", err)
	}

	runDir := filepath.Join(b.outputDir, state.RunID)
	if _, err := os.Stat(filepath.Join(runDir, "research_cache.json")); err != nil {
		t.Errorf("research_cache.json not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "structure_cache.json")); err != nil {
		t.Errorf("structure_cache.json not written: %v", err)
	}
}

// TestPhaseARunner_ResearcherCacheHit_StructurerRunsNormally verifies the
// cross path: researcher is served from cache (0 calls) while structurer has
// no cache file and runs normally (1 call, populates state.Structure).
func TestPhaseARunner_ResearcherCacheHit_StructurerRunsNormally(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	b := defaultRunnerBuilder(t)

	runDir := filepath.Join(b.outputDir, "run-1")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Only pre-seed research cache — no structure_cache.json.
	cachedResearch := &domain.ResearcherOutput{SCPID: "scp-1", Title: "cached"}
	writeTestCacheJSON(t, filepath.Join(runDir, "research_cache.json"), cachedResearch)

	var researcherCalls, structurerCalls int
	b.researcher = func(_ context.Context, _ *agents.PipelineState) error {
		researcherCalls++
		return nil
	}
	b.structurer = func(_ context.Context, state *agents.PipelineState) error {
		structurerCalls++
		state.Structure = &domain.StructurerOutput{SCPID: "scp-1", TargetSceneCount: 7}
		return nil
	}

	r := b.build(t)
	state := newState()
	if err := r.Run(context.Background(), state); err != nil {
		t.Fatalf("Run: %v", err)
	}

	testutil.AssertEqual(t, researcherCalls, 0)
	testutil.AssertEqual(t, structurerCalls, 1)
	if state.Research == nil || state.Research.Title != "cached" {
		t.Errorf("state.Research not loaded from cache: %+v", state.Research)
	}
	if state.Structure == nil || state.Structure.TargetSceneCount != 7 {
		t.Errorf("state.Structure not populated by structurer agent: %+v", state.Structure)
	}
}

// writeTestCacheJSON marshals v to JSON and writes it to path.
func writeTestCacheJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal cache: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write cache file %s: %v", path, err)
	}
}

// --- ScenarioPath helper (AC-SCENARIO-JSON-PATH-ON-RUN) ------------------

func TestScenarioPath(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	tests := []struct {
		name      string
		outputDir string
		runID     string
		want      string
	}{
		{"typical", "/out", "run-1", filepath.Join("/out", "run-1", "scenario.json")},
		{"relative", "out", "r", filepath.Join("out", "r", "scenario.json")},
		{"empty outputDir", "", "r", filepath.Join("r", "scenario.json")},
		{"empty runID", "/out", "", filepath.Join("/out", "scenario.json")},
		{"both empty", "", "", "scenario.json"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ScenarioPath(tc.outputDir, tc.runID)
			testutil.AssertEqual(t, got, tc.want)
		})
	}
}
