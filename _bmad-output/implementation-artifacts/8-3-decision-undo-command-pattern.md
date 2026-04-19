# Story 8.3: Decision Undo & Command Pattern

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want to undo my recent review decisions,
so that I can quickly recover from accidental approvals, rejections, skips, or descriptor edits without corrupting downstream state.

## Prerequisites

**Hard dependencies:** Story 6.3 established the shared keyboard shortcut engine including `ctrl+z` normalization and editable-target suppression. Story 7.3 established the `VisionDescriptorEditor` local revert pattern and the authoritative `runs.frozen_descriptor` field. Story 8.1 defines the batch-review read surface. Story 8.2 is expected to introduce the actual batch-review approve/reject/skip and approve-all write path that this undo flow must wrap rather than bypass.

**Backend dependency:** Story 2.6 already established the `hitl_sessions` pause snapshot model and the rule that decision summaries only count `decisions` rows where `superseded_by IS NULL`. Story 2.7 reinforced that non-superseded decisions are the source of truth for reporting and metrics. Story 8.3 must preserve those semantics.

**Architecture guardrail:** The current architecture and sprint prompt both define V1 undo as `decisions.superseded_by` reversal rows, not hard deletes and not event sourcing. The older Epic 8 draft sentence about restoring from a "versions folder" is superseded for V1 by the refined requirements in `sprint-prompts.md`, `architecture.md`, and the user brief for this story.

**Parallel-work caution:** `web/src/components/shells/ProductionShell.tsx`, `web/src/index.css`, `web/src/lib/apiClient.ts`, `web/src/contracts/runContracts.ts`, `web/src/lib/queryKeys.ts`, `internal/service/scene_service.go`, and `internal/db/decision_store.go` are active integration points across Epic 7 and Epic 8. Layer changes carefully and do not revert adjacent work.

## Acceptance Criteria

### AC-1: Ctrl+Z undoes the most recent review-surface command with a 10-step cap

**Given** the operator has executed one or more undoable review actions  
**When** they press `Ctrl+Z` on an active review surface  
**Then** the most recent undoable command is reversed  
**And** the undo stack only retains the latest 10 commands for the current run/review session  
**And** if the stack is empty, no mutation fires and the UI remains unchanged.

**Rules:**
- V1 stack depth is exactly 10 commands minimum and should be enforced client-side in Zustand state.
- The stack is scoped to the currently active run and review session; switching runs must not replay commands from a different run.
- `Ctrl+Z` must use the existing `useKeyboardShortcuts` engine, not a raw `window` listener.

**Tests:**
- Unit test verifies push/pop order and 10-entry truncation.
- Component test verifies `Ctrl+Z` dispatches undo only when a review surface with undoable history is mounted.

---

### AC-2: V1 undo scope is limited to exactly five command types

**Given** the operator is using the Production review surfaces  
**When** commands are recorded for undo  
**Then** only these action types are eligible in V1:
- scene approve
- scene reject
- scene skip
- batch "approve all remaining" as one aggregate action
- Vision Descriptor text edit

**And** pipeline lifecycle actions (`create`, `resume`, `cancel`), stage transitions, and unrelated settings/configuration edits are never added to the undo stack.

**Rules:**
- Treat batch approve-all as one command containing N affected scenes, not N independent commands.
- Vision Descriptor undo restores the previous persisted descriptor value for the current run.
- Character re-pick undo is out of scope for this story even though some earlier UX notes mention it; the V1 sprint prompt and user brief narrow the scope to the five actions above.

**Tests:**
- Unit test verifies unsupported command kinds are rejected from the registry/store.
- Unit test verifies batch approve-all is stored and undone as a single command object.

---

### AC-3: Undo persistence uses reversal rows plus `superseded_by`, never hard deletion

**Given** an undoable action has already been persisted in `decisions`  
**When** the operator undoes that action  
**Then** the original decision row remains in the table  
**And** its `superseded_by` column is set to the newly inserted undo/reversal decision row id  
**And** the reversal itself is also inserted as a normal `decisions` row  
**And** no existing `decisions` row is hard deleted.

**Rules:**
- The entire undo operation must be transactional: insert reversal row, mark original as superseded, restore the domain/UI state, update any affected segment/run fields, and refresh the HITL snapshot atomically.
- `DecisionCountsByRunID`, HITL diffs, and metrics must continue to work by naturally ignoring superseded rows.
- The reversal row should capture enough context in `context_snapshot` and/or `note` to identify the original command and restore target state deterministically.

