---
title: 'Writer agent per-act fan-out (parallel LLM calls)'
type: 'refactor'
created: '2026-04-26'
status: 'in-review'
baseline_commit: 'f35582ae847378582c0a805f428d4ad204e00fa9'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/3-3-writer-agent-critic-post-writer-checkpoint.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Writer agent emits the entire 8–10-scene NarrationScript in a single LLM call, which hits the deepseek-reasoner 8192-token output cap (truncated, validation fails), forces full-script regeneration on any single-scene defect, and runs ~100s wall-clock per attempt.

**Approach:** Fan out the writer into 4 per-act LLM calls (one per `domain.Act`). Run Act 1 (incident) serially first to lock the hook tone, then run Acts 2–4 (mystery / revelation / unresolved) in parallel via `errgroup` with a per-act retry budget, then merge the results into one `NarrationScript` and run a single forbidden-term scan + validator pass on the whole script. Public `NewWriter` signature, `NarrationScript` shape, and `scenario.json` schema stay unchanged.

## Boundaries & Constraints

**Always:**
- `AgentFunc` signature unchanged; `NewWriter(gen, cfg, prompts, validator, terms) AgentFunc`.
- `domain.NarrationScript` / `domain.NarrationScene` schemas unchanged. The merged script must still pass `writer_output.schema.json`.
- ActIDs in code use the actual domain constants (`incident`, `mystery`, `revelation`, `unresolved` — `domain.ActOrder`). The story prompt's "hook/build/climax/resolution" labels are descriptive only.
- Act 1 is awaited before Acts 2–4 launch; only Acts 2–4 are concurrent.
- Per-act response is rejected (and that act alone retried) if it returns the wrong scene count, scene_nums outside the assigned range, or `act_id` not matching the requested act.
- Concurrency for Acts 2–4 is bounded by `cfg.Concurrency` (or a sensible default if zero) so DashScope's 2-concurrent ceiling is not exceeded.
- After successful merge: `script.Scenes` sorted by `scene_num` 1..N contiguous; `Metadata` filled once from the first successful response (model/provider); single forbidden-term scan over the whole merged script.
- All existing writer behaviors that survive — code-fence stripping, ErrValidation propagation on JSON/schema/forbidden-term failures, no state mutation on failure, audit log emission per LLM call — continue to hold.

**Ask First:**
- Adding a new field to `TextAgentConfig` beyond `Concurrency` (e.g. per-act retry budget). Default: keep `Concurrency` only; reuse the existing single retry-by-caller pattern.
- Splitting `03_writing.md` into multiple prompt files vs. a single template with new placeholders. Default: single template, new placeholders (`{act_id}`, `{scene_num_range}`, `{prior_act_summary}`, `{act_synopsis}`, `{act_key_points}`, `{scene_budget}`).
- Wiring `state.PriorCriticFeedback` (does not exist today). Default: keep `{quality_feedback}` empty for now, leave the placeholder in the per-act template so a follow-up can wire it without re-shaping prompts.

**Never:**
- Per-act fan-out for `critic`, `reviewer`, or `visual_breakdowner` (out of scope).
- Touching `structurer` (still deterministic).
- Changing `target_scene_count` (stays 10 hardcoded).
- Spawning more than `cfg.Concurrency` in-flight calls for Acts 2–4.
- Persisting per-act intermediate artifacts to disk (only the merged script, as today).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Happy 4-act merge | `state.Structure.Acts` with `SceneBudget=[2,3,4,1]`; each act response returns `scenes` matching its scene_num range | Merged `state.Narration` with 10 scenes ordered 1..10, ActIDs matching structure, `Metadata.SceneCount=10` | N/A |
| Act 2 first call schema-violates, retry succeeds | Act 2 first response has `len(scenes) != 3` or scene_num out of range | Act 2 retried once; Acts 1/3/4 each called exactly once; final merge OK | Retry budget = 1 per act; if budget exhausted return ErrValidation |
| Act 1 fails (post-retry) | Act 1 response invalid after retry budget exhausted | Acts 2–4 not launched (or canceled if already in-flight via shared ctx); writer returns ErrValidation; `state.Narration` unchanged | Wrap as `writer: <error>: ErrValidation` |
| Forbidden term in merged script | Any act response contains a forbidden term | Writer returns ErrValidation after merge; `state.Narration` unchanged | `formatForbiddenTermHits` over the whole merged script (single scan) |
| `cfg.Concurrency=0` | unset config | Use default (Acts 2–4 sequential or default-2 — implementer chooses, must be ≤ 3) | N/A |
| Context canceled mid-fan-out | Caller cancels ctx during Act 2/3/4 errgroup | All in-flight goroutines respect ctx; first context error returned; no partial state mutation | Return ctx.Err() unwrapped (existing behavior) |

