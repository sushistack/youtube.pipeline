# Story 7.2: Scenario Inspector & Inline Editor

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want to inspect and edit the generated narration paragraphs inline,
so that I can refine the script before committing to image/TTS generation.

## Prerequisites

**Hard dependencies:** Stories 6.1, 6.2, 6.3, 6.5, and 7.1 established the design system, Production shell, keyboard engine, frontend test harness, and Production dashboard shell this story must extend rather than replace.

**Backend dependency:** Epic 2 and Story 2.6 already established HITL wait-state status payloads (`paused_position`, `decisions_summary`, summary string, diff metadata). Story 7.2 builds the operator-facing scenario edit surface on top of that state model and must not introduce a second pause/resume contract.

- Story 7.1 already created the initial Production dashboard surface, selected-run inventory, and live status polling contract. Story 7.2 should attach the scenario inspector into the slot Story 7.1 intentionally left for future review content.
- The keyboard system from Story 6.3 already suppresses global shortcuts while an input or textarea has focus. Story 7.2 should reuse that behavior so editing text does not accidentally trigger unrelated global commands.
- The current backend route table exposes lifecycle endpoints plus `/api/runs/{id}/status` and character search/pick routes, but it does not yet expose scenario scene-list or inline-edit endpoints.
- The `segments` persistence model already contains `run_id`, `scene_index`, and `narration`, and `SegmentStore.ListByRunID` already returns scene rows in review order. A focused narration update path is missing and should be added rather than duplicating storage.
- `internal/service/HITLService.BuildStatus` already identifies whether a run is paused at `scenario_review`; the scenario inspector should use that fact to gate visibility instead of inferring review readiness from UI-only state.

## Acceptance Criteria

1. **AC-SCENARIO-REVIEW-PARAGRAPH-SURFACE:** when the selected run is paused at `scenario_review`, the Production surface renders a Scenario Inspector that shows the generated Korean narration grouped into readable scene/paragraph blocks.

   Required outcome:
   - the inspector appears only when the selected run is at the scenario-review wait point
   - narration is rendered from persisted backend scene data, not placeholder copy
   - each scene row clearly exposes scene order and its narration paragraph
   - the layout preserves the Story 7.1 status/dashboard shell while adding a dedicated scenario-review content region

   Rules:
   - use the selected run from the existing Production/dashboard flow rather than inventing a second selection source
   - keep the read surface paragraph-first and readable; do not collapse narration into raw JSON or dense table rows
   - preserve room for Story 7.3 character selection to reuse the same Production-shell composition without rework

   Tests:
   - route/integration test verifies the Scenario Inspector renders only for `scenario_review` waiting runs
   - component test verifies scene order and narration text come from parsed API payloads

2. **AC-INLINE-EDIT-ACTIVATION-AND-FEEDBACK:** clicking a narration paragraph, or pressing `Tab` on an editable paragraph, switches that paragraph into inline edit mode with a focused textarea and clear editing-state feedback.

   Required outcome:
   - click enters edit mode for the targeted paragraph
   - keyboard users can reach the paragraph and enter edit mode with `Tab`
   - the active editor autofocuses a textarea containing the current narration text
   - editing mode changes border/background styling so the active paragraph is unmistakable

   Rules:
   - only one paragraph may be in edit mode at a time
   - editing affordances must remain keyboard-reachable and visually obvious under the existing focus-ring system
   - reuse the existing keyboard shortcut infrastructure where helpful, but do not fight native textarea behavior once the editor has focus

   Tests:
   - component test verifies click and keyboard entry both activate edit mode
   - accessibility test verifies focus moves into the textarea and the active paragraph shows editing-state styling

