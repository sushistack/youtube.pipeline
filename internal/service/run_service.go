// Package service contains business logic that orchestrates domain, db, and pipeline.
package service

import (
	"context"
	"fmt"
	"regexp"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
)

// scpIDPattern matches allowed characters in an SCP identifier: alphanumeric,
// underscore, and hyphen. Deliberately rejects `/`, `..`, and control bytes
// to prevent path-escape via the run output directory.
var scpIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// RunStore is the persistence interface for pipeline runs.
// Defined here (consumer) and implemented by internal/db.RunStore.
type RunStore interface {
	Create(ctx context.Context, scpID, outputDir string) (*domain.Run, error)
	CreateWithPromptVersion(ctx context.Context, scpID, outputDir string, tag *db.PromptVersionTag, dryRun bool) (*domain.Run, error)
	Get(ctx context.Context, id string) (*domain.Run, error)
	List(ctx context.Context) ([]*domain.Run, error)
	Cancel(ctx context.Context, id string) error
	MarkComplete(ctx context.Context, id string) error                                         // NEW: sets stage=complete, status=completed
	ApplyPhaseAResult(ctx context.Context, runID string, res domain.PhaseAAdvanceResult) error // atomic stage advance write used by ApproveScenarioReview
}

// DryRunProvider returns the effective Phase B dry-run mode at the moment a
// new run is created. Implemented by *SettingsService via
// LoadEffectiveRuntimeConfig. A nil provider (tests, headless flows) leaves
// dry_run = false, matching the production-default safety stance.
type DryRunProvider interface {
	EffectiveDryRun(ctx context.Context) (bool, error)
}

// Resumer is the minimal engine surface that RunService delegates Resume to.
// *pipeline.Engine satisfies this interface. Declaring it here keeps the
// dependency direction one-way: service → pipeline interface. The returned
// report carries FS/DB inconsistency descriptions surfaced to the caller as
// warnings (CLI `--force` output, API response `warnings` field).
//
// PrepareResume + ExecuteResume are the split entry points used by the API
// handler so Phase B/C work runs detached from the HTTP request lifetime.
// ResumeWithOptions stays for the CLI's synchronous path.
type Resumer interface {
	PrepareResume(ctx context.Context, runID string, opts pipeline.ResumeOptions) (*domain.Run, *domain.InconsistencyReport, error)
	ExecuteResume(ctx context.Context, runID string) error
	ResumeWithOptions(ctx context.Context, runID string, opts pipeline.ResumeOptions) (*domain.InconsistencyReport, error)
}

// Advancer is the minimal engine surface for kicking off a pending run
// (Phase A entry: pending → critic). *pipeline.Engine satisfies this.
// Resume rejects pending status by design; Advance is the matching path.
type Advancer interface {
	Advance(ctx context.Context, runID string) error
}

// Rewinder is the engine surface for stepper-driven rewind. *pipeline.Engine
// satisfies it. Declared here so service stays one-way dependent on
// pipeline (interface declared in consumer).
type Rewinder interface {
	Rewind(ctx context.Context, runID string, node pipeline.StageNodeKey) (*domain.Run, error)
}

// Canceller is the engine surface that drains in-flight workers when a run
// is cancelled. *pipeline.Engine satisfies it via its CancelRegistry. The
// status='cancelled' DB mark is the service's responsibility (store.Cancel);
// the canceller is the worker-stop half — pairing the two at the service
// layer keeps DB feedback immediate while the registry drain runs after.
// Nil canceller is tolerated for CLI/test paths that do not run workers
// in-process; in that case Cancel degrades to a status-only mark, which is
// harmless because no workers exist to overwrite anything.
type Canceller interface {
	Cancel(ctx context.Context, runID string) error
}

// RunService implements pipeline run lifecycle management.
type RunService struct {
	store        RunStore
	resumer      Resumer
	advancer     Advancer
	rewinder     Rewinder
	canceller    Canceller
	prompt       PromptVersionProvider
	dryRun       DryRunProvider
	hitlSessions pipeline.HITLSessionStore // optional; needed by ApproveScenarioReview to maintain hitl_sessions invariant
	clock        clock.Clock               // for last_interaction_timestamp on hitl_sessions upsert
}

