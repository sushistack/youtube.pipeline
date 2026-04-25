# Story 11.6: ProductionShell URL Race Fix

Status: review

## Story

As an operator,
I want a newly created run to remain selected immediately after creation,
so that the Production shell lands on the correct `?run=` URL instead of falling back to an older run during the post-create refetch window.

## Acceptance Criteria

1. The `Sidebar.handleNewRunSuccess` flow deterministically preserves the newly created run selection across dialog close and run-list invalidation:
   - after `POST /api/runs` succeeds, the URL ends in `?run=<new run id>`
   - the selected run in `ProductionShell` resolves to that same new run
   - no reactive refetch / rerender path may overwrite that explicit selection with fallback inventory selection
2. `ProductionShell` fallback auto-select remains a bootstrap-only behavior instead of racing an explicit post-create selection:
   - when the route has no `?run=` and the inventory has runs, the shell may still seed the first run with `replace: true`
   - when a valid explicit `?run=` already exists, the shell must not immediately replace it during the create-success path
   - the implementation must use one clear source of truth for post-create run selection rather than independent competing writes from `Sidebar` and `ProductionShell`
3. **Verification criterion carried forward exactly**: the existing `web/e2e/new-run-creation.spec.ts` is green again, and `cd web && npx playwright test` returns **15/15 green**. This is the exact blocker-release criterion recorded in `_bmad-output/planning-artifacts/post-epic-quality-prompts.md` Step 5 / Step 6 for Story 11-6.
4. Existing Production-shell navigation behavior stays intact apart from the race fix:
   - clicking an existing `RunCard` still updates `?run=<run id>`
   - first-load fallback selection still works when there is no explicit run in the URL
   - closing the New Run panel without creating a run still restores focus and does not mutate selection
5. Regression coverage proves the fix at both component and browser level:
   - `Sidebar` / `ProductionShell` tests cover the create-success refetch window and assert the new run stays selected
   - the Playwright create-run flow asserts the final URL and pending guidance against the newly created run, not a fallback run

## Requested Planning Output

### 1. Acceptance Criteria for blocker release

- Create-success URL selection survives dialog close + invalidateQueries refetch window
- `Verification`: existing `new-run-creation.spec.ts` green; `cd web && npx playwright test` returns **15/15 green**

### 2. Affected Files

Likely affected files for this story:

- `web/src/components/shared/Sidebar.tsx`
- `web/src/components/shells/ProductionShell.tsx`
- `web/src/components/shared/Sidebar.test.tsx`
- `web/src/components/shells/ProductionShell.test.tsx`
- `web/e2e/new-run-creation.spec.ts`

Possible support files if the coordination seam is extracted cleanly:

- `web/src/components/production/NewRunContext.tsx`
- `web/src/components/production/useNewRunCoordinator.ts`

### 3. E2E scenarios unblocked when this story is done

- Existing `web/e2e/new-run-creation.spec.ts` recovers from red to green
- Epic 11 Step 6 verification target: `cd web && npx playwright test` returns **15/15 green**

### 4. Estimated effort

- Jay solo estimate: **2-4 hours** implementation + local verification
- Add **30-60 minutes elapsed** for full Playwright rerun and one follow-up adjustment if the fix changes URL-write ordering

## Tasks / Subtasks

- [x] Task 1: Remove the post-create selection race between `Sidebar` and `ProductionShell` (AC: 1, 2)
  - [x] Trace the exact ordering between `handleNewRunSuccess`, `close_new_run_panel`, `invalidateQueries`, and the `ProductionShell` fallback `set_search_params(..., { replace: true })` effect.
  - [x] Refactor so the create-success path has one authoritative selection write for the new run.
  - [x] Ensure the fallback auto-select logic does not run over a newly written valid `?run=` during the same reactive cycle.

- [x] Task 2: Preserve existing selection and bootstrap semantics (AC: 2, 4)
  - [x] Keep existing RunCard selection behavior unchanged.
  - [x] Keep first-load fallback behavior for the no-`?run=` case.
  - [x] Avoid widening scope into unrelated inventory sort/filter behavior or new navigation state.

