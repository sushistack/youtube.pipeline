package llmclient_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/llmclient"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestRetryReasonFor_RateLimited(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	testutil.AssertEqual(t, llmclient.RetryReasonFor(domain.ErrRateLimited), "rate_limit")
	// Wrapped too.
	wrapped := fmt.Errorf("dashscope: %w", domain.ErrRateLimited)
	testutil.AssertEqual(t, llmclient.RetryReasonFor(wrapped), "rate_limit")
}

func TestRetryReasonFor_UpstreamTimeout(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	testutil.AssertEqual(t, llmclient.RetryReasonFor(domain.ErrUpstreamTimeout), "timeout")
}

func TestRetryReasonFor_StageFailed(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	testutil.AssertEqual(t, llmclient.RetryReasonFor(domain.ErrStageFailed), "stage_failed")
}

func TestRetryReasonFor_CostCapExceeded(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	testutil.AssertEqual(t, llmclient.RetryReasonFor(domain.ErrCostCapExceeded), "")
}

func TestRetryReasonFor_NonRetryables(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	cases := []error{
		domain.ErrValidation,
		domain.ErrConflict,
		domain.ErrNotFound,
		errors.New("unclassified"),
		nil,
	}
	for _, err := range cases {
		testutil.AssertEqual(t, llmclient.RetryReasonFor(err), "")
	}
}

func TestWithRetry_SucceedsOnFirstAttempt(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	clk := clock.NewFakeClock(time.Now())
	called := 0
	err := llmclient.WithRetry(context.Background(), clk, 3, func() error {
		called++
		return nil
	}, func(int, string) { t.Fatal("onRetry should not be called") })
	if err != nil {
		t.Fatalf("WithRetry: %v", err)
	}
	testutil.AssertEqual(t, called, 1)
}

func TestWithRetry_RetriesOn429(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	clk := clock.NewFakeClock(time.Unix(0, 0))
	start := clk.Now()

	var mu sync.Mutex
	var retryLog []string
	retrySignals := make(chan struct{}, 2)
	fnCalls := 0
	fn := func() error {
		mu.Lock()
		fnCalls++
		n := fnCalls
		mu.Unlock()
		if n < 3 {
			return fmt.Errorf("dashscope call: %w", domain.ErrRateLimited)
		}
		return nil
	}
	onRetry := func(_ int, reason string) {
		mu.Lock()
		retryLog = append(retryLog, reason)
		mu.Unlock()
		retrySignals <- struct{}{}
	}

	done := make(chan error, 1)
	go func() {
		done <- llmclient.WithRetryPolicy(context.Background(), clk, llmclient.BackoffPolicy{
			MaxRetries: 5,
			MaxDelay:   30 * time.Second,
			Jitter:     func(int) time.Duration { return 0 },
		}, fn, onRetry)
	}()

	waitForSignal(t, retrySignals, 50)
	clk.Advance(1 * time.Second)
	waitForSignal(t, retrySignals, 50)
	clk.Advance(2 * time.Second)
	err := waitForRetryDone(t, done, 50)
	if err != nil {
		t.Fatalf("WithRetry returned error: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	testutil.AssertEqual(t, fnCalls, 3)
	testutil.AssertEqual(t, len(retryLog), 2)
	testutil.AssertEqual(t, retryLog[0], "rate_limit")
	testutil.AssertEqual(t, retryLog[1], "rate_limit")
	testutil.AssertEqual(t, clk.Now().Sub(start), 3*time.Second)
}

func TestWithRetry_NonRetryableSurfacesImmediately(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	clk := clock.NewFakeClock(time.Now())
	called := 0
	err := llmclient.WithRetry(context.Background(), clk, 5, func() error {
		called++
		return domain.ErrValidation
	}, func(int, string) { t.Fatal("onRetry must not fire for ErrValidation") })
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	testutil.AssertEqual(t, called, 1)
}

func TestWithRetry_CostCapBypassesRetries(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	clk := clock.NewFakeClock(time.Now())
	called := 0
	err := llmclient.WithRetry(context.Background(), clk, 5, func() error {
		called++
		return domain.ErrCostCapExceeded
	}, func(int, string) { t.Fatal("onRetry must not fire for ErrCostCapExceeded") })
	if !errors.Is(err, domain.ErrCostCapExceeded) {
		t.Fatalf("expected ErrCostCapExceeded, got %v", err)
	}
	testutil.AssertEqual(t, called, 1)
}

func TestWithRetry_ContextCanceledDuringSleep(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	clk := clock.NewFakeClock(time.Now())
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- llmclient.WithRetryPolicy(ctx, clk, llmclient.BackoffPolicy{
			MaxRetries: 3,
			MaxDelay:   30 * time.Second,
			Jitter:     func(int) time.Duration { return 0 },
		}, func() error {
			return domain.ErrRateLimited
		}, nil)
	}()

	// Wait for the retry loop to register a Sleep waiter on the FakeClock,
	// then cancel. Deterministic — no real wall-clock sleep.
	waitForRetrySleeper(clk, 50)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("WithRetry did not return after context cancel")
	}
}

