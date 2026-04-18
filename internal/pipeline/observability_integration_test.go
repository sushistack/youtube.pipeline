package pipeline_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/llmclient"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// TestIntegration_429Backoff_DoesNotAdvanceStage is the NFR-P3 pin:
// a 429 response triggers retry + observability recording but never advances
// runs.stage and never sets runs.status to failed.
//
// The invariant is enforced by interface segregation: Recorder consumes
// ObservationStore, which only declares RecordStageObservation. SetStatus is
// not on that interface, so the retry path CANNOT advance status at compile
// time. The assertions below pin the observable post-state: stage and status
// are unchanged, retry metadata is recorded.
func TestIntegration_429Backoff_DoesNotAdvanceStage(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.LoadRunStateFixture(t, "running_at_write")

	store := db.NewRunStore(database)
	acc := pipeline.NewCostAccumulator(nil, 0)
	logger, _ := testutil.CaptureLog(t)
	rec := pipeline.NewRecorder(store, acc, clock.RealClock{}, logger)

	clk := clock.NewFakeClock(time.Unix(0, 0))

	var mu sync.Mutex
	fnCalls := 0
	fn := func() error {
		mu.Lock()
		fnCalls++
		n := fnCalls
		mu.Unlock()
		if n < 3 {
			return fmt.Errorf("dashscope 429: %w", domain.ErrRateLimited)
		}
		return nil
	}

	onRetry := func(_ int, reason string) {
		if err := rec.RecordRetry(context.Background(), "scp-049-run-1", domain.StageWrite, reason); err != nil {
			t.Errorf("RecordRetry: %v", err)
		}
	}

	done := make(chan error, 1)
	go func() {
		done <- llmclient.WithRetry(context.Background(), clk, 5, fn, onRetry)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("WithRetry: %v", err)
			}
			// NFR-P3: stage and status must be unchanged after the retry flow.
			// If the retry path had advanced status to failed, these asserts
			// would fail. Retry metadata must be recorded.
			updated, err := store.Get(context.Background(), "scp-049-run-1")
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			testutil.AssertEqual(t, updated.Stage, domain.StageWrite)
			testutil.AssertEqual(t, updated.Status, domain.StatusRunning)
			testutil.AssertEqual(t, updated.RetryCount, 2)
			if updated.RetryReason == nil || *updated.RetryReason != "rate_limit" {
				t.Errorf("RetryReason: got %v want rate_limit", updated.RetryReason)
			}
			return
		default:
			if time.Now().After(deadline) {
				t.Fatal("WithRetry did not finish within deadline")
			}
			clk.Advance(1 * time.Second)
			time.Sleep(1 * time.Millisecond)
		}
	}
}

// TestIntegration_NonRetryableError_Bypasses429Path confirms non-retryable
// errors (e.g. ErrCostCapExceeded) surface immediately without the retry
// path touching observability.
func TestIntegration_NonRetryableError_Bypasses429Path(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.LoadRunStateFixture(t, "running_at_write")
	store := db.NewRunStore(database)

	clk := clock.NewFakeClock(time.Now())
	called := 0
	retryCalls := 0
	err := llmclient.WithRetry(context.Background(), clk, 5, func() error {
		called++
		return fmt.Errorf("budget: %w", domain.ErrCostCapExceeded)
	}, func(int, string) { retryCalls++ })

	if !errors.Is(err, domain.ErrCostCapExceeded) {
		t.Fatalf("expected ErrCostCapExceeded, got %v", err)
	}
	testutil.AssertEqual(t, called, 1)
	testutil.AssertEqual(t, retryCalls, 0)

	// Original run state untouched.
	run, _ := store.Get(context.Background(), "scp-049-run-1")
	testutil.AssertEqual(t, run.RetryCount, 0)
	if run.RetryReason != nil {
		t.Errorf("RetryReason expected nil, got %v", *run.RetryReason)
	}
}
