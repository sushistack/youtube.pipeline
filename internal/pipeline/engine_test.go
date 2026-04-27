package pipeline

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline/agents"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestNextStage_ValidTransitions(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	tests := []struct {
		name    string
		current domain.Stage
		event   domain.Event
		want    domain.Stage
	}{
		{"pending+start→research", domain.StagePending, domain.EventStart, domain.StageResearch},
		{"research+complete→structure", domain.StageResearch, domain.EventComplete, domain.StageStructure},
		{"structure+complete→write", domain.StageStructure, domain.EventComplete, domain.StageWrite},
		{"write+complete→visual_break", domain.StageWrite, domain.EventComplete, domain.StageVisualBreak},
		{"visual_break+complete→review", domain.StageVisualBreak, domain.EventComplete, domain.StageReview},
		{"review+complete→critic", domain.StageReview, domain.EventComplete, domain.StageCritic},
		{"critic+complete→scenario_review", domain.StageCritic, domain.EventComplete, domain.StageScenarioReview},
		{"critic+retry→write", domain.StageCritic, domain.EventRetry, domain.StageWrite},
		{"scenario_review+approve→character_pick", domain.StageScenarioReview, domain.EventApprove, domain.StageCharacterPick},
		{"character_pick+approve→image", domain.StageCharacterPick, domain.EventApprove, domain.StageImage},
		{"image+complete→tts", domain.StageImage, domain.EventComplete, domain.StageTTS},
		{"tts+complete→batch_review", domain.StageTTS, domain.EventComplete, domain.StageBatchReview},
		{"batch_review+approve→assemble", domain.StageBatchReview, domain.EventApprove, domain.StageAssemble},
		{"assemble+complete→metadata_ack", domain.StageAssemble, domain.EventComplete, domain.StageMetadataAck},
		{"metadata_ack+approve→complete", domain.StageMetadataAck, domain.EventApprove, domain.StageComplete},
	}

	if len(tests) != 15 {
		t.Fatalf("expected 15 valid transitions, got %d", len(tests))
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NextStage(tt.current, tt.event)
			if err != nil {
				t.Fatalf("NextStage(%s, %s) returned unexpected error: %v", tt.current, tt.event, err)
			}
			testutil.AssertEqual(t, got, tt.want)
		})
	}
}

func TestNextStage_InvalidTransitions(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Build the set of valid (stage, event) pairs.
	type pair struct {
		stage domain.Stage
		event domain.Event
	}
	valid := map[pair]bool{
		{domain.StagePending, domain.EventStart}:          true,
		{domain.StageResearch, domain.EventComplete}:      true,
		{domain.StageStructure, domain.EventComplete}:     true,
		{domain.StageWrite, domain.EventComplete}:         true,
		{domain.StageVisualBreak, domain.EventComplete}:   true,
		{domain.StageReview, domain.EventComplete}:        true,
		{domain.StageCritic, domain.EventComplete}:        true,
		{domain.StageCritic, domain.EventRetry}:           true,
		{domain.StageScenarioReview, domain.EventApprove}: true,
		{domain.StageCharacterPick, domain.EventApprove}:  true,
		{domain.StageImage, domain.EventComplete}:         true,
		{domain.StageTTS, domain.EventComplete}:           true,
		{domain.StageBatchReview, domain.EventApprove}:    true,
		{domain.StageAssemble, domain.EventComplete}:      true,
		{domain.StageMetadataAck, domain.EventApprove}:    true,
	}

	invalidCount := 0
	for _, stage := range domain.AllStages() {
		for _, event := range domain.AllEvents() {
			if valid[pair{stage, event}] {
				continue
			}
			invalidCount++
			t.Run(string(stage)+"+"+string(event), func(t *testing.T) {
				_, err := NextStage(stage, event)
				if err == nil {
					t.Errorf("NextStage(%s, %s) expected error, got nil", stage, event)
				}
			})
		}
	}

	// 15 stages × 4 events = 60 total, minus 15 valid = 45 invalid.
	testutil.AssertEqual(t, invalidCount, 45)
}

