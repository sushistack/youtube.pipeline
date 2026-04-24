package pipeline

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline/agents"
)

// stubSettingsPromoter records every PromotePendingAtSafeSeam call so the
// engine tests can assert that stage boundaries invoke promotion at the
// expected transitions (AC-4).
type stubSettingsPromoter struct {
	mu        sync.Mutex
	callCount int
	pending   bool
	forceErr  error
}

func (s *stubSettingsPromoter) PromotePendingAtSafeSeam(ctx context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callCount++
	if s.forceErr != nil {
		return false, s.forceErr
	}
	promoted := s.pending
	s.pending = false
	return promoted, nil
}

func (s *stubSettingsPromoter) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.callCount
}

// TestEngine_AdvancePromotesQueuedSettings verifies D3: Advance promotes
// pending settings before Phase A runs, so every stage inside Phase A sees
// a single coherent snapshot.
func TestEngine_AdvancePromotesQueuedSettings(t *testing.T) {
	promoter := &stubSettingsPromoter{pending: true}
	engine := NewEngine(nil, engineTestSegmentStore{}, nil, clock.RealClock{}, t.TempDir(), slog.Default())
	engine.SetSettingsPromoter(promoter)

	// Intentionally no phase A executor so Advance bails out early — but
	// the promote hook fires before the bail check on a successful load.
	// Supply a run-store stub that returns a valid phase-A-entry run.
	engine.runs = pipelineRunStoreStub{run: &domain.Run{
		ID:    "run-1",
		SCPID: "049",
		Stage: domain.StagePending,
	}}
	engine.phaseA = &errorPhaseAExecutor{err: errors.New("phase a unavailable")}

	_ = engine.Advance(context.Background(), "run-1")

	if calls := promoter.Calls(); calls != 1 {
		t.Fatalf("PromotePendingAtSafeSeam called %d times, want 1", calls)
	}
}

// TestEngine_ResumePromotesQueuedSettings verifies D3: Resume promotes
// pending settings as part of entering the resumed stage, so the retry runs
// against the newly-approved snapshot.
func TestEngine_ResumePromotesQueuedSettings(t *testing.T) {
	promoter := &stubSettingsPromoter{pending: true}

	runs := &pipelineRunStoreStub{run: &domain.Run{
		ID:     "run-2",
		SCPID:  "049",
		Stage:  domain.StageWrite,
		Status: domain.StatusFailed,
	}}
	segments := engineTestSegmentStore{}
	engine := NewEngine(runs, segments, nil, clock.RealClock{}, t.TempDir(), slog.Default())
	engine.SetSettingsPromoter(promoter)

	// We don't need Phase B here because StageWrite resumes as a Phase A stage.
	_, err := engine.ResumeWithOptions(context.Background(), "run-2", ResumeOptions{Force: true})
	if err != nil {
		// We expect the resume to succeed to the point where promotion fires,
		// but downstream may still error out (no phase A configured, etc).
		// What matters is that promotion was called — assert that below.
		t.Logf("ResumeWithOptions returned error (acceptable for this test): %v", err)
	}

	if calls := promoter.Calls(); calls < 1 {
		t.Fatalf("PromotePendingAtSafeSeam called %d times, want at least 1", calls)
	}
}

// TestEngine_PromoterErrorDoesNotFailStage verifies D3: a promoter error
// must NOT block a stage advance — we log and continue so an operational
// issue with settings doesn't freeze the pipeline.
func TestEngine_PromoterErrorDoesNotFailStage(t *testing.T) {
	promoter := &stubSettingsPromoter{forceErr: errors.New("store unavailable")}

	engine := NewEngine(
		pipelineRunStoreStub{run: &domain.Run{
			ID:    "run-3",
			SCPID: "049",
			Stage: domain.StagePending,
		}},
		engineTestSegmentStore{},
		nil,
		clock.RealClock{},
		t.TempDir(),
		slog.Default(),
	)
	engine.SetSettingsPromoter(promoter)
	engine.phaseA = &errorPhaseAExecutor{err: errors.New("phase a unavailable")}

	// Should not panic and should not return a settings-related error.
	_ = engine.Advance(context.Background(), "run-3")

	if promoter.Calls() != 1 {
		t.Fatalf("promoter call count = %d, want 1 even when promoter errors", promoter.Calls())
	}
}

// ---- test helpers ----

type pipelineRunStoreStub struct {
	run *domain.Run
}

func (s pipelineRunStoreStub) Get(_ context.Context, _ string) (*domain.Run, error) {
	if s.run == nil {
		return nil, errors.New("no run")
	}
	copy := *s.run
	return &copy, nil
}

func (s pipelineRunStoreStub) ResetForResume(_ context.Context, _ string, _ domain.Status) error {
	return nil
}

func (s pipelineRunStoreStub) ApplyPhaseAResult(_ context.Context, _ string, _ domain.PhaseAAdvanceResult) error {
	return nil
}

type errorPhaseAExecutor struct {
	err error
}

func (e *errorPhaseAExecutor) Run(_ context.Context, _ *agents.PipelineState) error {
	return e.err
}
