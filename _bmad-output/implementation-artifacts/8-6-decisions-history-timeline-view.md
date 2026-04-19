# Story 8.6: Decisions History & Timeline View

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want to browse the full chronological history of my review decisions with type and reason filters,
so that I can audit my own workflow, recover the reasoning behind past actions, and understand how a run's review state evolved.

## Prerequisites

**Hard dependencies:** Story 6.1/6.2/6.3 established the design system, shell layout, and the shared keyboard shortcut engine (including editable-target suppression for `J/K`). Story 8.1 established the batch-review queue pattern and its `J/K` + `scope: 'context'` keyboard registration idiom. Story 8.2 introduced the canonical per-scene decision write path and the `context_snapshot` convention for storing structured operator context. Story 8.3 defined the V1 undo model (`decisions.superseded_by` reversal rows with no hard delete) — the timeline MUST render both the original decision and its reversal row and clearly mark superseded state rather than hide it.

**Backend dependency:** Story 2.7 already added `idx_decisions_run_id_type` and `idx_decisions_created_at` (migration `005_metrics_indexes.sql`) for the metrics CLI. Story 8.6 must reuse those indexes for timeline queries and add at most one additional index only if the V1 read path cannot be served by them. Stories 2.6 and 5.4 established the `decisions` row shape used throughout the pipeline; Story 8.6 MUST NOT widen the schema beyond the existing `decision_type`, `context_snapshot`, `scene_id`, `note`, `superseded_by`, `created_at` columns.

**Architecture guardrail:** `architecture.md` positions decisions history as a read-only, passive-capture surface. `ux-design-specification.md §2273-2281` scopes V1 TimelineView to a **single scrollable list, not a master/detail pattern**, with a filter bar, `J/K` navigate, and a V1.5 Patterns toggle (out of scope here). The UX filter-debounce guidance is explicitly **100ms client-side** on a bounded page (UX spec §2387-2389) — not a 300ms server-side search convention. Story 8.6 MUST follow that timing and MUST NOT reach for a debounce framework or a new network request per keystroke.

**Parallel-work caution:** `web/src/components/shells/SettingsShell.tsx`, `web/src/App.tsx` routing, `web/src/components/shared/Sidebar.tsx`, `web/src/index.css`, `web/src/lib/apiClient.ts`, `web/src/contracts/runContracts.ts`, `web/src/lib/queryKeys.ts`, `internal/api/routes.go`, `internal/api/handler_scene.go`, `internal/service/scene_service.go`, and `internal/db/decision_store.go` are active integration points across Epic 7 and Epic 8. Stories 8.3/8.4/8.5 are still in flight in the same files — layer additively and do not revert adjacent work.

**Scope boundary with Epic 10:** `epics.md §597-599` eventually places TimelineView inside the **Settings & History tab** with override-rate metric, production-velocity delta, and burnout-detection. Story 8.6 builds the TimelineView primitive only; it MUST NOT implement override-rate/velocity/burnout (Epic 10 scope) or the V1.5 Patterns aggregation lens. The route host for V1 is the existing `/settings` shell, re-purposed with a `"Decisions history"` section — not a new top-level nav item.

## Acceptance Criteria

### AC-1: Settings shell renders a chronological decisions timeline with required row fields

**Given** the operator navigates to `/settings`
**When** the shell renders
**Then** a `TimelineView` section is visible alongside any existing settings content
**And** the timeline shows rows ordered by `created_at` **descending** (most recent first)
**And** each row displays at minimum: monospaced `timestamp`, `scp_id` (resolved from `decisions.run_id → runs.scp_id`), `decision_type`, originating scene reference (`scene_id` when present), and the reason string derived from `note` (falling back to a `context_snapshot.reason` field when `note` is null)
**And** superseded decisions are visually distinguished (e.g. strikethrough row or "undone" badge) but still rendered — undo history is visible, not deleted.

**Rules:**
- V1 renders decisions across **all runs** in a single global timeline. A future stage-scoped filter is acceptable but not required.
- Row uses a monospaced font only for the timestamp cell; body text stays in the design-system default typography.
- Reason extraction precedence: `note` → `context_snapshot.reason` (parsed as JSON best-effort) → empty string. A missing reason MUST NOT render as `"null"` or `"undefined"`.
- Timezone: display timestamps in the browser's local timezone; the backend continues to store UTC strings as produced by SQLite `datetime('now')`.
- The timeline lives inside `SettingsShell`. Do NOT add a new top-level route, a new sidebar entry, or move the existing `/settings` path.

