# Next session — Visual breakdowner follow-ups (Goal 2 + hardening sweep)

Run with `/bmad-quick-dev`. This intent bundles **five items** queued from the
post-implementation review of `spec-visual-breakdowner-stabilize.md` (commit
`f8ba5b0`). They split naturally into:

- **Item 1 (strategic refactor)** — Goal 2: narration ↔ shot alignment.
- **Items 2-5 (small hardening sweep)** — observability, test breadth,
  defensive guard, pre-existing race.

**Plan step recommendation**: SPLIT into two specs. Item 1 is a multi-file
refactor with schema/contract decisions. Items 2-5 are small, low-coupling,
and could ship as one bundled hardening spec OR be cherry-picked. Bundling
all five into one spec will exceed the 1600-token target.

If the user chooses to ship only one of the two groups, recommended order:
**Items 2-5 first** (cheap, observability for the next dogfood), then
Item 1 once HITL spot-check confirms visual variety is still inadequate.

---

## Current state on main (read first)

Most recent commit: `f8ba5b0 fix(visual_breakdowner): per-scene retry budget +
tighten transition contract`. This is the starting state — do not assume
unmerged work.

Dogfood result on SCP-049 (2026-05-03 17:32, 17 scenes):

- **Retry budget consumed = 0**. All 17 scenes passed validation on the first
  LLM call. `audit.log` shows exactly 17 entries for `stage=visual_breakdowner`.
- **Prompt tightening was sufficient on its own.** The retry loop is now
  defense-in-depth, not the active mitigation.
- Pipeline now blocks at the **next stage (reviewer)**: 3 non-critical issues
  cause `overall_pass=false → review_failed`. That is reviewer-policy
  territory, NOT a regression of this cycle's fix. Out of scope here.

Open files / artifacts of interest:

- `internal/pipeline/agents/visual_breakdowner.go` — `runVisualBreakdownerScene`
  helper (lines ~119-174) holds the retry loop. `visualBreakdownerPerSceneRetryBudget`
  const at line 27.
- `internal/pipeline/agents/visual_breakdowner_test.go` — test-only generators
  `queueTextGenerator` (~466) and `sceneErrorGenerator` (~432). Existing 4
  retry tests at lines 239-330.
- `internal/pipeline/agents/writer.go:234-334` — sibling retry pattern that
  visual_breakdowner mirrors. Whatever observability/guard work happens here
  should consider whether to lift to a shared helper.
- `internal/pipeline/agents/writer_test.go:587` — `fakeTextGenerator`,
  pre-existing race carrier.

---

## Item 1 — Goal 2: narration ↔ shot alignment (strategic refactor)

The original intent of the prior cycle, descoped via the multi-goal split.

### Outcome target

1. **1:1 narration ↔ shot mapping**: shot count is no longer derived from
   `ShotCountForDuration(seconds)`. It equals the number of narration beats
   in the scene. Each shot consumes exactly one beat as its primary content.
2. **Frozen prefix becomes a constraint, not a verbatim concatenation**:
   `visual_descriptor` must preserve visual-identity continuity across shots,
   but the agent can shift focal subject (entity / environment / character POV
   / artifact close-up) per beat. No more "every shot starts with the same
   200-character entity description."

### Decisions locked from prior planning

- **No new LLM calls per scene.** Still one call per scene to the visual
  breakdowner.
- **Per-shot narration anchor**: each shot in the response must echo back
  which beat/sentence it is rendering (`narration_beat_index: int`). Shot
  ordering must match beat ordering.
- **Soft frozen prefix**: `EnsureFrozenPrefix` becomes a soft validator
  (warn-and-fix) instead of a hard prepender. `BuildFrozenDescriptor`
  stays intact (still the source of truth for visual identity).

### Open questions for the plan step

These were deferred from the prior cycle and must be settled here:

- **(a) writer adds `narration_beats: []string` field, or (b)
  visual_breakdowner splits narration internally?** (a) is cleaner long-term
  (writer + critic + structurer schema bumps). (b) is faster (no schema
  bump, sentence-boundary heuristic in agent code).
- **Min beats per scene.** Recommendation: 1 — `incident` hooks should be
  visually monolithic ("one striking image"). Confirm at plan time.
