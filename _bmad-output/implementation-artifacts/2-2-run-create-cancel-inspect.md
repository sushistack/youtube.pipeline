# Story 2.2: Run Create, Cancel & Inspect

Status: done

## Story

As an operator,
I want to create, cancel, and inspect pipeline runs,
so that I can manage the lifecycle of video production.

## Acceptance Criteria

1. **AC-CREATE:** `pipeline create scp-049` inserts a new row in `runs` with `id=scp-049-run-1`, `scp_id=049`, `stage=pending`, `status=pending`, and creates output dir `{cfg.OutputDir}/scp-049-run-1/`.

2. **AC-SEQID:** Sequential numbering is per-SCP-ID. If `scp-049-run-1` and `scp-049-run-2` exist, the next run is `scp-049-run-3`. Different SCP IDs have independent sequences.

3. **AC-CANCEL:** `pipeline cancel scp-049-run-1` sets `status=cancelled` for a `running` or `waiting` run. Any in-flight stage row (if implemented) is marked `failed`. Cancelling an already-terminal run returns error `ErrConflict`.

4. **AC-STATUS-ALL:** `pipeline status` prints all runs with `id`, `stage`, `status`, `created_at`, `updated_at`.

5. **AC-STATUS-ONE:** `pipeline status scp-049-run-1` prints full run details and stage-by-stage progression.

6. **AC-REST-SKELETON:** `internal/api/routes.go` exists with `RegisterRoutes()`. Middleware chain applied via `Chain()`: `WithRequestID`, `WithRecover`, `WithCORS`, `WithRequestLog`. Pipeline lifecycle endpoints registered: `POST /api/runs`, `GET /api/runs`, `GET /api/runs/{id}`, `GET /api/runs/{id}/status`, `POST /api/runs/{id}/cancel`, `POST /api/runs/{id}/resume`.

7. **AC-ENVELOPE:** All API responses use `{"version": 1, "data": ...}` or `{"version": 1, "error": {...}}` via `writeJSON`/`writeError` — never raw `json.Encode`.

8. **AC-SERVE:** `pipeline serve` starts HTTP server bound to `127.0.0.1:8080` only (NFR-S2). `--dev` flag proxies unknown paths to `http://localhost:5173` (Vite dev server).

9. **AC-UPDATED-AT-TRIGGER:** Migration `002` adds an `AFTER UPDATE` trigger on `runs` to auto-set `updated_at=datetime('now')`. Resolves deferred work from Story 1.2.

10. **AC-RUN-STATUS-TYPE:** `domain.Run.Status` field changes from `string` to `domain.Status`.

11. **AC-TESTS:** Integration tests cover: run creation with correct ID + DB row + output dir; sequential ID increment across multiple runs; cancel state transition; status output. Handler tests cover: happy path + 404 + 409 conflict.

---

## Tasks / Subtasks