**Tests:**
- Backend service test verifies the timeline payload is ordered `created_at DESC`, includes `scp_id` resolved from the joined `runs` row, and includes superseded decisions with their `superseded_by` link intact.
- Component test (`TimelineView.test.tsx`) verifies the required fields render per row and that a superseded row carries the "undone" visual marker.
- Contract test verifies the timeline response envelope shape via Zod.

---

### AC-2: Filter bar supports decision-type filter and reason-text search with 100ms client-side debounce

**Given** the TimelineView is rendered with at least one page of decisions
**When** the operator selects a `decision_type` in the filter bar (e.g. `"reject"`, `"approve"`, `"skip_and_remember"`, `"descriptor_edit"`, `"undo"`, `"system_auto_approved"`, `"override"`)
**Then** the visible rows are restricted to that type within the currently loaded page
**And** the filter applies **client-side** without issuing a new API request per selection.

**Given** the operator types into the reason search input
**When** 100ms elapses after the last keystroke
**Then** rows whose `note` (or `context_snapshot.reason`) case-insensitively contains the typed substring remain visible
**And** the search applies **client-side** over the loaded page; it does NOT trigger a per-keystroke API request.

**Rules:**
- Debounce is exactly `100ms` per UX spec §2387-2389. Do not implement `300ms`, do not leave the input undebounced, and do not add a heavyweight debounce dependency; a minimal custom debounce (`setTimeout` + `clearTimeout`, or a ~10-line hook) is sufficient.
- The decision-type control is a discrete selector (`select`/segmented control/chip group). V1 acceptable options: `all`, `approve`, `reject`, `skip_and_remember`, `descriptor_edit`, `undo`, plus `system_auto_approved` and `override` for completeness. Values MUST come from domain constants, not hardcoded literals.
- An active filter MUST be cancellable in one action (clear-all button or `X` on each active filter chip). Clearing restores the full loaded page.
- The filter state is UI-local; do NOT persist it to Zustand, localStorage, or URL params in V1. Navigating away and back is allowed to reset the filter.
- Filtering logic MUST run over the decoded/typed row array, not over the raw JSON text, to avoid matching against JSON keys.

**Tests:**
- Component test with fake timers verifies exactly-100ms debounce: 99ms yields no filter application; 100ms does.
- Component test verifies reason-search is case-insensitive and matches substrings.
- Component test verifies decision-type filter restricts rows and `"all"` restores them.
- Regression test verifies the filter bar does NOT issue a new `fetch`/`useQuery` network request per keystroke (mock API call count stays at the initial page fetch count).

---

### AC-3: J/K navigate the timeline selection and scroll the selected row into view

**Given** the TimelineView is mounted and contains at least two rows
**When** the operator presses `J`
**Then** selection moves to the next visible (filtered) row
**And** the selected row gets `aria-selected="true"` and the selected visual state
**And** the selected row scrolls into view if it was outside the viewport.

**Given** the first row is selected
**When** the operator presses `K`
**Then** selection moves to the previous visible row or stays bounded at index 0.

**Rules:**
- Registration MUST use the shared `useKeyboardShortcuts` engine with `scope: 'context'` and `allow_in_editable: false` so typing in the reason search input does NOT trigger navigation. Do NOT add raw `window.addEventListener('keydown', ...)` listeners.
- Navigation operates on the **currently visible, filtered** row list, not the full loaded page. Applying a filter that excludes the selected row MUST fall back to selecting the first visible row.
- Shortcuts are active only while the timeline is mounted. They MUST NOT leak into Production or Tuning surfaces.
- V1 is **read-only navigation**. There is no detail panel, no "open", and no `Enter` handler. Do NOT repurpose the master/detail pattern from Story 8.1.

**Tests:**
- Component test verifies `J` advances selection and `K` retreats it within the filtered list.
- Component test verifies bounded behavior at both edges (first row + K, last row + J) and that selection resets to index 0 when the filter excludes the prior selection.
- Component test verifies shortcuts are suppressed while the reason search input is focused (`allow_in_editable: false`).
- Regression test verifies the timeline does not register any global `window` keydown listener.

---

### AC-4: Timeline query is indexed for large decision sets and paginates deterministically

**Given** the `decisions` table contains many non-superseded rows (V1 performance target: at least 1000 rows without UI lag)
**When** the backend serves the timeline list
**Then** the SQL query uses an index for its primary ordering key
**And** the query plan does NOT show a full table scan for the default ordering (`created_at DESC`) or for a filtered-by-`decision_type` query
**And** the response is bounded by a server-side `limit` (default `100`, max `500`) with deterministic ordering so subsequent pages are consistent.