3. **AC-BLUR-OR-ENTER-SAVE-WITH-PERSISTENCE:** finishing an edit via blur or `Enter` triggers a save cycle that shows factual inline status, persists the change through the backend API, and updates the read surface with the saved text.

   Required outcome:
   - blur saves the current textarea value
   - `Enter` saves when not combined with modifiers; `Shift+Enter` remains available for newline entry if multiline editing is preserved
   - the active paragraph shows a visible `Saving...` state during the request
   - successful saves replace the read-mode paragraph text with the new persisted content

   Rules:
   - use standard TanStack Query mutation flows for save behavior; optimistic UI is not required here because the UX explicitly expects a factual saving state
   - invalidate or refresh the relevant scenario query after save so UI state and persisted state converge cleanly
   - keep save scope narrow to the edited paragraph/scene; do not refetch unrelated Production data unless necessary

   Tests:
   - integration test verifies blur-save persistence cycle against a mocked API
   - interaction test verifies `Enter` saves and `Shift+Enter` does not accidentally submit
   - contract test verifies the scene-edit request/response schemas catch payload drift

4. **AC-ERROR-RECOVERY-AND-REVERT:** if the inline save fails, the UI shows an inline error message and restores the original narration text instead of leaving the paragraph in a ghost-edited state.

   Required outcome:
   - failed saves surface a visible inline error message near the edited paragraph
   - the paragraph exits the transient saving state cleanly
   - the displayed text reverts to the last persisted value
   - the operator can re-enter edit mode without refreshing the page

   Rules:
   - preserve the pre-edit persisted text locally so failures can restore it immediately
   - map backend validation/conflict failures into actionable operator-facing error copy
   - do not silently swallow save failures or leave the textarea stuck open without status

   Tests:
   - integration test verifies failed save restores original text and shows error UI
   - regression test verifies subsequent edit attempts still work after a failed mutation

5. **AC-KEYBOARD-REVERT-BEHAVIOR:** the scenario editor supports the Epic 7 keyboard contract for edit entry and revert behavior without breaking the existing global shortcut semantics.

   Required outcome:
   - `Tab` can enter edit mode from the paragraph surface
   - `Ctrl+Z` while editing reverts the textarea back to the last persisted paragraph value
   - global review shortcuts remain suppressed while the textarea is focused
   - keyboard usage does not cause accidental route-level actions during text entry

   Rules:
   - keep `Ctrl+Z` scoped to the active paragraph editor in this story; do not prematurely implement the broader decision Command Pattern owned by later review stories
   - align with the current keyboard hook behavior that editable controls block global handlers by default
   - document any deliberate `Enter`/newline tradeoff explicitly in code comments or tests so future stories do not drift

   Tests:
   - component test verifies `Ctrl+Z` restores the last persisted text for the active paragraph
   - keyboard regression test verifies textarea focus suppresses global shortcut handlers while editing

## Tasks / Subtasks