func TestStatusForStage_AllStages(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	tests := []struct {
		stage domain.Stage
		want  domain.Status
	}{
		{domain.StagePending, domain.StatusPending},
		{domain.StageResearch, domain.StatusRunning},
		{domain.StageStructure, domain.StatusRunning},
		{domain.StageWrite, domain.StatusRunning},
		{domain.StageVisualBreak, domain.StatusRunning},
		{domain.StageReview, domain.StatusRunning},
		{domain.StageCritic, domain.StatusRunning},
		{domain.StageScenarioReview, domain.StatusWaiting},
		{domain.StageCharacterPick, domain.StatusWaiting},
		{domain.StageImage, domain.StatusRunning},
		{domain.StageTTS, domain.StatusRunning},
		{domain.StageBatchReview, domain.StatusWaiting},
		{domain.StageAssemble, domain.StatusRunning},
		{domain.StageMetadataAck, domain.StatusWaiting},
		{domain.StageComplete, domain.StatusCompleted},
	}

	if len(tests) != 15 {
		t.Fatalf("expected 15 status mappings, got %d", len(tests))
	}

	for _, tt := range tests {
		t.Run(string(tt.stage), func(t *testing.T) {
			testutil.AssertEqual(t, StatusForStage(tt.stage), tt.want)
		})
	}
}

func TestEngine_AdvanceRequiresPhaseAExecutor(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Run at a Phase A entry stage — no Phase A executor configured.
	runs := &engineTestRunStore{
		run: &domain.Run{ID: "run-anything", Stage: domain.StagePending, Status: domain.StatusPending},
	}
	engine := NewEngine(runs, engineTestSegmentStore{}, nil, clock.RealClock{}, t.TempDir(), slog.Default())
	err := engine.Advance(context.Background(), "run-anything")
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	want := "advance run-anything: validation error: phase a executor is nil"
	if err.Error() != want {
		t.Errorf("unexpected message: got %q, want %q", err.Error(), want)
	}
}

type fakePhaseAExecutor struct {
	run func(ctx context.Context, state *agents.PipelineState) error
}

func (f fakePhaseAExecutor) Run(ctx context.Context, state *agents.PipelineState) error {
	return f.run(ctx, state)
}

type engineTestRunStore struct {
	run *domain.Run
}

func (s *engineTestRunStore) Get(ctx context.Context, id string) (*domain.Run, error) {
	if s.run == nil || s.run.ID != id {
		return nil, domain.ErrNotFound
	}
	copy := *s.run
	return &copy, nil
}

func (s *engineTestRunStore) ResetForResume(ctx context.Context, id string, status domain.Status) error {
	return nil
}

func (s *engineTestRunStore) ApplyPhaseAResult(ctx context.Context, id string, res domain.PhaseAAdvanceResult) error {
	if s.run == nil || s.run.ID != id {
		return domain.ErrNotFound
	}
	s.run.Stage = res.Stage
	s.run.Status = res.Status
	s.run.RetryReason = res.RetryReason
	s.run.CriticScore = res.CriticScore
	s.run.ScenarioPath = res.ScenarioPath
	return nil
}

type engineTestSegmentStore struct{}

func (engineTestSegmentStore) ListByRunID(ctx context.Context, runID string) ([]*domain.Episode, error) {
	return nil, nil
}

func (engineTestSegmentStore) DeleteByRunID(ctx context.Context, runID string) (int64, error) {
	return 0, nil
}

func (engineTestSegmentStore) ClearClipPathsByRunID(ctx context.Context, runID string) (int64, error) {
	return 0, nil
}

func (engineTestSegmentStore) ClearImageArtifactsByRunID(ctx context.Context, runID string) (int64, error) {
	return 0, nil
}

func (engineTestSegmentStore) ClearTTSArtifactsByRunID(ctx context.Context, runID string) (int64, error) {
	return 0, nil
}

