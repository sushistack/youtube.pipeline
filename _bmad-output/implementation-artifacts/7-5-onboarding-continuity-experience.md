# Story 7.5: Onboarding & Continuity Experience

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want an onboarding guide and a continuity summary when resuming,
so that I always know what my next task is when I open the tool or return to the Production tab.

## Prerequisites

**Hard dependencies:** Stories 6.1, 6.2, 6.3, and 6.5 established the app shell, shared styling, keyboard infrastructure, and frontend test harness. Story 7.1 established the Production dashboard shell and `useRunStatus` polling contract. Story 2.6 already added the backend `GET /api/runs/{id}/status` payload fields `summary` and `changes_since_last_interaction` for continuity/resume UX.

**Parallel-work caution:** The current worktree already has in-flight Epic 7 edits touching `web/src/App.tsx`, `web/src/components/shells/ProductionShell.tsx`, `web/src/index.css`, and `web/src/stores/useUIStore.ts`. Implement Story 7.5 by layering onto those files carefully; do not revert or overwrite adjacent work from Stories 7.3/7.4.

**Backend dependency:** Story 7.5 is frontend-only. The continuity banner must consume existing backend status data; do not add new API endpoints or server fields.

## Acceptance Criteria

### AC-1: First-run onboarding modal appears exactly once per browser profile

**Given** the operator opens the SPA and no onboarding-dismissed flag exists in persisted client state  
**When** the app shell hydrates on any route  
**Then** a modal appears immediately with a short overview of the three workflow modes: Production, Tuning, and Settings  
**And** dismissing the modal persists a one-time flag in localStorage-backed UI state so it does not reappear on subsequent reloads or route changes.

**Rules:**
- Persist the first-run flag through the existing Zustand `persist` store, not an ad-hoc standalone localStorage key.
- The modal is route-global: it can render from `AppShell` or equivalent shared shell, not inside a single tab component.
- The modal must be dismissible by explicit close/continue action and `Esc`.
- This story does not add progressive step-by-step tours, coach marks, or analytics.

**Tests:**
- UI store test verifies onboarding flag defaults to `false` and persists once set.
- App-level test verifies modal shows on first render and stays dismissed after re-render when persisted state exists.

---

### AC-2: Onboarding content explains the 3-mode workflow without blocking future work

**Given** the onboarding modal is open  
**When** the operator reads the content  
**Then** the modal explains, in compact copy, what each route is for:
- Production: active run monitoring and HITL decisions
- Tuning: prompt/rubric evaluation and diagnostics
- Settings: provider/configuration management

**And** the modal includes a single primary dismissal CTA that returns focus to the underlying shell.

**Rules:**
- Keep copy brief and factual; no marketing language.
- Use existing typography/color tokens in `index.css`; do not introduce a new component library.
- The modal should be accessible: `role="dialog"`, labelled title, focusable dismissal control.

**Tests:**
- Component test verifies the three workflow-mode labels render.
- Accessibility-oriented test verifies dialog role and dismissal control are present.

---

### AC-3: Production tab shows a continuity banner only when server-reported state changed since the last seen session

**Given** the operator has previously visited the Production tab for a selected run  
**When** they re-enter Production later and the current run status payload differs from the last persisted snapshot for that run  
**Then** an inline continuity banner appears near the top of the Production surface with the title `"What changed since last session"`  
**And** the banner body is derived from backend continuity data, not hardcoded static copy.

**Rules:**
- Compare against a persisted client snapshot for the selected run at minimum containing `run_id`, `updated_at`, `stage`, and `status`.
- The banner only appears on Production entry when the snapshot indicates the run changed while the operator was away.
- Message source priority:
  1. `status_payload.changes_since_last_interaction` when non-empty
  2. `status_payload.summary` when present
  3. Otherwise do not show the banner
- Do not fabricate a banner from frontend-only heuristics if backend continuity data is absent.
- Banner placement is inline within `ProductionShell`, above the hero/dashboard content, and must coexist safely with FailureBanner work from Story 7.4.

**Tests:**
- ProductionShell integration test verifies no banner on first-ever Production visit for a run.
- Integration test verifies banner renders when persisted snapshot is stale and status payload contains backend diff/summary data.
- Integration test verifies banner does not render when snapshot and live run state match.

---

### AC-4: Continuity banner auto-dismisses after 5 seconds and cancels dismissal on unmount/interaction

**Given** the continuity banner is visible  
**When** 5 seconds elapse with no operator interaction  
**Then** the banner dismisses automatically.

**Given** the banner is visible  
**When** the operator interacts with the page before 5 seconds pass  
**Then** the banner dismisses immediately  
**And** the pending auto-dismiss timer is cancelled.