func TestWithRetry_MaxRetriesExceeded(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	clk := clock.NewFakeClock(time.Now())
	retrySignals := make(chan struct{}, 2)

	done := make(chan error, 1)
	go func() {
		done <- llmclient.WithRetryPolicy(context.Background(), clk, llmclient.BackoffPolicy{
			MaxRetries: 2,
			MaxDelay:   30 * time.Second,
			Jitter:     func(int) time.Duration { return 0 },
		}, func() error {
			return domain.ErrRateLimited
		}, func(int, string) { retrySignals <- struct{}{} })
	}()

	waitForSignal(t, retrySignals, 50)
	clk.Advance(1 * time.Second)
	waitForSignal(t, retrySignals, 50)
	clk.Advance(2 * time.Second)
	err := waitForRetryDone(t, done, 50)
	if !errors.Is(err, domain.ErrRateLimited) {
		t.Fatalf("expected errors.Is ErrRateLimited, got %v", err)
	}
	if err == nil || !contains(err.Error(), "max retries (2) exceeded") {
		t.Fatalf("expected max-retries message, got %v", err)
	}
}

func TestWithRetry_BackoffSequence_DeterministicFakeClock(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	clk := clock.NewFakeClock(time.Unix(0, 0))
	start := clk.Now()
	retrySignals := make(chan struct{}, 3)

	done := make(chan error, 1)
	go func() {
		done <- llmclient.WithRetryPolicy(context.Background(), clk, llmclient.BackoffPolicy{
			MaxRetries: 3,
			MaxDelay:   30 * time.Second,
			Jitter:     func(int) time.Duration { return 0 },
		}, func() error {
			return domain.ErrRateLimited
		}, func(int, string) { retrySignals <- struct{}{} })
	}()

	waitForSignal(t, retrySignals, 50)
	clk.Advance(1 * time.Second)
	waitForSignal(t, retrySignals, 50)
	clk.Advance(2 * time.Second)
	waitForSignal(t, retrySignals, 50)
	clk.Advance(4 * time.Second)
	err := waitForRetryDone(t, done, 50)
	if !errors.Is(err, domain.ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
	testutil.AssertEqual(t, clk.Now().Sub(start), 7*time.Second)
}

func TestWithRetry_JitterInjection_Deterministic(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	clk := clock.NewFakeClock(time.Unix(0, 0))
	start := clk.Now()
	retrySignals := make(chan struct{}, 2)

	done := make(chan error, 1)
	go func() {
		done <- llmclient.WithRetryPolicy(context.Background(), clk, llmclient.BackoffPolicy{
			MaxRetries: 2,
			MaxDelay:   30 * time.Second,
			Jitter: func(attempt int) time.Duration {
				return time.Duration(attempt+1) * 100 * time.Millisecond
			},
		}, func() error {
			return domain.ErrRateLimited
		}, func(int, string) { retrySignals <- struct{}{} })
	}()

	waitForSignal(t, retrySignals, 50)
	clk.Advance(1100 * time.Millisecond)
	waitForSignal(t, retrySignals, 50)
	clk.Advance(2200 * time.Millisecond)
	err := waitForRetryDone(t, done, 50)
	if !errors.Is(err, domain.ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
	testutil.AssertEqual(t, clk.Now().Sub(start), 3300*time.Millisecond)
}

func TestWithRetry_MaxDelayCappedAt30Seconds(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	clk := clock.NewFakeClock(time.Unix(0, 0))
	start := clk.Now()
	retrySignals := make(chan struct{}, 6)

	done := make(chan error, 1)
	go func() {
		done <- llmclient.WithRetryPolicy(context.Background(), clk, llmclient.BackoffPolicy{
			MaxRetries: 6,
			MaxDelay:   30 * time.Second,
			Jitter:     func(int) time.Duration { return 2 * time.Second },
		}, func() error {
			return domain.ErrRateLimited
		}, func(int, string) { retrySignals <- struct{}{} })
	}()

	for _, delay := range []time.Duration{
		3 * time.Second,
		4 * time.Second,
		6 * time.Second,
		10 * time.Second,
		18 * time.Second,
		30 * time.Second,
	} {
		waitForSignal(t, retrySignals, 50)
		clk.Advance(delay)
	}
	err := waitForRetryDone(t, done, 50)
	if !errors.Is(err, domain.ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
	testutil.AssertEqual(t, clk.Now().Sub(start), 71*time.Second)
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// retryTestTimeout bounds busy-spin helpers so CI failures surface as a clear
// t.Fatal instead of a hung goroutine. 2s is generous for local + race CI.
const retryTestTimeout = 2 * time.Second

func waitForRetrySleeper(clk *clock.FakeClock, spins int) {
	deadline := time.Now().Add(retryTestTimeout)
	for time.Now().Before(deadline) {
		if clk.PendingSleepers() > 0 {
			return
		}
		runtime.Gosched()
	}
}

func waitForSignal(t *testing.T, ch <-chan struct{}, spins int) {
	t.Helper()
	select {
	case <-ch:
		return
	case <-time.After(retryTestTimeout):
		t.Fatal("expected signal was not observed")
	}
}

func waitForRetryDone(t *testing.T, done <-chan error, spins int) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(retryTestTimeout):
		t.Fatal("retry operation did not complete")
		return nil
	}
}
