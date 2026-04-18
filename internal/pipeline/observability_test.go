package pipeline_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// fakeObsStore is an inline fake for ObservationStore.
type fakeObsStore struct {
	mu    sync.Mutex
	calls []domain.StageObservation
	err   error
}

func (f *fakeObsStore) RecordStageObservation(ctx context.Context, runID string, obs domain.StageObservation) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, obs)
	return f.err
}

func (f *fakeObsStore) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func TestRecorder_Record_Happy(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	store := &fakeObsStore{}
	acc := pipeline.NewCostAccumulator(nil, 0)
	logger, buf := testutil.CaptureLog(t)
	rec := pipeline.NewRecorder(store, acc, clock.RealClock{}, logger)

	obs := domain.StageObservation{Stage: domain.StageWrite, DurationMs: 100, CostUSD: 0.02}
	if err := rec.Record(context.Background(), "scp-049-run-1", obs); err != nil {
		t.Fatalf("Record: %v", err)
	}
	testutil.AssertEqual(t, store.count(), 1)

	// Exactly one structured log line with all expected keys.
	lines := splitLogLines(buf.String())
	testutil.AssertEqual(t, len(lines), 1)
	entry := decodeLog(t, lines[0])
	for _, key := range []string{"run_id", "stage", "cost_usd", "token_in", "token_out", "duration_ms", "retry_count", "retry_reason", "critic_score", "human_override"} {
		if _, ok := entry[key]; !ok {
			t.Errorf("log missing key %q: %s", key, lines[0])
		}
	}
	testutil.AssertEqual(t, entry["msg"].(string), "stage observation")
	testutil.AssertEqual(t, entry["stage"].(string), string(domain.StageWrite))
}

func TestRecorder_Record_CapExceeded_StillPersists(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	store := &fakeObsStore{}
	acc := pipeline.NewCostAccumulator(map[domain.Stage]float64{
		domain.StageWrite: 0.10,
	}, 0)
	logger, buf := testutil.CaptureLog(t)
	rec := pipeline.NewRecorder(store, acc, clock.RealClock{}, logger)

	// First call: under cap, no error.
	_ = rec.Record(context.Background(), "r1", domain.StageObservation{Stage: domain.StageWrite, CostUSD: 0.05})

	// Second call pushes past $0.10 cap → error, but store is still called.
	err := rec.Record(context.Background(), "r1", domain.StageObservation{Stage: domain.StageWrite, CostUSD: 0.10})
	if !errors.Is(err, domain.ErrCostCapExceeded) {
		t.Fatalf("expected ErrCostCapExceeded, got %v", err)
	}
	testutil.AssertEqual(t, store.count(), 2) // NFR-C3: persisted even on cap

	// Warn log must follow the info log.
	lines := splitLogLines(buf.String())
	if len(lines) < 3 {
		t.Fatalf("expected ≥3 log lines (info, info, warn), got %d", len(lines))
	}
	warn := decodeLog(t, lines[len(lines)-1])
	testutil.AssertEqual(t, warn["msg"].(string), "cost cap exceeded")
	testutil.AssertEqual(t, warn["cap_reason"].(string), "stage_cap")
}

func TestRecorder_Record_StoreError_ReturnsJoinedWhenAlsoCapExceeded(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	storeErr := errors.New("simulated db failure")
	store := &fakeObsStore{err: storeErr}
	acc := pipeline.NewCostAccumulator(map[domain.Stage]float64{
		domain.StageWrite: 0.05,
	}, 0)
	logger, _ := testutil.CaptureLog(t)
	rec := pipeline.NewRecorder(store, acc, clock.RealClock{}, logger)

	err := rec.Record(context.Background(), "r1", domain.StageObservation{Stage: domain.StageWrite, CostUSD: 0.10})
	if err == nil {
		t.Fatal("expected joined error, got nil")
	}
	if !errors.Is(err, domain.ErrCostCapExceeded) {
		t.Errorf("expected errors.Is ErrCostCapExceeded, got %v", err)
	}
	if !errors.Is(err, storeErr) {
		t.Errorf("expected errors.Is storeErr, got %v", err)
	}
}