- [x] Task 3: Add regression coverage at the component level (AC: 5)
  - [x] Expand `Sidebar.test.tsx` or `ProductionShell.test.tsx` to simulate the create-success refetch window and assert that the new run remains selected.
  - [x] Assert the final URL/search-param state is the created run, not the previous fallback run.
  - [x] Cover any extracted helper/guard if the fix introduces one.

- [x] Task 4: Re-prove the browser flow with the existing E2E spec (AC: 3, 5)
  - [x] Re-run `web/e2e/new-run-creation.spec.ts` without modifying its core success assertion intent.
  - [x] Re-run `cd web && npx playwright test` and confirm the suite returns **15/15 green**.

## Dev Agent Record

### Implementation Plan / Notes

**Surprise during investigation — the failure was *not* the URL race the story
described.** The story's planning artifact assumed React 19 batching let
`ProductionShell`'s fallback effect win a race with `Sidebar.set_search_params`.
A targeted reproduction (debug `console.warn`s in both `handleNewRunSuccess`
and the fallback effect, captured via Playwright's `page.on('console')`)
showed `Sidebar.handleNewRunSuccess` was *never invoked at all*. The Go server
logged `run created` (HTTP 201), but the success callback never fired, so the
URL stayed bare.

Root cause: a Zod schema mismatch. `createRunResponseSchema` required
`error: z.null()`, but the Go envelope at `internal/api/response.go` declares
the field as `*apiError` with `json:"error,omitempty"` and therefore *omits*
the key entirely on success. Every other response schema in
`runContracts.ts` (`runDetailResponseSchema`, `runListResponseSchema`, …) also
omits `error` from the schema, matching the Go envelope. Only
`createRunResponseSchema` had the stale `error: z.null()` requirement, so
`createRun()` always threw a `ZodError` against the real server, the
`NewRunPanel.handleSubmit` `catch` block swallowed it as a server error, and
`on_success` was never reached. The Sidebar unit test passed only because its
mock explicitly sent `error: null`.

The actual URL race the story warned about is benign as long as `set_search_params`
fires: the existing fallback guard `if (selected_run_id) return` already
short-circuits the effect once a valid `?run=` is present. To formally satisfy
AC2 ("must use one clear source of truth") and to make the bootstrap intent
self-documenting, the fallback effect was rewritten to be **bootstrap-once** —
it captures whether the URL is authoritative on first eligible render and
never re-fires for the lifetime of the shell mount. This eliminates the
theoretical race even under future React-router/React-runtime changes that
could re-order the URL commit relative to the inventory refetch.

### Completion Notes

- **Root cause fix:** `web/src/contracts/runContracts.ts` — drop
  `error: z.null()` from `createRunResponseSchema` so it matches the
  Go envelope (`json:"error,omitempty"` on the `*apiError` pointer in
  `internal/api/response.go`).
- **Defense-in-depth fix (AC1/AC2 single source of truth):**
  `web/src/components/shells/ProductionShell.tsx` — the fallback selection
  effect now uses a `has_bootstrapped_selection_ref` guard so it runs at most
  once per mount, regardless of how many times the dependency array changes.
  On its first eligible run it either accepts the explicit `?run=` already in
  the URL or seeds the fallback with `replace: true`; afterwards it returns
  immediately. `Sidebar.handleNewRunSuccess` therefore stays the sole writer
  of post-create selection. Note: `matched_selected_run` was dropped from the
  dependency array because the bootstrap guard makes it irrelevant — the
  effect can never re-run, so the dependency would only add noise.
- **Component regression test:** `web/src/components/shells/ProductionShell.test.tsx`
  — added an integration test that mounts `AppShell` (Sidebar + ProductionShell
  together) on `/production` against an empty inventory, drives the full
  create-run flow, and asserts (1) the URL ends up at `?run=<new id>`,
  (2) the dialog closed, (3) the pending guidance card resolves to the new
  run, and (4) the URL is still stable after the post-create inventory
  refetch. The mock POST response intentionally omits the `error` key,
  mirroring the Go server's wire format.
- **No file deletions, no scope widening.** The deferred sidebar push-vs-replace
  cleanup item (per `deferred-work.md`) was not absorbed.
- **Out-of-scope flake observed (not introduced):** `e2e/batch-review-chord.spec.ts`
  ("Esc opens reject composer" → "cancel button click") flaked once across
  three full-suite runs (run 1: 15/15 green, run 2: 14/15 with composer
  cancel detached-from-DOM error, run 3: 15/15 green). The flake is in the
  BatchReview cancel/composer handshake — completely orthogonal to Sidebar /
  ProductionShell selection. Story 11-6 explicitly forbids absorbing
  unrelated polish items, so this is left to a follow-up.

### File List

- `web/src/contracts/runContracts.ts` (modified) — remove `error: z.null()` from `createRunResponseSchema`
- `web/src/components/shells/ProductionShell.tsx` (modified) — rewrite fallback selection effect as bootstrap-once via `has_bootstrapped_selection_ref`
- `web/src/components/shells/ProductionShell.test.tsx` (modified) — add `SelectedRunProbe` helper + post-create refetch-window regression test (8 → 9 cases for `ProductionShell.test.tsx`, suite-wide vitest 226 → 227 total)

### Change Log

- 2026-04-25 (Amelia / dev) — Story 11-6: fix `createRunResponseSchema` to match the Go envelope (omitempty error key), and rewrite the `ProductionShell` fallback selection effect as bootstrap-once so it can never overwrite an explicit post-create `?run=` selection. Adds a component-level regression test that mounts the full shell against an empty inventory, drives the create-run flow, and asserts the URL/dialog/pending-guidance state across the inventory refetch window. Verified locally: vitest 227/227 green (35 files); playwright 15/15 green on first and third full-suite runs (one `batch-review-chord` flake on run 2 is a pre-existing, out-of-scope BatchReview composer issue). `web/e2e/new-run-creation.spec.ts` runs 5/5 green under `--repeat-each=5`. `tsc -b` clean.

## Dev Notes

- This story is a **post-Step-4 discovery blocker**, not one of the original Step 2 `§3 P0` rows. The blocker was recorded after UI E2E Round 1 in `_bmad-output/planning-artifacts/post-epic-quality-prompts.md` and intentionally split into its own small Story 11-6 because the failure is in production code, not in Playwright infrastructure.
- The concrete failure mode from the planning artifact must remain the scope anchor:
  - `Sidebar.handleNewRunSuccess` calls `set_search_params`
  - `close_new_run_panel` and `invalidateQueries` trigger reactive updates
  - `ProductionShell` fallback selection effect can win the race
  - result: the newly created run exists, but URL `?run=` is missing or points at the fallback selection instead
- Relevant current code paths:
  - `web/src/components/shared/Sidebar.tsx` writes `?run=` on create success and invalidates `queryKeys.runs.list()`
  - `web/src/components/shells/ProductionShell.tsx` auto-seeds fallback `?run=` with `{ replace: true }` whenever there is no selected run in the URL
- Keep this story intentionally small. Do **not** absorb adjacent deferred/polish items unless the chosen fix naturally eliminates them with low risk:
  - sidebar push-vs-replace back-stack cleanliness follow-up from `deferred-work.md`
  - inventory filtering / sorting changes
  - new page objects, new Playwright specs, or test-infra-only workarounds

## References

- `_bmad-output/planning-artifacts/post-epic-quality-prompts.md` Step 4, Step 5, Step 6
- `_bmad-output/test-artifacts/test-design-epic-1-10-2026-04-25.md` §5 (existing UI E2E surface / file references)
- `_bmad-output/test-artifacts/quality-strategy-2026-04-25.md` §3 (adjacent UI quality-gate context)
- `_bmad-output/implementation-artifacts/deferred-work.md`
- `_bmad-output/implementation-artifacts/epic-1-10-retro-2026-04-24.md`
- `web/src/components/shared/Sidebar.tsx`
- `web/src/components/shells/ProductionShell.tsx`
- `web/src/components/shared/Sidebar.test.tsx`
- `web/src/components/shells/ProductionShell.test.tsx`
- `web/e2e/new-run-creation.spec.ts`
