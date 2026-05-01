package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline/agents"
)

// RunStore is the minimal persistence surface the Engine needs.
// Declared here (not in service/) to keep pipeline/ independent of service/.
// *db.RunStore satisfies this interface structurally.
type RunStore interface {
	Get(ctx context.Context, id string) (*domain.Run, error)
	ResetForResume(ctx context.Context, id string, status domain.Status) error
	ApplyPhaseAResult(ctx context.Context, runID string, res domain.PhaseAAdvanceResult) error
}

type PhaseAExecutor interface {
	Run(ctx context.Context, state *agents.PipelineState) error
}

type PhaseBExecutor interface {
	Run(ctx context.Context, req PhaseBRequest) (PhaseBResult, error)
}

// SegmentStore is the minimal persistence surface the Engine needs for
// Phase B clean-slate semantics. *db.SegmentStore satisfies it structurally.
type SegmentStore interface {
	ListByRunID(ctx context.Context, runID string) ([]*domain.Episode, error)
	DeleteByRunID(ctx context.Context, runID string) (int64, error)
	ClearClipPathsByRunID(ctx context.Context, runID string) (int64, error)
	ClearImageArtifactsByRunID(ctx context.Context, runID string) (int64, error)
	ClearTTSArtifactsByRunID(ctx context.Context, runID string) (int64, error)
}

// NarrationSeeder is the narrow persistence surface the Engine uses to
// seed segments rows from a NarrationScript at the Phase A → scenario_review
// boundary. *db.SegmentStore satisfies this structurally via SeedFromNarration.
// Without seeding, segments stays empty until Phase B runs, leaving the
// scenario_review UI with no scenes to render.
type NarrationSeeder interface {
	SeedFromNarration(ctx context.Context, runID string, scenes []domain.NarrationScene) (int64, error)
}

// HITLSessionCleaner is the narrow persistence surface the Engine uses to
// drop stale hitl_sessions rows when a run exits HITL state on Resume.
// *db.DecisionStore satisfies this interface structurally. Story 2.6
// invariant: hitl_sessions row exists iff run.status=waiting AND
// run.stage ∈ HITL stages. Resume that transitions the run out of that
// state must clean the row.
type HITLSessionCleaner interface {
	DeleteSession(ctx context.Context, runID string) error
}

// CriticReportStore persists Phase A critic checkpoint reports and the
// narration attempts they evaluated. Every attempt (pass/retry/accept) is
// recorded so retry verdicts can be audited and writer prompts iterated
// against concrete failure cases. *db.CriticReportStore satisfies this
// interface structurally. Nil is tolerated: if no store is wired, Phase A
// proceeds without persisting diagnostics (used by tests/tools).
type CriticReportStore interface {
	InsertCriticReport(ctx context.Context, runID string, attemptNumber int, report domain.CriticCheckpointReport) error
	InsertNarrationAttempt(ctx context.Context, runID string, attemptNumber int, narration *domain.NarrationScript) error
}

// ResumeOptions controls optional Resume behavior.
type ResumeOptions struct {
	// Force bypasses the FS/DB inconsistency abort. Warnings are still logged.
	Force bool
}

// Engine orchestrates pipeline lifecycle operations. Advance dispatches
// automated stage execution (Phase A, Phase B, Phase C) and rejects HITL
// boundaries; Resume re-enters a failed/waiting stage with cleanup and
// consistency-check semantics. The Engine satisfies pipeline.Runner.
type Engine struct {
	runs     RunStore
	segments SegmentStore
	sessions HITLSessionCleaner
	// hitlSessions is the full session store used to upsert hitl_sessions
	// rows when Advance transitions a run INTO a HITL stage (scenario_review,
	// batch_review, metadata_ack). Optional — nil keeps the legacy behavior
	// where rows are only DELETED on Resume out of HITL state, leaving the
	// row missing on transition (manifests as the UI seeing "no session" and
	// failing to render scenes).
	hitlSessions   HITLSessionStore
	narrationSeed  NarrationSeeder
	criticReports  CriticReportStore
	phaseA         PhaseAExecutor
	phaseB         PhaseBExecutor
	phaseC         *PhaseCRunner
	phaseCMetadata MetadataBuilder
	clock          clock.Clock
	outputDir      string
	logger         *slog.Logger

	// rewindStore wires the rewind-only persistence primitives (parking
	// the run in cancelled, then applying the bucket-aware reset).
	// Nil disables Engine.Rewind with ErrValidation. *db.RunStore
	// satisfies it.
	rewindStore RewindStore
	// cancelRegistry tracks in-flight stage execution per run so
	// Rewind can interrupt and wait for clean unwinding before
	// destructive cleanup. Nil = no race protection (legacy/test paths
	// that never reach the rewind entry point).
	cancelRegistry *CancelRegistry
	// rewindLocks serializes Rewind requests per-run so a double-click
	// or retried POST cannot interleave two cleanup passes.
	rewindLocks *rewindLocks
}

