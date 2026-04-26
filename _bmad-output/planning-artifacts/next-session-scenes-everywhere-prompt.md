---
title: 'Next-session prompt — Production Master-Detail scenes-everywhere'
type: 'spec-amendment / refactor'
created: '2026-04-26'
workflow: '/bmad-quick-dev'
parent_spec: '_bmad-output/implementation-artifacts/spec-production-master-detail.md'
---

# Use

Paste this prompt into a fresh session. Run `/bmad-quick-dev`. The output of this story is a **spec amendment + implementation** — the parent spec is already `status: done` and `<frozen-after-approval>`, so do **not** rewrite it; append a Spec Change Log entry.

---

# Goal

Make the Production page's **left scene list** appear at every post-Phase-A stage when scenes exist, matching the Direction B mockup. Today the master pane is wired only at `batch_review/waiting`; at `character_pick` / `image` / `assemble` / `complete` the segments table can be fully populated and the user still sees the empty-state placeholder. This was never user intent — the parent spec literally says "master-detail when scenes exist" but the I/O Matrix only enumerated `batch_review/waiting`, so the implementation closed early.

---

# Background

## Mockup (the source of truth for user intent)

`_bmad-output/planning-artifacts/ux-design-directions.html` → "Direction B — Master-Detail Split". The mockup explicitly shows:

- Left pane: 8 scene cards with status pills (`auto-approved`, `pending`, `rejected`).
- Right pane: selected scene's detail (visual placeholder, NARRATION/COHERENCE/PACING/SCP_ACCURACY/AUDIO/VISUAL 6-metric grid, audio scrubber).
- Stepper highlights **Stage 4 — Assets**, NOT batch_review. The user signed off on this picture.

## Parent spec wording

`_bmad-output/implementation-artifacts/spec-production-master-detail.md:17` (§Approach):

> "The body is master-detail **when scenes exist**, otherwise the right pane falls back to today's stage-routed content."

But §I/O Matrix only enumerated 4 rows: `pending`, `batch_review/waiting`, `non-review stage no scenes`, `6-metric mapping`. The case **"non-review stage WITH scenes"** is missing — and that's exactly what dogfood hit.

## Current implementation gap

