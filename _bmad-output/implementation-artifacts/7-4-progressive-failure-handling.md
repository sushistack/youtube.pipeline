# Story 7.4: Progressive Failure Handling (FailureBanner)

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want clear feedback and remediation actions when a stage fails,
so that I can quickly understand what went wrong and resume the pipeline without losing prior work.

## Prerequisites

**Hard dependencies:** Stories 6.1, 6.2, 6.3, 6.5 established the design system, keyboard engine, and frontend test harness. Story 7.1 established the Production dashboard shell, status polling contract, and run inventory. Story 7.2 established `ProductionShell` composition with the scenario inspector content slot. Story 7.4 extends the same shell without reworking the dashboard or status layer.

**Backend dependency:** Epic 2 already implemented `POST /api/runs/{id}/resume` (`handler_run.go:171`). The `retry_reason` field in the run response carries `"rate_limit"` for 429 failures per NFR-P3. **Story 7.4 is a pure frontend story — zero backend changes are needed.**

## Acceptance Criteria

### AC-1: FailureBanner renders for failed runs

**Given** the selected run has `status === 'failed'`  
**When** the ProductionShell renders  
**Then** a FailureBanner appears at the top of the content slot, above all other dashboard content, pushing it down  
**And** the banner shows:
- A failure message derived from `run.retry_reason` (see message-copy map in Dev Notes) or a default
- Cost-cap status: spend formatted as `formatCurrency(run.cost_usd)` (no cap percentage — cap value not in API)
- Explicit `"No work was lost. Completed stages remain intact."` message
- A `[Enter] Resume` CTA button with keyboard hint label visible inline

**Rules:**
- Banner mounts inside `production-dashboard`, at the top, as a direct sibling before all sub-sections
- Banner is inline — not a modal or fixed-position overlay; it pushes content down
- Other dashboard content (stage stepper, metrics, inspector) remains visible below the banner

**Tests:**
- Banner renders when `status === 'failed'`
- Banner shows `"No work was lost"` text
- Banner shows spend from `cost_usd` via `formatCurrency`
- Banner does NOT render when `status !== 'failed'`

---

### AC-2: Visual severity distinction — retryable (orange) vs fatal (red)

**Given** `run.status === 'failed'` and `run.retry_reason === 'rate_limit'`  
**When** FailureBanner renders  
**Then** banner applies `failure-banner--retryable` CSS class (orange left border, orange accent)

**Given** `run.status === 'failed'` and `run.retry_reason !== 'rate_limit'` (null or any other value)  
**When** FailureBanner renders  
**Then** banner applies `failure-banner--fatal` CSS class (red left border)

**Rules:**
- `retry_reason === 'rate_limit'` → retryable → orange (429-type, per NFR-P3)
- All other failed states → fatal → red (cost cap, stage failed, anti-progress, etc.)
- Variant affects border color only; layout is identical across variants

**Tests:**
- `retry_reason === 'rate_limit'` → `failure-banner--retryable` class present
- `retry_reason === null` → `failure-banner--fatal` class present

---

### AC-3: [Enter] Resume button triggers backend stage re-entry

**Given** the FailureBanner is visible  
**When** the operator clicks the Resume button or presses `[Enter]`  
**Then** `POST /api/runs/{id}/resume` fires (via `resumeRun` to be added to `apiClient.ts`)  
**And** the button shows a disabled/pending state during the request

**Rules:**
- Keyboard handler uses `useKeyboardShortcuts` (not raw `window.addEventListener`)
- `[Enter]` only fires when banner is mounted and mutation is not already in flight
- Resume body is `{}` (empty, maps to `confirm_inconsistent: false` default per `handler_run.go:173`)
- Existing keyboard engine suppresses `[Enter]` when a `<textarea>` or `<input>` is focused — rely on this, do not guard it separately

**Tests:**
- Resume button click fires the mutation with correct `run_id`
- Button is disabled while mutation is pending
- `[Enter]` keypress fires Resume when banner is mounted

---

### AC-4: Auto-dismiss on successful resume

**Given** the Resume mutation succeeds (200 OK from `POST /api/runs/{id}/resume`)  
**When** the mutation `onSuccess` callback fires  
**Then** the banner dismisses immediately (parent `dismissed` state → `true`)  
**And** `queryKeys.runs.list()` and `queryKeys.runs.status(run_id)` are invalidated to refresh run state

