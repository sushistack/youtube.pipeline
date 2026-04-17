package domain

import "fmt"

// StageObservation captures the observability deltas produced by one stage
// execution (one successful completion, one failed attempt, one retry). It
// is the input to RunStore.RecordStageObservation, which folds the deltas
// into the runs row.
//
// Semantic conventions (see story 2.4 AC-OBS-RECORD):
//   - DurationMs, TokenIn, TokenOut, RetryCount, CostUSD accumulate (row += obs).
//   - RetryReason and CriticScore overwrite when non-nil; a nil pointer means
//     "no overwrite" (COALESCE-style — preserves the prior value).
//   - HumanOverride is sticky-OR: once true, never reverts.
type StageObservation struct {
	Stage         Stage    `json:"stage"`
	DurationMs    int64    `json:"duration_ms"`
	TokenIn       int      `json:"token_in"`
	TokenOut      int      `json:"token_out"`
	RetryCount    int      `json:"retry_count"`
	RetryReason   *string  `json:"retry_reason,omitempty"`
	CriticScore   *float64 `json:"critic_score,omitempty"`
	CostUSD       float64  `json:"cost_usd"`
	HumanOverride bool     `json:"human_override"`
}

// NewStageObservation returns a zero-valued observation for the given stage.
// Nil pointers stay nil so the DB preserves any prior RetryReason / CriticScore.
func NewStageObservation(stage Stage) StageObservation {
	return StageObservation{Stage: stage}
}

// Validate rejects malformed observations. All numeric deltas must be
// non-negative; Stage must be one of the defined Stage constants. Returns
// ErrValidation on failure so callers can classify via errors.Is.
func (o StageObservation) Validate() error {
	if !o.Stage.IsValid() {
		return fmt.Errorf("stage observation: invalid stage %q: %w", o.Stage, ErrValidation)
	}
	if o.DurationMs < 0 {
		return fmt.Errorf("stage observation: duration_ms %d < 0: %w", o.DurationMs, ErrValidation)
	}
	if o.TokenIn < 0 {
		return fmt.Errorf("stage observation: token_in %d < 0: %w", o.TokenIn, ErrValidation)
	}
	if o.TokenOut < 0 {
		return fmt.Errorf("stage observation: token_out %d < 0: %w", o.TokenOut, ErrValidation)
	}
	if o.RetryCount < 0 {
		return fmt.Errorf("stage observation: retry_count %d < 0: %w", o.RetryCount, ErrValidation)
	}
	if o.CostUSD < 0 {
		return fmt.Errorf("stage observation: cost_usd %.4f < 0: %w", o.CostUSD, ErrValidation)
	}
	return nil
}

// IsZero reports whether every delta field is zero-valued. Useful in tests.
func (o StageObservation) IsZero() bool {
	return o.DurationMs == 0 &&
		o.TokenIn == 0 &&
		o.TokenOut == 0 &&
		o.RetryCount == 0 &&
		o.RetryReason == nil &&
		o.CriticScore == nil &&
		o.CostUSD == 0 &&
		!o.HumanOverride
}
