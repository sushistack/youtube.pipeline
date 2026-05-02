---
title: 'Cache panel for pending runs + drop_caches advance option'
type: 'feature'
created: '2026-05-02'
status: 'done'
context: []
baseline_commit: '907da83'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Operator (Jay, solo) has to drop deterministic-agent caches by running `rm -rf {outputDir}/{run_id}/*.json` in a terminal. Every other operation (stepper rewind, advance, scenario review) is UI-driven, so cache handling breaks UX consistency and risks typos against the wrong run.

**Approach:** When a run is `pending`, show a panel listing which caches exist on disk and let the operator toggle keep/drop per cache. On Start run, delete the unchecked caches before Phase A executes by extending the existing advance endpoint with an optional `drop_caches` body. Complementary to the SourceVersion auto-invalidation already shipped in `1a6c6de`.

## Boundaries & Constraints

**Always:**
- Empty cache set → no panel rendered (keeps the pending screen clean).
- Missing or empty `drop_caches` → existing advance behavior preserved verbatim.
- Cache deletion is idempotent: missing file is not an error.
- Backend: `writeJSON` / `writeDomainError` helpers, `domain.ErrValidation` sentinel, table-driven tests with `testutil` only (no testify).
- Frontend: snake_case identifiers, `queryKeys.runs.*` factory pattern, strict TS, Zod schemas via `apiRequest`.

**Never:**
- Touching `tryLoadCache` or `SourceVersionV1` (already shipped).
- Modifying deterministic agent logic (`researcher.go`, `structurer.go`).
- Bumping `testdata/contracts/*.schema.json` versions (separate work).
- Touching the running-state Restart button — different surface, separate spec.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected | Error |
|---|---|---|---|
| Both caches present | research + structure files exist | 200, 2 entries with size + mtime + source_version | — |
| One cache present | only research_cache.json | 200, 1 entry | — |
| Scenario cache present | scenario.json exists | included as `stage: "scenario"` | — |
| No caches | run dir empty or missing | 200, `{"caches":[]}` | — |
| Run not found | unknown run_id in DB | 404 | NOT_FOUND |
| Cache JSON malformed | source_version unparseable | entry included, source_version = "" | — |
| Advance with drop_caches=["research"] | file present | file deleted before goroutine, 202 | — |
| Advance with drop_caches=["research"] | file missing | 202, no error | — |
| Advance with drop_caches=["typo"] | unknown stage | 400, no files deleted | VALIDATION_ERROR |
| Advance with empty body | no body | 202, existing path | — |

</frozen-after-approval>

## Code Map

