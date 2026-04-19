# Story 8.5: Batch "Approve All Remaining"

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want to approve all remaining unreviewed scenes at once,
so that I can move to final assembly faster when the remaining scenes are already good enough.

## Prerequisites

**Hard dependencies:** Story 8.1 defines the batch-review master-detail surface and keyboard navigation. Story 8.2 is expected to introduce the canonical scene decision write path plus action bar ownership for `Enter`, `Esc`, `S`, and `Space`. Story 8.3 defines the undo contract, including the rule that batch approve-all must be represented as one undoable aggregate action rather than N unrelated commands.

**Backend dependency:** Story 2.6 already established `pipeline.UpsertSessionFromState(...)` as the canonical HITL snapshot refresh after operator actions. Story 4.4 already established `segments.review_status` plus `system_auto_approved` decision behavior. Story 8.5 must layer on top of those primitives rather than inventing a second review summary mechanism.

**Architecture guardrail:** The architecture and UX materials still contain an older draft route (`POST /api/runs/{id}/approve-all`) and modal-style wording. For implementation, the sprint prompt and refined Epic 8 story set take precedence: use the current batch-review write stack, render an inline confirmation surface instead of an overlay modal, and preserve SQLite-backed decision history for undo.

**Parallel-work caution:** `web/src/components/shells/ProductionShell.tsx`, `web/src/index.css`, `web/src/lib/apiClient.ts`, `web/src/contracts/runContracts.ts`, `web/src/lib/queryKeys.ts`, `web/src/stores/useUIStore.ts`, `internal/api/routes.go`, `internal/service/scene_service.go`, and `internal/db/decision_store.go` are active integration points across Epic 8. Layer changes carefully and do not revert adjacent work from Stories 8.1-8.4.

## Acceptance Criteria

### AC-1: Shift+Enter opens an inline confirmation panel for batch approve

**Given** the operator is on a `batch_review/waiting` run and at least one unreviewed scene remains  
**When** they press `Shift+Enter` or activate the batch-approve affordance  
**Then** an `InlineConfirmPanel` appears inline within the current review context instead of an overlay modal  
**And** the panel is visually pushed upward by `60px` from the bottom action area  
**And** the panel announces itself with `role="alertdialog"`  
**And** focus moves into the panel and is trapped until the operator confirms or cancels.

**Rules:**
- The inline panel must support `Enter` to confirm and `Esc` to cancel.
- Focus trap must keep keyboard navigation inside the panel while it is open, including reverse-tab traversal.
- The trigger should be unavailable when there are zero remaining reviewable scenes.
- The confirmation copy should include the exact count of scenes that will be approved.

**Tests:**
- Component test verifies `Shift+Enter` opens the panel only when remaining scenes exist.
- Accessibility-focused test verifies `role="alertdialog"`, initial focus handoff, and focus trap behavior.
- Keyboard test verifies `Enter` confirms and `Esc` dismisses without leaking to underlying review shortcuts.

---

### AC-2: Confirming approves every remaining unreviewed scene and refreshes review state

**Given** the confirmation panel is open  
**When** the operator confirms the action  
**Then** every remaining unreviewed scene in the current batch-review run is updated to effective `Approved` state  
**And** the review counts, selected scene state, and status polling results refresh without a full page reload  
**And** if no unreviewed scenes remain afterward, the UI transitions to the existing “all reviewed / proceed” state instead of leaving stale pending cards visible.

**Rules:**
- “Remaining unreviewed” means only scenes still requiring manual review in the current run; already approved, rejected, skipped-only, or auto-approved scenes must not be rewritten.
- The action must be gated to `batch_review/waiting`; the backend returns `ErrConflict` / HTTP 409 for any other run stage or status.
- The implementation must call `pipeline.UpsertSessionFromState(...)` after successful persistence so HITL summary strings and counts stay authoritative.
- The client should invalidate or reconcile the same review-items and run-status queries used by Stories 8.1 and 8.2 rather than inventing a second refresh path.

