---
title: 'D6 — state machine + resume wiring (writer atomic; mid-writer resume disallowed)'
type: 'feature'
created: '2026-05-04'
status: 'draft'
context:
  - '_bmad-output/planning-artifacts/next-session-monologue-mode-decoupling.md'
  - '_bmad-output/implementation-artifacts/spec-d1-domain-types-and-writer-v2.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** D1's writer is two-stage (monologue + beat segmentation) but the engine treats writer as a single atomic stage. Without explicit wiring, a run that dies between writer-stage1 and writer-stage2 may persist a partial `state.Narration` (acts have monologues but missing beats) — which subsequent resume attempts would treat as a valid stage artifact and skip the stage. That's the D plan's own warning: "writer-stage1 output (monologues only, beats absent) is **not a valid stage artifact** — resume cannot restart from the mid-writer point."

**Approach:** Enforce writer atomicity at the engine wiring level. The writer stage persists `state.Narration` ONLY if all acts have BOTH stages complete. On any per-stage failure, `state.Narration` is left unset (or rolled back to its pre-writer value) and the run fails with `ErrStageFailed`. Resume from a failed-writer run restarts the writer stage from scratch — never mid-writer. Update `phase_a` wiring + resume.go to reflect this. Carry the cycle-C resume bug fix (commit `2ef1d3c` resume.go path for post_writer retry) forward unchanged — that fix already lives in v2 baseline.

## Boundaries & Constraints

**Always:**
- Writer is atomic: either `state.Narration.Acts` has all 4 acts populated with both Monologue and Beats, or `state.Narration` is unset. No intermediate state persists across run-fail-resume boundaries.
- Resume from a writer-failed run restarts writer from scratch (stage 1 + stage 2 both re-run for all 4 acts).
- Carry cycle-C `resume.go` post_writer-retry classification fix (commit `2ef1d3c`) verbatim — that bug fix is schema-agnostic.
- Stage transition order from D plan: `researcher → structurer → writer (stage1+2 atomic) → critic → visual_breakdowner → tts → assembler → ...` (polisher excluded per P5).
- `phase_a` invariant tests cover the atomic boundary explicitly (writer fails mid-stage → state.Narration nil; resume runs writer fully).

**Ask First:**
- If a per-stage retry within writer (stage1 retry exhausted on act 2 while acts 1/3/4 stage1 succeeded) reveals a desire for partial-act resume, HALT — that's a scope expansion that contradicts the atomic invariant.
- If `state.Narration` rollback requires DB schema changes (e.g., a new `narration_pending` flag), HALT before introducing — atomic should be expressible without new flags.

**Never:**
- No `writer_stage1_complete` flag in state. (Plan considered this; recommended atomic.)
- No mid-writer resume artifact path.
- No partial-act persistence (e.g., persisting acts[0..2] while act 3 retries). Atomicity is at the run level, not the act level.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected | Error Handling |
|---|---|---|---|
| Writer all 4 acts succeed both stages | clean run | `state.Narration` populated; stage advances | N/A |
| Writer stage 1 succeeds for 3 acts, fails act 2 after retries | exhaustion | `state.Narration` left unset (or rolled back); run fails with `ErrStageFailed` | `ErrStageFailed` |
| Writer stage 1 all 4 ok, stage 2 fails act 3 | exhaustion | same as above — `state.Narration` left unset | `ErrStageFailed` |
| Resume from writer-failed run | failed run state | re-run writer from scratch; no partial reuse of stage 1 monologues | N/A |
| post_writer critic retry (cycle-C path) | `state.Critic.PostWriter.Verdict == retry` | resume re-enters writer; same atomic invariants apply | per cycle-C fix |

</frozen-after-approval>

## Code Map

- `internal/pipeline/phase_a.go` (or wherever phase A wiring lives) -- writer stage execution + persistence boundary.
- `internal/pipeline/agents/writer.go` -- writer return contract: either fully populated `[]ActScript` or error; never partial.
- `internal/pipeline/resume.go` -- atomic writer rollback path; carry cycle-C post_writer fix unchanged.
- `internal/pipeline/phase_a_test.go`, `internal/pipeline/phase_a_integration_test.go` -- atomic-boundary invariant tests.
- `internal/pipeline/state_machine.go` (or wherever stage transitions are defined) -- align the v2 stage list (no polisher per P5).
- `internal/runs/runs.go` (or wherever `ApplyPhaseAResult` lives) -- ensure writer-failed result does NOT advance stage to post-writer.

## Tasks & Acceptance

**Execution:**
- [ ] `internal/pipeline/agents/writer.go` -- enforce return contract (fully populated or error).
- [ ] `internal/pipeline/phase_a.go` -- writer persistence happens only after both stages complete for all acts.
- [ ] `internal/pipeline/resume.go` -- atomic rollback path; verify cycle-C post_writer fix retained.
- [ ] `internal/pipeline/state_machine.go` -- v2 stage list (drop polisher per P5).
- [ ] `internal/pipeline/phase_a_test.go` + `phase_a_integration_test.go` -- atomic-boundary invariant tests + I/O matrix coverage.
- [ ] Unit-test: writer-mid-stage failure → `state.Narration` nil after persistence.
- [ ] Unit-test: resume after writer-failed run → writer re-runs stage 1 for all 4 acts.

**Acceptance Criteria:**
- Given a writer-mid-stage failure injected via test mock, when phase A runs, then `state.Narration` is nil at end-of-stage and the run fails with `ErrStageFailed`.
- Given a writer-failed run state and `engine.advancePhaseA` is called, then writer re-runs stage 1 + stage 2 from scratch for all 4 acts (no partial reuse).
- Given a post_writer critic retry verdict, when resume re-enters the engine, then writer re-runs (cycle-C `resume.go` fix path) and stage advance applies post-writer-result symmetric with v1.
- Given `go test -race ./internal/pipeline/...`, then all green — no data races at the atomic boundary.

## Design Notes

The atomicity invariant is the simplest expressible formulation of "writer must not leave half-states." A `writer_stage1_complete` flag was considered in the D plan and rejected — it adds a state variable that exists only to encode an invariant that atomic persistence already guarantees. Per Jay's "no dead layers" memory, that flag is dead-layer risk on its own.

Resume granularity tradeoff is real and accepted: v1 could resume per-act mid-writer; v2 cannot. The compensating factor is that writer two-stage is faster per attempt (parallel acts, smaller per-call envelope) so the practical cost of a full re-run is ≤ v1's per-act resume cost in most failure modes. Document this tradeoff in the spec change log if it surfaces empirically as a problem.

## Verification

**Commands:**
- `go build ./...` + `go test ./...` + `go test -race ./internal/pipeline/...`
- SCP-049 phase-A dogfood with simulated writer-mid-stage failure (use existing test infra) -- confirm atomic rollback.

**Manual checks:**
- `git grep -n "writer_stage1_complete\|narration_pending"` -- expected: zero matches (flags forbidden per Boundaries).
- Inspect a writer-failed run's persisted state -- `state.Narration` is nil; resume restarts writer cleanly.