// NewRunService creates a RunService backed by the provided RunStore.
// resumer MAY be nil for call paths that never invoke Resume (tests, tools);
// runtime Resume calls with a nil resumer return ErrValidation rather than panicking.
// The advancer is wired separately via SetAdvancer; *pipeline.Engine satisfies
// both Resumer and Advancer so a single engine instance is passed twice.
func NewRunService(store RunStore, resumer Resumer) *RunService {
	return &RunService{store: store, resumer: resumer}
}

// SetAdvancer wires the engine surface used by Advance. nil disables the path
// (Advance returns ErrValidation), matching the resumer pattern for tests.
func (s *RunService) SetAdvancer(advancer Advancer) {
	s.advancer = advancer
}

// SetRewinder wires the engine surface used by Rewind. Mirrors SetAdvancer:
// nil disables the path with ErrValidation.
func (s *RunService) SetRewinder(rewinder Rewinder) {
	s.rewinder = rewinder
}

// SetCanceller wires the engine surface used by Cancel to drain in-flight
// workers after the status='cancelled' DB mark. Nil leaves Cancel as a
// status-only mark — acceptable for CLI/tools that do not own running
// workers, but the in-process server MUST wire this or Cancel becomes
// cosmetic and worker goroutines keep producing stale writes.
func (s *RunService) SetCanceller(canceller Canceller) {
	s.canceller = canceller
}

// SetHITLSessionStore wires the hitl_sessions writer used by
// ApproveScenarioReview to drop the scenario_review session row and create
// the character_pick row at the boundary. Nil disables HITL row management
// for that path (acceptable for tests/tools that don't observe the rows).
// clk MAY be nil; nil falls back to clock.RealClock{} (RFC3339 wall clock).
func (s *RunService) SetHITLSessionStore(store pipeline.HITLSessionStore, clk clock.Clock) {
	s.hitlSessions = store
	if clk == nil {
		clk = clock.RealClock{}
	}
	s.clock = clk
}

// SetPromptVersionProvider wires the Story 10.2 AC-3 stamping path. When
// provider is non-nil, Create reads the active Critic prompt version tag
// at run-creation time and persists it on the new row. A nil provider
// (tests, headless flows, legacy call sites) leaves the columns NULL,
// matching "existing runs stay NULL" behavior.
func (s *RunService) SetPromptVersionProvider(provider PromptVersionProvider) {
	s.prompt = provider
}

// SetDryRunProvider wires the effective-config reader Create consults to
// snapshot Phase B dry-run mode onto the new run row. Nil disables the
// path (Create persists dry_run=false), matching the safety-default stance
// for legacy/test entry points that don't observe Settings.
func (s *RunService) SetDryRunProvider(provider DryRunProvider) {
	s.dryRun = provider
}

// Create creates a new pipeline run for the given SCP ID and returns it.
// scpID is validated against scpIDPattern to block path-escape and injection.
//
// At creation time the active Critic prompt tag and the effective Phase B
// dry-run flag are snapshotted onto the new row. Both providers are
// optional; when absent, fields default to NULL / false.
func (s *RunService) Create(ctx context.Context, scpID, outputDir string) (*domain.Run, error) {
	if !scpIDPattern.MatchString(scpID) {
		return nil, fmt.Errorf("create run: invalid scp_id %q: %w", scpID, domain.ErrValidation)
	}
	var tag *db.PromptVersionTag
	if s.prompt != nil {
		tag = s.prompt.ActivePromptVersion()
	}
	var dryRun bool
	if s.dryRun != nil {
		// On provider error, default to dryRun=false rather than failing
		// Create. Real-mode is the safety-default stance: a real run hits
		// real APIs and the operator notices billing immediately. A
		// hard-fail on transient SettingsService hiccups would block run
		// creation entirely. Mirrors PromptVersionProvider's nil-tolerant
		// philosophy at the same call site.
		if v, err := s.dryRun.EffectiveDryRun(ctx); err == nil {
			dryRun = v
		}
	}
	run, err := s.store.CreateWithPromptVersion(ctx, scpID, outputDir, tag, dryRun)
	if err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}
	return run, nil
}

// Get returns the run with the given ID.
func (s *RunService) Get(ctx context.Context, id string) (*domain.Run, error) {
	run, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get run: %w", err)
	}
	return run, nil
}

// List returns all pipeline runs ordered by creation time.
func (s *RunService) List(ctx context.Context) ([]*domain.Run, error) {
	runs, err := s.store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	return runs, nil
}

