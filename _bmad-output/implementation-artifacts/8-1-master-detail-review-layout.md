# Story 8.1: Master-Detail Review Layout (SceneCard/DetailPanel)

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want a 30:70 review layout to manage scenes efficiently,
so that I can see the whole batch while focusing on one scene at a time.

## Prerequisites

**Hard dependencies:** Stories 6.1, 6.2, 6.3, and 6.5 established the design system, shell layout, keyboard engine, and frontend test harness. Story 7.1 established the Production dashboard shell and run polling. Story 7.2 established the scene-review read/edit pattern and `SceneService` / `SceneHandler` layering. Story 7.3 established TanStack Query + Zod contract extension patterns for stage-specific Production surfaces. Story 7.4 established the inline banner coexistence pattern inside `ProductionShell`.

**Backend dependency:** Story 4.4 already added dedicated scene-level review gating (`segments.review_status`, `segments.safeguard_flags`, `system_auto_approved` decisions). Story 2.6 already added HITL summary/diff support for `batch_review` sessions. Story 8.1 must consume that state; do not redesign review gating.

**Parallel-work caution:** The current worktree already contains in-flight Epic 7 changes touching `web/src/components/shells/ProductionShell.tsx`, `web/src/index.css`, `web/src/contracts/runContracts.ts`, `web/src/lib/apiClient.ts`, `web/src/lib/queryKeys.ts`, and the `internal/api/handler_scene.go` / `internal/service/scene_service.go` path. Implement Story 8.1 by layering onto those files carefully; do not revert adjacent work from Stories 7.2-7.5.

## Acceptance Criteria

### AC-1: Batch review renders as a 30:70 master-detail layout

**Given** the selected run is paused at `batch_review/waiting`  
**When** the Production surface renders the review stage  
**Then** the left pane occupies roughly 30% width and shows a scrollable list of `SceneCard`s  
**And** the right pane occupies roughly 70% width and shows a `DetailPanel` for the selected scene  
**And** each `SceneCard` displays a shot thumbnail strip using 1-5 thumbnails derived from the persisted `segments.shots` payload  
**And** the `DetailPanel` shows either the scene clip (`clip_path`) in an inline video player or, when no clip is available yet, a scrollable shot gallery with transition indicators between shots.

**Rules:**
- `Focus-Follows-Selection`: there is never an empty detail state while scenes exist.
- Mobile/narrow layouts may stack vertically, but desktop must preserve the 30:70 split direction.
- Do not reuse the current `ScenarioInspector` list markup for batch review; this is a separate surface with separate contracts.

**Tests:**
- Component/integration test verifies the batch-review shell renders both panes and the first reviewable scene is selected by default.
- Contract test verifies review items include enough data for thumbnail strip, clip fallback, and detail rendering.

---

### AC-2: Keyboard-first navigation keeps sidebar and detail panel synchronized

**Given** the batch review surface is mounted  
**When** the operator presses `J` or `K`  
**Then** selection moves to the next or previous review item in the rendered queue order  
**And** the selected `SceneCard` receives the selected visual state and `aria-selected="true"`  
**And** the `DetailPanel` updates immediately to the same scene  
**And** the selected card scrolls into view if it was outside the visible list region.

**Rules:**
- Use the existing `useKeyboardShortcuts` provider; do not add raw `window.addEventListener` listeners.
- `J/K` are active only while the batch-review surface is mounted.
- Selection state stays local to the batch-review surface; do not widen it into Zustand.

**Tests:**
- Integration test verifies `J/K` update selection and detail content together.
- Test verifies selection stays bounded at list edges.

---

### AC-3: Regenerated scenes show an optional before/after diff surface

**Given** a review item includes both current and prior scene content  
**When** the operator opens its `DetailPanel`  
**Then** the panel shows a before/after diff visualization for narration and images  
**And** a version toggle allows switching between current and prior variants  
**And** the diff surface is omitted entirely when no prior version exists.

