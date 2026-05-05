---
title: 'Drop "Key visual moments" from frozen descriptor (per-shot grid fix)'
type: 'bugfix'
created: '2026-05-05'
status: 'done'
context: []
baseline_commit: '535e3b221b4bbf3ee153ff443ec5f81f0a1ca788'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** `BuildFrozenDescriptor` joins `VisualIdentity.KeyVisualMoments` (a list of distinct narrative scenarios — e.g. "performing surgery", "writing in journal", "being restrained") into a single `Key visual moments:` field. Phase B per-shot image-edit prompts treat the joined list as "draw all of these" → 3-4 panel comic-strip output instead of one coherent scene per beat. Canonical generation already strips the field via `extractCharacterDescriptor`, so only per-shot is broken.

**Approach:** Remove the `Key visual moments` field from `BuildFrozenDescriptor` at the source — reduce to 3 fields (`Appearance`, `Distinguishing features`, `Environment`). No information loss: `visual_breakdowner` still receives full `VisualIdentity` JSON (incl. `KeyVisualMoments`) via the `{scp_visual_reference}` placeholder.

## Boundaries & Constraints

**Always:**
- Keep `domain.VisualIdentity.KeyVisualMoments` field unchanged (research output, JSON schema, DB columns, API surface untouched).
- Keep `BuildFrozenDescriptor` signature `(domain.VisualIdentity) string` — only the format string and joined fields change.
- All 4-field-frozen test fixtures sync to 3-field in the same change set; suite green at session end.

**Ask First:**
- If new dogfood run still shows multi-panel grid after this change, the residual trigger is non-frozen (e.g. `visual_descriptor` LLM output containing list-style content). Stop and report — do not add a `SceneStylePrompt` negative guard in this cycle.

**Never:**
- Do not modify `domain.VisualIdentity.KeyVisualMoments` or its persistence.
- Do not migrate existing run `scenario.json` files (operator starts a new run).
- Do not edit `internal/service/scp_image_service_test.go:308-349` — it manually seeds a 4-field frozen to validate `extractCharacterDescriptor` strip behavior, which stays as defense-in-depth.
- Do not add image-prompt grid guards to `SceneStylePrompt`; root-cause first.
- Do not touch uncommitted `testdata/golden/eval/manifest.json` (out-of-scope leftover from prior cycle).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior |
|----------|--------------|---------------------------|
| Full identity | All fields populated; KeyVisualMoments has N items | `Appearance: …; Distinguishing features: …; Environment: …` — no `Key visual moments` substring |
| Empty KeyVisualMoments | Slice empty or nil | Same 3-field output; no trailing empty segment |
| Per-shot prompt | Phase B `ComposeImagePrompt` receives 3-field frozen | Composed image-edit prompt contains no `Key visual moments:` substring |
| Canonical prompt | Frozen → `extractCharacterDescriptor` | `Environment:` still stripped; character content unchanged vs. before |

</frozen-after-approval>

## Code Map

- `internal/pipeline/agents/visual_breakdown_helpers.go:97-105` -- `BuildFrozenDescriptor`. Reduce 4-field → 3-field; one-line comment on rationale.
- `internal/pipeline/agents/visual_breakdown_helpers_test.go:52-63` -- `TestBuildFrozenDescriptor_Stable`. Update `want` to 3-field; add `Contains` negative assertion for `"Key visual moments"`.
- `internal/domain/review_test.go:61` -- `Corrected` fixture: 4 → 3 fields.
- `internal/domain/visual_breakdown_test.go:111` -- `FrozenDescriptor` fixture: 4 → 3 fields.
- `internal/pipeline/agents/reviewer_test.go:131,142` -- `VisualDescriptor` (line 131) and `FrozenDescriptor` (line 142) fixtures: 4 → 3 fields. Preserve test-specific suffixes (`; shot description`, `; Blink`-style).
- `internal/pipeline/phase_a_integration_test.go:880,901` -- `VisualDescriptor` and `FrozenDescriptor` fixtures: 4 → 3 fields.
- `internal/pipeline/image_track_test.go:314` -- `frozen` fixture: 4 → 3 fields.
- `internal/service/scp_image_service.go:307-326` (verify only) — `extractCharacterDescriptor` still strips `Environment:`; `Key visual moments:` branch becomes no-op for new runs (intentional defense-in-depth).
- `internal/pipeline/agents/visual_breakdowner.go:113,291-300` (verify only) — `frozen` still receives 3-field; `{scp_visual_reference}` marshals full `VisualIdentity` (KeyVisualMoments preserved for LLM context).