**Tests:**
- Service/handler integration test verifies only remaining reviewable scenes are approved.
- Frontend integration test verifies the queue, counts, and end-state copy update after success.
- Handler test verifies non-`batch_review/waiting` runs return 409.

---

### AC-3: Batch persistence is processed in chunks of 50 scenes to reduce lock pressure

**Given** the run has many remaining scenes  
**When** the backend performs the batch approve mutation  
**Then** it processes the target scenes in chunks of at most `50` scenes per write pass  
**And** each chunk persists deterministically in scene-index order  
**And** the implementation avoids a single giant SQLite write burst that increases lock duration unnecessarily.

**Rules:**
- Chunk size is fixed at `50` for V1 and should be explicit in code, not an accidental side effect of pagination.
- The batch operation must remain idempotent for already-approved scenes by filtering the target set before chunking.
- If a chunk fails, the API must not report full success; return a failure that preserves truthful state reporting.
- Prefer storing a shared batch command identifier / aggregate metadata across all chunk writes so the operation can still be reasoned about as one logical action.

**Tests:**
- Store/service test verifies 120 target scenes are split into `50 + 50 + 20`.
- Reliability test verifies already-approved scenes are excluded before chunking.
- Failure-path test verifies a mid-operation error does not return a misleading success response.

---

### AC-4: Batch approve is registered as one undoable action

**Given** the operator successfully approves all remaining scenes  
**When** the action is written to the decision ledger and reflected in the client undo stack  
**Then** the entire batch approve is represented as one aggregate undoable command  
**And** `Ctrl+Z` from Story 8.3 can reverse the batch as one action rather than requiring one undo per scene.

**Rules:**
- The UI undo stack must receive exactly one command entry for the batch approve action.
- Backend persistence must keep enough aggregate metadata to identify every scene affected by the batch operation.
- Do not register one independent undo-stack item per approved scene.
- The aggregate command should preserve focus-restoration metadata for the review surface after undo.

**Tests:**
- Unit test verifies the UI store receives one batch command entry after batch approve success.
- Backend/service test verifies the persisted metadata links all chunked scene approvals to one logical batch command.
- Regression test verifies undo stack depth increases by `1`, not by the number of approved scenes.

---

### AC-5: Inline confirmation integrates cleanly with existing batch-review shortcuts and layout

**Given** the batch-review surface is already using `J/K`, `Enter`, `Esc`, `S`, `Tab`, and `Ctrl+Z`  
**When** the inline confirmation panel opens  
**Then** conflicting underlying shortcuts are temporarily suppressed  
**And** the panel remains visually anchored to the current action zone without obscuring the entire screen  
**And** dismissing the panel restores focus to the invoking review surface control.

**Rules:**
- Reuse the shared keyboard shortcut engine; do not add raw `window` listeners.
- The panel should follow the UX “Inline Confirmation Pattern,” not a generic modal or toast.
- Focus restoration after cancel should return to the batch-approve trigger or equivalent action-bar control.
- The push-up `60px` offset should be implemented in a way that still behaves correctly on narrower layouts.

**Tests:**
- Component test verifies underlying approve/reject shortcuts do not fire while the panel is open.
- Layout test verifies the panel renders inline with the expected offset class/behavior.
- Focus-restoration test verifies dismiss returns keyboard focus to the invoking control.

## Tasks / Subtasks