func TestEngineAdvance_PhaseAHappyPath_MovesToScenarioReview(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	outDir := t.TempDir()
	runs := &engineTestRunStore{
		run: &domain.Run{ID: "run-1", SCPID: "SCP-TEST", Stage: domain.StagePending, Status: domain.StatusPending},
	}
	engine := NewEngine(runs, engineTestSegmentStore{}, nil, clock.RealClock{}, outDir, slog.Default())
	engine.SetPhaseAExecutor(fakePhaseAExecutor{run: func(ctx context.Context, state *agents.PipelineState) error {
		state.Quality = &agents.PhaseAQualitySummary{PostWriterScore: 81, PostReviewerScore: 88, CumulativeScore: 85, FinalVerdict: domain.CriticVerdictPass}
		state.Contracts = &agents.PhaseAContractManifest{}
		state.Critic = &domain.CriticOutput{
			PostReviewer: &domain.CriticCheckpointReport{
				Checkpoint:   domain.CriticCheckpointPostReviewer,
				Verdict:      domain.CriticVerdictPass,
				OverallScore: 88,
			},
		}
		if err := os.MkdirAll(filepath.Join(outDir, state.RunID), 0o755); err != nil {
			return err
		}
		return os.WriteFile(ScenarioPath(outDir, state.RunID), []byte(`{}`), 0o644)
	}})

	if err := engine.Advance(context.Background(), "run-1"); err != nil {
		t.Fatalf("Advance: %v", err)
	}
	testutil.AssertEqual(t, runs.run.Stage, domain.StageScenarioReview)
	testutil.AssertEqual(t, runs.run.Status, domain.StatusWaiting)
	if runs.run.CriticScore == nil {
		t.Fatal("expected critic_score persisted")
	}
	testutil.AssertFloatNear(t, *runs.run.CriticScore, 0.88, 0.000001)
	if runs.run.ScenarioPath == nil || *runs.run.ScenarioPath != "scenario.json" {
		t.Fatalf("unexpected scenario path: %v", runs.run.ScenarioPath)
	}
}

func TestEngineAdvance_PhaseARetry_MovesBackToWriteWithoutScenarioJSON(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	outDir := t.TempDir()
	runs := &engineTestRunStore{
		run: &domain.Run{ID: "run-2", SCPID: "SCP-TEST", Stage: domain.StageCritic, Status: domain.StatusRunning},
	}
	engine := NewEngine(runs, engineTestSegmentStore{}, nil, clock.RealClock{}, outDir, slog.Default())
	engine.SetPhaseAExecutor(fakePhaseAExecutor{run: func(ctx context.Context, state *agents.PipelineState) error {
		state.Critic = &domain.CriticOutput{
			PostReviewer: &domain.CriticCheckpointReport{
				Checkpoint:   domain.CriticCheckpointPostReviewer,
				Verdict:      domain.CriticVerdictRetry,
				RetryReason:  "weak_hook",
				OverallScore: 54,
			},
		}
		return nil
	}})

	err := engine.Advance(context.Background(), "run-2")
	if !errors.Is(err, domain.ErrStageFailed) {
		t.Fatalf("expected ErrStageFailed, got %v", err)
	}
	testutil.AssertEqual(t, runs.run.Stage, domain.StageWrite)
	testutil.AssertEqual(t, runs.run.Status, domain.StatusFailed)
	if runs.run.ScenarioPath != nil {
		t.Fatalf("expected no scenario path, got %v", *runs.run.ScenarioPath)
	}
	if _, err := os.Stat(ScenarioPath(outDir, "run-2")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no scenario.json, got %v", err)
	}
}

func TestIsHITLStage(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	hitl := map[domain.Stage]bool{
		domain.StageScenarioReview: true,
		domain.StageCharacterPick:  true,
		domain.StageBatchReview:    true,
		domain.StageMetadataAck:    true,
	}

	for _, s := range domain.AllStages() {
		t.Run(string(s), func(t *testing.T) {
			testutil.AssertEqual(t, IsHITLStage(s), hitl[s])
		})
	}
}

// ── Advance multi-phase dispatch tests ───────────────────────────────────────

type fakePhaseBExecutor struct {
	run func(ctx context.Context, req PhaseBRequest) (PhaseBResult, error)
}

