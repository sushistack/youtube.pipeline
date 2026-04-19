# Story 7.3: Character Selection Interface (Candidate Grid)

Status: done

## Story

As an operator,
I want to select a character reference from 10 generated candidates and confirm a Vision Descriptor,
so that I can establish a consistent visual identity before image generation proceeds.

## Prerequisites

**Hard dependencies:** Stories 7.1 and 7.2 already established the Production shell structure, TanStack Query + Zod patterns, apiClient/queryKeys helpers, keyboard shortcut infrastructure, and the `ScenarioInspector` precedent. This story must extend those foundations — do NOT replace or duplicate them.

**Backend state entering this story:**
- `GET /api/runs/{id}/characters?query=<q>` searches DDG cache + stores `character_query_key` on the run.
- `POST /api/runs/{id}/characters/pick` accepts `{candidate_id}` and advances stage from `character_pick`.
- `runs.character_query_key` and `runs.selected_character_id` columns exist (migration 008).
- `FrozenDescriptor` lives in the scenario artifact (`scenario.json` → `visual_breakdown.frozen_descriptor`), loaded by `image_track.go:147`. No DB column for it yet.
- `image_track.go` uses `state.VisualBreakdown.FrozenDescriptor` verbatim in every image prompt prefix.

**Frontend state entering this story:**
- `ProductionShell.tsx` renders `<ScenarioInspector>` for `scenario_review/waiting` and `<ProductionShortcutPanel>` otherwise — the `character_pick/waiting` case is currently the fallback.
- `apiClient.ts`, `queryKeys.ts`, `runContracts.ts`, and `useRunStatus.ts` are live with scene and status patterns.
- `useKeyboardShortcuts.tsx` suppresses global shortcuts inside editable controls — reuse for the grid and descriptor textarea.

## Acceptance Criteria

1. **AC-CHARACTER-GRID-DISPLAY:** when the selected run is at `character_pick/waiting`, the Production surface shows a candidate grid of up to 10 images.
   - Required outcome: 2×5 grid layout; each cell shows the candidate image and a number label (1 through 9 and 0); cell images begin preloading immediately on data arrival; selected cell shows an accent border + 1.02 scale transform.
   - Rules: CharacterGrid is implemented inline within the `CharacterPick` component file, not as a separate top-level component file (per UX-DR17).
   - Tests: component test verifies 10 cells render with correct labels; image preloading fires before first paint; selected state styling test.

2. **AC-KEYBOARD-GRID-SELECTION:** the operator can select a candidate without touching the mouse.
   - Required outcome: pressing keys `1`–`9` selects candidate at that position; pressing `0` selects candidate 10; visual selection highlight appears immediately; pressing `Esc` navigates back to the search input.
   - Rules: keyboard handlers live on the grid container (not `window`), so global shortcut suppression in `useKeyboardShortcuts` is not needed here — the grid is never inside a textarea.
   - Tests: keyboard interaction test verifies 1-9/0 updates the selected candidate; Esc test verifies navigation to search state.

3. **AC-CHARACTER-PICK-CONFIRM:** pressing `Enter` after a candidate is selected persists the pick and the frozen descriptor atomically.
   - Required outcome: `POST /api/runs/{id}/characters/pick` is called with `{candidate_id, frozen_descriptor}`; on success the run advances from `character_pick`; the Production status polling reflects the new stage.
   - Rules: the existing `POST /api/runs/{id}/characters/pick` endpoint is extended to accept an optional `frozen_descriptor` field — do NOT add a separate descriptor endpoint for the pick action. The pick and frozen descriptor save must be a single atomic backend write.
   - Tests: integration test verifies pick + descriptor persist together; contract test verifies the extended pick payload schema.

4. **AC-VISION-DESCRIPTOR-PREFILL:** after candidate selection, a descriptor textarea appears below the grid pre-filled per UX-DR62.
   - Required outcome: if a prior run exists for the same `scp_id` with a saved `frozen_descriptor`, the prior value is shown; otherwise the auto-extracted `FrozenDescriptor` from the current run's `scenario.json` is shown; `GET /api/runs/{id}/characters/descriptor` returns `{auto: string, prior: string | null}`.
   - Rules: "prior" = most recent other completed run for same SCP ID with a non-null `frozen_descriptor` column; "auto" = read from `run.scenario_path` → parse scenario JSON → return `visual_breakdown.frozen_descriptor`.
   - Tests: service unit test verifies prior-run lookup prefers most recent; handler test verifies 404 vs. valid descriptor response shapes.

5. **AC-DESCRIPTOR-EDIT-AND-REVERT:** the operator can edit the descriptor inline.
   - Required outcome: `Tab` on the descriptor surface enters edit mode (textarea focused); blur saves the draft to local state (no API call on blur — the save happens at confirm/Enter); `Ctrl+Z` while the textarea is focused reverts the draft to the pre-fill value; the textarea displays the current draft at all times.
   - Rules: mirror the `InlineNarrationEditor` Ctrl+Z revert pattern (revert_to ref initialized from prefill, reset on new prefill load); do NOT use the blur to trigger the pick API — blur is local state only.
   - Tests: keyboard test verifies Tab activates textarea; Ctrl+Z test verifies revert to prefill; blur test verifies local draft update (no mutation call).

