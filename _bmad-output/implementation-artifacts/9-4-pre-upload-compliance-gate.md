# Story 9.4: Pre-Upload Compliance Gate

Status: done

## Story

As an operator,
I want a final manual check of the video and metadata before public upload,
so that I can verify everything is correct and explicitly set the run to "ready-for-upload" status.

## Prerequisites

**Hard dependencies (runtime, not development):**
- Story 9.1 (FFmpeg Assembly) + Story 9.2 (Metadata Bundle) + Story 9.3 (Compliance Audit Logging)
  must complete at runtime before this story's UI is reachable — the state machine only reaches
  `metadata_ack` after `assemble` completes with EventComplete (engine.go line 78–79).
- Story 9.2 populates `metadata.json` and `manifest.json` as the entry action for `metadata_ack`.
  These files MUST exist before Story 9.4's UI is shown. Do not add fallback logic for missing files —
  if they are absent, the handler returns 404 and that is a Story 9.2 regression.

**State machine context (engine.go lines 81–84):**
```go
case domain.StageMetadataAck:
    switch event {
    case domain.EventApprove:
        return domain.StageComplete, nil
    }
```
`metadata_ack` → (EventApprove) → `complete` is the ONLY path to `status=completed`.
`IsHITLStage` already returns true for `StageMetadataAck` (engine.go line 96) — do NOT add it again.

**NFR-L1 (HARD):** `ready-for-upload` (i.e., `stage='complete', status='completed'`) MUST only be
reachable via `POST /api/runs/{id}/metadata/ack`. There is no bypass path. The service-layer guard
(validate `stage==metadata_ack && status==waiting` before any DB write) is the enforcement point.

---

## Acceptance Criteria

### AC-1: AcknowledgeMetadata service + DB transition

**Given** a run at `stage=metadata_ack, status=waiting`
**When** `RunService.AcknowledgeMetadata(ctx, runID)` is called
**Then** the run's `stage` is updated to `complete` and `status` to `completed` in the database
**And** the updated `*domain.Run` is returned

**Given** a run NOT at `stage=metadata_ack` or `status!=waiting`
**When** `AcknowledgeMetadata` is called
**Then** it returns `domain.ErrConflict` wrapping an explanatory message
**And** no DB write occurs

**Tests:**
- Unit — `RunService.AcknowledgeMetadata` with correct stage: returns run with `stage='complete'`
- Unit — with wrong stage: returns ErrConflict, DB not touched

---

### AC-2: `POST /api/runs/{id}/metadata/ack` endpoint

**Given** a run at `metadata_ack + waiting`
**When** `POST /api/runs/{id}/metadata/ack` is called (no request body required)
**Then** 200 with `{"version":1,"data":{...run...}}` where run has `stage='complete'`

**Given** run does not exist
**Then** 404 with `VALIDATION_ERROR` (via `mapDomainError`)

**Given** wrong stage/status (e.g., run already completed)
**Then** 409 with `CONFLICT`

**Tests:** Handler tests in `handler_run_test.go` using `httptest.NewRecorder` pattern (mirror
existing handler tests — see `handler_character_test.go` for pick endpoint pattern).

---

### AC-3: Artifact serving endpoints (video, metadata, manifest)

Three new read-only endpoints serve run artifacts to the browser. **Security invariant:** run ID is
validated against the database before building any file path — this prevents arbitrary reads.

```
GET /api/runs/{id}/video        → {outputDir}/{id}/output.mp4     (video/mp4)
GET /api/runs/{id}/metadata     → {outputDir}/{id}/metadata.json  (application/json)
GET /api/runs/{id}/manifest     → {outputDir}/{id}/manifest.json  (application/json)
```

**Given** the run exists and its stage is `metadata_ack` or `complete`
**When** the artifact endpoint is hit
**Then** the file is served with the correct `Content-Type`
**And** range requests are supported (use `http.ServeContent` — handles ETag, Range, Last-Modified)

**Given** the run exists but is at an earlier stage (artifact not yet generated)
**Then** 404

**Given** the run does not exist
**Then** 404

**File path construction (no path traversal possible):**
```go
func (h *ArtifactsHandler) serveRunFile(w http.ResponseWriter, r *http.Request, filename, contentType string) {
    runID := r.PathValue("id")
    run, err := h.svc.Get(r.Context(), runID)
    if err != nil || (run.Stage != domain.StageMetadataAck && run.Stage != domain.StageComplete) {
        http.NotFound(w, r)
        return
    }
    path := filepath.Join(h.outputDir, runID, filename)
    f, err := os.Open(path)
    if err != nil {
        http.NotFound(w, r)
        return
    }
    defer f.Close()
    stat, _ := f.Stat()
    w.Header().Set("Content-Type", contentType)
    http.ServeContent(w, r, filename, stat.ModTime(), f)
}
```

**Tests:** In `handler_artifacts_test.go` — use `os.CreateTemp`/`t.TempDir()` to create fixture files;
verify correct status codes and Content-Type headers.