// Cancel cancels the run with the given ID.
// Returns ErrNotFound if the run does not exist.
// Returns ErrConflict if the run is not in a cancellable state.
//
// Two-phase: store.Cancel marks the DB row 'cancelled' (immediately visible
// to the operator) → canceller.Cancel drains in-flight worker goroutines so
// they cannot continue producing stage writes that would race with a
// subsequent Resume. The canceller is best-effort: if it cannot drain in
// time, PrepareResume's stage cleanup is the safety net.
func (s *RunService) Cancel(ctx context.Context, id string) error {
	if err := s.store.Cancel(ctx, id); err != nil {
		return fmt.Errorf("cancel run: %w", err)
	}
	if s.canceller != nil {
		if err := s.canceller.Cancel(ctx, id); err != nil {
			return fmt.Errorf("cancel run: drain workers: %w", err)
		}
	}
	return nil
}

// Resume re-enters the failed (or waiting) stage of a run and returns the
// updated run snapshot plus any FS/DB inconsistency warnings that were
// bypassed via force. When force is true, inconsistencies are tolerated
// rather than aborting; the report lists what was bypassed so the caller can
// surface them (CLI/API warnings).
//
// Error classes propagated from the engine:
//   - ErrNotFound:   run does not exist.
//   - ErrConflict:   run state is not resumable (pending/running/completed/cancelled).
//   - ErrValidation: FS/DB inconsistency without force, or resumer not configured.
func (s *RunService) Resume(ctx context.Context, id string, force bool) (*domain.Run, *domain.InconsistencyReport, error) {
	if s.resumer == nil {
		return nil, nil, fmt.Errorf("resume run: %w: engine not configured", domain.ErrValidation)
	}
	report, err := s.resumer.ResumeWithOptions(ctx, id, pipeline.ResumeOptions{Force: force})
	if err != nil {
		return nil, report, fmt.Errorf("resume run: %w", err)
	}
	run, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, report, fmt.Errorf("resume run: reload: %w", err)
	}
	return run, report, nil
}

// PrepareResume runs the synchronous portion of Resume (validation,
// consistency check, artifact cleanup, status reset) and returns the
// post-reset run snapshot plus any FS/DB inconsistency warnings bypassed
// via force. ExecuteResume must follow to perform the actual stage work.
//
// The API handler calls this synchronously to fail fast on 4xx errors, then
// dispatches ExecuteResume on a detached context so Phase B (TTS/image,
// minutes-long) is not bound to the HTTP request's WriteTimeout.
func (s *RunService) PrepareResume(ctx context.Context, id string, force bool) (*domain.Run, *domain.InconsistencyReport, error) {
	if s.resumer == nil {
		return nil, nil, fmt.Errorf("resume run: %w: engine not configured", domain.ErrValidation)
	}
	run, report, err := s.resumer.PrepareResume(ctx, id, pipeline.ResumeOptions{Force: force})
	if err != nil {
		return nil, report, fmt.Errorf("resume run: %w", err)
	}
	return run, report, nil
}

// ExecuteResume runs Phase B/C/metadata for a run that PrepareResume has
// already prepared. Long-running; intended to be dispatched in a goroutine
// with context.Background() by the API handler. Errors are returned for
// caller-side logging; the engine itself transitions the run back to
// `failed` status on stage error so the UI's status poll observes it.
func (s *RunService) ExecuteResume(ctx context.Context, id string) error {
	if s.resumer == nil {
		return fmt.Errorf("resume run: %w: engine not configured", domain.ErrValidation)
	}
	if err := s.resumer.ExecuteResume(ctx, id); err != nil {
		return fmt.Errorf("resume run: %w", err)
	}
	return nil
}

// PrepareAdvance validates that the run can be advanced and returns it.
// Call this synchronously before dispatching ExecuteAdvance in a goroutine
// so that configuration errors and missing runs surface immediately as typed
// HTTP errors rather than silently failing in the background.
func (s *RunService) PrepareAdvance(ctx context.Context, id string) (*domain.Run, error) {
	if s.advancer == nil {
		return nil, fmt.Errorf("advance run: %w: engine not configured", domain.ErrValidation)
	}
	run, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("advance run: %w", err)
	}
	return run, nil
}

// ExecuteAdvance runs the engine advance for a run. Intended to be called in
// a goroutine after PrepareAdvance has succeeded; the engine writes the result
// (success or failure) directly to the DB via ApplyPhaseAResult.
func (s *RunService) ExecuteAdvance(ctx context.Context, id string) error {
	if err := s.advancer.Advance(ctx, id); err != nil {
		return fmt.Errorf("advance run: %w", err)
	}
	return nil
}

