package llmclient

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// RetryReasonFor maps a wrapped domain error to the canonical retry_reason
// string written to runs.retry_reason. Returns "" for non-retryable errors
// (including ErrCostCapExceeded, ErrValidation, ErrConflict, ErrNotFound, and
// unclassified errors).
//
// Truth table:
//
//	ErrRateLimited     → "rate_limit"
//	ErrUpstreamTimeout → "timeout"
//	ErrStageFailed     → "stage_failed"  (operator-resumable; auto-retries cap out)
//	everything else    → ""              (caller must NOT auto-retry)
func RetryReasonFor(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, domain.ErrCostCapExceeded):
		// Explicit: cost cap is non-retryable even though it looks "retryable"
		// in error shape. Must short-circuit WithRetry's loop.
		return ""
	case errors.Is(err, domain.ErrRateLimited):
		return "rate_limit"
	case errors.Is(err, domain.ErrUpstreamTimeout):
		return "timeout"
	case errors.Is(err, domain.ErrStageFailed):
		return "stage_failed"
	default:
		return ""
	}
}

// jitterSource is a package-level PRNG for backoff jitter. Seeded once at
// init; the clock interface doesn't currently expose a Rand surface — see
// deferred-work.md for the follow-up to thread jitter through the clock.
var (
	jitterSource   = rand.New(rand.NewSource(time.Now().UnixNano()))
	jitterSourceMu sync.Mutex
)

func jitter() time.Duration {
	jitterSourceMu.Lock()
	defer jitterSourceMu.Unlock()
	return time.Duration(jitterSource.Int63n(int64(time.Second / 2)))
}

// WithRetry executes fn, retrying on retryable domain errors with exponential
// backoff (1s, 2s, 4s, ..., capped at 30s) plus jitter. clk.Sleep drives the
// delay so tests can advance a FakeClock.
//
// onRetry is invoked BEFORE each sleep with (attempt, reason). Callers use it
// to record observability (e.g. via Recorder.RecordRetry). onRetry may be nil.
//
// Non-retryable errors (ErrCostCapExceeded, ErrValidation, ErrConflict,
// ErrNotFound, unclassified) surface immediately without sleeping. Context
// cancellation during Sleep propagates as ctx.Err().
func WithRetry(
	ctx context.Context,
	clk clock.Clock,
	maxRetries int,
	fn func() error,
	onRetry func(attempt int, reason string),
) error {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		reason := RetryReasonFor(lastErr)
		if reason == "" {
			return lastErr
		}
		if attempt == maxRetries {
			break
		}
		if onRetry != nil {
			onRetry(attempt+1, reason)
		}
		delay := backoffDelay(attempt)
		if err := clk.Sleep(ctx, delay); err != nil {
			return err
		}
	}
	return fmt.Errorf("max retries (%d) exceeded: %w", maxRetries, lastErr)
}

// backoffDelay returns the exponential-with-jitter delay for a given attempt.
// Attempt 0 → ~1s; attempt 1 → ~2s; capped at 30s.
func backoffDelay(attempt int) time.Duration {
	base := time.Duration(1<<attempt) * time.Second
	if base > 30*time.Second {
		base = 30 * time.Second
	}
	return base + jitter()
}
