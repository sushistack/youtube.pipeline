---
title: 'visual_breakdowner follow-ups: narration↔shot alignment + hardening sweep'
type: 'feature'
created: '2026-05-03'
status: 'done'
baseline_commit: '878e6e4'
context:
  - _bmad-output/planning-artifacts/next-session-visual-breakdown-followups.md
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The visual_breakdowner stage shipped per-scene retry + tightened transition contract (commit `f8ba5b0`), but two gaps remain. (1) Goal 2 — narration↔shot alignment — is unfulfilled: shot count is duration-derived via `ShotCountForDuration`, and `EnsureFrozenPrefix` hard-prepends the frozen descriptor verbatim, producing repetitive shots within a scene. (2) The retry loop is duplicated across `writer.go` and `visual_breakdowner.go` with no shared abstraction, no observability beyond stderr grep, no negative-budget guard, scene-1-only test coverage, and a pre-existing race in `fakeTextGenerator`.

**Approach:** Bundle five items into one cycle, ordered by dependency. (i) Add `sync.Mutex` to `fakeTextGenerator`. (ii) Extract a generic `runWithRetry[T]` helper with structured slog observability and a negative-budget defensive guard; rewire writer + visual_breakdowner onto it. (iii) Add multi-scene retry tests (scene N>1, concurrent failures) + negative-budget tests. (iv) Refactor visual_breakdowner so shot count equals narration-beat count: writer emits `narration_beats: []string` per scene; visual_breakdowner emits `narration_beat_index` per shot; `EnsureFrozenPrefix` becomes a soft warn-and-fix validator while the prompt rule shifts from "begin verbatim" to "preserve visual identity, vary focal subject per beat". `ComposeImagePrompt` (image_track.go:125-147) already defensively prepends frozen — no image-gen change needed.

## Boundaries & Constraints

**Always:**
- One LLM call per scene to visual_breakdowner. No new LLM calls.
- Shot count == `len(scene.NarrationBeats)`. Min beats per scene = 1.
- Per-beat duration uniform within scene (first-cut). Total scene duration unchanged.
- Shot ordering matches beat ordering. Each shot echoes `narration_beat_index` ∈ [0, len-1].
- `BuildFrozenDescriptor` stays intact — single source of truth for visual identity.
- `runWithRetry` clamps budget pre-loop: `budget < 0` returns explicit error rather than zero-value silent success.
- Retry observability via structured slog fields: `stage`, `attempt`, `outcome` ∈ {`success_first_try`, `retry_succeeded`, `retry_exhausted`}, plus existing per-stage fields (`run_id` + `scene_num`/`act_id`, `reason`, `error`).
- `retry_exhausted` log level = Warn. `success_first_try` and `retry_succeeded` = Info. nil-Logger guard preserved.
- `fakeTextGenerator` reads from test goroutine remain valid (post-`Run` only). Mutex internal; expose accessors only if existing call sites race.

**Ask First:**
- Any contract cascade beyond writer + visual_breakdown (critic / structurer / researcher schema bumps for `narration_beats`). Default: skip — beats live on writer output only.
- Surfacing retry counters on `MetricsReport` or a new run-summary structure (Item 2 stretch). Default: slog-only is the minimum bar; surface counter is optional and gated.
- If `runWithRetry` cannot be expressed cleanly with Go generics for both stages' return types, fall back to a per-stage closure pattern — confirm before committing.

**Never:**
- Touching reviewer policy (current dogfood blocker — separate concern).
- Critic 어미-pattern check (Phase D, queued elsewhere).
- Adding fewshot exemplars (Phase 2, queued elsewhere).
- Image-gen prompt assembly — `ComposeImagePrompt` already defensively prepends; nothing to change there.
- TTS pacing / audio alignment.
- Per-beat duration weighting beyond uniform split.
- Backwards-compat shims for the old shot-count path — replace, don't dual-emit.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Single-beat scene (incident hook) | NarrationScene with 1 beat | 1 shot, `narration_beat_index=0`, visual identity preserved | N/A |
| Multi-beat scene | NarrationScene with 4 beats | 4 shots in beat order, indices 0..3, uniform duration split, varied focal subjects | N/A |
| Beat count mismatch in LLM response | LLM returns 3 shots for 4-beat scene | retry once via `runWithRetry`; on second mismatch return validation error | logged `outcome=retry_exhausted` (Warn) |
| Frozen-prefix soft miss | Shot `visual_descriptor` lacks frozen-identity continuity | `EnsureFrozenPrefix` warn-fixes: log Warn, prepend, accept | logged, not fatal |
| Retry on scene N>1 | scene 3 fails first attempt, valid on retry; scenes 1/2/4 pass first | `callsByScene[3]==2`; others `==1`; final ordering preserved 1,2,3,4 | N/A |
| Concurrent multi-scene retry | scenes 1 AND 3 both fail-then-recover | both succeed; final ordering preserved 1,2,3,4 | N/A |
| Negative retry budget | budget = -1 (config-driven seam) | `runWithRetry` returns `"retry budget invalid: -1"` error; no silent zero-value | explicit error |
| Transport error | LLM call returns network/transport error | propagate immediately, no retry | per existing behavior |