- `internal/api/routes.go:40-58` -- mount `GET /api/runs/{id}/cache` next to `/scenes` (line ~46).
- `internal/api/handler_run.go:24-36` -- `RunHandler` already has `outputDir` field.
- `internal/api/handler_run.go:377-401` -- `Advance()`: extend with optional body parsing; delete caches between `PrepareAdvance` (sync) and the goroutine `ExecuteAdvance`.
- `internal/api/handler_run.go` -- new `Cache()` method scans `{outputDir}/{id}/`.
- `internal/api/handler_run_test.go` -- existing table-driven `testutil` pattern.
- `internal/pipeline/phase_a.go:384,481` -- file names: `research_cache.json`, `structure_cache.json`.
- `internal/domain/scenario.go:18,44` -- `ResearcherOutput`/`StructurerOutput` `SourceVersion string `json:"source_version"`'.
- `web/src/lib/queryKeys.ts:19-32` -- add `runs.cache(id)`.
- `web/src/lib/apiClient.ts:203-218` -- extend `advanceRun` signature; add `fetchRunCache`.
- `web/src/hooks/useRunScenes.ts` -- shape to mirror for `useRunCache`.
- `web/src/components/shells/ProductionShell.tsx:217-274` -- `renderPendingDetail()` panel insertion site.
- `web/src/components/shells/ProductionShell.test.tsx:485-527` -- pending-state test to extend.

## Tasks & Acceptance

**Execution:**

- [x] `internal/api/handler_run.go` -- declare `var cacheFiles = map[string]string{"research": "research_cache.json", "structure": "structure_cache.json", "scenario": "scenario.json"}` as the single source of truth.
- [x] `internal/api/handler_run.go` -- add `Cache(w,r)` method: 404 if run not in DB; for each entry in `cacheFiles`, `os.Stat` then partial-unmarshal `{"source_version":...}`; return `{"caches":[...]}`.
- [x] `internal/api/handler_run.go` -- extend `Advance(w,r)`: when body non-empty, decode `{drop_caches: []string}`; validate every entry is a key of `cacheFiles` (else `domain.ErrValidation`); after successful `PrepareAdvance`, `os.Remove` each requested file (ignore `os.ErrNotExist`); then dispatch goroutine.
- [x] `internal/api/routes.go` -- `api.HandleFunc("GET /api/runs/{id}/cache", deps.Run.Cache)`.
- [x] `internal/api/handler_run_test.go` -- table-driven tests for `Cache` (both / one / none / not-found / malformed source_version) and `Advance` (drop existing / drop missing / unknown stage rejected / empty body backward-compatible / drop happens before goroutine — verify by checking file gone after handler returns).
- [x] `web/src/lib/queryKeys.ts` -- add `cache: (run_id) => ['runs','cache',run_id]`.
- [x] `web/src/lib/apiClient.ts` -- new Zod schema + `fetchRunCache(run_id)`; extend `advanceRun(run_id, options?: { drop_caches?: string[] })` — body only attached when `options?.drop_caches?.length > 0`.
- [x] `web/src/hooks/useRunCache.ts` -- mirror `useRunScenes`, gated on `run_id != null && status === 'pending'`.
- [x] `web/src/components/shells/ProductionShell.tsx` -- in pending branch, fetch cache; if non-empty, render panel of rows (checkbox default-checked = keep, label = stage, secondary line = `source_version` + relative `modified_at`); pass `drop_caches` (unchecked stages) to `advanceRun` on Start.
- [x] `web/src/components/shells/ProductionShell.test.tsx` -- three new pending-state cases: 0 caches → panel absent; N caches → N rows; uncheck research + Start → fetch called with `drop_caches:['research']`.
- [x] `web/src/index.css` -- minimal `.pending-cache-panel` styles (eyebrow header, mono stage label, muted metadata) — no new design tokens.

**Acceptance Criteria:**

- Given a pending run with research + structure caches, when the pending screen mounts, then a "Cached artifacts" section lists two rows with stage labels and source_version metadata.
- Given a pending run with no caches, when the pending screen mounts, then no cache section is rendered (DOM contains no `.pending-cache-panel`).
- Given research is unchecked and Start run is clicked, when advance fires, then the POST body is exactly `{"drop_caches":["research"]}` and `research_cache.json` is gone by the time the goroutine starts.
- Given `drop_caches` contains an unknown stage, when advance is POSTed, then status is 400 with `VALIDATION_ERROR` and no files were deleted.
- Given the legacy advance call (no body), when posted, then existing 202 + run-detail response is unchanged.

## Design Notes

**Cache deletion ordering in `Advance`:**

```go
run, err := h.svc.PrepareAdvance(ctx, id) // validates run + advancer
// parse body, validate drop_caches
// os.Remove each requested file (ignore ErrNotExist)
go h.svc.ExecuteAdvance(...)              // engine starts after files are gone
writeJSON(w, 202, toRunResponse(run))
```

Synchronous deletion before the goroutine guarantees test observability and avoids the race where the engine writes a fresh cache before deletion lands.

**Source-version extraction:** partial unmarshal `struct { SourceVersion string `json:"source_version"` }`; on JSON error return `""` (entry still surfaces — operator decides). Consistent with the JSON tag on both `ResearcherOutput` and `StructurerOutput`.

**Frontend gating:** `useRunCache` only fires for `pending` runs to avoid polling caches during normal Phase A execution.

## Verification

**Commands:**
- `go test ./internal/api/... ./internal/pipeline/...` -- expected: pass (existing tests + new Cache/Advance cases).
- `pnpm -C web test` -- expected: pass (extended ProductionShell.test.tsx).
- `pnpm -C web build` -- expected: type-clean build.

**Manual:**
- `go run ./cmd/youtube-pipeline serve`; create a fresh run; advance through Phase A to scenario_review; rewind to pending; refresh → panel shows two cached rows. Uncheck research, click Start run → research_cache.json mtime moves forward (regenerated), structure_cache.json mtime stays.

## Suggested Review Order

**Single source of truth + canonical ordering**

- Stage ↔ filename map plus the stable iteration order both ranges follow.
  [`handler_run.go:25-39`](../../internal/api/handler_run.go#L25)

**Backend: cache listing**

- Cache() walks `cacheStageOrder`, surfaces per-row metadata + partial-unmarshal source_version.
  [`handler_run.go:422`](../../internal/api/handler_run.go#L422)

- New route mounted next to /scenes.
  [`routes.go:47`](../../internal/api/routes.go#L47)

**Backend: drop_caches semantics in Advance**

- Optional body parsed; unknown stage rejected BEFORE PrepareAdvance so no FS side-effects on bad input.
  [`handler_run.go:487`](../../internal/api/handler_run.go#L487)

- Synchronous deletion happens between PrepareAdvance and the goroutine — engine sees a clean slate.
  [`handler_run.go:520`](../../internal/api/handler_run.go#L520)

**Backend: tests proving the contract**

- Table-driven coverage of every I/O matrix row.
  [`handler_run_test.go:723`](../../internal/api/handler_run_test.go#L723)

- "Deleted before goroutine" assertion pins the synchronous-delete contract.
  [`handler_run_test.go:867`](../../internal/api/handler_run_test.go#L867)

**Frontend: data plumbing**

- Zod schema mirrors wire shape; `runs.cache(id)` factory key.
  [`apiClient.ts:227`](../../web/src/lib/apiClient.ts#L227)

- `advanceRun` body attached only when drop_caches is non-empty (legacy callers byte-identical).
  [`apiClient.ts:208`](../../web/src/lib/apiClient.ts#L208)

- Hook gated on `pending` status — no polling during Phase A.
  [`useRunCache.ts:15`](../../web/src/hooks/useRunCache.ts#L15)

**Frontend: panel render + state**

- Panel rendered between guidance text and Start, only when `cache_entries.length > 0`.
  [`ProductionShell.tsx:279`](../../web/src/components/shells/ProductionShell.tsx#L279)

- Per-run `dropped_cache_state` keyed by `for_run_id` — selection auto-clears when run changes.
  [`ProductionShell.tsx:513`](../../web/src/components/shells/ProductionShell.tsx#L513)

- Start derives drop_caches from the unchecked set.
  [`ProductionShell.tsx:325`](../../web/src/components/shells/ProductionShell.tsx#L325)

**Frontend: tests covering the three AC scenarios**

- Panel hidden / N rows / drop_caches body emitted on uncheck+Start.
  [`ProductionShell.test.tsx:645`](../../web/src/components/shells/ProductionShell.test.tsx#L645)

**Peripherals**

- Minimal panel styles, design-token aligned (no new tokens introduced).
  [`index.css:2176`](../../web/src/index.css#L2176)
