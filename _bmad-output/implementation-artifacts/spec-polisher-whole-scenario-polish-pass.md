---
title: 'Polisher: whole-scenario smooth-pass agent stage (Lever C)'
type: 'feature'
created: '2026-05-03'
status: 'in-review'
baseline_commit: '0c9558303265a7d90839a2cc70612cc64999ca23'
context:
  - '{project-root}/_bmad-output/planning-artifacts/next-session-whole-scenario-polish-pass.md'
  - '{project-root}/_bmad-output/implementation-artifacts/spec-writer-continuity-commentary-volume-bridge.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The writer fan-out produces 4 acts independently, so cross-act transitions feel jumpy and the final act-4 closer reads as a scene-ender rather than a video-ender. No single agent sees the full merged script before critic review.

**Approach:** Insert a new `polisher` agent stage between `writer` and `postWriterCritic`. It makes one LLM call over the full merged `NarrationScript` and returns the same schema with three targeted edits: cross-act transition rewrites (first scene of acts 2/3/4), final-closer rewrite (last scene of `unresolved`), and within-act bridge smoothing. If the polisher fails or oversteps its edit budget, it falls back to the writer's output without failing the run.

## Boundaries & Constraints

**Always:**
- Schema unchanged: only `narration` and `narration_beats` text within affected `NarrationScene` entries may change. All other fields are read-only and must be enforced explicitly (not just by JSON schema, which is too permissive).
- Per-scene edit budget: rune-length delta ratio strictly **greater than** `PolisherMaxEditRatio` (0.25) for any scene's `narration` triggers fallback. A ratio of exactly 0.25 is accepted.
- Fallback-not-fail (runtime): on any **runtime** failure (LLM error after retry, schema violation, budget violation, forbidden terms newly introduced), leave `state.Narration` unchanged, emit a `polisher_failed` audit event, and return `nil` to the runner. Polisher never aborts a run on runtime failures.
- Precondition fail-fast (wiring): if the polisher is constructed or invoked with a nil dependency (state, generator, validator, terms) or empty model/provider, return `domain.ErrValidation`. These are wiring bugs, not runtime failures, and must abort the run so they get fixed at the call site rather than silently degrading every run.
- Context cancellation propagates: if `ctx.Err()` is non-nil at any point (before the LLM call, surfaced through `runWithRetry`, or returned by the generator), return the context error directly. Do not fall back — the caller has explicitly asked the run to stop.
- Retry budget: 1 (same pattern as writer per-act via `runWithRetry`). Transient generator errors trigger one retry; non-retryable failures (truncated completion, context cancellation) abort the retry loop.
- Forbidden terms are evaluated as a delta against the writer's pre-polish output: only terms newly introduced by the polisher trigger fallback. Terms already present in the writer's output are the writer's responsibility, not the polisher's, and must not cause the polisher to silently revert good edits.
- Audit log: one `AuditEventTextGeneration` entry **after all post-LLM checks pass and `state.Narration` is replaced** — never inside the retry loop where it could pair with a later `polisher_failed`. One `polisher_failed` entry on any fallback path.

**Ask First:**
- If the 25% rune-delta threshold needs tuning after the first SCP-049 dogfood, surface to user before changing `PolisherMaxEditRatio`.

**Never:**
- New fields on `NarrationScript` or `NarrationScene`.
- Multiple LLM calls (no per-scene fan-out).
- Changing writer, postWriterCritic, or visualBreakdowner logic.
- Per-scene Levenshtein computation — rune-length delta ratio is the budget check.
- YAML config block for polisher (constructed in code, same as writer).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Happy path | Valid `state.Narration` from writer; LLM returns schema-valid script within budget | `state.Narration` replaced with polished script; one `text_generation` audit event emitted **after** state mutation | N/A |
| Wiring bug | Nil generator / validator / terms / state, or empty model / provider at call time | Return `domain.ErrValidation`; runner aborts | **Abort** — wiring bug, fix at call site |
| Context cancelled | `ctx.Err()` non-nil before or during the call | Return the ctx error to the runner; no audit emit | **Propagate** — operator stop request |
| Transient LLM error | Generator returns retryable error (e.g., 5xx, timeout) on first attempt | Retry once. If second attempt succeeds, continue; if it also fails, fallback path | Fallback only after retry exhausted |
| LLM error / retry exhausted | Generator returns error on all attempts | `state.Narration` unchanged; `polisher_failed` audit event (no `text_generation`); runner receives `nil` | Fallback, no abort |
| Truncated completion | `finish_reason` indicates truncation | Abort retry, fall back; surface `raise max_tokens` hint in error wrapped by fallback log | Fallback (with operator hint) |
| Schema violation | LLM returns malformed JSON or fails `Validate()` | Fallback; no `text_generation` audit | Fallback, no abort |
| Edit budget violation | Any scene rune-delta strictly > 25% of original narration length | Fallback; no `text_generation` audit | Fallback, no abort |
| Read-only field mutation | `act_id`, `scene_num`, fact-tags, location, or any non-text field on any scene differs from the writer's | Fallback; no `text_generation` audit | Fallback, no abort |
| Forbidden term newly introduced | `MatchNarration(polished) \ MatchNarration(original)` is non-empty | Fallback; no `text_generation` audit | Fallback, no abort |
| Forbidden term inherited | Both `original` and `polished` contain the same hit | Continue; the writer is responsible for its own pre-existing matches | No fallback on this account |

