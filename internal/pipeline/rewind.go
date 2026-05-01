package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// CancelRegistryClient is the engine-side surface needed to interleave
// cancellation with rewind. *CancelRegistry satisfies this structurally;
// engine tests can swap a stub.
type CancelRegistryClient interface {
	CancelAndWait(runID string, timeout time.Duration) error
}

// RewindStore is the persistence surface Engine.Rewind requires beyond
// what RunStore (the resume contract) already exposes. *db.RunStore
// satisfies this structurally; tests can swap a stub. The params type is
// shared via the domain package so neither side needs a cross-import.
type RewindStore interface {
	PreRewindCancel(ctx context.Context, runID string) error
	ApplyRewindReset(ctx context.Context, runID string, params domain.RewindResetParams) error
}

// rewindCancelTimeout caps how long Rewind waits for in-flight workers to
// release. 30s covers a Phase B HTTP call that's mid-retry; longer than
// that and the rewind proceeds anyway, accepting that the dying worker's
// final write (if any) will be overwritten by ApplyRewindReset.
const rewindCancelTimeout = 30 * time.Second

// SetCancelRegistry wires the registry the engine will use to interleave
// stage cancellation with rewind. Existing async dispatch sites (Advance,
// ExecuteResume, the Phase A goroutine in Resume) consult the registry
// when present; nil keeps the legacy behavior of running everything on
// context.Background() with no rewind safety net.
func (e *Engine) SetCancelRegistry(reg *CancelRegistry) {
	e.cancelRegistry = reg
}

// SetRewindStore wires the RunStore-shaped rewind primitive so Engine.Rewind
// can issue PreRewindCancel + ApplyRewindReset against the persistence layer.
// Required for Rewind to function; nil disables the entry point with
// ErrValidation.
func (e *Engine) SetRewindStore(store RewindStore) {
	e.rewindStore = store
}

// rewindLocks serializes rewind requests per run ID so a double-click or
// retried POST cannot race two cleanup passes against each other. Cheap:
// in-process map + sync.Mutex.
type rewindLocks struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func newRewindLocks() *rewindLocks {
	return &rewindLocks{locks: map[string]*sync.Mutex{}}
}

func (r *rewindLocks) Acquire(runID string) func() {
	r.mu.Lock()
	lk, ok := r.locks[runID]
	if !ok {
		lk = &sync.Mutex{}
		r.locks[runID] = lk
	}
	r.mu.Unlock()
	lk.Lock()
	return lk.Unlock
}