- [x] **T1: Migration 002 — updated_at trigger** (AC: #9)
  - [x] Create `migrations/002_updated_at_trigger.sql` — `CREATE TRIGGER IF NOT EXISTS runs_updated_at AFTER UPDATE ON runs BEGIN UPDATE runs SET updated_at = datetime('now') WHERE id = NEW.id; END;`
  - [x] Verify `db.Migrate(db)` picks up migration 002 correctly (version number = 2)

- [x] **T2: domain.Run.Status type change** (AC: #10)
  - [x] In `internal/domain/types.go`, change `Run.Status` field from `string` to `domain.Status`
  - [x] Update `internal/testutil/contract_test.go` comparison: `run.Status != "pending"` → `run.Status != domain.StatusPending`
  - [x] Verify `go build ./...` passes (no compile errors from the type change)

- [x] **T3: db/run_store.go — RunStore CRUD** (AC: #1, #2, #3)
  - [x] Create `internal/db/run_store.go` implementing `service.RunStore` interface (defined in T4 first, or define interface here as placeholder)
  - [x] `Create(ctx, scpID, outputDir string) (*domain.Run, error)` — in a single SQLite transaction: compute next seq = `SELECT COUNT(*)+1 FROM runs WHERE scp_id=?`, construct ID=`scp-{scpID}-run-{seq}`, INSERT run row, create output dir via `os.MkdirAll`
  - [x] `Get(ctx, id string) (*domain.Run, error)` — SELECT * by ID; return `fmt.Errorf("get run: %w", domain.ErrNotFound)` if no row
  - [x] `List(ctx) ([]*domain.Run, error)` — SELECT * ORDER BY created_at ASC
  - [x] `Cancel(ctx, id string) error` — UPDATE status=cancelled WHERE id=? AND status IN ('running','waiting'); if 0 rows affected → `domain.ErrConflict`
  - [x] Create `internal/db/run_store_test.go` — integration tests using `testutil.NewTestDB(t)` (defined in T5): create sequential IDs across 3 runs same scp_id, cancel flow, get-not-found

- [x] **T4: service/run_service.go — RunStore interface + business logic** (AC: #1, #3)
  - [x] Create `internal/service/run_service.go`
  - [x] Define `RunStore` interface in `service/` package (per architecture interface-in-consumer rule): `Create`, `Get`, `List`, `Cancel` methods
  - [x] `RunService` struct with `store RunStore` field
  - [x] `NewRunService(store RunStore) *RunService` constructor
  - [x] `Create(ctx, scpID, outputDir string) (*domain.Run, error)` — delegates to store, wraps errors
  - [x] `Cancel(ctx, id string) error` — delegates to store; get run first to verify it exists (returns ErrNotFound), then cancel
  - [x] `List(ctx) ([]*domain.Run, error)` — delegates to store
  - [x] `Get(ctx, id string) (*domain.Run, error)` — delegates to store
  - [x] Create `internal/service/run_service_test.go`

- [x] **T5: testutil — NewTestDB, ReadJSON, CaptureLog** (AC: #11)
  - [x] Create `internal/testutil/db.go` — `func NewTestDB(t testing.TB) *sql.DB` using `t.TempDir()` + `db.Migrate()` + `t.Cleanup(db.Close)` (model after `LoadRunStateFixture` in fixture.go but without seeding)
  - [x] Create `internal/testutil/response.go` — `func ReadJSON[T any](t testing.TB, body io.Reader) T`
  - [x] Create `internal/testutil/slog.go` — `func CaptureLog(t testing.TB) (*slog.Logger, *bytes.Buffer)`

- [x] **T6: internal/api — middleware, response, routes** (AC: #6, #7)
  - [x] Create `internal/api/middleware.go`:
    - `type Middleware func(http.Handler) http.Handler`
    - `func Chain(h http.Handler, mws ...Middleware) http.Handler` — applies in reverse order
    - `func WithRequestID(next http.Handler) http.Handler` — UUID in context + `X-Request-ID` header; use `crypto/rand` UUID v4 (see notes below)
    - `func WithRecover(next http.Handler) http.Handler` — recover() → 500 + `slog.Error`
    - `func WithCORS(next http.Handler) http.Handler` — permissive for localhost only
    - `func WithRequestLog(logger *slog.Logger) Middleware` — structured log per request
  - [x] Create `internal/api/response.go`:
    - `type apiResponse struct { Version int "json:\"version\""; Data any "json:\"data,omitempty\""; Error *apiError "json:\"error,omitempty\"" }`
    - `type apiError struct { Code string; Message string; Recoverable bool }`
    - `func writeJSON(w http.ResponseWriter, status int, data any)` — sets Content-Type, writes envelope
    - `func writeError(w http.ResponseWriter, err error)` — calls `domain.Classify(err)`, writes error envelope
  - [x] Create `internal/api/routes.go` — `RegisterRoutes(mux *http.ServeMux, deps *Dependencies)` with Dependencies struct and 6 lifecycle routes

- [x] **T7: internal/api/handler_run.go** (AC: #6, #7)
  - [x] `RunHandler` struct with `svc service.RunService` and `logger *slog.Logger`
  - [x] `Create(w, r)` — decode `{"scp_id": "..."}`, call svc.Create, return 201 + run JSON
  - [x] `List(w, r)` — call svc.List, return 200 + `{"items": [...], "total": N}`
  - [x] `Get(w, r)` — `r.PathValue("id")`, call svc.Get, return 200 or 404
  - [x] `Status(w, r)` — same as Get for now (stub: return same run detail)
  - [x] `Cancel(w, r)` — call svc.Cancel, return 200 or 409
  - [x] `Resume(w, r)` — stub: return 501 Not Implemented (Epic 2.3 implements real logic)
  - [x] Create `internal/api/handler_run_test.go` using `httptest.NewRequest` + `httptest.NewRecorder` pattern

- [x] **T8: internal/api/spa.go** (AC: #8)
  - [x] Create `internal/api/spa.go` — `spaHandler(fsys fs.FS) http.Handler` that serves `index.html` for all unmatched paths
  - [x] Create `internal/web/embed.go` — `//go:embed all:dist` with `var WebFS embed.FS` (pre-existing, already correct)
  - [x] `internal/web/dist/` — pre-existing React build output used as-is (no placeholder needed)

- [x] **T9: pipeline serve command** (AC: #8)
  - [x] Create `cmd/pipeline/serve.go` — Cobra command `serve`, flags: `--port 8080`, `--dev`
  - [x] Bind ONLY to `127.0.0.1:{port}` — never `0.0.0.0`
  - [x] Open DB (`db.OpenDB(cfg.DBPath)`), create `RunService`, create `RunHandler`, call `RegisterRoutes`
  - [x] Without `--dev`: serve embedded `web.WebFS` as SPA catch-all
  - [x] With `--dev`: reverse proxy all non-`/api/` paths to `http://localhost:5173` using `httputil.ReverseProxy`
  - [x] Register command in `cmd/pipeline/main.go`

- [x] **T10: CLI create / cancel / status commands** (AC: #1, #2, #3, #4, #5)
  - [x] Create `cmd/pipeline/create.go` — Cobra command `create <scp-id>`, opens DB, calls `RunService.Create`, renders result
  - [x] Create `cmd/pipeline/cancel.go` — Cobra command `cancel <run-id>`, calls `RunService.Cancel`
  - [x] Create `cmd/pipeline/status.go` — Cobra command `status [run-id]`, calls `RunService.List` or `RunService.Get`
  - [x] Add output types to `cmd/pipeline/render.go`: `RunOutput`, `RunListOutput`; add `renderRun`, `renderRunList` to `HumanRenderer`
  - [x] Register all three commands in `cmd/pipeline/main.go`

- [x] **T11: Contract fixtures** (AC: #11)
  - [x] Create `testdata/contracts/run.detail.response.json` — full API envelope `{"version": 1, "data": {run object}}`
  - [x] Create `testdata/contracts/run.list.response.json` — `{"version": 1, "data": {"items": [...], "total": 1}}`
  - [x] Add contract validation tests in `internal/api/handler_run_test.go`

- [x] **T12: Verify green build** (AC: all)
  - [x] `go build ./cmd/pipeline/...` passes
  - [x] `go test ./...` passes — zero regressions on all existing tests
  - [x] `make lint-layers` passes

### Review Findings

- [x] [Review][Patch] **[HIGH] API accepts client-controlled `output_dir`** — Fixed: `createRequest` no longer accepts `output_dir`; `RunHandler.outputDir` is injected from `cfg.OutputDir` via `NewDependencies`. [internal/api/handler_run.go, routes.go, cmd/pipeline/serve.go]
- [x] [Review][Patch] **[HIGH] Raw internal errors leaked to HTTP clients** — Fixed: `writeDomainError` now calls `clientMessage(status, code)` returning a canonical safe string per domain code; internal details stay in server logs only. [internal/api/response.go]
- [x] [Review][Patch] **[HIGH] Permissive CORS `*` on localhost enables DNS rebinding** — Fixed: added `WithHostAllowlist` middleware rejecting non-localhost Host headers. Applied in the chain before `WithCORS`. [internal/api/middleware.go, routes.go]
- [x] [Review][Patch] **[HIGH] scpID with `../` or `/` escapes output dir** — Fixed: `RunService.Create` validates against `^[A-Za-z0-9_-]+$`; rejects path chars / control bytes with `ErrValidation`. Tests added for 6 malicious IDs. [internal/service/run_service.go, run_service_test.go]
- [x] [Review][Patch] **[MED] Orphan DB row if `os.MkdirAll` fails after tx commit** — Fixed: `createOnce` mkdirs the per-run directory BEFORE tx.Commit; mkdir failure triggers tx.Rollback. If commit itself fails (rare), the directory is removed best-effort. [internal/db/run_store.go]
- [x] [Review][Patch] **[MED] Concurrent `Create` race on `COUNT(*)+1`** — Fixed: added `maxCreateRetries=3` retry loop using `isPKCollision()` detector. Two concurrent Creates with same scpID now retry cleanly instead of surfacing a raw PK-collision error. [internal/db/run_store.go]
- [x] [Review][Patch] **[MED] Migration 002 trigger is recursion-fragile** — Fixed: added `WHEN OLD.updated_at IS NEW.updated_at` guard so self-recursion becomes a no-op under any `recursive_triggers` setting. [migrations/002_updated_at_trigger.sql]
- [x] [Review][Patch] **[MED] `WithRecover` re-writes headers after handler wrote them** — Fixed: `responseWriter` now tracks a `wrote` flag via both `WriteHeader` and `Write`. `WithRecover` skips the 500 envelope when the handler already produced a response. [internal/api/middleware.go]
- [x] [Review][Patch] **[LOW] `spaHandler` string-concatenates user path** — Fixed: rewrote using `fs.Sub(fsys, "dist")` + `path.Clean` + strip leading slash; traversal attempts return index.html fallback instead of touching the FS. [internal/api/spa.go]
- [x] [Review][Patch] **[LOW] `serve` returns bogus error on rare clean-shutdown path** — Fixed: goroutine only sends non-`ErrServerClosed` errors to `errCh`; select returns `nil` when the channel is empty, avoiding `"server error: <nil>"`. [cmd/pipeline/serve.go]
- [x] [Review][Patch] **[LOW] `Cancel` non-atomic exists-then-update** — Fixed: Cancel issues the UPDATE first; if RowsAffected=0 does a disambiguating Get to return ErrNotFound vs ErrConflict correctly. [internal/db/run_store.go]
- [x] [Review][Defer] **Vite proxy error responses bypass request-id / slog** — deferred, cosmetic (addressed when web UI stabilizes in Epic 6). [cmd/pipeline/serve.go:75-77]
- [x] [Review][Defer] **`newRequestID` entropy-failure fallback returns `"unknown"` — collision risk** — deferred, theoretical (crypto/rand has never failed in practice on Linux). [internal/api/middleware.go:100-107]

---

## Dev Notes

### Run ID Sequential Numbering

**Architecture spec:** `SELECT COUNT(*) + 1 FROM runs WHERE scp_id = ?`

This is NOT a global counter. Each SCP ID has its own independent sequence.

```go
// in run_store.go Create():
var seq int
err := tx.QueryRowContext(ctx, "SELECT COUNT(*)+1 FROM runs WHERE scp_id=?", scpID).Scan(&seq)
id := fmt.Sprintf("scp-%s-run-%d", scpID, seq)
```

No gap-free guarantee needed — if a run is deleted (not a current feature), the next ID would reuse a number. This is acceptable for a single-operator tool. **Do NOT use UUIDs or ULIDs.**

### domain.Run.Status Type Change

Current: `Status string`
Required: `Status domain.Status`

This is a type change — Go will catch all places where `string` was assigned directly. Common places to fix:
- `contract_test.go` line 28: `run.Status != "pending"` → `run.Status != domain.StatusPending`
- Any DB scan that scans into `run.Status` — since `domain.Status` is a `type Status string`, `rows.Scan(&run.Status)` works without change
- JSON marshaling is unchanged — `domain.Status` is a string type, encodes as string

### Output Directory Creation

Created in `db/run_store.go Create()` **after** the INSERT commits successfully:

```go
// After tx.Commit():
outDir := filepath.Join(outputDir, id)
if err := os.MkdirAll(outDir, 0755); err != nil {
    return nil, fmt.Errorf("create output dir %s: %w", outDir, err)
}
```

The `outputDir` (base output path) is passed in from `cfg.OutputDir`. The run-specific dir is `{cfg.OutputDir}/{run_id}/`.

### Interface Definition Rule

**`RunStore` interface is defined in `service/` (consumer), implemented in `db/` (provider).** This is the architecture's mandatory convention — interfaces live in the consuming package.

```go
// internal/service/run_service.go
type RunStore interface {
    Create(ctx context.Context, scpID, outputDir string) (*domain.Run, error)
    Get(ctx context.Context, id string) (*domain.Run, error)
    List(ctx context.Context) ([]*domain.Run, error)
    Cancel(ctx context.Context, id string) error
}
```

### WithRequestID — UUID Generation

No `github.com/google/uuid` in go.mod. Implement UUID v4 with `crypto/rand`:

```go
func newRequestID() string {
    var b [16]byte
    if _, err := rand.Read(b[:]); err != nil {
        return "unknown"
    }
    b[6] = (b[6] & 0x0f) | 0x40 // version 4
    b[8] = (b[8] & 0x3f) | 0x80 // variant bits
    return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
```

Do NOT add `github.com/google/uuid` — keep dependencies minimal for a single-operator CLI tool.

### writeError — Uses domain.Classify

`response.go` uses the existing `domain.Classify()` function (NOT the architecture's handwritten `mapDomainError()`). The actual implementation is:

```go
func writeError(w http.ResponseWriter, err error) {
    status, code, recoverable := domain.Classify(err)
    writeJSON(w, status, nil) // overridden below
    // Actually, write error envelope:
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(apiResponse{
        Version: 1,
        Error: &apiError{Code: code, Message: err.Error(), Recoverable: recoverable},
    })
}
```

`domain.Classify()` is at `internal/domain/errors.go:28`. **Do not re-implement mapDomainError() — it already exists as Classify().**

### Go http.ServeMux Path Patterns (Go 1.22+)

Go 1.25 (our module version) supports `GET /api/runs/{id}` pattern syntax and `r.PathValue("id")`:

```go
mux.HandleFunc("GET /api/runs/{id}", h.run.Get)
// In handler:
id := r.PathValue("id")
```

No need for third-party routers. The `Chain()` applies middleware to a sub-mux:

```go
api := http.NewServeMux()
apiWrapped := Chain(api, WithRequestID, WithRecover, WithCORS, WithRequestLog(logger))
mux.Handle("/api/", apiWrapped)
```

### pipeline serve — localhost binding

```go
srv := &http.Server{
    Addr:    fmt.Sprintf("127.0.0.1:%d", port),
    Handler: mux,
}
```

**Never `":8080"` or `"0.0.0.0:8080"`.** NFR-S2 requires localhost-only.

### pipeline serve — --dev Reverse Proxy

```go
import "net/http/httputil"

target, _ := url.Parse("http://localhost:5173")
proxy := httputil.NewSingleHostReverseProxy(target)
// Register as catch-all for non-/api/ paths:
mux.Handle("/", proxy)
```

In production mode (no --dev), serve `web.WebFS` via spaHandler.

### web/embed.go — Placeholder for Epic 6

Since `web/dist/` doesn't exist yet, create a minimal placeholder:

```
internal/web/dist/index.html  ← minimal HTML placeholder
internal/web/embed.go         ← //go:embed all:dist
```

`spaHandler` tries `fs.Open("dist/index.html")` — if it returns a placeholder page, that's fine. Epic 6 will build the real SPA into this directory.

### Cancel Semantics

From epics AC: "실행 중 런 status=cancelled, 진행 중 스테이지 failed"

In Story 2.2, `stage_runs` table doesn't exist yet (Epic 2.4 adds per-stage rows). The cancel just updates `runs.status=cancelled`. The "진행 중 스테이지 failed" part becomes relevant in Story 2.3 when the stage execution engine is wired. For now: cancel only affects `runs` table.

### Import Rules (make lint-layers)

```
api       → service → domain
                    → db
                    → pipeline (for Runner interface type)
                    → clock
db        → domain
service   → domain, db, pipeline
api       → service, domain
```

- `api/` imports `service/`, `domain/` — ✓
- `service/` imports `domain/`, `pipeline/` (for `pipeline.Runner` type reference) — ✓
- `db/` imports `domain/` only — ✓
- `domain/` imports nothing from internal — ✓

New packages `service/` and `api/` are NOT in the existing layer lint rules. Check `scripts/lintlayers/main.go` and add them if needed to avoid false positives.

### testutil.NewTestDB vs LoadRunStateFixture

`testutil.NewTestDB(t)` = clean DB with migrations, no seed data. Use it for run_store tests.
`testutil.LoadRunStateFixture(t, name)` = NewTestDB + seed SQL. Use it for higher-level integration tests with pre-existing data.

`NewTestDB` is implemented in new `internal/testutil/db.go` following `LoadRunStateFixture`'s WAL+foreign_keys setup pattern.

### render.go additions

Add to `render.go` (do NOT create a new file — keep CLI output types co-located):

```go
// RunOutput is the structured output for create/get single run.
type RunOutput struct {
    ID        string `json:"id"`
    SCPID     string `json:"scp_id"`
    Stage     string `json:"stage"`
    Status    string `json:"status"`
    CreatedAt string `json:"created_at"`
    OutputDir string `json:"output_dir"`
}

// RunListOutput is the structured output for `pipeline status`.
type RunListOutput struct {
    Runs  []RunOutput `json:"runs"`
    Total int         `json:"total"`
}
```

Human renderer: table format showing ID, stage, status, created_at. Use fixed-width columns.

### Project Structure After This Story

```
internal/
  domain/
    types.go           # MODIFIED — Run.Status: string → domain.Status
  db/
    run_store.go       # NEW
    run_store_test.go  # NEW
  service/
    run_service.go     # NEW — RunStore interface + RunService
    run_service_test.go # NEW
  api/
    middleware.go      # NEW
    middleware_test.go # NEW
    response.go        # NEW
    response_test.go   # NEW
    routes.go          # NEW
    handler_run.go     # NEW
    handler_run_test.go # NEW
    spa.go             # NEW
  web/
    embed.go           # NEW
    dist/index.html    # NEW (placeholder)
  testutil/
    db.go              # NEW — NewTestDB
    response.go        # NEW — ReadJSON[T]
    slog.go            # NEW — CaptureLog
    contract_test.go   # MODIFIED — use domain.StatusPending
  pipeline/
    engine.go          # EXISTING — no change
    runner.go          # EXISTING — no change
migrations/
  001_init.sql         # EXISTING — no change
  002_updated_at_trigger.sql # NEW
cmd/pipeline/
  main.go              # MODIFIED — add create, cancel, status, serve
  render.go            # MODIFIED — add RunOutput, RunListOutput types + renderers
  create.go            # NEW
  cancel.go            # NEW
  status.go            # NEW
  serve.go             # NEW
testdata/
  contracts/
    run.detail.response.json  # NEW
    run.list.response.json    # NEW
```

### Critical Constraints

- **No testify, no gomock.** Use `testing` + `testutil.AssertEqual[T]` + `testutil.ReadJSON[T]`.
- **No UUID library.** Use `crypto/rand` UUID v4 for `WithRequestID`.
- **No 0.0.0.0 bind.** `pipeline serve` MUST bind `127.0.0.1:{port}`.
- **RunStore interface in service/ not db/.** Architecture's consumer-defines-interface rule.
- **Cancel only affects status column.** Per-stage failure marking is Story 2.3 scope.
- **Resume handler is a stub (501).** Real resume logic is Story 2.3.
- **snake_case everywhere in JSON.** `json:"run_id"`, not `json:"runId"`.
- **Module path:** `github.com/sushistack/youtube.pipeline`.
- **CGO_ENABLED=0.** All tests must compile and run without CGO.
- **Call `testutil.BlockExternalHTTP(t)` in all new test files.**

### Deferred Work Resolved

- **From Story 1.2:** "`runs.updated_at` column has no AFTER UPDATE trigger." → Resolved by T1 (migration 002).

### Deferred Work Awareness

- **From Story 2.1:** "The caller in Story 2.2 will wrap: `next, err := NextStage(run.Stage, event); if err != nil { return fmt.Errorf(\"advance run %s: %w\", runID, err) }`" → This pattern is used in engine.Advance() wiring in Story 2.3. Not directly needed in 2.2 but be aware.
- **stage_runs table / per-stage failure:** Cancel sets run status=cancelled. Stage-level failure marking deferred to Story 2.3.
- **Resume endpoint stub returns 501.** Full resume logic is Story 2.3.

### References

- Epic 2 scope: [epics.md:378-399](_bmad-output/planning-artifacts/epics.md)
- Story 2.2 AC (BDD): [epics.md:937-966](_bmad-output/planning-artifacts/epics.md)
- Run ID format: [architecture.md:1007-1011](_bmad-output/planning-artifacts/architecture.md)
- REST API skeleton + middleware: [architecture.md:1112-1163](_bmad-output/planning-artifacts/architecture.md)
- Response envelope: [architecture.md:1176-1203](_bmad-output/planning-artifacts/architecture.md)
- Interface definition rule: [architecture.md:1079-1084](_bmad-output/planning-artifacts/architecture.md)
- Import direction rules: [architecture.md:1059-1077](_bmad-output/planning-artifacts/architecture.md)
- Project structure: [architecture.md:1501-1623](_bmad-output/planning-artifacts/architecture.md)
- Go test patterns: [architecture.md:1414-1432](_bmad-output/planning-artifacts/architecture.md)
- NFR-S2 localhost binding: implicitly from architecture.md:426 serve command notes
- domain.Classify: [internal/domain/errors.go:28-34](../../internal/domain/errors.go)
- domain.Run struct: [internal/domain/types.go:111-128](../../internal/domain/types.go)
- domain.Status constants: [internal/domain/types.go:79-109](../../internal/domain/types.go)
- pipeline.Runner interface: [internal/pipeline/runner.go](../../internal/pipeline/runner.go)
- pipeline.IsHITLStage: [internal/pipeline/engine.go:93-100](../../internal/pipeline/engine.go)
- testutil.LoadRunStateFixture: [internal/testutil/fixture.go:28-64](../../internal/testutil/fixture.go)
- testutil.BlockExternalHTTP: [internal/testutil/nohttp.go](../../internal/testutil/nohttp.go)
- testutil.AssertEqual: [internal/testutil/assert.go](../../internal/testutil/assert.go)
- contract_test.go: [internal/testutil/contract_test.go](../../internal/testutil/contract_test.go)
- Previous story (2.1): [2-1-state-machine-core-stage-transitions.md](2-1-state-machine-core-stage-transitions.md)
- Deferred work registry: [deferred-work.md](deferred-work.md)
- Layer lint script: [scripts/lintlayers/main.go](../../scripts/lintlayers/main.go)

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

None.

### Completion Notes List

- `migrations/002_updated_at_trigger.sql`: AFTER UPDATE trigger on `runs` table. Resolves Story 1.2 deferred work. Updated sqlite_test.go to expect user_version=2.
- `internal/domain/types.go`: `Run.Status` field changed from `string` to `domain.Status`. JSON marshaling unchanged (domain.Status is a named string type).
- `internal/testutil/contract_test.go`: Updated Status comparison to use `domain.StatusPending` typed constant.
- `internal/db/run_store.go`: `RunStore` with Create (transaction + sequential ID + output dir), Get, List, Cancel. Sequential ID via `SELECT COUNT(*)+1 FROM runs WHERE scp_id=?`. Cancel uses `status IN ('running','waiting')` guard; returns ErrConflict on 0 rows affected.
- `internal/db/run_store_test.go`: 9 integration tests covering ID format, sequential per-SCP-ID, output dir creation, Get-not-found, List ordering, cancel-running, cancel-terminal-conflict, cancel-not-found.
- `internal/testutil/db.go`: `NewTestDB(t)` — clean SQLite DB with all migrations applied.
- `internal/testutil/response.go`: `ReadJSON[T](t, r)` generic JSON decoder helper.
- `internal/testutil/slog.go`: `CaptureLog(t)` returns a JSON slog.Logger writing to *bytes.Buffer.
- `internal/service/run_service.go`: `RunStore` interface (consumer-side per architecture rule) + `RunService` with Create/Get/List/Cancel. Validates empty scpID returns ErrValidation.
- `internal/service/run_service_test.go`: 5 tests covering create, empty-scpid validation, get-not-found, cancel-conflict, list-empty.
- `internal/api/middleware.go`: `Chain()`, `WithRequestID` (crypto/rand UUID v4), `WithRecover` (panic→500), `WithCORS` (permissive localhost), `WithRequestLog` (slog structured). No external UUID library added.
- `internal/api/response.go`: `writeJSON`, `writeError`, `writeDomainError` (uses domain.Classify). All responses use `{"version":1,...}` envelope.
- `internal/api/routes.go`: `RegisterRoutes()` + `Dependencies` struct + `NewDependencies()`. Registers 6 pipeline lifecycle endpoints under `/api/` with full middleware chain.
- `internal/api/handler_run.go`: `RunHandler` with Create (201), List (200), Get (200/404), Status (200/404 stub), Cancel (200/409), Resume (501 stub). All use writeJSON/writeDomainError — no raw json.Encode.
- `internal/api/middleware_test.go`: 5 tests for Chain ordering, WithRequestID header, WithRecover panic, WithCORS headers/OPTIONS.
- `internal/api/response_test.go`: 4 tests for writeJSON envelope, writeDomainError not-found/conflict/validation.
- `internal/api/handler_run_test.go`: 7 handler tests + 2 contract fixture validation tests.
- `internal/api/spa.go`: `spaHandler(fs.FS)` with index.html fallback for SPA client-side routing.
- `cmd/pipeline/serve.go`: `pipeline serve --port 8080 --dev`. Binds to `127.0.0.1:{port}` only (NFR-S2). `--dev` uses `httputil.ReverseProxy` to Vite at localhost:5173. Graceful shutdown on SIGINT/SIGTERM.
- `cmd/pipeline/create.go`: `pipeline create <scp-id>` command.
- `cmd/pipeline/cancel.go`: `pipeline cancel <run-id>` command.
- `cmd/pipeline/status.go`: `pipeline status [run-id]` — list all runs or single run detail.
- `cmd/pipeline/render.go`: Added `RunOutput`, `RunListOutput`, `CancelOutput` types; `renderRun`, `renderRunList`, `renderCancel` on HumanRenderer.
- `cmd/pipeline/main.go`: Registered create, cancel, status, serve commands.
- `testdata/contracts/run.detail.response.json`: API envelope contract for single run response.
- `testdata/contracts/run.list.response.json`: API envelope contract for run list response.
- All tests pass: `go test ./...` — zero regressions. `make lint-layers` — OK. `go build ./...` — clean.

### Change Log

- 2026-04-17: Story 2.2 implemented — run lifecycle CLI + REST API skeleton, db/service/api layers, testutil helpers, migration 002

### File List

- migrations/002_updated_at_trigger.sql (new)
- internal/domain/types.go (modified — Run.Status string→domain.Status)
- internal/testutil/contract_test.go (modified — Status comparison)
- internal/testutil/db.go (new)
- internal/testutil/response.go (new)
- internal/testutil/slog.go (new)
- internal/db/run_store.go (new)
- internal/db/run_store_test.go (new)
- internal/db/sqlite_test.go (modified — user_version 1→2)
- internal/service/run_service.go (new)
- internal/service/run_service_test.go (new)
- internal/api/middleware.go (new)
- internal/api/middleware_test.go (new)
- internal/api/response.go (new)
- internal/api/response_test.go (new)
- internal/api/routes.go (new)
- internal/api/handler_run.go (new)
- internal/api/handler_run_test.go (new)
- internal/api/spa.go (new)
- cmd/pipeline/create.go (new)
- cmd/pipeline/cancel.go (new)
- cmd/pipeline/status.go (new)
- cmd/pipeline/serve.go (new)
- cmd/pipeline/main.go (modified — add 4 commands)
- cmd/pipeline/render.go (modified — add RunOutput/RunListOutput/CancelOutput types + renderers)
- testdata/contracts/run.detail.response.json (new)
- testdata/contracts/run.list.response.json (new)
