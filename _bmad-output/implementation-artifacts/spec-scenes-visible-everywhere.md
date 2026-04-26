---
title: 'Production scene list visible at every post-Phase-A stage'
type: 'refactor'
created: '2026-04-26'
status: 'in-review'
baseline_commit: 'f3ebae4af1d6d019a3a968b3dce4f844f545110e'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/spec-production-master-detail.md'
  - '{project-root}/_bmad-output/planning-artifacts/next-session-scenes-everywhere-prompt.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The Production page's left scene list is wired only at `batch_review/waiting`. At every other post-Phase-A stage (`scenario_review`, `character_pick`, `image`, `assemble`, `complete`) the segments table can be fully populated yet the user only sees the empty-state placeholder, contradicting the parent spec's "master-detail when scenes exist" approach and the Direction B mockup.

**Approach:** Relax `SceneService.ListScenes` to return segments whenever they exist (no stage gate); keep `SceneService.EditNarration` strictly gated to `scenario_review/waiting`. In `ProductionShell`, fetch scenes whenever `current_run` is non-null and not `pending`, render a read-only scene list as the `master` prop, and continue to bypass the master-detail wrapper only for the `batch_review/waiting` full-bleed surface. Append SCL-5 to the parent spec.

## Boundaries & Constraints

**Always:**
- Reads loose, writes locked: `EditNarration` retains its `scenario_review/waiting` gate.
- `BatchReview` keeps its current full-bleed rendering at `batch_review/waiting` — the new master pane does NOT wrap it.
- Reuse `useRunScenes`, `SceneCard`, `DetailPanel`, `ProductionMasterDetail` as-is. Selection (`active_scene_index`) lives in `ProductionShell`, defaults to first scene.
- Parent spec is `<frozen-after-approval>`. Append SCL-5 only — never edit body sections.

**Ask First:**
- Adding a `?scene=N` URL param (skip unless ≤5 lines, no test refactor).
- Any edit to `internal/api/handler_scene.go` beyond what an unchanged service contract demands.
- Any new CSS rule.

**Never:**
- New HTTP endpoints, new components, new schema. `SceneCard`/`DetailPanel` are already presentational and have no action affordances of their own — no `SceneListItem` variant needed.
- Any read-time backfill from `scenario_path` in `ListScenes` — none exists today, do not invent one.
- Approve/reject/regen/narration-edit affordances at non-`scenario_review/waiting` and non-`batch_review/waiting` stages.
- Keyboard, mobile-breakpoint, or theme changes.
- Touching the parent spec body (Intent / Boundaries / I/O Matrix / Tasks).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|---------------|----------------------------|----------------|
| List with segments at any stage | Run exists, N≥1 segments (any stage/status) | `ListScenes` returns N scenes; UI shows N cards | N/A |
| List with no segments | Run exists, 0 segments | Returns empty list (no error); UI shows existing placeholder | N/A |
| List for missing run | `runID` not found | `ErrNotFound` (404) — unchanged | Pass-through |
| EditNarration outside scenario_review/waiting | Any other stage/status | `ErrConflict` (409) — unchanged | Pass-through |
| BatchReview surface | `stage=batch_review`, `status=waiting` | `BatchReview` rendered full-bleed; master pane NOT wrapped | N/A |
| HITL stage WITH scenes | `scenario_review`/`character_pick`/`metadata_ack` waiting, segments present | Master = scene cards; Detail = the existing stage HITL component (`ScenarioInspector`/`CharacterPick`/`ComplianceGate`) unchanged | N/A |
| Non-HITL stage WITH scenes | `image`/`assemble`/`complete`, segments present | Master = scene cards; Detail = selected scene's `DetailPanel` (no action bar) | N/A |
| Pending or no-segments stage | `pending`, or any stage with 0 segments | Existing empty-state placeholder | N/A |

</frozen-after-approval>

## Code Map

- `internal/service/scene_service.go:304-319` — `ListScenes` gate to delete (the `Stage != ScenarioReview || Status != Waiting` check).
- `internal/service/scene_service.go:451-465` — `EditNarration` gate KEEP unchanged.
- `internal/service/scene_service_test.go:195-211, 239-281, 443-490` — helpers `scenarioReviewRun(id)` / `batchReviewRun(id, path)`; existing `ListScenes` / `EditNarration` test patterns to extend.
- `internal/api/handler_scene.go:148-162, 269-297` — handlers carry no extra stage gate; no edits required.
- `web/src/components/shells/ProductionShell.tsx:68-77, 154-164, 273-350` — wire `useRunScenes`, manage `active_scene_index`, build `master` node, route into `ProductionMasterDetail` for non-BatchReview surfaces; preserve `BatchReview` full-bleed bypass.
- `web/src/components/shared/{ProductionMasterDetail,SceneCard,DetailPanel}.tsx` — used as-is.
- `web/src/hooks/useRunScenes.ts` — already returns disabled query for null run_id; pass `current_run.id` only when stage is not `pending`.
- `web/src/components/shells/ProductionShell.test.tsx` — extend with `image/running` cases and a `scenario_review/waiting` master+detail co-render case.
- `_bmad-output/implementation-artifacts/spec-production-master-detail.md` — append SCL-5 only.

