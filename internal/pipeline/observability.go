package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// NOTE: This package never advances runs.status. Status transitions belong to
// engine.Resume / engine.Advance. NFR-P3 (429 does not advance stage status)
// is upheld by the absence of SetStatus calls in the retry+record flow.

// ObservationStore is the minimal persistence surface the Recorder needs.
// Declared locally (not in service/) to keep pipeline/ one-way dependent.
// *db.RunStore satisfies this interface structurally.
type ObservationStore interface {
	RecordStageObservation(ctx context.Context, runID string, obs domain.StageObservation) error
}

// Recorder orchestrates stage observation persistence and the cost circuit
// breaker. It is the only code path through which pipeline code mutates the
// 8 observability columns on the runs row.
type Recorder struct {
	store  ObservationStore
	costs  *CostAccumulator
	clock  clock.Clock
	logger *slog.Logger
}

// NewRecorder builds a Recorder. costs MAY be nil to disable cost enforcement
// (useful in tests that only exercise persistence). logger MUST be non-nil;
// the caller is responsible for constructor injection per NFR-L1.
func NewRecorder(store ObservationStore, costs *CostAccumulator, clk clock.Clock, logger *slog.Logger) *Recorder {
	if logger == nil {
		logger = slog.Default()
	}
	return &Recorder{store: store, costs: costs, clock: clk, logger: logger}
}

// Record persists a stage observation to the runs row and folds the cost
// delta into the accumulator. Ordering is deliberate:
//
//  1. obs.Validate() — fail fast on malformed input.
//  2. costs.Add — updates the in-memory accumulator (NFR-C3: always record).
//  3. store.RecordStageObservation — persist to DB (NFR-C3: always persist).
//  4. structured slog.Info per call; slog.Warn on cap-exceeded.
//
// NOT idempotent: calling twice with the same obs doubles the accumulating
// columns. This is by design — every call is a real spend event.
//
// On cap-exceeded, the persistence still runs and the returned error wraps
// ErrCostCapExceeded. On a DB error together with a cap-exceeded error, both
// are joined so callers can errors.Is each independently.
func (r *Recorder) Record(ctx context.Context, runID string, obs domain.StageObservation) error {
	if err := obs.Validate(); err != nil {
		return fmt.Errorf("recorder: %w", err)
	}

	var costErr error
	if r.costs != nil {
		costErr = r.costs.Add(obs.Stage, obs.CostUSD)
	}

	dbErr := r.store.RecordStageObservation(ctx, runID, obs)

	r.logger.Info("stage observation",
		"run_id", runID,
		"stage", string(obs.Stage),
		"cost_usd", obs.CostUSD,
		"token_in", obs.TokenIn,
		"token_out", obs.TokenOut,
		"duration_ms", obs.DurationMs,
		"retry_count", obs.RetryCount,
		"retry_reason", nullableString(obs.RetryReason),
		"critic_score", nullableFloat(obs.CriticScore),
		"human_override", obs.HumanOverride,
	)

	if costErr != nil {
		stage, reason := "", ""
		if r.costs != nil {
			s, rz := r.costs.TripReason()
			stage, reason = string(s), rz
		}
		r.logger.Warn("cost cap exceeded",
			"run_id", runID,
			"stage", string(obs.Stage),
			"cap_stage", stage,
			"cap_reason", reason,
			"run_total", runTotalOr(r.costs),
		)
	}

	switch {
	case dbErr != nil && costErr != nil:
		return errors.Join(costErr, dbErr)
	case dbErr != nil:
		return fmt.Errorf("recorder: %w", dbErr)
	case costErr != nil:
		return costErr
	default:
		return nil
	}
}

// RecordRetry is a convenience for the 429 backoff flow. It builds an
// observation with only RetryCount=1 + RetryReason=reason and persists via
// Record. Cost is zero so no cap fires.
func (r *Recorder) RecordRetry(ctx context.Context, runID string, stage domain.Stage, reason string) error {
	obs := domain.StageObservation{
		Stage:       stage,
		RetryCount:  1,
		RetryReason: &reason,
	}
	return r.Record(ctx, runID, obs)
}

// RecordAntiProgress persists an anti-progress event (FR8). Semantics:
//   - RetryCount += 1 (this retry was attempted before we detected stuckness)
//   - RetryReason overwrites with "anti_progress"
//   - Cost is zero (the underlying LLM call is charged separately via Record;
//     this method is the decision that short-circuits the loop, not a new
//     external call)
//
// Emits a slog.Warn("anti-progress detected") line BEFORE delegating to
// Record so the warn is guaranteed even if Record returns an error.
//
// The caller is responsible for ALSO returning domain.ErrAntiProgress so
// the engine routes the run to HITL. This method records the event; it
// does NOT alter the run's stage/status.
func (r *Recorder) RecordAntiProgress(
	ctx context.Context,
	runID string,
	stage domain.Stage,
	similarity float64,
	threshold float64,
) error {
	r.logger.Warn("anti-progress detected",
		"run_id", runID,
		"stage", string(stage),
		"similarity", similarity,
		"threshold", threshold,
	)
	reason := "anti_progress"
	obs := domain.StageObservation{
		Stage:       stage,
		RetryCount:  1,
		RetryReason: &reason,
	}
	return r.Record(ctx, runID, obs)
}

func nullableString(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullableFloat(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func runTotalOr(c *CostAccumulator) float64 {
	if c == nil {
		return 0
	}
	return c.RunTotal()
}