func (f *fakePhaseBExecutor) Run(ctx context.Context, req PhaseBRequest) (PhaseBResult, error) {
	return f.run(ctx, req)
}

func TestEngineAdvance_HITLStages_ReturnConflict(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	hitlStages := []domain.Stage{
		domain.StageScenarioReview,
		domain.StageCharacterPick,
		domain.StageBatchReview,
		domain.StageMetadataAck,
	}

	for _, stage := range hitlStages {
		t.Run(string(stage), func(t *testing.T) {
			runs := &engineTestRunStore{
				run: &domain.Run{ID: "run-1", Stage: stage, Status: domain.StatusWaiting},
			}
			engine := NewEngine(runs, engineTestSegmentStore{}, nil, clock.RealClock{}, t.TempDir(), slog.Default())
			err := engine.Advance(context.Background(), "run-1")
			if !errors.Is(err, domain.ErrConflict) {
				t.Errorf("stage %s: expected ErrConflict, got %v", stage, err)
			}
		})
	}
}

func TestEngineAdvance_PhaseBExecutorNil_ReturnsValidation(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	runs := &engineTestRunStore{
		run: &domain.Run{ID: "run-1", Stage: domain.StageImage, Status: domain.StatusRunning},
	}
	engine := NewEngine(runs, engineTestSegmentStore{}, nil, clock.RealClock{}, t.TempDir(), slog.Default())
	// No Phase B executor configured.
	err := engine.Advance(context.Background(), "run-1")
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestEngineAdvance_PhaseCRunnerNil_ReturnsValidation(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	runs := &engineTestRunStore{
		run: &domain.Run{ID: "run-1", Stage: domain.StageAssemble, Status: domain.StatusRunning},
	}
	engine := NewEngine(runs, engineTestSegmentStore{}, nil, clock.RealClock{}, t.TempDir(), slog.Default())
	// No Phase C runner configured.
	err := engine.Advance(context.Background(), "run-1")
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

// trackingPhaseCDep records whether a Phase C update method ran. The
// guard short-circuits inside runPhaseC before either updater is touched,
// so on a successful guard fire both flags stay false.
type trackingPhaseCDep struct{ called bool }

func (d *trackingPhaseCDep) UpdateClipPath(_ context.Context, _ string, _ int, _ string) error {
	d.called = true
	return nil
}
func (d *trackingPhaseCDep) UpdateOutputPath(_ context.Context, _ string, _ string) error {
	d.called = true
	return nil
}

func TestEngine_RunPhaseC_DryRunBlocksAssembly(t *testing.T) {
	// Centralized guard inside runPhaseC covers BOTH Engine.Advance and
	// ExecuteResume — placeholder image/audio cannot reach ffmpeg via either
	// dispatch path. Test wires a real PhaseCRunner with tracking stubs and
	// asserts: guard fires (ErrValidation + "dry-run" message) AND no
	// Phase C dependency was touched.
	testutil.BlockExternalHTTP(t)

	runs := &engineTestRunStore{
		run: &domain.Run{
			ID:     "run-dry",
			Stage:  domain.StageAssemble,
			Status: domain.StatusWaiting,
			DryRun: true,
		},
	}
	clipUpdater := &trackingPhaseCDep{}
	outputUpdater := &trackingPhaseCDep{}
	phaseC := NewPhaseCRunner(clipUpdater, outputUpdater, nil, clock.RealClock{}, slog.Default())

	engine := NewEngine(runs, engineTestSegmentStore{}, nil, clock.RealClock{}, t.TempDir(), slog.Default())
	engine.SetPhaseCRunner(phaseC)

	err := engine.Advance(context.Background(), "run-dry")
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "dry-run") {
		t.Errorf("error does not name dry-run reason: %v", err)
	}
	if clipUpdater.called || outputUpdater.called {
		t.Errorf("Phase C dependencies were touched despite DryRun=true; guard failed to short-circuit")
	}
}

func TestEngineAdvance_PhaseB_MovesToBatchReview(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	score := 0.88
	scenarioPathVal := "scenario.json"
	runs := &engineTestRunStore{
		run: &domain.Run{
			ID:           "run-1",
			Stage:        domain.StageImage,
			Status:       domain.StatusRunning,
			CriticScore:  &score,
			ScenarioPath: &scenarioPathVal,
		},
	}
	var capturedReq PhaseBRequest
	phaseBExec := &fakePhaseBExecutor{
		run: func(_ context.Context, req PhaseBRequest) (PhaseBResult, error) {
			capturedReq = req
			return PhaseBResult{}, nil
		},
	}
	engine := NewEngine(runs, engineTestSegmentStore{}, nil, clock.RealClock{}, t.TempDir(), slog.Default())
	engine.SetPhaseBExecutor(phaseBExec)

	if err := engine.Advance(context.Background(), "run-1"); err != nil {
		t.Fatalf("Advance: %v", err)
	}

	testutil.AssertEqual(t, runs.run.Stage, domain.StageBatchReview)
	testutil.AssertEqual(t, runs.run.Status, domain.StatusWaiting)
	testutil.AssertEqual(t, capturedReq.RunID, "run-1")
	testutil.AssertEqual(t, capturedReq.Stage, domain.StageImage)
	// CriticScore and ScenarioPath must be preserved across Phase B.
	if runs.run.CriticScore == nil || *runs.run.CriticScore != score {
		t.Errorf("expected critic_score %.2f preserved, got %v", score, runs.run.CriticScore)
	}
	if runs.run.ScenarioPath == nil || *runs.run.ScenarioPath != scenarioPathVal {
		t.Errorf("expected scenario_path preserved, got %v", runs.run.ScenarioPath)
	}
}

func TestEngineAdvance_PhaseB_FailureRollsBackToFailed(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	score := 0.91
	scenarioPathVal := "scenario.json"
	retryReasonVal := "earlier phase a retry context"
	runs := &engineTestRunStore{
		run: &domain.Run{
			ID:           "run-1",
			Stage:        domain.StageImage,
			Status:       domain.StatusRunning,
			CriticScore:  &score,
			ScenarioPath: &scenarioPathVal,
			RetryReason:  &retryReasonVal,
		},
	}
	phaseBExec := &fakePhaseBExecutor{
		run: func(_ context.Context, _ PhaseBRequest) (PhaseBResult, error) {
			return PhaseBResult{}, errors.New("image generation failed")
		},
	}
	engine := NewEngine(runs, engineTestSegmentStore{}, nil, clock.RealClock{}, t.TempDir(), slog.Default())
	engine.SetPhaseBExecutor(phaseBExec)

	err := engine.Advance(context.Background(), "run-1")
	if err == nil {
		t.Fatal("expected error from Phase B failure, got nil")
	}
	testutil.AssertEqual(t, runs.run.Stage, domain.StageImage)
	testutil.AssertEqual(t, runs.run.Status, domain.StatusFailed)
	// Phase A meta must survive the rollback so retry context (and UI) is intact.
	if runs.run.CriticScore == nil || *runs.run.CriticScore != score {
		t.Errorf("expected critic_score %.2f preserved after rollback, got %v", score, runs.run.CriticScore)
	}
	if runs.run.ScenarioPath == nil || *runs.run.ScenarioPath != scenarioPathVal {
		t.Errorf("expected scenario_path preserved after rollback, got %v", runs.run.ScenarioPath)
	}
	if runs.run.RetryReason == nil || *runs.run.RetryReason != retryReasonVal {
		t.Errorf("expected retry_reason preserved after rollback, got %v", runs.run.RetryReason)
	}
}

func TestEngineAdvance_CompleteStage_ReturnsConflict(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	runs := &engineTestRunStore{
		run: &domain.Run{ID: "run-1", Stage: domain.StageComplete, Status: domain.StatusCompleted},
	}
	engine := NewEngine(runs, engineTestSegmentStore{}, nil, clock.RealClock{}, t.TempDir(), slog.Default())
	err := engine.Advance(context.Background(), "run-1")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict for complete stage, got %v", err)
	}
}
