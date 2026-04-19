# Story 8.2: Scene Review Actions & AudioPlayer

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want to listen to narration and commit review actions,
so that I can refine scene quality quickly without breaking review flow.

## Prerequisites

**Hard dependencies:** Stories 6.1, 6.2, 6.3, and 6.5 established the design system, shell layout, keyboard engine, and frontend test harness. Story 7.1 established Production polling and run inventory. Story 7.2 established scene-service / scene-handler layering plus local inline-edit patterns. Story 7.4 established banner coexistence inside `ProductionShell`. Story 8.1 is the immediate dependency: Story 8.2 assumes the batch-review read surface already exists with `BatchReview`, `DetailPanel`, and the dedicated batch-review read contract.

**Backend dependency:** Story 4.4 already introduced dedicated scene review-state (`segments.review_status`, `segments.safeguard_flags`) plus `system_auto_approved` decision writes. Story 2.6 already introduced `pipeline.UpsertSessionFromState`, which Epic 8 is expected to call after each HITL decision write. Story 8.2 must reuse those primitives rather than inventing a second review snapshot system.

**Current codebase reality:** today the repo still only has `SceneService.ListScenes/EditNarration` for `scenario_review`, and there is no decision-capture write handler yet. `DecisionStore` already owns the `decisions` table and the batch-review preparation transaction, but it does not yet expose per-scene approve/reject/skip writes. `web/src/components/shared/AudioPlayer.tsx` does not exist yet.

**Parallel-work caution:** the active worktree already contains in-flight Epic 7 and Epic 8 changes around `ProductionShell.tsx`, `index.css`, `runContracts.ts`, `apiClient.ts`, and `queryKeys.ts`. Layer onto those files carefully. Do not revert adjacent changes or widen the old `GET /api/runs/{id}/scenes` scenario-review contract.

## Acceptance Criteria

### AC-1: Detail panel renders a compact AudioPlayer for the selected scene

**Given** the selected batch-review item has `tts_path` and optional `tts_duration_ms`  
**When** the detail panel renders  
**Then** it shows an `AudioPlayer` with play/pause control, current time, total duration, and a seekbar  
**And** pressing `Space` toggles playback while the batch-review surface is mounted  
**And** selecting a different scene resets playback to `0:00` and pauses the previous audio.

**Rules:**
- Implement `AudioPlayer` as a shared component at `web/src/components/shared/AudioPlayer.tsx`, matching the architecture location guidance.
- Use HTML5 `<audio>` only. No waveform library, no custom media backend.
- Default is manual playback only; do not auto-play on selection change.
- If `tts_path` is missing, render a compact unavailable state instead of a broken player.

**Tests:**
- Component test verifies play/pause and seekbar rendering for a valid source.
- Component test verifies `Space` toggles playback and calls `preventDefault`.
- Component test verifies scene change resets time display to `0:00` and pauses playback.

---

### AC-2: Approve / Reject / Skip actions write a decision row and move review forward

**Given** the selected run is at `batch_review/waiting` and a scene is focused  
**When** the operator triggers `Approve` (`Enter`), `Reject` (`Esc`), or `Skip` (`S`)  
**Then** the client sends one canonical decision-write request  
**And** the backend inserts a new `decisions` row with run id, scene id, decision type, and timestamp  
**And** the batch-review surface auto-selects the next reviewable scene after success.

**Rules:**
- Story 8.2 is the story that picks up the deferred canonical endpoint from Story 2.6: `POST /api/runs/{id}/decisions`.
- Request shape should be action-based, for example:
  - `{ "scene_index": 3, "decision_type": "approve" }`
  - `{ "scene_index": 3, "decision_type": "reject" }`
  - `{ "scene_index": 3, "decision_type": "skip_and_remember", "context_snapshot": {...} }`