func TestRecorder_Record_ValidationError_NoStoreCall(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	store := &fakeObsStore{}
	logger, buf := testutil.CaptureLog(t)
	rec := pipeline.NewRecorder(store, nil, clock.RealClock{}, logger)

	err := rec.Record(context.Background(), "r1", domain.StageObservation{
		Stage:   domain.StageWrite,
		CostUSD: -1,
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	testutil.AssertEqual(t, store.count(), 0)
	if strings.TrimSpace(buf.String()) != "" {
		t.Errorf("expected no log output, got %q", buf.String())
	}
}

func TestRecorder_RecordRetry_Shape(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	store := &fakeObsStore{}
	logger, _ := testutil.CaptureLog(t)
	rec := pipeline.NewRecorder(store, nil, clock.RealClock{}, logger)

	if err := rec.RecordRetry(context.Background(), "r1", domain.StageWrite, "rate_limit"); err != nil {
		t.Fatalf("RecordRetry: %v", err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	got := store.calls[0]
	testutil.AssertEqual(t, got.Stage, domain.StageWrite)
	testutil.AssertEqual(t, got.RetryCount, 1)
	if got.RetryReason == nil || *got.RetryReason != "rate_limit" {
		t.Errorf("RetryReason: got %v want rate_limit", got.RetryReason)
	}
	testutil.AssertEqual(t, got.CostUSD, 0.0)
	testutil.AssertEqual(t, got.DurationMs, int64(0))
	testutil.AssertEqual(t, got.TokenIn, 0)
	testutil.AssertEqual(t, got.TokenOut, 0)
	testutil.AssertEqual(t, got.HumanOverride, false)
}

func TestRecorder_Record_Concurrent(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	store := &fakeObsStore{}
	acc := pipeline.NewCostAccumulator(nil, 0)
	logger, _ := testutil.CaptureLog(t)
	rec := pipeline.NewRecorder(store, acc, clock.RealClock{}, logger)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = rec.Record(context.Background(), "r1", domain.StageObservation{Stage: domain.StageImage, CostUSD: 0.01})
		}()
	}
	wg.Wait()
	testutil.AssertEqual(t, store.count(), 50)
}

func TestRecorder_RecordAntiProgress_ShapeAndLogs(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	store := &fakeObsStore{}
	acc := pipeline.NewCostAccumulator(nil, 0)
	logger, buf := testutil.CaptureLog(t)
	rec := pipeline.NewRecorder(store, acc, clock.RealClock{}, logger)

	if err := rec.RecordAntiProgress(context.Background(), "scp-049-run-1", domain.StageWrite, 0.98, 0.92); err != nil {
		t.Fatalf("RecordAntiProgress: %v", err)
	}
	testutil.AssertEqual(t, store.count(), 1)

	lines := splitLogLines(buf.String())
	if len(lines) != 2 {
		t.Fatalf("expected 2 log lines (warn + info), got %d: %v", len(lines), lines)
	}

	warnEntry := decodeLog(t, lines[0])
	testutil.AssertEqual(t, warnEntry["msg"].(string), "anti-progress detected")
	testutil.AssertEqual(t, warnEntry["stage"].(string), string(domain.StageWrite))
	testutil.AssertEqual(t, warnEntry["similarity"].(float64), 0.98)
	testutil.AssertEqual(t, warnEntry["threshold"].(float64), 0.92)
	testutil.AssertEqual(t, warnEntry["run_id"].(string), "scp-049-run-1")

	infoEntry := decodeLog(t, lines[1])
	testutil.AssertEqual(t, infoEntry["msg"].(string), "stage observation")

	got := store.calls[0]
	testutil.AssertEqual(t, got.Stage, domain.StageWrite)
	testutil.AssertEqual(t, got.RetryCount, 1)
	if got.RetryReason == nil || *got.RetryReason != "anti_progress" {
		t.Errorf("RetryReason = %v, want \"anti_progress\"", got.RetryReason)
	}
	testutil.AssertEqual(t, got.CostUSD, 0.0)
	testutil.AssertEqual(t, got.TokenIn, 0)
	testutil.AssertEqual(t, got.TokenOut, 0)
	testutil.AssertEqual(t, got.HumanOverride, false)
}

func TestRecorder_RecordAntiProgress_SecondCallIncrementsRetry(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	store := &fakeObsStore{}
	acc := pipeline.NewCostAccumulator(nil, 0)
	logger, _ := testutil.CaptureLog(t)
	rec := pipeline.NewRecorder(store, acc, clock.RealClock{}, logger)

	for i := 0; i < 2; i++ {
		if err := rec.RecordAntiProgress(context.Background(), "r1", domain.StageWrite, 0.95, 0.92); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	testutil.AssertEqual(t, store.count(), 2)
	// Each call sends RetryCount=1 to the store; real RunStore accumulates via
	// SQL UPDATE. The fake's append-only list proves the caller intent.
	for i, obs := range store.calls {
		testutil.AssertEqual(t, obs.RetryCount, 1)
		if obs.RetryReason == nil || *obs.RetryReason != "anti_progress" {
			t.Errorf("call %d: RetryReason = %v", i, obs.RetryReason)
		}
	}
}

func TestRecorder_RecordAntiProgress_PropagatesStoreError(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	storeErr := errors.New("boom")
	store := &fakeObsStore{err: storeErr}
	acc := pipeline.NewCostAccumulator(nil, 0)
	logger, buf := testutil.CaptureLog(t)
	rec := pipeline.NewRecorder(store, acc, clock.RealClock{}, logger)

	err := rec.RecordAntiProgress(context.Background(), "r1", domain.StageWrite, 0.99, 0.92)
	if err == nil {
		t.Fatal("expected error propagated from store")
	}
	if !errors.Is(err, storeErr) {
		t.Errorf("err = %v, want wrapping storeErr", err)
	}
	// Warn was still emitted (fire-and-forget logging before delegation).
	if !strings.Contains(buf.String(), "anti-progress detected") {
		t.Errorf("expected warn log even on store error, got: %s", buf.String())
	}
}

// splitLogLines returns non-empty lines of the buffer output.
func splitLogLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

func decodeLog(t testing.TB, line string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("decode log line %q: %v", line, err)
	}
	return m
}
