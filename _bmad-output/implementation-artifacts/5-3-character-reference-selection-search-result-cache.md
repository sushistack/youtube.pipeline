# Story 5.3: Character Reference Selection & Search Result Cache

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want to choose a character reference from cached search results,
so that I can maintain visual continuity without redundant search costs.

## Prerequisites

**Story 5.2 is the execution-context dependency for this story.** Character selection is reached only after Phase A completes and the run is paused at `character_pick`. This story must extend the existing stage machine and HITL persistence flow rather than inventing a parallel state model.

- preserve the existing `scenario_review -> character_pick -> image` transition contract in `internal/pipeline/engine_test.go`
- keep canonical run state in SQLite per NFR-R4; do not store the selected character only in memory or only on disk
- do not generate the canonical edited image here; Story 5.4 owns the Qwen-Image-Edit step that consumes `selected_character_id`

**Current codebase gap to resolve deliberately:** architecture documents mention future `character_service.go` and `handler_character.go`, but those files do not exist yet in the repository. The implementation for this story should follow the current repo's existing layering:

- persistence additions in `internal/db/`
- business logic in `internal/service/`
- HTTP wiring in `internal/api/`
- stage transitions and resume behavior in `internal/pipeline/`

## Acceptance Criteria

Unless stated otherwise, new tests follow the project's `TestXxx_CaseName` convention, live beside the code under test, call `testutil.BlockExternalHTTP(t)`, and use inline fakes plus `testutil.AssertEqual[T]` / `testutil.AssertJSONEq` rather than testify or gomock. Module path `github.com/sushistack/youtube.pipeline`. CGO_ENABLED=0.

**Persistence guard before implementation:** this story must introduce one SQLite-backed persistent cache for character-search results and one canonical run-state field for the chosen character. Do **not** split cache ownership across temp files, ad-hoc JSON blobs, or only-in-handler memory maps.

1. **AC-DDG-SEARCH-RETURNS-CHARACTERGROUP:** add a character search flow that retrieves up to 10 DuckDuckGo image candidates for a named character and returns them through a `CharacterGroup` response schema.

   Required outcome:
   - one service entry point accepts `run_id` plus a normalized character query
   - results are returned in deterministic order for a given cached payload
   - the HTTP/API response shape is stable enough for Story 7.3 to consume later

   Required candidate payload per item:
   - stable candidate ID within the cached result set
   - source page URL
   - direct image URL
   - preview/thumbnail URL when available
   - display title / source label when available

   Rules:
   - cap surfaced candidates at exactly 10 when DuckDuckGo returns more
   - normalize the operator-facing payload into a `CharacterGroup` domain/API schema rather than leaking raw DDG fields
   - external lookup belongs behind an injected client interface so service and API tests stay offline

   Tests:
   - `TestCharacterService_Search_ReturnsCharacterGroupWithTenCandidates`
   - `TestCharacterHandler_Search_EncodesCharacterGroupEnvelope`

2. **AC-SQLITE-PERSISTENT-SEARCH-CACHE:** persist character-search results in SQLite so repeated lookups for the same normalized query do not trigger a second external call (FR19).

   Required outcome:
   - add a migration for a dedicated cache table instead of overloading `runs` or `segments`
   - cached rows survive process restarts and are shared across runs
   - cache hit returns previously persisted results byte-for-byte equivalent after JSON round-trip

   Suggested table shape:

   ```sql
   CREATE TABLE character_search_cache (
       query_key   TEXT PRIMARY KEY,
       query_text  TEXT NOT NULL,
       result_json TEXT NOT NULL,
       created_at  TEXT NOT NULL DEFAULT (datetime('now')),
       updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
   );
   ```

   Rules:
   - use a normalized `query_key` so superficial input differences do not miss the cache
   - store the canonical result payload as JSON in SQLite; do not depend on a filesystem cache directory
   - no TTL logic is required in V1 unless implementation truly needs one; aggressive reuse is the requirement

   Tests:
   - `TestCharacterSearchCache_GetOrCreate_CacheMissStoresResult`
   - `TestCharacterSearchCache_GetOrCreate_CacheHitAvoidsExternalLookup`
   - `TestCharacterSearchCache_PersistsAcrossStoreReopen`