- `scene_id` stored in `decisions` continues to be the string form of `scene_index` (`"0"`, `"1"`, ...), matching existing HITL snapshot logic.
- `Approve` sets `segments.review_status = 'approved'`.
- `Reject` sets `segments.review_status = 'rejected'`.
- `Skip` records the decision row but leaves `segments.review_status` unchanged in V1; the immediate “move to next scene” behavior is a batch-review UI concern in this story, while richer skip/undo semantics arrive in Story 8.3.
- Reject reason prompting is out of scope here. Story 8.2 writes a reject with `note = NULL`; Story 8.4 extends the same write path with inline reason capture.

**Tests:**
- Handler/service integration test verifies each action inserts the correct `decision_type`.
- Store test verifies approve/reject also update `segments.review_status`.
- Frontend integration test verifies success auto-selects the next scene and updates the visible running count.

---

### AC-3: Skip writes a queryable `skip_and_remember` pattern for future hints

**Given** the operator presses `S` on a scene  
**When** the skip action succeeds  
**Then** the inserted decision row has `decision_type = 'skip_and_remember'`  
**And** the scene pattern is stored in a queryable JSON `context_snapshot` shape  
**And** that snapshot includes, at minimum:
- `scene_index`
- `critic_score`
- `critic_sub` when available
- `content_flags` derived from `segments.safeguard_flags`
- `review_status_before`
- `action_source = "batch_review"`

**Rules:**
- “Queryable” means the payload lives in `decisions.context_snapshot` as stable JSON keys addressable via SQLite `json_extract(...)`; do not bury the pattern in `note`.
- `content_flags` should reuse persisted scene metadata already on the segment row. In V1 this is primarily `safeguard_flags`; do not invent a second flag taxonomy.
- Future “Based on your past choices” hints are read-only deferred work for later Epic 8 stories. Story 8.2 only guarantees the write shape is stable and tested.

**Tests:**
- Store/handler test verifies `context_snapshot` JSON contains the required keys for a skip decision.
- Queryability test exercises `json_extract(context_snapshot, '$.scene_index')` and `json_extract(context_snapshot, '$.content_flags[0]')` against a seeded skip row.

---

### AC-4: Decision writes keep HITL session state and status polling in sync

**Given** a decision write succeeds  
**When** the backend commits the decision  
**Then** it calls `pipeline.UpsertSessionFromState(...)` for the run  
**And** subsequent status polling reflects updated `approved_count` / `rejected_count` / `pending_count` values without a page refresh.

**Rules:**
- Reuse the Story 2.6 helper exactly; do not duplicate HITL snapshot math in `service/` or `api/`.
- The decision write transaction must complete before session upsert is attempted.
- Skip still triggers session upsert even though V1 leaves `review_status` unchanged; that keeps `last_interaction_timestamp` fresh and preserves the canonical write-after-action contract from Story 2.6.
- If all scenes are resolved, `NextSceneIndex(...)` may return `totalScenes`; the UI should treat that as “all scenes reviewed” rather than as an out-of-bounds bug.

**Tests:**
- Service or handler test verifies `UpsertSessionFromState` is called once after approve/reject/skip.
- Status integration test verifies the returned / polled summary reflects newly approved/rejected scenes.

---

### AC-5: Keyboard-first action flow coexists cleanly with batch-review navigation

**Given** the batch-review surface is mounted  
**When** the operator uses keyboard shortcuts  
**Then** `Enter` approves, `Esc` rejects, `S` skips, and `Space` toggles audio  
**And** those shortcuts work without breaking the existing `J/K` navigation flow from Story 8.1  
**And** shortcuts are suppressed while focus is inside editable controls.

**Rules:**
- Register all shortcuts via `useKeyboardShortcuts`; do not add raw `window.addEventListener`.
- `Space` belongs to the audio player while batch-review is mounted; it must not scroll the page.
- Action buttons should show the inline keyboard hints from UX-DR13.
- Keep shortcut ownership local to the batch-review surface so other tabs do not inherit review controls.

**Tests:**
- Frontend integration test verifies `J/K` still navigates while action shortcuts mutate the selected scene.
- Test verifies shortcuts do not fire from focused inputs / textareas.
- Test verifies `Space` does not trigger approve/reject handlers.

