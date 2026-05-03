package pipeline_test

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
)

// TestEngineCancel_DrainsInflightWorker verifies Engine.Cancel propagates
// cancellation to a worker registered via the cancel registry and blocks
// until the worker releases. This is the core invariant the operator-facing
// Cancel API depends on: without it, the DB row flips to 'cancelled' but
// the worker keeps running and eventually overwrites the next Resume's
// outputs.
func TestEngineCancel_DrainsInflightWorker(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.DiscardHandler)
	eng := pipeline.NewEngine(nil, nil, nil, clock.RealClock{}, t.TempDir(), logger)
	reg := pipeline.NewCancelRegistry()
	eng.SetCancelRegistry(reg)

	const runID = "scp-test-run-1"
	var workerCancelled int32
	workerStarted := make(chan struct{})
	workerExited := make(chan struct{})
	go func() {
		ctx, _, release := reg.Begin(context.Background(), runID)
		defer release()
		close(workerStarted)
		select {
		case <-ctx.Done():
			atomic.StoreInt32(&workerCancelled, 1)
		case <-time.After(5 * time.Second):
			// Safety net so a regression doesn't hang the suite.
		}
		close(workerExited)
	}()

	<-workerStarted

	if err := eng.Cancel(context.Background(), runID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	// CancelAndWait blocks until release; worker must have exited before
	// Cancel returned. This window is small but non-zero on slow CI — the
	// 1s budget is conservative.
	select {
	case <-workerExited:
	case <-time.After(1 * time.Second):
		t.Fatalf("worker did not exit before Cancel returned")
	}

	if atomic.LoadInt32(&workerCancelled) != 1 {
		t.Fatalf("worker did not observe ctx.Done(); cancel signal lost")
	}
	if reg.ActiveCount(runID) != 0 {
		t.Fatalf("registry still tracks the worker after release")
	}
}

// TestEngineCancel_NoRegistryIsNoop covers the CLI/test path where Engine
// is constructed without a cancel registry. Cancel must return nil so the
// service layer can call it unconditionally.
func TestEngineCancel_NoRegistryIsNoop(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.DiscardHandler)
	eng := pipeline.NewEngine(nil, nil, nil, clock.RealClock{}, t.TempDir(), logger)

	if err := eng.Cancel(context.Background(), "scp-any-run-9"); err != nil {
		t.Fatalf("Cancel without registry should be noop, got %v", err)
	}
}

// TestEngineCancel_NoWorkersIsNoop covers the common case where the run is
// in 'waiting' (HITL) and no goroutines are registered. The registry is
// wired but empty for that runID.
func TestEngineCancel_NoWorkersIsNoop(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.DiscardHandler)
	eng := pipeline.NewEngine(nil, nil, nil, clock.RealClock{}, t.TempDir(), logger)
	eng.SetCancelRegistry(pipeline.NewCancelRegistry())

	if err := eng.Cancel(context.Background(), "scp-any-run-9"); err != nil {
		t.Fatalf("Cancel with empty registry should be noop, got %v", err)
	}
}