// NewEngine constructs an Engine. outputDir is the base output path
// ({cfg.OutputDir}); per-run directories are joined by run ID.
// sessions MAY be nil for call paths that never Resume a HITL run
// (tests, tools); nil skips the Story 2.6 session-cleanup step.
func NewEngine(runs RunStore, segments SegmentStore, sessions HITLSessionCleaner, clk clock.Clock, outputDir string, logger *slog.Logger) *Engine {
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{
		runs:        runs,
		segments:    segments,
		sessions:    sessions,
		clock:       clk,
		outputDir:   outputDir,
		logger:      logger,
		rewindLocks: newRewindLocks(),
	}
}

func (e *Engine) SetPhaseAExecutor(exec PhaseAExecutor) {
	e.phaseA = exec
}

func (e *Engine) SetPhaseBExecutor(exec PhaseBExecutor) {
	e.phaseB = exec
}

func (e *Engine) SetPhaseCRunner(runner *PhaseCRunner) {
	e.phaseC = runner
}

// SetPhaseCMetadataBuilder sets the metadata builder used during the
// StageAssemble → StageMetadataAck transition. May be nil (no metadata
// written), which skips the metadata entry call.
func (e *Engine) SetPhaseCMetadataBuilder(builder MetadataBuilder) {
	e.phaseCMetadata = builder
}

// SetNarrationSeeder wires the segments seeder invoked at the Phase A →
// scenario_review boundary. Without this, /api/runs/{id}/scenes returns
// empty during scenario_review because no production code creates segments
// rows until Phase B starts. Nil disables seeding (acceptable for
// tests/tools that operate strictly post-Phase-B).
func (e *Engine) SetNarrationSeeder(s NarrationSeeder) {
	e.narrationSeed = s
}

// SetCriticReportStore wires the persistence target for Phase A critic
// checkpoints and narration attempts. Nil disables persistence (acceptable
// for tests/tools).
func (e *Engine) SetCriticReportStore(s CriticReportStore) {
	e.criticReports = s
}

// seedSegmentsAtScenarioReview is the post-Phase-A hook that creates the
// segments rows from the in-memory NarrationScript. Best-effort: a seed
// failure is logged but the transition still completes (scenes will simply
// stay empty until manual intervention; the run is not stuck).
func (e *Engine) seedSegmentsAtScenarioReview(ctx context.Context, runID string, narration *domain.NarrationScript) {
	if e.narrationSeed == nil || narration == nil || len(narration.Scenes) == 0 {
		return
	}
	inserted, err := e.narrationSeed.SeedFromNarration(ctx, runID, narration.Scenes)
	if err != nil {
		e.logger.Warn("seed segments at scenario_review failed",
			"run_id", runID, "scenes", len(narration.Scenes), "error", err.Error())
		return
	}
	e.logger.Info("seeded segments from narration",
		"run_id", runID, "rows_inserted", inserted, "scenes_total", len(narration.Scenes))
}

// SetHITLSessionStore wires the full HITL session store used to upsert
// hitl_sessions rows when Advance transitions a run INTO a HITL stage
// (scenario_review, batch_review, metadata_ack). Without this, the row stays
// missing after the transition and the UI shows "no session" + empty scene
// list. Nil disables the upsert (legacy behavior; tests/tools).
func (e *Engine) SetHITLSessionStore(s HITLSessionStore) {
	e.hitlSessions = s
}

// upsertHITLSessionAtTransition is the post-transition hook that creates
// the hitl_sessions row when a run lands in a HITL wait state. Best-effort:
// failures are logged but never override the transition's success. No-op
// when hitlSessions is unset or stage is not a HITL stage.
func (e *Engine) upsertHITLSessionAtTransition(ctx context.Context, runID string, stage domain.Stage) {
	if e.hitlSessions == nil {
		return
	}
	if !IsHITLStage(stage) {
		return
	}
	if _, err := UpsertSessionFromState(ctx, e.hitlSessions, e.clock, runID, stage, domain.StatusWaiting); err != nil {
		e.logger.Warn("upsert hitl session at transition failed",
			"run_id", runID, "stage", string(stage), "error", err.Error())
	}
}

