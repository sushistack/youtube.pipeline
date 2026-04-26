---
title: 'Scenario review HITL approve gate (P0 unblocker)'
type: 'feature'
created: '2026-04-26'
status: 'in-progress'
baseline_commit: '37a45ed8b06f972eb9755986dcc58c4c83e7bfe4'
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** `scenario_review` is a HITL stage in the state machine but has no operator approval path. Other HITL stages (`character_pick` / `batch_review` / `metadata_ack`) each have a dedicated endpoint + UI; `scenario_review` has neither. Every run that reaches `scenario_review` is permanently stuck — `/advance` rejects HITL stages, no `/scenario/approve` route exists, the FE renders no button, and dogfood progress is blocked.

**Approach:** Add the missing approve gate end-to-end. New backend endpoint `POST /api/runs/{id}/scenario/approve` invoking a new `RunService.ApproveScenarioReview` that mirrors the `CharacterService.Pick` transition pattern (guard → settings promote → `NextStage(EventApprove)` → atomic stage advance → HITL session row management). New FE "Approve scenario" CTA inside `ScenarioInspector` calling a new `apiClient.approveScenarioReview` and refreshing run status.

## Boundaries & Constraints

**Always:**
- Stage transition: `scenario_review`/`waiting` → `character_pick`/`waiting` via `pipeline.NextStage(StageScenarioReview, EventApprove)`. No bypass — invalid current stage returns `ErrConflict`.
- The transition writes through `runs.ApplyPhaseAResult` (or an equivalent atomic store method) with the new stage/status. Existing meta fields (`CriticScore`, `ScenarioPath`) preserved across the write.
- Settings promotion fires at the boundary via `SettingsPromoter.PromotePendingAtSafeSeam` — same hook every other stage transition uses.
- HITL row management: drop `hitl_sessions` row for the just-exited `scenario_review` (per Story 2.6 invariant) and rely on the existing transition-time upsert hook (commit `37a45ed`) to create the `character_pick` row. If the upsert hook is wired into the Engine, reuse it; if not, perform an explicit upsert here.
- API surface: `POST /api/runs/{id}/scenario/approve`, no body, returns the updated run JSON (`runDetailResponseSchema` shape, mirroring `/characters/pick`). Errors map to standard codes: 404 (run not found), 409 (wrong stage/status), 422 (validation), 500 (internal).
- FE: an "Approve scenario" button is rendered ONLY when `current_run.stage === 'scenario_review' && current_run.status === 'waiting'`. The button optimistically disables on click, calls the API, on success refreshes run status; on error surfaces a non-blocking inline error.

**Ask First:**
- Whether to record a per-decision row in `decisions` for the approve action (CharacterService records descriptor edits for undo; scenario approve has no per-scene granularity — default: no decision row, just the stage advance).
- Whether to gate approve on per-scene decisions being settled (e.g., disallow approve while any scene shows `rejected` or `pending`). Default: no per-scene gate at this stage — `scenario_review` is a whole-script approve in V1.

**Never:**
- No automatic re-trigger of Phase A on approve. Approve is purely a stage advance.
- No backfill of decisions table (scenario_review decisions are not surfaced today).
- No FE ability to re-edit narration AFTER clicking approve (the next stage owns its own state).
- No new domain types — reuse `domain.Run`, `domain.Stage`, `domain.Status`, `domain.PhaseAAdvanceResult`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Happy approve | `run.Stage=scenario_review, run.Status=waiting` | Stage advances to `character_pick`/`waiting`, response = updated `Run`, `hitl_sessions` row replaced (scenario row dropped, character row created) | N/A |
| Wrong stage | `run.Stage=write` (or anything ≠ `scenario_review`) | 409 ErrConflict; no state mutation | Return wrapped `ErrConflict` |
| Wrong status | `run.Stage=scenario_review, run.Status=running` | 409 ErrConflict; no state mutation | Return wrapped `ErrConflict` |
| Run not found | unknown run_id | 404 ErrNotFound | Return wrapped `ErrNotFound` |
| Settings promote fails | `SettingsPromoter` returns error | Log warn, continue with stage advance (matches existing `promoteSettingsAtBoundary` semantics) | Best-effort, non-fatal |
| HITL row delete fails | `DeleteSession` returns error | Log warn, continue (state machine truth lives in `runs`; row inconsistency is tolerable per existing behavior) | Best-effort |
| HITL row upsert fails | Upsert returns error | Log warn, return success — run is in correct stage; UI will backfill on next read (re-uses the read-time backfill from prior fix) | Best-effort |

</frozen-after-approval>

## Code Map