</frozen-after-approval>

## Code Map

- `internal/pipeline/agents/agent.go` -- `PipelineStage` iota enum; add `StagePolisher` between `StageWriter` and `StagePostWriterCritic`; extend `String()` / `DomainStage()` methods
- `internal/domain/narration.go` -- `NarrationScript` / `NarrationScene` types; add `PolisherMaxEditRatio = 0.25` constant here
- `docs/prompts/scenario/03_5_polish.md` -- new polish prompt; mirrors `03_writing.md` structure; inputs: full `NarrationScript` JSON + Lever P continuity rules; three edit tasks named explicitly; edit-budget callout; output: same `NarrationScript` JSON shape
- `internal/pipeline/agents/polisher.go` -- new file (~200 LOC); `NewPolisher(gen domain.TextGenerator, cfg TextAgentConfig, prompts PromptAssets, validator *Validator, terms *ForbiddenTerms) AgentFunc`; reads `state.Narration`, one `runWithRetry` call, diff-budget check per scene, `MatchNarration`, fallback logic, audit log
- `internal/pipeline/agents/polisher_test.go` -- new file; six unit tests covering I/O matrix; table-driven style matching `writer_test.go` patterns; uses a stub generator
- `internal/pipeline/phase_a.go` -- add `polisher AgentFunc` field to `PhaseARunner` struct and as 4th parameter to `NewPhaseARunner`; insert `{agents.StagePolisher, r.polisher}` in chain after `StageWriter`
- `internal/pipeline/phase_a_test.go` -- add `polisher agents.AgentFunc` to `runnerBuilder`; default to `agents.NoopAgent()` in `build()`; fix any `NewPhaseARunner` call sites broken by signature change

## Tasks & Acceptance

