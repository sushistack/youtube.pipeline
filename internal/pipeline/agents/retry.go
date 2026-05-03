package agents

import (
	"context"
	"fmt"
	"log/slog"
)

// retryReason classifies why an attempt failed in a retryable way. Empty
// string signals success; non-empty values are propagated into slog so a
// scraper can aggregate failure modes per stage.
type retryReason string

const (
	retryReasonJSONDecode       retryReason = "json_decode"
	retryReasonSchemaValidation retryReason = "schema_validation"
	// retryReasonAbort signals "do not retry; surface the error verbatim".
	// Used for transport errors and for hard-fail conditions like provider
	// truncation where re-running the same prompt cannot recover.
	retryReasonAbort retryReason = "abort"
)

// retry outcome classifications used in slog `outcome` field.
const (
	retryOutcomeSuccessFirstTry retryReason = "success_first_try"
	retryOutcomeRetrySucceeded  retryReason = "retry_succeeded"
	retryOutcomeRetryExhausted  retryReason = "retry_exhausted"
)

// retryOpts configures the runWithRetry helper. Stage tags emitted slog
// events so a single scraper can split writer / visual_breakdowner counts.
// Logger is nil-safe — the helper guards every emission.
type retryOpts struct {
	Stage     string
	Budget    int
	Logger    *slog.Logger
	BaseAttrs []slog.Attr
}

// runWithRetry runs fn up to (Budget+1) times. fn returns:
//   - (result, "", nil)          — success; helper returns immediately.
//   - (_, retryReason, err)       — retryable failure; helper logs and loops.
//   - (_, _, err) where err != nil and retryReason == "" — also retryable
//     (maps to "unknown"); we still loop, but emit the underlying error.
//
// Pre-loop the helper validates Budget >= 0; a negative budget surfaces an
// explicit error rather than the silent zero-value success the
// `for attempt := 0; attempt <= -1; attempt++` shape used to produce.
//
// Transport errors should be propagated directly by the caller (return them
// without a retryReason) by short-circuiting before invoking runWithRetry,
// OR by signaling retryReason == "" + non-nil err — the helper logs a
// retry but the next attempt will hit the same transport error and burn the
// budget. Stages that need the strict "transport error means no retry"
// behavior should test for transport errors before returning to the helper.
func runWithRetry[T any](
	ctx context.Context,
	opts retryOpts,
	fn func(attempt int) (T, retryReason, error),
) (T, error) {
	var zero T
	if opts.Budget < 0 {
		return zero, fmt.Errorf("retry budget invalid: %d", opts.Budget)
	}

	var lastErr error
	maxAttempts := opts.Budget + 1
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		result, reason, err := fn(attempt)
		if err == nil && reason == "" {
			emitRetry(opts, attempt, retrySuccessOutcome(attempt), "", nil)
			return result, nil
		}
		// Abort signals a hard-fail (transport error or non-retryable
		// validation): propagate immediately without consuming further
		// budget. No outcome event — the caller's error path is the
		// observability surface here.
		if reason == retryReasonAbort {
			return zero, err
		}
		lastErr = err
		emitRetryFailure(opts, attempt, reason, err)
	}
	emitRetry(opts, maxAttempts-1, retryOutcomeRetryExhausted, "", lastErr)
	return zero, lastErr
}

func retrySuccessOutcome(attempt int) retryReason {
	if attempt == 0 {
		return retryOutcomeSuccessFirstTry
	}
	return retryOutcomeRetrySucceeded
}

// emitRetryFailure logs a single retry-loop failure attempt at Info level.
// The terminal `retry_exhausted` event is emitted separately by runWithRetry
// at Warn level so log scrapers can split per-attempt churn from
// run-blocking exhaustion.
func emitRetryFailure(opts retryOpts, attempt int, reason retryReason, err error) {
	if opts.Logger == nil {
		return
	}
	attrs := baseAttrs(opts, attempt)
	if reason != "" {
		attrs = append(attrs, slog.String("reason", string(reason)))
	}
	if err != nil {
		attrs = append(attrs, slog.String("error", err.Error()))
	}
	opts.Logger.LogAttrs(context.Background(), slog.LevelInfo, opts.Stage+" retry", attrs...)
}

// emitRetry emits the terminal outcome event. Successful outcomes are Info,
// `retry_exhausted` is Warn (run is failing).
func emitRetry(opts retryOpts, attempt int, outcome retryReason, _ retryReason, err error) {
	if opts.Logger == nil {
		return
	}
	level := slog.LevelInfo
	if outcome == retryOutcomeRetryExhausted {
		level = slog.LevelWarn
	}
	attrs := baseAttrs(opts, attempt)
	attrs = append(attrs, slog.String("outcome", string(outcome)))
	if err != nil {
		attrs = append(attrs, slog.String("error", err.Error()))
	}
	opts.Logger.LogAttrs(context.Background(), level, opts.Stage+" retry outcome", attrs...)
}

func baseAttrs(opts retryOpts, attempt int) []slog.Attr {
	attrs := make([]slog.Attr, 0, len(opts.BaseAttrs)+3)
	attrs = append(attrs, slog.String("stage", opts.Stage))
	attrs = append(attrs, slog.Int("attempt", attempt))
	attrs = append(attrs, opts.BaseAttrs...)
	return attrs
}
