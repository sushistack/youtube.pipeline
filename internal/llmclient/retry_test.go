package llmclient_test

import (
	"context"
	"errors"
	"fmt"
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
	}

	done := make(chan error, 1)
	go func() {
		done <- llmclient.WithRetry(context.Background(), clk, 5, fn, onRetry)
	}()

	// Drive the FakeClock until WithRetry finishes. Each Sleep will be
	// satisfied by advancing clock past its deadline.
	deadline := time.Now().Add(2 * time.Second)
	for {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("WithRetry returned error: %v", err)
			}
			mu.Lock()
			defer mu.Unlock()
			testutil.AssertEqual(t, fnCalls, 3)
			testutil.AssertEqual(t, len(retryLog), 2)
			testutil.AssertEqual(t, retryLog[0], "rate_limit")
			testutil.AssertEqual(t, retryLog[1], "rate_limit")
			// FakeClock advanced ≥ first two backoffs (1s + 2s before jitter).
			if clk.Now().Sub(start) < 3*time.Second {
				t.Errorf("FakeClock elapsed %v, expected ≥3s", clk.Now().Sub(start))
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
		done <- llmclient.WithRetry(ctx, clk, 3, func() error {
			return domain.ErrRateLimited
		}, nil)
	}()

	// Let the first fn call complete and enter Sleep, then cancel.
	time.Sleep(20 * time.Millisecond)
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

	done := make(chan error, 1)
	go func() {
		done <- llmclient.WithRetry(context.Background(), clk, 2, func() error {
			return domain.ErrRateLimited
		}, nil)
	}()

	// Drive the clock until done.
	deadline := time.Now().Add(2 * time.Second)
	for {
		select {
		case err := <-done:
			if !errors.Is(err, domain.ErrRateLimited) {
				t.Fatalf("expected errors.Is ErrRateLimited, got %v", err)
			}
			if err == nil || !contains(err.Error(), "max retries (2) exceeded") {
				t.Fatalf("expected max-retries message, got %v", err)
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

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
