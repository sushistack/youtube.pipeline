package main

import (
	"context"

	"github.com/sushistack/youtube.pipeline/internal/critic/eval"
)

// nullShadowSource returns an empty candidate set, triggering ShadowReport.Empty.
// Used in CI environments where no production database is available. The gate
// treats Empty as a soft-fail (warning, non-blocking) so CI does not turn red
// merely because it has no production runs to replay.
type nullShadowSource struct{}

func (nullShadowSource) RecentPassedCases(_ context.Context, _ int) ([]eval.ShadowCase, error) {
	return nil, nil
}