## Tasks / Subtasks

- [x] **T1: Add canonical decision-write backend flow for batch review** (AC: #2, #3, #4)
  - Add action constants for missing decision types in `internal/domain/`:
    - `approve`
    - `reject`
    - `skip_and_remember`
  - Add a dedicated write input struct owned by the consumer layer, for example in `internal/service/scene_service.go` or a new `internal/service/review_decision.go`:
    - `RunID`
    - `SceneIndex`
    - `DecisionType`
    - `ContextSnapshot *string`
    - `Note *string`
  - Prefer a new review-decision method over widening `SceneService.EditNarration`.
  - Gate the write path to `batch_review/waiting`; return `ErrConflict` outside that state.

- [x] **T2: Extend `DecisionStore` with transactional scene decision recording** (AC: #2, #3)
  - Add a method such as `RecordSceneDecision(ctx, input)` in `internal/db/decision_store.go`.
  - Transaction responsibilities:
    - validate the segment exists
    - insert into `decisions`
    - update `segments.review_status` for approve/reject only
    - leave `review_status` unchanged for `skip_and_remember`
  - Use the existing `scene_id = strconv.Itoa(sceneIndex)` convention.
  - Add focused store tests for approve, reject, skip, nonexistent scene, and conflict-safe behavior.

- [x] **T3: Add service orchestration that updates HITL session after write** (AC: #2, #4)
  - Wire the service layer to call `pipeline.UpsertSessionFromState` after a successful store write.
  - Reuse the existing run loader and `clock.Clock` injection patterns already established elsewhere in the codebase; do not call `time.Now()` directly.
  - Keep the call sequence explicit: write decision first, then rebuild session snapshot.

- [x] **T4: Add API route + handler for `POST /api/runs/{id}/decisions`** (AC: #2, #3, #4)
  - Create or extend an API handler under `internal/api/` for the canonical decision path.
  - Request contract should validate:
    - `scene_index` is a non-negative integer
    - `decision_type` is one of `approve`, `reject`, `skip_and_remember`
    - `context_snapshot` is optional and only required for skip
  - Response should use the standard envelope and return enough data for the client to refresh or optimistically update the selected item.
  - Register the route centrally in `internal/api/routes.go`.

- [x] **T5: Extend batch-review frontend contracts and API client** (AC: #2, #3)
  - Add decision-write schemas to `web/src/contracts/runContracts.ts`.
  - Add `recordSceneDecision(run_id, payload)` to `web/src/lib/apiClient.ts`.
  - If Story 8.1 added `reviewItems` query keys, reuse them; otherwise add them as part of this story rather than piggybacking on `runs.scenes`.
  - Add contract tests for approve / reject / skip request and response shapes.

- [x] **T6: Build shared `AudioPlayer` and wire it into the detail panel** (AC: #1, #5)
  - Create `web/src/components/shared/AudioPlayer.tsx`.
  - Props should stay narrow and reusable:
    - `src`
    - `duration_ms?`
    - `scene_key`
  - Implement:
    - play / pause button
    - current time + duration labels
    - seekbar
    - reset-on-`scene_key` change
    - `Space` toggle via `useKeyboardShortcuts`
  - Mount it from Story 8.1’s `DetailPanel` instead of embedding raw `<audio>` logic there.

- [x] **T7: Add review action bar behavior to the batch-review frontend** (AC: #2, #5)
  - Extend Story 8.1’s `BatchReview` / `DetailPanel` flow so the selected scene can be approved, rejected, or skipped.
  - Add inline keyboard-hint buttons matching UX-DR13:
    - `[Enter] Approve`
    - `[Esc] Reject`
    - `[S] Skip`
    - keep `[Tab] Edit` and `[Ctrl+Z] Undo` present but non-functional if those stories are not yet implemented
  - On mutation success:
    - update or invalidate the review-items query
    - invalidate `queryKeys.runs.status(run.id)`
    - move selection to the next reviewable scene
    - preserve focus inside the batch-review surface

- [x] **T8: Build skip snapshot assembly in the UI or service boundary** (AC: #3)
  - Assemble the `skip_and_remember` `context_snapshot` from already-loaded review item data:
    - `scene_index`
    - `critic_score`
    - parsed `critic_sub`
    - `safeguard_flags` as `content_flags`
    - `review_status_before`
    - `action_source`
  - Prefer building this snapshot close to the canonical write boundary so all clients would send the same shape.
  - Serialize as stable JSON with predictable key names.

- [x] **T9: Add focused backend and frontend coverage** (AC: #1-#5)
  - Backend:
    - `decision_store_test.go`
    - handler tests for validation + write behavior
    - service tests for session upsert after write
  - Frontend:
    - `AudioPlayer.test.tsx`
    - batch-review action tests in `BatchReview.test.tsx` and/or `DetailPanel.test.tsx`
    - keyboard coexistence tests for `J/K`, `Enter`, `Esc`, `S`, `Space`

## Dev Notes

### Canonical write path: use the deferred Epic 8 endpoint

Story 2.6 already documented the intended write surface: `POST /api/runs/{id}/decisions`. Use that endpoint here. Do **not** scatter write logic across `POST /approve`, `POST /reject`, and `POST /skip` endpoints unless a later story explicitly changes the contract.

This matters because Story 8.3 undo and Story 8.6 history both depend on one canonical decision ledger. A single endpoint also makes the `pipeline.UpsertSessionFromState` hook impossible to forget.

### Skip semantics: deliberately lightweight in V1

The current persisted scene review states are:
- `pending`
- `waiting_for_review`
- `auto_approved`
- `approved`
- `rejected`

There is no first-class persisted `skipped` state yet. For Story 8.2:
- write `decision_type = 'skip_and_remember'`
- persist the queryable snapshot payload
- keep `segments.review_status` unchanged
- advance to the next scene in the mounted UI session

That keeps the data model stable for now and leaves richer skip/undo/focus restoration semantics to Story 8.3. Document this clearly in code comments so future contributors do not mistake it for an omission.

### Audio reset behavior

The UX requirement is specific: scene change resets the player to `0:00`, not “continue from prior scene” and not “auto-play next scene”. The least surprising implementation is:
- either key the `<audio>` element by `scene_key`
- or imperatively pause + set `currentTime = 0` when `scene_key` changes

Whichever approach is used, the rendered current-time label must also reset immediately.

### Shared component boundaries

- `AudioPlayer` belongs in `web/src/components/shared/`, not in `production/`.
- Action orchestration belongs in the batch-review owner component from Story 8.1.
- Do not widen `useUIStore` for transient selection / playback state.

### Optimistic mutation guidance

The planning artifacts explicitly allow optimistic UI only for high-frequency HITL decisions. Story 8.2 may optimistically move selection or mark the current row pending-success, but it must still invalidate the canonical status query after success so `decisions_summary` stays trustworthy.

Keep optimistic behavior narrow:
- okay: temporarily disable buttons, move local focus, style the scene as mutating
- not okay: fabricate new aggregate counts without reconciling against server state

### Queryable skip snapshot shape

Use stable JSON keys. Recommended payload:

```json
{
  "action_source": "batch_review",
  "content_flags": ["Safeguard Triggered: Minors"],
  "critic_score": 0.61,
  "critic_sub": {
    "emotional_variation": 0.42,
    "fact_accuracy": 0.83,
    "hook_strength": 0.58,
    "immersion": 0.47
  },
  "review_status_before": "waiting_for_review",
  "scene_index": 3
}
```

Do not store this as free-form prose in `note`. Future hinting needs predictable keys.

### Scope boundaries

- Reject reason prompt is Story 8.4, not this story.
- Undo stack wiring is Story 8.3, not this story.
- “Approve all remaining” is Story 8.5, not this story.
- Decision history UI is Story 8.6, not this story.
- Do not redesign Story 8.1’s read contract. This story layers audio + decision writes on top of it.

## References

- Epics: `_bmad-output/planning-artifacts/epics.md`
  - Epic 8 overview
  - Story 8.2 acceptance criteria
  - FR35, FR36, UX-DR12, UX-DR13, UX-DR18, UX-DR38, UX-DR55
- Architecture: `_bmad-output/planning-artifacts/architecture.md`
  - `decisions` table DDL
  - centralized route registration
  - shared component location guidance
- UX: `_bmad-output/planning-artifacts/ux-design-specification.md`
  - AudioPlayer spec
  - batch-review keyboard flow
  - skip-and-remember affordance
- Previous story: `_bmad-output/implementation-artifacts/8-1-master-detail-review-layout.md`
  - batch-review read surface and queue ownership
- Deferred work: `_bmad-output/implementation-artifacts/deferred-work.md`
  - canonical decision endpoint from Story 2.6

## Dev Agent Record

### Implementation Plan

- Add one canonical batch-review decision write path from API -> service -> DecisionStore and refresh HITL session state via `pipeline.UpsertSessionFromState(...)`.
- Extend the batch-review read contract with audio metadata and content flags so the detail panel can render a shared audio player and the skip action can emit a stable snapshot payload.
- Wire batch-review action mutations and keyboard shortcuts into the existing Story 8.1 surface, then verify backend, frontend, and build validation.

### Debug Log

- 2026-04-19: Implemented `POST /api/runs/{id}/decisions` with approve / reject / skip validation, transactional decision persistence, and post-write HITL session rebuild.
- 2026-04-19: Added shared `AudioPlayer`, spacebar shortcut support, batch-review action buttons/shortcuts, optimistic row updates, next-scene focus movement, and “all scenes reviewed” handling.
- 2026-04-19: Fixed adjacent TypeScript strictness issues surfaced by `npm run build` in batch-review-related tests/hooks so the story lands with a clean web build.

### Completion Notes

- Completed AC-1 through AC-5 with a canonical decision-write flow, queryable skip snapshot payloads, session upsert orchestration, shared audio playback UI, and keyboard-first batch-review actions.
- Added/updated backend coverage for decision recording, service orchestration, and handler validation; added frontend coverage for audio playback, skip/approve/reject action flow, keyboard coexistence, and contract parsing.
- Validation run completed successfully with `go test ./...`, `npm run lint -- src/components/shared/AudioPlayer.tsx src/components/shared/AudioPlayer.test.tsx src/components/shared/DetailPanel.tsx src/components/shared/DetailPanel.test.tsx src/components/production/BatchReview.tsx src/components/production/BatchReview.test.tsx src/contracts/runContracts.ts src/contracts/runContracts.test.ts src/lib/apiClient.ts src/lib/keyboardShortcuts.ts`, `npm run test:unit -- AudioPlayer BatchReview DetailPanel SceneCard runContracts`, and `npm run build`.

## File List

- cmd/pipeline/serve.go
- internal/api/handler_scene.go
- internal/api/handler_scene_test.go
- internal/api/routes.go
- internal/db/decision_store.go
- internal/db/decision_store_test.go
- internal/domain/review_gate.go
- internal/service/scene_service.go
- internal/service/scene_service_test.go
- testdata/contracts/run.review-items.response.json
- web/src/components/production/BatchReview.test.tsx
- web/src/components/production/BatchReview.tsx
- web/src/components/production/InlineNarrationEditor.test.tsx
- web/src/components/shared/AudioPlayer.test.tsx
- web/src/components/shared/AudioPlayer.tsx
- web/src/components/shared/DetailPanel.test.tsx
- web/src/components/shared/DetailPanel.tsx
- web/src/components/shared/SceneCard.test.tsx
- web/src/components/shells/ProductionShell.test.tsx
- web/src/contracts/runContracts.test.ts
- web/src/contracts/runContracts.ts
- web/src/hooks/useRunScenes.ts
- web/src/hooks/useRunStatus.test.tsx
- web/src/lib/apiClient.ts
- web/src/lib/keyboardShortcuts.ts

## Change Log

- 2026-04-19: Implemented Story 8.2 batch-review decision writes, shared audio playback, skip snapshot persistence, keyboard review actions, and associated backend/frontend/build validation.

### Review Findings

- [x] [Review][Decision] Story 8.1 read-surface files landed in 8.2 diff — `BatchReview.tsx`, `DetailPanel.tsx`, `SceneCard.tsx`, `testdata/contracts/run.review-items.response.json`, `GET /review-items` handler ([internal/api/handler_scene.go](../../internal/api/handler_scene.go)), `ListReviewItems` service, `reviewItemSchema`/`fetchBatchReviewItems` contracts are all new files belonging to Story 8.1 per this story's Prerequisites. **Resolved: split into 8.1-first commit, then 8.2 as a layered follow-up commit.**
- [x] [Review][Patch] `skip_and_remember` does not advance `next_scene_index` — `BuildSessionSnapshot` now maps skip decisions to a `"skipped"` pseudo-status so `NextSceneIndex` moves past them without changing `segments.review_status`. [internal/pipeline/hitl_session.go:68-79](../../internal/pipeline/hitl_session.go#L68-L79) + regression test in `hitl_session_test.go`.
- [x] [Review][Patch] Server now assembles `context_snapshot` from server-held segment state (`SceneService.buildSkipSnapshot`); the client-submitted payload is discarded, a test forges a bogus client snapshot and asserts server values win. [internal/service/scene_service.go](../../internal/service/scene_service.go) + `TestSceneService_RecordSceneDecision_BuildsSkipSnapshotFromServerState`.
- [x] [Review][Patch] `normalizeOptionalScore` — heuristic left unchanged for backward-compat with existing persisted scores; added an explicit doc block naming the ambiguity and pointing at the deferred-work consolidation task. [internal/service/scene_service.go:519](../../internal/service/scene_service.go).
- [x] [Review][Patch] AudioPlayer `Space` shortcut is always enabled; `togglePlayback` early-returns when `src` is missing, so page scroll is absorbed even in the unavailable state. [web/src/components/shared/AudioPlayer.tsx](../../web/src/components/shared/AudioPlayer.tsx) + new `absorbs space and prevents default` test.
- [x] [Review][Patch] Decision mutation now has `onError` that invalidates `reviewItems` + `runs.status` so the UI resyncs with server state when a post-commit step (`UpsertSessionFromState`) fails. [web/src/components/production/BatchReview.tsx](../../web/src/components/production/BatchReview.tsx).
- [x] [Review][Patch] `parseCriticBreakdown` now emits `slog.Warn` on malformed `critic_sub` JSON so ops can distinguish corrupted rows from absent data. [internal/service/scene_service.go](../../internal/service/scene_service.go).
- [x] [Review][Patch] `onSuccess` only snaps selection forward when `selected_scene_index` still equals the scene dispatched on the mutation — manual J/K between dispatch and response is preserved. [web/src/components/production/BatchReview.tsx](../../web/src/components/production/BatchReview.tsx).
- [x] [Review][Defer] `ListReviewItems` re-reads `scenario.json` from disk per request — caching/perf concern on the 8.1 read surface, not 8.2 write-path. [internal/service/scene_service.go:869-877](../../internal/service/scene_service.go) — deferred, out of 8.2 scope
- [x] [Review][Defer] `computeHighLeverage` silently masks scenario↔segment index drift with zero-value classifications — 8.1 read surface. [internal/service/scene_service.go:1049-1064](../../internal/service/scene_service.go) — deferred, out of 8.2 scope
- [x] [Review][Defer] `DetailPanel.buildDiffParts` is set-difference, not a real text diff; reordered or punctuation-attached words mislabel. 8.1 UX polish. [web/src/components/shared/DetailPanel.tsx:14-22](../../web/src/components/shared/DetailPanel.tsx#L14-L22) — deferred, out of 8.2 scope
