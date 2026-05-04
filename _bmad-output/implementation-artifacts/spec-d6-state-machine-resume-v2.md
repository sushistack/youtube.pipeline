---
title: 'D6 — state machine + resume wiring (writer atomic; mid-writer resume disallowed)'
type: 'feature'
created: '2026-05-04'
status: 'done'
baseline_commit: '6b0dbb948a294dca9949c49b223542305d7d1fb1'
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

D6 is primarily a **lock-the-invariant** spec: the atomic boundary is already encoded in the writer agent (D1) and the cycle-C classification fix already lives in resume.go (v2 baseline). D6 adds explicit invariant tests so the contract cannot regress, and verifies the v2 state machine has no polisher transition.

- `internal/pipeline/agents/writer.go` -- writer agent. `state.Narration` is set ONLY at the end after all 4 acts × 2 stages succeed (writer.go:173). Atomic by construction; D6 verifies via tests.
- `internal/pipeline/phase_a.go` -- runner has no separate narration-persistence step; it just invokes the writer agent. Atomicity lives in the agent. D6 verifies the integration boundary (mid-stage failure → no scenario.json + ErrStageFailed wrapping).
- `internal/pipeline/resume.go` -- carries cycle-C post_writer-retry classification fix (resume.go:317–333). On `state.Critic.PostWriter.Verdict == retry`, writes `Stage: StageWrite, Status: StatusFailed` + returns `ErrStageFailed`. `state.Narration` is never advanced past write. D6 adds regression test.
- `internal/pipeline/engine.go` -- `NextStage` is the v2 stage machine. Polisher is not a `domain.Stage` (it's a runner-internal sub-stage), so the transition table is already polisher-free. D6 verifies via test.
- `internal/pipeline/agents/writer_test.go` -- existing `TestWriter_Stage1OkThenStage2Fails_AtomicFailure` covers Act 1 failure. D6 adds mid-cascade case (Act 1 OK, Act 2 fails → no state.Narration; subsequent acts not attempted).
- `internal/pipeline/phase_a_integration_test.go` -- D6 adds writer-mid-stage failure integration test (real PhaseARunner + mock writer agent that fails after partial work → state.Narration nil + no scenario.json + ErrStageFailed wrap).
- `internal/pipeline/engine_test.go` -- D6 adds post_writer retry advancement test: `engine.Advance` with `state.Critic.PostWriter.Verdict=retry` → run.Stage=write, run.Status=failed, ErrStageFailed wrap; PostReviewer left nil (cycle-C path).

Polisher slot in `PhaseARunner` is intentionally retained as a no-op stub (D1 spec change log; D7 reintroduction). Not D6's scope.

## Tasks & Acceptance

**Execution:**
- [x] `internal/pipeline/agents/writer_test.go` -- add `TestWriter_MidCascadeFailure_LeavesNarrationNil`: Act 1 stage1+stage2 OK, Act 2 stage 2 fails after retries; assert `state.Narration == nil` AND stage1 calls counted (act 1: 1 stage1, act 2: 1 stage1; acts 3,4: 0 — fail-fast cascade).
- [x] `internal/pipeline/phase_a_integration_test.go` -- add `TestPhaseAIntegration_WriterMidStageFailure_AtomicNarration`: stub writer agent returns error after stage attempt, leaves state.Narration nil; assert PhaseARunner.Run returns wrapped error with `stage=writer`, no scenario.json on disk, `state.Narration == nil`, `state.FinishedAt == ""`.
- [x] `internal/pipeline/phase_a_integration_test.go` -- add `TestPhaseAIntegration_ResumeAfterWriterFailure_RunsWriterFromScratch`: simulate failed writer run by entering Run() with `state.Narration == nil`; assert writer agent invoked from scratch (no partial reuse) and on success `state.Narration.Acts` is fully populated.
- [x] `internal/pipeline/engine_test.go` -- add `TestEngineAdvance_PostWriterRetry_StaysAtWriteFailed`: PhaseAExecutor stub sets `state.Critic.PostWriter = retry verdict`, leaves PostReviewer nil; assert `engine.Advance` returns `ErrStageFailed`, persists `Stage=write/Status=failed`, no scenario.json, no advance to post-writer (cycle-C regression).
- [x] `internal/pipeline/engine_test.go` -- add `TestNextStage_V2StageList_NoPolisherTransition`: walk every `(stage, event)` pair in `NextStage` and assert no transition involves a polisher domain stage (since polisher is a runner-internal sub-stage, this is a freeze test).
- [x] Run `go test -race ./internal/pipeline/...` to verify no data races at the atomic boundary (no shared mutable state between writer's per-act loop and PhaseARunner).

**Acceptance Criteria:**
- Given a writer mock that succeeds Act 1 and fails Act 2 stage 2 after retries, when the writer agent runs, then `state.Narration == nil` and stage 1 was invoked exactly twice (acts 1, 2 — never 3, 4).
- Given a phase_a runner with a writer that returns an error mid-stage, when `Run` is called, then it returns an error wrapped with `stage=writer`, `state.Narration` is nil, `state.FinishedAt` is empty, and no `scenario.json` exists on disk under the run dir.
- Given a phase_a runner re-invoked after a previous writer failure (state.Narration nil, no narration cache file — narration is not cached on disk by design), when `Run` is called and the writer succeeds, then `state.Narration` is populated end-to-end (no partial reuse of stage 1 output from the prior failed attempt).
- Given an engine with a PhaseAExecutor that emits a post_writer retry verdict, when `engine.Advance` runs, then it returns `ErrStageFailed`, persists `Stage=StageWrite/Status=StatusFailed`, leaves `state.Critic.PostReviewer` nil per cycle-C short-circuit, and produces no scenario.json.
- Given the v2 NextStage table, when iterated over all `(stage, event)` pairs, then no transition produces a polisher-named domain stage.
- Given `go test -race ./internal/pipeline/...`, all green.

## Spec Change Log

### 2026-05-04 — review pass 1: harden invariant freezes

The first implementation pass added the six tests the spec called for and
all passed (`go test -race ./internal/pipeline/... -v` clean). Three
parallel reviews (blind hunter, edge case hunter, acceptance auditor)
flagged hardening opportunities. None were classified `bad_spec` or
`intent_gap`; all resolved `patch`. Applied patches:

- **Atomic boundary coverage** — Added `TestWriter_FullCascadeOk_ScriptValidatorFailure_LeavesNarrationNil`
  to `writer_test.go`. The mid-cascade test only exercises the per-act
  failure branch; this sibling exercises the post-cascade
  forbidden-terms / validator path (`writer.go:165–170`) where every act
  succeeds but the assembled `NarrationScript` is rejected. Without it,
  a refactor that moved `state.Narration = &script` above the
  script-level checks would silently regress.
- **Retry budget pinned to constant** — `TestWriter_MidCascadeFailure_*`
  now sources its bad-response queue depth from
  `writerPerStageRetryBudget+1` instead of hardcoding 2. A future budget
  tweak will not silently break the freeze.
- **Resume test now actually exercises resume** — `TestPhaseAIntegration_ResumeAfterWriterFailure_*`
  now runs the runner twice on the same run dir: attempt 1 fails the
  writer, attempt 2 succeeds. Writer is asserted invoked once per
  attempt with `state.Narration` nil on entry both times. The original
  single-Run test was a tautology (`Narration` starts nil on every fresh
  state) — the two-attempt shape is the resume invariant.
- **Cache freeze inverted** — `assertNoNarrationCache` walks the run dir
  and rejects any filename matching narration/writer-stage substrings
  (allowlist: research_cache.json, structure_cache.json, scenario.json).
  The previous closed denylist would have missed regression cache names
  not in the list.
- **t.Fatal in stub closures replaced with counters** — The
  `TestPhaseAIntegration_WriterMidStageFailure_AtomicNarration` stubs no
  longer call `t.Fatal` from agent callbacks. Each stage tracks calls
  via `atomic.Int32` counters (forward-compatible with `-race` if the
  runner ever parallelizes stages); post-Run assertions pin the chain
  shape: researcher=1, structurer=1, writer=1, every downstream stage=0.
- **Timestamp invariant pinned both sides** — Failure path asserts
  `StartedAt!=""` (stamped before chain) AND `FinishedAt==""` (only set
  on full success). Success path asserts both timestamps non-empty plus
  scenario.json on disk.
- **post_writer retry CriticScore round-trip** — `TestEngineAdvance_PostWriterRetry_*`
  now asserts `runs.run.CriticScore == 0.52` (NormalizeCriticScore of
  the test's OverallScore=52). Also asserts the error message does NOT
  contain "post_reviewer" — the cycle-C bug symptom freeze.
- **NextStage freeze tightened** — `TestNextStage_NoPolisherTransition`
  now counts successful transitions and requires ≥15. It also iterates
  `IsHITLStage` and `StatusForStage` for every domain.Stage and checks
  no polisher-named stage surfaces on either. The original test would
  have green-passed if all NextStage transitions silently errored.
- **Spec / docstring** — Integration test docstring clarified to state
  it locks the runner-level boundary (writer error → no Narration
  synthesized by runner, downstream skipped) and points to the
  unit-level writer atomicity tests for the agent-side guarantee.

KEEP: the unit-level writer atomicity test pattern (per-act call
counters + state.Narration nil assertion) is the load-bearing freeze.
Integration tests complement it but cannot replace it — the runner
treats writer as a black box.

## Design Notes

The atomicity invariant is the simplest expressible formulation of "writer must not leave half-states." A `writer_stage1_complete` flag was considered in the D plan and rejected — it adds a state variable that exists only to encode an invariant that atomic persistence already guarantees. Per Jay's "no dead layers" memory, that flag is dead-layer risk on its own.

D6's deliverable is mostly **tests**, not code. The atomic boundary already lives in `writer.go` (D1: `state.Narration = &script` is the last action after all 4 acts × 2 stages succeed) and the cycle-C post_writer classification fix already lives in `resume.go` (v2 baseline). Without explicit invariant tests, a future refactor could silently introduce partial persistence (e.g., setting `state.Narration.Acts = acts` inside the per-act loop) and pass existing happy-path tests. D6 locks the contract.

Resume granularity tradeoff is real and accepted: v1 could resume per-act mid-writer; v2 cannot. The compensating factor is that writer two-stage is faster per attempt (parallel acts, smaller per-call envelope) so the practical cost of a full re-run is ≤ v1's per-act resume cost in most failure modes. Document this tradeoff in the spec change log if it surfaces empirically as a problem.

Polisher is intentionally retained as a no-op stub in `PhaseARunner` per D1's spec change log — preserves the constructor signature so `cmd/pipeline/serve.go` doesn't churn, and gives D7 a known re-introduction point. The "polisher excluded per P5" boundary refers to the v2 **state machine** (`engine.NextStage`), where polisher is not — and never was — a `domain.Stage`. D6 verifies this with a freeze test rather than rewiring.

## Verification

**Commands:**
- `go build ./...` -- expected: clean.
- `go test ./internal/pipeline/... ./internal/pipeline/agents/...` -- expected: all green; new D6 tests pass.
- `go test -race ./internal/pipeline/...` -- expected: all green; no data races at the atomic boundary.

**Manual checks:**
- `git grep -n "writer_stage1_complete\|narration_pending"` -- expected: zero matches (flags forbidden per Boundaries).
- Inspect `internal/pipeline/agents/writer.go:173` -- `state.Narration = &script` is the final write; no per-act partial assignment exists.
- Inspect `internal/pipeline/resume.go:317-333` -- post_writer retry classification untouched (cycle-C verbatim).