3. **AC-SELECTED-CHARACTER-ID-PERSISTED-ON-RUN:** persist the chosen character reference in run state as `selected_character_id`.

   Required outcome:
   - add a migration that extends canonical run persistence for `selected_character_id`
   - expose the field on `domain.Run` and `db.RunStore` scan/update paths
   - the selected ID survives server restart and resume flows

   Rules:
   - prefer `runs.selected_character_id` unless code inspection reveals a stronger existing canonical run-state home
   - the stored value must be the service-level candidate ID that can be resolved later by Story 5.4
   - do not hide the selection only inside `hitl_sessions.snapshot_json`; that row is transient by design

   Tests:
   - `TestRunStore_SetSelectedCharacterID_PersistsValue`
   - `TestRunStore_Get_IncludesSelectedCharacterID`

4. **AC-PICK-ENDPOINT-ADVANCES-STAGE:** when the operator submits a character choice, persist `selected_character_id` and advance the run from `character_pick` to `image`.

   Required behavior:
   - reject picks for runs not currently at `stage=character_pick` with a conflict-style domain error
   - validate that the selected candidate exists in the cached result set for the active query
   - on success, set `runs.stage = image` and `runs.status = running` (or the current engine-equivalent active status for `image`)

   Rules:
   - keep transition logic aligned with the existing `NextStage(..., EventApprove)` contract
   - do not perform the canonical image-edit call inside the pick endpoint
   - API surface should match architecture intent: `GET /api/runs/{id}/characters` and `POST /api/runs/{id}/characters/pick`

   Tests:
   - `TestCharacterService_Pick_PersistsSelectedCharacterIDAndAdvancesRun`
   - `TestCharacterService_Pick_RejectsUnknownCandidate`
   - `TestCharacterHandler_Pick_ReturnsConflictOutsideCharacterStage`

5. **AC-STORY-5-4-HANDOFF-CONTRACT:** the selected candidate must remain resolvable by Story 5.4, which uses `selected_character_id` to obtain the source reference for canonical image generation.

   Required outcome:
   - define one service/persistence method that resolves `selected_character_id` back to its cached candidate payload
   - document or encode the invariant that candidate IDs are meaningful only within the cached result set identified for that run/query
   - keep the handoff contract inside service/store code, not UI-only state

   Rules:
   - Story 5.4 should not need to call DuckDuckGo again if the run already has a selected candidate
   - resolution failure should surface a typed validation/not-found error, not a silent fallback to a new external search
   - if helpful, persist the normalized query key alongside the run selection so later resolution is straightforward

   Tests:
   - `TestCharacterService_GetSelectedCandidate_ResolvesFromRunStateAndCache`
   - `TestCharacterService_GetSelectedCandidate_MissingCacheRowFailsLoudly`

6. **AC-NO-REGRESSION-TO-HITL-DURABILITY:** the new character-search and pick flow must preserve the existing HITL durability model.

   Required behavior:
   - runs waiting at `character_pick` still reconstruct correctly through the existing status/HITL payloads
   - selection is durable even if the web UI is closed and reopened before Story 5.4 executes
   - no new behavior may break `scenario_review`, `batch_review`, or `metadata_ack` wait-point semantics

   Rules:
   - prefer additive persistence changes over modifying the `hitl_sessions` lifecycle invariant
   - if summary/status payloads expose the selected character, derive it from canonical SQLite state
   - preserve the current response envelope and domain error mapping style

   Tests:
   - `TestHITLStatus_CharacterPickRunIncludesDurableSelectionState`
   - `TestResume_CharacterPickRunPreservesSelectedCharacterID`

7. **AC-NO-REGRESSIONS:** `go test ./... && go build ./...` pass. Existing `run_store`, `engine`, `resume`, `hitl_service`, and API route tests remain green after the new cache table, run field, and character endpoints are added.

## Tasks / Subtasks