**Rules:**
- Dismiss immediately in `onSuccess` — do NOT wait for the next 5s status poll
- The run transitions to `running` after resume; `useRunStatus` resumes polling automatically once status becomes pollable
- Use `useQueryClient().invalidateQueries` inside mutation `onSuccess`

**Tests:**
- Successful resume mutation → banner is removed from the DOM
- Both `runs.list()` and `runs.status(run_id)` query keys are invalidated on success

---

### AC-5: Manual Esc / close-button dismiss

**Given** the FailureBanner is visible  
**When** the operator presses `Esc` or clicks the × close button in the banner  
**Then** the banner dismisses (parent `dismissed` state → `true`)  
**And** no API call is made

**Rules:**
- `Esc` dismiss is client-side only — does not resume or cancel the run
- `dismissed` state is owned by `ProductionShell`; page refresh or run-id change resets it
- Close button (×) must be focusable and keyboard-reachable

**Tests:**
- `Esc` key dismisses the banner without making API calls
- Close button click dismisses the banner without making API calls

---

## Tasks / Subtasks

- [x] **T1: Add `resumeRun` to `web/src/lib/apiClient.ts`** (AC: #3, #4)
  - Add `resumeRun(run_id: string): Promise<RunDetail>` using `runResumeResponseSchema` (already at `runContracts.ts:97`)
  - Pattern: same `apiRequest` helper as `fetchRunDetail` but method `POST` with empty body `{}`
  - No new Zod schema needed — `runResumeResponseSchema` already covers the response shape

- [x] **T2: Build `FailureBanner` component** (AC: #1, #2, #3, #4, #5)
  - Create `web/src/components/shared/FailureBanner.tsx`
  - Props: `run: RunSummary`, `on_dismiss: () => void`
  - Severity variant: `run.retry_reason === 'rate_limit'` → `failure-banner--retryable`; else `failure-banner--fatal`
  - Resume: `useMutation({ mutationFn: () => resumeRun(run.id), onSuccess: … })`
  - `onSuccess`: `on_dismiss()` + `queryClient.invalidateQueries(queryKeys.runs.list())` + `queryClient.invalidateQueries(queryKeys.runs.status(run.id))`
  - Keyboard: register `Enter` → `mutation.mutate()` (guarded: not when `mutation.isPending`) and `Esc` → `on_dismiss()` via `useKeyboardShortcuts`

- [x] **T3: Mount FailureBanner in `ProductionShell`** (AC: #1, #4, #5)
  - Track dismissed state per run id in `ProductionShell`, so switching runs naturally resets the banner without a `setState`-in-effect pattern
  - Render `<FailureBanner run={current_run} on_dismiss={() => set_dismissed_run_id(current_run.id)} />` at the top of `production-dashboard`, before `production-dashboard__hero`, when `current_run.status === 'failed'` and the current run has not been dismissed

- [x] **T4: Add FailureBanner CSS to `web/src/index.css`** (AC: #1, #2)
  - `.failure-banner`: inline card styling, left-border emphasis, responsive action row, `"No work was lost"` messaging
  - `.failure-banner--retryable`: orange left border
  - `.failure-banner--fatal`: red left border
  - Layout: top of content slot, inline (not `position: fixed`)

- [x] **T5: Add test contract fixture for failed run** (AC: #1, #2)
  - Added `testdata/contracts/run.failure.response.json` — `status: "failed"`, `retry_reason: "rate_limit"` (retryable variant)
  - Validated it through the existing frontend contract test suite

- [x] **T6: Add focused test coverage in `FailureBanner.test.tsx`** (AC: #1–#5)
  - Added coverage for render/no-render, retryable vs fatal styling, click resume, pending disable state, Enter resume, Escape dismiss, close-button dismiss, and query invalidation on success
  - Used `renderWithProviders` from Story 6.5 harness together with `KeyboardShortcutsProvider`
  - Mocked `resumeRun` via `vi.mock('../../lib/apiClient')`

---

## Dev Notes

### Story Intent and Scope Boundary

- **Pure frontend story.** Zero backend changes. Backend resume endpoint is complete and tested.
- `FailureBanner` goes in `web/src/components/shared/` (not `production/`) because it's a recovery surface that may later appear on non-Production tabs.
- Do NOT implement Sonner toast, milestone notifications, or the full UX-DR34 notification system in this story. FailureBanner is one of five notification patterns listed in UX-DR34; the rest belong to later epics.
- Do NOT touch the ScenarioInspector, CharacterGrid, or StageStepper — banner slots in above the existing conditional in ProductionShell.

### Current Codebase Reality

| What | Where | State |
|---|---|---|
| `POST /api/runs/{id}/resume` | `handler_run.go:171` | Complete, no changes needed |
| `runResumeResponseSchema` | `runContracts.ts:97` | Exists — import directly |
| `resumeRun` function | `apiClient.ts` | **Missing — add in T1** |
| `ProductionShell` banner slot | `ProductionShell.tsx:74` | Open — insert before `production-dashboard__hero` |
| `retry_reason` in run schema | `runContracts.ts:38` | `z.string().min(1).nullable().optional()` — `'rate_limit'` for 429 per NFR-P3 |
| `formatCurrency` | `formatters.ts:117` | Exists — reuse for cost display |
| `useKeyboardShortcuts` | `hooks/useKeyboardShortcuts.tsx` | Exists — use for Enter/Esc binding |
| `renderWithProviders` | test harness (Story 6.5) | Exists — use in FailureBanner.test.tsx |
| `queryKeys.runs` | `lib/queryKeys.ts` | Has `.list()` and `.status(id)` — use in mutation onSuccess |

### Failure Message Copy Map

```ts
function getFailureMessage(retry_reason: string | null | undefined): string {
  if (retry_reason === 'rate_limit') return 'DashScope rate limit — request throttled'
  if (retry_reason) return retry_reason  // show raw reason for unknown strings
  return 'Stage failed — check the run log for details'
}
```

### Domain Error ↔ Color Mapping (for reference)

| Domain Code | HTTP | Retryable | `retry_reason` value | Banner variant |
|---|---|---|---|---|
| `RATE_LIMITED` | 429 | true | `'rate_limit'` | orange (`--retryable`) |
| `STAGE_FAILED` | 500 | true | null (not set by NFR-P3) | red (`--fatal`) |
| `COST_CAP_EXCEEDED` | 402 | false | null | red (`--fatal`) |
| `ANTI_PROGRESS` | 422 | false | null | red (`--fatal`) |
| `UPSTREAM_TIMEOUT` | 504 | true | null | red (`--fatal`) |

Only `retry_reason === 'rate_limit'` is currently distinguishable from the run data. All other failures map to `--fatal` (red) regardless of actual retryability.

### Architecture Compliance

- Server state via TanStack Query v5 (`useMutation`, `useQueryClient`).
- Client-only ephemeral state (`dismissed`) in component/parent state — not in Zustand store.
- Keyboard via `useKeyboardShortcuts` engine (Story 6.3) — not raw `window` listeners.
- No `useEffect` for state transitions (react-hooks/set-state-in-effect ESLint rule from Story 7.2 review). Banner dismiss and mutation callbacks fire from event handlers and mutation `onSuccess` only. Exception: the `useEffect` in `ProductionShell` watching `current_run?.id` to reset `dismissed` is a legitimate side-effect of an external value change and is acceptable.

### Library / Framework Requirements

- React 19, TanStack Query v5, Zod v4, Vitest 4 — no new dependencies.
- `useKeyboardShortcuts` already handles the `Enter` guard inside `<textarea>`/`<input>` focus — do not add a second guard.
- `formatCurrency` from `web/src/lib/formatters.ts` handles USD formatting — reuse, do not inline `Intl.NumberFormat`.

### File Structure Requirements

New files:
- `web/src/components/shared/FailureBanner.tsx`
- `web/src/components/shared/FailureBanner.test.tsx`
- `testdata/contracts/run.failure.response.json`

Updated files:
- `web/src/lib/apiClient.ts` — add `resumeRun`
- `web/src/components/shells/ProductionShell.tsx` — add `dismissed` state + banner mount
- `web/src/index.css` — FailureBanner CSS classes

### Testing Requirements

No backend tests needed. All tests are frontend-only.

`FailureBanner.test.tsx` must cover:
- Renders for `status === 'failed'`, absent for other statuses
- Orange class for `retry_reason === 'rate_limit'`, red class for null
- Resume button triggers `resumeRun` mutation with correct run id
- Button disabled while `mutation.isPending`
- `onSuccess` → banner removed from DOM + query invalidation calls
- `Esc` closes without API call
- Close button closes without API call
- `[Enter]` key triggers resume mutation

Verification checklist before marking done:
- `npm run lint` passes
- `npm run test:unit` passes (all frontend tests including new ones)
- Visually confirm banner appears/dismisses in browser with a failed run (or mocked state)

### Previous Story Intelligence

- Story 7.2 review: `useEffect` for state updates → banned. Use event handlers and mutation callbacks instead. `dismissed` state reset via `useEffect` on `current_run?.id` is an exception (external value drives it, not a state-to-state transition).
- Story 7.2 established: `vi.mock('../../lib/apiClient')` as the standard mock pattern for client functions. Use the same approach for `resumeRun`.
- Story 7.2 established: `renderWithProviders` wraps components with required providers. Import from the test harness path.
- Story 7.1 left `ProductionShell` with a clear banner slot above the hero section. Use it — do not restructure the dashboard layout.
- Story 6.3 keyboard engine: `[Enter]` is suppressed inside focused `<textarea>` and `<input>` elements. The FailureBanner Resume shortcut will not fire while `ScenarioInspector` text editing is active. No additional guard needed.

### Git Intelligence Summary

- Latest commit: `1e44084 Implement Epic 6: Web UI — Design System & Application Shell`
- Epic 7 stories 7.1 and 7.2 are complete; 7.3 is `backlog`. Story 7.4 can proceed in parallel with 7.3 or after; no blocking dependency either way.
- Implementation order: T1 (add `resumeRun`) → T2 (build component) → T4 (CSS) → T3 (mount in shell) → T5 (fixture) → T6 (tests)

### Project Context Reference

No separate `project-context.md` was present. Planning artifacts plus the current codebase were used as authoritative context sources.

---

## Story Completion Status

- Story file created: `_bmad-output/implementation-artifacts/7-4-progressive-failure-handling.md`
- Story status: `review`
- Sprint status updated: `7-4-progressive-failure-handling: review`
- Completion note: Failure recovery banner implemented with resume/dismiss interactions, query invalidation, contract fixture coverage, and focused frontend tests

---

## Dev Agent Record

### Implementation Plan

- Add `resumeRun` to the shared API client using the existing response schema and request helper.
- Build a shared `FailureBanner` component with retryable/fatal styling, mutation-driven resume flow, and Enter/Escape keyboard bindings via `useKeyboardShortcuts`.
- Mount the banner above the Production dashboard hero, keep dismiss state local to `ProductionShell`, add CSS, add a failed-run fixture, and verify with lint/unit tests.

### Debug Log

- Implemented `resumeRun` in `web/src/lib/apiClient.ts` using `POST /api/runs/{id}/resume` with `{}` request body.
- Added `web/src/components/shared/FailureBanner.tsx` with failure copy mapping, spend display via `formatCurrency`, TanStack Query resume mutation, and Enter/Escape shortcut handling.
- Initial shell reset logic used `useEffect` to clear dismissed state on run-id changes, but ESLint `react-hooks/set-state-in-effect` rejected it.
- Reworked the shell to store `dismissed_run_id` instead, preserving the required per-run reset behavior without a synchronous effect state update.
- Added `testdata/contracts/run.failure.response.json` and expanded `web/src/contracts/runContracts.test.ts` to validate it.
- Added `web/src/components/shared/FailureBanner.test.tsx` covering render/no-render, severity class, click resume, pending disable state, Enter resume, Escape dismiss, close-button dismiss, and query invalidation on success.
- Verification completed with `npm run lint` and `npm run test:unit` in `web/`.

### Completion Notes

- AC 1-5 implemented in the frontend without backend changes.
- `ProductionShell` now shows an inline failure banner above all dashboard sections for failed runs, and dismisses it per run until the selected run changes.
- Successful resume dismisses immediately and invalidates both run list and run status queries so refreshed data can replace the failed state without waiting for the next poll cycle.
- Visual browser confirmation was not run in this turn; automated verification covered lint and unit tests only.

### File List

- `web/src/lib/apiClient.ts`
- `web/src/components/shared/FailureBanner.tsx`
- `web/src/components/shared/FailureBanner.test.tsx`
- `web/src/components/shells/ProductionShell.tsx`
- `web/src/index.css`
- `web/src/contracts/runContracts.test.ts`
- `testdata/contracts/run.failure.response.json`