- [x] **T1: Add a canonical batch-approve service and API path** (AC: #2, #3, #4)
  - Extend the Epic 8 decision-write surface with a dedicated batch-approve operation rather than reviving the old draft `POST /approve-all` route blindly.
  - Add a service-layer entry point such as `SceneService.ApproveAllRemaining(ctx, runID string) (...)`.
  - Gate the action to `batch_review/waiting` and return `ErrConflict` outside that state.
  - Return enough response data for the client to refresh counts, remaining-scene totals, and undo metadata.

- [x] **T2: Extend `DecisionStore` with chunked batch-approve persistence** (AC: #2, #3, #4)
  - Add a write helper in `internal/db/decision_store.go` that:
    - resolves all remaining manually reviewable scenes
    - filters out scenes already approved/rejected/auto-approved
    - processes target scenes in deterministic chunks of `50`
    - writes approval decisions plus segment status updates
  - Persist shared aggregate metadata in `context_snapshot` (or equivalent existing field) so undo can reverse the whole batch as one command later.
  - Keep the implementation SQLite-friendly; do not issue one giant all-scenes mutation with no chunk boundary.

- [x] **T3: Refresh HITL session state and review queries after success** (AC: #2)
  - Call `pipeline.UpsertSessionFromState(...)` after the batch mutation completes.
  - Ensure `approved_count` / `pending_count` and the review summary string are immediately consistent with the new database state.
  - Invalidate the review-items and run-status queries already used by the batch-review frontend.

- [x] **T4: Build `InlineConfirmPanel` for batch approve** (AC: #1, #5)
  - Create or extend a shared inline confirmation component near the batch-review action surface.
  - Required behavior:
    - push-up `60px`
    - `role="alertdialog"`
    - focus trap
    - `Enter` confirm
    - `Esc` cancel
    - exact remaining-scene count in the copy
  - Keep it inline and local to the review surface; do not use a blocking modal overlay.

- [x] **T5: Wire `Shift+Enter` and action-bar affordance into batch review** (AC: #1, #5)
  - Register `Shift+Enter` through `useKeyboardShortcuts`.
  - Suppress underlying scene approve/reject shortcuts while the confirmation panel is open.
  - Disable or hide the trigger when there are zero remaining reviewable scenes.
  - Restore focus to the invoking control on cancel.

- [x] **T6: Register one aggregate undo-stack entry on success** (AC: #4)
  - Extend `web/src/stores/useUIStore.ts` with the batch-approve command shape if Story 8.3 has not landed yet.
  - Push exactly one `approve_all_remaining` command entry after successful batch approve.
  - Include serializable metadata such as `run_id`, affected `scene_indices`, focus target, and aggregate command id.

- [x] **T7: Add focused backend and frontend coverage** (AC: #1-#5)
  - Backend:
    - `decision_store_test.go`: chunking at 50, filtering target scenes, aggregate metadata persistence
    - service tests: stage gating, HITL session refresh, mid-chunk failure handling
    - handler tests: success path and 409 conflict
  - Frontend:
    - `InlineConfirmPanel` tests: `alertdialog`, focus trap, Enter/Esc, focus restore
    - batch-review tests: `Shift+Enter` open, confirm success, shortcut suppression while panel is open
    - `useUIStore.test.ts`: single batch undo entry

## Dev Notes

### Story Intent and Scope Boundary

- Story 8.5 implements the bulk-approve interaction for the batch-review surface.
- Do not add a full-screen modal, toast-only confirmation, or separate confirmation route.
- Do not fragment the logical action into one undo entry per scene.
- Do not widen this story into Phase C start/metadata acknowledgment; it ends at “remaining scenes approved and review state refreshed.”

### Current Codebase Reality

| What | Where | State |
|---|---|---|
| Batch-review UI surface | `web/src/components/production/BatchReview.tsx` | Not present in current tree yet |
| Inline confirmation component | `web/src/components/` | Not present in current tree yet |
| UI store | `web/src/stores/useUIStore.ts` | Exists, but currently only persists onboarding/sidebar/last-seen state |
| Scene service | `internal/service/scene_service.go` | Currently scoped to scenario-review reads + narration edits only |
| Decision store | `internal/db/decision_store.go` | Supports batch-review preparation and decision reads, but not operator batch approve writes |
| Scene routes | `internal/api/routes.go` | No batch-review write endpoint yet |
| Undo aggregate contract | Story 8.3 document | Defined, but not implemented in current tree |

### Recommended Persistence Shape

Batch approve needs two layers of truth:

1. **Per-scene persistence**
   - each affected scene still needs an authoritative approval decision row and `segments.review_status = 'approved'`
   - this preserves existing counts, summaries, and scene-level reporting
2. **Aggregate command metadata**
   - a shared batch command id or equivalent payload must link the affected scenes together
   - this preserves the Story 8.3 rule that the batch is undone as one logical action

Prefer using existing decision fields such as `context_snapshot` to carry aggregate metadata before introducing a new migration.

### Chunking Guidance

Chunking is a database-pressure mitigation, not a semantic split:

- compute the full target scene list first
- divide it into slices of at most `50`
- persist chunks in ascending `scene_index` order
- attach the same aggregate command metadata to every affected scene decision

This gives the implementation predictable lock behavior while still letting Story 8.3 reverse the batch as one user action.

### Inline Confirmation Guidance

The UX spec explicitly calls for the shared **Inline Confirmation Pattern**:

- inline panel anchored to the current action area
- no blocking overlay modal
- `Enter` = confirm
- `Esc` = cancel
- focus trap while open

Treat this panel as part of the batch-review interaction loop, not as a generic app-wide dialog.

### Suggested File Touches

Likely backend files:
- `internal/api/routes.go`
- `internal/api/handler_scene.go` or a dedicated batch-review handler file
- `internal/service/scene_service.go`
- `internal/db/decision_store.go`
- `internal/db/decision_store_test.go`
- `internal/service/scene_service_test.go`

Likely frontend files:
- `web/src/components/production/BatchReview.tsx`
- `web/src/components/shared/ActionBar.tsx` or a new `InlineConfirmPanel.tsx`
- `web/src/components/shared/ProductionShortcutPanel.tsx`
- `web/src/stores/useUIStore.ts`
- `web/src/stores/useUIStore.test.ts`
- `web/src/lib/apiClient.ts`
- `web/src/lib/queryKeys.ts`
- `web/src/contracts/runContracts.ts`
- `web/src/index.css`

## Open Questions / Saved Clarifications

1. Stories 8.1-8.3 are documented but not yet implemented in the current tree. If Story 8.5 is developed before those land, the dev pass will need to include the minimal missing review-surface scaffolding instead of assuming it already exists.
2. The architecture document still shows an older dedicated `approve-all` route. The implementation should reconcile that with the newer Epic 8 canonical decision-ledger direction rather than copying the outdated route shape without review.
3. If partial chunk success is considered unacceptable for operator UX, implementation may need an explicit compensation strategy; this story requires at minimum that the API never reports false full success and that aggregate undo metadata remains coherent.

## Dev Agent Record

### Debug Log

- Added `POST /api/runs/{id}/approve-all-remaining` and `SceneService.ApproveAllRemaining(...)` to the canonical batch-review write path.
- Implemented chunked `DecisionStore.ApproveAllRemaining(...)` with shared aggregate metadata in `context_snapshot`, then extended undo to reverse the full batch as one logical action.
- Added `InlineConfirmPanel` plus `Shift+Enter` wiring in `BatchReview.tsx`, suppressing underlying review shortcuts while the panel is open and restoring focus on cancel.

### Completion Notes

- Batch approve now targets only `pending` and `waiting_for_review` scenes, chunked at `50`, and refreshes HITL session state via `pipeline.UpsertSessionFromState(...)`.
- The frontend registers one `approve_all_remaining` undo entry with aggregate command metadata and invalidates the same review-items/status queries used by the rest of the batch-review surface.
- Added backend and frontend coverage for chunking, stage gating, aggregate undo behavior, alertdialog semantics, focus trapping, keyboard confirm/cancel, and single-entry undo stack behavior.

### File List

- `internal/api/handler_scene.go`
- `internal/api/handler_scene_test.go`
- `internal/api/routes.go`
- `internal/db/decision_store.go`
- `internal/db/decision_store_test.go`
- `internal/service/scene_service.go`
- `internal/service/scene_service_test.go`
- `web/src/components/production/BatchReview.tsx`
- `web/src/components/production/BatchReview.test.tsx`
- `web/src/components/shared/InlineConfirmPanel.tsx`
- `web/src/components/shared/InlineConfirmPanel.test.tsx`
- `web/src/contracts/runContracts.ts`
- `web/src/index.css`
- `web/src/lib/apiClient.ts`
- `web/src/stores/useUIStore.ts`

### Change Log

- Added aggregate batch-approve API/service/store support with chunked scene approvals and batch-aware undo semantics.
- Added inline batch approve confirmation UI and keyboard shortcut integration in the batch-review surface.
- Added focused Go and Vitest coverage for the new batch approve flow.

### Review Findings

- [x] [Review][Patch] Aggregate command ID collides under FakeClock / rapid calls (UnixNano only) [internal/service/scene_service.go:563]
- [x] [Review][Patch] `skip_and_remember` scenes re-approved by ApproveAllRemaining — violates AC-2 Rules #1 [internal/db/decision_store.go:405-414]
- [x] [Review][Patch] Zero-target batch returns success with phantom aggregate_command_id, pushing a bogus undo entry [internal/db/decision_store.go:432-439, web/src/components/production/BatchReview.tsx:204-223]
- [x] [Review][Patch] `focus_scene_index` never validated against actual target set (snap to approved member) [internal/db/decision_store.go:441-443]
- [x] [Review][Patch] InlineConfirmPanel Enter fires `on_confirm` from any focused element (incl. Cancel) [web/src/components/shared/InlineConfirmPanel.tsx:43-47]
- [x] [Review][Patch] InlineConfirmPanel keydown ignores `is_confirming`, allowing double-submit [web/src/components/shared/InlineConfirmPanel.tsx:36-47]
- [x] [Review][Patch] InlineConfirmPanel missing `aria-modal="true"` on alertdialog [web/src/components/shared/InlineConfirmPanel.tsx:77]
- [x] [Review][Patch] InlineConfirmPanel hardcoded element IDs (collision risk) — use `useId` [web/src/components/shared/InlineConfirmPanel.tsx:73-86]
- [x] [Review][Patch] InlineConfirmPanel focus trap collapses when both buttons disabled [web/src/components/shared/InlineConfirmPanel.tsx:53-56]
- [x] [Review][Patch] closeApproveAllPanel focuses missing/disabled trigger (retry-exhausted, zero-remaining) [web/src/components/production/BatchReview.tsx:347-350]
- [x] [Review][Patch] AC-3 Tests #1: 50+50+20 chunk split not asserted in decision_store_test.go
- [x] [Review][Patch] AC-3 Tests #3: mid-chunk failure test missing in decision_store_test.go
- [x] [Review][Patch] AC-4 Tests #1: useUIStore.test.ts missing single-batch-undo-entry assertion
- [x] [Review][Patch] AC-1 Tests #1: negative-case (zero remaining) Shift+Enter test missing in BatchReview.test.tsx
- [x] [Review][Patch] AC-1 Tests #2: forward-tab focus-trap not exercised in InlineConfirmPanel unit test
- [x] [Review][Defer] RetryExhausted threshold mismatch between ListReviewItems (`>=`) and RecordSceneDecision (`>`) — deferred, belongs to Story 8.4 [internal/service/scene_service.go:2705 vs 2817]
- [x] [Review][Defer] `CountRegenAttempts` counts superseded rejects while `ReviewItem.RegenAttempts` doc says "non-superseded" — deferred, pre-existing from 8.4
- [x] [Review][Defer] N+1 sub-queries in `ListReviewItems` per scene — deferred, performance optimization out of scope
- [x] [Review][Defer] Snapshot blob duplicated per decision row (O(N²) storage) — deferred, schema change, requires migration
- [x] [Review][Defer] Decision inserts one-row-at-a-time inside chunk loop (chunking only batches UPDATE) — deferred, still bounded by chunk-size lock window
- [x] [Review][Defer] No `aria-modal` / `role="alertdialog"` re-check inside the approve-all tx — deferred, SQLite single-writer contract holds
- [x] [Review][Defer] `ctrl+z` only binding (no `cmd+z` on macOS) — deferred, keyboard engine design is Story 6.3
- [x] [Review][Defer] `undo_stacks` never garbage-collected across runs — deferred, bounded by session lifetime
- [x] [Review][Defer] Narrow-layout `translateY(0)` silently drops push-up offset — deferred, acknowledged design call
- [x] [Review][Defer] Persistence of undo stack across reload (reloading loses server-undoable state) — deferred, UX polish