**Rules:**
- Story 8.1 only renders optional diff data; it does not implement the rejection/regeneration write flow from Story 8.4.
- The review-item contract may expose optional `previous_version` data for narration and shots; keep the UI resilient when that field is absent.
- Narration diff can be a lightweight textual comparison; do not introduce a heavy diff-editor dependency.

**Tests:**
- Component test verifies version toggle + before/after narration/image rendering when `previous_version` exists.
- Test verifies the diff section is absent for standard scenes.

---

### AC-4: High-leverage scenes are promoted and visibly distinguished

**Given** batch-review items are loaded  
**When** the system identifies high-leverage scenes  
**Then** those scenes are sorted to the top of the pending review queue  
**And** each corresponding `SceneCard` shows a `"High-Leverage"` badge  
**And** the classification reason is available to the `DetailPanel`.

**Rules:**
- High-leverage classification for V1 comes from deterministic metadata, not heuristics hidden in CSS:
  - first character appearance in `scenario.json`
  - hook scene (`act_id` matching the opening hook / first scene of the narrative)
  - act-boundary scenes (first scene of a new `act_id`)
- Preserve relative scene order within each priority bucket.
- Only pending review items are promoted; already approved/rejected/auto-approved items remain below the actionable queue.

**Tests:**
- Service/unit test verifies first-appearance, hook, and act-boundary detection.
- Integration test verifies high-leverage pending scenes sort ahead of other waiting scenes.

---

### AC-5: High-leverage detail view exposes deeper review context

**Given** the selected review item is high-leverage  
**When** its `DetailPanel` renders  
**Then** the panel shows a larger hero image preview than standard scenes  
**And** it renders the full Critic score classification surface: aggregate score plus hook strength, fact accuracy, emotional variation, and immersion  
**And** it displays a `"Why high-leverage"` annotation explaining the classification reason.

**Rules:**
- Use the existing score-color token thresholds from UX/epics: `>= 80` green, `50-79` accent, `< 50` amber.
- Critic sub-scores come from persisted review data (`segments.critic_sub` JSON payload when present); parse defensively.
- The high-leverage variant extends the standard detail panel rather than branching into a separate route or modal.

**Tests:**
- Component test verifies aggregate + four sub-score rows render with the correct high/mid/low visual classes.
- Test verifies the `"Why high-leverage"` annotation content matches the computed reason.

## Tasks / Subtasks