## Tasks & Acceptance

**Execution:**
- [x] `internal/service/scene_service.go` — In `ListScenes`, delete the stage/status gate. Run lookup + `ListByRunID` remain. `EditNarration` untouched.
- [x] `internal/service/scene_service_test.go` — Add: (a) `ListScenes` returns segments at `image/running` with N>0; (b) at `complete/completed`; (c) returns empty list (no error) at `image/running` with 0 segments. Replace `TestSceneService_ListScenes_ReturnsConflictWhenNotAtScenarioReview` with `TestSceneService_ListScenes_AllowsAnyStageWhenSegmentsExist`. Keep existing `EditNarration_ReturnsConflictWhenNotAtScenarioReview`.
- [x] `web/src/components/shells/ProductionShell.tsx` — Call `useRunScenes(current_run?.stage !== 'pending' ? current_run.id : null)`. Add `active_scene_index` state (default 0, reset on `current_run.id` change). When `scenes.length > 0` AND surface is NOT `batch_review/waiting`: build `master = <ul>{scenes.map(SceneCard with selected/on_select)}</ul>`; choose `detail` by stage — HITL waiting stages keep their existing component, non-HITL stages render `<DetailPanel item={scenes[active_scene_index]} />`. Pass both into `<ProductionMasterDetail master detail master_empty_message=… />`. `batch_review/waiting` continues full-bleed `<BatchReview />`.
- [x] `web/src/components/shells/ProductionShell.test.tsx` — Add: (a) run at `image/running` with N≥2 mocked scenes → assert N `SceneCard`s, first selected, right pane shows `DetailPanel` content for first; (b) run at `image/running` with 0 scenes → master placeholder present, no `SceneCard`; (c) run at `scenario_review/waiting` with mocked scenes → master shows cards AND right pane is `ScenarioInspector`. Existing pending / batch_review / character_pick / metadata_ack cases must stay green.
- [x] `_bmad-output/implementation-artifacts/spec-production-master-detail.md` — Append SCL-5 (verbatim block in §Spec Amendment). Do NOT touch any other line.

**Acceptance Criteria:**
- Given run `scp-049-run-4` (`stage=image`, 10 segments), when opening `/production?run=scp-049-run-4`, then 10 cards render on the left and the right pane shows the first scene's read-only `DetailPanel`.
- Given a run at `scenario_review/waiting` with segments, when opening Production, then the left pane shows scene cards AND the right pane still renders `ScenarioInspector` (no regression on the inline narration editor).
- Given a run at `batch_review/waiting`, when opening Production, then `BatchReview` renders full-bleed (no master wrapper, J/K still works).
- Given a run at `pending` OR any stage with 0 segments, when opening Production, then the existing empty-state placeholder renders and no `SceneCard` is shown.
- `go test ./internal/service/...` passes; `cd web && npm run test` passes.
- Parent spec diff = single appended SCL-5 block; no other line changes.
- Branch `fix/scenes-visible-everywhere` merges into `main` fast-forward; `git stash pop` restores dogfood WIP intact.

## Spec Amendment (to append to parent spec)

Append verbatim under `## Spec Change Log` after SCL-4:

```
- **SCL-5** — Trigger: dogfood at scp-049-run-4 (`stage=image`, 10 segments) showed empty master pane despite populated segments; this contradicts §Approach "master-detail when scenes exist" and the Direction B mockup. Amendment: §I/O Matrix is augmented with the row "non-review stage WITH scenes" → master = scene list (read-only at non-HITL surfaces), detail = `DetailPanel` for selected scene (already action-less). `SceneService.ListScenes` no longer gates on stage; `EditNarration` keeps its strict `scenario_review/waiting` gate. Deviation from the trigger prompt: no `scenario_path → segments` backfill is added because none exists today (the prompt's "keep existing backfill" line described code that never shipped). KEEP: scenario_review/waiting and batch_review/waiting interactive surfaces unchanged; `BatchReview` continues full-bleed without the master wrapper.
```

## Verification

- `go test ./internal/service/... -run SceneService` — all pass, including new `ListScenes` cases and unchanged `EditNarration` regression guard.
- `cd web && npm run test -- ProductionShell` — all new + existing cases pass.
- `cd web && npm run test` — full suite green.
- Manual: open `/production?run=scp-049-run-4`, click between cards, verify right pane updates; switch to a scenario_review/batch_review/pending run and confirm correct surface.