</frozen-after-approval>

## Code Map

- `internal/pipeline/agents/visual_breakdowner.go:27,137-219,280` -- retry loop + `EnsureFrozenPrefix` call site; replace `ShotCountForDuration` with `len(scene.NarrationBeats)`; require + propagate `narration_beat_index`.
- `internal/pipeline/agents/visual_breakdown_helpers.go:33-49,73-96` -- `ShotCountForDuration` removed if unused after refactor; `EnsureFrozenPrefix` repurposed as soft validator; `BuildFrozenDescriptor` untouched.
- `internal/pipeline/agents/writer.go:63,234-334` -- per-act retry loop swapped to `runWithRetry`; emit `narration_beats` per scene from LLM response.
- `internal/pipeline/agents/retry.go` (NEW) -- generic `runWithRetry[T]` helper: budget clamp, slog observability, retry outcome classification.
- `internal/pipeline/agents/visual_breakdowner_test.go:239-330,429-490` -- new retry tests (scene N>1, concurrent, negative-budget); update existing tests for beat-driven shot count.
- `internal/pipeline/agents/writer_test.go:577-584,137,160-165,402,517` -- add `sync.Mutex` to `fakeTextGenerator`; introduce accessor methods if direct reads race; new negative-budget test.
- `internal/domain/visual_breakdown.go:31-36` -- `VisualShot` gains `NarrationBeatIndex int`, `NarrationBeatText string`.
- `internal/domain/narration.go:16-27` -- `NarrationScene` gains `NarrationBeats []string`.
- `testdata/contracts/visual_breakdown.{schema,sample}.json` -- shot schema requires `narration_beat_index`; sample rebuilt with multi-beat scene.
- `testdata/contracts/writer_output.{schema,sample}.json` -- scene schema requires `narration_beats`; sample fixture adds beats arrays.
- `docs/prompts/scenario/03_5_visual_breakdown.md:48-56` -- replace "begin with `{frozen_descriptor}` verbatim" rule; add `narration_beat_index` echo requirement; drop duration-driven shot-count instruction.
- `internal/pipeline/image_track.go:125-147,272` -- VERIFY ONLY: `ComposeImagePrompt` already defensively prepends frozen; no change.

## Tasks & Acceptance

**Execution (ordered by dependency — sequence matters to avoid mid-flight rebases):**