- [x] **T1: Add canonical SQLite persistence for character search cache and selected ID** (AC: #2, #3, #5)
  - [x] Add a migration for `character_search_cache`.
  - [x] Add a migration for `runs.selected_character_id` and any supporting query-key column if needed.
  - [x] Extend `internal/domain/types.go` and `internal/db/run_store.go` scan/update paths.

- [x] **T2: Implement offline-testable character search service and cache store** (AC: #1, #2, #5)
  - [x] Add store/service interfaces for DDG lookup and SQLite cache access.
  - [x] Normalize DDG payloads into a `CharacterGroup` schema with 10 surfaced candidates.
  - [x] Reuse cached results on repeated normalized queries.

- [x] **T3: Add operator pick flow that persists selection and advances stage** (AC: #3, #4, #6)
  - [x] Add service methods for search, pick, and selected-candidate resolution.
  - [x] Validate candidate membership against cached results before persisting.
  - [x] Advance the run from `character_pick` to `image` using the existing stage-transition semantics.

- [x] **T4: Wire HTTP routes and handlers for character search/pick** (AC: #1, #4, #6)
  - [x] Extend `internal/api/routes.go` with `/api/runs/{id}/characters` and `/api/runs/{id}/characters/pick`.
  - [x] Add handler coverage for success, validation failure, conflict, and not-found cases.
  - [x] Preserve the project's versioned response envelope.

- [x] **T5: Add deterministic persistence and integration coverage** (AC: #2, #3, #4, #5, #6, #7)
  - [x] Add DB tests for cache hit/miss and run-state persistence.
  - [x] Add service tests for stage gating and Story 5.4 handoff resolution.
  - [x] Run `go test ./...` and `go build ./...`.

## Dev Notes

### Epic Intent and Story Boundary

- Epic 5 covers FR14-FR19, and Story 5.3 specifically owns FR17 plus the FR19 cache contract. [Source: _bmad-output/planning-artifacts/epics.md, Epic 5 / Story 5.3]
- Story 5.4 depends on the persisted `selected_character_id` to resolve the chosen source reference for canonical image editing. This story should create that durable handoff, but must not perform the image-edit generation itself. [Source: _bmad-output/planning-artifacts/epics.md, Stories 5.3-5.4]

### Architecture Alignment

- Architecture already reserves `GET /api/runs/{id}/characters` and `POST /api/runs/{id}/characters/pick` for this workflow. Follow that contract now instead of inventing alternate paths. [Source: _bmad-output/planning-artifacts/architecture.md, API Surface]
- `character_pick` is a first-class HITL wait point. Operator selection should move the run forward only through the stage machine's approve semantics. [Source: _bmad-output/planning-artifacts/architecture.md, Stage constants / HITL wait points]
- The planned source tree points to `internal/service/character_service.go` and `internal/api/handler_character.go`, but those files are not present yet. Create them in line with current repo conventions rather than forcing the implementation into unrelated files. [Source: _bmad-output/planning-artifacts/architecture.md, source tree; codebase inspection on 2026-04-18]

### PRD and UX Guardrails

- PRD explicitly says the image track halts at the character-reference prerequisite, shows 10 DuckDuckGo candidates, lets the operator pick one, then resumes after a later canonical image-upgrade step. [Source: _bmad-output/planning-artifacts/prd.md, operator flow / FR17-FR19]
- PRD requires aggressive cache reuse across runs; a cache hit must avoid a second external DuckDuckGo lookup. [Source: _bmad-output/planning-artifacts/prd.md, FR19 / vendor notes]
- UX design expects a 10-grid with re-search / re-pick affordance and no auto-advance. The backend should therefore support repeated search before final pick, not a one-shot irreversible choice. [Source: _bmad-output/planning-artifacts/ux-design-specification.md, character anchoring / flow]
- Keyboard and UI design imply a stable candidate ordering and one selected item highlighted before confirmation, which reinforces the need for deterministic cached candidate IDs. [Source: _bmad-output/planning-artifacts/ux-design-specification.md, keyboard map]

### Existing Code to Extend, Not Replace

- `internal/domain/types.go` already defines `StageCharacterPick`, `StageImage`, and `domain.Run`; extend the run model rather than adding a separate duplicate state struct.
- `internal/db/run_store.go` is the canonical persistence path for `runs`; if `selected_character_id` is added to the schema, update `Create`, `Get`, `List`, and any explicit mutation methods there.
- `internal/service/hitl_service.go` already rebuilds paused-run status from canonical SQLite state plus `hitl_sessions`; selection state should integrate with that model, not bypass it.
- `internal/api/routes.go` currently registers only run lifecycle routes, so this story likely introduces the first dedicated character handler wiring.
- `internal/pipeline/resume.go` currently treats Phase B as a coarse cleanup boundary; do not accidentally erase `selected_character_id` during a `character_pick` or later resume unless the stage contract explicitly requires it.

### File Structure Notes

- Expected new files:
  - `internal/service/character_service.go`
  - `internal/service/character_service_test.go`
  - `internal/api/handler_character.go`
  - `internal/api/handler_character_test.go`
  - `internal/db/character_cache_store.go`
  - `internal/db/character_cache_store_test.go`
  - migration file for cache table / run-state extension
- Expected existing files to extend:
  - `internal/domain/types.go`
  - `internal/db/run_store.go`
  - `internal/api/routes.go`
  - `internal/service/hitl_service.go`
  - `internal/pipeline/resume.go`

### Testing Requirements

- Every new test must call `testutil.BlockExternalHTTP(t)`.
- Service tests should use an injected fake DDG client so cache hit/miss behavior is deterministic and offline.
- Persistence tests should verify both process-local reuse and reopen-from-SQLite durability.
- Add at least one end-to-end-ish service/API test proving that a pick made at `character_pick` persists `selected_character_id` and the run can later resolve the chosen candidate without another external lookup.

### Open Implementation Tension

- Candidate IDs need to be stable enough for Story 5.4 handoff, but DuckDuckGo payloads are not inherently canonical.
- A practical V1 solution is to persist the normalized query key plus an ordered candidate list and synthesize IDs from that stable ordering, for example `{query_key}#1` through `{query_key}#10`.
- If a better invariant emerges during implementation, preserve the two must-haves: durable resolution without re-search, and deterministic IDs across cache hits.

### References

- Epic definition and ACs: [epics.md](/home/jay/projects/youtube.pipeline/_bmad-output/planning-artifacts/epics.md)
- Sprint prompt shorthand: [sprint-prompts.md](/home/jay/projects/youtube.pipeline/_bmad-output/planning-artifacts/sprint-prompts.md)
- Architecture API + stage model: [architecture.md](/home/jay/projects/youtube.pipeline/_bmad-output/planning-artifacts/architecture.md)
- PRD character workflow and FR17-FR19: [prd.md](/home/jay/projects/youtube.pipeline/_bmad-output/planning-artifacts/prd.md)
- UX character-pick behavior: [ux-design-specification.md](/home/jay/projects/youtube.pipeline/_bmad-output/planning-artifacts/ux-design-specification.md)
- Prior story context: [5-2-parallel-media-generation-runner.md](/home/jay/projects/youtube.pipeline/_bmad-output/implementation-artifacts/5-2-parallel-media-generation-runner.md)

## Dev Agent Record

### Agent Model Used

GPT-5 Codex

### Debug Log References

- Create-story workflow analysis on 2026-04-18
- 2026-04-18: implemented SQLite cache + run-state persistence, character service, DDG client, API routes/handlers, and targeted tests
- 2026-04-18: validation runs: `go test ./internal/db ./internal/service ./internal/api` ✅, `go build ./...` ✅, `go test ./...` ✅

### Completion Notes List

- Story file created for explicit user-requested target `5-3-character-reference-selection-search-result-cache`
- Story scope anchored to current repository reality: no existing character service/handler files yet
- Story 5.4 handoff contract called out explicitly through durable `selected_character_id` resolution
- Added migration `008_character_search_cache.sql` for persistent DDG cache plus `runs.character_query_key` / `runs.selected_character_id`
- Added `CharacterGroup` / `CharacterCandidate` domain schema and extended `domain.Run` with character selection fields
- Implemented `CharacterCacheStore`, `CharacterService`, and a default DuckDuckGo client with injected interface for offline tests
- Added `GET /api/runs/{id}/characters` and `POST /api/runs/{id}/characters/pick` handlers wired through the existing versioned envelope
- Added DB, service, and API tests covering cache hit/miss, durable selected ID persistence, stage-gated pick flow, and Story 5.4 candidate resolution handoff
- Full repo validation now passes after rerun: `go test ./...` and `go build ./...`

## File List

- cmd/pipeline/serve.go
- internal/api/handler_character.go
- internal/api/handler_character_test.go
- internal/api/routes.go
- internal/db/character_cache_store.go
- internal/db/character_cache_store_test.go
- internal/db/run_store.go
- internal/db/run_store_test.go
- internal/db/sqlite_test.go
- internal/domain/types.go
- internal/service/character_service.go
- internal/service/character_service_test.go
- internal/service/duckduckgo_client.go
- migrations/008_character_search_cache.sql

## Change Log

- 2026-04-18: implemented Story 5.3 character reference search/pick flow, SQLite cache persistence, run-state selected character persistence, and DB/service/API coverage; validated with full `go test ./...` and `go build ./...`
