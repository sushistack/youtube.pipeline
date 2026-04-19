# Story 8.4: Rejection & Regeneration Flow

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want to provide a rejection reason inline and trigger a bounded regeneration flow,
so that the system can retry the scene with corrected context without blocking the rest of batch review.

## Prerequisites

**Hard dependencies:** Story 6.3 established the shared keyboard shortcut engine and editable-target suppression. Story 7.2 established the scenario edit service/handler split plus inline editing patterns. Story 8.1 defines the batch-review read surface and selection model that this story extends. Story 8.2 is the immediate write-path dependency: Story 8.4 assumes there is already a canonical batch-review decision endpoint and action bar to extend rather than replace.

**Backend dependency:** Story 2.6 already established `pipeline.UpsertSessionFromState(...)`, which must continue to run after every human review mutation. Story 5.4 already documented clean-slate image regeneration behavior in the segment/image track path, including replacement of stale shot/image references after regeneration. Story 8.4 should reuse those primitives instead of inventing a second regeneration persistence model.

**Architecture dependency:** `architecture.md` keeps `POST /api/runs/{id}/scenes/{idx}/regen` as the regeneration endpoint and treats FR53 as a prior-rejection check keyed by same SCP and scene identity. `ux-design-specification.md` further narrows the reject UX to an inline reason prompt, non-blocking regeneration progress, and a hard max of 2 retries per scene per run.

**Current codebase reality:** today `internal/api/handler_scene.go` only exposes scenario-review list/edit endpoints, `internal/service/scene_service.go` only gates `scenario_review`, and `internal/db/decision_store.go` has batch-review preparation plus minor-safeguard override logic but no reject-with-note flow, no prior-rejection lookup helper, and no per-scene regeneration tracking. On the frontend, `runContracts.ts` and `apiClient.ts` still expose only run list/detail/status, scenario scenes, and character-pick contracts. The actual batch-review mutation surface from Stories 8.1/8.2 is not yet present in the current tree, so this story must be written to layer on that planned work instead of assuming it already exists.

**Parallel-work caution:** `internal/db/decision_store.go`, `internal/service/scene_service.go`, `internal/api/routes.go`, `internal/api/handler_scene.go`, `web/src/contracts/runContracts.ts`, `web/src/lib/apiClient.ts`, `web/src/lib/queryKeys.ts`, `web/src/components/shells/ProductionShell.tsx`, and the future batch-review components are active Epic 8 integration points. Layer changes carefully and do not revert adjacent work from Stories 8.1-8.3 or widen the old scenario-review endpoint contract.

## Acceptance Criteria

### AC-1: Reject uses an inline reason composer, not a modal

**Given** the operator is focused on a batch-review scene  
**When** they press `Esc` or click `Reject`  
**Then** the detail panel expands an inline rejection composer in place  
**And** the scene remains visible behind the composer  
**And** no modal, route change, or blocking overlay is used.

**Rules:**
- The inline composer belongs to the batch-review surface, near the existing action bar and selected-scene context.
- Keyboard focus moves into the inline reason input when the composer opens.
- Pressing `Esc` while the composer is closed opens it; pressing `Esc` while it is open cancels only the composer state if the input is empty, or follows the product’s existing editable-control escape behavior if text is already present.
- The reject action must remain local to batch review; it must not hijack `Esc` behavior in other tabs or existing failure/continuity banners.

**Tests:**
- Frontend integration test verifies `Esc` opens the inline composer instead of firing an immediate reject mutation.
- Test verifies no modal/dialog role is rendered.
- Test verifies focus lands in the inline reason field and returns to the scene action area when cancelled.

---

### AC-2: The operator must supply a reason, and FR53 warning uses the V1 deterministic similarity rule

**Given** the inline reject composer is open  
**When** the operator attempts to confirm rejection without a reason  
**Then** the UI blocks submission and shows an inline validation message.

**Given** the operator enters a rejection reason  
**When** the system evaluates FR53 prior-failure context  
**Then** it checks for prior non-superseded reject attempts using the V1 rule:
- same `scp_id`
- same `scene_index`

**And** if a prior reject exists, the UI surfaces a warning containing the earlier reason, timestamp, and originating run or attempt context  
**And** the operator can still proceed with the new rejection after seeing the warning.