---

### AC-4: ComplianceGate UI component

**Given** a run at `stage=metadata_ack, status=waiting`
**When** ProductionShell renders the active content slot
**Then** `<ComplianceGate>` is rendered (new case added to ProductionShell's stage dispatch)

**ComplianceGate layout:**
1. **Video preview panel** — `<video>` element with `src="/api/runs/{run_id}/video"`, `autoPlay`, `muted`,
   `loop={false}`, and a 5-second auto-stop. On load success, mark "Video plays" checklist item.
   On 404/error, show amber banner "Video not yet available" but don't block Finalize.
2. **Metadata checklist** — Fetches `/api/runs/{run_id}/metadata` and `/api/runs/{run_id}/manifest`.
   Displays the following checkboxes (operator must manually check each):
   - `[ ] Title confirmed: {metadata.title}`
   - `[ ] AI disclosure — Narration: {metadata.ai_generated.narration ? "AI" : "Human"}`
   - `[ ] AI disclosure — Imagery: {metadata.ai_generated.imagery ? "AI" : "Human"}`
   - `[ ] AI disclosure — TTS: {metadata.ai_generated.tts ? "AI" : "Human"}`
   - `[ ] Models logged: {Object.keys(metadata.models_used).join(", ")}`
   - `[ ] Source URL confirmed: {manifest.source_url}`
   - `[ ] Author confirmed: {manifest.author_name}`
   - `[ ] License: {manifest.license}`
3. **Finalize button** — enabled ONLY when all checkboxes are checked.
   On click: calls `POST /api/runs/{id}/metadata/ack` (standard mutation pattern).
   On success: TanStack Query invalidates `queryKeys.runs.status(run_id)` so ProductionShell
   re-renders with the new `stage=complete` — this triggers `CompletionReward` automatically.
   On error: display error banner inline.

**Loading states:** While metadata/manifest fetch is pending, show skeleton placeholder for
checklist items. If either fetch fails (not just 404 — also network errors), show amber error
message but still allow the Finalize button (operator can proceed if they have out-of-band
knowledge the content is correct).

**Tests:** `ComplianceGate.test.tsx` — mock `apiClient` calls; verify:
- All checkboxes unchecked → Finalize disabled
- All checkboxes checked → Finalize enabled
- Successful ack → invalidates status query

---

### AC-5: CompletionReward UI component

**Given** a run at `stage=complete, status=completed`
**When** ProductionShell renders the active content slot
**Then** `<CompletionReward>` is rendered (new case in ProductionShell dispatch)

**CompletionReward layout:**
1. **Video reward panel** — same `<video>` element as ComplianceGate with `autoPlay`, `muted`,
   5-second auto-stop. Title: "Upload ready 🎬" (or without emoji if house style prohibits).
2. **Metadata status** — compact table showing `Title`, `Source`, `Author`, `License` from metadata/manifest.
3. **Next-action CTA:**
   - Primary: `"Start Next SCP"` button → calls `open_new_run_panel()` from `useNewRunCoordinator`
   - Secondary: plain text `"Run ID: {run_id}"` for clipboard copy

**Tests:** `CompletionReward.test.tsx` — mock fetch calls; verify summary table renders with correct
values; verify "Start Next SCP" button invokes `open_new_run_panel`.

---

## Tasks / Subtasks

- [x] Task 1: DB layer — add `MarkComplete` to RunStore (AC: 1)
  - [x] 1.1 Add `MarkComplete(ctx context.Context, id string) error` to `RunStore` interface in
        `internal/service/run_service.go`
  - [x] 1.2 Implement `MarkComplete` on `*db.RunStore` in `internal/db/run_store.go`:
        `UPDATE runs SET stage='complete', status='completed', updated_at=? WHERE id=?`
        Use `time.Now().UTC().Format(time.RFC3339Nano)` for `updated_at`
  - [x] 1.3 Add `TestRunStore_MarkComplete` in `internal/db/run_store_test.go`:
        seed run at `metadata_ack/waiting`, call `MarkComplete`, re-fetch and assert stage/status

- [x] Task 2: Service — `RunService.AcknowledgeMetadata` (AC: 1)
  - [x] 2.1 Add `AcknowledgeMetadata(ctx context.Context, runID string) (*domain.Run, error)` to
        `RunService` in `internal/service/run_service.go`
  - [x] 2.2 Implementation: `Get` → validate stage + status → `store.MarkComplete` → `Get` (re-fetch)
  - [x] 2.3 Wrong stage returns `fmt.Errorf("acknowledge metadata: run is not awaiting metadata acknowledgment: %w", domain.ErrConflict)`
  - [x] 2.4 Unit tests in `internal/service/run_service_test.go`:
        - happy path: stage transitions to complete
        - wrong stage: ErrConflict returned, MarkComplete not called (use test double/mock)

- [x] Task 3: API handler — `AcknowledgeMetadata` (AC: 2)
  - [x] 3.1 Add `AcknowledgeMetadata(w, r)` handler method to `RunHandler` in `internal/api/handler_run.go`
        Pattern: no request body, `runID := r.PathValue("id")`, call `h.svc.AcknowledgeMetadata`, return run via `writeJSON`
  - [x] 3.2 Tests in `internal/api/handler_run_test.go`: 200 happy path; 404 not found; 409 wrong stage

- [x] Task 4: Artifact serving handlers (AC: 3)
  - [x] 4.1 Create `internal/api/handler_artifacts.go`:
        - `ArtifactsHandler` struct with fields: `svc RunArtifactsStore` (interface), `outputDir string`
        - `RunArtifactsStore` interface: `Get(ctx, id) (*domain.Run, error)`
        - Methods: `Video`, `Metadata`, `Manifest` — each calls `serveRunFile(w, r, filename, contentType)`
        - `serveRunFile` implementation (see AC-3 code sample above)
        - Use `filepath.Join` not string concatenation for path safety
  - [x] 4.2 Tests in `internal/api/handler_artifacts_test.go`:
        - Create temp file; assert 200 + Content-Type for metadata_ack and complete stages
        - Assert 404 for non-existent run
        - Assert 404 for run at wrong stage (e.g., batch_review)

- [x] Task 5: Route registration (AC: 2, 3) — 7-step endpoint checklist
  - [x] 5.1 Add `ArtifactsHandler` field to `Dependencies` struct in `routes.go`
  - [x] 5.2 Register 4 new routes in `RegisterRoutes`:
        ```go
        api.HandleFunc("POST /api/runs/{id}/metadata/ack", deps.Run.AcknowledgeMetadata)
        api.HandleFunc("GET /api/runs/{id}/video",         deps.Artifacts.Video)
        api.HandleFunc("GET /api/runs/{id}/metadata",      deps.Artifacts.Metadata)
        api.HandleFunc("GET /api/runs/{id}/manifest",      deps.Artifacts.Manifest)
        ```
  - [x] 5.3 Update `NewDependencies` to construct `ArtifactsHandler` and wire it

- [x] Task 6: API client + query keys (AC: 4, 5)
  - [x] 6.1 Add to `web/src/lib/apiClient.ts`:
        ```ts
        export function acknowledgeMetadata(run_id: string) {
            return apiFetch(`/runs/${encodeURIComponent(run_id)}/metadata/ack`, runResponseSchema, { method: 'POST' })
        }
        export function fetchRunMetadata(run_id: string) {
            return apiFetch(`/runs/${encodeURIComponent(run_id)}/metadata`, metadataBundleSchema)
        }
        export function fetchRunManifest(run_id: string) {
            return apiFetch(`/runs/${encodeURIComponent(run_id)}/manifest`, sourceManifestSchema)
        }
        ```
  - [x] 6.2 Define Zod schemas `metadataBundleSchema` and `sourceManifestSchema` in `web/src/contracts/runContracts.ts`
        Mirroring domain types from Story 9.2
  - [x] 6.3 Add to `web/src/lib/queryKeys.ts`:
        ```ts
        metadata: (run_id: string) => ['runs', 'metadata', run_id] as const,
        manifest: (run_id: string) => ['runs', 'manifest', run_id] as const,
        ```

- [x] Task 7: ComplianceGate component (AC: 4)
  - [x] 7.1 Create `web/src/components/production/ComplianceGate.tsx`
  - [x] 7.2 Create `web/src/components/production/ComplianceGate.test.tsx`

- [x] Task 8: CompletionReward component (AC: 5)
  - [x] 8.1 Create `web/src/components/production/CompletionReward.tsx`
  - [x] 8.2 Create `web/src/components/production/CompletionReward.test.tsx`

- [x] Task 9: ProductionShell integration (AC: 4, 5)
  - [x] 9.1 Add imports for `ComplianceGate` and `CompletionReward` to `ProductionShell.tsx`
  - [x] 9.2 Add two new cases in the stage dispatch block

### Review Findings (2026-04-24)

- [x] [Review][Patch] Zod schema strictness split: keep `.min(1)` on compliance-critical fields, relax nice-to-have fields ([web/src/contracts/runContracts.ts](../../web/src/contracts/runContracts.ts)) — Decision C: `title`, `source_url`, `license` remain `.min(1)` (NFR-L1 compliance disclosure requires them); `author_name` → `.string()` (allow empty — orphan works), `license_url` → `.string()` (CC0 and some PD licenses omit URL). Also make `license_chain[].author_name` and `license_chain[].license_url` consistent.
- [x] [Review][Patch] Split `apiRequest` into envelope-less variant for raw-JSON endpoints ([web/src/lib/apiClient.ts](../../web/src/lib/apiClient.ts)) — Decision B: extract a `apiRequestRaw<T>(path, schema, init)` helper that (a) still parses `errorEnvelopeSchema` from non-2xx responses so `ApiClientError.code` is populated, but (b) parses the 2xx body directly with the provided schema (no `{data: T}` wrapper). Replace the raw `fetch` in `fetchRunMetadata`/`fetchRunManifest` with this helper.
- [x] [Review][Patch] Video error banner bound to wrong signal ([web/src/components/production/ComplianceGate.tsx:129-134](../../web/src/components/production/ComplianceGate.tsx)) — `{metadata_query.isError && <... "Video not yet available" ...>}`. Spec AC-4: "On 404/error, show amber banner". Video element has no `onError` handler. Metadata fetch error ≠ video error; signals are crossed. Attach `onError`/`onLoadedData` to the `<video>` element and gate the banner on video-load state.
- [x] [Review][Patch] `MarkComplete` + `AcknowledgeMetadata` TOCTOU / no RowsAffected check ([internal/db/run_store.go:298-304](../../internal/db/run_store.go), [internal/service/run_service.go:119-131](../../internal/service/run_service.go)) — `MarkComplete` UPDATE has no stage/status predicate and ignores `RowsAffected`. Every other UPDATE in the same file returns `ErrNotFound` on 0 rows. Service `Get → check → MarkComplete` is not transactional. A concurrent `Cancel` racing an `Ack` flips a cancelled run to `complete/completed`, violating NFR-L1's terminal-state invariant. `TestRunStore_MarkComplete_NotFound` ([internal/db/run_store_test.go:1084-1090](../../internal/db/run_store_test.go)) even codifies the silent-success behavior as desired. Fix: `UPDATE runs SET ... WHERE id=? AND stage='metadata_ack' AND status='waiting'`, check `RowsAffected`, return `ErrConflict` on 0 rows; update the test to assert this.
- [x] [Review][Patch] Stale metadata/manifest after resume — staleTime 60s + no cache control ([web/src/components/production/ComplianceGate.tsx:48-60](../../web/src/components/production/ComplianceGate.tsx), [internal/api/handler_artifacts.go:67-68](../../internal/api/handler_artifacts.go)) — `resume.go:333-335` re-runs metadata entry on `StageMetadataAck` resume, rewriting `metadata.json`/`manifest.json`. React Query holds stale data for 60s; backend sets no `Cache-Control` on JSON artifacts. Operator can confirm checklist against pre-resume content. Fix: set `staleTime: 0` on these two queries (cheap JSON), OR set `Cache-Control: no-store` on `GET /api/runs/{id}/metadata` + `/manifest` handlers, OR invalidate `runs.metadata`/`runs.manifest` when `useRunStatus` observes `updated_at` change.
- [x] [Review][Patch] `runs.list()` not invalidated after ack ([web/src/components/production/ComplianceGate.tsx:64-68](../../web/src/components/production/ComplianceGate.tsx)) — only `queryKeys.runs.status(run.id)` is invalidated. Sidebar (list) shows run at `metadata_ack/waiting` for up to 5s after ack. Add `query_client.invalidateQueries({ queryKey: queryKeys.runs.list() })` alongside the status invalidation.
- [x] [Review][Patch] Finalize button re-enabled briefly between ack success and status refetch ([web/src/components/production/ComplianceGate.tsx:185](../../web/src/components/production/ComplianceGate.tsx)) — `disabled={!all_checked || ack_mutation.isPending}`. After success, `isPending` flips to `false` synchronously while the status query refetch is in-flight. Component still mounted (stage hasn't changed in state yet). Second click → 409 conflict → red error banner on an already-successful ack. Fix: also gate on `ack_mutation.isSuccess` or track a local `ack_submitted` flag.
- [x] [Review][Patch] Error banner is sticky across retries/success ([web/src/components/production/ComplianceGate.tsx:33,69-72](../../web/src/components/production/ComplianceGate.tsx)) — `error_message` set in `onError` but never cleared in `onSuccess` or before `mutate()`. First attempt fails (transient 500), retry succeeds, red banner still visible. Clear `error_message` in `onSuccess` and in `onMutate`.
- [x] [Review][Patch] Hard-coded test path `scp-049-run-1` drifts if Create ID format changes ([internal/api/handler_artifacts_test.go:38](../../internal/api/handler_artifacts_test.go)) — `filepath.Join(outDir, "scp-049-run-1")` + raw UPDATE seeding by `scp_id='049'`. If `RunStore.Create` ever changes ID synthesis, the UPDATE mutates 0 rows silently and tests false-pass via 404 paths. Use `run.ID` from `svc.Create`'s return value instead.
- [x] [Review][Patch] Missing nil guard in `serveRunFile` ([internal/api/handler_artifacts.go:50-52](../../internal/api/handler_artifacts.go)) — `if err != nil || (run.Stage != ...)`. Current `db.RunStore.Get` never returns `(nil, nil)`, but `RunArtifactsStore` is an exported interface others can implement. Add explicit `run == nil` branch before the stage check.
- [x] [Review][Patch] No test for `ack_mutation.onSuccess` invalidation ([web/src/components/production/ComplianceGate.test.tsx](../../web/src/components/production/ComplianceGate.test.tsx)) — AC-4 tests list requires "Successful ack → invalidates status query" but no assertion verifies `queryClient.invalidateQueries({queryKey: queryKeys.runs.status(run.id)})` is invoked. Add a test that spies on the query client.
- [x] [Review][Patch] Checklist state typed `Record<string, boolean>` instead of `Record<ChecklistId, boolean>` ([web/src/components/production/ComplianceGate.tsx:32](../../web/src/components/production/ComplianceGate.tsx)) — loses the compile-time guarantee that `toggleCheckbox(id)` accepts only valid checklist IDs. Tighten the type.
- [x] [Review][Defer] Symlink path-traversal defense-in-depth [internal/api/handler_artifacts.go:55](../../internal/api/handler_artifacts.go) — deferred, low risk for single-operator desktop tool (attacker model is weak; output dir created by pipeline itself).
- [x] [Review][Defer] `CompletionReward` autoplay missing `currentTime = 0` reset on replay [web/src/components/production/CompletionReward.tsx:23-26](../../web/src/components/production/CompletionReward.tsx) — deferred, nit UX polish.
- [x] [Review][Defer] Zod `version: z.number().int().nonnegative()` accepts any non-negative int [web/src/contracts/runContracts.ts](../../web/src/contracts/runContracts.ts) — deferred, forward-compat trap but no current break.
- [x] [Review][Defer] `ack_mutation.onError` doesn't distinguish error types (network vs 409 vs 500) [web/src/components/production/ComplianceGate.tsx:69-72](../../web/src/components/production/ComplianceGate.tsx) — deferred, minor UX.
- [x] [Review][Defer] `AcknowledgeMetadata` handler lacks `MaxBytesReader` protection [internal/api/handler_run.go](../../internal/api/handler_run.go) — deferred, hardening item not exercised by normal client.

**Review summary:** Blind Hunter + Edge Case Hunter + Acceptance Auditor. 2 decisions resolved (D1→C, D2→B), 12 patch, 5 deferred, 7 dismissed (spec-sanctioned `has_fetch_error` Finalize allowance, false-positive Provider-crash, spec typo `VALIDATION_ERROR` vs `NOT_FOUND`, `BlockExternalHTTP` cargo-culting, `fakeRunStore` stub, spec self-contradictory "Video plays" checklist item, `http.ServeContent` preset Content-Type).

---

## Dev Notes

### New files to create

| File | Purpose |
|------|---------|
| `internal/api/handler_artifacts.go` | Video/metadata/manifest file serving |
| `internal/api/handler_artifacts_test.go` | Artifact handler tests |
| `web/src/components/production/ComplianceGate.tsx` | Compliance gate UI (metadata_ack stage) |
| `web/src/components/production/ComplianceGate.test.tsx` | Component tests |
| `web/src/components/production/CompletionReward.tsx` | Completion reward UI (complete stage) |
| `web/src/components/production/CompletionReward.test.tsx` | Component tests |

### Files to modify

| File | Change |
|------|--------|
| `internal/service/run_service.go` | Add `MarkComplete` to `RunStore` interface; add `AcknowledgeMetadata` method |
| `internal/service/run_service_test.go` | Add `AcknowledgeMetadata` tests |
| `internal/db/run_store.go` | Implement `MarkComplete` |
| `internal/db/run_store_test.go` | Add `TestRunStore_MarkComplete` |
| `internal/api/handler_run.go` | Add `AcknowledgeMetadata` handler method |
| `internal/api/handler_run_test.go` | Add ack endpoint tests |
| `internal/api/routes.go` | Register 4 new routes; add `ArtifactsHandler` to `Dependencies`/`NewDependencies` |
| `web/src/lib/apiClient.ts` | Add 3 new functions + Zod schemas |
| `web/src/lib/queryKeys.ts` | Add `metadata` and `manifest` query keys |
| `web/src/components/shells/ProductionShell.tsx` | Add ComplianceGate + CompletionReward to dispatch |

### State machine invariants (do NOT change)

- `pipeline.NextStage(StageMetadataAck, EventApprove)` already returns `StageComplete` — do not touch `engine.go`
- `pipeline.StatusForStage(StageComplete)` returns `StatusCompleted` — but we call `MarkComplete` directly (single DB round-trip) rather than going through NextStage + StatusForStage separately
- `pipeline.IsHITLStage(StageMetadataAck)` already returns `true` — do not re-add

### RunStore interface extension (in run_service.go)

Add only `MarkComplete` to the existing interface — no other changes:
```go
type RunStore interface {
    Create(ctx context.Context, scpID, outputDir string) (*domain.Run, error)
    Get(ctx context.Context, id string) (*domain.Run, error)
    List(ctx context.Context) ([]*domain.Run, error)
    Cancel(ctx context.Context, id string) error
    MarkComplete(ctx context.Context, id string) error  // NEW: sets stage=complete, status=completed
}
```
Any existing RunStore mock/test double used in `run_service_test.go` must be updated to implement `MarkComplete`.

### MarkComplete implementation (db/run_store.go)

```go
func (s *RunStore) MarkComplete(ctx context.Context, id string) error {
    _, err := s.db.ExecContext(ctx,
        `UPDATE runs SET stage = 'complete', status = 'completed', updated_at = ? WHERE id = ?`,
        time.Now().UTC().Format(time.RFC3339Nano), id,
    )
    return err
}
```

### AcknowledgeMetadata implementation (service/run_service.go)

```go
func (s *RunService) AcknowledgeMetadata(ctx context.Context, runID string) (*domain.Run, error) {
    run, err := s.store.Get(ctx, runID)
    if err != nil {
        return nil, err
    }
    if run.Stage != domain.StageMetadataAck || run.Status != domain.StatusWaiting {
        return nil, fmt.Errorf("acknowledge metadata: run is not awaiting metadata acknowledgment: %w", domain.ErrConflict)
    }
    if err := s.store.MarkComplete(ctx, runID); err != nil {
        return nil, fmt.Errorf("acknowledge metadata: persist: %w", err)
    }
    return s.store.Get(ctx, runID)
}
```

### AcknowledgeMetadata handler (api/handler_run.go)

```go
// AcknowledgeMetadata handles POST /api/runs/{id}/metadata/ack.
// No request body. Transitions metadata_ack → complete (NFR-L1 gate).
func (h *RunHandler) AcknowledgeMetadata(w http.ResponseWriter, r *http.Request) {
    runID := r.PathValue("id")
    run, err := h.svc.AcknowledgeMetadata(r.Context(), runID)
    if err != nil {
        writeDomainError(w, err)
        return
    }
    writeJSON(w, http.StatusOK, toRunResponse(run))
}
```

### ArtifactsHandler (api/handler_artifacts.go)

```go
package api

import (
    "net/http"
    "os"
    "path/filepath"

    "github.com/sushistack/youtube.pipeline/internal/domain"
)

type RunArtifactsStore interface {
    Get(ctx context.Context, id string) (*domain.Run, error)
}

type ArtifactsHandler struct {
    svc       RunArtifactsStore
    outputDir string
}

func NewArtifactsHandler(svc RunArtifactsStore, outputDir string) *ArtifactsHandler {
    return &ArtifactsHandler{svc: svc, outputDir: outputDir}
}

func (h *ArtifactsHandler) Video(w http.ResponseWriter, r *http.Request) {
    h.serveRunFile(w, r, "output.mp4", "video/mp4")
}
func (h *ArtifactsHandler) Metadata(w http.ResponseWriter, r *http.Request) {
    h.serveRunFile(w, r, "metadata.json", "application/json")
}
func (h *ArtifactsHandler) Manifest(w http.ResponseWriter, r *http.Request) {
    h.serveRunFile(w, r, "manifest.json", "application/json")
}

func (h *ArtifactsHandler) serveRunFile(w http.ResponseWriter, r *http.Request, filename, contentType string) {
    runID := r.PathValue("id")
    run, err := h.svc.Get(r.Context(), runID)
    if err != nil || (run.Stage != domain.StageMetadataAck && run.Stage != domain.StageComplete) {
        http.NotFound(w, r)
        return
    }
    path := filepath.Join(h.outputDir, runID, filename)
    f, err := os.Open(path)
    if err != nil {
        http.NotFound(w, r)
        return
    }
    defer f.Close()
    stat, err := f.Stat()
    if err != nil {
        http.NotFound(w, r)
        return
    }
    w.Header().Set("Content-Type", contentType)
    http.ServeContent(w, r, filename, stat.ModTime(), f)
}
```

### Routes update (api/routes.go)

```go
type Dependencies struct {
    Run       *RunHandler
    Artifacts *ArtifactsHandler  // NEW
    Character *CharacterHandler
    Scene     *SceneHandler
    // ... existing fields unchanged
}

// In RegisterRoutes, add after existing api.HandleFunc calls:
api.HandleFunc("POST /api/runs/{id}/metadata/ack", deps.Run.AcknowledgeMetadata)
api.HandleFunc("GET /api/runs/{id}/video",          deps.Artifacts.Video)
api.HandleFunc("GET /api/runs/{id}/metadata",       deps.Artifacts.Metadata)
api.HandleFunc("GET /api/runs/{id}/manifest",       deps.Artifacts.Manifest)

// In NewDependencies, add:
Artifacts: NewArtifactsHandler(svc, outputDir),
```

Note: `RunArtifactsStore` interface is satisfied by `*service.RunService` structurally (it has `Get`).
Wire `svc` (the `*service.RunService`) into `NewArtifactsHandler`.

### ComplianceGate 5-second autoplay pattern

```tsx
const video_ref = useRef<HTMLVideoElement>(null)

useEffect(() => {
  const video = video_ref.current
  if (!video) return
  const onTimeUpdate = () => {
    if (video.currentTime >= 5) {
      video.pause()
    }
  }
  video.addEventListener('timeupdate', onTimeUpdate)
  return () => video.removeEventListener('timeupdate', onTimeUpdate)
}, [run.id])
```

Use `run.id` in the effect dependency (not just `[]`) so switching between runs remounts cleanly.
Video element: `<video ref={video_ref} src={`/api/runs/${run.id}/video`} autoPlay muted playsInline />`.

### ProductionShell dispatch pattern

The existing dispatch block (ProductionShell.tsx line 299–358) uses `stage + status` conditions.
Insert the two new cases BEFORE the final `<ProductionShortcutPanel />` fallback:

```tsx
) : current_run.stage === 'metadata_ack' && current_run.status === 'waiting' ? (
  <ComplianceGate key={current_run.id} run={current_run} />
) : current_run.stage === 'complete' && current_run.status === 'completed' ? (
  <CompletionReward key={current_run.id} run={current_run} />
) : (
  <ProductionShortcutPanel />
```

The `key={current_run.id}` is essential — mirrors the existing `CharacterPick` and `BatchReview` keys
to prevent state leakage when switching between runs.

### Domain types for metadata (for Zod schemas)

`MetadataBundle` (from Story 9.2 `internal/domain/compliance.go`):
```json
{
  "version": 1,
  "generated_at": "2026-04-20T...",
  "run_id": "scp-049-run-1",
  "scp_id": "SCP-049",
  "title": "SCP-049: The Plague Doctor",
  "ai_generated": { "narration": true, "imagery": true, "tts": true },
  "models_used": {
    "writer": { "provider": "deepseek", "model": "deepseek-chat" },
    "critic": { "provider": "gemini", "model": "gemini-pro" },
    "image": { "provider": "dashscope", "model": "wanx-v1" },
    "tts": { "provider": "dashscope", "model": "sambert-zhichu-v1", "voice": "...", },
    "visual_breakdown": { "provider": "dashscope", "model": "qwen-vl-max" }
  }
}
```

`SourceManifest` (from Story 9.2):
```json
{
  "version": 1,
  "generated_at": "...",
  "run_id": "...",
  "scp_id": "SCP-049",
  "source_url": "https://scp-wiki.wikidot.com/scp-049",
  "author_name": "djkaktus",
  "license": "CC BY-SA 3.0",
  "license_url": "https://creativecommons.org/licenses/by-sa/3.0/",
  "license_chain": [{ "component": "SCP article text", "source_url": "...", "author_name": "...", "license": "..." }]
}
```

### NFR-L1 enforcement strategy

The service-layer guard in `AcknowledgeMetadata` is the single enforcement point:
```go
if run.Stage != domain.StageMetadataAck || run.Status != domain.StatusWaiting {
    return nil, fmt.Errorf("acknowledge metadata: run is not awaiting metadata acknowledgment: %w", domain.ErrConflict)
}
```
There is no other code path that calls `MarkComplete`. Do NOT add any "shortcut" or admin endpoint
that bypasses this check.

### References

- State machine (StageMetadataAck, EventApprove, StageComplete): [internal/pipeline/engine.go](../../internal/pipeline/engine.go)
- `IsHITLStage` (already includes StageMetadataAck): [internal/pipeline/engine.go](../../internal/pipeline/engine.go)
- `domain.Run`, `domain.StageMetadataAck`, `domain.StageComplete`: [internal/domain/types.go](../../internal/domain/types.go)
- `MetadataBundle`, `SourceManifest` (from Story 9.2): [internal/domain/compliance.go](../../internal/domain/compliance.go)
- `RunStore` interface (to extend): [internal/service/run_service.go](../../internal/service/run_service.go)
- `db.RunStore` existing methods pattern: [internal/db/run_store.go](../../internal/db/run_store.go)
- `mapDomainError`, `writeDomainError`, `writeJSON`: [internal/api/response.go](../../internal/api/response.go)
- `RegisterRoutes`, `Dependencies`, `NewDependencies`: [internal/api/routes.go](../../internal/api/routes.go)
- Handler pattern (Pick endpoint): [internal/api/handler_character.go](../../internal/api/handler_character.go)
- Character pick service pattern (mirror for AcknowledgeMetadata): [internal/service/character_service.go](../../internal/service/character_service.go)
- `ProductionShell` dispatch block (lines 299–358): [web/src/components/shells/ProductionShell.tsx](../../web/src/components/shells/ProductionShell.tsx)
- `useNewRunCoordinator`: [web/src/components/production/useNewRunCoordinator.ts](../../web/src/components/production/useNewRunCoordinator.ts)
- `InlineConfirmPanel`: [web/src/components/shared/InlineConfirmPanel.tsx](../../web/src/components/shared/InlineConfirmPanel.tsx)
- `BatchReview.tsx` (mutation + invalidation pattern): [web/src/components/production/BatchReview.tsx](../../web/src/components/production/BatchReview.tsx)
- `queryKeys.ts`: [web/src/lib/queryKeys.ts](../../web/src/lib/queryKeys.ts)
- `apiClient.ts` (apiFetch pattern): [web/src/lib/apiClient.ts](../../web/src/lib/apiClient.ts)
- NFR-L1 (no bypass path): [_bmad-output/planning-artifacts/architecture.md](../_bmad-output/planning-artifacts/architecture.md)
- FR23 (gates ready-for-upload on operator ack): [_bmad-output/planning-artifacts/epics.md](../_bmad-output/planning-artifacts/epics.md)
- UX completion reward (thumbnail + 5s autoplay): [_bmad-output/planning-artifacts/ux-design-specification.md](../_bmad-output/planning-artifacts/ux-design-specification.md)
- Story 9.2 (metadata.json/manifest.json schema): [9-2-metadata-attribution-bundle.md](./9-2-metadata-attribution-bundle.md)

## Story Status

**Status:** `done`

### Acceptance Criteria Verification

| AC | Description | Status |
|----|-------------|--------|
| AC-1 | AcknowledgeMetadata service + DB transition | ✅ |
| AC-2 | `POST /api/runs/{id}/metadata/ack` endpoint | ✅ |
| AC-3 | Artifact serving endpoints (video, metadata, manifest) | ✅ |
| AC-4 | ComplianceGate UI component | ✅ |
| AC-5 | CompletionReward UI component | ✅ |

## Dev Agent Record

### Agent Model Used

deepseek-reasoner

### Debug Log References

- **Bugfix:** Service test functions were nested inside `TestRunService_Resume_ForwardsForceFlag` causing syntax error; moved to package-level
- **Bugfix:** `fakeRunStore` in `hitl_service_test.go` (shared by `scene_service_test.go`) missing `MarkComplete` method; added stub
- **Bugfix:** `CompletionReward.test.tsx` missing `NewRunCoordinatorProvider` wrapper; added `renderCompletionReward` helper

### Completion Notes List

All 9 tasks completed and verified:
1. **DB layer** — `MarkComplete` added to `RunStore` interface + `db.RunStore` implementation
2. **Service** — `AcknowledgeMetadata` with stage/status validation guard
3. **API handler** — `POST /api/runs/{id}/metadata/ack` with 200/404/409 responses
4. **Artifact handlers** — `GET /api/runs/{id}/video`, `/metadata`, `/manifest` with `http.ServeContent`
5. **Routes** — All 4 endpoints registered in `RegisterRoutes`
6. **Web API client** — `acknowledgeMetadata`, `fetchRunMetadata`, `fetchRunManifest` + Zod schemas + query keys
7. **ComplianceGate** — 8-item checklist + 5s video auto-stop + Finalize button
8. **CompletionReward** — Metadata summary table + "Start Next SCP" button
9. **ProductionShell** — Stage dispatch for `metadata_ack` and `complete` stages

### File List

**New files:**
- `internal/api/handler_artifacts.go` — Artifact serving handler (Video, Metadata, Manifest)
- `internal/api/handler_artifacts_test.go` — 6 tests for artifact serving (200, 404)
- `web/src/components/production/ComplianceGate.tsx` — Compliance gate UI component
- `web/src/components/production/ComplianceGate.test.tsx` — 5 tests (renders, checklist, finalize)
- `web/src/components/production/CompletionReward.tsx` — Completion reward UI component
- `web/src/components/production/CompletionReward.test.tsx` — 4 tests (renders, summary, button)

**Modified files:**
- `internal/service/run_service.go` — Added `MarkComplete` to `RunStore` interface; added `AcknowledgeMetadata` method
- `internal/db/run_store.go` — Added `MarkComplete` implementation (UPDATE stage='complete', status='completed')
- `internal/api/handler_run.go` — Added `AcknowledgeMetadata` handler method
- `internal/api/routes.go` — Added `Artifacts` to `Dependencies`; registered 4 routes
- `web/src/contracts/runContracts.ts` — Added Zod schemas: `metadataBundleSchema`, `sourceManifestSchema`
- `web/src/lib/apiClient.ts` — Added `acknowledgeMetadata`, `fetchRunMetadata`, `fetchRunManifest`
- `web/src/lib/queryKeys.ts` — Added `metadata` and `manifest` query keys
- `web/src/components/shells/ProductionShell.tsx` — Added ComplianceGate and CompletionReward dispatch
- `internal/service/run_service_test.go` — Added 3 tests for AcknowledgeMetadata; fixed syntax error
- `internal/service/hitl_service_test.go` — Added `MarkComplete` stub to `fakeRunStore`
