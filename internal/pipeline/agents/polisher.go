package agents

import (
	"context"
	"fmt"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// NewPolisher returns a no-op stub for the v2 transition. The v1
// per-scene polisher (Lever C, commit c8aea99) was schema-bound to
// NarrationScene and produced reactive prompt patches that golden study
// proved anti-hada (cycle-C revert in commit f71565c).
//
// Per plan resolution P5 (`_bmad-output/planning-artifacts/next-session-monologue-mode-decoupling.md`),
// v2 ships D1–D6 without polisher in line. D7 reintroduces a v2 polisher
// operating on `ActScript[]` with a per-act monologue rune-delta budget
// recalibrated against v2 baseline measurements.
//
// Until then, this stub satisfies PhaseARunner's non-nil agent contract
// without performing any edits. NewPolisher's signature is preserved so
// cmd/pipeline/serve.go does not have to change shape; unused parameters
// are accepted but ignored.
func NewPolisher(
	_ domain.TextGenerator,
	_ TextAgentConfig,
	_ PromptAssets,
	_ *Validator,
	_ *ForbiddenTerms,
) AgentFunc {
	return func(_ context.Context, state *PipelineState) error {
		if state == nil {
			return fmt.Errorf("polisher: %w: state is nil", domain.ErrValidation)
		}
		return nil
	}
}
