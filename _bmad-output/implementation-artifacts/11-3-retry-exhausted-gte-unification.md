# Story 11.3: RetryExhausted `>=` Unification (3 Sites)

Status: review

## Story

As an operator,
I want the retry-exhausted threshold to be evaluated consistently everywhere,
so that the batch-review UI and regeneration flow agree on exactly when a scene has exhausted its retry budget.

## Acceptance Criteria

1. **AI-3 landed**: the `RetryExhausted` threshold is unified on `>= MaxSceneRegenAttempts` across the three server-side evaluation sites called out in deferred work:
   - `ListReviewItems`
   - `RecordSceneDecision`
   - `DispatchSceneRegeneration`
   The implementation must use one shared constant / single threshold source of truth rather than three ad-hoc comparisons.
2. The threshold semantics are consistent at the cap boundary:
   - when attempts are below the cap, all three sites report `retry_exhausted=false`
   - when attempts equal the cap, all three sites report `retry_exhausted=true`
   - the regeneration dispatch path still blocks only the overflow attempt, but the returned/read-model state at the cap boundary is already exhausted
3. **Verification criterion carried forward exactly**: `UI-E2E-05` is green, and the property test for the three threshold sites passes **100 iterations green**. This is the exact blocker-release verification from Step 3 `test-design-epic-1-10-2026-04-25.md §12` for R-11 / AI-3.
4. Existing Epic 8 behavior stays intact apart from the boundary fix:
   - no change to the retry cap value itself (`2`)
   - no widening of scope into `CountRegenAttempts` semantics, undo normalization, or new UI copy
   - existing reject/regenerate/skip-and-flag flows continue to use the same API contracts

## Requested Planning Output

### 1. Acceptance Criteria for blocker release

- `AI-3` landed: shared threshold source of truth, unified on `>=` across `ListReviewItems`, `RecordSceneDecision`, and `DispatchSceneRegeneration`
- `Verification`: `UI-E2E-05` green; property test **100 iterations green**

### 2. Affected Files

Likely affected files for this story:

- `internal/service/scene_service.go`
- `internal/service/scene_service_test.go`
- `internal/api/handler_scene_test.go`

Possible support file if the threshold helper is extracted cleanly:

- `internal/service/scene_retry_threshold_test.go`

### 3. E2E scenarios unblocked when this story is done

- `UI-E2E-05` — Rejection + Regeneration + Retry-Exhausted

### 4. Estimated effort

- Jay solo estimate: **2-4 hours** implementation + local verification
- Add **1-2 hours elapsed** if Step 6 is run immediately to prove `UI-E2E-05` green on top of the property test

## Tasks / Subtasks

- [x] Task 1: Unify the threshold comparison in one server-side source of truth (AC: 1, 2)
  - [x] Replace the lone `>` comparison in `RecordSceneDecision` with the same `>=` semantics already used by the other read/dispatch paths.
  - [x] Centralize the exhausted-at-threshold check behind one helper or explicit shared comparison so future edits cannot drift.
  - [x] Keep `MaxSceneRegenAttempts = 2` unchanged.

- [x] Task 2: Add regression coverage for the cap boundary across all three sites (AC: 2, 3)
  - [x] Expand service tests so the same attempt counts are exercised against `ListReviewItems`, `RecordSceneDecision`, and `DispatchSceneRegeneration`.
  - [x] Add the Step 2 / Step 3 requested property-style coverage so boundary values and nearby counts are exercised for **100 iterations**.
  - [x] Preserve the existing conflict-path assertion that overflow dispatch is rejected.

- [x] Task 3: Keep handler/API contract behavior stable while tightening boundary semantics (AC: 3, 4)
  - [x] Re-run the handler tests that serialize `retry_exhausted` to ensure the API envelope stays unchanged.
  - [x] Confirm existing frontend integration tests around retry-exhausted CTA rendering still pass without client-side patching.

## Dev Agent Record

### Implementation Plan

- Introduced unexported `retryExhausted(attempts int) bool` in `internal/service/scene_service.go` immediately after the `MaxSceneRegenAttempts` constant. This is the single read-model threshold helper called from all three sites.
- Replaced the three ad-hoc comparisons:
  - `ListReviewItems` (line 391): `attempts >= MaxSceneRegenAttempts` → `retryExhausted(attempts)`.
  - `RecordSceneDecision` (line 563): `attempts > MaxSceneRegenAttempts` → `retryExhausted(attempts)`. **This is the bug fix** — previously the read-model state at `attempts == cap` was reported as not-exhausted.
  - `DispatchSceneRegeneration` returned result (line 636): `attempts >= MaxSceneRegenAttempts` → `retryExhausted(attempts)`.
- Preserved the dispatch overflow mutation gate (`if attempts > MaxSceneRegenAttempts { … ErrConflict … }`) deliberately. Dev Notes call out this distinction: read-model becomes exhausted at the cap, but mutation is rejected only when the client tries to push past it.
- `MaxSceneRegenAttempts = 2` left untouched.

### Test Plan

