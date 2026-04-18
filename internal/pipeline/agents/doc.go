// Package agents declares the AgentFunc contract and PipelineState
// carrier for the Phase A 6-agent chain (Researcher → Structurer →
// Writer → VisualBreakdowner → Reviewer → Critic).
//
// Agents are pure functions (no DB, no HTTP, no filesystem). External
// capabilities (LLM text generation, local corpus reads) are injected
// via domain.TextGenerator closures provided through agent factory
// functions. The PhaseARunner in the parent pipeline/ package owns
// orchestration and scenario.json persistence.
//
// Stories 3.2–3.5 each introduce one or two concrete agents; Story 3.1
// ships this scaffold.
package agents