// Advance dispatches automated execution from a non-HITL stage. The primary
// use case is kicking off a freshly-created pending run (Phase A entry); the
// engine also supports advancing from image/tts/assemble. HITL stages return
// ErrConflict from the engine and are propagated unchanged.
//
// Error classes propagated from the engine:
//   - ErrNotFound:   run does not exist.
//   - ErrConflict:   stage is a HITL boundary or has no automated dispatch.
//   - ErrValidation: required executor (phase a/b/c) is not configured.
func (s *RunService) Advance(ctx context.Context, id string) (*domain.Run, error) {
	run, err := s.PrepareAdvance(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.advancer.Advance(ctx, id); err != nil {
		return nil, fmt.Errorf("advance run: %w", err)
	}
	run, err = s.store.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("advance run: reload: %w", err)
	}
	return run, nil
}

// Rewind drives the operator-initiated stepper rewind for runID to the
// given work-phase node. Validates the node + delegates to the engine,
// which performs the cancel-wait + DB cleanup + FS cleanup orchestration.
//
// Error classes propagated from the engine:
//   - ErrNotFound:   run does not exist.
//   - ErrConflict:   target is not strictly before current run stage.
//   - ErrValidation: node is not rewindable, or rewinder is nil.
func (s *RunService) Rewind(ctx context.Context, runID string, node pipeline.StageNodeKey) (*domain.Run, error) {
	if s.rewinder == nil {
		return nil, fmt.Errorf("rewind run: %w: engine not configured", domain.ErrValidation)
	}
	run, err := s.rewinder.Rewind(ctx, runID, node)
	if err != nil {
		return nil, fmt.Errorf("rewind run: %w", err)
	}
	return run, nil
}

// AcknowledgeMetadata transitions a run from metadata_ack+waiting to complete+completed.
// Returns ErrNotFound if the run does not exist, ErrConflict if it is not in the
// correct stage/status. This is the NFR-L1 enforcement point: ready-for-upload is
// ONLY reachable via this path. The stage/status guard is enforced atomically in
// RunStore.MarkComplete to eliminate TOCTOU races with concurrent Cancel.
func (s *RunService) AcknowledgeMetadata(ctx context.Context, runID string) (*domain.Run, error) {
	if err := s.store.MarkComplete(ctx, runID); err != nil {
		return nil, fmt.Errorf("acknowledge metadata: %w", err)
	}
	return s.store.Get(ctx, runID)
}

// ApproveScenarioReview transitions a run from scenario_review/waiting to
// character_pick/waiting on operator approve. This is the only code path that
// resolves the scenario_review HITL gate; without it, every run that reaches
// scenario_review is permanently stuck (P0 unblocker).
//
// Mirrors CharacterService.Pick semantics: stage/status guard → settings
// promote at boundary → atomic stage advance preserving CriticScore and
// ScenarioPath → drop the scenario_review hitl_sessions row and upsert the
// character_pick row so the UI sees a session anchor on its next poll.
//
// Returns ErrConflict when the run is not at scenario_review/waiting,
// ErrNotFound when the run does not exist.
func (s *RunService) ApproveScenarioReview(ctx context.Context, runID string) (*domain.Run, error) {
	run, err := s.store.Get(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("approve scenario review: %w", err)
	}
	if run.Stage != domain.StageScenarioReview {
		return nil, fmt.Errorf("approve scenario review: %w: run stage is %s", domain.ErrConflict, run.Stage)
	}
	if run.Status != domain.StatusWaiting {
		return nil, fmt.Errorf("approve scenario review: %w: run status is %s", domain.ErrConflict, run.Status)
	}
	nextStage, err := pipeline.NextStage(run.Stage, domain.EventApprove)
	if err != nil {
		return nil, fmt.Errorf("approve scenario review: next stage: %w", err)
	}
	res := domain.PhaseAAdvanceResult{
		Stage:        nextStage,
		Status:       pipeline.StatusForStage(nextStage),
		CriticScore:  run.CriticScore,
		ScenarioPath: run.ScenarioPath,
		// RetryReason intentionally cleared: an approve is the success path,
		// any prior write/critic retry reason is no longer relevant.
		RetryReason: nil,
	}
	if err := s.store.ApplyPhaseAResult(ctx, runID, res); err != nil {
		return nil, fmt.Errorf("approve scenario review: %w", err)
	}
	// HITL row management is best-effort: a row inconsistency is recoverable
	// (read-time backfill) but a stage advance failure is not. So failures
	// past this point do NOT roll back the transition.
	if s.hitlSessions != nil {
		clk := s.clock
		if clk == nil {
			clk = clock.RealClock{}
		}
		// Drop the scenario_review row so the existing UpsertSessionFromState
		// helper builds a fresh character_pick row in one call. The helper
		// already deletes when the run is non-HITL — but here both the old
		// and the new state are HITL, so we must explicitly delete first.
		if err := s.hitlSessions.DeleteSession(ctx, runID); err != nil {
			// Tolerable — the upsert below will overwrite the row anyway,
			// but log so the gap is visible if it ever blocks debugging.
			_ = err
		}
		if _, err := pipeline.UpsertSessionFromState(ctx, s.hitlSessions, clk, runID, nextStage, pipeline.StatusForStage(nextStage)); err != nil {
			// Non-fatal: the run is already in character_pick/waiting; the
			// next read-time path will rebuild the row.
			_ = err
		}
	}
	return s.store.Get(ctx, runID)
}