</frozen-after-approval>

## Code Map

- `internal/pipeline/agents/writer.go` -- replace single-call body with per-act fan-out; add `Concurrency` to `TextAgentConfig`; introduce `writerActResponse` struct, `actCallSpec` builder, per-act validator, merge logic.
- `internal/pipeline/agents/writer_test.go` -- keep existing semantics (Happy / StripsCodeFence / NilStructure / SchemaViolation / ForbiddenTerms / MetadataFilled / DoesNotMutateOnFailure) but route fixture through 4 per-act fake responses; introduce `actIndexedTextGenerator` helper.
- `docs/prompts/scenario/03_writing.md` -- restructure as a per-act prompt with placeholders `{act_id}`, `{scene_num_range}`, `{prior_act_summary}`, `{act_synopsis}`, `{act_key_points}`, `{scene_budget}`; collapse hook-specific guidance under a conditional `{prior_act_summary}` block (empty → Act 1 hook rules apply).
- `internal/pipeline/agents/visual_breakdowner.go` -- read-only reference for sequential gen.Generate pattern (not modified).
- `internal/pipeline/agents/structurer.go` -- read-only reference for `domain.ActOrder` and SceneBudget distribution (not modified).
- `internal/pipeline/phase_a_integration_test.go` -- only the post-reviewer test path uses the writer fixture as a static narration; not affected by per-act path because it injects narration directly.
- `internal/pipeline/phase_a_test.go` -- writer slot uses `agents.NoopAgent()`; unaffected.
- `testdata/contracts/writer_output.sample.json` -- merged-script fixture stays valid (no schema change); used as-is plus split into 4 per-act response fixtures inline in tests.

## Tasks & Acceptance

**Execution:**
- [x] `internal/pipeline/agents/writer.go` -- add `Concurrency int` field to `TextAgentConfig`; refactor `NewWriter` body into Act 1 serial → Acts 2–4 parallel (`errgroup` with `SetLimit(min(cfg.Concurrency, 3))`); compute per-act `[lo, hi]` scene_num range from `state.Structure.Acts[].SceneBudget`; validate each per-act response (act_id matches, scene count == budget, scene_nums in range, sorted); merge scenes in scene_num order; `fillNarrationMetadata` once from the first response; run `terms.MatchNarration` once on the merged script; preserve no-state-mutation-on-failure.
- [x] `internal/pipeline/agents/writer.go` -- add `renderWriterPromptForAct(state, prompts, terms, act, sceneRange, priorSummary)` and a `summarizePriorAct(prevAct domain.NarrationScript, acts []domain.NarrationScene)` that returns the last 1–2 scene narrations as a continuity blurb.
- [x] `internal/pipeline/agents/writer.go` -- per-act response struct `writerActResponse { ActID string; Scenes []domain.NarrationScene }`; per-act retry budget = 1 (single retry on schema violation), so failure-budget parity with the prior single-call writer (which had no retry) plus a small headroom; act-call cancellation propagates via shared ctx.
- [x] `docs/prompts/scenario/03_writing.md` -- restructure as per-act template. Keep tone/voice/sensory rules (now applied per act). Add placeholders enumerated in Code Map. Add a "Prior Act Summary" block: when empty, instructs the LLM "this is Act 1 — open with a 5-second hook, no SCP-XXX class intro"; when non-empty, instructs "continue the tone established here, do not re-introduce the entity." Update the Task block to require `{"act_id": "...", "scenes": [...]}` not the full `NarrationScript`.
- [x] `internal/pipeline/agents/writer_test.go` -- introduce `actIndexedTextGenerator` (routes by act_id placeholder appearing in prompt → returns the right per-act fixture); update existing tests so the same writer_output.sample.json is split into 4 per-act responses on the fly.
- [x] `internal/pipeline/agents/writer_test.go` -- add `TestWriter_PerAct_Happy_Merges10Scenes_InOrder`: 4 per-act fixtures with budgets [2,3,4,1], assert `state.Narration.Scenes` has scene_num 1..10 in order and ActIDs match structure.
- [x] `internal/pipeline/agents/writer_test.go` -- add `TestWriter_PerAct_RetriesOnlyFailedAct`: Act 2 first response returns 2 scenes (budget 3) → retry returns 3; assert per-act call counts = [Act1=1, Act2=2, Act3=1, Act4=1].
- [x] `internal/pipeline/agents/writer_test.go` -- add `TestWriter_PerAct_PriorActSummaryInjected`: capture Act 2 prompt, assert it contains a substring of Act 1's last scene narration.
- [x] `internal/pipeline/agents/writer_test.go` -- add `TestWriter_PerAct_Concurrency`: `cfg.Concurrency=2`, generator increments an `int32` on entry, sleeps 20ms, decrements on exit, asserts max observed in-flight ≤ 2 across the run.
- [x] `internal/pipeline/agents/writer_test.go` -- existing tests (Happy / StripsCodeFence / SchemaViolation / ForbiddenTerms / MetadataFilled / DoesNotMutateOnFailure / InvalidJSON) updated to use the per-act fake generator. Semantics preserved.