- **Per-beat duration weighting.** Today shot durations are uniform within
  a scene. When beats are uneven (e.g. one beat = "the scientist screams" /
  next beat = "8s of silence"), uniform split is wrong. First-cut: keep
  uniform; estimate later.
- **Image-gen downstream defensive prepend.** Story
  `_bmad-output/implementation-artifacts/5-4-frozen-descriptor-propagation-per-shot-image-generation.md`
  documents how `visual_descriptor` flows into image generation. If frozen
  prefix becomes soft, the image-prompt assembly may need a defensive
  prepend so visual identity is anchored at image-gen time. Verify before
  relaxing.

### Trigger / urgency

The dequeue precondition was "after Goal 1 ships and dogfood completes
visual_breakdowner end-to-end, run a HITL spot-check: are shots within a
scene visually varied or repetitive?" Goal 1 has shipped and the stage
runs end-to-end, BUT the dogfood is now blocked downstream by the
reviewer policy issue (3 non-critical issues → fail). That blocker has
to clear before the visual-variety HITL eval can actually be performed.
Consider that timing when prioritising Item 1 vs Items 2-5.

### Touch surface (preview)

- `internal/pipeline/agents/visual_breakdowner.go` — replace
  `ShotCountForDuration` invocation, change response shape to include
  `narration_beat_index`, normalize durations across beats.
- `internal/pipeline/agents/visual_breakdown_helpers.go` —
  `ShotCountForDuration` and `EnsureFrozenPrefix` rework. Keep
  `BuildFrozenDescriptor` intact.
- `docs/prompts/scenario/03_5_visual_breakdown.md` — major rewrite. Remove
  "every visual_descriptor must begin with `{frozen_descriptor}` verbatim"
  rule (line 52) and replace with "preserve visual identity continuity
  across shots while letting focal subject shift per beat."
- `internal/domain/scenario.go` — `VisualShot` may need `NarrationBeatIndex int`
  and `NarrationBeatText string`. If decision (a): also
  `NarrationScene.NarrationBeats []string`.
- `testdata/contracts/visual_breakdown.{schema,sample}.json` — schema bump,
  new sample fixture.
- `internal/pipeline/agents/visual_breakdowner_test.go` — heavy churn on
  shot-count tests.
- (only if (a)) `internal/pipeline/agents/writer.go` +
  `testdata/contracts/writer_output.{schema,sample}.json` — emit/encode
  `narration_beats` per scene.

---

## Item 2 — Retry observability metric (cross-stage)

Today, "did retry actually help?" is answered only by grepping
`"visual breakdowner retry"` in stderr. No counter, no metric,
no per-stage retry rate visible to the operator. Same gap exists in
writer's per-act retry.

### Outcome target

A counter (or structured event) per stage that distinguishes:

- `retry_total` — number of attempts that were retries (i.e. `attempt > 0`).
- `retry_succeeded` — retries that produced a passing result.
- `retry_exhausted` — retries that reached the budget without recovery.

Surface via `slog` structured fields at minimum (so log scrapers can
aggregate). Stretch: a simple in-memory counter exposed on the run report.

### Decisions for the plan step

- Emit at stage level (visual_breakdowner / writer) or unify in a shared
  helper? The retry shape is now duplicated across writer.go and
  visual_breakdowner.go — a shared `runWithRetry` helper would unify
  observability AND remove the duplication finding from the prior review.
  **Recommendation**: extract the helper as part of this item.
- Should `retry_exhausted` events be Error-level or Warn-level? Writer is
  Info; visual_breakdowner is Info. Probably Warn for exhaustion (run
  fails) and stay Info for success retries.

### Touch surface (preview)

- `internal/pipeline/agents/writer.go` — retry loop uses the new helper.
- `internal/pipeline/agents/visual_breakdowner.go` — same.
- New file (or add to an existing helpers file) for the shared
  `runWithRetry[T]` if Go generics are acceptable here, otherwise a
  function-with-interface variant.
- Test files for both stages.

---

## Item 3 — Test breadth (scene N>1, multi-scene concurrent)

Current retry tests only exercise scene 1 failing while scenes 2-4 succeed.
A regression where retry only works for the first scene index — or where
two simultaneously-failing scenes interfere — would not be caught.

### Outcome target

Add to `visual_breakdowner_test.go`:

