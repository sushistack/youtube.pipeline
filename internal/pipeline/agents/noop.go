package agents

import "context"

// NoopAgent returns an AgentFunc that succeeds without touching state.
// Useful as a placeholder while wiring a partial chain during incremental
// development (Stories 3.2–3.5) and as a spy stand-in in tests.
func NoopAgent() AgentFunc {
	return func(ctx context.Context, state *PipelineState) error { return nil }
}