**Frontend** — left master is always blank outside batch_review:
- [web/src/components/shells/ProductionShell.tsx:346-349](web/src/components/shells/ProductionShell.tsx#L346-L349) — `<ProductionMasterDetail detail={renderStageDetail()} master_empty_message={...} />` does not pass a `master` prop. The empty placeholder is the only thing the left pane ever renders for non-batch_review.
- [web/src/components/shared/ProductionMasterDetail.tsx:22-30](web/src/components/shared/ProductionMasterDetail.tsx#L22-L30) — empty fallback when `master` is null. Component is fine; consumer just never feeds it.

**Backend** — `/scenes` hard-gates on `scenario_review/waiting`:
- [internal/service/scene_service.go:339](internal/service/scene_service.go#L339) — returns `ErrConflict` unless `run.Stage == StageScenarioReview && run.Status == StatusWaiting`. This blocks `useRunScenes` from being reused at any later stage.

## Live repro

```bash
sqlite3 .tmp/startup/pipeline.db "SELECT id, stage, status, scenario_path FROM runs WHERE id='scp-049-run-4';"
# scp-049-run-4|image|running|scenario.json
sqlite3 .tmp/startup/pipeline.db "SELECT COUNT(*) FROM segments WHERE run_id='scp-049-run-4';"
# 10
```

Open `/production?run=scp-049-run-4` → left pane shows "Scenes are not yet available — assets in progress." Should show 10 scene cards per the mockup.

---

# Scope

## Backend

1. **Relax `SceneService.ListScenes` gate** ([internal/service/scene_service.go:334-361](internal/service/scene_service.go#L334-L361)).
   - New rule: return segments whenever `len(segments) > 0` for the run. No stage check on the read.
   - Keep the existing read-time backfill (empty-segments + scenario_review/waiting + scenario_path → seed from scenario.json).
   - At `pending` / pre-Phase-A with no segments and no scenario_path → return empty list (UI shows existing placeholder).
2. **Keep `SceneService.EditNarration` strict** ([internal/service/scene_service.go:493-508](internal/service/scene_service.go#L493-L508)).
   - Reads loose, writes locked to `scenario_review/waiting`. Do not let a user mutate narration at image/assemble.
3. **Decisions store untouched.** No new endpoints, no schema changes.

## Frontend

1. **`ProductionShell.tsx`** ([web/src/components/shells/ProductionShell.tsx](web/src/components/shells/ProductionShell.tsx)).
   - Call `useRunScenes(current_run.id)` whenever `current_run` is non-null and `current_run.stage !== 'pending'`. Reuse the existing hook; backend change above means it now succeeds at all post-Phase-A stages.
   - Build `master` node when `scenes.length > 0`: render a read-only scene list (use `SceneCard` if it can be configured to suppress action affordances, otherwise add a sibling `<SceneListItem>` that mirrors the mockup's compact row — title + status pill + critic score badge).
   - Selection: `active_scene_index` state in shell. Default to first scene; when interactive HITL surface owns the active scene, defer to that surface's selection (don't fight `BatchReview`'s J/K).
2. **`detail` prop** stays stage-routed.
   - At `scenario_review/waiting` → `ScenarioInspector` (unchanged).
   - At `character_pick/waiting` → `CharacterPick` (unchanged).
   - At `batch_review/waiting` → `BatchReview` owns its own master+detail; ProductionShell should bypass the new master-detail wrapping and render `BatchReview` full-bleed exactly as today.
   - At `metadata_ack/waiting` → `ComplianceGate` (unchanged).
   - At non-HITL stages **with scenes** (image/assemble/complete or any running state with segments) → render selected scene's `DetailPanel` read-only. Hide approve/reject. The 6-metric grid + narration text + audio (when path present) stay visible.
   - At non-HITL stages **no scenes** → existing "Stage in progress" placeholder.
3. **URL state** (optional, do only if cheap): `?scene=N` query param so refresh preserves selection. If costly, skip — selection resets on remount is acceptable.

## Tests

- **`internal/service/scene_service_test.go`** — new cases:
  - ListScenes returns segments at `image/running` when populated.
  - ListScenes returns segments at `complete/completed` when populated.
  - ListScenes still backfills at `scenario_review/waiting` with empty segments + scenario_path.
  - EditNarration still rejects at non-`scenario_review/waiting` (regression guard for the strict-write rule).
- **`web/src/components/shells/ProductionShell.test.tsx`** — new cases:
  - Run at `image/running` with mocked scenes → master-detail rendered, scene list visible, detail shows selected scene's `DetailPanel`.
  - Run at `image/running` with empty scenes → existing "Stage in progress" placeholder.
  - Existing pending / scenario_review / batch_review / character_pick / metadata_ack cases stay green.

---

# Spec amendment

The parent spec is `<frozen-after-approval>` and `status: done`. Do **not** edit the §Intent / §Boundaries / §I/O Matrix / §Tasks. Append a new entry under the existing **§Spec Change Log** section (search for `SCL-1`, `SCL-2` precedent in the file):

```
- **SCL-N** — Trigger: dogfood at scp-049-run-4 (`stage=image`, 10 segments, scenario_path set) showed empty master pane despite populated segments; this contradicts the mockup which shows scenes at "Stage 4 — Assets" and the §Approach line "master-detail when scenes exist". Amendment: §I/O Matrix is augmented with a new row "non-review stage WITH scenes" → master = scene list (read-only at non-HITL), detail = DetailPanel for selected scene (read-only). `SceneService.ListScenes` no longer gates on stage; `EditNarration` keeps its strict `scenario_review/waiting` gate. KEEP: scenario_review/waiting and batch_review/waiting interactive surfaces unchanged.
```

Number `SCL-N` based on the highest existing entry +1.

---

# Non-goals

- No new HTTP endpoints. Reuse `/scenes`.
- No critic re-fetch / shadow eval changes. Reuse what's already on segment rows.
- No keyboard model changes. J/K/Enter/Esc stay scoped to BatchReview.
- No regen / approve / reject from non-HITL stages. View-only.
- No mobile breakpoint changes.
- No CSS overhaul. Reuse `.production-master-detail__*` and existing `SceneCard` / `DetailPanel` rules; minor additions only if a read-only variant needs distinct styling.

---

# Commit-scope warning (CRITICAL)

The working tree currently has **80+ dirty dogfood files** (run `git status` to confirm). The user's commit-scope rule (auto-memory `feedback_commit_scope.md`) is strict: **only files touched by this fix may land in the commit**. Spillover = rejected.

**Required workflow:**
1. `git stash -u` to preserve dogfood WIP.
2. New branch from `main`: `git checkout -b fix/scenes-visible-everywhere main`.
3. Implement scope above.
4. Commit + merge to `main` (FF only).
5. `git stash pop` after merge to restore dogfood WIP.

**Allowed file set** (anything else — ask first):
- `internal/service/scene_service.go` + its `_test.go`
- `internal/api/handler_*.go` only if route-level guards exist (search before assuming)
- `web/src/components/shells/ProductionShell.tsx` + its `_test.tsx`
- `web/src/components/shared/ProductionMasterDetail.tsx` (only if its signature must change — it shouldn't)
- A new `web/src/components/shared/SceneListItem.tsx` (or equivalent) **only** if `SceneCard` can't be rendered read-only without invasive prop drilling
- `web/src/index.css` — append-only, no rule deletion
- `_bmad-output/implementation-artifacts/spec-production-master-detail.md` — **append SCL-N only**

---

# Acceptance

- Run at `stage>=character_pick` with populated segments → opening `/production?run={id}` shows N scene cards on the left, right pane shows either the stage-routed HITL component or the selected scene's read-only `DetailPanel`.
- Run at `scenario_review/waiting` → existing `ScenarioInspector` renders unchanged on the right; left pane now shows the scene list too (was previously empty in the non-batch_review path; this is an enhancement, not a regression).
- Run at `batch_review/waiting` → existing `BatchReview` surface renders full-bleed, unchanged.
- Run at `pending` / `write/failed` / pre-segments → empty-state placeholder unchanged.
- All vitest + go test suites pass.
- Spec amendment SCL-N appended; parent spec body untouched.
- Branch merged to `main`, stash popped, dogfood WIP intact.
