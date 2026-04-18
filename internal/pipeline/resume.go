package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// RunStore is the minimal persistence surface the Engine needs.
// Declared here (not in service/) to keep pipeline/ independent of service/.
// *db.RunStore satisfies this interface structurally.
type RunStore interface {
	Get(ctx context.Context, id string) (*domain.Run, error)
	ResetForResume(ctx context.Context, id string, status domain.Status) error
}

// SegmentStore is the minimal persistence surface the Engine needs for
// Phase B clean-slate semantics. *db.SegmentStore satisfies it structurally.
type SegmentStore interface {
	ListByRunID(ctx context.Context, runID string) ([]*domain.Episode, error)
	DeleteByRunID(ctx context.Context, runID string) (int64, error)
	ClearClipPathsByRunID(ctx context.Context, runID string) (int64, error)
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

// Engine orchestrates pipeline lifecycle operations. In V1 it implements
// Resume only; Advance remains a stub (automated stage execution lands in
// Epic 3). The Engine satisfies pipeline.Runner.
type Engine struct {
	runs      RunStore
	segments  SegmentStore
	sessions  HITLSessionCleaner
	clock     clock.Clock
	outputDir string
	logger    *slog.Logger
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

// Advance is not implemented in V1; automated stage execution lands in
// Epic 3's agent-chain implementation. Returns a descriptive error.
func (e *Engine) Advance(ctx context.Context, runID string) error {
	return fmt.Errorf("advance not implemented: epic 3 scope")
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
//  6. Clean partial artifacts scoped to the failed stage. Phase B failure
//     cleans BOTH tracks (image AND tts) because segments are wiped
//     wholesale and the two tracks share the same "phase" boundary.
//  7. If Phase B: DELETE all segments for the run.
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

	if isPhaseB(run.Stage) {
		// Phase B = one phase with two tracks. Failure in either clears both.
		if err := CleanStageArtifacts(runDir, domain.StageImage); err != nil {
			return report, fmt.Errorf("resume %s: clean images: %w", runID, err)
		}
		if err := CleanStageArtifacts(runDir, domain.StageTTS); err != nil {
			return report, fmt.Errorf("resume %s: clean tts: %w", runID, err)
		}
	} else {
		if err := CleanStageArtifacts(runDir, run.Stage); err != nil {
			return report, fmt.Errorf("resume %s: clean artifacts: %w", runID, err)
		}
	}

	if isPhaseB(run.Stage) {
		n, err := e.segments.DeleteByRunID(ctx, runID)
		if err != nil {
			return report, fmt.Errorf("resume %s: delete segments: %w", runID, err)
		}
		e.logger.Info("segments deleted for phase b resume",
			"run_id", runID, "count", n)
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
