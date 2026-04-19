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

const defaultMaxBackoffDelay = 30 * time.Second

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

// BackoffPolicy defines the retry loop behavior. Tests can inject Jitter to
// make delays fully deterministic under clock.FakeClock.
type BackoffPolicy struct {
	MaxRetries int
	MaxDelay   time.Duration
	Jitter     func(attempt int) time.Duration
}

// DefaultBackoffPolicy preserves the production retry behavior while allowing
// tests to override specific fields without replacing the retry helper.
func DefaultBackoffPolicy() BackoffPolicy {
	return BackoffPolicy{
		MaxRetries: 0,
		MaxDelay:   defaultMaxBackoffDelay,
		Jitter: func(int) time.Duration {
			return defaultJitter()
		},
	}
}

var (
	jitterSource   = rand.New(rand.NewSource(time.Now().UnixNano()))
	jitterSourceMu sync.Mutex
)

func defaultJitter() time.Duration {
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
	policy := DefaultBackoffPolicy()
	policy.MaxRetries = maxRetries
	return WithRetryPolicy(ctx, clk, policy, fn, onRetry)
}

// WithRetryPolicy executes fn using the supplied policy while preserving the
// canonical RetryReasonFor taxonomy and the clock.Clock sleep surface.
func WithRetryPolicy(
	ctx context.Context,
	clk clock.Clock,
	policy BackoffPolicy,
	fn func() error,
	onRetry func(attempt int, reason string),
) error {
	var lastErr error
	for attempt := 0; attempt <= policy.MaxRetries; attempt++ {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		reason := RetryReasonFor(lastErr)
		if reason == "" {
			return lastErr
		}
		if attempt == policy.MaxRetries {
			break
		}
		if onRetry != nil {
			onRetry(attempt+1, reason)
		}
		delay := backoffDelay(attempt, policy)
		if err := clk.Sleep(ctx, delay); err != nil {
			return err
		}
	}
	if policy.MaxRetries == 0 {
		return fmt.Errorf("retries disabled, surfacing first failure: %w", lastErr)
	}
	return fmt.Errorf("max retries (%d) exceeded: %w", policy.MaxRetries, lastErr)
}

// backoffDelay returns the exponential-with-jitter delay for a given attempt.
// Attempt 0 → ~1s; attempt 1 → ~2s; capped at 30s.
func backoffDelay(attempt int, policy BackoffPolicy) time.Duration {
	maxDelay := policy.MaxDelay
	if maxDelay <= 0 {
		maxDelay = defaultMaxBackoffDelay
	}
	// Cap the exponent: time.Second = 1e9 ns, so 1<<attempt overflows int64
	// when multiplied by time.Second at attempt ≥ 34. Clamp before the shift
	// to keep the cap predictable even under pathological MaxRetries values.
	exp := attempt
	if exp > 30 {
		exp = 30
	}
	base := time.Duration(1<<exp) * time.Second
	if base > maxDelay {
		base = maxDelay
	}
	jitter := time.Duration(0)
	if policy.Jitter != nil {
		jitter = policy.Jitter(attempt)
	}
	delay := base + jitter
	if delay > maxDelay {
		delay = maxDelay
	}
	if delay < 0 {
		return 0
	}
	return delay
}