// Advance dispatches automated execution for runID based on the run's current
// stage. Phase A (pending→critic), Phase B (image/tts), and Phase C (assemble)
// are each forwarded to their configured executor. HITL stages
// (scenario_review, character_pick, batch_review, metadata_ack) return
// ErrConflict — Advance must never silently auto-approve an operator boundary.
func (e *Engine) Advance(ctx context.Context, runID string) error {
	if e.cancelRegistry != nil {
		var release func()
		ctx, _, release = e.cancelRegistry.Begin(ctx, runID)
		defer release()
	}
	run, err := e.runs.Get(ctx, runID)
	if err != nil {
		return fmt.Errorf("advance %s: %w", runID, err)
	}

	// HITL stages are operator-gated boundaries; Advance must not bypass them.
	if IsHITLStage(run.Stage) {
		return fmt.Errorf("advance %s: %w: stage %s is a HITL boundary", runID, domain.ErrConflict, run.Stage)
	}

	switch {
	case isPhaseAEntryStage(run.Stage):
		return e.advancePhaseA(ctx, runID, run)
	case isPhaseB(run.Stage):
		if e.phaseB == nil {
			return fmt.Errorf("advance %s: %w: phase b executor is nil", runID, domain.ErrValidation)
		}
		segs, err := e.segments.ListByRunID(ctx, runID)
		if err != nil {
			return fmt.Errorf("advance %s: load segments: %w", runID, err)
		}
		if err := e.runPhaseB(ctx, runID, run, segs); err != nil {
			return fmt.Errorf("advance %s: %w", runID, err)
		}
		return nil
	case run.Stage == domain.StageAssemble:
		if e.phaseC == nil {
			return fmt.Errorf("advance %s: %w: phase c runner is nil", runID, domain.ErrValidation)
		}
		segs, err := e.segments.ListByRunID(ctx, runID)
		if err != nil {
			return fmt.Errorf("advance %s: load segments: %w", runID, err)
		}
		if err := e.runPhaseC(ctx, runID, run, segs); err != nil {
			return fmt.Errorf("advance %s: %w", runID, err)
		}
		return nil
	default:
		return fmt.Errorf("advance %s: %w: stage %s has no automated dispatch", runID, domain.ErrConflict, run.Stage)
	}
}

// advancePhaseA runs the Phase A chain from any entry stage (pending→critic).
func (e *Engine) advancePhaseA(ctx context.Context, runID string, run *domain.Run) error {
	if e.phaseA == nil {
		return fmt.Errorf("advance %s: %w: phase a executor is nil", runID, domain.ErrValidation)
	}

	// Transition to research/running immediately so the UI stops showing the
	// "pending" guidance card and can display an in-progress indicator instead.
	// Terminal states (scenario_review/waiting or write/failed) are written by
	// ApplyPhaseAResult at the end of the chain.
	if err := e.runs.ApplyPhaseAResult(ctx, runID, domain.PhaseAAdvanceResult{
		Stage:  domain.StageResearch,
		Status: domain.StatusRunning,
	}); err != nil {
		return fmt.Errorf("advance %s: mark running: %w", runID, err)
	}

	state := &agents.PipelineState{
		RunID: run.ID,
		SCPID: run.SCPID,
		OnSubStageStart: func(ctx context.Context, ps agents.PipelineStage) error {
			return e.runs.ApplyPhaseAResult(ctx, runID, domain.PhaseAAdvanceResult{
				Stage:  ps.DomainStage(),
				Status: domain.StatusRunning,
			})
		},
	}
	if err := e.phaseA.Run(ctx, state); err != nil {
		return fmt.Errorf("advance %s: %w", runID, err)
	}
	if state.Critic == nil || state.Critic.PostReviewer == nil {
		return fmt.Errorf("advance %s: %w: post_reviewer critic result missing", runID, domain.ErrValidation)
	}

	postReviewer := state.Critic.PostReviewer
	if postReviewer.Verdict == domain.CriticVerdictRetry {
		var score *float64
		if !postReviewer.Precheck.ShortCircuited {
			normalized := NormalizeCriticScore(postReviewer.OverallScore)
			score = &normalized
		}
		res := domain.PhaseAAdvanceResult{
			Stage:       domain.StageWrite,
			Status:      domain.StatusFailed,
			RetryReason: stringPtrOrNil(postReviewer.RetryReason),
			CriticScore: score,
		}
		if err := e.runs.ApplyPhaseAResult(ctx, runID, res); err != nil {
			return fmt.Errorf("advance %s: apply phase a retry result: %w", runID, err)
		}
		return fmt.Errorf("advance %s: %w: %s", runID, domain.ErrStageFailed, postReviewer.RetryReason)
	}

	if state.Quality == nil || state.Contracts == nil {
		return fmt.Errorf("advance %s: %w: final phase a state missing quality or contracts", runID, domain.ErrValidation)
	}
	if _, err := os.Stat(ScenarioPath(e.outputDir, runID)); err != nil {
		return fmt.Errorf("advance %s: scenario artifact missing: %w", runID, err)
	}

	score := NormalizeCriticScore(postReviewer.OverallScore)
	scenarioPath := "scenario.json"
	res := domain.PhaseAAdvanceResult{
		Stage:        domain.StageScenarioReview,
		Status:       domain.StatusWaiting,
		CriticScore:  &score,
		ScenarioPath: &scenarioPath,
	}
	if err := e.runs.ApplyPhaseAResult(ctx, runID, res); err != nil {
		return fmt.Errorf("advance %s: apply phase a success result: %w", runID, err)
	}
	// Seed segments BEFORE the HITL session upsert so that a status poll
	// arriving immediately after the transition sees both the session row
	// and the per-scene rows the UI renders. Best-effort: a seed failure
	// is logged but does not block the transition.
	e.seedSegmentsAtScenarioReview(ctx, runID, state.Narration)
	// HITL-row upsert MUST follow ApplyPhaseAResult: the runs row drives
	// hitl_service's IsHITLStage check, so the session row is meaningless
	// until the run is actually in scenario_review/waiting.
	e.upsertHITLSessionAtTransition(ctx, runID, domain.StageScenarioReview)
	return nil
}