**Execution:**
- [ ] `internal/pipeline/agents/agent.go` -- add `StagePolisher` iota value between `StageWriter` and `StagePostWriterCritic`; extend `String()` to return `"polisher"` for the new value and update `DomainStage()` accordingly; downstream iota values shift automatically
- [ ] `internal/domain/narration.go` -- add `const PolisherMaxEditRatio = 0.25`; no other changes to types
- [ ] `docs/prompts/scenario/03_5_polish.md` -- write prompt: `## Inputs` (full NarrationScript JSON + continuity rules from Lever P), `## Your task` (three edit operations: cross-act transition rewrite for first scenes of acts 2/3/4, video-closer rewrite for last scene of act 4, bridge smoothing for within-act adjacent scenes), `## Edit budget` (≤25% rune change per scene), `## Output` (same NarrationScript JSON schema, no new fields)
- [ ] `internal/pipeline/agents/polisher.go` -- implement `NewPolisher`. (1) Wiring preconditions: nil state/gen/validator/terms or empty model/provider → return `domain.ErrValidation` (abort run). (2) `state.Narration == nil` → return `nil` (writer skipped, nothing to polish; no audit, no log noise). (3) `ctx.Err()` non-nil → return ctx error directly. (4) Snapshot scenes and compute `originalForbiddenHits := terms.MatchNarration(state.Narration)`. (5) Marshal `state.Narration` to JSON for prompt. (6) Call `runWithRetry` with `Budget: 1` and stage `"polisher"`; inside the retry callback the generator's transient errors return `retryReasonNetwork`, truncation/ctx-cancel return `retryReasonAbort`, JSON decode failure returns `retryReasonJSONDecode`. **Do not emit any audit entry inside the retry callback.** (7) After retry: if context was cancelled, return ctx error (no fallback). If retry exhausted with non-ctx error, fallback. (8) Run `validator.Validate`, scene-count match, per-scene rune-delta ratio (`> 0.25` → fail), per-scene non-text field invariance (`act_id`, `scene_num`, `location`, fact tags, etc.), and forbidden-terms delta against `originalForbiddenHits`. Any check fails → `polisher_failed` audit event, return `nil` without mutating state. (9) All checks pass → replace `state.Narration`, **then** emit `AuditEventTextGeneration` with `Stage: "polisher"`. This ordering ensures success and fallback are mutually exclusive in the audit ledger.
- [ ] `internal/pipeline/agents/polisher_test.go` -- implement: `TestPolisher_HappyPath` (asserts every scene's narration is updated to the polished response and the full state.Narration round-trips byte-for-byte; not just a single-scene marker), `TestPolisher_SchemaViolation`, `TestPolisher_EditBudgetViolation`, `TestPolisher_ReadOnlyFieldMutation` (act_id swap; verifies non-text invariance gate fires), `TestPolisher_ForbiddenTermsNewlyIntroduced`, `TestPolisher_ForbiddenTermsInherited` (writer already had a hit; polisher inherits and is **not** punished), `TestPolisher_LLMFailFallback` (transient error retried once, then fallback), `TestPolisher_TransientRetryRecovers` (first call fails, second succeeds, no fallback), `TestPolisher_ContextCancelledPropagates` (returns ctx error, not nil), `TestPolisher_DoesNotMutateOnFailure`, `TestPolisher_NoAuditOnFallback` (asserts `text_generation` is NOT emitted when any post-LLM check fails — only `polisher_failed`).
- [ ] `internal/pipeline/agents/agent_test.go` -- extend `TestPipelineStage_String` and `TestPipelineStage_DomainStage` table-driven tests with the `StagePolisher` row so future renames/remappings fail the test (count test alone is insufficient).
- [ ] `internal/pipeline/phase_a.go` -- add `polisher agents.AgentFunc` as 4th positional arg in `NewPhaseARunner` (after `writer`, before `postWriterCritic`); store as `r.polisher`; insert into chain slice between writer and postWriterCritic
- [ ] `internal/pipeline/phase_a_test.go` -- add `polisher agents.AgentFunc` field to `runnerBuilder`; default to `agents.NoopAgent()` in `build()`; update any direct `NewPhaseARunner` invocations to pass the polisher slot

**Acceptance Criteria:**
- Given valid `state.Narration` from writer and a well-behaved LLM response, when polisher runs, then `state.Narration` is replaced and exactly one audit entry with `Stage="polisher"` / `EventType=AuditEventTextGeneration` is emitted (no `polisher_failed`).
- Given any post-LLM check fails (schema, scene-count, per-scene rune budget, non-text-field invariance, newly-introduced forbidden terms), when the runner proceeds, then `state.Narration` is unchanged, exactly one `polisher_failed` audit entry is emitted, **no `text_generation` audit entry is emitted**, and the runner receives `nil`.
- Given the polisher is constructed or invoked with a nil dependency (state, generator, validator, terms) or empty model/provider, when invoked, then it returns `domain.ErrValidation` (the runner aborts) — wiring bugs do NOT silently fall back.
- Given `ctx.Err()` is non-nil at any point — before the LLM call, surfaced through `runWithRetry`, or returned by the generator — when the polisher runs, then it returns the ctx error directly to the runner; no fallback path activates and no `polisher_failed` audit is emitted.
- Given the generator returns a transient (retryable) error on the first attempt and a valid response on the second, when the polisher runs, then the polished script is accepted, no fallback occurs, and exactly one `text_generation` audit entry is emitted (not two).
- Given a polished scene has rune-length delta strictly > 25% of its original narration, when validation runs, then the fallback path activates. A polished scene with delta exactly 25% is accepted.
- Given the polisher swaps `act_id` (or any non-text field) on any scene while keeping schema validity, when invariance check runs, then the fallback path activates.
- Given the writer's pre-polish narration already contains a forbidden term and the polisher does not introduce any **new** ones, when forbidden-terms-delta is evaluated, then the polished script is accepted (the writer's pre-existing matches do not punish the polisher).
- Given the polisher introduces a new forbidden term not present in the writer's output, when forbidden-terms-delta is evaluated, then the fallback path activates.
- `go test ./...` passes; `go test -race ./internal/pipeline/agents/...` clean.

## Design Notes

**Fallback-not-fail contract (runtime).** Polisher is a quality lift, not a correctness gate. The writer output is already valid by construction. Every **runtime** error path (LLM error after retry, schema, budget, forbidden-term-delta, read-only-field mutation) must leave `state.Narration` unchanged, emit `polisher_failed`, and return `nil`. The only observable difference between a runtime fallback and a happy run is the audit log event type — which means audit emit must happen **after** all checks pass, never inside the retry callback.

