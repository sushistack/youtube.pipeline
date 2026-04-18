package agents

import (
	"context"
	"fmt"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// AgentFunc is the Phase A agent contract. Each agent is a pure function
// that reads input fields of state, writes its output fields of state,
// and returns an error on failure. Purity rule — enforced by layer-lint
// (AC-PURITY-LINT) and asserted in tests (AC-TESTS-PURITY):
//   - NO database access (no internal/db import)
//   - NO HTTP calls (LLM calls are injected via domain.TextGenerator)
//   - NO filesystem side effects (state is in-memory; the runner owns
//     scenario.json persistence)
//   - NO goroutines that outlive the call
//   - NO shared mutable state beyond the *PipelineState argument
//
// If an agent needs external capabilities (text generation, local corpus
// reads), construct it via a factory that closes over the dependency
// and returns an AgentFunc — do NOT add fields to PipelineState that
// carry service-layer or db-layer types.
type AgentFunc func(ctx context.Context, state *PipelineState) error

// PipelineState is the in-memory data carrier passed between Phase A
// agents. Each agent reads upstream fields and writes its own output
// field. Fields are EXPLICITLY TYPED per agent — no map[string]any,
// no generic "payload" bag — so schema drift is a compile error.
//
// Persistence: PipelineState lives in memory during Phase A execution
// only. The runner serializes it to {outputDir}/{runID}/scenario.json
// after the Critic agent returns successfully (AC-SCENARIO-JSON).
// Never embed, never carry domain.TextGenerator, *sql.DB, *http.Client,
// or any other service/infrastructure handle — those flow through agent
// factories (AC-AGENTFUNC-TYPE).
//
// Story 3.1 defines the slots but not the concrete schemas for each
// output — Stories 3.2–3.5 promote these fields to domain types
// (ResearchSummary, ScenarioStructure, NarrationScript, ShotBreakdown,
// ReviewReport, CriticOutput). Writer/Critic are promoted in Story 3.3
// on this same carrier; no duplicate pipeline state type is allowed.
type PipelineState struct {
	// Input — populated by the runner from the Run row before the chain starts.
	RunID string `json:"run_id"`
	SCPID string `json:"scp_id"`

	// Agent outputs — populated left-to-right by the chain. A nil value
	// means "upstream agent has not run yet"; a non-nil value is the
	// agent's serialized output. Stories 3.2–3.5 progressively replace
	// placeholders with strongly-typed structs on this same carrier.
	Research        *domain.ResearcherOutput      `json:"research,omitempty"`         // Researcher (3.2)
	Structure       *domain.StructurerOutput      `json:"structure,omitempty"`        // Structurer (3.2)
	Narration       *domain.NarrationScript       `json:"narration,omitempty"`        // Writer (3.3)
	VisualBreakdown *domain.VisualBreakdownOutput `json:"visual_breakdown,omitempty"` // VisualBreakdowner (3.4)
	Review          *domain.ReviewReport          `json:"review,omitempty"`           // Reviewer (3.4)
	Critic          *domain.CriticOutput          `json:"critic,omitempty"`           // Critic (3.3 post-Writer + 3.5 post-Reviewer)
	Quality         *PhaseAQualitySummary         `json:"quality,omitempty"`          // Final Phase A quality summary (3.5)
	Contracts       *PhaseAContractManifest       `json:"contracts,omitempty"`        // Final Phase A schema manifest (3.5)

	// Provenance — runner-populated bookkeeping for NFR-M2 (version-
	// controlled artifacts must record their own generator).
	StartedAt  string `json:"started_at"`  // RFC3339 from clock.Clock
	FinishedAt string `json:"finished_at"` // RFC3339; empty until chain completes
}

// PipelineStage is the ordinal position of a Phase A agent within the
// chain. It is NOT persisted — it exists only so the runner and tests
// can reference a specific agent slot by a typed constant instead of
// an integer.
type PipelineStage int

const (
	StageResearcher PipelineStage = iota
	StageStructurer
	StageWriter
	StagePostWriterCritic  // post-writer Critic checkpoint (3.3); runs between Writer and VisualBreakdowner
	StageVisualBreakdowner
	StageReviewer
	StageCritic
	phaseAStageCount // sentinel: number of agents in the Phase A chain
)

// String returns the canonical snake_case name of the Phase A agent
// slot. Used in structured logs and error wrapping.
func (ps PipelineStage) String() string {
	switch ps {
	case StageResearcher:
		return "researcher"
	case StageStructurer:
		return "structurer"
	case StageWriter:
		return "writer"
	case StagePostWriterCritic:
		return "post_writer_critic"
	case StageVisualBreakdowner:
		return "visual_breakdowner"
	case StageReviewer:
		return "reviewer"
	case StageCritic:
		return "critic"
	}
	return fmt.Sprintf("unknown_pipeline_stage(%d)", int(ps))
}

// DomainStage maps a PipelineStage to its corresponding domain.Stage for
// observability/logging purposes only. The chain's internal invariants
// speak in PipelineStage; domain.Stage is the run-level state machine
// position. Unknown values panic (programmer error, unreachable).
func (ps PipelineStage) DomainStage() domain.Stage {
	switch ps {
	case StageResearcher:
		return domain.StageResearch
	case StageStructurer:
		return domain.StageStructure
	case StageWriter:
		return domain.StageWrite
	case StagePostWriterCritic:
		return domain.StageCritic
	case StageVisualBreakdowner:
		return domain.StageVisualBreak
	case StageReviewer:
		return domain.StageReview
	case StageCritic:
		return domain.StageCritic
	}
	panic(fmt.Sprintf("agents: DomainStage called with out-of-range PipelineStage(%d)", int(ps)))
}