// runPhaseB executes Phase B for a run whose stage is image or tts.
// On success: advances the run to StageBatchReview/StatusWaiting.
// On Phase B error: rolls back to run.Stage/StatusFailed (preserving existing
// meta fields so retry context survives the failure).
// Callers are responsible for promoting settings and loading segments before
// calling this method.
func (e *Engine) runPhaseB(ctx context.Context, runID string, run *domain.Run, segments []*domain.Episode) error {
	// Mark running before dispatching so the UI stops showing the "Generate
	// Assets" button and reflects in-progress state while tracks execute.
	if err := e.runs.ApplyPhaseAResult(ctx, runID, domain.PhaseAAdvanceResult{
		Stage:        run.Stage,
		Status:       domain.StatusRunning,
		CriticScore:  run.CriticScore,
		ScenarioPath: run.ScenarioPath,
		RetryReason:  run.RetryReason,
	}); err != nil {
		return fmt.Errorf("mark phase b running: %w", err)
	}
	req := PhaseBRequest{
		RunID:        runID,
		Stage:        run.Stage,
		ScenarioPath: ScenarioPath(e.outputDir, runID),
		Segments:     segments,
	}
	if _, err := e.phaseB.Run(ctx, req); err != nil {
		res := domain.PhaseAAdvanceResult{
			Stage:        run.Stage,
			Status:       domain.StatusFailed,
			RetryReason:  run.RetryReason,
			CriticScore:  run.CriticScore,
			ScenarioPath: run.ScenarioPath,
		}
		if fixErr := e.runs.ApplyPhaseAResult(ctx, runID, res); fixErr != nil {
			e.logger.Warn("failed to reset status after phase b error",
				"run_id", runID, "error", fixErr)
		}
		return fmt.Errorf("phase b run: %w", err)
	}
	res := domain.PhaseAAdvanceResult{
		Stage:        domain.StageBatchReview,
		Status:       domain.StatusWaiting,
		RetryReason:  nil,
		CriticScore:  run.CriticScore,
		ScenarioPath: run.ScenarioPath,
	}
	if err := e.runs.ApplyPhaseAResult(ctx, runID, res); err != nil {
		return fmt.Errorf("apply phase b success result: %w", err)
	}
	e.upsertHITLSessionAtTransition(ctx, runID, domain.StageBatchReview)
	return nil
}