- `TestVisualBreakdowner_Run_RetriesOnSceneN_WhenNGreaterThanOne` —
  parameterized: scene 3 fails first, valid second; assert
  `callsByScene[3] == 2`, scenes 1, 2, 4 == 1.
- `TestVisualBreakdowner_Run_HandlesMultipleConcurrentRetries` — scenes 1
  AND 3 both fail-then-recover; assert both succeed and the final scene
  ordering in `state.VisualBreakdown.Scenes` is preserved (1, 2, 3, 4).

`queueTextGenerator` already supports per-scene queues, so no new
test-helper code needed.

### Touch surface

- `internal/pipeline/agents/visual_breakdowner_test.go` only.

---

## Item 4 — Negative retry-budget defensive guard

`for attempt := 0; attempt <= visualBreakdownerPerSceneRetryBudget; attempt++`
would skip the body entirely if the constant ever became negative,
returning `(zero, 0, nil)` — a silent success with empty payload. Currently
impossible (const = 1 in source), but if/when the budget becomes
config-driven the path opens up. Same shape applies to writer.

### Outcome target

- Clamp budget to ≥0 inside the retry helper, OR
- Add a post-loop guard: `if lastErr == nil { lastErr = fmt.Errorf("retry budget invalid") }`.
- Test: `Test*_Run_NegativeBudget_DoesNotSilentlySucceed` — set budget to
  `-1` (via test seam if needed), assert error returned.

If Item 2 lands and extracts the helper, this guard lives in one place.
Otherwise add to both writer.go and visual_breakdowner.go.

### Touch surface

- `internal/pipeline/agents/writer.go`,
  `internal/pipeline/agents/visual_breakdowner.go` (or just the new helper).
- Corresponding test files.

---

## Item 5 — Pre-existing race in `fakeTextGenerator`

`fakeTextGenerator` (writer_test.go:587) has unsynchronized writes
(`f.calls++`, `f.last = req`, `append(f.reqs, req)`) and is shared across
visual_breakdowner errgroup goroutines by `RejectsInvalidTransition`,
`RejectsWrongShotCount`, and `DoesNotMutateStateOnFailure`. Race exists on
baseline `856a7bb`, NOT introduced by the stabilize cycle. Verified by
running `go test -race` against baseline.

### Outcome target

Add `sync.Mutex` to `fakeTextGenerator` matching the pattern already in
`sequenceTextGenerator` / `queueTextGenerator` / `sceneErrorGenerator`.
After the fix, `go test -race ./internal/pipeline/agents/...` should be
clean (or at least not flag this generator).

### Decisions for the plan step

- This helper is shared with writer tests. Audit all usage sites to
  confirm none will regress (e.g. tests that read `gen.calls` from the
  test goroutine without locking). Acceptable resolutions: gate via
  `sync.Mutex` and provide accessor methods (`Calls() int` etc.), or
  document that all reads must happen post-`Run` return.

### Touch surface

- `internal/pipeline/agents/writer_test.go` — `fakeTextGenerator` struct.
- Any test that reads `gen.calls` / `gen.last` / `gen.reqs` directly may
  need an accessor swap.

---

## Acceptance signals (cumulative — what "done" looks like for the bundle)

If shipped together:

- `go test ./...` clean.
- `go test -race ./internal/pipeline/agents/...` clean (Item 5).
- New retry-observability metrics emit on visual_breakdowner and writer
  stages (Item 2). Retry counts visible in the next dogfood log without
  grep-by-string.
- Test count grows: at least two new retry-coverage tests for
  visual_breakdowner (Item 3). At least one negative-budget guard test
  per stage (Item 4).
- For Item 1 only: HITL spot-check on the next clean dogfood — shots
  within a scene look visually varied (not repetitive); visual identity
  still cohesive across the video.

---

## Out of scope for this cycle

- **Reviewer 3-issue rejection** (the current dogfood blocker after Goal 1
  shipped). Different agent, different concern. Investigate separately.
- **Critic prompt 어미-pattern check** — already queued as Phase D from the
  fewshot dogfood result.
- **Adding more fewshot exemplars** — Phase 2, queued separately.
- **Image generation prompt assembly** — only the defensive prepend
  question (Item 1 sub-decision) belongs here.
- **TTS pacing / audio alignment**.
- **Schema bumps for stages other than visual_breakdown / writer** — only
  if (a) is chosen for Item 1's beat source.
