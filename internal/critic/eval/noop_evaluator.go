package eval

import (
	"context"
	"fmt"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// NotConfiguredEvaluator returns a validation error from every Evaluate
// call. It is the default evaluator injected into the Tuning service when
// no real Critic-backed text-generation runtime has been wired in this
// build. It lets the prompt / golden-list / calibration endpoints operate
// fully while causing RunGolden, RunShadow, and Fast Feedback to fail
// loudly so the UI can surface a "provider not configured" state rather
// than silently reporting a fabricated green result.
type NotConfiguredEvaluator struct{}

// Evaluate satisfies the Evaluator interface but always refuses — no
// fixture is ever graded. Error is wrapped with domain.ErrValidation so
// handlers translate it to HTTP 400 with a VALIDATION_ERROR code.
func (NotConfiguredEvaluator) Evaluate(context.Context, Fixture) (VerdictResult, error) {
	return VerdictResult{}, fmt.Errorf("critic evaluator not configured: %w", domain.ErrValidation)
}