// runPhaseC executes Phase C assembly and metadata entry for a run at
// StageAssemble. On success: advances to StageMetadataAck/StatusWaiting.
// On assembly error: rolls back to StageAssemble/StatusFailed.
// On metadata error: rolls back to StageMetadataAck/StatusFailed so a
// targeted retry can regenerate compliance files without re-running assembly.
// All rollback paths preserve Phase A meta fields (CriticScore, ScenarioPath,
// RetryReason) — ApplyPhaseAResult writes a full row, so omitting them would
// silently wipe Phase A artifacts visible to the UI.
// Callers are responsible for promoting settings and loading segments before
// calling this method.
func (e *Engine) runPhaseC(ctx context.Context, runID string, run *domain.Run, segments []*domain.Episode) error {
	// Block dry-run rows from composing placeholder image/audio into a final
	// video. The runs.dry_run column is an immutable snapshot of the effective
	// DryRun flag at row creation, so this guard cannot be bypassed by
	// toggling Settings off mid-flight. Centralized here (not at the
	// Engine.Advance dispatch site) so Resume — which also reaches Phase C
	// via ExecuteResume — is covered without a second guard. Error is wrapped
	// as ErrValidation so the API layer surfaces it as 4xx, not 5xx.
	if run.DryRun {
		return fmt.Errorf("phase c: %w: run was created in dry-run mode; placeholder image/audio cannot be assembled", domain.ErrValidation)
	}
	// Mark running before dispatching so the UI stops showing the "Generate
	// Video" gate and reflects in-progress state while assembly executes.
	// Mirrors runPhaseB's manual-gate flow.
	if err := e.runs.ApplyPhaseAResult(ctx, runID, domain.PhaseAAdvanceResult{
		Stage:        run.Stage,
		Status:       domain.StatusRunning,
		CriticScore:  run.CriticScore,
		ScenarioPath: run.ScenarioPath,
		RetryReason:  run.RetryReason,
	}); err != nil {
		return fmt.Errorf("mark phase c running: %w", err)
	}
	// Reflect the new status locally so downstream rollback paths preserve
	// "running"-derived metadata if Phase C errors out.
	run.Status = domain.StatusRunning
	runDir := filepath.Join(e.outputDir, runID)
	req := PhaseCRequest{
		RunID:    runID,
		RunDir:   runDir,
		Segments: segments,
	}
	if _, err := e.phaseC.Run(ctx, req); err != nil {
		if fixErr := e.runs.ApplyPhaseAResult(ctx, runID, domain.PhaseAAdvanceResult{
			Stage:        run.Stage,
			Status:       domain.StatusFailed,
			RetryReason:  run.RetryReason,
			CriticScore:  run.CriticScore,
			ScenarioPath: run.ScenarioPath,
		}); fixErr != nil {
			e.logger.Warn("failed to reset status after phase c error",
				"run_id", runID, "error", fixErr)
		}
		return fmt.Errorf("phase c assembly: %w", err)
	}

	nextStage, err := NextStage(run.Stage, domain.EventComplete)
	if err != nil {
		return fmt.Errorf("compute next stage: %w", err)
	}
	res := domain.PhaseAAdvanceResult{
		Stage:        nextStage,
		Status:       StatusForStage(nextStage),
		CriticScore:  run.CriticScore,
		ScenarioPath: run.ScenarioPath,
	}
	if err := e.runs.ApplyPhaseAResult(ctx, runID, res); err != nil {
		return fmt.Errorf("apply stage advance: %w", err)
	}

	// Build and write metadata bundles after DB stage advance (spec: entry
	// action for metadata_ack). A failure here rolls back to metadata_ack/failed
	// so the operator can retry metadata generation without re-assembling.
	if e.phaseCMetadata != nil {
		if err := PhaseCMetadataEntry(ctx, e.phaseCMetadata, runID); err != nil {
			if fixErr := e.runs.ApplyPhaseAResult(ctx, runID, domain.PhaseAAdvanceResult{
				Stage:        nextStage,
				Status:       domain.StatusFailed,
				CriticScore:  run.CriticScore,
				ScenarioPath: run.ScenarioPath,
			}); fixErr != nil {
				e.logger.Warn("failed to reset status after metadata error",
					"run_id", runID, "error", fixErr)
			}
			return fmt.Errorf("phase c metadata entry: %w", err)
		}
	} else {
		e.logger.Warn("phase c metadata builder not configured — compliance files skipped",
			"run_id", runID)
	}
	e.upsertHITLSessionAtTransition(ctx, runID, nextStage)
	return nil
}

// Resume re-enters the failed (or waiting) stage of a run with default
// options. Returns the observed consistency report (empty on success,
// populated when Force bypassed mismatches).
func (e *Engine) Resume(ctx context.Context, runID string) (*domain.InconsistencyReport, error) {
	return e.ResumeWithOptions(ctx, runID, ResumeOptions{})
}

// ResumeWithOptions is the full Resume orchestration: PrepareResume +
// ExecuteResume in one synchronous call. Used by the CLI; the API splits
// these so Phase B/C work runs detached from the HTTP request lifetime.
func (e *Engine) ResumeWithOptions(ctx context.Context, runID string, opts ResumeOptions) (*domain.InconsistencyReport, error) {
	_, report, err := e.PrepareResume(ctx, runID, opts)
	if err != nil {
		return report, err
	}
	if err := e.ExecuteResume(ctx, runID); err != nil {
		return report, err
	}
	return report, nil
}