- [x] `internal/pipeline/agents/writer_test.go` -- add `sync.Mutex` to `fakeTextGenerator`; gate `calls`, `last`, `reqs` writes under lock; introduce `Calls()`, `Last()`, `Reqs()` accessors only if direct read sites race under `-race`; rerun `go test -race`.
- [x] `internal/pipeline/agents/retry.go` -- new file; `runWithRetry[T any](ctx, opts, fn) (T, error)` where `opts{Stage, Budget, Logger}` and `fn` returns `(T, retryReason, error)` with `retryReason` ∈ {empty (success), `json_decode`, `schema_validation`, ...}. Pre-loop: `if opts.Budget < 0 { return zero, fmt.Errorf("retry budget invalid: %d", opts.Budget) }`. Emit slog with `outcome` field and `nil`-Logger guard.
- [x] `internal/pipeline/agents/writer.go` -- swap inline retry loop for `runWithRetry`. Preserve all existing slog fields (`run_id`, `act_id`, `reason`, `error`, plus `tokens_in`/`tokens_out`/`finish_reason`/`prompt_chars`/`has_quality_feedback`). Emit `narration_beats` from LLM response into `NarrationScene.NarrationBeats`.
- [x] `internal/domain/narration.go` -- add `NarrationBeats []string` to `NarrationScene`.
- [x] `internal/domain/visual_breakdown.go` -- add `NarrationBeatIndex int` and `NarrationBeatText string` to `VisualShot`.
- [x] `testdata/contracts/writer_output.schema.json` + `.sample.json` -- schema bump (require `narration_beats: array<string>` per scene, `minItems: 1`); rebuild sample.
- [x] `internal/pipeline/agents/visual_breakdowner.go` -- swap inline retry loop for `runWithRetry`. Replace `ShotCountForDuration(seconds)` with `len(scene.NarrationBeats)`; require `len ≥ 1`. Validate per-shot `narration_beat_index` ∈ [0, len-1] and ordering matches. Move `EnsureFrozenPrefix` from hard-prepend to warn-fix validator (Warn slog on miss, then prepend).
- [x] `internal/pipeline/agents/visual_breakdown_helpers.go` -- repurpose `EnsureFrozenPrefix` (no signature change; doc the soft semantics). Remove `ShotCountForDuration` if all call sites are gone (otherwise mark internal).
- [x] `testdata/contracts/visual_breakdown.schema.json` + `.sample.json` -- schema bump (require `narration_beat_index: integer ≥ 0`); rebuild sample with realistic multi-beat scene.
- [x] `docs/prompts/scenario/03_5_visual_breakdown.md` -- rewrite Rules section (line ~52): drop "begin with `{frozen_descriptor}` verbatim"; add "preserve visual-identity continuity, shift focal subject per beat (entity / environment / character POV / artifact close-up)"; require `narration_beat_index` echo; remove duration-driven shot-count instruction.
- [x] `internal/pipeline/agents/visual_breakdowner_test.go` -- add `TestVisualBreakdowner_Run_RetriesOnSceneN_WhenNGreaterThanOne`, `TestVisualBreakdowner_Run_HandlesMultipleConcurrentRetries`, `TestVisualBreakdowner_Run_NegativeBudget_DoesNotSilentlySucceed`; update existing 4 retry tests for beat-driven shot count.
- [x] `internal/pipeline/agents/writer_test.go` -- add `TestWriter_Run_NegativeBudget_DoesNotSilentlySucceed`; verify `narration_beats` propagation tests.

**Acceptance Criteria:**

- Given a writer-emitted scene with N narration beats, when visual_breakdowner runs, then exactly N shots are produced with sequential `narration_beat_index` 0..N-1.
- Given the inline `for attempt := 0; attempt <= budget; attempt++` loop in writer.go and visual_breakdowner.go before this cycle, when the cycle completes, then both call `runWithRetry` and `grep -RIn "for attempt := 0; attempt <=" internal/pipeline/agents/{writer,visual_breakdowner}.go` returns no matches.
- Given retry budget = -1, when `runWithRetry` is invoked, then it returns `"retry budget invalid: -1"` rather than a zero-value success.
- Given scene 3 fails first attempt and recovers on retry while scenes 1,2,4 succeed first try, when the run completes, then `callsByScene[3]==2`, others `==1`, final scene ordering 1,2,3,4 preserved.
- Given two scenes simultaneously fail-then-recover, when the run completes, then both succeed and ordering is preserved.
- Given `go test -race ./internal/pipeline/agents/...`, when run on the cycle's HEAD, then no race is reported in `fakeTextGenerator`.
- Given a slog scraper aggregating retry events, when a dogfood run executes, then `outcome=success_first_try|retry_succeeded|retry_exhausted` counts are visible without grep-by-string.
- Given the post-cycle dogfood (HITL spot-check on visual variety), when an operator reviews shots within multi-beat scenes, then focal subjects vary per beat while visual identity remains cohesive across the video. (Gated on reviewer-policy unblock; record observation in dogfood log only.)

## Spec Change Log

**2026-05-03 dogfood patch — schema cap + soft-validator behavior.**