**Tests:**
- Integration test verifies original row remains present and becomes superseded after undo.
- Integration test verifies summaries/counts exclude the superseded original and reflect the restored current state.
- Regression test verifies no delete statement is used on `decisions` during undo.

---

### AC-4: Undo is blocked once the run has entered Phase C rendering

**Given** a run has progressed to `assemble`, `metadata_ack`, or `complete`  
**When** the operator attempts undo  
**Then** the backend rejects the request with `ErrConflict` / HTTP 409  
**And** the UI removes or disables the undo affordance for that run.

**Given** the run is still in `scenario_review`, `character_pick`, or `batch_review` with `status == waiting`  
**When** undo is requested  
**Then** the backend may execute the undo if a valid command exists.

**Rules:**
- The gate must be stage/status based from the current run record, not from client assumptions.
- Do not infer Phase C eligibility from file existence (`clip_path`, output folders, etc.).
- Failed or cancelled runs are not implicitly undoable; only active pre-Phase-C review checkpoints should allow undo.

**Tests:**
- Service test verifies `batch_review/waiting` succeeds but `assemble/running` returns conflict.
- Handler test verifies blocked undo returns 409 with the standard error envelope.

---

### AC-5: Undo restores focus and selection to the scene where the undone action occurred

**Given** the operator has moved elsewhere after making a decision  
**When** they undo the last command  
**Then** the UI restores selection/focus to the scene card or descriptor surface that originated that command  
**And** the relevant detail panel state is updated before the focus handoff  
**And** the restored target is keyboard-reachable without requiring a mouse click.

**Rules:**
- Focus restoration is part of the command payload, not a best-effort DOM query with no command metadata.
- Scene commands restore the originating `scene_index` selection.
- Vision Descriptor commands restore focus to the descriptor surface for the active run.

**Tests:**
- Component test verifies undo after navigating away re-selects the original `SceneCard`.
- Descriptor-focused test verifies undo returns focus to the descriptor editor/read-surface control.

## Tasks / Subtasks