// PrepareResume performs the synchronous portion of Resume:
//
//  1. Load run.
//  2. Validate: stage is a known constant AND stage != complete AND status
//     ∈ {failed, waiting}.
//  3. Load segments.
//  4. FS/DB consistency check (BEFORE any cleanup).
//  5. Abort on mismatches unless opts.Force.
//  6. Clean partial artifacts scoped to the failed stage. Phase B failures
//     preserve successful sibling-track artifacts when partial success exists;
//     otherwise they fall back to the historical clean-slate rerun.
//  7. If Phase B without preservable partial success: DELETE all segments for
//     the run. Mixed-result resumes instead clear only failed-track fields.
//     If assemble: NULL every segment's clip_path (the file is gone).
//  8. ResetForResume — a single UPDATE that sets status, clears
//     retry_reason, and increments retry_count atomically (no torn-state
//     window between two UPDATEs).
//
// On nil error the run is in `running`/`waiting` status, segments and
// filesystem are clean, and the returned snapshot reflects the post-reset
// state. The caller MUST follow with ExecuteResume to actually run the
// stage's automated work. Splitting these lets the API return 202 fast and
// dispatch Phase B (TTS/image, minutes-long) on a context detached from
// the inbound HTTP request — http.Server.WriteTimeout would otherwise
// cancel mid-flight.
func (e *Engine) PrepareResume(ctx context.Context, runID string, opts ResumeOptions) (*domain.Run, *domain.InconsistencyReport, error) {
	run, err := e.runs.Get(ctx, runID)
	if err != nil {
		return nil, nil, fmt.Errorf("resume %s: %w", runID, err)
	}

	if !run.Stage.IsValid() {
		return nil, nil, fmt.Errorf("resume %s: %w: invalid stage %q", runID, domain.ErrValidation, run.Stage)
	}
	if err := validateResumable(run); err != nil {
		return nil, nil, fmt.Errorf("resume %s: %w", runID, err)
	}

	segments, err := e.segments.ListByRunID(ctx, runID)
	if err != nil {
		return nil, nil, fmt.Errorf("resume %s: %w", runID, err)
	}

	runDir := filepath.Join(e.outputDir, runID)

	report, err := CheckConsistency(runDir, run, segments)
	if err != nil {
		return nil, nil, fmt.Errorf("resume %s: consistency check: %w", runID, err)
	}
	if len(report.Mismatches) > 0 {
		for _, m := range report.Mismatches {
			e.logger.Warn("fs/db mismatch",
				"run_id", runID, "stage", run.Stage,
				"kind", m.Kind, "path", m.Path, "detail", m.Detail)
		}
		// Auto-bypass when the run was marked failed by the orphan reconciler
		// at server startup. The mismatches in this case are mid-flight Phase
		// B/C writes that never finished — the resume cleanup path below
		// handles them. Blocking with ErrValidation is a UI dead-end since
		// the FailureBanner has no way to retry with confirm_inconsistent=true.
		// An explicit operator Cancel + Resume would still go through the
		// strict gate; only the system-reconciled marker triggers auto-force.
		systemReconciled := run.RetryReason != nil && *run.RetryReason == domain.RetryReasonServerRestarted
		if !opts.Force && !systemReconciled {
			return nil, report, fmt.Errorf("resume %s: %w: %s", runID, domain.ErrValidation, report.Error())
		}
		if systemReconciled && !opts.Force {
			e.logger.Info("resume: auto-bypassing consistency gate for reconciler-marked run",
				"run_id", runID, "stage", run.Stage, "mismatches", len(report.Mismatches))
		}
	}

	preserveSibling := phaseBPreservesSibling(run.Stage, segments)
	if isPhaseB(run.Stage) {
		if preserveSibling {
			if err := cleanFailedPhaseBTrack(runDir, run.Stage); err != nil {
				return nil, report, fmt.Errorf("resume %s: clean failed phase b track: %w", runID, err)
			}
		} else {
			if err := CleanStageArtifacts(runDir, domain.StageImage); err != nil {
				return nil, report, fmt.Errorf("resume %s: clean images: %w", runID, err)
			}
			if err := CleanStageArtifacts(runDir, domain.StageTTS); err != nil {
				return nil, report, fmt.Errorf("resume %s: clean tts: %w", runID, err)
			}
		}
	} else {
		if err := CleanStageArtifacts(runDir, run.Stage); err != nil {
			return nil, report, fmt.Errorf("resume %s: clean artifacts: %w", runID, err)
		}
	}

	if isPhaseB(run.Stage) {
		if preserveSibling {
			if err := clearFailedPhaseBTrack(ctx, e.segments, runID, run.Stage); err != nil {
				return nil, report, fmt.Errorf("resume %s: clear failed phase b track state: %w", runID, err)
			}
		} else {
			n, err := e.segments.DeleteByRunID(ctx, runID)
			if err != nil {
				return nil, report, fmt.Errorf("resume %s: delete segments: %w", runID, err)
			}
			e.logger.Info("segments deleted for phase b resume",
				"run_id", runID, "count", n)
		}
	} else if run.Stage == domain.StageAssemble {
		// clips/ is gone on disk; null the DB references so subsequent
		// consistency checks don't flag every segment forever.
		n, err := e.segments.ClearClipPathsByRunID(ctx, runID)
		if err != nil {
			return nil, report, fmt.Errorf("resume %s: clear clip_paths: %w", runID, err)
		}
		e.logger.Info("clip_paths cleared for assemble resume",
			"run_id", runID, "count", n)
	}

	newStatus := StatusForStage(run.Stage)
	if err := e.runs.ResetForResume(ctx, runID, newStatus); err != nil {
		return nil, report, fmt.Errorf("resume %s: reset: %w", runID, err)
	}

	updated, err := e.runs.Get(ctx, runID)
	if err != nil {
		return nil, report, fmt.Errorf("resume %s: reload: %w", runID, err)
	}

	e.logger.Info("run resume prepared",
		"run_id", runID, "stage", updated.Stage, "status", updated.Status,
		"force", opts.Force, "warnings", len(report.Mismatches))
	return updated, report, nil
}