6. **AC-FROZEN-DESCRIPTOR-PROPAGATION:** the saved frozen descriptor overrides the artifact value in image generation.
   - Required outcome: `image_track.go` prefers `run.FrozenDescriptor` (DB column) over `state.VisualBreakdown.FrozenDescriptor` (JSON artifact) when the column is non-null and non-empty.
   - Rules: this requires `image_track.go` to receive the run record (or just the frozen_descriptor field) — add a new `ImageTrackRequest` field `FrozenDescriptorOverride *string` populated from the run before invoking the track; do NOT change the artifact JSON.
   - Tests: unit test verifies override behavior; existing `TestImagePromptComposer_PrefixesFrozenDescriptorVerbatim` must still pass.

7. **AC-RESEARCHABLE-CANDIDATES:** the search form allows the operator to initiate or re-initiate a character search.
   - Required outcome: a search input with a submit action shows at the start of the `character_pick` phase; on submit, `GET /api/runs/{id}/characters?query=<q>` is called and results populate the grid; if `run.character_query_key` is already set (from a prior search in this session), the GET call is made automatically on mount without requiring the operator to re-type the query.
   - Rules: extend `GET /api/runs/{id}/characters` handler to allow empty `?query` when the run's `character_query_key` is already set — if so, load from cache directly (no external DDG call). This supports restoring the grid on page reload.
   - Tests: handler test verifies empty-query fall-through to cache; frontend hook test verifies auto-load when `character_query_key` is set.

## Tasks / Subtasks