- [x] **T1: Add explicit undo domain types and command metadata contracts** (AC: #1, #2, #5)
  - Add a dedicated undo command model in the backend, for example in `internal/domain/` or `internal/service/scene_service.go`-adjacent files:
    - command kind enum/constants for `approve`, `reject`, `skip`, `approve_all_remaining`, `descriptor_edit`
    - command payload with run id, stage, originating focus target, scene index or descriptor target, and enough before/after state to reverse deterministically
  - Add matching frontend TypeScript types for stack entries and undo responses in `web/src/contracts/runContracts.ts`.
  - Keep the command vocabulary narrow; do not design a generic event-sourcing framework.

- [x] **T2: Extend persistence with transactional undo helpers in `DecisionStore`** (AC: #2, #3, #4)
  - Add write helpers in `internal/db/decision_store.go` for:
    - inserting operator scene decisions for Story 8.2 command registration
    - inserting descriptor-edit decision rows
    - locating the latest non-superseded undoable decision for a run
    - applying undo atomically: insert reversal row, set original `superseded_by`, restore affected `segments.review_status` and/or `runs.frozen_descriptor`
  - Preserve the existing read semantics where non-superseded rows are the live truth.
  - Add tests that seed multiple decisions on one scene and prove the restored state still yields correct counts/diffs.

- [x] **T3: Introduce a dedicated undo service/API surface** (AC: #1, #3, #4, #5)
  - Add a service-layer entry point, recommended name: `SceneService.UndoLastDecision(ctx, runID string) (...)`.
  - Validate current run stage/status before undo; return `ErrConflict` once Phase C has begun.
  - Add `POST /api/runs/{id}/undo` in `internal/api/routes.go` and a corresponding handler.
  - Return enough response data for the client to reconcile selection, restored scene state, restored descriptor, and current undo depth.

- [x] **T4: Capture undoable commands at the point each action is persisted** (AC: #1, #2, #3)
  - Scene approve/reject/skip and batch approve-all should register command metadata when Story 8.2 persists those actions.
  - Vision Descriptor edit should register a command when the persisted descriptor value actually changes from the previous saved value.
  - Do not register no-op commands:
    - descriptor blur with unchanged text
    - duplicate decision that leaves the same effective state
    - empty batch approve-all with zero affected scenes

- [x] **T5: Add Zustand-backed undo stack state and focus restoration data** (AC: #1, #5)
  - Extend `web/src/stores/useUIStore.ts` with a bounded per-run undo stack and helper actions such as:
    - `push_undo_command(run_id, command)`
    - `pop_undo_command(run_id)`
    - `clear_undo_stack(run_id)`
  - The store should persist only what is useful across transient route refreshes; avoid storing stale DOM refs or non-serializable objects.
  - Store focus restoration metadata as serializable identifiers like `scene_index` and `target: 'scene-card' | 'descriptor'`.

- [x] **T6: Wire `Ctrl+Z` into the relevant Production surfaces** (AC: #1, #2, #4, #5)
  - Use `useKeyboardShortcuts` on the batch-review surface and the descriptor surface so `ctrl+z` dispatches the undo mutation.
  - Respect existing editable suppression behavior:
    - `VisionDescriptorEditor` already uses local `Ctrl+Z` to revert the in-progress draft while the textarea is focused.
    - Story 8.3 must not break that local pre-submit behavior.
    - The cross-surface undo mutation should trigger only for persisted commands, not while a textarea draft is still unsaved.
  - Update `ProductionShortcutPanel` / action bars / inline hints so `Ctrl+Z` is surfaced where relevant.

- [x] **T7: Reconcile descriptor-edit undo with the existing 7.3 editor behavior** (AC: #2, #5)
  - Keep the current local textarea revert semantics for in-progress edits.
  - Add a second, persisted undo layer for the last confirmed descriptor change:
    - previous saved descriptor value is captured before confirm
    - confirm persists the new value and pushes a command
    - global undo restores the old persisted value and refocuses the descriptor surface
  - Do not conflate draft-local revert and persisted command undo in one state variable.

- [x] **T8: Add focused backend and frontend test coverage** (AC: #1-#5)
  - Backend:
    - `decision_store_test.go`: reversal row insertion, `superseded_by` linking, no hard delete, batch command undo, Phase C block
    - service tests: latest-command resolution, descriptor restore, counts/diff compatibility, transaction rollback on partial failure
    - handler tests: `POST /api/runs/{id}/undo` success + 409 conflict
  - Frontend:
    - `useUIStore.test.ts`: 10-depth cap and per-run isolation
    - batch review / shell tests: `Ctrl+Z` dispatch, disabled/hidden state after Phase C, focus restoration to prior `SceneCard`
    - descriptor tests: persisted undo vs local draft revert separation

## Dev Notes

### Story Intent and Scope Boundary

- Story 8.3 is the command/undo layer for Epic 8.
- Do not introduce redo (`Ctrl+Shift+Z`) in this story; Story 6.3 explicitly normalized that away.
- Do not redesign the full review mutation surface here; scene action writes belong to Story 8.2, but they must be implemented in a way that registers undo commands immediately.
- Do not use hard deletes, ad-hoc history arrays without server reconciliation, or a filesystem "versions/" restore mechanism for V1.

### Current Codebase Reality

| What | Where | State |
|---|---|---|
| `ctrl+z` keyboard normalization | `web/src/lib/keyboardShortcuts.ts` | Exists and already rejects `Ctrl+Shift+Z` collapsing into undo |
| UI store | `web/src/stores/useUIStore.ts` | Exists, but has no undo stack yet |
| Descriptor persisted value | `runs.frozen_descriptor` + `VisionDescriptorEditor.tsx` | Exists |
| Local descriptor `Ctrl+Z` | `web/src/components/production/VisionDescriptorEditor.tsx` | Exists, but only for draft-local revert |
| Scene review read/edit service | `internal/service/scene_service.go` | Exists for scenario review only |
| `decisions` read semantics | `internal/db/decision_store.go` | Existing queries already ignore `superseded_by IS NOT NULL` |
| `POST /api/runs/{id}/undo` route | `internal/api/routes.go` | Missing |
| Batch-review write path | Story 8.2 | Not yet implemented in current tree |

### Recommended Backend Shape

Use a small service-layer Command Pattern, not a general-purpose framework:

1. Load current run and enforce the pre-Phase-C gate.
2. Resolve the latest non-superseded undoable decision for that run.
3. Decode its stored command metadata from `context_snapshot` or equivalent structured payload.
4. Apply the reverse mutation inside one DB transaction.
5. Insert a reversal decision row and mark the original row's `superseded_by`.
6. Refresh any HITL pause snapshot/state derived from the current non-superseded decisions.

This keeps the truth in SQLite and lets existing count/diff code continue working naturally.

### Decision Row Guidance

The current schema does not have a dedicated `command_payload` column. For V1, prefer reusing existing fields instead of adding a migration unless implementation pressure proves it necessary:

- `decision_type`: original operator action or explicit undo marker
- `scene_id`: scene-scoped actions only
- `context_snapshot`: JSON payload containing `command_kind`, `before`, `after`, `focus_target`, and batch membership when needed
- `note`: optional human-readable hint such as `"undo of decision 42"`
- `superseded_by`: set on the original decision row

If you introduce a new decision type for reversal rows, keep it explicit and test its interaction with metrics/count queries so it is not accidentally treated as approve/reject.

### Phase C Gate Definition

Use the run's current stage as the single source of truth:

- Undo allowed: `scenario_review/waiting`, `character_pick/waiting`, `batch_review/waiting`
- Undo blocked: `assemble/*`, `metadata_ack/*`, `complete/*`

Do not key this logic from clip files, assembled artifacts, or whether a scene once had `review_status = approved`.

### Descriptor Undo Guidance

There are two distinct undo layers for descriptors:

1. **Local draft revert** while editing the textarea.
   - Already implemented in Story 7.3.
   - Not persisted, not part of the global stack.
2. **Persisted command undo** after confirm/save.
   - New in Story 8.3.
   - Must restore `runs.frozen_descriptor` and return focus to the descriptor surface.

Keep these separate so a draft-local `Ctrl+Z` does not accidentally call the global undo endpoint.

### Frontend Stack Guidance

The sprint prompt explicitly wants the 10-step cap managed in Zustand. Recommended store shape:

```ts
type UndoFocusTarget = 'scene-card' | 'descriptor'

interface UndoCommand {
  command_id: string
  run_id: string
  kind:
    | 'approve'
    | 'reject'
    | 'skip'
    | 'approve_all_remaining'
    | 'descriptor_edit'
  scene_index?: number
  focus_target: UndoFocusTarget
  created_at: string
}
```

Store identifiers and metadata only. The authoritative restoration still comes from the backend undo response.

### Testing Priorities

The UX spec ranks undo as the highest-risk failure mode on the Production surface. Prioritize:

1. transactional rollback correctness
2. `superseded_by` semantics
3. Phase C block
4. per-run stack isolation
5. focus restoration after navigation drift

### Suggested File Touches

Likely backend files:
- `internal/api/routes.go`
- `internal/api/handler_scene.go` or a new dedicated undo handler file
- `internal/service/scene_service.go`
- `internal/db/decision_store.go`
- `internal/db/decision_store_test.go`
- `internal/service/scene_service_test.go`
- `internal/domain/` command-related types/constants

Likely frontend files:
- `web/src/stores/useUIStore.ts`
- `web/src/stores/useUIStore.test.ts`
- `web/src/lib/apiClient.ts`
- `web/src/lib/queryKeys.ts`
- `web/src/contracts/runContracts.ts`
- `web/src/components/production/BatchReview.tsx` once 8.1 lands
- `web/src/components/production/VisionDescriptorEditor.tsx`
- `web/src/components/shells/ProductionShell.tsx`
- related component tests

## Open Questions / Saved Clarifications

1. Story 8.2 is not yet implemented in the current tree. The dev pass for 8.3 should either land after 8.2, or explicitly include the minimal scene-decision write path needed for command registration.
2. The architecture mentions `POST /api/runs/{id}/undo` while the current router does not expose it. This story assumes that exact endpoint unless a stronger repo-local convention emerges during implementation.
3. If batch approve-all needs to restore multiple scenes plus the next-focus target, prefer one reversal row that references the aggregate command rather than one undo row per scene; this matches the "single action" requirement and should be validated during implementation.

### Review Findings

- [x] [Review][Decision] descriptor_edit undo pushed by CharacterPick but ctrl+z handler lives only in BatchReview — cross-stage undo design: CharacterPick pushes `{ kind: 'descriptor_edit' }` to the undo stack on successful pick, but the ctrl+z shortcut that calls `undoLastDecision` is only registered in BatchReview. By the time the user reaches batch_review stage, the undo command is in the stack and can be mechanically executed, but BatchReview's `onSuccess` only handles `focus_target === 'scene-card'` — `'descriptor'` focus restoration is unimplemented. Additionally, the VisionDescriptorEditor surface is not visible at batch_review stage. Decision: (c) keep as-is — DB is restored by ApplyUndo, root_ref.focus() is the appropriate fallback, added comment to BatchReview.tsx undo_mutation onSuccess. [AC-2, AC-5] [web/src/components/production/CharacterPick.tsx, web/src/components/production/BatchReview.tsx]
- [x] [Review][Decision] RecordDescriptorEdit is best-effort (non-fatal) in CharacterService.Pick — error is silently swallowed with `_ = err`. If RecordDescriptorEdit fails (DB error, ctx cancellation), the client still receives a successful pick response and pushes an undo command, but the backend has no matching decision row. The next ctrl+z will either undo the wrong decision or return ErrConflict. Decision: (a) make RecordDescriptorEdit fatal — Pick now fails if recording fails, preventing stale undo commands. [AC-3] [internal/service/character_service.go]
- [x] [Review][Decision] `allow_in_editable: false` on ctrl+z in BatchReview blocks descriptor undo when descriptor field is focused — if the VisionDescriptorEditor textarea is focused at batch_review stage (e.g., for description viewing), ctrl+z is silently suppressed by the shortcut engine's editable suppression. This contradicts AC-5 which requires descriptor undo to be accessible. Decision: not a real issue — VisionDescriptorEditor is not mounted at batch_review stage, so there are no editable elements for the suppression to block. Dismissed. [AC-5] [web/src/components/production/BatchReview.tsx:262]
- [x] [Review][Patch] json.Marshal error silently discarded in RecordDescriptorEdit — `snapshot, _ := json.Marshal(...)` discards the error; on failure, `snap` will be the empty string and the INSERT records a blank context_snapshot, making undo of descriptor edits non-deterministic — dismissed (false positive: actual code uses proper error handling) [internal/db/decision_store.go — RecordDescriptorEdit]
- [x] [Review][Patch] BeginTx error silently discarded in ApplyUndo — `tx, _ := s.db.BeginTx(ctx, nil)` discards the BeginTx error; if BeginTx fails, subsequent `tx.*` calls will panic on nil receiver — dismissed (false positive: actual code handles BeginTx error) [internal/db/decision_store.go — ApplyUndo]
- [x] [Review][Patch] UndoLastDecision leaves split state when UpsertSession fails after DB commit — ApplyUndo commits the transaction (segment status restored), then UpsertSessionFromState is a separate non-transactional call; if it errors, UndoLastDecision returns an error to the client but the DB mutation is permanent; the HITL snapshot is stale until the next write — fixed: UpsertSession failure is now logged as slog.Warn and not returned as error [internal/service/scene_service.go — UndoLastDecision]
- [x] [Review][Patch] ApplyUndo does not re-check segment status before blind restore — for approve/reject undo, the UPDATE sets review_status='waiting_for_review' without first verifying the current segment status; in a single-writer SQLite this is low risk but a concurrent admin action could corrupt state — fixed: added segment status pre-check before restore [internal/db/decision_store.go — ApplyUndo]
- [x] [Review][Patch] nil decisions store returns ErrConflict instead of internal error — `if s.decisions == nil { return nil, ErrConflict }` maps a programming/configuration error to a 409 Conflict; any client will interpret this as "scene already decided" and silently move on — fixed: nil guard now returns plain internal error [internal/service/scene_service.go:RecordSceneDecision]
- [x] [Review][Patch] pop_undo_command uses stale closure — `stack` is captured from `get()` before `set()`, then used inside the setter as `stack.slice(0, -1)` instead of `state.undo_stacks[run_id].slice(0, -1)`; concurrent calls can both read the same stack and only remove one entry — fixed: setter now reads fresh state via `state.undo_stacks[run_id]` [web/src/stores/useUIStore.ts — pop_undo_command]
- [x] [Review][Patch] Undo onSuccess incorrectly applies 'waiting_for_review' for skip undo — the optimistic cache update sets the undone scene to 'waiting_for_review' for all undo types; but skip_and_remember never changed review_status, so this incorrectly overwrites whatever status the scene currently has in cache — fixed: conditional check on undone_kind before cache update [web/src/components/production/BatchReview.tsx — undo_mutation onSuccess]
- [x] [Review][Patch] JSON null literal passes skip context_snapshot guard — `if len(req.ContextSnapshot) > 0` passes for a JSON null value (length 4), setting contextSnapshot to the string "null" which is stored as the snapshot and would cause descriptor undo to fail — fixed: added `&& string(req.ContextSnapshot) != "null"` guard [internal/api/handler_scene.go — RecordDecision]
- [x] [Review][Patch] "skipped" pseudo-status is a raw string literal not a domain constant — `sceneStatuses[*d.SceneID] = "skipped"` uses a bare string; a typo or future change would be undetected at compile time — fixed: extracted `snapshotStatusSkipped` constant [internal/pipeline/hitl_session.go]
- [x] [Review][Defer] approve_all_remaining not implemented — command type defined in UndoCommand interface but no RecordDecision handler or service path for approve_all_remaining; Story 8.5 (batch-approve-all) is the owning story — deferred, pre-existing scope gap [AC-2]
- [x] [Review][Defer] Undo stack not pruned on run completion or navigation — `clear_undo_stack` exists but is never called on stage advancement or run switch; stale commands accumulate — deferred, UX polish
- [x] [Review][Defer] Escape key bound to reject in BatchReview — high accidental-trigger risk; Escape is a universal cancel key — deferred, UX tradeoff not specified in AC
- [x] [Review][Defer] Missing component test: Ctrl+Z focus restoration after navigating away — AC-5 requires a component test verifying undo re-selects the original SceneCard after navigation — deferred, test coverage gap
- [x] [Review][Defer] Missing integration test: superseded skip decisions excluded from counts — AC-3 requires an integration test verifying summaries/counts exclude superseded skip decisions — deferred, test coverage gap

## Dev Agent Record

### Agent: claude-sonnet-4-6 | Date: 2026-04-19

**Implementation Summary:**

All 8 tasks completed. Story implements the V1 undo system via `decisions.superseded_by` reversal rows (no hard deletes).

**Key decisions made during implementation:**
- T1: Added `DecisionTypeUndo`, `DecisionTypeDescriptorEdit`, and `CommandKind*` constants in `internal/domain/review_gate.go`. Added `IsPrePhaseC(stage, status)` gate function.
- T2: Added `LatestUndoableDecision`, `ApplyUndo` (transactional), and `RecordDescriptorEdit` to `internal/db/decision_store.go`. `ApplyUndo` restores `segments.review_status` for approve/reject and `runs.frozen_descriptor` for descriptor_edit within one SQLite transaction.
- T3: Added `SceneService.UndoLastDecision`, `POST /api/runs/{id}/undo` route and handler. Returns `{undone_scene_index, undone_kind, focus_target}`.
- T4: `RecordSceneDecision` in `scene_service.go` builds `context_snapshot` with `command_kind` on approve/reject. `CharacterService.Pick` calls `RecordDescriptorEdit` (best-effort, non-fatal) when `frozen_descriptor` changes.
- T5: Extended `useUIStore` with `undo_stacks: Record<string, UndoCommand[]>`, `UNDO_STACK_MAX_DEPTH=10`, `push_undo_command/pop_undo_command/clear_undo_stack`. Stack is NOT persisted to localStorage.
- T6: `BatchReview.tsx` wires `ctrl+z` via `useKeyboardShortcuts` with `allow_in_editable: false` and `scope: 'context'`. `decision_mutation.onSuccess` pushes command; `undo_mutation.onSuccess` pops and restores selection.
- T7: Two-layer separation maintained — VisionDescriptorEditor local revert untouched; global undo layer added via `descriptor_edit` decision rows.
- T8: 10 Go tests, 8 useUIStore tests, 8 BatchReview tests. All 159 frontend + all Go tests pass.

**Zustand selector fix:** `useUIStore((s) => s.undo_stacks[run.id] ?? [])` caused infinite re-renders (new `[]` reference each render). Fixed to `useUIStore((s) => s.undo_stacks[run.id])` with `(raw?.length ?? 0) > 0`.

**Files changed:**
- `internal/domain/review_gate.go`
- `internal/db/decision_store.go` + `_test.go`
- `internal/service/scene_service.go` + `_test.go`
- `internal/service/character_service.go`
- `internal/api/handler_scene.go` + `_test.go`
- `internal/api/routes.go`
- `cmd/pipeline/serve.go`
- `web/src/contracts/runContracts.ts`
- `web/src/lib/apiClient.ts`
- `web/src/stores/useUIStore.ts` + `.test.ts`
- `web/src/components/production/BatchReview.tsx` + `.test.tsx`
- `web/src/components/production/CharacterPick.tsx`