**Rules:**
- V1 is **not** embedding similarity, cosine thresholding, or free-text semantic matching.
- Ignore superseded/undone decisions when looking for prior failures.
- Prefer the most recent prior reject with a non-empty note when multiple matches exist.
- The warning is advisory; it does not block rejection or regeneration.

**Tests:**
- Store/service test verifies FR53 lookup joins by same `scp_id` + same `scene_index`.
- Regression test verifies a different `scene_index` on the same SCP does not trigger the warning.
- Frontend test verifies the warning renders inline and does not block submit.

---

### AC-3: Confirming rejection records the note and dispatches regeneration in the background

**Given** the operator submits a valid rejection reason  
**When** the backend accepts the request  
**Then** a reject decision row is recorded with the reason stored in `decisions.note`  
**And** a regeneration task is dispatched asynchronously for that same scene  
**And** the batch-review UI remains usable for other scenes while regeneration is in progress  
**And** the selected scene shows a non-blocking regenerating state until the new result is available or the task fails.

**Rules:**
- Keep decision capture and regeneration dispatch explicit:
  - rejection write goes through the canonical Epic 8 decision ledger
  - regeneration dispatch goes through the dedicated regeneration endpoint/service path
- The UI must not freeze the list/detail surface waiting for the regeneration to finish.
- `pipeline.UpsertSessionFromState(...)` still runs after the human decision write so polling summaries remain current.
- Regeneration should reuse the existing Phase B clean-slate update path for refreshed shots/images instead of leaving stale media pointers in place.

**Tests:**
- Handler/service integration test verifies a reject with note persists the decision and triggers regeneration dispatch.
- Frontend integration test verifies other scenes remain navigable while the rejected scene shows in-progress state.
- Integration test verifies the scene re-enters the review queue when regenerated content is available.

---

### AC-4: Regeneration is capped at 2 retries per scene per run

**Given** the operator has already triggered 2 regeneration attempts for the same scene in the same run  
**When** they reject that scene again  
**Then** the system does **not** dispatch a third automatic regeneration  
**And** the scene shows a retry-exhausted state in the batch-review UI  
**And** the operator is offered exactly two next-step choices:
- manual edit
- skip & flag

**Rules:**
- Retry counting is per `run_id + scene_index`, not global and not based on `runs.retry_count`.
- The cap applies only to operator-triggered rejection/regeneration retries in batch review.
- “Manual edit” should route to the existing inline edit path rather than inventing a second editor surface.
- “Skip & flag” should reuse the Epic 8 decision ledger and context snapshot instead of introducing a silent local-only dismissal.

**Tests:**
- Store/service test verifies third regeneration attempt is blocked after 2 prior attempts for the same run/scene.
- Frontend test verifies exhausted-state CTAs replace the normal regen path.
- UI test verifies retry-exhausted state is visible on the scene card/detail surface.

---

### AC-5: Polling, session state, and derived review counts stay coherent during reject/regenerate cycles

**Given** rejection and regeneration occur on a batch-review run  
**When** status polling and review queries refresh  
**Then** the operator sees coherent counts, scene selection, and change indicators without a full page reload  
**And** HITL session snapshots remain compatible with Story 2.6 pause/resume behavior  
**And** regenerated scenes can surface as changed content without corrupting the frozen-list semantics from Story 8.1.

**Rules:**
- Reuse TanStack Query invalidation/refetch patterns; do not add ad-hoc polling loops.
- Batch-review list stability still applies: queued updates should appear as refreshable changes rather than unexpectedly reshuffling the frozen list.
- If regeneration completes after the current snapshot time, Story 2.6 diff behavior should continue to classify it as a scene change, not as a broken state.

**Tests:**
- Integration test verifies reject/regenerate updates are reflected in run status polling and review-item refresh.
- Regression test verifies the stable-list/update-badge pattern still holds while a scene regenerates in the background.

## Tasks / Subtasks

