package agents

import (
	"context"
	"errors"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// TestRunWithRetry_NegativeBudget_DoesNotSilentlySucceed locks down the
// defensive guard: a negative budget must surface an explicit error
// rather than the silent zero-value success the legacy
// `for attempt := 0; attempt <= -1; attempt++` shape used to produce.
func TestRunWithRetry_NegativeBudget_DoesNotSilentlySucceed(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	calls := 0
	got, err := runWithRetry(context.Background(), retryOpts{
		Stage:  "writer",
		Budget: -1,
	}, func(int) (string, retryReason, error) {
		calls++
		return "ok", "", nil
	})
	if err == nil {
		t.Fatal("expected error for negative budget, got nil")
	}
	if got != "" {
		t.Fatalf("expected zero-value result on negative budget, got %q", got)
	}
	if calls != 0 {
		t.Fatalf("fn was invoked %d time(s) with negative budget, want 0", calls)
	}
	if err.Error() != "retry budget invalid: -1" {
		t.Fatalf("error = %q, want %q", err.Error(), "retry budget invalid: -1")
	}
}

// TestWriter_Run_NegativeBudget_DoesNotSilentlySucceed mirrors the helper
// guarantee at the writer-stage seam. It exercises runWithRetry with a
// budget of -1 (the same code path the writer's retry loop uses).
func TestWriter_Run_NegativeBudget_DoesNotSilentlySucceed(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	got, err := runWithRetry(context.Background(), retryOpts{
		Stage:  "writer",
		Budget: -1,
	}, func(int) (int, retryReason, error) {
		return 1, "", nil
	})
	if err == nil {
		t.Fatal("expected error for writer-stage negative budget, got nil")
	}
	if got != 0 {
		t.Fatalf("expected zero-value int on negative budget, got %d", got)
	}
}

// TestVisualBreakdowner_Run_NegativeBudget_DoesNotSilentlySucceed mirrors
// the same guarantee at the visual_breakdowner-stage seam.
func TestVisualBreakdowner_Run_NegativeBudget_DoesNotSilentlySucceed(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	type vbResult struct{ shots int }
	got, err := runWithRetry(context.Background(), retryOpts{
		Stage:  "visual_breakdowner",
		Budget: -2,
	}, func(int) (vbResult, retryReason, error) {
		return vbResult{shots: 5}, "", nil
	})
	if err == nil {
		t.Fatal("expected error for visual_breakdowner-stage negative budget, got nil")
	}
	if got.shots != 0 {
		t.Fatalf("expected zero-value vbResult on negative budget, got %+v", got)
	}
}

// TestRunWithRetry_AbortShortCircuits proves the abort retryReason
// surface — used by transport errors and provider truncation — exits the
// loop immediately without consuming further attempts.
func TestRunWithRetry_AbortShortCircuits(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	calls := 0
	transportErr := errors.New("network: connection reset")
	_, err := runWithRetry(context.Background(), retryOpts{
		Stage:  "writer",
		Budget: 5,
	}, func(int) (string, retryReason, error) {
		calls++
		return "", retryReasonAbort, transportErr
	})
	if !errors.Is(err, transportErr) {
		t.Fatalf("expected abort to surface transport error verbatim, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("abort path took %d calls, want 1 (no retry)", calls)
	}
}

// TestRunWithRetry_RetryThenSuccess proves a retryable failure followed
// by a success classifies the outcome correctly and surfaces the result.
func TestRunWithRetry_RetryThenSuccess(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	calls := 0
	got, err := runWithRetry(context.Background(), retryOpts{
		Stage:  "writer",
		Budget: 1,
	}, func(int) (string, retryReason, error) {
		calls++
		if calls == 1 {
			return "", retryReasonJSONDecode, errors.New("bad json")
		}
		return "ok", "", nil
	})
	if err != nil {
		t.Fatalf("expected success after one retry, got %v", err)
	}
	if got != "ok" {
		t.Fatalf("got %q, want %q", got, "ok")
	}
	if calls != 2 {
		t.Fatalf("calls=%d, want 2", calls)
	}
}