- `internal/api/routes.go` — register `api.HandleFunc("POST /api/runs/{id}/scenario/approve", deps.Run.ApproveScenarioReview)`.
- `internal/api/handler_run.go` — new handler `ApproveScenarioReview` that pulls `id` from path, calls `svc.ApproveScenarioReview`, writes JSON response. Mirrors `/characters/pick` handler shape.
- `internal/service/run_service.go` — new method `ApproveScenarioReview(ctx, runID) (*domain.Run, error)`. Loads run, guards stage/status, calls settings promote, calls atomic store transition, returns reloaded run.
- `internal/service/run_service.go` — declare a small interface for the store transition (e.g. `ApplyScenarioApprove`) OR reuse `ApplyPhaseAResult` if its semantics fit. Use whichever already exists; do not invent a new store method if `ApplyPhaseAResult(StageCharacterPick, StatusWaiting, ...)` is correct.
- `internal/service/run_service.go` — wire HITL row management: drop scenario_review row + upsert character_pick row. Use the existing `pipeline.UpsertSessionFromState` helper if the service has access; else add a narrow setter on RunService (matches Engine's `SetHITLSessionStore` pattern from `37a45ed`).
- `cmd/pipeline/serve.go` — wire the HITL session writer into the new RunService method (parallel to existing engine wiring at `engine.SetHITLSessionStore(newHITLSessionStoreAdapter(decisionStore))`).
- `web/src/lib/apiClient.ts` — new function `approveScenarioReview(run_id)` → POST `/runs/{id}/scenario/approve`, returns `runDetailResponseSchema`.
- `web/src/components/production/ScenarioInspector.tsx` — render an "Approve scenario" button (or footer CTA). On click: optimistic disable → call apiClient → on success invalidate run status query / set status / route to next stage; on error show inline message.
- `web/src/components/production/ScenarioInspector.test.tsx` — add test covering button presence at `scenario_review/waiting`, click invokes apiClient, error path keeps button enabled.
- `internal/api/handler_run_test.go` — add table-driven test for the new endpoint covering happy / 409 wrong stage / 404.
- `internal/service/run_service_test.go` — add test covering `ApproveScenarioReview` happy path + ErrConflict + ErrNotFound.

## Tasks & Acceptance

**Execution:**
- [ ] `internal/service/run_service.go` — add `ApproveScenarioReview(ctx, runID)` method. Guard `run.Stage == StageScenarioReview && run.Status == StatusWaiting`. Compute `nextStage = NextStage(StageScenarioReview, EventApprove)` (must equal `StageCharacterPick`). Promote settings. Apply atomic stage advance via existing `ApplyPhaseAResult` (or equivalent). Return the reloaded run.
- [ ] `internal/service/run_service.go` — add an optional HITL session writer setter (mirroring Engine's `SetHITLSessionStore`). Wire it from `cmd/pipeline/serve.go`. After the stage advance succeeds, call `UpsertSessionFromState` to drop the scenario_review row and create the character_pick row.
- [ ] `internal/api/handler_run.go` — add `ApproveScenarioReview(w, r)` handler. Read `id` path param, call service, write run JSON or error.
- [ ] `internal/api/routes.go` — register the new route under the same chain as `/advance`, `/cancel`, etc.
- [ ] `cmd/pipeline/serve.go` — add the wiring call to enable the HITL row management on the new service method.
- [ ] `web/src/lib/apiClient.ts` — add `approveScenarioReview(run_id)`.
- [ ] `web/src/components/production/ScenarioInspector.tsx` — render approve CTA at the bottom of the inspector (or wherever the stage-action area lives), button label "Approve scenario", aria-label, disabled state during in-flight call, error display on failure. Trigger run status refresh on success.
- [ ] `web/src/components/production/ScenarioInspector.test.tsx` — assert button presence, click→api call, success→status refresh, error→stays enabled.
- [ ] `internal/api/handler_run_test.go` — table tests for the new endpoint.
- [ ] `internal/service/run_service_test.go` — unit tests for `ApproveScenarioReview` (happy / wrong stage / wrong status / not found).

**Acceptance Criteria:**
- Given a run at `scenario_review`/`waiting`, when the operator POSTs `/api/runs/{id}/scenario/approve`, then the run advances to `character_pick`/`waiting` and the response body contains the updated run.
- Given a run at any non-`scenario_review` stage, when the same endpoint is called, then it returns 409 ErrConflict and the run state is unchanged.
- Given a missing run ID, when the endpoint is called, then it returns 404 ErrNotFound.
- Given a successful approve, when the FE polls run status, then `hitl_sessions` has a `character_pick` row (and no `scenario_review` row) — verified by the existing read-time backfill plus the transition-time upsert hook.
- Given a failed run lookup or store error during approve, when the endpoint returns, then no partial state mutation has occurred (atomic write OR pre-write guard).
- Given the FE shows a run at `scenario_review`/`waiting`, when the operator clicks "Approve scenario", then the button disables, the API is called once, on success the inspector unmounts and `CharacterPick` mounts (because run.stage flipped); on error the button re-enables and an inline message appears.
- `go build ./...` succeeds, `go test ./internal/api/... ./internal/service/...` is green, FE `npm test` is green.

## Verification

**Commands:**
- `go build ./...` — expected: success.
- `go test ./internal/api/... ./internal/service/...` — expected: all tests green, including new endpoint + service tests.
- `go vet ./...` — expected: clean.
- `(cd web && npm test -- --run ScenarioInspector)` — expected: ScenarioInspector approve tests green.

**Manual checks:**
- After deploy, existing stuck run at `scenario_review` shows the "Approve scenario" button. Clicking it transitions the run to `character_pick`/`waiting` (UI swaps to `CharacterPick` panel).
- Server log shows: stage transition, settings promote (if pending), HITL session row drop + upsert. No `hitl session row missing for waiting run` warning after approve.
