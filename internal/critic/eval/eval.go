package eval

import (
	"context"
	"encoding/json"
)

// Evaluator is the narrow seam between the Golden runner and the Critic implementation.
// Story 4.1 uses a fake implementation in tests; the real Critic wires in later.
type Evaluator interface {
	Evaluate(ctx context.Context, fixture Fixture) (VerdictResult, error)
}

// VerdictResult is the outcome of a single evaluation.
//
// OverallScore is the 0..100 Critic rubric score used by Story 4.2 Shadow
// to compute normalized score drift. It is optional for Story 4.1 Golden
// recall reporting (Golden only cares about Verdict) so existing fakes
// that leave it at zero remain valid.
type VerdictResult struct {
	Verdict      string
	RetryReason  string
	OverallScore int
	Model        string
	Provider     string
}

// Fixture is a single Golden eval input with its expected verdict.
type Fixture struct {
	FixtureID       string          `json:"fixture_id"`
	Kind            string          `json:"kind"`             // "positive" | "negative"
	Checkpoint      string          `json:"checkpoint"`       // "post_writer"
	Input           json.RawMessage `json:"input"`            // NarrationScript payload
	ExpectedVerdict string          `json:"expected_verdict"` // "pass" | "retry"
	Category        string          `json:"category"`
	Notes           string          `json:"notes,omitempty"`
}