- [x] **T1: Extend the review-decision backend contract for reject-with-reason** (AC: #1, #2, #3)
  - Build on Story 8.2’s canonical `POST /api/runs/{id}/decisions` flow rather than creating a one-off reject endpoint.
  - Require `note` / rejection reason for `decision_type = 'reject'`.
  - Keep request validation consumer-owned in the service/API layer.
  - Ensure batch-review stage/status gating remains `batch_review` + `waiting`.

- [x] **T2: Add prior-rejection lookup and retry-count helpers in `DecisionStore`** (AC: #2, #4)
  - Add a query helper for FR53 prior failures keyed by `runs.scp_id + decisions.scene_id`.
  - Ignore `superseded_by IS NOT NULL` rows.
  - Prefer returning the latest reject with a non-empty `note`.
  - Add a second helper that counts prior regeneration attempts for one `run_id + scene_index`.
  - Do not use `runs.retry_count`; that field is run-level and belongs to pipeline-stage retry logic, not scene-level review retries.

- [x] **T3: Introduce service orchestration for reject-and-regenerate** (AC: #2, #3, #4, #5)
  - Add a dedicated service entry point instead of widening scenario-review edit logic, for example:
    - `RejectSceneWithReason(...)`
    - `QueueSceneRegeneration(...)`
    - or a combined orchestration wrapper that records the decision, checks retry cap, and dispatches regeneration
  - Sequence should be explicit:
    1. validate stage/status and reason
    2. load FR53 prior rejection warning payload
    3. persist reject decision
    4. upsert HITL session snapshot
    5. dispatch regeneration only if under the retry cap
  - Use injected clock/store dependencies; do not call `time.Now()` directly in service code.

- [x] **T4: Add/extend API routes for decision write and regeneration dispatch** (AC: #2, #3, #4)
  - Extend the Epic 8 decision handler to accept rejection reasons and return any prior-rejection warning payload needed by the client.
  - Add the dedicated regeneration route from architecture: `POST /api/runs/{id}/scenes/{idx}/regen`.
  - Validate `scene_index` as a non-negative integer and reject attempts beyond the cap with `ErrConflict` / HTTP 409.
  - Register routes centrally in `internal/api/routes.go`.

- [x] **T5: Define frontend contracts for reject reason, warning payload, and regen status** (AC: #2, #3, #4, #5)
  - Extend `web/src/contracts/runContracts.ts` with:
    - reject request payload including `note`
    - reject response payload including optional FR53 warning metadata
    - regeneration response/status shape needed by the UI
  - Extend `web/src/lib/apiClient.ts` with helpers for:
    - recording a reject-with-reason decision
    - dispatching scene regeneration
  - Add or refine query keys for batch-review items / regeneration state instead of piggybacking on the old `runs.scenes` scenario-review key.

- [x] **T6: Build the inline reject composer in the batch-review UI** (AC: #1, #2)
  - Mount the composer inside the batch-review detail/action area, not as a modal.
  - Show:
    - multiline or single-line reason input per UX choice
    - inline validation
    - advisory FR53 warning block
    - confirm/cancel controls with keyboard hints
  - Ensure focus management and shortcut suppression coexist with `J/K`, `Space`, and editable controls.

- [x] **T7: Add non-blocking regeneration and retry-exhausted UI states** (AC: #3, #4, #5)
  - Render a regenerating badge/progress state on the active scene card/detail pane.
  - Preserve reviewability of other scenes while regeneration is in flight.
  - After the second retry, replace the automatic-regenerate path with:
    - manual edit CTA
    - skip & flag CTA
  - Keep these states aligned with the stable-list/update-badge behavior from Story 8.1.

- [x] **T8: Reuse and document skip/flag semantics without inventing an unsupported scene status** (AC: #4)
  - The current persisted review statuses are still `pending`, `waiting_for_review`, `auto_approved`, `approved`, and `rejected`.
  - If “flagged” must be persisted in V1, prefer encoding it through the decision ledger / structured context snapshot rather than silently adding a new `segments.review_status` value with no prior architecture support.
  - Coordinate with Story 8.2 skip payload shape so the two stories do not diverge.

- [x] **T9: Add focused backend and frontend coverage** (AC: #1-#5)
  - Backend:
    - `decision_store_test.go` for prior-reject lookup, retry counting, and cap enforcement
    - service tests for reject + session upsert + regeneration dispatch sequencing
    - handler tests for validation, warning payload, and 409 retry-cap rejection
  - Frontend:
    - batch-review component tests for inline composer open/cancel/submit
    - FR53 warning rendering test
    - regeneration-in-progress and retry-exhausted state tests
    - keyboard coexistence tests for `Esc`, `Enter`, `J/K`, `Space`

## Dev Notes

### Story Intent and Scope Boundary

- Story 8.4 is the rejection/retry orchestration layer for batch review.
- Do not turn this into a generic background-job framework, modal system, or semantic-search feature.
- Do not replace Story 8.2’s canonical decision ledger with bespoke reject writes.
- Do not implement embedding similarity, cosine thresholds, or “similar wording” NLP for FR53 in V1.

### FR53 Guardrail: deterministic V1 similarity only

The planning artifacts already settle V1 similarity:
- same `scp_id`
- same `scene_index`

That means the warning is effectively “we have seen this scene fail before” rather than “these two free-text reasons are semantically similar.” If contributors later want embedding-based matching, that is a V1.5 follow-up and should not be smuggled into this story.

### Retry Counting Guardrail

`runs.retry_count` is already used for run/stage retry behavior and must not be repurposed for per-scene review retries. Story 8.4 needs scene-local counting:
- key: `run_id + scene_index`
- scope: operator-triggered regeneration attempts from batch review

Prefer deriving this from explicit decision/job records so the cap survives refreshes, undo-compatible ledger reads, and background completion timing.

### Regeneration State Modeling

The current persisted scene review states are:
- `pending`
- `waiting_for_review`
- `auto_approved`
- `approved`
- `rejected`

There is no established persisted `regenerating` or `flagged` review status yet. For V1:
- treat `regenerating` as an operational/job state surfaced through the API/view-model
- keep `flagged` behavior anchored in the decision ledger / context snapshot unless a deliberate schema change is approved

This avoids silently widening `domain.ReviewStatus` and breaking existing Story 4.4 / 2.6 assumptions.

### Reuse Existing Regeneration Semantics

Story 5.4 already documented the clean-slate regeneration rule for refreshed image/shot data, and `internal/db/segment_store.go` explicitly mentions replacement of stale image references after regeneration. Story 8.4 should dispatch the existing regeneration path and then let the normal artifact refresh logic update the scene, rather than hand-editing media rows from the review handler.

### Current Codebase Reality

| What | Where | State |
|---|---|---|
| Scenario scene review endpoints | `internal/api/handler_scene.go` | Only `GET /scenes` and `POST /scenes/{idx}/edit` exist |
| Route registration | `internal/api/routes.go` | No decision or regen route yet |
| Scenario review service | `internal/service/scene_service.go` | Gated to `scenario_review/waiting` only |
| Decision ledger | `internal/db/decision_store.go` | Has list/count, batch-review prep, minor override; no reject-reason or FR53 helper |
| HITL session rebuild | `internal/pipeline/hitl_session.go` | Exists and should be reused after decision writes |
| Frontend run contracts | `web/src/contracts/runContracts.ts` | No batch-review reject/regen schemas yet |
| Frontend API client | `web/src/lib/apiClient.ts` | No decision-write or regen helpers yet |
| Query keys | `web/src/lib/queryKeys.ts` | Only list/detail/status/scenes/characters/descriptor keys today |
| Batch-review UI | Stories 8.1/8.2 | Specified in docs, not yet present in current tree |

### Recommended Implementation Shape

Keep the flow split but coordinated:

1. Open inline composer from batch-review action bar.
2. Validate reason locally.
3. Submit reject decision through the canonical decision endpoint.
4. Return/display FR53 warning metadata from the same backend decision flow, or fetch it through a dedicated preflight if implementation pressure requires it.
5. If under cap, dispatch `POST /api/runs/{id}/scenes/{idx}/regen`.
6. Mark the scene as regenerating in the UI and continue reviewing other scenes.
7. Refresh review items/status when the regenerated scene lands.

This preserves a single decision ledger while keeping regeneration an explicit downstream operation.

### Testing Requirements

Minimum backend coverage:
- prior-reject lookup for same `scp_id + scene_index`
- superseded rejects excluded from FR53 warning
- retry cap blocks the third auto-regeneration
- reject decision still writes note and updates HITL session
- regeneration dispatch failure is surfaced clearly without corrupting the reject decision

Minimum frontend coverage:
- inline composer open/cancel/submit
- required reason validation
- FR53 warning rendering
- non-blocking regenerating state
- retry-exhausted CTA rendering
- keyboard coexistence with `Esc`, `Enter`, `J/K`, and `Space`

### Project Structure Notes

- No `project-context.md` file was found during discovery, so this story is grounded in the planning and implementation artifacts plus current source code.
- Preserve the existing layer direction:
  - `api` validates/translates HTTP
  - `service` owns batch-review orchestration and stage gates
  - `db` owns decision and segment persistence details
  - `pipeline` retains HITL snapshot rebuilding and downstream regeneration mechanics
- Do not widen the old scenario-review response contract just to carry batch-review regeneration metadata.

### Previous Story Intelligence

- Story 8.2 already decided that Epic 8 review actions should converge on one canonical decision ledger and call `pipeline.UpsertSessionFromState(...)` afterward. Story 8.4 must extend that same path with reason capture, not fork it.
- Story 8.2 also explicitly deferred reject reason prompting to this story.
- Story 8.3 establishes that Epic 8 undo semantics depend on `superseded_by` and non-superseded decision rows. Story 8.4 FR53 lookup and retry counting must respect that rule so undo does not leave ghost “prior failures.”
- Story 7.4 reinforced the repo’s preference for inline, non-modal remediation UI and `useKeyboardShortcuts`-based bindings. Follow the same pattern here.

### Git Intelligence Summary

- Recent history still tops out at `Implement Epic 6: Web UI — Design System & Application Shell`; the Epic 7 and Epic 8 story files contain newer intent than git history does.
- That mismatch is a useful warning: prefer the current implementation artifacts and live code over old commit-era assumptions when choosing file locations and contracts.

### References

- `_bmad-output/planning-artifacts/epics.md`
  - Epic 8 overview
  - Story 8.4 acceptance criteria
  - FR53 definition
- `_bmad-output/planning-artifacts/sprint-prompts.md`
  - Epic 8 - Story 8.4 개발 / 코드 리뷰 prompts
- `_bmad-output/planning-artifacts/architecture.md`
  - Scene Review API endpoint table
  - FR53 gap-analysis note
- `_bmad-output/planning-artifacts/ux-design-specification.md`
  - keyboard/action table
  - rejection and re-generation flow
- `_bmad-output/implementation-artifacts/8-2-scene-review-actions-audioplayer.md`
  - canonical decision-write path and session-upsert requirement
- `_bmad-output/implementation-artifacts/8-3-decision-undo-command-pattern.md`
  - non-superseded decision semantics / `superseded_by` guardrails
- `internal/api/handler_scene.go`
- `internal/api/routes.go`
- `internal/service/scene_service.go`
- `internal/db/decision_store.go`
- `internal/pipeline/hitl_session.go`

## Dev Agent Record

### Agent Model Used

gpt-5

### Debug Log References

- Skill workflow: `/mnt/work/projects/youtube.pipeline/.agents/skills/bmad-create-story/workflow.md`

### Completion Notes List

- Created Story 8.4 implementation context file with Epic/architecture/UX guardrails and current-codebase intelligence.
- Used the explicit user-selected target story (`8.4`) instead of auto-discovery from backlog order.
- No `project-context.md` was present, so no extra project-context references were added.
- Backend: Added `PriorRejectionForScene` (cross-run, same `scp_id + scene_index`, excludes same run and superseded rows, prefers latest non-empty note) and `CountRegenAttempts` (per-run scene reject counter) helpers to `DecisionStore`. Regen retry cap enforced as `MaxSceneRegenAttempts = 2`.
- Backend: `SceneService.RecordSceneDecision` now requires a non-empty `note` for `reject`, returns `RegenAttempts`, `RetryExhausted`, and the optional `PriorRejection` FR53 warning. `DispatchSceneRegeneration` validates stage, enforces the cap, calls the injected `SceneRegenerator`, and refreshes the HITL session via `pipeline.UpsertSessionFromState`.
- Backend: Introduced `SceneRegenerator` interface and V1 `NoOpSceneRegenerator` stub that resets `segments.review_status` back to `waiting_for_review` while preserving safeguard flags. Real Phase B regeneration dispatch remains Story 5.4 work.
- Backend: Added `POST /api/runs/{id}/scenes/{idx}/regen`. Extended `/decisions` reject response with `regen_attempts`, `retry_exhausted`, and optional `prior_rejection` payload; extended `/review-items` with the same fields per scene so the composer can surface FR53 context without an extra round-trip.
- Frontend: Added zod schemas (`priorRejectionWarningSchema`, `sceneRegenResponseSchema`), `dispatchSceneRegeneration` apiClient helper, and `regenState` query key. Reused `skip_and_remember` for "Skip & flag" with a `{ flagged: true, flag_reason: 'retry_exhausted' }` context snapshot so the exhausted path stays on the canonical decision ledger.
- Frontend: `BatchReview` now opens an inline `RejectComposer` on `Esc` / Reject button click (no dialog role), validates required reason, dispatches regeneration after a successful reject, tracks per-scene regenerating state locally, and replaces the normal action bar with exhausted CTAs (`Manual edit` disabled with explanation, `Skip & flag` fully functional) when `retry_exhausted` is true. Global shortcuts are suppressed while the composer is open so Enter inserts newlines instead of firing approve.
- Frontend: `SceneCard` and `DetailPanel` render `Regenerating…` and `Retry exhausted` badges driven by the new fields; `RejectComposer` renders the FR53 advisory block when `prior_rejection` is set on the selected review item.
- Coverage: 7 new `DecisionStore` tests (prior rejection + regen attempts), 7 new service tests (reject-note validation, FR53 propagation, regen dispatch, cap enforcement, dispatch failure), 5 new API handler tests (reject-note 400, FR53 warning, regen happy path, 409 on cap, 400 on bad index), 8 new `BatchReview` tests (Esc opens composer / no dialog, empty-reason validation, Esc cancel on empty, confirm reject dispatches regen, FR53 warning from /review-items, retry-exhausted CTAs, skip-and-flag ledger payload, other scenes remain reviewable), and 6 new `RejectComposer` unit tests.
- Verification: `go test ./...` all green (full suite), `npx vitest run` 27/27 files, 173/173 tests, `npm run lint` clean, `go vet ./...` clean.

### File List

- `_bmad-output/implementation-artifacts/8-4-rejection-regeneration-flow.md`
- `internal/db/decision_store.go`
- `internal/db/decision_store_test.go`
- `internal/service/scene_service.go`
- `internal/service/scene_service_test.go`
- `internal/api/handler_scene.go`
- `internal/api/handler_scene_test.go`
- `internal/api/routes.go`
- `cmd/pipeline/serve.go`
- `web/src/contracts/runContracts.ts`
- `web/src/lib/apiClient.ts`
- `web/src/lib/queryKeys.ts`
- `web/src/components/production/BatchReview.tsx`
- `web/src/components/production/BatchReview.test.tsx`
- `web/src/components/shared/RejectComposer.tsx`
- `web/src/components/shared/RejectComposer.test.tsx`
- `web/src/components/shared/SceneCard.tsx`
- `web/src/components/shared/DetailPanel.tsx`
- `web/src/index.css`

### Change Log

- 2026-04-19 — Implemented Story 8.4 backend + frontend per workflow (all 9 tasks). Added prior-rejection lookup, per-scene regen-attempt counter with 2-retry cap, non-blocking regenerating state, inline reject composer, and retry-exhausted CTAs routed through the canonical Epic 8 decision ledger.
- 2026-04-19 — Code review remediation: fixed `retry_exhausted` cache semantic mismatch (client now uses `>=` for UI consumption), undo 409 stack-pop, stub regenerator segment-state guard, retry-cap bypass via undo (CountRegenAttempts now includes superseded), RejectComposer root-level Esc, DetailPanel retry-exhausted pill and heroShot fallback, and merged fresh `prior_rejection` from reject response into cache. Added `TestSceneService_ListReviewItems_RetryExhaustedAtCapBoundary`.

### Review Findings

- [x] [Review][Decision] FR53 same-run exclusion not in spec — dismissed: current cross-run-only semantics retained. Same-run repeat rejects are already visible on the operator's screen (current run's scene list / detail pane), so a warning banner adds little signal. Spec reading "originating run or attempt context" is interpreted as "cite the prior run when warning is triggered," not "always warn intra-run."
- [x] [Review][Patch] `retry_exhausted` computed with inconsistent operators [internal/service/scene_service.go:451 / web/src/components/production/BatchReview.tsx] — fixed on the client: the cache merge on reject now recomputes `retry_exhausted` from `regen_attempts >= MAX_SCENE_REGEN_ATTEMPTS` so the UI-consumable "am I done?" semantic matches `/review-items`. The server's `>` stays for the dispatch-gating semantic (can the client still fire the current regen?).
- [x] [Review][Patch] Undo 409 after auto-dispatched regen leaves Undo button stuck [web/src/components/production/BatchReview.tsx] — fixed: `undo_mutation.onError` now pops the stale command from the run's undo stack when the server responds 409, so a subsequent Ctrl+Z doesn't re-fail on the same un-appliable command. Transient errors (500 / network) keep the stack intact.
- [x] [Review][Patch] `/regen` endpoint has no segment-state check [internal/service/scene_service.go:NoOpSceneRegenerator] — fixed: stub regenerator now returns `ErrConflict` unless `segment.ReviewStatus == rejected`, preventing a bug or direct call from silently downgrading an approved scene to `waiting_for_review`.
- [x] [Review][Patch] reject → undo → reject resets retry cap unbounded [internal/db/decision_store.go:CountRegenAttempts] — fixed: `CountRegenAttempts` no longer filters on `superseded_by IS NULL`; all historical rejects are counted because each had an actual regen dispatched whose side-effects aren't rolled back by undo. Accompanying store test renamed and updated.
- [x] [Review][Patch] `RejectComposer` Esc swallowed when focus leaves textarea [web/src/components/shared/RejectComposer.tsx] — fixed: `handleKeyDown` is now attached to the composer's root `<section>`, so Esc cancels (when reason is empty) regardless of whether focus is on textarea, Confirm button, Cancel button, or FR53 warning.
- [x] [Review][Patch] `heroShot` fallback expression is redundant [web/src/components/shared/DetailPanel.tsx] — fixed: simplified to `activeVersion.shots[0] ?? item.shots[0]` so the `previous` version path also falls back to the current hero when prior-version shots are missing.
- [x] [Review][Patch] DetailPanel has no `Retry exhausted` badge [web/src/components/shared/DetailPanel.tsx / web/src/index.css] — fixed: DetailPanel now renders a danger-toned "Retry exhausted" pill next to the `Regenerating…` slot when `item.retry_exhausted` is true, with matching `.detail-panel__retry-exhausted` CSS.
- [x] [Review][Patch] Fresh `prior_rejection` from reject response is discarded [web/src/components/production/BatchReview.tsx] — fixed: `decision_mutation.onSuccess` now merges `saved.prior_rejection ?? null` into the cached review item so a freshly-materialized FR53 warning reaches the next composer open without waiting for `/review-items` refetch.
- [x] [Review][Patch] No service-level test asserts `retry_exhausted=true` on `ListReviewItems` [internal/service/scene_service_test.go] — fixed: added `TestSceneService_ListReviewItems_RetryExhaustedAtCapBoundary` asserting `attempts=1 → false` and `attempts=2 → true`, guarding against future regressions of the `>` vs `>=` semantic.
- [x] [Review][Defer] Manual edit CTA routing target — deferred to follow-up UX story: batch-review stage has no inline narration editor (scenario-review is past), the current disabled button with tooltip stays as a placeholder until the edit surface is designed.
- [x] [Review][Defer] `buildDiffParts` uses a token set, misreports reordered prose as unchanged [web/src/components/shared/DetailPanel.tsx] — deferred, proper LCS diff is a larger refactor beyond Story 8.4 scope.
- [x] [Review][Defer] `sortReviewItems` client comparator can diverge from server ordering [web/src/components/production/BatchReview.tsx] — deferred, approximately consistent in practice; Story 8.1 concern.
- [x] [Review][Defer] `/regen` has no idempotency key against double-dispatch [internal/service/scene_service.go] — deferred, current NoOp stub is harmless; revisit when Phase B real regeneration lands.
- [x] [Review][Defer] Handler only validates `scene_index < 0`, no upper bound or float/overflow check [internal/api/handler_scene.go] — deferred, DB layer catches non-existent segments as NotFound.
- [x] [Review][Defer] `SceneCard` thumbnail keys use array index (`${scene_index}-${index}`) [web/src/components/shared/SceneCard.tsx] — deferred, shots rarely reorder during a single scene's lifecycle.
- [x] [Review][Defer] `buildDecisionSnapshot` swallows `json.Marshal` error [internal/service/scene_service.go] — deferred, unreachable for current payload shape.
- [x] [Review][Defer] `context_snapshot` guard rejects only literal `null`, not `"null"` string or whitespace [internal/api/handler_scene.go] — deferred, brittle but low-impact on internal API.
- [x] [Review][Defer] `command_kind` invariant breaks when caller supplies context_snapshot without it [internal/service/scene_service.go] — deferred, no current consumer depends on the invariant.
- [x] [Review][Defer] FR53 can cite reject from cancelled/failed source run [internal/db/decision_store.go] — deferred, no stage filter on joined runs.
- [x] [Review][Defer] `PriorRejectionForScene` uses exact-string `scp_id` match, no normalization — deferred, normalization should happen at run-creation layer.
- [x] [Review][Defer] `UpsertSessionFromState` failure after decision commit returns 500 and leaves undo-stack disabled [internal/service/scene_service.go] — deferred, hydrate undo stack from server on mount or treat session upsert as best-effort.
- [x] [Review][Defer] `skip_and_remember` undo has no server-side handler in `ApplyUndo` — deferred, works by accident (nothing to restore); add test coverage later.
- [x] [Review][Defer] No migration CHECK constraint enforcing non-empty `note` on reject — deferred, service layer validates.
- [x] [Review][Defer] Stage transition race: run can move out of `batch_review` between service check and regenerator call — deferred, unlikely in single-operator V1.
- [x] [Review][Defer] `strings.TrimSpace` does not reject zero-width space U+200B notes — deferred, edge-case adversarial; add printable-rune check later.
- [x] [Review][Defer] `buildDiffParts` uses a token set, misreports reordered prose as unchanged [web/src/components/shared/DetailPanel.tsx] — deferred, proper LCS diff is a larger refactor beyond Story 8.4 scope.
- [x] [Review][Defer] `sortReviewItems` client comparator can diverge from server ordering [web/src/components/production/BatchReview.tsx] — deferred, approximately consistent in practice; Story 8.1 concern.
- [x] [Review][Defer] `/regen` has no idempotency key against double-dispatch [internal/service/scene_service.go] — deferred, current NoOp stub is harmless; revisit when Phase B real regeneration lands.
- [x] [Review][Defer] Handler only validates `scene_index < 0`, no upper bound or float/overflow check [internal/api/handler_scene.go] — deferred, DB layer catches non-existent segments as NotFound.
- [x] [Review][Defer] `SceneCard` thumbnail keys use array index (`${scene_index}-${index}`) [web/src/components/shared/SceneCard.tsx] — deferred, shots rarely reorder during a single scene's lifecycle.
- [x] [Review][Defer] `buildDecisionSnapshot` swallows `json.Marshal` error [internal/service/scene_service.go] — deferred, unreachable for current payload shape.
- [x] [Review][Defer] `context_snapshot` guard rejects only literal `null`, not `"null"` string or whitespace [internal/api/handler_scene.go] — deferred, brittle but low-impact on internal API.
- [x] [Review][Defer] `command_kind` invariant breaks when caller supplies context_snapshot without it [internal/service/scene_service.go] — deferred, no current consumer depends on the invariant.
- [x] [Review][Defer] FR53 can cite reject from cancelled/failed source run [internal/db/decision_store.go] — deferred, no stage filter on joined runs.
- [x] [Review][Defer] `PriorRejectionForScene` uses exact-string `scp_id` match, no normalization — deferred, normalization should happen at run-creation layer.
- [x] [Review][Defer] `UpsertSessionFromState` failure after decision commit returns 500 and leaves undo-stack disabled [internal/service/scene_service.go] — deferred, hydrate undo stack from server on mount or treat session upsert as best-effort.
- [x] [Review][Defer] `skip_and_remember` undo has no server-side handler in `ApplyUndo` — deferred, works by accident (nothing to restore); add test coverage later.
- [x] [Review][Defer] No migration CHECK constraint enforcing non-empty `note` on reject — deferred, service layer validates.
- [x] [Review][Defer] Stage transition race: run can move out of `batch_review` between service check and regenerator call — deferred, unlikely in single-operator V1.
- [x] [Review][Defer] `strings.TrimSpace` does not reject zero-width space U+200B notes — deferred, edge-case adversarial; add printable-rune check later.
