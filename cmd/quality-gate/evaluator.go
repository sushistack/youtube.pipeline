package main

import (
	"context"

	"github.com/sushistack/youtube.pipeline/internal/critic/eval"
)

// fixtureExpectationEvaluator returns each fixture's declared ExpectedVerdict
// without calling an external LLM. This keeps the CI quality gate free of API
// key requirements while still exercising the full RunGolden/RunShadow
// pipeline: fixture loading, manifest I/O, threshold enforcement, and report
// rendering all run exactly as in production. The evaluator is the only seam
// that differs from a live critic-backed run.
//
// For Golden fixtures the ExpectedVerdict is "pass" (positive) or "retry"
// (negative), so recall is deterministic and reflects fixture integrity rather
// than LLM behaviour. For Shadow replay fixtures LoadShadowInput always sets
// ExpectedVerdict="pass", so false_rejections remain 0 unless the source
// returns cases with a corrupted scenario artifact.
type fixtureExpectationEvaluator struct{}

func (fixtureExpectationEvaluator) Evaluate(_ context.Context, f eval.Fixture) (eval.VerdictResult, error) {
	return eval.VerdictResult{Verdict: f.ExpectedVerdict, OverallScore: 80}, nil
}