// Rewind drives the full operator-initiated rewind for runID to the given
// stepper node. Order of operations (see rewind_plan.go header for the
// design rationale):
//
//  1. Validate node + acquire per-run rewind mutex.
//  2. Load the run; refuse if target is not strictly before run.Stage.
//  3. PreRewindCancel — set status='cancelled' so workers stop interfering.
//  4. CancelAndWait — signal in-flight goroutines via cancellation context
//     and block until each releases its registry slot.
//  5. ApplyRewindReset — single transactional batch: delete hitl_sessions,
//     selectively delete decisions by bucket, clear/delete segments per
//     plan, then UPDATE runs (stage, status, columns) atomically.
//  6. applyRewindFS — idempotent on-disk artifact removal scoped to plan.
//  7. If finalStage is HITL+waiting (Cast node), upsert a fresh
//     hitl_sessions row so the UI sees a session anchor on its next poll.
//
// Returns the post-rewind run snapshot. ErrConflict when node is not
// strictly before current stage; ErrValidation when node is not rewindable.
func (e *Engine) Rewind(ctx context.Context, runID string, node StageNodeKey) (*domain.Run, error) {
	if e.rewindStore == nil {
		return nil, fmt.Errorf("rewind %s: %w: rewind store not configured", runID, domain.ErrValidation)
	}
	if !node.IsRewindable() {
		return nil, fmt.Errorf("rewind %s: %w: stage node %q is not rewindable", runID, domain.ErrValidation, node)
	}

	release := e.rewindLocks.Acquire(runID)
	defer release()

	run, err := e.runs.Get(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("rewind %s: %w", runID, err)
	}

	plan, err := PlanRewind(node)
	if err != nil {
		return nil, fmt.Errorf("rewind %s: %w", runID, err)
	}
	if _, err := CanRewind(run.Stage, node); err != nil {
		return nil, fmt.Errorf("rewind %s: %w", runID, err)
	}

	// 1. Park the run in cancelled status so any racing worker UPDATE
	//    is overwritten by step 3 below and FS deletion is safe.
	if err := e.rewindStore.PreRewindCancel(ctx, runID); err != nil {
		return nil, fmt.Errorf("rewind %s: pre-cancel: %w", runID, err)
	}

	// 2. Drain in-flight workers. ErrCancelTimeout is logged but
	//    non-fatal: the subsequent ApplyRewindReset will clobber any
	//    late-arriving worker write.
	if e.cancelRegistry != nil {
		if err := e.cancelRegistry.CancelAndWait(runID, rewindCancelTimeout); err != nil {
			if errors.Is(err, ErrCancelTimeout) {
				e.logger.Warn("rewind: cancel timeout, proceeding with cleanup",
					"run_id", runID, "node", string(node))
			} else {
				return nil, fmt.Errorf("rewind %s: cancel workers: %w", runID, err)
			}
		}
	}

	// 3. DB cleanup + final reset.
	params := domain.RewindResetParams{
		FinalStage:            plan.FinalStage,
		FinalStatus:           plan.FinalStatus,
		DeleteSegments:        plan.DeleteSegments,
		ClearImageArtifacts:   plan.ClearImageArtifacts,
		ClearTTSArtifacts:     plan.ClearTTSArtifacts,
		ClearClipPaths:        plan.ClearClipPaths,
		ClearScenarioPath:     plan.ClearScenarioPath,
		ClearCharacterPick:    plan.ClearCharacterPick,
		ClearOutputPath:       plan.ClearOutputPath,
		ClearCriticScore:      plan.ClearCriticScore,
		DecisionTypesToDelete: plan.DecisionTypesToDelete,
	}
	if err := e.rewindStore.ApplyRewindReset(ctx, runID, params); err != nil {
		return nil, fmt.Errorf("rewind %s: apply reset: %w", runID, err)
	}

	// 4. Filesystem cleanup. Independent of the DB transaction; idempotent
	//    on missing files. A failure here is logged but does not unwind the
	//    DB reset — the run is now logically at the rewound stage; orphaned
	//    files are recoverable on the next Resume's consistency check.
	runDir := filepath.Join(e.outputDir, runID)
	if err := applyRewindFS(runDir, plan); err != nil {
		e.logger.Warn("rewind: filesystem cleanup error",
			"run_id", runID, "error", err.Error())
	}

	// 5. Re-create HITL session row if the final state demands one.
	if e.hitlSessions != nil && IsHITLStage(plan.FinalStage) && plan.FinalStatus == domain.StatusWaiting {
		clk := e.clock
		if clk == nil {
			clk = clock.RealClock{}
		}
		if _, err := UpsertSessionFromState(ctx, e.hitlSessions, clk, runID, plan.FinalStage, plan.FinalStatus); err != nil {
			e.logger.Warn("rewind: hitl session upsert failed",
				"run_id", runID, "error", err.Error())
		}
	}

	// 6. Reload + return the canonical post-rewind snapshot.
	updated, err := e.runs.Get(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("rewind %s: reload: %w", runID, err)
	}
	e.logger.Info("run rewound",
		"run_id", runID, "node", string(node),
		"stage", string(updated.Stage), "status", string(updated.Status))
	return updated, nil
}

// applyRewindFS deletes the on-disk artifacts called for by plan. Each
// removal tolerates ErrNotExist so repeated rewinds (or rewind on a
// freshly-created run with no artifacts) are no-ops.
func applyRewindFS(runDir string, plan RewindPlan) error {
	type op struct {
		want bool
		path string
		dir  bool
	}
	ops := []op{
		{plan.FSRemoveScenario, filepath.Join(runDir, "scenario.json"), false},
		{plan.FSRemoveImages, filepath.Join(runDir, "images"), true},
		{plan.FSRemoveTTS, filepath.Join(runDir, "tts"), true},
		{plan.FSRemoveClips, filepath.Join(runDir, "clips"), true},
		{plan.FSRemoveOutputMP4, filepath.Join(runDir, "output.mp4"), false},
		{plan.FSRemoveMetadata, filepath.Join(runDir, "metadata.json"), false},
		{plan.FSRemoveManifest, filepath.Join(runDir, "manifest.json"), false},
	}
	for _, o := range ops {
		if !o.want {
			continue
		}
		if o.dir {
			if err := os.RemoveAll(o.path); err != nil {
				return fmt.Errorf("remove %s: %w", o.path, err)
			}
			continue
		}
		if err := os.Remove(o.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", o.path, err)
		}
	}
	return nil
}