// FinalizeBatchReview transitions a run from batch_review/waiting to
// assemble/waiting once every scene has a decision. Mirrors the manual gate
// pattern introduced by character_pick → image (commit 596e9be): the run
// parks at assemble/waiting so the UI surfaces a "Generate Video" button,
// and the operator dispatches Phase C via /advance.
//
// Returns ErrConflict when the run is not at batch_review/waiting OR when
// any scene is still pending review (PendingCount > 0). Returns ErrNotFound
// when the run does not exist. The pending-count guard treats skipped scenes
// as decided (snapshotStatusSkipped), matching BuildSessionSnapshot.
func (s *RunService) FinalizeBatchReview(ctx context.Context, runID string) (*domain.Run, error) {
	run, err := s.store.Get(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("finalize batch review: %w", err)
	}
	if run.Stage != domain.StageBatchReview {
		return nil, fmt.Errorf("finalize batch review: %w: run stage is %s", domain.ErrConflict, run.Stage)
	}
	if run.Status != domain.StatusWaiting {
		return nil, fmt.Errorf("finalize batch review: %w: run status is %s", domain.ErrConflict, run.Status)
	}

	// Pending guard: refuse the transition while any scene still needs a
	// decision. Mirrors the UI's actionable_count: pending = total - approved
	// - rejected. V1 skip_and_remember leaves segments.review_status unchanged
	// (BatchReview UI counts the scene as still actionable), so skipped scenes
	// intentionally block finalize here too — keeps backend and UI aligned.
	if s.hitlSessions != nil {
		counts, err := s.hitlSessions.DecisionCountsByRunID(ctx, runID)
		if err != nil {
			return nil, fmt.Errorf("finalize batch review: decision counts: %w", err)
		}
		pending := counts.TotalScenes - counts.Approved - counts.Rejected
		if pending > 0 {
			return nil, fmt.Errorf("finalize batch review: %w: %d scenes still pending review", domain.ErrConflict, pending)
		}
	}

	nextStage, err := pipeline.NextStage(run.Stage, domain.EventApprove)
	if err != nil {
		return nil, fmt.Errorf("finalize batch review: next stage: %w", err)
	}
	// Manual gate: park at assemble/waiting (override StatusForStage's default
	// of StatusRunning). Operator triggers Phase C via /advance.
	res := domain.PhaseAAdvanceResult{
		Stage:        nextStage,
		Status:       domain.StatusWaiting,
		CriticScore:  run.CriticScore,
		ScenarioPath: run.ScenarioPath,
		RetryReason:  nil,
	}
	if err := s.store.ApplyPhaseAResult(ctx, runID, res); err != nil {
		return nil, fmt.Errorf("finalize batch review: %w", err)
	}
	// Drop the batch_review hitl_sessions row — assemble is non-HITL so
	// UpsertSessionFromState short-circuits to DeleteSession.
	if s.hitlSessions != nil {
		clk := s.clock
		if clk == nil {
			clk = clock.RealClock{}
		}
		if _, err := pipeline.UpsertSessionFromState(ctx, s.hitlSessions, clk, runID, nextStage, domain.StatusWaiting); err != nil {
			// Best-effort: row inconsistency is recoverable, the stage
			// advance is not. Mirrors ApproveScenarioReview.
			_ = err
		}
	}
	return s.store.Get(ctx, runID)
}