**Acceptance Criteria:**
- Given a `state.Structure` with 4 acts and SceneBudget=[2,3,4,1], when `NewWriter` runs and every act returns a valid per-act response, then `state.Narration.Scenes` has length 10, scene_nums are 1..10 in ascending order, and each scene's `ActID` matches its act's `ID`.
- Given Act 2's first response is schema-invalid, when the writer runs, then Act 2 is retried exactly once and Acts 1/3/4 are called exactly once each.
- Given `cfg.Concurrency = 2`, when Acts 2/3/4 fan out, then no more than 2 act calls are in-flight simultaneously (verified with an atomic counter inside the fake generator).
- Given a forbidden term appears in any act's narration, when the writer runs, then it returns `domain.ErrValidation` with the forbidden-term hit message and `state.Narration` is not mutated.
- Given Act 1 fails after retry, when the writer runs, then `state.Narration` is unchanged and the writer returns `domain.ErrValidation` (Acts 2–4 either not launched or short-circuited via ctx).
- Given the merged script, when `validator.Validate` runs, then it passes the existing `writer_output.schema.json` (10 scenes, all required fields).
- Given any state file change, `go build ./...` succeeds and `go test ./internal/pipeline/agents/... ./internal/pipeline/...` is green.

## Design Notes

**Scene-num ranges.** Walk `state.Structure.Acts` in order, running offset = 1, `[lo,hi] = [offset, offset+SceneBudget-1]`; offset += SceneBudget. ActOrder = [incident,mystery,revelation,unresolved] + budgets [2,3,4,1] → [1,2]/[3,5]/[6,9]/[10,10].

**Prior-act summary.** Single ~3-line string built by `summarizePriorAct` from merged Act 1: last narration line + last scene mood + "do not re-introduce the entity." Acts 2/3/4 all receive the SAME Act-1-derived summary; cascading (Act 2 → 3 → 4) would re-serialize and erase the wall-clock win.

**Per-act response schema.** `{ "act_id": "incident", "scenes": [<NarrationScene>, ...] }`. Writer rejects on act_id mismatch, scene count != `SceneBudget`, or scene_num outside `[lo, hi]`.

**Concurrency default.** `cfg.Concurrency <= 0` → 2 (DashScope's hard floor). Hard ceiling `min(cfg.Concurrency, 3)` for Acts 2–4 (only 3 acts).

**Metadata.** `fillNarrationMetadata` runs once on merge using the FIRST per-act response's Model/Provider. Drift across acts is silently tolerated (matches existing behavior; audit log captures per-call detail).

## Verification

**Commands:**
- `go build ./...` -- expected: success.
- `go test ./internal/pipeline/agents/...` -- expected: all writer tests green, including 4 new `TestWriter_PerAct_*` cases.
- `go test ./internal/pipeline/...` -- expected: phase_a / phase_a_integration tests still green (writer slot is NoopAgent there; integration test uses the merged-script fixture directly).
- `go vet ./...` -- expected: clean.

**Manual checks:**
- Open `docs/prompts/scenario/03_writing.md` and verify it now reads as a per-act prompt: a single `{act_id}` template, with conditional Act-1 hook block when `{prior_act_summary}` is empty.