**Rules:**
- Reuse `idx_decisions_created_at` for the default ordering. For the decision-type-filtered path, the existing `idx_decisions_run_id_type` covers per-run filtering only. If the V1 cross-run timeline + type filter cannot avoid a scan, add ONE migration (`010_decisions_timeline_indexes.sql` or equivalent) with `CREATE INDEX IF NOT EXISTS idx_decisions_type_created_at ON decisions(decision_type, created_at);`. Do NOT add more than strictly necessary; justify the addition in the migration header.
- Ordering MUST be `created_at DESC, id DESC` so ties on identical timestamps (SQLite `datetime('now')` has 1-second resolution) do not produce flapping order between calls.
- Pagination is keyset-first: accept optional `before_created_at` and `before_id` query params; return the next cursor in the response envelope. Offset pagination is acceptable as a fallback only if keyset complexity is disproportionate — document the choice inline.
- The store method MUST be driven by an explicit options struct, not a growing positional arg list. Example: `TimelineListOptions{ DecisionType *string; Limit int; BeforeCreatedAt *string; BeforeID *int64 }`.
- Always include superseded rows. This is a history surface, not a live-state surface. The consumer (UI) handles the visual treatment.

**Tests:**
- Store test seeds 1000+ decisions spanning multiple runs and asserts:
  - default query returns `limit` rows in `created_at DESC, id DESC` order
  - filter-by-type reduces the result to matching rows
  - keyset pagination with `before_created_at`/`before_id` returns the next page without overlap or gaps
  - `EXPLAIN QUERY PLAN` (or equivalent assertion) confirms an index is used for the default and filtered paths
- Store test verifies superseded rows ARE included in the timeline output.
- Service test verifies the `scp_id` join returns the correct label per decision.

---

### AC-5: Timeline surface handles empty, loading, and error states cleanly

**Given** no decisions exist yet anywhere in the system
**When** TimelineView renders
**Then** it shows a non-empty, non-blocking empty state (e.g. "No decisions yet.") without breaking the Settings shell layout.

**Given** the timeline API call is in flight
**When** TimelineView renders
**Then** it shows a lightweight loading indicator and does NOT render an empty-state message or flicker between empty and loaded.

**Given** the timeline API call fails
**When** TimelineView renders
**Then** it shows a recoverable error state with a retry affordance and does NOT unmount the rest of the settings shell.

**Rules:**
- Reuse the existing design-system loading + error idioms from `BatchReview.tsx` (loading div with `aria-busy="true"`, error div with `role="alert"`). Do NOT invent a new pattern here.
- The empty-state message MUST NOT block keyboard focus on the rest of Settings; the section remains keyboard-reachable.
- Do NOT add toast/notification side-effects for load failures in V1. The inline error region is sufficient.

**Tests:**
- Component test verifies the three states render with the correct aria attributes and do not overlap.
- Test verifies the retry action triggers a refetch.

## Tasks / Subtasks

