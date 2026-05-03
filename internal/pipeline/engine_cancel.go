package pipeline

import (
	"context"
	"time"
)

// cancelTimeout caps how long Engine.Cancel waits for in-flight workers to
// release. Short relative to rewindCancelTimeout (30s) because Cancel is
// operator-initiated and demands fast feedback; any worker that overshoots
// is contained by PrepareResume's stage cleanup when the operator clicks
// Restart, which deletes the failed-stage artifacts before re-dispatch.
const cancelTimeout = 10 * time.Second

// Cancel drains in-flight workers for runID via the cancel registry and
// blocks until each releases its slot or cancelTimeout fires. The DB
// status='cancelled' mark is owned by the caller (RunService.Cancel) and
// performed BEFORE invoking this so the operator sees immediate state-change
// feedback while the registry drain runs after.
//
// Best-effort by design: the only failure mode the registry surfaces is
// ErrCancelTimeout (worker did not release in time). Logged at warn and
// swallowed — Resume's stage cleanup overwrites any late worker write — so
// the caller never has to translate a drain timeout into a user-facing
// cancel failure. Returns nil when no workers are registered or no registry
// is wired.
func (e *Engine) Cancel(ctx context.Context, runID string) error {
	if e == nil || e.cancelRegistry == nil {
		return nil
	}
	if err := e.cancelRegistry.CancelAndWait(runID, cancelTimeout); err != nil {
		e.logger.Warn("cancel: worker drain incomplete, proceeding",
			"run_id", runID,
			"timeout", cancelTimeout,
			"err", err)
	}
	return nil
}