**Wiring-bug fail-fast (construction-time and call-time preconditions).** Nil dependencies and empty model/provider are wiring bugs, not runtime failures. They must abort the run by returning `domain.ErrValidation` so the operator notices and fixes the call site. A fallback-on-wiring-bug would silently degrade every run forever with no observable signal.

**Context cancellation propagates.** `ctx.Canceled` and `ctx.DeadlineExceeded` are operator stop requests, not LLM failures. Surfacing them through fallback would have the runner happily continue to the next stage after the operator asked the run to stop. The polisher must check `ctx.Err()` directly, recognize cancellation when it surfaces through the generator, and return the ctx error to the runner.

**Rune-length delta as edit budget proxy.** For each scene, compute `abs(len([]rune(polished)) - len([]rune(original))) / len([]rune(original))`. If the ratio is **strictly greater than** `PolisherMaxEditRatio` (i.e., > 0.25), reject the full polished script (fall back entirely — no partial apply). A ratio of exactly 0.25 is accepted. This is cheaper than Levenshtein and sufficient: a smoothing pass that adds or removes more than 25% of characters has clearly overstepped.

**Read-only field invariance.** Schema validation alone is too permissive — a polisher could swap `act_id` values, reorder scenes (same length), or rewrite `fact_tags` and the JSON schema would still pass. The polisher must explicitly verify that every non-text field of every scene survives unchanged, comparing against the snapshot taken before the LLM call. Any divergence triggers fallback.

**Forbidden-term baseline diff.** The writer is responsible for its own forbidden-term matches; the polisher should not be punished for them. The polisher computes `original_hits = MatchNarration(original)` once before the LLM call and `polished_hits = MatchNarration(polished)` after; only the **delta** (terms in `polished_hits` not present in `original_hits`) triggers fallback. Inherited matches pass through, identical to writer behavior.

**Transient retry on generator error.** A single TCP reset or 5xx during the polisher LLM call should not permanently downgrade the run — the polisher's mandate is quality lift, and one retry is the difference between "polisher works most of the time" and "polisher works whenever the network is perfect." `runWithRetry` with `Budget: 1` retries on `retryReasonNetwork`; `retryReasonAbort` is reserved for non-retryable failures (truncation, context cancellation).

**Stage enum shift.** Adding `StagePolisher` before `StagePostWriterCritic` shifts all subsequent iota values. Persisted stage IDs in state files use `String()` (e.g. `"post_writer_critic"`), not raw integers, so existing run state is safe.

**Constructor positional order.** `NewPhaseARunner` uses positional args, not a config struct. Adding `polisher AgentFunc` as the 4th arg (after `writer`) is the minimal-diff change. All call sites — production and test — must be updated in the same commit.

## Verification

**Commands:**
- `go build ./...` -- expected: no compilation errors
- `go test ./internal/pipeline/agents/... -run Polisher -v` -- expected: all six polisher tests pass
- `go test -race ./internal/pipeline/agents/...` -- expected: no race detector findings
- `go test ./internal/pipeline/... -count=1` -- expected: all phase-A tests pass with NoopAgent in polisher slot
- `go test ./...` -- expected: green

## Spec Change Log

### 2026-05-03 — Post-implementation review amendment (step-04 BAD_SPEC loopback)

**Triggering findings (from three-reviewer step-04 audit):**
- `audit-emit-precedes-validation` (Acceptance Auditor + Edge Case Hunter): `text_generation` audit was emitted inside the retry callback, before schema/budget/forbidden-term checks. On any post-LLM failure, the audit ledger contained both `text_generation` and `polisher_failed`, violating the "only observable difference is the event type" guarantee.
- `nil-state-precondition-deviates-from-fallback-not-fail` (Acceptance Auditor): the original "Polisher never aborts a run" wording in the frozen Always block was unconditional, but the implementation correctly aborted on wiring-bug preconditions. The spec was wrong, not the code.
- `edit-budget-comparison-is-strictly-greater-not-greater-equal` (Acceptance Auditor): the Always block said `≤ 0.25` while Design Notes said `≥ 0.25` — internal contradiction. The implementation's `> 0.25` matches the Always semantics.
- `context-cancellation-swallowed-by-fallback` (Blind Hunter): `ctx.Canceled`/`DeadlineExceeded` was being swallowed by the fallback path, so a cancelled run would happily continue to the next stage. Spec did not mention cancellation handling.
- `transient-network-error-no-retry` (Blind Hunter + Edge Case Hunter): generator errors used `retryReasonAbort`, so a single TCP reset permanently downgraded the polisher. Spec mentioned `Budget: 1` but did not specify which errors are retryable.
- `forbidden-terms-no-baseline-diff` (Blind Hunter + Edge Case Hunter): writer-inherited forbidden-term hits were causing unjust polisher fallback even when the polisher had not introduced any new ones. Spec evaluated `MatchNarration(polished)` only.
- `non-text-field-invariance-not-enforced` (Blind Hunter + Edge Case Hunter): JSON schema is too permissive — a polisher could swap `act_id`, reorder scenes, or rewrite `fact_tags` and the schema gate would still pass. Spec said "all other fields are read-only" but did not mandate explicit enforcement.
- `pipelinestage-table-tests-not-extended` (Acceptance Auditor): `TestPipelineStage_String` / `DomainStage` table-driven tests were not extended for `StagePolisher`; only the count test was bumped.
- `happy-path-test-too-narrow` (Blind Hunter): `TestPolisher_HappyPath` only asserted scene[0] ended in `.`, not full equality with the polished response.