- [x] **T1: Add scene review read/edit API contracts and persistence path** (AC: #1, #3, #4)
  - [x] Add backend routes for `GET /api/runs/{id}/scenes` and `POST /api/runs/{id}/scenes/{idx}/edit` under `internal/api/routes.go`.
  - [x] Add request/response schemas and handlers for scene listing and narration editing, keeping the versioned API envelope contract intact.
  - [x] Add a focused segment-store update method for narration text keyed by `(run_id, scene_index)` rather than bypassing persistence with ad-hoc SQL in handlers.
  - [x] Decide and implement whether narration edits also need `scenario.json` synchronization for downstream Phase B/C consistency; if required, keep DB and artifact updates in one service-level operation. (Decision: segments.narration is the authoritative source for Phase B/C; no scenario.json sync needed — captured as deferred work if needed later.)

- [x] **T2: Extend frontend contracts, client helpers, and queries for scenario review data** (AC: #1, #3, #4)
  - [x] Extend `web/src/contracts/runContracts.ts` or adjacent contract files with scene-list and scene-edit schemas.
  - [x] Introduce `web/src/lib/apiClient.ts` and `web/src/lib/queryKeys.ts` if Story 7.1 implementation did not already create them, and add scene list/edit helpers there.
  - [x] Add a `useRunScenes` query hook and a focused edit mutation hook for inline persistence.

- [x] **T3: Build the Scenario Inspector and inline paragraph editor UI** (AC: #1, #2, #3, #4, #5)
  - [x] Replace the remaining placeholder body in `web/src/components/shells/ProductionShell.tsx` with a composed Production review surface if Story 7.1 has not already done so.
  - [x] Create a Production-surface component such as `ScenarioInspector` that renders ordered scene paragraphs for `scenario_review`.
  - [x] Create a focused inline editor component or subcomponent for per-paragraph edit/read state, including editing, saving, and error visuals.
  - [x] Keep the shell composition explicit so Story 7.3 can swap in character-selection content when the run stage changes.

- [x] **T4: Wire keyboard entry and revert behavior** (AC: #2, #5)
  - [x] Reuse `useKeyboardShortcuts` only for non-editable-surface activation and revert flows that need it.
  - [x] Ensure textarea focus relies on native editing behavior and that global shortcuts remain suppressed.
  - [x] Add `Ctrl+Z` restore-to-persisted-value behavior for the active paragraph without touching the later batch-review undo stack.

- [x] **T5: Add focused test coverage across backend and frontend** (AC: #1, #2, #3, #4, #5)
  - [x] Add backend handler/service/store tests for scene list and narration edit success/failure paths.
  - [x] Add RTL tests for scenario review rendering, edit activation, blur-save, Enter save, failed save recovery, and `Ctrl+Z` revert.
  - [x] Add contract fixture tests for the new scene list/edit payloads.

## Dev Notes

### Story Intent and Scope Boundary

- Story 7.2 is the first operator-editable content surface in the Production tab. Its purpose is to let the operator inspect and refine Phase A narration before the workflow proceeds into character/image/TTS generation.
- This story owns paragraph-level scenario inspection and persistence only.
- Keep later Epic 7 and 8 work unblocked by preserving clear boundaries:
  - Story 7.3 owns character candidate selection and Vision Descriptor editing
  - Story 7.4 owns failure banners and progressive error recovery around run execution
  - Epic 8 owns batch review actions, optimistic approve/reject flows, and the broader undo stack semantics
- Do not turn this story into a general rich-text editor, batch review panel, or scene-decision workflow.

### Current Codebase Reality

- `web/src/components/shells/ProductionShell.tsx` is still a minimal route shell with `ProductionShortcutPanel` placeholder content in the current repo state.
- `web/src/components/shared/Sidebar.tsx` currently handles route navigation only; scenario editing should live in the Production main content area, not inside the sidebar.
- `web/src/stores/useUIStore.ts` persists only `sidebar_collapsed` today. If paragraph edit state is client-only and short-lived, prefer route-local component state instead of widening the global store prematurely.
- `web/src/contracts/runContracts.ts` currently defines run stage/status/list/detail/resume schemas only. It does not yet describe scene list or scene edit contracts.
- `internal/api/routes.go` currently registers lifecycle routes plus character search/pick routes, but there is no scene review route family yet.
- `internal/db/segment_store.go` already exposes `ListByRunID` and the `segments` rows include `narration`, but no dedicated narration update method exists.
- `internal/service/HITLService.BuildStatus` already returns enough stage metadata to determine whether the selected run is paused at `scenario_review`.
- `web/src/hooks/useKeyboardShortcuts.tsx` already suppresses global shortcuts inside editable controls (`input`, `textarea`, `contenteditable`). That behavior is a guardrail for this story, not something to work around.

### Technical Requirements

- The scenario inspector should be gated by actual backend run stage/status: only show the editable narration surface when `run.stage === 'scenario_review'` and `run.status === 'waiting'`.
- Use persisted scene data from the backend, not copied fragments from the status payload. The status endpoint is for run telemetry; scene narration belongs in a dedicated scene-list contract.
- Treat one scene paragraph as one edit unit for V1. Do not introduce cross-paragraph batch editing, draft autosave queues, or multi-paragraph dirty-state coordination in this story.
- Saving should use an explicit mutation with visible pending state rather than optimistic replacement.
- Preserve the last persisted paragraph string in local component state so failed saves and `Ctrl+Z` revert have a stable source of truth.
- Keep the API envelope shape consistent with the rest of the app: `{ "version": 1, "data": ... }` or `{ "version": 1, "error": ... }`.
- Preserve backend conflict/validation handling:
  - invalid run or scene index -> `NOT_FOUND` / `VALIDATION_ERROR`
  - stage mismatch or non-editable state -> `CONFLICT`
- Downstream propagation matters: UX-DR61 requires scenario edits to flow into later phases. If Phase B/C consume `segments.narration`, that may be sufficient; if they still read `scenario.json`, this story must update both sources together or explicitly create deferred work.

### Architecture Compliance

- Follow the existing frontend architecture conventions:
  - server state via TanStack Query
  - client-only ephemeral UI state in component state or narrowly scoped store state
  - contracts parsed with Zod before the UI consumes them
  - query keys centralized in `web/src/lib/queryKeys.ts`
- Follow the backend layering already present in the repo:
  - HTTP handler parses/validates request
  - service layer owns edit rules and any multi-write consistency
  - store layer owns `segments` persistence details
- Keep the Production shell compositional. The scenario surface should plug into the Production content slot instead of bypassing the shell or mutating sidebar responsibilities.
- Reuse the current keyboard engine semantics rather than binding raw `window` listeners inside editor components.

### Library / Framework Requirements

- Continue using React 19, TanStack Query v5, React Router v7, Zustand v5, Zod v4, and Vitest 4 already present in the repo.
- Reuse the current keyboard shortcut infrastructure and tests. Do not introduce a second keyboard library for inline editing.
- Use the existing API response envelope helpers on the Go side (`writeJSON`, `writeDomainError`) rather than writing custom response JSON per handler.

### File Structure Requirements

- Expected backend additions:
  - `internal/api/handler_scene.go` or equivalent scene-review handlers
  - `internal/service/` scene review/edit service wiring if narration updates need service-level orchestration
  - `internal/db/segment_store.go` updates for narration edit persistence
- Expected frontend additions:
  - `web/src/components/production/ScenarioInspector.tsx` or similar Production-specific surface component
  - `web/src/components/production/InlineNarrationEditor.tsx` or similar focused editor component
  - `web/src/hooks/useRunScenes.ts`
  - `web/src/lib/apiClient.ts`
  - `web/src/lib/queryKeys.ts`
- Expected updates:
  - `web/src/components/shells/ProductionShell.tsx`
  - `web/src/contracts/runContracts.ts`
  - `internal/api/routes.go`
  - related tests under `web/src/` and `internal/`
- If Story 7.1 implementation already introduced some of these files, extend them rather than creating parallel variants.

### Testing Requirements

- Backend:
  - handler tests for `GET /api/runs/{id}/scenes`
  - handler tests for `POST /api/runs/{id}/scenes/{idx}/edit`
  - service/store tests for narration persistence, missing scene handling, and stage mismatch conflict behavior
- Frontend:
  - component tests for read mode, edit mode, saving state, and error state
  - keyboard tests for `Tab` entry and `Ctrl+Z` revert
  - integration tests for blur-save and `Enter` save against mocked API responses
- Keep tests deterministic:
  - no real network calls
  - no wall-clock save timing assertions beyond visible pending-state semantics
  - verify behavior under focused textarea to guard against shortcut regressions
- Story verification should include, at minimum:
  - `npm run lint`
  - `npm run test:unit`
  - targeted Go tests covering new scene handlers/services/stores

### Previous Story Intelligence

- Story 7.1 deliberately left a clear slot boundary for later scenario and character surfaces. Use that seam instead of reworking the dashboard/status layer.
- Story 7.1 also reinforced TanStack Query + Zod + `renderWithProviders` as the standard frontend path. Story 7.2 should continue that pattern for scene list/edit data.
- Story 6.3's keyboard hook is directly relevant here because it already proves editable controls suppress global shortcuts. That is exactly the behavior the inline editor needs once the textarea is focused.
- Story 6.5's provider-backed frontend test setup should remain the default harness for this story's component and route integration coverage.

### Git Intelligence Summary

- The current commit history still shows the web layer as recently established groundwork rather than a mature Production editing surface. This means Story 7.2 should stay incremental and architecture-led instead of relying on hidden prior implementations.
- The lowest-risk implementation order remains:
  - add backend scene list/edit contracts and persistence
  - extend frontend contracts and query hooks
  - build the scenario inspector UI
  - wire edit activation/save/revert behavior
  - add deterministic tests for both success and failure paths

### Project Context Reference

- No separate `project-context.md` was present in the repository during story creation. Planning artifacts plus the current backend/frontend codebase were used as the authoritative context sources.

## Story Completion Status

- Story file created: `_bmad-output/implementation-artifacts/7-2-scenario-inspector-inline-editor.md`
- Story status set to `ready-for-dev`
- Sprint status should reflect this story as `ready-for-dev`
- Completion note: Ultimate context engine analysis completed - comprehensive developer guide created

## Dev Agent Record

### Implementation Plan

- Add backend scene-review read/edit endpoints and a narration persistence path centered on `segments`.
- Extend frontend contracts, query helpers, and Production-shell composition so `scenario_review` runs render an editable narration inspector.
- Implement per-paragraph inline edit state with visible editing/saving/error feedback, plus keyboard entry and `Ctrl+Z` revert to persisted text.
- Add deterministic backend and frontend tests that cover success, failure, and shortcut-suppression behavior.

### Debug Log

- Story creation workflow review on 2026-04-19
- Planning artifact analysis: Epic 7, architecture API/frontend layout, UX scenario-edit requirements
- Current codebase inspection: Production shell, sidebar, UI store, run contracts, keyboard shortcuts, API routes, segment persistence, HITL status service

### Completion Notes

- Implemented `GET /api/runs/{id}/scenes` and `POST /api/runs/{id}/scenes/{idx}/edit` with full CONFLICT/NOT_FOUND/VALIDATION_ERROR gating.
- Added `SegmentStore.UpdateNarration` (keyed by `run_id, scene_index`) and `SceneService` as the service layer enforcing `scenario_review/waiting` state precondition.
- Built `ScenarioInspector` + `InlineNarrationEditor` components: read mode, edit mode (click or Tab/Enter), blur-save, Enter-save, Shift+Enter newline, Ctrl+Z revert, Saving… indicator, error recovery.
- `ProductionShell` now conditionally shows `ScenarioInspector` for `scenario_review/waiting` runs; falls back to `ProductionShortcutPanel` otherwise — clean slot for Story 7.3.
- No `useEffect` for state transitions — component uses event-handler-only state updates to satisfy strict `react-hooks/set-state-in-effect` ESLint rule.
- Downstream note: `segments.narration` is the authoritative source for Phase B/C narration; `scenario.json` synchronization is deferred work if Phase B/C prove to consume it.
- All tests: Go 20 packages ✓, Frontend 82 tests (17 files) ✓, ESLint ✓, TypeScript ✓.

### Review Findings

- [x] [Review][Patch] Compile error false positive — `scene_service.go:57` — Blind Hunter flagged `return nil, fmt.Errorf` in single-return function; actual file uses `return fmt.Errorf`. Dismissed after `go build` confirmed no error.
- [x] [Review][Patch] Tab key does not enter edit mode [web/src/components/production/InlineNarrationEditor.tsx:98] — `handle_key_down_read` handled only Enter/Space; AC #2 and #5 explicitly require Tab to activate edit mode. Fixed: added `'Tab'` to the key condition and updated aria-label and ScenarioInspector hint.
- [x] [Review][Patch] `handle_blur` stale-closure double-save [web/src/components/production/InlineNarrationEditor.tsx:90] — `handle_blur` captured `save_state` in closure; if blur fired before React settled the `'saving'` state update (e.g. mobile keyboard dismiss after Enter), `mutation.mutate()` was called twice. Fixed: added `is_saving_ref` guard; `handle_blur` now delegates unconditionally to `save()` which returns early if a save is already in-flight.
- [x] [Review][Patch] Stale read-mode text after successful save [web/src/hooks/useRunScenes.ts:19] — after `on_deactivate()`, read mode showed old `scene.narration` until the background refetch completed. Fixed: `onSuccess` now calls `setQueryData` to update the cache immediately before `invalidateQueries`.
- [x] [Review][Patch] Trim comparison mismatch causes spurious save on open-then-close [web/src/components/production/InlineNarrationEditor.tsx:50] — `draft.trim() === revert_to` was false when persisted narration had surrounding whitespace, triggering a silent write on every open-close. Fixed: changed to `draft.trim() === revert_to.trim()`.
- [x] [Review][Defer] TOCTOU in EditNarration service layer [internal/service/scene_service.go:62] — non-atomic `Get` then `UpdateNarration` allows a concurrent run-resume to advance stage between the two operations. Very low probability in a single-operator tool; deferred, pre-existing design boundary.
- [x] [Review][Defer] Backend Edit endpoint echoes request payload not DB value [internal/api/handler_scene.go:81] — handler returns `req.Narration` not a post-write DB read; fragile if normalization is ever added in the DB or service layer. No current impact; deferred.
- [x] [Review][Defer] `useState` baseline never re-synced from scene prop [web/src/components/production/InlineNarrationEditor.tsx:26] — `draft` and `revert_to` are initialized once and not updated if the parent query re-fetches while the editor is active. Only manifests in multi-tab concurrent editing; deferred.

### File List

- `_bmad-output/implementation-artifacts/7-2-scenario-inspector-inline-editor.md`
- `internal/db/segment_store.go` — added `UpdateNarration` method
- `internal/service/scene_service.go` — new `SceneService` with `ListScenes` / `EditNarration`
- `internal/api/handler_scene.go` — new `SceneHandler` for scene list/edit endpoints
- `internal/api/routes.go` — registered scene routes; added `Scene *SceneHandler` to `Dependencies`; extended `NewDependencies` signature
- `cmd/pipeline/serve.go` — wired `SceneService` + `NewDependencies` call updated
- `web/src/contracts/runContracts.ts` — added `sceneSchema`, `sceneListResponseSchema`, `sceneEditResponseSchema`, `Scene` type
- `web/src/lib/apiClient.ts` — added `fetchRunScenes`, `editSceneNarration`
- `web/src/lib/queryKeys.ts` — added `scenes` key factory
- `web/src/hooks/useRunScenes.ts` — new `useRunScenes` query hook + `useEditNarration` mutation hook
- `web/src/components/production/ScenarioInspector.tsx` — new production surface component
- `web/src/components/production/InlineNarrationEditor.tsx` — new per-paragraph inline editor
- `web/src/components/shells/ProductionShell.tsx` — shows `ScenarioInspector` at `scenario_review/waiting`
- `web/src/index.css` — ScenarioInspector + InlineNarrationEditor CSS classes
- `internal/api/handler_scene_test.go` — handler tests for List/Edit endpoints
- `internal/service/scene_service_test.go` — service unit tests
- `internal/db/segment_store_test.go` — added `UpdateNarration` store tests
- `web/src/components/production/InlineNarrationEditor.test.tsx` — RTL tests: read, edit activation, Enter save, Shift+Enter, Ctrl+Z revert, error recovery
- `web/src/components/production/ScenarioInspector.test.tsx` — RTL tests: scene list, loading, error, empty
- `web/src/contracts/runContracts.test.ts` — contract fixture tests for scene list/edit schemas
- `testdata/contracts/run.scenes.response.json` — scene list fixture
- `testdata/contracts/run.scene.edit.response.json` — scene edit fixture
