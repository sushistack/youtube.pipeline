package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

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

// SettingsPromoter is the narrow surface the engine uses to flip queued
// settings versions to effective at stage boundaries. *service.SettingsService
// satisfies it structurally. Nil is tolerated for callers (tests, tools)
// that don't care about settings promotion.
type SettingsPromoter interface {
	PromotePendingAtSafeSeam(ctx context.Context) (bool, error)
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

// HITLSessionCleaner is the narrow persistence surface the Engine uses to
// drop stale hitl_sessions rows when a run exits HITL state on Resume.
// *db.DecisionStore satisfies this interface structurally. Story 2.6
// invariant: hitl_sessions row exists iff run.status=waiting AND
// run.stage ∈ HITL stages. Resume that transitions the run out of that
// state must clean the row.
type HITLSessionCleaner interface {
	DeleteSession(ctx context.Context, runID string) error
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
	runs           RunStore
	segments       SegmentStore
	sessions       HITLSessionCleaner
	phaseA         PhaseAExecutor
	phaseB         PhaseBExecutor
	phaseC         *PhaseCRunner
	phaseCMetadata MetadataBuilder
	settings       SettingsPromoter
	clock          clock.Clock
	outputDir      string
	logger         *slog.Logger
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
		runs:      runs,
		segments:  segments,
		sessions:  sessions,
		clock:     clk,
		outputDir: outputDir,
		logger:    logger,
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

// SetSettingsPromoter wires the queued-settings promoter that fires at every
// stage-advance boundary. Nil disables promotion (acceptable for tests).
func (e *Engine) SetSettingsPromoter(p SettingsPromoter) {
	e.settings = p
}

// promoteSettingsAtBoundary fires the queued-settings promoter at a stage
// boundary. Errors are logged but not propagated — a promotion failure
// should not block a stage transition that succeeded otherwise. The next
// boundary will retry.
func (e *Engine) promoteSettingsAtBoundary(ctx context.Context, runID string, from, to domain.Stage) {
	if e.settings == nil {
		return
	}
	promoted, err := e.settings.PromotePendingAtSafeSeam(ctx)
	if err != nil {
		e.logger.Warn("settings promotion at stage boundary failed",
			"run_id", runID, "from", from, "to", to, "error", err.Error())
		return
	}
	if promoted {
		e.logger.Info("settings promoted at stage boundary",
			"run_id", runID, "from", from, "to", to)
	}
}

// Advance dispatches automated execution for runID based on the run's current
// stage. Phase A (pending→critic), Phase B (image/tts), and Phase C (assemble)
// are each forwarded to their configured executor. HITL stages
// (scenario_review, character_pick, batch_review, metadata_ack) return
// ErrConflict — Advance must never silently auto-approve an operator boundary.
func (e *Engine) Advance(ctx context.Context, runID string) error {
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
		e.promoteSettingsAtBoundary(ctx, runID, run.Stage, run.Stage)
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
		e.promoteSettingsAtBoundary(ctx, runID, run.Stage, run.Stage)
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

	// Promote queued settings BEFORE Phase A begins so the full chain runs
	// against a coherent snapshot. Mid-chain promotion would violate the
	// invariant that each stage sees exactly one config.
	e.promoteSettingsAtBoundary(ctx, runID, run.Stage, domain.StageResearch)

	state := &agents.PipelineState{
		RunID: run.ID,
		SCPID: run.SCPID,
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
	return nil
}

// runPhaseB executes Phase B for a run whose stage is image or tts.
// On success: advances the run to StageBatchReview/StatusWaiting.
// On Phase B error: rolls back to run.Stage/StatusFailed (preserving existing
// meta fields so retry context survives the failure).
// Callers are responsible for promoting settings and loading segments before
// calling this method.
func (e *Engine) runPhaseB(ctx context.Context, runID string, run *domain.Run, segments []*domain.Episode) error {
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
	// Stage boundary — promote queued settings so metadata entry runs
	// against the newly-approved config.
	e.promoteSettingsAtBoundary(ctx, runID, run.Stage, nextStage)
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
	return nil
}

// Resume re-enters the failed (or waiting) stage of a run with default
// options. Returns the observed consistency report (empty on success,
// populated when Force bypassed mismatches).
func (e *Engine) Resume(ctx context.Context, runID string) (*domain.InconsistencyReport, error) {
	return e.ResumeWithOptions(ctx, runID, ResumeOptions{})
}

// ResumeWithOptions is the full Resume orchestration. The order of steps
// is load-bearing for correctness — see story 2.3 AC-ENGINE-RESUME:
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
// Returns the InconsistencyReport for caller surfacing (CLI warnings,
// API envelope), and any terminal error.
func (e *Engine) ResumeWithOptions(ctx context.Context, runID string, opts ResumeOptions) (*domain.InconsistencyReport, error) {
	run, err := e.runs.Get(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("resume %s: %w", runID, err)
	}

	if !run.Stage.IsValid() {
		return nil, fmt.Errorf("resume %s: %w: invalid stage %q", runID, domain.ErrValidation, run.Stage)
	}
	if err := validateResumable(run); err != nil {
		return nil, fmt.Errorf("resume %s: %w", runID, err)
	}

	segments, err := e.segments.ListByRunID(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("resume %s: %w", runID, err)
	}

	runDir := filepath.Join(e.outputDir, runID)

	report, err := CheckConsistency(runDir, run, segments)
	if err != nil {
		return nil, fmt.Errorf("resume %s: consistency check: %w", runID, err)
	}
	if len(report.Mismatches) > 0 {
		for _, m := range report.Mismatches {
			e.logger.Warn("fs/db mismatch",
				"run_id", runID, "stage", run.Stage,
				"kind", m.Kind, "path", m.Path, "detail", m.Detail)
		}
		if !opts.Force {
			return report, fmt.Errorf("resume %s: %w: %s", runID, domain.ErrValidation, report.Error())
		}
	}

	preserveSibling := phaseBPreservesSibling(run.Stage, segments)
	if isPhaseB(run.Stage) {
		if preserveSibling {
			if err := cleanFailedPhaseBTrack(runDir, run.Stage); err != nil {
				return report, fmt.Errorf("resume %s: clean failed phase b track: %w", runID, err)
			}
		} else {
			if err := CleanStageArtifacts(runDir, domain.StageImage); err != nil {
				return report, fmt.Errorf("resume %s: clean images: %w", runID, err)
			}
			if err := CleanStageArtifacts(runDir, domain.StageTTS); err != nil {
				return report, fmt.Errorf("resume %s: clean tts: %w", runID, err)
			}
		}
	} else {
		if err := CleanStageArtifacts(runDir, run.Stage); err != nil {
			return report, fmt.Errorf("resume %s: clean artifacts: %w", runID, err)
		}
	}

	if isPhaseB(run.Stage) {
		if preserveSibling {
			if err := clearFailedPhaseBTrack(ctx, e.segments, runID, run.Stage); err != nil {
				return report, fmt.Errorf("resume %s: clear failed phase b track state: %w", runID, err)
			}
		} else {
			n, err := e.segments.DeleteByRunID(ctx, runID)
			if err != nil {
				return report, fmt.Errorf("resume %s: delete segments: %w", runID, err)
			}
			e.logger.Info("segments deleted for phase b resume",
				"run_id", runID, "count", n)
		}
	} else if run.Stage == domain.StageAssemble {
		// clips/ is gone on disk; null the DB references so subsequent
		// consistency checks don't flag every segment forever.
		n, err := e.segments.ClearClipPathsByRunID(ctx, runID)
		if err != nil {
			return report, fmt.Errorf("resume %s: clear clip_paths: %w", runID, err)
		}
		e.logger.Info("clip_paths cleared for assemble resume",
			"run_id", runID, "count", n)
	}

	newStatus := StatusForStage(run.Stage)
	if err := e.runs.ResetForResume(ctx, runID, newStatus); err != nil {
		return report, fmt.Errorf("resume %s: reset: %w", runID, err)
	}

	// Resume is a fresh stage entry: promote queued settings before the
	// stage executes so the retried stage sees the latest approved config.
	e.promoteSettingsAtBoundary(ctx, runID, run.Stage, run.Stage)

	if isPhaseB(run.Stage) && e.phaseB != nil {
		if err := e.runPhaseB(ctx, runID, run, segments); err != nil {
			return report, fmt.Errorf("resume %s: %w", runID, err)
		}
	}

	// Execute StageAssemble if Phase C runner is configured.
	if run.Stage == domain.StageAssemble && e.phaseC != nil {
		if err := e.runPhaseC(ctx, runID, run, segments); err != nil {
			return report, fmt.Errorf("resume %s: %w", runID, err)
		}
	}

	// Re-run metadata entry when resuming from StageMetadataAck (artifacts were
	// cleaned by CleanStageArtifacts and must be regenerated before HITL review).
	if run.Stage == domain.StageMetadataAck && e.phaseCMetadata != nil {
		if err := PhaseCMetadataEntry(ctx, e.phaseCMetadata, runID); err != nil {
			return report, fmt.Errorf("resume %s: phase c metadata entry: %w", runID, err)
		}
	}

	// Story 2.6 cleanup: drop the hitl_sessions row when the run exits HITL
	// state (new status != waiting). If still waiting (e.g. retry within the
	// same HITL stage), the session row stays and is updated by the next
	// decision event.
	if e.sessions != nil && newStatus != domain.StatusWaiting {
		if err := e.sessions.DeleteSession(ctx, runID); err != nil {
			// Non-fatal: log and continue. Invariant repair happens on next
			// Cancel or new decision-capture upsert.
			e.logger.Warn("resume: delete hitl_session failed",
				"run_id", runID, "error", err.Error())
		}
	}

	e.logger.Info("run resumed",
		"run_id", runID, "stage", run.Stage, "status", newStatus,
		"force", opts.Force, "warnings", len(report.Mismatches))
	return report, nil
}

// validateResumable returns ErrConflict when the run cannot be resumed.
// Resumable states are: status ∈ {failed, waiting} AND stage != complete.
// Everything else is a conflict. Stage validity is checked separately by
// the caller (IsValid) before this function is invoked.
func validateResumable(run *domain.Run) error {
	if run.Stage == domain.StageComplete {
		return fmt.Errorf("%w: run already at complete stage", domain.ErrConflict)
	}
	switch run.Status {
	case domain.StatusFailed, domain.StatusWaiting:
		return nil
	case domain.StatusPending:
		return fmt.Errorf("%w: run has not started; use create/advance to begin", domain.ErrConflict)
	case domain.StatusRunning:
		return fmt.Errorf("%w: run already in progress", domain.ErrConflict)
	case domain.StatusCompleted:
		return fmt.Errorf("%w: run already completed", domain.ErrConflict)
	case domain.StatusCancelled:
		return fmt.Errorf("%w: run was cancelled", domain.ErrConflict)
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