// ExecuteResume runs the long-running portion of Resume (Phase B/C/metadata
// + HITL session cleanup) for a run that PrepareResume has already prepared.
// Re-loads the run + segments to operate on the post-reset state.
//
// Failures inside Phase B/C/metadata are responsible for transitioning the
// run back to `failed` status via ApplyPhaseAResult; this method just
// surfaces the error. Callers that dispatch this in a goroutine should
// use a context detached from any HTTP request lifetime.
func (e *Engine) ExecuteResume(ctx context.Context, runID string) error {
	if e.cancelRegistry != nil {
		var release func()
		ctx, _, release = e.cancelRegistry.Begin(ctx, runID)
		defer release()
	}
	run, err := e.runs.Get(ctx, runID)
	if err != nil {
		return fmt.Errorf("resume %s: %w", runID, err)
	}
	segments, err := e.segments.ListByRunID(ctx, runID)
	if err != nil {
		return fmt.Errorf("resume %s: %w", runID, err)
	}

	if isPhaseB(run.Stage) && e.phaseB != nil {
		if err := e.runPhaseB(ctx, runID, run, segments); err != nil {
			return fmt.Errorf("resume %s: %w", runID, err)
		}
	}

	if run.Stage == domain.StageAssemble && e.phaseC != nil {
		if err := e.runPhaseC(ctx, runID, run, segments); err != nil {
			return fmt.Errorf("resume %s: %w", runID, err)
		}
	}

	if run.Stage == domain.StageMetadataAck && e.phaseCMetadata != nil {
		if err := PhaseCMetadataEntry(ctx, e.phaseCMetadata, runID); err != nil {
			return fmt.Errorf("resume %s: phase c metadata entry: %w", runID, err)
		}
	}

	// Phase A entry stages (research/structure/write/visual_break/review/critic)
	// have no synchronous executor in this Resume path because Phase A is the
	// long-running multi-LLM chain — running it inline would block the request
	// past WriteTimeout. Mirror Advance's async dispatch pattern: kick off the
	// chain in a goroutine using context.Background() (request ctx is cancelled
	// when Resume returns 200), persist artifacts on completion, and rely on
	// the engine's stage transitions to surface progress to the UI via polling.
	if isPhaseAEntryStage(run.Stage) && e.phaseA != nil {
		runCopy := *run
		go func() {
			bgCtx := context.Background()
			if e.cancelRegistry != nil {
				var release func()
				bgCtx, _, release = e.cancelRegistry.Begin(bgCtx, runID)
				defer release()
			}
			if err := e.advancePhaseA(bgCtx, runID, &runCopy); err != nil {
				e.logger.Error("phase a after resume failed",
					"run_id", runID, "stage", runCopy.Stage, "error", err.Error())
				return
			}
			e.logger.Info("phase a after resume complete",
				"run_id", runID, "stage", runCopy.Stage)
		}()
	}

	// Story 2.6 cleanup: drop the hitl_sessions row when the run exits HITL
	// state. If still waiting (retry within the same HITL stage), the session
	// row stays and is updated by the next decision event.
	if e.sessions != nil && run.Status != domain.StatusWaiting {
		if err := e.sessions.DeleteSession(ctx, runID); err != nil {
			e.logger.Warn("resume: delete hitl_session failed",
				"run_id", runID, "error", err.Error())
		}
	}

	e.logger.Info("run resume executed",
		"run_id", runID, "stage", run.Stage, "status", run.Status)
	return nil
}