**Amended sections (this entry):**
- **Always block (frozen):** split fallback policy into runtime fallback vs wiring-bug fail-fast. Added explicit ctx-cancellation, transient-retry, forbidden-terms-baseline, audit-ordering rules.
- **I/O matrix (frozen):** added rows for wiring bug, context cancelled, transient LLM error (one retry), truncated completion, read-only field mutation, forbidden term inherited (no fallback) vs newly introduced (fallback).
- **Design Notes (non-frozen):** added wiring-bug fail-fast, context-cancellation, read-only field invariance, forbidden-term baseline diff, and transient-retry rationales. Replaced `≥` with strictly-greater wording for budget boundary.
- **Tasks block (non-frozen):** rewrote `polisher.go` task to specify the exact ordering (preconditions → ctx check → snapshot + baseline hits → retry callback **without audit** → post-retry checks → mutate state → **then** audit). Added new task to extend `agent_test.go` table tests.
- **Tests block (non-frozen):** added `TestPolisher_TransientRetryRecovers`, `TestPolisher_ContextCancelledPropagates`, `TestPolisher_ReadOnlyFieldMutation`, `TestPolisher_ForbiddenTermsInherited`, `TestPolisher_NoAuditOnFallback`. Strengthened `TestPolisher_HappyPath` requirements.
- **Acceptance Criteria (non-frozen):** rewrote ACs to cover all the above scenarios explicitly with Given/When/Then.

**Known-bad state avoided by this amendment:** A polisher that (a) leaves a paired-event audit ledger that conflates fallback with success, (b) ignores `ctx` cancel, (c) permanently downgrades on a single network blip, (d) blames itself for the writer's pre-existing forbidden-term hits, (e) silently rewrites `act_id`/scene-order metadata, and (f) is checked only by a single-scene marker in tests.

**KEEP instructions for code re-derivation (from current implementation):**
- Keep `polisherRetryBudget = 1` constant and the use of `runWithRetry` with `Stage: "polisher"`.
- Keep `polisherFallback(ctx, cfg, state, cause)` helper that emits `AuditEventPolisherFailed` and warn-logs.
- Keep `checkPolisherEditBudget(original, polished []NarrationScene) error` signature; extend it to also check non-text fields, OR add a sibling `checkPolisherReadOnlyInvariance` and call both.
- Keep `renderPolisherPrompt` shape with `{scp_id}` and `{narration_script_json}` placeholders and `MarshalIndent` of `state.Narration`.
- Keep `originalScenes := make([]domain.NarrationScene, len(state.Narration.Scenes)); copy(originalScenes, state.Narration.Scenes)` snapshot.
- Keep the production wiring in `cmd/pipeline/serve.go`: `polisherCfg` reuses writer's model/provider with `MaxTokens: 12288`, `Temperature: 0.5`. Pass `polisher` as 4th arg to `NewPhaseARunner`.
- Keep all the existing test infrastructure (`polisherQueueGen`, `spyAuditLogger`, `samplePolisherState`, `polishedScriptJSON`, `budgetBustingScriptJSON`, `forbiddenScriptJSON`); extend rather than replace.
- Keep `StagePolisher` enum value at iota position between `StageWriter` and `StagePostWriterCritic`, mapping to `domain.StageWrite`.
- Keep the `OnSubStageStart` skip for `StagePolisher` (it is a write-phase sub-step and surfacing it would jump the UI off "write" and back).

**Re-implementation scope:** Apply targeted edits to `polisher.go`, `polisher_test.go`, and `agent_test.go`; do not regenerate the whole file. The structural wiring (constructor, prompt, snapshot, audit helper) stays.