- **Finding** (real run, scp-049-run-1): scene 17 emitted 6 narration beats → 6 shots → schema rejected because `visual_breakdown.schema.json` still capped `shot_count.maximum=5` and `shots.maxItems=5` (relics of the old `ShotCountForDuration` heuristic). Run failed at visual_breakdowner. Separately, the soft `EnsureFrozenPrefix` validator fired Warn + prepend on virtually every shot because the new prompt explicitly invites focal-subject variation per beat — meaning the prepend was overwriting the model's intended variation, contradicting Goal 2.
- **Amended**:
  1. Schema caps raised from 5 → 12 in three places (`shot_count.maximum`, `shots.maxItems`, and `shot_overrides.shot_count.maximum`). Cap chosen to give 50% headroom over realistic worst-case beat counts; not removed entirely so a runaway LLM still fails fast.
  2. `softEnsureFrozenPrefix` removed. Descriptors now flow through the agent verbatim (`strings.TrimSpace` only). `EnsureFrozenPrefix` and its unit test deleted as dead code (Jay's no-dead-layers rule). Visual identity is anchored downstream by `image_track.ComposeImagePrompt` (image_track.go:125-147), which already defensively prepends frozen — that remains the single safety net.
  3. `TestVisualBreakdowner_Run_PrefixesFrozenDescriptor_EveryShot` renamed to `TestVisualBreakdowner_Run_PassesDescriptorsThroughVerbatim` and inverted: now asserts the agent does NOT prepend.
- **Avoids**: scenes with >5 beats failing schema validation in production runs; soft-validator overwriting the new prompt's focal-subject variation; Warn-log spam (one per shot, normal-operation).
- **KEEP**: the I/O matrix row "Frozen-prefix soft miss → log Warn, prepend, accept" was correct under the OLD prompt assumption that frozen-prefix would be the norm. With the new prompt where focal-subject variation IS the norm, the prepend conflicts with model intent. Future readers: do not reintroduce agent-side prepend without also rolling back the prompt rule. ComposeImagePrompt remains authoritative for visual-identity anchoring.

## Design Notes

**Why bundle.** Item 2 (extract helper) makes Item 4's negative-budget guard land in one place. Item 3 leverages `queueTextGenerator` / `sceneErrorGenerator` / `callsByScene` that already exist (visual_breakdowner_test.go:429-490) — no new test-helper code. Item 5 unblocks `go test -race` cleanly. Item 1 is orthogonal but touches the same agent file; sequencing it last lets the helper extraction settle first and avoids two rounds of test churn.

**Open question (a) vs (b) resolution.** Choose **(a)** — writer emits `narration_beats: []string`. Reasons: (i) Korean sentence-boundary heuristics are language-coupled and would lock visual_breakdowner to a specific NLP path. (ii) Schema-anchored beats are debuggable in `audit.log`; heuristic splits are not. (iii) Critic / structurer schemas don't need bumping unless they consume beats — verify and skip if not.

**Helper signature sketch:**

```go
type retryOpts struct {
    Stage    string         // "writer" | "visual_breakdowner"
    Budget   int            // attempts beyond first; clamp <0 → error
    Logger   *slog.Logger   // nil-safe
    BaseAttrs []slog.Attr   // run_id, act_id|scene_num, ...
}
type retryReason string
// fn returns (result, "" on success | reason on retryable failure, err)
func runWithRetry[T any](ctx context.Context, opts retryOpts,
    fn func(attempt int) (T, retryReason, error)) (T, error)
```

**Image-gen verification (no change).** `ComposeImagePrompt(frozen, visual)` at `image_track.go:125-147` already prepends frozen if `visual` doesn't start with it. Soft frozen prefix on visual_breakdowner side is therefore safe — image-gen continues anchoring visual identity at the prompt-assembly boundary.

## Verification

**Commands:**

- `go test ./...` -- expected: pass.
- `go test -race ./internal/pipeline/agents/...` -- expected: pass without race report on `fakeTextGenerator`.
- `go vet ./...` -- expected: clean.
- `grep -RIn "for attempt := 0; attempt <=" internal/pipeline/agents/writer.go internal/pipeline/agents/visual_breakdowner.go` -- expected: no matches.
- `grep -RIn "narration_beats" testdata/contracts/writer_output.schema.json testdata/contracts/writer_output.sample.json internal/domain/narration.go` -- expected: matches in all three.
- `grep -RIn "narration_beat_index" testdata/contracts/visual_breakdown.schema.json testdata/contracts/visual_breakdown.sample.json internal/domain/visual_breakdown.go` -- expected: matches in all three.

**Manual checks:**

- Inspect a clean dogfood run's `audit.log` after the cycle lands — confirm `outcome=` field appears on `stage=visual_breakdowner` and `stage=writer` audit entries.
- HITL visual-variety spot-check: open the post-cycle run's first 3 multi-beat scenes; eyeball whether shots within a scene show different focal subjects (entity / environment / artifact close-up / character POV) while visual identity remains cohesive across the video. (Gated on reviewer-policy unblock — the current dogfood blocker.)