- New file `internal/service/scene_retry_threshold_test.go` (package `service_test`):
  - `TestSceneService_RecordSceneDecision_RetryExhaustedAtCapBoundary` — direct regression for the `>` → `>=` fix in `RecordSceneDecision`. Seeds `regenAttempts = MaxSceneRegenAttempts` and asserts `RetryExhausted == true`. Without the fix this assertion fails.
  - `TestSceneService_RetryExhausted_AgreesAcross3Sites` — property test, 100 iterations. First exercises deterministic boundary-relevant values (`0, 1, cap-1, cap, cap+1, cap+5`), then deterministic-seeded random counts in `[0, 2*cap+5]`. Each iteration spins up isolated fixtures for all three sites with identical `attempts` and asserts:
    - `ListReviewItems[…].RetryExhausted == (attempts >= cap)`
    - `RecordSceneDecision(reject).RetryExhausted == (attempts >= cap)`
    - For `attempts <= cap`: `DispatchSceneRegeneration.RetryExhausted == (attempts >= cap)`
    - For `attempts > cap`: `DispatchSceneRegeneration` returns `ErrConflict` and the regenerator is **not** invoked (mutation gate semantics preserved).
- Existing `TestSceneService_ListReviewItems_RetryExhaustedAtCapBoundary` still passes (no behavior change at site 1 — the `>=` semantics there were already correct).

### Verification

- `go test ./internal/service/ -count=1` → ok (all service tests, including the new property test, green in 0.75 s).
- `go test ./...` → all Go packages green (api, db, pipeline, etc.).
- `npm run test:unit` (web/) → 35 files / 227 tests passed. Frontend `retry_exhausted` consumers in `BatchReview`, `SceneCard`, `DetailPanel` and `runContracts` remain unchanged at the contract layer.
- Property test: **100 iterations green** as required by §12 / R-11 / AI-3.

### Out of Scope (per AC-4 + Dev Notes)

The following deferred items are explicitly NOT touched by this story and remain on the deferred list:

- `CountRegenAttempts` counting superseded reject rows
- `BatchApprove` undo `aggregate_command_id` normalization
- `/regen` idempotency key
- macOS `Cmd+Z`
- Authoring of the `UI-E2E-05` Playwright spec (this story unblocks it; spec authoring belongs to the next `/bmad-qa-generate-e2e-tests` round, per test-design §13).

### Completion Notes

- AI-3 R-11 unblocked: the three server-side evaluation sites now read `retry_exhausted` through one shared helper. Future drift is structurally prevented because there is only one comparison expression to maintain.
- Boundary semantics: at `attempts == 2` all three sites report `retry_exhausted = true`; at `attempts == 3` `DispatchSceneRegeneration` returns `ErrConflict` and the regenerator is not called.
- No API envelope changes — `retry_exhausted` JSON field shape is identical; only the boundary value flips at `attempts == cap`.
- Frontend unit tests pass without modification, confirming the CTA rendering layer was already coded against the `>=` contract that the read-model now honors.

### File List

- Modified: `internal/service/scene_service.go` — added `retryExhausted` helper; replaced three threshold comparisons with helper calls.
- Added: `internal/service/scene_retry_threshold_test.go` — boundary regression + 100-iteration property test across the three sites.

### Change Log

- 2026-04-25 — Story 11.3 (AI-3): Unified `RetryExhausted` threshold on `>=` across `ListReviewItems`, `RecordSceneDecision`, and `DispatchSceneRegeneration` via shared `retryExhausted` helper. Fixed `RecordSceneDecision` boundary (was `>`, now `>=`). Preserved dispatch overflow mutation gate (`>`) so only the third attempt is rejected with `ErrConflict`. Added 100-iteration property test for cross-site agreement plus a dedicated cap-boundary regression test for `RecordSceneDecision`. No API contract changes.

## Dev Notes

- This story clears the P0/P1 blocker recorded as `AI-3` in the post-epic quality prompts and Step 3 dependency table: `RetryExhausted` `>=` / `>` mismatch across three sites.
- The concrete deferred item says the three sites must be unified on `>=` with a single shared constant and currently points to `internal/service/scene_service.go`.
- Current codebase reality:
  - `ListReviewItems` already uses `attempts >= MaxSceneRegenAttempts`
  - `RecordSceneDecision` still uses `attempts > MaxSceneRegenAttempts`
  - `DispatchSceneRegeneration` returns `RetryExhausted: attempts >= MaxSceneRegenAttempts` while separately blocking overflow dispatch on `attempts > MaxSceneRegenAttempts`
- Preserve that subtle distinction: read-model state becomes exhausted at the cap, while mutation blocking still happens only once the client tries to go past the cap.
- Do not absorb adjacent deferred items into this story:
  - `CountRegenAttempts` counting superseded reject rows
  - `BatchApprove` undo `aggregate_command_id` normalization
  - `/regen` idempotency key
  - macOS `Cmd+Z`

## References

- `_bmad-output/test-artifacts/test-design-epic-1-10-2026-04-25.md` §10, §12
- `_bmad-output/test-artifacts/quality-strategy-2026-04-25.md` §3 P0 / `RetryExhausted` row
- `_bmad-output/implementation-artifacts/deferred-work.md`
- `_bmad-output/implementation-artifacts/epic-1-10-retro-2026-04-24.md`
- `internal/service/scene_service.go`
- `internal/service/scene_service_test.go`
- `internal/api/handler_scene_test.go`