## Tasks & Acceptance

**Execution:**
- [x] `internal/pipeline/agents/visual_breakdown_helpers.go` -- Reduce `BuildFrozenDescriptor` to 3-field. Drop the 4th `Key visual moments: %s` segment + corresponding `strings.Join(v.KeyVisualMoments, ", ")` arg. Add one-line comment.
- [x] `internal/pipeline/agents/visual_breakdown_helpers_test.go` -- Update `want` to 3-field; add `strings.Contains(got, "Key visual moments")` negative assertion.
- [x] `internal/domain/review_test.go` -- Sync line 61 to 3-field.
- [x] `internal/domain/visual_breakdown_test.go` -- Sync line 111 to 3-field.
- [x] `internal/pipeline/agents/reviewer_test.go` -- Sync lines 131, 142 to 3-field.
- [x] `internal/pipeline/phase_a_integration_test.go` -- Sync lines 880, 901 to 3-field.
- [x] `internal/pipeline/image_track_test.go` -- Sync line 314 to 3-field.
- [x] `go build ./... && go test ./...` -- in-scope packages green; pre-existing critic/eval failures (incident rune cap 480→720 from prior cycle) confirmed independent of this change set via baseline run.

**Acceptance Criteria:**
- Given a populated `VisualIdentity`, when `BuildFrozenDescriptor` runs, then the result has no `"Key visual moments"` substring and ordered segments `Appearance; Distinguishing features; Environment`.
- Given Phase B per-shot generation, when `ComposeImagePrompt` is invoked on a frozen produced by the new `BuildFrozenDescriptor`, then the composed prompt contains no `"Key visual moments:"` substring.
- Given canonical generation, when `composeCanonicalPrompt` runs on a 3-field frozen, then `Environment:` is still stripped (`extractCharacterDescriptor` unchanged).
- Given `go test ./...`, when run on the change set, then all packages pass.

## Design Notes

**Source change vs. surgical strip:** Story 5.4 byte-stability contract is _"frozen does not vary within a single run"_, not _"function output frozen forever"_. Source change benefits both consumers — `visual_breakdowner` LLM also receives `{frozen_descriptor}` and doesn't need KVM duplicated there (it has it via `{scp_visual_reference}`).

**Commit split (per `feedback_commit_scope.md`):** (1) `fix(visual_breakdown): drop "Key visual moments" from frozen descriptor` — helpers.go + helpers_test.go. (2) `test: sync frozen descriptor fixtures to 3-field format` — all other fixtures. Combined commit acceptable if splitting feels artificial.

## Spec Change Log

### 2026-05-05 — step-04 review patches

Three patch-level findings applied without spec amendment:
1. **Edge#1:** `testdata/contracts/visual_breakdown.sample.json:4` updated from legacy 4-field to 3-field for documentation consistency (schema only typed it as `string` so tests passed regardless).
2. **Edge#2:** `internal/service/scp_image_service.go:282-307` doc comments described the 4-field shape as current state; updated to mark `Key visual moments` as legacy and explain the strip branch is retained for defense-in-depth on regenerated legacy runs.
3. **Audit#1:** `BuildFrozenDescriptor` comment compressed from 6 → 3 lines to better match spec's "one-line comment" guidance while preserving the rationale.

One defer logged to `deferred-work.md`:
- **Edge#3:** Undo of a pre-2026-05-05 `descriptor_edit` decision restores a legacy 4-field frozen verbatim, which Phase B regeneration would pass through to image-edit and re-trigger the multi-panel grid bug for that one run. Spec D4 already accepts the broader pre-change-runs migration gap; this is a narrow corner of it.

## Verification

**Commands:**
- `go build ./...` -- expected: clean build.
- `go test ./...` -- expected: all green.
- `grep -rn "Key visual moments" internal/` -- expected: only the lowercase branch in `scp_image_service.go` (the strip path) and possibly the unmodified `scp_image_service_test.go` strip-test seed.

**Manual checks (operator-driven dogfood, not session gate):**
- New SCP run through Phase B; inspect `output/{run-id}/audit.log` `image_generation` events — no `"Key visual moments:"` substring in any prompt.
- Production review UI: 3 random scenes are single coherent scenes, not multi-panel grids.
- Canonical reference sheet still renders as single front-facing pose.