**Rules:**
- Interaction includes at least pointer/click and keyboard input while the banner is mounted.
- Timer/listener lifecycle must clean up on dismiss and unmount.
- This auto-dismiss logic is local UI behavior only; no network request is made.

**Tests:**
- Component or integration test with fake timers verifies auto-dismiss at 5000 ms.
- Test verifies keyboard or pointer interaction dismisses immediately and prevents the later timer callback from re-firing.

---

### AC-5: Dismissing or expiring the continuity banner updates the persisted last-seen snapshot

**Given** the continuity banner is shown for a run  
**When** it is dismissed manually or automatically  
**Then** the current run snapshot is stored as the new last-seen Production snapshot for that run  
**So that** the same unchanged state does not repeatedly trigger the banner.

**Rules:**
- Snapshot persistence belongs in the local UI store layer, not in TanStack Query cache metadata.
- Updating the last-seen snapshot should also happen on clean Production unmount so a later return compares against the most recent viewed state.
- The snapshot format should stay minimal and serializable; do not persist the full run payload or full diff array.

**Tests:**
- UI store test verifies snapshot write/read shape.
- ProductionShell integration test verifies a dismissed banner does not reappear on immediate remount with unchanged run data.

## Tasks / Subtasks

- [x] **T1: Extend persisted UI state for onboarding + Production continuity snapshots** (AC: #1, #3, #5)
  - Update `web/src/stores/useUIStore.ts` to add:
    - `onboarding_dismissed: boolean`
    - `dismiss_onboarding(): void`
    - `production_last_seen: Record<string, { run_id: string; updated_at: string; stage: RunStage; status: RunStatus }>`
    - `set_production_last_seen(snapshot): void`
  - Keep `partialize` limited to persisted UI fields only.
  - Update `web/src/stores/useUIStore.test.ts` for defaults, hydration, and snapshot persistence.

- [x] **T2: Create a shared `OnboardingModal` component and mount it from the app shell** (AC: #1, #2)
  - Create `web/src/components/shared/OnboardingModal.tsx`
  - Optional companion test: `web/src/components/shared/OnboardingModal.test.tsx`
  - Mount from `web/src/components/shared/AppShell.tsx` so it is route-global.
  - Dismiss action calls `dismiss_onboarding()` from the UI store.

- [x] **T3: Create a shared `ContinuityBanner` component** (AC: #3, #4, #5)
  - Create `web/src/components/shared/ContinuityBanner.tsx`
  - Props should stay narrow, e.g. `{ message: string; on_dismiss: () => void }`
  - Implement 5-second auto-dismiss with cleanup.
  - Register immediate dismissal on interaction while mounted.

- [x] **T4: Wire continuity detection into `ProductionShell`** (AC: #3, #4, #5)
  - Use existing `useRunStatus(selected_run?.id)` data as the authoritative continuity source.
  - On Production entry, compare the persisted last-seen snapshot for `current_run.id` against the live run.
  - Derive banner copy from `changes_since_last_interaction` first, else `summary`.
  - After manual or automatic dismissal, persist the current snapshot via the UI store.
  - On shell unmount, persist the current snapshot so future entries compare against the latest seen state.

- [x] **T5: Add copy/formatting helpers for continuity messages** (AC: #3)
  - Add a small helper near `web/src/lib/formatters.ts` or within `ContinuityBanner.tsx` to turn backend diff data into short human-readable copy.
  - Keep mapping deterministic and grounded in backend fields:
    - `scene_status_flipped`
    - `scene_added`
    - `scene_removed`
  - If multiple changes exist, show the first change plus a compact remainder indicator like `+2 more updates`.

- [x] **T6: Add shared styles in `web/src/index.css`** (AC: #1, #2, #3, #4)
  - Add modal surface styles for onboarding.
  - Add inline banner styles for continuity.
  - Preserve existing shell layout and spacing contracts.
  - Ensure the continuity banner remains visually subordinate to failure handling surfaces.

- [x] **T7: Add focused frontend test coverage** (AC: #1–#5)
  - Update `web/src/App.test.tsx` for first-run modal behavior.
  - Update `web/src/components/shells/ProductionShell.test.tsx` for continuity banner trigger/suppression.
  - Add component tests for `ContinuityBanner` timer + interaction dismissal.

### Review Findings

- [x] [Review][Patch] ContinuityBanner window listeners dismiss on unrelated typing / IME composition [web/src/components/shared/ContinuityBanner.tsx]
- [x] [Review][Patch] ProductionShell unmount persists snapshot unconditionally — swallows unseen continuity changes and stamps pre-status list rows [web/src/components/shells/ProductionShell.tsx]
- [x] [Review][Patch] ContinuityBanner 5s timer resets on every ProductionShell render (on_dismiss identity churn) [web/src/components/shared/ContinuityBanner.tsx]
- [x] [Review][Patch] formatContinuityChange produces "moved from X to X" when scene_status_flipped has before===after [web/src/lib/formatters.ts]
- [x] [Review][Patch] formatContinuityMessage returns empty/whitespace summary as non-null, producing a ghost banner [web/src/lib/formatters.ts]
- [x] [Review][Patch] OnboardingModal Esc handler fires during IME composition, breaking Hangul compose-cancel [web/src/components/shared/OnboardingModal.tsx]
- [x] [Review][Patch] formatContinuityChange lacks before-only (before && !after) branch for scene_status_flipped [web/src/lib/formatters.ts]
- [x] [Review][Defer] production_last_seen grows unbounded and has no rehydrate schema validation [web/src/stores/useUIStore.ts] — deferred, scale/versioning concern outside Story 7.5 acceptance scope
- [x] [Review][Defer] .status-bar remains tab-focusable when visually hidden [web/src/index.css] — deferred, pre-existing, not touched by this story

## Dev Notes

### Story Intent and Scope Boundary

- This story delivers lightweight orientation, not a guided product tour.
- Do NOT add backend changes, database migrations, or analytics events.
- Do NOT convert continuity into a toast system; UX specifically calls for an inline entry banner.
- Keep Failure UX as the highest-priority banner surface. If Story 7.4's FailureBanner is present, continuity should not displace or obscure it.

### Existing Backend Contract to Reuse

Story 2.6 already made `GET /api/runs/{id}/status` the continuity source. `web/src/contracts/runContracts.ts` already models:

- `summary?: string`
- `changes_since_last_interaction?: Array<{ kind, scene_id, before?, after?, timestamp? }>`

Use that payload directly. This satisfies the sprint prompt requirement that the Story 7.5 message comes from the Story 2.6 backend diff path rather than from a static frontend-only sentence.

### Recommended Continuity Detection Pattern

Use a minimal persisted snapshot per run:

```ts
type ProductionLastSeen = {
  run_id: string
  updated_at: string
  stage: RunStage
  status: RunStatus
}
```

Entry rule:
- no prior snapshot for `run.id` -> no banner, just persist current snapshot on exit/dismiss lifecycle
- prior snapshot exists and all four fields match -> no banner
- prior snapshot exists and any field differs -> inspect backend `changes_since_last_interaction` / `summary`

Message rule:
- non-empty `changes_since_last_interaction` -> summarize first backend diff item
- else `summary` present -> show `summary`
- else suppress banner and refresh snapshot

This keeps the banner tied to actual server-reported change data while still using local persisted state to answer "since last session."

### Component Placement Guidance

- `OnboardingModal` belongs in `web/src/components/shared/` and should be mounted from [AppShell.tsx](/mnt/work/projects/youtube.pipeline/web/src/components/shared/AppShell.tsx).
- `ContinuityBanner` belongs in `web/src/components/shared/` and should be mounted from [ProductionShell.tsx](/mnt/work/projects/youtube.pipeline/web/src/components/shells/ProductionShell.tsx).
- `ProductionShell` currently renders the hero, metrics, and stage-specific content slot. Insert the continuity banner above the hero block, while coordinating with the future FailureBanner slot from Story 7.4.

### Local State and Effects Guidance

- Persisted UI state should stay in Zustand with `persist`, consistent with the architecture notes.
- TanStack Query remains the server-state owner; do not stash continuity payloads in the UI store.
- `useEffect` is acceptable here for timer/listener setup and cleanup, and for persisting the latest snapshot on unmount. Avoid effect-driven derived-state loops.

### Suggested Copy Shape

Title:
- `What changed since last session`

Body examples derived from backend data:
- `Scene 4 moved from pending to approved`
- `1 new scene appeared in review`
- `Scenario review is waiting for your input`
- `Scene 2 was removed from the current review set`

If multiple backend diffs exist, append a short remainder hint:
- `Scene 4 moved from pending to approved (+2 more updates)`

### File Structure Requirements

New files:
- `web/src/components/shared/OnboardingModal.tsx`
- `web/src/components/shared/ContinuityBanner.tsx`
- `web/src/components/shared/ContinuityBanner.test.tsx`

Optional new file:
- `web/src/components/shared/OnboardingModal.test.tsx`

Updated files:
- `web/src/components/shared/AppShell.tsx`
- `web/src/components/shells/ProductionShell.tsx`
- `web/src/stores/useUIStore.ts`
- `web/src/stores/useUIStore.test.ts`
- `web/src/App.test.tsx`
- `web/src/index.css`

### Testing Requirements

Frontend-only verification is sufficient.

Minimum test coverage:
- onboarding modal appears once with empty persisted state
- onboarding modal stays dismissed when persisted flag exists
- continuity banner appears only when prior snapshot exists and live run changed
- continuity banner body uses backend diff/summary data
- banner auto-dismisses after 5 seconds
- interaction dismiss cancels the timer
- dismissed/unchanged snapshot does not cause repeat banner on immediate remount

Verification checklist before marking done:
- `cd web && npm run lint`
- `cd web && npm run test:unit`

### Previous Story Intelligence

- Story 7.1 established the current Production dashboard composition and the `useRunStatus` polling seam.
- Story 7.2 reinforced the pattern of keeping transient edit/view state local to the surface component and not widening Zustand unnecessarily.
- Story 7.3 extended `runContracts.ts`, `apiClient.ts`, and `ProductionShell.tsx`; Story 7.5 should build around those same seams instead of introducing parallel abstractions.
- Story 7.4 already reserves the top-of-dashboard banner area for failure handling. Keep the continuity banner secondary in both order and styling.

### Git Intelligence Summary

- Latest visible commit in this repo: `1e44084 Implement Epic 6: Web UI — Design System & Application Shell`
- The current worktree is dirty and already contains uncommitted Epic 7 work, including changes in `App.tsx`, `ProductionShell.tsx`, `index.css`, `useUIStore.ts`, and related tests.
- Story 7.5 should therefore be implemented as an additive frontend slice with extra care around merge boundaries.

### Project Context Reference

No separate `project-context.md` was present. Planning artifacts plus the live codebase were used as the authoritative context sources.

---

## Story Completion Status

- Story file created: `_bmad-output/implementation-artifacts/7-5-onboarding-continuity-experience.md`
- Story status: `review`
- Sprint status updated: `7-5-onboarding-continuity-experience: review`
- Completion note: Route-global onboarding and Production continuity surfaces now persist through the shared UI store and reuse backend continuity diffs without adding frontend-only heuristics.

---

## Dev Agent Record

### Implementation Plan

- Extend the persisted UI store with an onboarding dismissal flag plus per-run Production last-seen snapshots.
- Add shared onboarding and continuity components that keep accessibility, timer cleanup, and interaction dismissal local to the component layer.
- Wire Production continuity detection to `useRunStatus`, add deterministic diff formatting, then cover the flow with focused App, store, component, and shell tests.

### Debug Log

- Added Zustand persistence for `onboarding_dismissed` and `production_last_seen`, then expanded the store tests for defaults, hydration, serialization, and snapshot writes.
- Implemented `OnboardingModal` in the shared shell and `ContinuityBanner` in the Production shell, including `Esc` dismissal, 5-second auto-dismiss, and immediate keyboard/pointer dismissal.
- Refined continuity detection so the banner waits for the authoritative status payload before comparing snapshots, and moved unmount persistence to a ref-backed cleanup to avoid effect-driven render loops under React/Vitest.
- Verified the story slice with `cd web && npm run lint` and `cd web && npx vitest run src/stores/useUIStore.test.ts src/lib/formatters.test.ts src/components/shared/OnboardingModal.test.tsx src/components/shared/ContinuityBanner.test.tsx src/App.test.tsx src/components/shells/ProductionShell.test.tsx`.

### Completion Notes

- First-run onboarding now renders once per browser profile from `AppShell`, persists through the existing UI store, and returns focus to the main shell after dismissal.
- Production continuity now compares minimal persisted snapshots against the latest status payload and only shows the inline banner when backend-provided `changes_since_last_interaction` or `summary` data exists for a changed run.
- Dismissing or aging out the banner updates the last-seen snapshot, and unmount persistence prevents the same unchanged run state from retriggering the banner on immediate return.

### File List

- `web/src/App.test.tsx`
- `web/src/components/shared/AppShell.tsx`
- `web/src/components/shared/ContinuityBanner.test.tsx`
- `web/src/components/shared/ContinuityBanner.tsx`
- `web/src/components/shared/OnboardingModal.test.tsx`
- `web/src/components/shared/OnboardingModal.tsx`
- `web/src/components/shells/ProductionShell.test.tsx`
- `web/src/components/shells/ProductionShell.tsx`
- `web/src/contracts/runContracts.ts`
- `web/src/index.css`
- `web/src/lib/formatters.test.ts`
- `web/src/lib/formatters.ts`
- `web/src/stores/useUIStore.test.ts`
- `web/src/stores/useUIStore.ts`