// validateResumable returns ErrConflict when the run cannot be resumed.
// Resumable states are: status ∈ {failed, waiting, cancelled} AND stage != complete.
// Everything else is a conflict. Stage validity is checked separately by
// the caller (IsValid) before this function is invoked.
func validateResumable(run *domain.Run) error {
	if run.Stage == domain.StageComplete {
		return fmt.Errorf("%w: run already at complete stage", domain.ErrConflict)
	}
	switch run.Status {
	case domain.StatusFailed, domain.StatusWaiting, domain.StatusCancelled:
		return nil
	case domain.StatusPending:
		return fmt.Errorf("%w: run has not started; use create/advance to begin", domain.ErrConflict)
	case domain.StatusRunning:
		return fmt.Errorf("%w: run already in progress", domain.ErrConflict)
	case domain.StatusCompleted:
		return fmt.Errorf("%w: run already completed", domain.ErrConflict)
	}
	return fmt.Errorf("%w: unknown status %q", domain.ErrConflict, run.Status)
}

func isPhaseB(s domain.Stage) bool {
	return s == domain.StageImage || s == domain.StageTTS
}

func phaseBPreservesSibling(stage domain.Stage, segments []*domain.Episode) bool {
	switch stage {
	case domain.StageImage:
		return hasSuccessfulTTSArtifacts(segments)
	case domain.StageTTS:
		return hasSuccessfulImageArtifacts(segments)
	default:
		return false
	}
}

func hasSuccessfulTTSArtifacts(segments []*domain.Episode) bool {
	for _, seg := range segments {
		if seg.TTSPath != nil && *seg.TTSPath != "" {
			return true
		}
	}
	return false
}

func hasSuccessfulImageArtifacts(segments []*domain.Episode) bool {
	for _, seg := range segments {
		for _, shot := range seg.Shots {
			if shot.ImagePath != "" {
				return true
			}
		}
	}
	return false
}

func cleanFailedPhaseBTrack(runDir string, stage domain.Stage) error {
	switch stage {
	case domain.StageImage:
		return CleanStageArtifacts(runDir, domain.StageImage)
	case domain.StageTTS:
		return CleanStageArtifacts(runDir, domain.StageTTS)
	default:
		return fmt.Errorf("unsupported phase b stage %s: %w", stage, domain.ErrValidation)
	}
}

func clearFailedPhaseBTrack(ctx context.Context, store SegmentStore, runID string, stage domain.Stage) error {
	switch stage {
	case domain.StageImage:
		_, err := store.ClearImageArtifactsByRunID(ctx, runID)
		return err
	case domain.StageTTS:
		_, err := store.ClearTTSArtifactsByRunID(ctx, runID)
		return err
	default:
		return fmt.Errorf("unsupported phase b stage %s: %w", stage, domain.ErrValidation)
	}
}

func isPhaseAEntryStage(s domain.Stage) bool {
	switch s {
	case domain.StagePending,
		domain.StageResearch,
		domain.StageStructure,
		domain.StageWrite,
		domain.StageVisualBreak,
		domain.StageReview,
		domain.StageCritic:
		return true
	}
	return false
}

func stringPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// formatPriorCriticFeedback builds the quality_feedback block injected into
// the writer prompt on Resume. It includes (a) rubric scores so the writer
// knows which dimensions to preserve and (b) the critic's specific feedback.
func formatPriorCriticFeedback(report *domain.CriticCheckpointReport) string {
	if report == nil {
		return ""
	}
	scoreLabel := func(score int) string {
		switch {
		case score >= 80:
			return "✅ GOOD — 유지하세요"
		case score >= 60:
			return "⚠️ 보통 — 개선 여지 있음"
		default:
			return "❌ 부족 — 반드시 개선"
		}
	}
	lines := []string{
		"## 이전 시도 품질 피드백",
		"",
		"이전 버전의 루브릭 점수입니다. **점수가 높은 항목은 반드시 유지하고, 낮은 항목만 개선하세요.**",
		fmt.Sprintf("- Hook: %d/100 %s", report.Rubric.Hook, scoreLabel(report.Rubric.Hook)),
		fmt.Sprintf("- Fact Accuracy: %d/100 %s", report.Rubric.FactAccuracy, scoreLabel(report.Rubric.FactAccuracy)),
		fmt.Sprintf("- Emotional Variation: %d/100 %s", report.Rubric.EmotionalVariation, scoreLabel(report.Rubric.EmotionalVariation)),
		fmt.Sprintf("- Immersion: %d/100 %s", report.Rubric.Immersion, scoreLabel(report.Rubric.Immersion)),
		"",
		"**구체적 피드백:**",
		report.Feedback,
	}
	return strings.Join(lines, "\n")
}