- [x] **T1: Add a dedicated batch-review read contract and backend aggregator** (AC: #1, #3, #4, #5)
  - Add a dedicated read path for batch-review items under the existing scene-review stack, for example:
    - `GET /api/runs/{id}/review-items`
  - Keep `GET /api/runs/{id}/scenes` scoped to `scenario_review`; do not widen that existing contract into a stage-polymorphic payload.
  - Add a `BatchReviewService` method or extend `SceneService` with a clearly named batch-review read method that:
    - validates `run.stage == batch_review && run.status == waiting`
    - loads segments from `SegmentStore.ListByRunID`
    - loads `scenario.json` from `run.scenario_path` for `act_id`, `characters_present`, and first-appearance context
    - computes high-leverage flags/reasons deterministically
    - preserves optional `previous_version` fields when present
  - Add response envelope + handler tests covering stage gating and payload shape.

- [x] **T2: Extend frontend contracts and API client for batch-review items** (AC: #1, #3, #4, #5)
  - Add Zod schemas in `web/src/contracts/runContracts.ts` for:
    - `reviewItemShot`
    - `reviewItemPreviousVersion`
    - `reviewItemCriticBreakdown`
    - `reviewItem`
    - `reviewItemListResponse`
  - Add `fetchBatchReviewItems(run_id)` to `web/src/lib/apiClient.ts`.
  - Add `queryKeys.runs.reviewItems(run_id)` to `web/src/lib/queryKeys.ts`.
  - Add frontend contract tests for the new payload.

- [x] **T3: Build a batch-review slot/container for the Production shell** (AC: #1, #2)
  - Create `web/src/components/production/BatchReview.tsx` as the stateful surface owner.
  - Responsibilities:
    - fetch review items via TanStack Query
    - derive initial selected scene from sorted actionable queue
    - own selected item state
    - wire keyboard navigation and scroll-into-view behavior
    - pass narrow props into presentational subcomponents

- [x] **T4: Build `SceneCard` and `DetailPanel` shared review primitives** (AC: #1, #4, #5)
  - Create `web/src/components/shared/SceneCard.tsx`
  - Create `web/src/components/shared/DetailPanel.tsx`
  - `SceneCard` requirements:
    - thumbnail strip from `shots`
    - narration excerpt
    - compact score badge
    - review-state badge
    - selected styling + `aria-selected`
    - optional `"High-Leverage"` badge
  - `DetailPanel` requirements:
    - clip player when `clip_path` exists
    - shot gallery fallback with transition chips between shots
    - full narration
    - large hero image region
    - optional diff section
    - optional high-leverage explanation

- [x] **T5: Implement queue ordering and focus-follows-selection behavior** (AC: #1, #2, #4)
  - Sort order should be:
    - waiting high-leverage
    - waiting non-high-leverage
    - rejected / approved manual review items as applicable
    - auto-approved collapsed items
  - Preserve original `scene_index` order within each bucket.
  - When the current selection disappears because data refetched or the run changed, fall back to the first item in the freshly sorted queue.

- [x] **T6: Add the optional before/after diff presentation** (AC: #3)
  - Render narration before/after blocks with inline emphasis for changed text.
  - Render image before/after previews side by side or in a toggleable single-frame view.
  - Hide the version toggle and diff container when `previous_version == null`.
  - Do not implement persistence or mutation paths for regenerations in this story.

- [x] **T7: Add high-leverage annotation and Critic sub-score rendering** (AC: #4, #5)
  - Parse `critic_sub` JSON into a typed breakdown structure.
  - Surface aggregate + four rubric bars in `DetailPanel`.
  - Add a compact helper that maps classification reason codes to user-facing copy such as:
    - `First appearance of SCP-049`
    - `Opening hook scene`
    - `Act boundary: act_3`

- [x] **T8: Wire batch review into `ProductionShell` and shared styling** (AC: #1, #2)
  - Update `web/src/components/shells/ProductionShell.tsx`:
    - render `<BatchReview key={current_run.id} run={current_run} />` when `stage === 'batch_review' && status === 'waiting'`
    - preserve coexistence with `FailureBanner`
  - Add layout and component styles in `web/src/index.css` for:
    - 30:70 split
    - list-pane scrolling
    - selected card state
    - detail hero/media area
    - transition indicator chips
    - high-leverage variant

- [x] **T9: Add focused backend and frontend tests** (AC: #1-#5)
  - Backend:
    - handler/service tests for batch-review gating and payload assembly
    - high-leverage classification tests
    - optional diff payload tests
  - Frontend:
    - `BatchReview.test.tsx`
    - `SceneCard.test.tsx`
    - `DetailPanel.test.tsx`
    - `ProductionShell.test.tsx` stage-conditional render coverage

### Review Findings

_Reviewed 2026-04-19 via parallel Blind Hunter + Edge Case Hunter + Acceptance Auditor layers._

**Decision-needed** _(resolved)_

- [x] [Review][Decision] Hook+first-appearance overlap → resolved as **concatenate with `"; "`** (orthodox single-string spec; primary `reason_code` kept for filtering). Implemented at [internal/service/scene_service.go:355-364](../../internal/service/scene_service.go#L355-L364).
- [x] [Review][Decision] K-at-first boundary behavior → resolved as **bounded** (spec AC-2 is authoritative). Existing `Math.max(0, current_index - 1)` kept; test coverage added for both edges.

**Patch** _(all applied)_

- [x] [Review][Patch] `parsePreviousVersion` now requires `previous_version:` prefix strictly ([scene_service.go:442-460](../../internal/service/scene_service.go#L442-L460))
- [x] [Review][Patch] DetailPanel `version` resets via `useEffect([item.scene_index])` on scene change ([DetailPanel.tsx:46-48](../../web/src/components/shared/DetailPanel.tsx#L46-L48))
- [x] [Review][Patch] heroShot in Previous view no longer falls back to current's shot ([DetailPanel.tsx:50](../../web/src/components/shared/DetailPanel.tsx#L50))
- [x] [Review][Patch] Nil `Shots` slice now emits `[]` via `normalizeShots` helper (applied both for `ReviewItem.Shots` and `ReviewItemPreviousVersion.Shots`)
- [x] [Review][Patch] `normalizeOptionalScore` clamps output to `[0, 100]` after scaling
- [x] [Review][Patch] `computeHighLeverage` skips `sceneIndex < 0` (guards `SceneNum=0`)
- [x] [Review][Patch] DetailPanel.test.tsx adds explicit standard-scene (non-high-leverage + null previous) absence case and version-reset test
- [x] [Review][Patch] BatchReview.test.tsx now exercises K-at-first bound (upper edge) in addition to J-at-last
- [x] [Review][Patch] Empty-string `ClipPath` now serialized as absent via `normalizeOptionalPath`

**Deferred**

- [x] [Review][Defer] `characters_present` entity match unreachable for Korean character names — normalized `"049"` label won't match `"연구원"` etc.; `entity_visible` fallback covers realistic cases ([scene_service.go:360-377](../../internal/service/scene_service.go#L360-L377))
- [x] [Review][Defer] `isHookAct` uses `strings.Contains("hook"/"opening")` — false positives for future act names like `act_hook_callback` or `reopening_act_4` ([scene_service.go:390-393](../../internal/service/scene_service.go#L390-L393))
- [x] [Review][Defer] Duplicate `SceneNum` in scenario.json silently overwrites classifications map — upstream scenario generator should enforce uniqueness
- [x] [Review][Defer] UX-DR60 AppShell sidebar-collapse coordination (220→48px in 1024-1279px range) — BatchReview internal 30:70 split IS preserved; sidebar collapse is AppShell's responsibility
- [x] [Review][Defer] `batch-review__list` fixed `max-height: 44rem` — forces internal scroll; minor UX for tall queues
- [x] [Review][Defer] J/K with invalid `selected_scene_index` collapses both directions to index 0 — low-probability edge case (refetch race)
- [x] [Review][Defer] High-leverage + auto_approved scenes demoted to bucket 3 (below reviewed) — editorial tradeoff
- [x] [Review][Defer] Listbox a11y pattern incomplete — no `aria-activedescendant`, no focus management across J/K navigation
- [x] [Review][Defer] `selected_item` null flash on refetch when selection is removed — momentary empty-state render
- [x] [Review][Defer] `normalizeOptionalScore` ambiguity at 0/1 boundary — heuristic auto-scale; upstream contract should be explicit
- [x] [Review][Defer] `useQuery` has no `refetchInterval` — stale review list after external approval updates (Story 8.2 scope)
- [x] [Review][Defer] `os.ReadFile(*run.ScenarioPath)` unbounded read, no size cap — internal path, not user input
- [x] [Review][Defer] Narration diff is bag-of-words (not sequence diff) — spec allows "lightweight textual comparison"
- [x] [Review][Defer] ProductionShortcutPanel also registers J/K globally — lifecycle-based enablement holds in practice
- [x] [Review][Defer] `review_status` frontend enum is strict — any new server-side status value would fail zod for entire payload

## Dev Notes

### Story Intent and Scope Boundary

- Story 8.1 creates the read-only master-detail review surface.
- Do NOT implement approve/reject/skip/edit mutations here; those belong to Story 8.2 and later.
- Do NOT implement undo stack/history timeline; those belong to Stories 8.3 and 8.6.
- Do NOT invent a new global review store. Batch-review selection is local UI state.

### Backend Read-Path Recommendation

The current `GET /api/runs/{id}/scenes` endpoint and `SceneService.ListScenes()` are explicitly scoped to `scenario_review` and return a narrow narration-edit payload. Reusing that endpoint for batch review would either break `ScenarioInspector` or force a stage-polymorphic response that weakens contracts.

Recommended approach:

- keep `GET /api/runs/{id}/scenes` unchanged for Story 7.2
- add a dedicated batch-review read endpoint
- keep the same handler -> service -> store layering already established in `internal/api/handler_scene.go` and `internal/service/scene_service.go`

This isolates Epic 8 read complexity without regressing Epic 7.

### Available Domain Data to Reuse

Current `segments` rows already expose most of what Story 8.1 needs:

- `shots` JSON -> thumbnail strip, shot gallery, transition indicators
- `clip_path` -> inline video player when available
- `tts_path` / `tts_duration_ms` -> future audio player integration in Story 8.2
- `critic_score` -> compact score badge
- `critic_sub` -> per-rubric breakdown (JSON string, parse defensively)
- `review_status` -> waiting/auto-approved/approved/rejected state
- `safeguard_flags` -> optional warning badges later if needed

Scenario-only metadata such as `act_id` and `characters_present` still lives in `scenario.json`. Use `run.scenario_path` as the canonical source when computing high-leverage reasons; do not duplicate that data into a new DB column in this story.

### High-Leverage Classification Guidance

Use deterministic V1 rules:

1. **First character appearance:** first scene where the primary SCP/entity appears in `characters_present` or `entity_visible`.
2. **Hook scene:** the narrative opening scene, or the first scene with `act_id` corresponding to the opening hook.
3. **Act boundary:** the first scene of each new `act_id`.

Return both:

- `is_high_leverage: boolean`
- `high_leverage_reason: string`

This keeps sorting logic and detail annotations aligned.

### Optional Diff Guidance

The sprint prompt requires a before/after surface, but Story 8.4 owns the actual rejection/regeneration workflow. For Story 8.1:

- support optional `previous_version` data in the read contract
- render the diff when present
- gracefully omit the section when absent

Do not add speculative mutation endpoints or version-history tables yet. If seeded fixtures or future artifacts provide a previous version, the UI should already know how to render it.

### Keyboard and React Query Guardrails

- Use the existing `useKeyboardShortcuts` provider for `J/K`; do not bypass the central shortcut registry.
- Keep batch-review data in TanStack Query server state and component-local selection state.
- Repository guidance already prefers effect usage only for true external synchronization. That lines up with React's guidance that Effects synchronize with external systems rather than serving as a general derived-state tool.  
  Reference: https://react.dev/reference/react/useEffect
- TanStack Query v5 removed `onSuccess`/`onError`/`onSettled` from `useQuery`, so preload/synchronization logic should live in component effects or derived rendering code rather than query callbacks.  
  Reference: https://tanstack.com/query/latest/docs/framework/react/guides/migrating-to-v5

### Suggested Response Shape

Recommended frontend-facing batch-review item shape:

```ts
type ReviewItem = {
  scene_index: number
  narration: string
  review_status: 'waiting_for_review' | 'auto_approved' | 'approved' | 'rejected'
  critic_score: number | null
  critic_breakdown: {
    aggregate: number | null
    hook: number | null
    fact_accuracy: number | null
    emotional_variation: number | null
    immersion: number | null
  } | null
  clip_path: string | null
  tts_path: string | null
  tts_duration_ms: number | null
  shots: Array<{
    image_path: string
    duration_s: number
    transition: string
    visual_descriptor: string
  }>
  is_high_leverage: boolean
  high_leverage_reason: string | null
  previous_version: {
    narration: string
    shots: Array<{ image_path: string; transition: string }>
  } | null
}
```

Exact field names can vary, but the contract must support all ACs without forcing the UI to re-open `scenario.json` directly.

### File Structure Requirements

New backend files or major additions:

- `internal/api/handler_review_items.go` or an equivalent batch-review handler alongside `handler_scene.go`
- `internal/service/batch_review_service.go` or an equivalent extension point under `scene_service.go`
- backend tests for the new read contract

New frontend files:

- `web/src/components/production/BatchReview.tsx`
- `web/src/components/production/BatchReview.test.tsx`
- `web/src/components/shared/SceneCard.tsx`
- `web/src/components/shared/SceneCard.test.tsx`
- `web/src/components/shared/DetailPanel.tsx`
- `web/src/components/shared/DetailPanel.test.tsx`

Updated frontend files:

- `web/src/components/shells/ProductionShell.tsx`
- `web/src/contracts/runContracts.ts`
- `web/src/contracts/runContracts.test.ts`
- `web/src/lib/apiClient.ts`
- `web/src/lib/queryKeys.ts`
- `web/src/index.css`

### Testing Requirements

Backend verification:

- `go test ./internal/api ./internal/service ./internal/db`
- add at least one fixture-backed test for a run paused at `batch_review`
- verify stage conflict remains correct for both `scenario_review` and `batch_review` read paths

Frontend verification:

- `npm run lint`
- `npm run test:unit`
- component coverage for queue ordering, J/K sync, diff rendering, and high-leverage detail mode

### Previous Story Intelligence

- Story 7.2 established the repo pattern of keeping stage-specific Production surfaces in `web/src/components/production/` and narrow reusable UI primitives in `web/src/components/shared/`.
- Story 7.2 review noted that handler responses echoing request payloads can drift from persisted state later. For batch-review reads, prefer assembling the response from authoritative persisted data rather than echoing transient UI inputs.
- Story 7.3 documented the TanStack Query v5 callback constraint and the local-state approach for stage-specific flow state. Reuse that pattern here.
- Story 7.4 established that new inline surfaces in `ProductionShell` should coexist cleanly with `FailureBanner` rather than replacing the shell layout.

### Git Intelligence Summary

- Recent visible commit history is still anchored at Epic 6 (`1e44084 Implement Epic 6: Web UI — Design System & Application Shell`), while Epic 7 work is currently present as uncommitted local changes and new files.
- That means Story 8.1 implementation must treat the current workspace as the source of truth for Production-shell patterns, not the older commit history alone.
- In particular, preserve local Epic 7 changes in `ProductionShell`, `runContracts`, `apiClient`, `queryKeys`, `handler_scene`, and `scene_service`.

## Dev Agent Record

### Debug Log

- 2026-04-19 15:38:36 KST: Implemented dedicated `/api/runs/{id}/review-items` read path, batch-review UI, keyboard navigation, and high-leverage ordering/detail rendering.
- 2026-04-19 15:38:36 KST: Added backend handler/service tests, full frontend unit coverage for the new surface, and contract fixture coverage.

### Completion Notes

- Added a dedicated batch-review contract without widening the existing `scenario_review` scenes endpoint.
- Implemented deterministic high-leverage classification from `scenario.json`, critic breakdown parsing, queue ordering, and local focus-follows-selection behavior with `J/K`.
- Added optional previous-version rendering in the UI and best-effort backend preservation when persisted metadata includes a `previous_version:` JSON payload.
- Verified with `go test ./...` and `npm --prefix web run test:unit`.

## File List

- `internal/service/scene_service.go`
- `internal/service/scene_service_test.go`
- `internal/api/handler_scene.go`
- `internal/api/handler_scene_test.go`
- `internal/api/routes.go`
- `web/src/contracts/runContracts.ts`
- `web/src/contracts/runContracts.test.ts`
- `web/src/lib/apiClient.ts`
- `web/src/lib/queryKeys.ts`
- `web/src/components/production/BatchReview.tsx`
- `web/src/components/production/BatchReview.test.tsx`
- `web/src/components/shared/SceneCard.tsx`
- `web/src/components/shared/SceneCard.test.tsx`
- `web/src/components/shared/DetailPanel.tsx`
- `web/src/components/shared/DetailPanel.test.tsx`
- `web/src/components/shells/ProductionShell.tsx`
- `web/src/components/shells/ProductionShell.test.tsx`
- `web/src/index.css`
- `testdata/contracts/run.review-items.response.json`

## Change Log

- 2026-04-19: Implemented Story 8.1 master-detail batch review surface and moved story to `review`.
- 2026-04-19: Code review complete (parallel Blind + Edge Case + Acceptance layers). Applied 9 patches and resolved 2 decision-needed findings; 15 items deferred to `deferred-work.md`. Tests pass (`go test ./...` + `npm run test:unit`). Story moved to `done`.