- [x] **T1: Add timeline read contract to `DecisionStore`** (AC: #1, #4)
  - Add `ListTimeline(ctx, opts)` to `internal/db/decision_store.go` returning a paged slice of domain decisions plus the next keyset cursor.
  - Options struct: `TimelineListOptions { DecisionType *string; Limit int; BeforeCreatedAt *string; BeforeID *int64 }`. Clamp `Limit` to `[1, 500]` with default `100`.
  - Query ordering: `ORDER BY created_at DESC, id DESC`. For the type-filtered path, prefer the existing `idx_decisions_run_id_type` when feasible; if a cross-run type filter is required, add a new composite index via migration (see T2).
  - Include superseded rows (do NOT filter by `superseded_by IS NULL`). Rename `ListByRunID` semantics are UNCHANGED — do not alter its callers.
  - Extend scan target to capture `scp_id` via a `JOIN runs r ON r.id = d.run_id`; return a small DTO (`TimelineDecision`) carrying the resolved `scp_id` so the service does not need N+1 lookups.

- [x] **T2: Add an index migration only if required** (AC: #4)
  - First, prove via a benchmark test or query-plan check whether `idx_decisions_created_at` alone is sufficient for the default + type-filtered paths on 1000+ rows.
  - If insufficient, add `migrations/010_decisions_timeline_indexes.sql` creating `idx_decisions_type_created_at ON decisions(decision_type, created_at)`. Include a header comment explaining why the existing indexes are insufficient.
  - Do NOT add per-column indexes speculatively (no free-floating `scene_id` or `note` indexes — V1 reason search is client-side and bounded by page size).

- [x] **T3: Expose a timeline endpoint via `SceneService` and `SceneHandler`** (AC: #1, #4)
  - Add `SceneService.ListDecisionsTimeline(ctx, opts)` returning a `[]*TimelineDecisionResponse` plus pagination cursor.
  - No stage gate — decision history is readable regardless of the active run stage. (The per-run stage gates on `ListReviewItems`/`RecordSceneDecision` are unrelated and must stay intact.)
  - Add `GET /api/decisions` in `internal/api/routes.go` with handler parsing `decision_type`, `limit`, `before_created_at`, `before_id` query params. Validate values; reject unknown `decision_type` with `400` / `VALIDATION_ERROR`.
  - Use the existing response envelope pattern `{ data: { items, total|next_cursor }, version: 1 }`.
  - Keep this endpoint under `/api/decisions`, **not** `/api/runs/{id}/decisions`, so the V1 cross-run timeline does not re-enter the run-scoped namespace. (A future per-run variant is out of scope.)

- [x] **T4: Extend frontend contracts and API client** (AC: #1)
  - Add Zod schemas in `web/src/contracts/runContracts.ts`:
    - `timelineDecisionSchema` with `id`, `run_id`, `scp_id`, `scene_id` (nullable), `decision_type`, `note` (nullable), `reason_from_snapshot` (nullable), `superseded_by` (nullable), `created_at`.
    - `timelineListResponseSchema` envelope with `items`, `next_cursor` (nullable).
  - Add `fetchDecisionsTimeline(params)` in `web/src/lib/apiClient.ts` accepting `{ decision_type?: string; limit?: number; before_created_at?: string; before_id?: number }`.
  - Add `queryKeys.decisions.timeline(params)` to `web/src/lib/queryKeys.ts` (new top-level `decisions` namespace — do NOT nest under `runs`).
  - Add contract tests covering the envelope + nullable fields.

- [x] **T5: Build the `TimelineView` component** (AC: #1, #2, #3, #5)
  - Create `web/src/components/settings/TimelineView.tsx` (new `settings/` folder under `components/`; mirrors the `production/` + `shared/` convention).
  - Fetch via TanStack Query with `queryKeys.decisions.timeline({})` as the initial page. Keep pagination plumbing minimal in V1 — a single page is acceptable if the test harness does not need cursor traversal.
  - Responsibilities:
    - render rows per AC-1
    - own filter state (decision type selector + reason search input)
    - apply 100ms debounced client-side filter
    - own selection index and register `J/K` shortcuts with `useKeyboardShortcuts`
    - render empty/loading/error states
  - Use function-local `useMemo` for filtered rows; do NOT re-run the filter per render without memoization.
  - Follow the snake-case-for-identifiers convention already present in `BatchReview.tsx`, `SceneCard.tsx`, `Sidebar.tsx`.

- [x] **T6: Mount TimelineView inside `SettingsShell`** (AC: #1)
  - Update `web/src/components/shells/SettingsShell.tsx` to render the existing intro plus a `<TimelineView />` section.
  - Add semantic sectioning (`<section aria-labelledby="settings-history-title">`).
  - Do NOT add a new route, a new sidebar entry, or rename `/settings`. The sidebar label MAY be updated from `"Settings"` to `"Settings & History"` in `Sidebar.tsx` if that flows naturally from the UX spec §2273 ("Decisions history in Settings & History"); treat that label change as optional polish, not required scope.
  - Preserve existing `SettingsShell` content so its test does not regress.

- [x] **T7: Styling for timeline rows and filter bar** (AC: #1, #2, #3)
  - Add styles in `web/src/index.css`:
    - `.timeline-view` container + scrollable list region
    - `.timeline-row` base + `--selected` variant
    - `.timeline-row--superseded` strikethrough/muted treatment
    - `.timeline-row__timestamp` monospace
    - `.timeline-filter-bar` layout (select + search input + clear button)
  - Reuse existing design-system tokens. Do NOT introduce a new color or spacing scale.

- [x] **T8: Focused backend and frontend tests** (AC: #1-#5)
  - Backend:
    - `decision_store_test.go`: timeline ordering, keyset pagination, type filter, inclusion of superseded rows, index usage on 1000+ rows
    - service test: `scp_id` join correctness, validation of `limit` bounds
    - handler test: success envelope, invalid `decision_type` → 400, pagination query param round-trip
  - Frontend:
    - `TimelineView.test.tsx`: row fields, superseded marker, debounced filter (fake timers), type filter, J/K navigation, bounded edges, empty/loading/error states, no network request per keystroke
    - `SettingsShell.test.tsx`: renders intro + timeline together
    - contract test: new Zod schemas parse representative fixtures
  - Verify with `go test ./...` and `npm --prefix web run test:unit`.

## Dev Notes

### Story Intent and Scope Boundary

- Story 8.6 builds a **read-only, single-list** timeline of all decisions.
- Do NOT implement the V1.5 Patterns view, CommandPalette, override-rate metric, production-velocity delta, burnout-detection, or JSON data export (all Epic 10 or V1.5 scope).
- Do NOT introduce a master/detail split, scene-detail panel, or any mutation (undo already lives in Story 8.3 and is triggered from the review surface, not from history).
- Do NOT widen `decisions` schema. If V1 query perf is inadequate with existing indexes, add at most one composite index.
- Do NOT bind decision-type filter values to raw string literals scattered across the UI. Source them from domain constants re-exported for the frontend.

### Current Codebase Reality

| What | Where | State |
|---|---|---|
| `decisions` table schema | `migrations/001_init.sql:21-35` | Columns available: id, run_id, scene_id, decision_type, context_snapshot, outcome_link, tags, feedback_source, external_ref, feedback_at, superseded_by, note, created_at |
| Existing indexes | `migrations/005_metrics_indexes.sql` | `idx_decisions_run_id_type(run_id, decision_type, superseded_by)`, `idx_decisions_created_at(created_at)` |
| `ListByRunID` | `internal/db/decision_store.go:65-90` | Filters `superseded_by IS NULL`, ordered `created_at ASC`, per-run only. Story 8.6 needs a DIFFERENT method — do not modify this one |
| Decision type constants | `internal/domain/review_gate.go:22-31` | `approve`, `reject`, `skip_and_remember`, `system_auto_approved`, `override`, `undo`, `descriptor_edit` |
| `SceneService` decision read | `internal/service/scene_service.go:44-46` | `ListByRunID` adapter only — no cross-run timeline method |
| `SceneHandler` routes | `internal/api/handler_scene.go`, `internal/api/routes.go:26-42` | No `/api/decisions` endpoint yet |
| SettingsShell | `web/src/components/shells/SettingsShell.tsx` | Minimal stub (static copy). Timeline is additive |
| Sidebar nav | `web/src/components/shared/Sidebar.tsx:16-20` | `Production / Tuning / Settings`. Timeline lives INSIDE `/settings`; no new nav item |
| Keyboard engine | `web/src/hooks/useKeyboardShortcuts.tsx`, `web/src/lib/keyboardShortcuts.ts` | Supports `scope: 'context'` and `allow_in_editable: false`. Reuse; do NOT add `window.addEventListener` |
| Contracts + API client | `web/src/contracts/runContracts.ts`, `web/src/lib/apiClient.ts`, `web/src/lib/queryKeys.ts` | Zod + TanStack v5 patterns already established |
| Reference idiom for J/K | `web/src/components/production/BatchReview.tsx:214-275` | Registers `j/k/enter/escape/s/ctrl+z` via `useKeyboardShortcuts` with `scope: 'context'` |

### Backend Read-Path Recommendation

The timeline endpoint is cross-run and read-only. Recommended layering mirrors the Story 8.1 `review-items` approach:

1. Add a narrow `TimelineDecision` DTO in `internal/db/decision_store.go`:

```go
type TimelineDecision struct {
    ID              int64
    RunID           string
    SCPID           string   // from JOIN runs
    SceneID         *string
    DecisionType    string
    ContextSnapshot *string
    Note            *string
    SupersededBy    *int64
    CreatedAt       string
}

type TimelineListOptions struct {
    DecisionType    *string
    Limit           int
    BeforeCreatedAt *string
    BeforeID        *int64
}
```

2. SQL shape (default path):

```sql
SELECT d.id, d.run_id, r.scp_id, d.scene_id, d.decision_type,
       d.context_snapshot, d.note, d.superseded_by, d.created_at
  FROM decisions d
  JOIN runs r ON r.id = d.run_id
 WHERE (? IS NULL OR d.decision_type = ?)
   AND (? IS NULL OR (d.created_at, d.id) < (?, ?))
 ORDER BY d.created_at DESC, d.id DESC
 LIMIT ?
```

The SQLite `ROW VALUE` comparison `(a, b) < (?, ?)` is supported and keeps keyset pagination ordering consistent.

3. Add `ListDecisionsTimeline` on `SceneService` with light validation (limit bounds, `decision_type` ∈ domain set). No stage gate.

### Reason Extraction Precedence

The `decisions` table stores operator-supplied reasons inconsistently across decision types:

- `reject` (Story 8.4, in flight): reason lands in `note`.
- `skip_and_remember` (Story 8.2): structured context in `context_snapshot` JSON; may include a `reason` field or equivalent — inspect the writer before relying on it.
- `descriptor_edit` (Story 8.3): `context_snapshot` stores `{ command_kind, before, after }` — there is NO natural reason field; render an empty reason.
- `approve` / `override`: typically no reason.

Backend MAY return `note` only and let the client attempt best-effort JSON parse of `context_snapshot.reason`. Either approach is acceptable; pick one and be consistent. If the backend parses, surface the parsed value as a separate field (`reason_from_snapshot`) rather than overwriting `note`.

### Index Strategy for Large Sets

V1 perf target is "no UI lag at 1000+ rows". The existing `idx_decisions_created_at` alone covers the default ordering path. The type-filtered cross-run path (`WHERE decision_type = ? ORDER BY created_at DESC`) will NOT use `idx_decisions_run_id_type` optimally because its leading column is `run_id`.

Two acceptable paths, in this preference order:

1. **Prove it's fine first.** Run an `EXPLAIN QUERY PLAN` or a seeded benchmark on 1000+ rows. If the filtered path uses `idx_decisions_created_at` as a filter-scan and completes under, say, 20ms, no new index is needed for V1.
2. **Add one composite index** via `migrations/010_decisions_timeline_indexes.sql`:
   ```sql
   CREATE INDEX IF NOT EXISTS idx_decisions_type_created_at
       ON decisions(decision_type, created_at);
   ```
   Header comment MUST explain why existing indexes are insufficient.

Do NOT add indexes for reason-text search — V1 reason search is strictly client-side within the loaded page.

### 100ms Debounce Guidance

UX spec §2387-2389 is emphatic: **100ms client-side, not 300ms server-side.** Motivation: 300ms is appropriate for network search, but for a bounded local page the perceived lag is unnecessary.

Minimal hook sketch:

```ts
function useDebouncedValue<T>(value: T, delay_ms: number): T {
  const [debounced, set_debounced] = useState(value)
  useEffect(() => {
    const t = setTimeout(() => set_debounced(value), delay_ms)
    return () => clearTimeout(t)
  }, [value, delay_ms])
  return debounced
}
```

Do NOT pull in `lodash.debounce`, `use-debounce`, or similar. If a shared hook emerges as useful, colocate it in `web/src/hooks/` and add a focused test — but a local implementation inside `TimelineView.tsx` is acceptable for V1.

### J/K Keyboard Registration

Reuse the exact pattern from `BatchReview.tsx:214-275`:

```ts
useKeyboardShortcuts([
  {
    action: 'history-next',
    handler: () => { /* advance selection, bounded */ },
    key: 'j',
    prevent_default: true,
    scope: 'context',
  },
  {
    action: 'history-prev',
    handler: () => { /* retreat selection, bounded */ },
    key: 'k',
    prevent_default: true,
    scope: 'context',
  },
], { enabled: true })
```

`scope: 'context'` ensures these do not fight Production-surface `J/K` bindings (the two surfaces are never mounted together, but the engine's conflict precedence is load-bearing for future multi-surface cases). `allow_in_editable: false` is the default and MUST remain so — the reason search input must not steal `J/K` presses.

### Frontend Test Guardrails

- Use `@testing-library/react` + `vitest` with `vi.useFakeTimers()` for the 100ms debounce assertion.
- Mock the TanStack Query client with the existing `renderWithQueryClient` helper (inspect `BatchReview.test.tsx` for the exact pattern in this tree).
- Spy on `fetch` (or the `apiClient` fn directly) to assert call count stays at 1 while typing in the reason search input.
- For `J/K` suppression in editable, focus the reason input before pressing `j`; assert selection did NOT advance.
- Do NOT call `act` manually around every state update — rely on RTL's built-in batching.

### Suggested File Touches

Likely backend files:

- `internal/db/decision_store.go` (+ `_test.go`) — add `TimelineDecision`, `TimelineListOptions`, `ListTimeline`
- `internal/service/scene_service.go` (+ `_test.go`) — add `ListDecisionsTimeline`
- `internal/api/handler_scene.go` (+ `_test.go`) — add timeline handler; extend `SceneHandler` with a timeline method OR split into `handler_decisions.go` if preferred
- `internal/api/routes.go` — register `GET /api/decisions`
- `migrations/010_decisions_timeline_indexes.sql` — only if T2 proves an additional index is required

Likely frontend files:

- `web/src/components/settings/TimelineView.tsx` + `TimelineView.test.tsx` (new folder)
- `web/src/components/shells/SettingsShell.tsx` (+ `.test.tsx` if one exists; create if absent)
- `web/src/components/shared/Sidebar.tsx` — optional label change only
- `web/src/contracts/runContracts.ts` (+ `.test.ts`)
- `web/src/lib/apiClient.ts`
- `web/src/lib/queryKeys.ts`
- `web/src/index.css`
- `web/src/hooks/useDebouncedValue.ts` (optional, if shared-hook route is taken) (+ test)

### Previous Story Intelligence

- Story 8.1 ended with a deferred note (`scene_service.go os.ReadFile unbounded read`) — mirror that guardrail awareness: for this story, the SQL `LIMIT` serves the same protective role.
- Story 8.3 documented the `context_snapshot` JSON shape for `descriptor_edit`: `{ command_kind, before, after }`. Use this when parsing reasons — there is no `reason` field in that shape; render empty.
- Story 8.3 also documented the `decisions.superseded_by` semantics: original rows are preserved, reversal rows are inserted as a new decision_type = `"undo"`. The timeline renders BOTH — undo rows appear as their own entries AND the originals are visually marked as superseded.
- Story 8.5 pushed all decision mutations through the canonical `DecisionStore` write path. The timeline's job is strictly to read; it MUST NOT short-circuit that write path.
- TanStack Query v5 removed `onSuccess` / `onError` / `onSettled` from `useQuery`. If preload/sync logic is needed, use a component-scoped `useEffect`, not a query callback. This is already normalized in Stories 7.3 and 8.1.

### Git Intelligence Summary

- The current tree has Stories 8.1 (`done`), 8.2 (`done`), and 8.3 (`review`) applied locally. Stories 8.4 and 8.5 are `ready-for-dev` in `sprint-status.yaml` but have story files only — their code changes are NOT present in the tree yet.
- Recent commit `091ff5b Implement Epic 7: Production Tab — Scenario Review & Character Selection` is the most recent public anchor; Epic 8 work is still uncommitted locally.
- Timeline implementation MUST treat the current workspace (with 8.1/8.2/8.3 local diff) as the source of truth for Production-shell patterns, store signatures, and the keyboard engine surface.
- When Story 8.4 / 8.5 land between this story's story-file creation and its implementation, recheck `decision_store.go` for any new columns or methods — do not assume the file matches what was captured here.

### Testing Requirements

Backend verification:

- `go test ./...` — all existing tests must remain green
- New tests seeded with ≥1000 decision rows to prove ordering and pagination without lag
- If a migration is added in T2, verify both `up` behavior and idempotency (`IF NOT EXISTS`)

Frontend verification:

- `npm --prefix web run lint`
- `npm --prefix web run test:unit`
- Full coverage of debounce timing, J/K navigation bounds, filter interactions, and empty/loading/error state renders

### File Structure Requirements

New files:

- `internal/db/decision_store.go` additions (NOT a new file)
- `internal/api/handler_scene.go` additions OR a new `internal/api/handler_decisions.go`
- `migrations/010_decisions_timeline_indexes.sql` (conditional on T2)
- `web/src/components/settings/TimelineView.tsx` + test
- `web/src/hooks/useDebouncedValue.ts` + test (optional)

Modified files:

- `internal/api/routes.go`
- `internal/service/scene_service.go` + test
- `web/src/contracts/runContracts.ts` + test
- `web/src/lib/apiClient.ts`
- `web/src/lib/queryKeys.ts`
- `web/src/components/shells/SettingsShell.tsx` + test
- `web/src/index.css`

## Open Questions / Saved Clarifications

1. **Cross-run vs. per-run timeline scope.** This story targets a cross-run global timeline (`GET /api/decisions`) to match UX-DR19 filter columns (SCP ID + stage + type). If product ultimately prefers a per-run timeline first, the endpoint name and sidebar placement need to change; flag during implementation if that preference surfaces.
2. **Reason-extraction source of truth.** Story 8.4 (reject) stores reasons in `note`; other decision types use `context_snapshot`. Implementation may choose backend-side parsing (returns unified `reason_from_snapshot`) or client-side parsing. Pick once, document inline, and stay consistent — dev agent does NOT need to ask the user again before shipping.
3. **Sidebar label rename ("Settings" → "Settings & History").** UX spec §2273 implies this rename, but the label is currently `"Settings"` and test selectors may depend on that. If the rename breaks a Playwright smoke test (Story 6.5 territory), keep the label as `"Settings"` and mark this as deferred UX polish rather than blocking implementation.
4. **Pagination strategy.** Keyset pagination is preferred (AC-4 rule), but offset pagination is an acceptable V1 fallback if keyset complexity slows implementation. If offset is chosen, add a deferred note in `deferred-work.md` so the story is honest about the tradeoff rather than papering over it.

## Dev Agent Record

### Debug Log

- 2026-04-19: Added cross-run timeline read path in `DecisionStore`, service/handler endpoint wiring, and the conditional timeline index migration.
- 2026-04-19: Added frontend contracts, API client/query-key support, `TimelineView`, Settings shell integration, and timeline styling.
- 2026-04-19: Verified focused backend/frontend tests; broader repo checks still show unrelated pre-existing failures outside Story 8.6.

### Completion Notes

- Implemented `GET /api/decisions` with deterministic keyset pagination, optional decision-type filtering, `scp_id` join resolution, and best-effort `reason_from_snapshot` extraction.
- Built the `/settings` timeline surface with client-side filters, exact 100ms debounced reason search, `J/K` selection over filtered rows, superseded-row treatment, and inline loading/error/empty states.
- Added regression coverage for query plans, pagination, debounce timing, keyboard suppression in editable fields, retry behavior, and Settings shell rendering.
- Verification run: `go test ./internal/db ./internal/service ./internal/api`, `npm --prefix web run test:unit -- --run src/components/settings/TimelineView.test.tsx src/components/shells/SettingsShell.test.tsx src/contracts/runContracts.test.ts`, `npx eslint web/src/components/settings/TimelineView.tsx web/src/components/settings/TimelineView.test.tsx web/src/components/shells/SettingsShell.tsx web/src/components/shells/SettingsShell.test.tsx web/src/contracts/runContracts.ts web/src/contracts/runContracts.test.ts web/src/lib/apiClient.ts web/src/lib/queryKeys.ts`.
- Full-suite note: `go test ./...` currently fails in `internal/pipeline` at `TestIntegration_429Backoff_DoesNotAdvanceStage`; `npm --prefix web run lint` reports a pre-existing `react-refresh/only-export-components` issue in `web/src/components/production/NewRunContext.tsx`; `npm --prefix web run test:unit` currently fails in `src/components/production/NewRunPanel.test.tsx`. These failures are outside Story 8.6 changes.

## File List

- migrations/010_decisions_timeline_indexes.sql
- internal/db/decision_store.go
- internal/db/decision_store_test.go
- internal/db/sqlite_test.go
- internal/service/scene_service.go
- internal/service/scene_service_test.go
- internal/api/handler_scene.go
- internal/api/handler_scene_test.go
- internal/api/routes.go
- web/src/contracts/runContracts.ts
- web/src/contracts/runContracts.test.ts
- web/src/lib/apiClient.ts
- web/src/lib/queryKeys.ts
- web/src/components/settings/TimelineView.tsx
- web/src/components/settings/TimelineView.test.tsx
- web/src/components/shells/SettingsShell.tsx
- web/src/components/shells/SettingsShell.test.tsx
- web/src/index.css

### Review Findings

- [x] [Review][Patch] `before_created_at` cursor param forwarded to SQL without RFC3339 format validation [internal/api/handler_scene.go:214] — **fixed 2026-04-19**
- [x] [Review][Defer] RetryExhausted `>` vs `>=` inconsistency [internal/service/scene_service.go] — deferred, pre-existing (Story 8.4 scope)
- [x] [Review][Defer] BatchApprove undo `aggregate_command_id` cross-run risk [internal/db/decision_store.go] — deferred, pre-existing (Story 8.5 scope)
- [x] [Review][Defer] CountRegenAttempts includes superseded rejects [internal/db/decision_store.go] — deferred, pre-existing (Story 8.4 scope)

## Change Log

- 2026-04-19: Implemented Story 8.6 decisions history timeline backend/frontend flow and added focused verification coverage.
- 2026-04-19: Code review pass — fixed `before_created_at` RFC3339 validation in timeline handler; deferred 3 pre-existing findings to deferred-work.md.
