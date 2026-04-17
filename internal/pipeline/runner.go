package pipeline

import "context"

// Runner abstracts the pipeline execution engine.
// Defined here (not in service/) to allow service/ to depend on pipeline.Runner
// without pipeline/ depending on service/. This breaks the engine ↔ run_service
// circular dependency.
type Runner interface {
	Advance(ctx context.Context, runID string) error
	Resume(ctx context.Context, runID string) error
}