- [x] **T1: Add migration 009 and update domain/DB layer** (AC: #3, #4, #6)
  - [x] Create `migrations/009_frozen_descriptor.sql`: `ALTER TABLE runs ADD COLUMN frozen_descriptor TEXT;`
  - [x] Add `FrozenDescriptor *string` field to `domain.Run` struct in `internal/domain/types.go` (JSON tag: `frozen_descriptor,omitempty`).
  - [x] Update `run_store.go` scan to include `frozen_descriptor` in SELECT and scan target.
  - [x] Add `run_store.go::LatestFrozenDescriptorBySCPID(ctx, scpID, excludeRunID string) (*string, error)` — queries `SELECT frozen_descriptor FROM runs WHERE scp_id=? AND id!=? AND frozen_descriptor IS NOT NULL ORDER BY updated_at DESC LIMIT 1`.
  - [x] Add `run_store.go::SetFrozenDescriptor(ctx, id, descriptor string) error`.
  - [x] Extend `ApplyCharacterPick` (used by `CharacterService.Pick`) to also write `frozen_descriptor` if provided: change signature to include `frozenDescriptor *string`; if non-nil write it in the same UPDATE statement.
  - [x] Update `sqlite_test.go` schema expectations to include `frozen_descriptor TEXT` column.

- [x] **T2: Extend character service and API handlers** (AC: #3, #4, #7)
  - [x] Extend `CharacterService.Pick(ctx, runID, candidateID, frozenDescriptor string) (*domain.Run, error)` to accept and persist `frozenDescriptor` (pass to `ApplyCharacterPick`).
  - [x] Add `CharacterService.GetDescriptorPrefill(ctx, runID string) (auto string, prior *string, err error)`:
    - load run → read `scenario_path` → parse scenario JSON → extract `visual_breakdown.frozen_descriptor` as `auto`
    - call `LatestFrozenDescriptorBySCPID(ctx, run.SCPID, runID)` → assign to `prior`
    - return both
  - [x] Add `CharacterService.GetCandidatesByRun(ctx, runID string) (*domain.CharacterGroup, error)`:
    - loads run → if `character_query_key` is set, calls `cache.Get(ctx, queryKey)`; else returns `ErrNotFound`.
  - [x] Extend `pickCharacterRequest` in `handler_character.go` to include `FrozenDescriptor string`.
  - [x] Update `CharacterHandler.Pick` to pass `req.FrozenDescriptor` to `svc.Pick`.
  - [x] Modify `CharacterHandler.Search` to allow empty `?query`: if `query == ""` call `svc.GetCandidatesByRun(ctx, runID)`; if notFound return 404 with `NOT_FOUND` code.
  - [x] Add `CharacterHandler.Descriptor` method for `GET /api/runs/{id}/characters/descriptor`: calls `GetDescriptorPrefill`, returns `{"version":1,"data":{"auto":"...","prior":"..." or null}}`.
  - [x] Register new route in `routes.go`: `api.HandleFunc("GET /api/runs/{id}/characters/descriptor", deps.Character.Descriptor)`.
  - [x] Add contract fixture files: `testdata/contracts/run.character.candidates.response.json`, `testdata/contracts/run.character.descriptor.response.json`.

- [x] **T3: Update image_track.go to prefer DB frozen descriptor** (AC: #6)
  - [x] In `ImageTrackRequest`, add `FrozenDescriptorOverride *string` field.
  - [x] In `ImageTrack.Run()`, after `frozen := state.VisualBreakdown.FrozenDescriptor`, add: if `req.FrozenDescriptorOverride != nil && strings.TrimSpace(*req.FrozenDescriptorOverride) != ""` → `frozen = *req.FrozenDescriptorOverride`.
  - [x] Update the pipeline runner code that calls `ImageTrack` to load `run.FrozenDescriptor` and pass it as `FrozenDescriptorOverride`.
  - [x] Add/update unit tests for `ImageTrack.Run()` with and without the override.

- [x] **T4: Frontend contracts, API client helpers, query keys** (AC: #3, #4, #7)
  - [x] Add to `web/src/contracts/runContracts.ts`:
    - `characterCandidateSchema`, `characterGroupSchema`, `characterGroupResponseSchema`
    - `descriptorPrefillSchema` (`{auto: string, prior: string | null}`), `descriptorPrefillResponseSchema`
    - Extend `runSummarySchema` to include `frozen_descriptor: z.string().nullable().optional()` and `character_query_key: z.string().nullable().optional()`
  - [x] Add to `web/src/lib/apiClient.ts`:
    - `fetchCharacterCandidates(run_id: string)` → GET `/runs/{id}/characters` (no query param; returns cached group or throws 404)
    - `searchCharacterCandidates(run_id: string, query: string)` → GET `/runs/{id}/characters?query=...`
    - `fetchDescriptorPrefill(run_id: string)` → GET `/runs/{id}/characters/descriptor`
    - `pickCharacterWithDescriptor(run_id: string, candidate_id: string, frozen_descriptor: string)` → POST `/runs/{id}/characters/pick`
  - [x] Add to `web/src/lib/queryKeys.ts`:
    - `characters: (run_id: string) => ['runs', 'characters', run_id]`
    - `descriptor: (run_id: string) => ['runs', 'descriptor', run_id]`
  - [x] Add contract fixture tests in `web/src/contracts/runContracts.test.ts` for the new schemas.

- [x] **T5: Build CharacterPick and inline CharacterGrid components** (AC: #1, #2, #3, #4, #5, #7)
  - [x] Create `web/src/components/production/CharacterPick.tsx`:
    - Phase state: `'search' | 'grid' | 'descriptor'`
    - Search phase: input field pre-populated from `run.character_query_key` label; on submit calls `searchCharacterCandidates` and transitions to `grid`
    - On mount: if `run.character_query_key` is set, auto-fetch via `fetchCharacterCandidates` and transition to `grid` immediately
    - Grid phase: renders inline `CharacterGrid` inside this file; keyboard 1-9/0 sets `selectedCandidateId`; Esc → `'search'`; Enter → if candidate selected, fetch descriptor prefill then transition to `'descriptor'`
    - Descriptor phase: renders `VisionDescriptorEditor`; Enter (outside textarea) → calls `pickCharacterWithDescriptor` mutation
    - Image preloading: on candidates data arrival, `candidates.forEach(c => { const img = new Image(); img.src = c.image_url })` inside the component (not a useEffect anti-pattern — triggered in data callback)
    - Mutation: `usePickCharacter` = `useMutation({ mutationFn: ..., onSuccess: () => queryClient.invalidateQueries(queryKeys.runs.all) })`
    - Elevated density: apply `--content-expand` CSS custom property on container (per UX affordance density spec)
  - [x] Create `web/src/components/production/VisionDescriptorEditor.tsx`:
    - Props: `{ prefill: string, onDescriptorChange: (v: string) => void, onConfirm: () => void }`
    - State: `{ draft: string, editMode: boolean }`; `revert_to` = prefill (ref, same pattern as `InlineNarrationEditor`)
    - Read mode: paragraph showing `draft` + "Tab to edit" hint; on `Tab` keydown → `setEditMode(true)`, focus textarea
    - Edit mode: textarea, `value=draft`, `onChange` updates draft, `onBlur` → `setEditMode(false)` and calls `onDescriptorChange(draft.trim())`
    - `Ctrl+Z` while textarea focused: `setDraft(revert_to)` (ref value)
    - `Enter` outside textarea: calls `onConfirm()`
    - When `prefill` prop changes (new candidate selected): reset `draft` and `revert_to` to new prefill value

- [x] **T6: Wire CharacterPick into ProductionShell** (AC: #1)
  - [x] Update `ProductionShell.tsx`: in the conditional render block, add `else if (current_run.stage === 'character_pick' && current_run.status === 'waiting')` → `<CharacterPick run={current_run} />`.
  - [x] Pass `run` (full detail) to `CharacterPick` so it has `character_query_key`, `scp_id`, and `id`.

- [x] **T7: Add CSS for CharacterGrid** (AC: #1)
  - [x] Add to `web/src/index.css`:
    - `.character-grid`: CSS grid, 5 columns × 2 rows, gap, width fills content area
    - `.character-grid__cell`: relative, cursor pointer, border 2px transparent, border-radius
    - `.character-grid__cell--selected`: border-color accent, scale(1.02) transform, transition 150-250ms
    - `.character-grid__label`: absolute bottom-left, monospace number badge
    - `.character-pick`: container with `--content-expand` CSS property for elevated density
    - `.vision-descriptor`: container for descriptor textarea + hint

- [x] **T8: Add focused test coverage** (AC: all)
  - [x] Backend handler tests in `handler_character_test.go`:
    - `TestCharacterHandler_Search_EmptyQueryFallsBackToCache` — verifies empty query loads cached group when `character_query_key` is set
    - `TestCharacterHandler_Search_EmptyQueryReturns404WhenNoQueryKey` — verifies 404 when no query key set
    - `TestCharacterHandler_Pick_PersistsFrozenDescriptor` — verifies `frozen_descriptor` field is saved
    - `TestCharacterHandler_Descriptor_ReturnsAutoAndPrior` — verifies response shape with/without prior run
  - [x] Service unit tests in `character_service_test.go`:
    - `TestCharacterService_GetDescriptorPrefill_PrefersPriorRunWhenAvailable`
    - `TestCharacterService_GetDescriptorPrefill_FallsBackToAutoWhenNoPrior`
    - `TestCharacterService_Pick_SavesFrozenDescriptor`
  - [x] Frontend RTL tests:
    - `web/src/components/production/CharacterPick.test.tsx`: auto-load on `character_query_key` present; search submit; grid renders; 1-9/0 keyboard selection; Esc back to search; Enter advances to descriptor; confirm calls pick mutation
    - `web/src/components/production/VisionDescriptorEditor.test.tsx`: Tab activates textarea; Ctrl+Z reverts; blur saves draft; prefill reset on prop change
    - `web/src/components/production/CharacterGrid.test.tsx` (if extracted as named inner component or test inline): 10 cells; number labels; image src attributes

## Dev Notes

### Critical Architecture Decision: Frozen Descriptor Persistence

The `FrozenDescriptor` currently lives only in the scenario artifact JSON (`scenario.json::visual_breakdown.frozen_descriptor`). The operator-edited version **must** be written to the DB (`runs.frozen_descriptor`) because:
1. The artifact is read-only output from Phase A — do NOT mutate it.
2. The DB field is the authoritative override for image generation, checked before the artifact value.
3. The "prior run pre-fill" (UX-DR62) queries the DB, not artifact files.

### image_track.go Override Pattern

The correct integration point is `ImageTrackRequest.FrozenDescriptorOverride`. The caller (pipeline runner) loads the run record at image track invocation time and passes the DB value. `image_track.go` applies the override in one conditional after it reads the artifact value — keeps the logic local and testable.

**Do not** change how the scenario artifact is loaded or written. The override is additive.

### Backend: atomic pick + descriptor

Extend the existing `POST /api/runs/{id}/characters/pick` request body to include `frozen_descriptor string`. Do NOT add a second endpoint for descriptor-only save. The atomicity requirement (AC #3) means both fields must be written in one DB UPDATE — extend `ApplyCharacterPick` accordingly.

The existing `ApplyCharacterPick` DB call is:
```go
SET character_query_key = ?, selected_character_id = ?, stage = ?, status = ?, updated_at = datetime('now')
```
Extend to:
```go
SET character_query_key = ?, selected_character_id = ?, frozen_descriptor = ?, stage = ?, status = ?, updated_at = datetime('now')
```

### CharacterHandler.Search: empty-query fallback

Modify the early-return guard from "query == '' → error" to "query == '' → try GetCandidatesByRun":
```go
if query == "" {
    group, err := h.svc.GetCandidatesByRun(r.Context(), r.PathValue("id"))
    if err != nil {
        writeDomainError(w, err)
        return
    }
    writeJSON(w, http.StatusOK, group)
    return
}
// ... existing search path
```

### Frontend: Image Preloading

Trigger preloading in the query `onSuccess` callback (or after `data` arrives in the hook), not in a `useEffect` keyed on `data`. The pattern:
```ts
const candidates_query = useQuery({
  queryKey: queryKeys.runs.characters(run.id),
  queryFn: () => fetchCharacterCandidates(run.id),
  onSuccess: (candidates) => {
    candidates.forEach((c) => {
      const img = new Image()
      img.src = c.preview_url ?? c.image_url
    })
  },
})
```
Note: TanStack Query v5 uses `select` and `gcTime` — `onSuccess` in the options was removed. Use a `useEffect` watching `data` instead, but guard against re-fire: `if (data && !preloaded_ref.current) { preload(); preloaded_ref.current = true }`.

### Frontend: Descriptor Pre-fill Priority

Fetch via `GET /api/runs/{id}/characters/descriptor`. Priority:
1. If `prior != null` → prefill = `prior` (UX-DR62 explicit)
2. Else → prefill = `auto`

This evaluation happens in the `CharacterPick` component when transitioning from `grid` → `descriptor` phase (after candidate selection).

### Frontend: No Global Store Widening

Character pick state (phase, selected candidate ID, descriptor draft) should live in `CharacterPick` local component state — not in `useUIStore`. The state is transient per character-pick session and does not need cross-component visibility.

### Frontend: VisionDescriptorEditor and Ctrl+Z

`revert_to` must be a `ref`, not state, so that the Ctrl+Z handler always has the latest prefill without stale closure issues — same pattern proven in `InlineNarrationEditor` (Story 7.2 review finding #3).

When `prefill` prop changes (operator picks a different candidate), reset both `draft` state and `revert_to` ref:
```ts
useEffect(() => {
  setDraft(prefill)
  revert_to_ref.current = prefill
}, [prefill])
```

### Architecture Compliance

- Server state: TanStack Query v5 (same as Stories 7.1/7.2).
- Contracts: Zod schemas in `runContracts.ts`, parsed before UI consumption.
- Backend layering: handler → service → store (same as CharacterService existing pattern).
- Keyboard: global shortcut suppression in `useKeyboardShortcuts` already handles editable controls — the descriptor textarea gets suppression automatically; no extra wiring needed.
- API envelope: `{"version": 1, "data": ...}` for all responses.

### Library/Framework Requirements

- React 19, TanStack Query v5, React Router v7, Zustand v5, Zod v4, Vitest 4 — no additions.
- No new keyboard library — reuse existing `useKeyboardShortcuts` infrastructure.
- No new image library — native `new Image()` for preload.
- Go: no new dependencies — scenario JSON parsed with `encoding/json`.

### File Structure

**New backend files:**
- `migrations/009_frozen_descriptor.sql`
- No new handler/service files — extend existing `handler_character.go`, `character_service.go`, `run_store.go`

**Modified backend files:**
- `internal/domain/types.go` — `Run.FrozenDescriptor *string`
- `internal/db/run_store.go` — scan + new methods + extended ApplyCharacterPick
- `internal/db/sqlite_test.go` — schema assertion update
- `internal/service/character_service.go` — extended Pick + 2 new methods
- `internal/api/handler_character.go` — extended Pick + Descriptor handler + empty-query Search
- `internal/api/routes.go` — new descriptor route
- `internal/pipeline/image_track.go` — FrozenDescriptorOverride field + override logic
- `cmd/pipeline/serve.go` or pipeline runner — pass override to ImageTrack

**New frontend files:**
- `web/src/components/production/CharacterPick.tsx`
- `web/src/components/production/VisionDescriptorEditor.tsx`
- `web/src/components/production/CharacterPick.test.tsx`
- `web/src/components/production/VisionDescriptorEditor.test.tsx`

**Modified frontend files:**
- `web/src/contracts/runContracts.ts` — new schemas + runSummary extension
- `web/src/lib/apiClient.ts` — new API functions
- `web/src/lib/queryKeys.ts` — characters + descriptor keys
- `web/src/components/shells/ProductionShell.tsx` — character_pick/waiting → `<CharacterPick>`
- `web/src/contracts/runContracts.test.ts` — new schema fixture tests
- `web/src/index.css` — character grid + vision descriptor CSS

**New contract fixtures:**
- `testdata/contracts/run.character.candidates.response.json`
- `testdata/contracts/run.character.descriptor.response.json`

### Testing Requirements

**Backend (Go):**
- Handler tests: search empty-query fallback, pick with frozen_descriptor, descriptor endpoint (with and without prior run)
- Service tests: `GetDescriptorPrefill` prior preference, `Pick` with descriptor persistence, `GetCandidatesByRun` cache-hit path
- Store tests: `LatestFrozenDescriptorBySCPID` ordering + excludeRunID correctness, `ApplyCharacterPick` with frozen_descriptor
- image_track tests: override takes precedence over artifact value; nil override falls through to artifact value

**Frontend (Vitest + RTL):**
- `CharacterPick.test.tsx`: ~8-10 tests covering all phases and keyboard paths
- `VisionDescriptorEditor.test.tsx`: ~5 tests (Tab, Ctrl+Z, blur, prefill reset, confirm callback)
- Contract tests: schema parse success + reject for new shapes

**Verification checklist:**
- `go test ./...` (all packages)
- `npm run lint`
- `npm run test:unit`
- No TypeScript errors (`npm run build` or `tsc --noEmit`)

### Previous Story Intelligence

- Story 7.2 review finding #3 (stale-closure double-save) → use `is_saving_ref` guard pattern for any mutation that blur might trigger; here blur is local-only so this risk is lower, but the Ctrl+Z revert_to ref is directly analogous.
- Story 7.2 review finding #4 (draft/revert mismatch on whitespace) → always `.trim()` when comparing draft to prefill for skip-save decisions.
- Story 7.2 review finding #1 (Tab key not wired) → wire Tab explicitly in the descriptor read-mode `onKeyDown` before assuming native behavior.
- Story 7.1 + 7.2 established `renderWithProviders` as the standard test harness — use it.
- `ProductionShell` conditionals must remain clean: `scenario_review/waiting` → ScenarioInspector, `character_pick/waiting` → CharacterPick, else → ProductionShortcutPanel. Do NOT merge branches or short-circuit.

### UX Design References

- UX-DR17: CharacterGrid — 2×5, keyboard 1-9/0, Enter confirm, Esc re-search, 150-250ms transition on selection.
- UX-DR41: Character pick surface — full-content grid + Vision Descriptor pre-fill below; elevated affordance density.
- UX-DR62: Vision Descriptor pre-fill from prior run's descriptor.
- UX-DR26: Ctrl+Z reverts to pre-fill (same pattern as scenario editor).
- Motion budget: selection confirmation uses upper bound 250ms; scale + border-color transition.
- Elevated density (`--content-expand` CSS var): applies to `character_pick` and `scenario edit` phases.

### Review Findings

> **Code review (2026-04-19)** — Three parallel adversarial layers (Blind Hunter / Edge Case Hunter / Acceptance Auditor). User's flagged concerns: prior-run prefill same-scp scoping (AC #4), frozen-descriptor immediate propagation to segments (AC #6), Ctrl+Z scoping to edits only (AC #5).

**Critical (AC-violating):**

- [x] [Review][Patch] AC #6 propagation not wired — pipeline runner never populates `PhaseBRequest.FrozenDescriptorOverride` from `run.FrozenDescriptor` [cmd/pipeline/serve.go:82-86,138, internal/pipeline/phase_b.go]
- [x] [Review][Patch] AC #4 prior-run SQL missing `status = 'completed'` filter — any mid-flight run with a frozen_descriptor shadows the true prior [internal/db/run_store.go:387-393]
- [x] [Review][Patch] AC #5 Ctrl+Z does not sync to parent ref; relies on blur-before-Enter ordering [web/src/components/production/VisionDescriptorEditor.tsx:55-63]

**High:**

- [x] [Review][Patch] `VisionDescriptorEditor` render-time setState clobbers active edits when prefill prop refetches — no `!edit_mode` guard [web/src/components/production/VisionDescriptorEditor.tsx:28-32]
- [x] [Review][Patch] `CharacterPick` state leaks across run switch — missing `key={current_run.id}` in ProductionShell render [web/src/components/shells/ProductionShell.tsx:144-145]
- [x] [Review][Patch] `query_input` starts empty despite `defaultValue` — first-Enter submit silently no-ops [web/src/components/production/CharacterPick.tsx:119,237]
- [x] [Review][Patch] No `isPending` guard on descriptor confirm + search submit — double-Enter / double-submit race [web/src/components/production/CharacterPick.tsx:183-196,206-214]
- [x] [Review][Patch] `handle_grid_escape` does not clear `selected_candidate_id` — stale selection leaks on re-search [web/src/components/production/CharacterPick.tsx:198-200]
- [x] [Review][Patch] `pick_mutation.onSuccess` invalidates `queryKeys.runs.all`, refetching characters/descriptor after stage advance [web/src/components/production/CharacterPick.tsx:158-161]
- [x] [Review][Patch] Descriptor handler: `ErrValidation` 400 when `scenario_path` is nil; dead `errors.Is(err, ErrNotFound)` branch [internal/service/character_service.go:167-168, internal/api/handler_character.go:79-84]
- [x] [Review][Patch] `GetDescriptorPrefill` silently returns `auto=""` when `visual_breakdown` missing — should be `ErrNotFound` [internal/service/character_service.go:177-188]
- [x] [Review][Patch] Image preload ref guard doesn't reset on new candidate set — re-search returns no preload [web/src/components/production/CharacterPick.tsx:127,166-173]
- [x] [Review][Patch] Cache-fallback 404 strands UI at `phase='grid'` with no recovery path — must auto-fall back to `search` [web/src/components/production/CharacterPick.tsx]

**Deferred:**

- [x] [Review][Defer] `LatestFrozenDescriptorBySCPID` `id DESC` tiebreaker unstable for random-UUID IDs — deferred, low impact (trigger-auto-bumped `updated_at` breaks ties in practice)
- [x] [Review][Defer] `ApplyCharacterPick` COALESCE with non-nil empty string wipes column — deferred, not reachable via current service trim-empty path
- [x] [Review][Defer] `SetFrozenDescriptor` is dead code — deferred, cleanup sweep
- [x] [Review][Defer] `scenarioPath` not validated for traversal — deferred, internal trust model
- [x] [Review][Defer] `runSummary.frozen_descriptor` (nullable+optional) vs `descriptorPrefill.prior` (nullable) schema inconsistency — deferred, both accept null
- [x] [Review][Defer] Image track shot-embedded frozen text still reflects artifact — deferred, out of Story 7.3 scope (Phase A composition concern)
- [x] [Review][Defer] `handle_blur` silently trims whitespace (draft retains untrimmed) — deferred, minor UX
- [x] [Review][Defer] `pickCharacterRequest.FrozenDescriptor` should be `*string` for omit-vs-empty distinction — deferred, minor
- [x] [Review][Defer] No `frozen_descriptor` length cap at handler — deferred, `maxRequestBodyBytes` bounds overall
- [x] [Review][Defer] `Descriptor` handler does not verify run stage — deferred, low-risk info leak
- [x] [Review][Defer] Hint text says "Press 1–9 or 0" regardless of candidate count — deferred, minor UX
- [x] [Review][Defer] Ctrl+Z `revert_to` captured on prefill change, not edit-mode activation — deferred, comment-vs-spec mismatch; spec AC #5 text ("revert to pre-fill value") matches current behavior
- [x] [Review][Defer] ProductionShell `runs` re-sort + flicker during run switch — deferred, out of Story 7.3 scope (Story 7.1 dashboard work)
- [x] [Review][Defer] Migration 009 lacks `IF NOT EXISTS` / version PRAGMA — deferred, consistent with existing migration conventions
- [x] [Review][Defer] `ProductionShell.tsx` / `scene_service.go` / `handler_scene.go` / scene fixtures / runSummarySchema telemetry-field additions leaked into 7.3 diff — deferred, handled at commit-scope via uncommitted-file strategy (per user rule)

**Dismissed as noise:** `ApplyCharacterPick` `updated_at` not bumped (Migration 002 AFTER UPDATE trigger auto-bumps); Listbox Enter double-fire (preventDefault on keydown suppresses native button click in all major browsers); preload missing URLs (Zod guards at ingestion).

## Story Completion Status

- Story file created: `_bmad-output/implementation-artifacts/7-3-character-selection-interface.md`
- Story status set to `review`
- Sprint status updated to `review`
- Completion note: Ultimate context engine analysis completed — comprehensive developer guide created

## Dev Agent Record

### Implementation Plan

- **T1 — Migration 009 + DB layer:** added `migrations/009_frozen_descriptor.sql`, the `FrozenDescriptor *string` field on `domain.Run`, SELECT + scan coverage in `run_store.go`, and three new/extended store methods (`ApplyCharacterPick` with `frozenDescriptor *string`, `SetFrozenDescriptor`, `LatestFrozenDescriptorBySCPID`). Migration idempotency assertions in `sqlite_test.go` bumped to user_version 9.
- **T2 — Service + handlers:** extended `CharacterService.Pick` to persist the edited descriptor atomically with the stage advance; added `GetCandidatesByRun` (empty-query cache restore) and `GetDescriptorPrefill` (scenario.json artifact + prior-run lookup). `CharacterHandler.Search` now falls back to cache when `?query=` is absent; `CharacterHandler.Descriptor` implements the new `GET /characters/descriptor` endpoint. `runResponse` surfaces `character_query_key`, `selected_character_id`, and `frozen_descriptor` so the frontend run summary stays a single source of truth.
- **T3 — image_track override:** added `PhaseBRequest.FrozenDescriptorOverride *string`. `runImageTrack` prefers the override bytes when non-nil and non-blank, otherwise falls back to the artifact's `VisualBreakdown.FrozenDescriptor`. Tests cover all three paths (override set, override nil, override blank).
- **T4 — Frontend contracts, API client, queryKeys:** added `characterCandidateSchema`, `characterGroupSchema`, `characterGroupResponseSchema`, `descriptorPrefillSchema`, `descriptorPrefillResponseSchema`; extended `runSummarySchema` with `character_query_key`/`selected_character_id`/`frozen_descriptor` (nullable, optional). `apiClient.ts` gains `fetchCharacterCandidates`, `searchCharacterCandidates`, `fetchDescriptorPrefill`, `pickCharacterWithDescriptor`. `queryKeys.runs` gains `characters` and `descriptor`.
- **T5 — CharacterPick + VisionDescriptorEditor:** new component with three phases (`search | grid | descriptor`). Grid handlers are scoped to the container (not `window`) per the story rule. Image preloading uses a ref guard in a `useEffect` keyed on `data`, matching TanStack Query v5 guidance. Descriptor editor uses the compare-prev-prefill pattern for prop→state sync (lint-clean in React 19).
- **T6 — ProductionShell wiring:** conditional branches remain three-way clean (`scenario_review/waiting → ScenarioInspector`, `character_pick/waiting → CharacterPick`, else → `ProductionShortcutPanel`).
- **T7 — CSS:** 2×5 grid with scale(1.02) + border transition under 180ms; `--content-expand` toggle on `.character-pick` for elevated density.
- **T8 — Tests:** backend store (4 new tests), service (4 new tests), handler (4 new tests), image_track (3 new tests); frontend schema fixtures (2 new tests) + `VisionDescriptorEditor.test.tsx` (6 tests) + `CharacterPick.test.tsx` (6 tests).

### Completion Notes

- `go test ./...` — **all packages pass** (no regressions).
- `npm run lint` — **clean** (0 errors) after resolving two `react-hooks/set-state-in-effect` flags by switching the prefill-sync in `VisionDescriptorEditor` from `useEffect` to the compare-previous-prefill pattern, and replacing `descriptor_draft` state in `CharacterPick` with a ref-driven `current_descriptor_ref` updated from the editor's `onDescriptorChange` callback.
- `npm run test:unit` — **99/99 tests pass** across 19 files (includes all new tests for this story).
- `npx tsc --noEmit` on Story 7.3 files is clean. `npm run build` surfaces pre-existing `tsc -b` errors in untracked Story 7.1/7.2 files (`ScenarioInspector.tsx`, `InlineNarrationEditor.test.tsx`, `ProductionShell.test.tsx`, `useRunStatus.test.tsx`). These predate this story and touching them would violate the commit-scope rule; flagging for the 7.1/7.2 retrospective.
- `PhaseBRunner` is not yet wired into the engine in `cmd/pipeline/serve.go` (the stub predates this story). When that plumbing lands, the caller must set `PhaseBRequest.FrozenDescriptorOverride` from `run.FrozenDescriptor`. This is noted at the field's doc comment in `internal/pipeline/phase_b.go`.
- `ApplyCharacterPick`'s signature changed (added `frozenDescriptor *string` before `stage`). Callers updated: `CharacterService.Pick` and its `CharacterRunStore` interface. No production production call sites outside the service.

### File List

**Migrations:**
- `migrations/009_frozen_descriptor.sql` (new)

**Backend (Go):**
- `internal/domain/types.go`
- `internal/db/run_store.go`
- `internal/db/run_store_test.go`
- `internal/db/sqlite_test.go`
- `internal/service/character_service.go`
- `internal/service/character_service_test.go`
- `internal/api/handler_character.go`
- `internal/api/handler_character_test.go`
- `internal/api/handler_run.go`
- `internal/api/routes.go`
- `internal/pipeline/phase_b.go`
- `internal/pipeline/image_track.go`
- `internal/pipeline/image_track_test.go`

**Contracts fixtures:**
- `testdata/contracts/run.character.candidates.response.json` (new)
- `testdata/contracts/run.character.descriptor.response.json` (new)

**Frontend (TypeScript):**
- `web/src/contracts/runContracts.ts`
- `web/src/contracts/runContracts.test.ts`
- `web/src/lib/apiClient.ts`
- `web/src/lib/queryKeys.ts`
- `web/src/components/production/CharacterPick.tsx` (new)
- `web/src/components/production/CharacterPick.test.tsx` (new)
- `web/src/components/production/VisionDescriptorEditor.tsx` (new)
- `web/src/components/production/VisionDescriptorEditor.test.tsx` (new)
- `web/src/components/shells/ProductionShell.tsx`
- `web/src/index.css`

### Change Log

- 2026-04-19 (code review): applied 13 patch findings from triage — `LatestFrozenDescriptorBySCPID` now filters `status='completed'` (AC-4); `PhaseBRunner` accepts an optional `PhaseBRunLoader` so `prepareRequest` auto-populates `FrozenDescriptorOverride` from `runs.frozen_descriptor` (AC-6 end-to-end wiring at the Phase B entry point); `VisionDescriptorEditor` Ctrl+Z now syncs revert_to to the parent via `onDescriptorChange` and render-time prefill sync skips when the operator is actively editing (AC-5). `CharacterPick` state scoped per-run via `key={current_run.id}`, search input is controlled (first-submit no-longer no-ops), grid Esc clears selection, pick mutation narrows invalidation to list/status/detail + removes character/descriptor caches, image preload re-fires on each new candidate group, and a 404 on cache-fallback auto-recovers to search phase. Descriptor service/handler: `ErrValidation`-for-missing-scenario-path → `ErrNotFound` + dead `errors.Is` branch removed; missing `visual_breakdown` now 404 instead of silent `auto=""`. Added: 2 Go tests (`TestPhaseBRunner_Run_PopulatesFrozenDescriptorOverrideFromRunStore`, `TestPhaseBRunner_Run_PreservesCallerSuppliedOverride`), 1 store test (`TestRunStore_LatestFrozenDescriptorBySCPID_ExcludesNonCompletedRuns`), 5 frontend tests (Ctrl+Z ref sync, mid-edit prefill survival, cache-404 search fallback, Esc-clears-selection, controlled-input prefill). `go test ./...` and `npm run test:unit` (111/111) + `npm run lint` all clean.
- 2026-04-19: Implemented Story 7.3. Added migration 009 + `runs.frozen_descriptor` column. Extended `CharacterService.Pick` for atomic pick+descriptor save. New endpoint `GET /api/runs/{id}/characters/descriptor`. Empty-query fallback on `GET /api/runs/{id}/characters`. `image_track.go` consumes `PhaseBRequest.FrozenDescriptorOverride`. New UI components `CharacterPick` + `VisionDescriptorEditor`, wired into `ProductionShell` for `character_pick/waiting`. 16 new tests + 8 fixture parses added. All Go and frontend tests pass (99 frontend + full Go suite).
