# Story 4.3: Cohen's Kappa Calibration Tracking

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want the system to persist Cohen's kappa calibration snapshots and trend history,
so that I can see whether the Critic is converging toward or drifting away from my standards over a rolling run window.

## Prerequisites

**Story 2.7 is the semantic foundation for this story and must not be rewritten.** The repository already ships the V1 calibration mechanics:

- `internal/service/kappa.go` implements the canonical binary Cohen's kappa helper.
- `internal/db/decision_store.go` already joins `decisions` and `runs` for V1 `KappaPairsForRuns`.
- `internal/service/metrics_service.go` already computes the live rolling-window `critic_calibration` metric used by `pipeline metrics`.

Story 4.3 is therefore **not** permission to invent a second kappa formula, a second join interpretation, or a richer per-scene verdict model. It extends the existing V1 semantics with **SQLite-backed persistence and trend history** so later tuning surfaces can read calibration over time.

Because Epic 8's full decision-writing UI/CLI flows are not implemented yet, this story also needs a **manual refresh trigger**. The least-surprising V1 trigger is the existing `pipeline metrics` command: when the operator asks for metrics, the current calibration snapshot should be computed and persisted from that same rolling window.

## Acceptance Criteria

Unless stated otherwise, new tests follow the project's `TestXxx_CaseName` convention, live beside the code under test, call `testutil.BlockExternalHTTP(t)`, and use inline fakes + `testutil.AssertEqual[T]` / `testutil.AssertFloatNear` (no testify, no gomock). Module path `github.com/sushistack/youtube.pipeline`. CGO_ENABLED=0.

1. **AC-CANONICAL-KAPPA-MATH-REUSE:** `internal/service/kappa.go` remains the one canonical Cohen's kappa implementation for V1 calibration.

   Required rules:
   - Keep the helper in `internal/service/`; do **not** duplicate the formula in `internal/db`, `internal/domain`, `cmd/`, or tests.
   - The top-level comment must explicitly say **"unweighted binary Cohen's kappa (2x2 table)"** so the weighted-vs-unweighted choice is documented for code review.
   - Preserve the current cell mapping:
     - `a`: Critic pass + operator approve
     - `b`: Critic pass + operator reject
     - `c`: Critic fail + operator approve
     - `d`: Critic fail + operator reject
   - Preserve and keep green the existing pure unit tests in `internal/service/kappa_test.go`, especially:
     - `TestCohensKappa_KnownTextbookExample`
     - `TestCohensKappa_Degenerate_AllOneClass`
     - `TestCohensKappa_NegativeAgreement`
   - If the helper is moved or renamed during refactor, the test coverage must move with it unchanged in substance.

2. **AC-MIGRATION-006-CALIBRATION-SNAPSHOTS:** add a new migration `migrations/006_critic_calibration_snapshots.sql` that stores persisted calibration snapshots in SQLite.

   Required table:

   ```sql
   CREATE TABLE critic_calibration_snapshots (
       id                    INTEGER PRIMARY KEY AUTOINCREMENT,
       source_key            TEXT NOT NULL UNIQUE,
       window_size           INTEGER NOT NULL,
       window_count          INTEGER NOT NULL,
       provisional           INTEGER NOT NULL DEFAULT 0,
       calibration_threshold REAL NOT NULL,
       kappa                 REAL,
       reason                TEXT,
       agreement_yes_yes     INTEGER NOT NULL DEFAULT 0,
       disagreement_yes_no   INTEGER NOT NULL DEFAULT 0,
       disagreement_no_yes   INTEGER NOT NULL DEFAULT 0,
       agreement_no_no       INTEGER NOT NULL DEFAULT 0,
       window_start_run_id   TEXT,
       window_end_run_id     TEXT,
       latest_decision_id    INTEGER NOT NULL DEFAULT 0,
       computed_at           TEXT NOT NULL DEFAULT (datetime('now'))
   );

   CREATE INDEX idx_critic_calibration_snapshots_window_computed_at
       ON critic_calibration_snapshots(window_size, computed_at DESC, id DESC);
   ```

   Rules:
   - Use migration number **006**. `005_metrics_indexes.sql` already exists, and the repository already has two `004_*.sql` files. Do not create a third collision.
   - `kappa` is nullable so degenerate / no-pair snapshots can still be persisted with a `reason`.
   - `source_key` is an idempotency key derived from the evaluated input state (see AC-SERVICE-REFRESH). Re-running refresh against unchanged data must update the same logical snapshot, not append duplicates.
   - This table is for **operational metric history**, so it belongs in SQLite alongside `runs` and `decisions`. Do **not** store calibration snapshots in `testdata/` or in Golden fixture manifests; file-backed governance is Story 4.1, DB-backed calibration is Story 4.3.

   Add migration tests proving a fresh DB includes the new table and index after `db.Migrate()`.

3. **AC-DOMAIN-AND-STORE-SURFACE:** add a small domain contract and a dedicated DB store for persisted calibration history.

   Add `internal/domain/calibration.go`:

   ```go
   package domain

   type CriticCalibrationSnapshot struct {
       WindowSize           int      `json:"window_size"`
       WindowCount          int      `json:"window_count"`
       Provisional          bool     `json:"provisional"`
       CalibrationThreshold float64  `json:"calibration_threshold"`
       Kappa                *float64 `json:"kappa,omitempty"`
       Reason               string   `json:"reason,omitempty"`
       AgreementYesYes      int      `json:"agreement_yes_yes"`
       DisagreementYesNo    int      `json:"disagreement_yes_no"`
       DisagreementNoYes    int      `json:"disagreement_no_yes"`
       AgreementNoNo        int      `json:"agreement_no_no"`
       WindowStartRunID     string   `json:"window_start_run_id,omitempty"`
       WindowEndRunID       string   `json:"window_end_run_id,omitempty"`
       LatestDecisionID     int      `json:"latest_decision_id,omitempty"`
       ComputedAt           string   `json:"computed_at"`
   }

   type CriticCalibrationTrendPoint struct {
       ComputedAt  string   `json:"computed_at"`
       WindowCount int      `json:"window_count"`
       Provisional bool     `json:"provisional"`
       Kappa       *float64 `json:"kappa,omitempty"`
       Reason      string   `json:"reason,omitempty"`
   }
   ```

   Add `internal/db/calibration_store.go`:

   ```go
   type CalibrationStore struct {
       db *sql.DB
   }

   func NewCalibrationStore(db *sql.DB) *CalibrationStore

   func (s *CalibrationStore) UpsertCriticCalibrationSnapshot(
       ctx context.Context,
       sourceKey string,
       snap domain.CriticCalibrationSnapshot,
   ) error

   func (s *CalibrationStore) RecentCriticCalibrationTrend(
       ctx context.Context,
       windowSize int,
       limit int,
   ) ([]domain.CriticCalibrationTrendPoint, error)
   ```

   Rules:
   - `RecentCriticCalibrationTrend` validates `windowSize > 0` and `limit > 0`; invalid inputs return `domain.ErrValidation`.
   - Trend points are returned **oldest → newest** so future charts can render directly without client-side reversal.
   - Upsert writes `computed_at` from the incoming snapshot; do not let SQLite clock drift make tests nondeterministic.
   - Add:
     - `TestCalibrationStore_UpsertCriticCalibrationSnapshot_IdempotentBySourceKey`
     - `TestCalibrationStore_RecentCriticCalibrationTrend_OldestFirst`
     - `TestCalibrationStore_RecentCriticCalibrationTrend_Validation`

4. **AC-SERVICE-REFRESH-AND-PROVISIONAL-LOGIC:** add `internal/service/calibration_service.go` to compute and persist the current calibration snapshot from the existing V1 rolling-window mechanics.

   Required surface:

   ```go
   type CalibrationMetricsReader interface {
       RecentCompletedRunsForMetrics(ctx context.Context, window int) ([]db.RunMetricsRow, error)
       KappaPairsForRuns(ctx context.Context, runIDs []string, threshold float64) ([]db.KappaPair, error)
       LatestDecisionIDForRuns(ctx context.Context, runIDs []string) (int, error)
   }

   type CalibrationSnapshotWriter interface {
       UpsertCriticCalibrationSnapshot(ctx context.Context, sourceKey string, snap domain.CriticCalibrationSnapshot) error
       RecentCriticCalibrationTrend(ctx context.Context, windowSize int, limit int) ([]domain.CriticCalibrationTrendPoint, error)
   }

   type CalibrationService struct {
       metrics   CalibrationMetricsReader
       snapshots CalibrationSnapshotWriter
       clk       clock.Clock
   }

   func NewCalibrationService(metrics CalibrationMetricsReader, snapshots CalibrationSnapshotWriter, clk clock.Clock) *CalibrationService

   func (s *CalibrationService) RefreshCriticCalibration(
       ctx context.Context,
       window int,
       calibrationThreshold float64,
   ) (*domain.CriticCalibrationSnapshot, error)
   ```

   Behavior:
   - Use `RecentCompletedRunsForMetrics(ctx, window)` as the sole rolling-window source. Do **not** introduce a second definition of the evaluation window.
   - `provisional = (window_count < window)`. This must be parameterized by the caller's `window`, not hard-coded to 25.
   - Reuse `KappaPairsForRuns` and `CohensKappa`; do **not** write a second SQL join just for this story unless a missing anchor query requires it.
   - Add `DecisionStore.LatestDecisionIDForRuns(ctx, runIDs []string) (int, error)` returning the max non-superseded approve/reject decision id in the evaluated window, or `0` when none exist. This is used only to build the idempotent `source_key`.
   - `source_key` must deterministically include:
     - `window`
     - `calibrationThreshold`
     - latest completed run id in the window (or empty string)
     - latest decision id in the window (or 0)
     - `window_count`
   - If `CohensKappa` returns `ok=false`, persist the snapshot anyway with:
     - `kappa = nil`
     - `reason = returned reason`
     - agreement/disagreement counts filled from the current observation set
   - If no completed runs exist, persist a provisional snapshot with `window_count=0`, `kappa=nil`, `reason="no paired observations"`, and blank run anchors.
   - `ComputedAt` comes from injected `clock.Clock`, RFC3339 UTC.

   Add:
   - `TestCalibrationService_Refresh_PersistsSnapshot`
   - `TestCalibrationService_Refresh_ProvisionalWhenShort`
   - `TestCalibrationService_Refresh_DegeneratePersistsReason`
   - `TestCalibrationService_Refresh_NoCompletedRunsStillPersists`

5. **AC-MANUAL-TRIGGER-VIA-METRICS-CMD:** wire the refresh path into `cmd/pipeline/metrics.go` so the current calibration snapshot is persisted whenever the operator runs the existing metrics command.

   Required rules:
   - Keep the user-facing JSON/human output shape from Story 2.7 unchanged; existing golden files under `testdata/golden/cli_metrics_*.{txt,json}` should remain semantically unchanged.
   - `runMetrics` should:
     1. compute the in-memory metrics report via `MetricsService.Report(...)`
     2. call `CalibrationService.RefreshCriticCalibration(...)` with the same `--window` and `--calibration-threshold`
     3. render the original report output
   - If calibration persistence fails, the command fails. Silent loss of trend history is worse than a visible error.
   - Do **not** print the trend in this story; persistence now, richer surface later.

   Add `TestMetricsCmd_PersistsCalibrationSnapshot` using a temp DB and a fixed fake clock.

6. **AC-QUERY-AND-TREND-READINESS:** add the minimal DB query support needed for future Tuning-tab trend views, but do not build the UI/API in this story.

   Required rules:
   - `RecentCriticCalibrationTrend` returns the latest `limit` snapshots for one `window_size`, ordered oldest → newest.
   - Trend rows must preserve provisional/unavailable states (`kappa=nil`, `reason!= ""`) rather than filtering them out.
   - No new HTTP handlers, web routes, or React components are in scope for Story 4.3.
   - No file-backed export is in scope either; JSON export belongs to Story 10.5.

   This story is done when the SQLite layer can answer: "what is the latest persisted kappa?" and "what are the last N persisted points for the current rolling window?"

7. **AC-FR-COVERAGE-AND-VALIDATION:** update the FR traceability and validation set so FR29 now covers both live computation and persisted calibration history.

   Required `testdata/fr-coverage.json` update:
   - `FR29` must include the new persistence tests alongside the existing kappa formula and metrics-report tests.
   - Annotation should mention:
     - rolling window
     - provisional label when `n < window`
     - persisted snapshot/trend history in SQLite

   Validation commands:
   - `go test ./internal/service -run 'CohensKappa|Calibration|Metrics'`
   - `go test ./internal/db -run 'Calibration|DecisionStore|RunStore'`
   - `go test ./cmd/pipeline -run Metrics`
   - `go test ./...`
   - `go build ./...`
   - `go run scripts/lintlayers/main.go`

## Tasks / Subtasks

- [x] **T1: Preserve and document the canonical kappa math** (AC: 1)
  - [x] Clarify in code comments that V1 uses unweighted binary Cohen's kappa.
  - [x] Keep the existing textbook/agreement/disagreement unit tests intact.

- [x] **T2: Add SQLite snapshot storage** (AC: 2, 3)
  - [x] Create `migrations/006_critic_calibration_snapshots.sql`.
  - [x] Add `internal/domain/calibration.go`.
  - [x] Add `internal/db/calibration_store.go` + tests.

- [x] **T3: Add refresh service and anchor query** (AC: 4)
  - [x] Add `DecisionStore.LatestDecisionIDForRuns`.
  - [x] Add `internal/service/calibration_service.go`.
  - [x] Reuse `RecentCompletedRunsForMetrics`, `KappaPairsForRuns`, and `CohensKappa`.

- [x] **T4: Wire manual persistence trigger into metrics CLI** (AC: 5)
  - [x] Update `cmd/pipeline/metrics.go` to persist the current snapshot after report computation.
  - [x] Add metrics-command integration coverage.

- [x] **T5: Add trend-readiness and traceability** (AC: 6, 7)
  - [x] Add trend query coverage.
  - [x] Update `testdata/fr-coverage.json`.
  - [x] Run the validation command set.

### Review Findings

- [x] [Review][Patch] `--calibration-threshold` NaN/Inf/out-of-[0,1] not validated [internal/service/calibration_service.go:38-46, cmd/pipeline/metrics.go:50-56]
- [x] [Review][Patch] Missing `TestDecisionStore_LatestDecisionIDForRuns_EmptyRunIDs` [internal/db/decision_store_test.go]
- [x] [Review][Patch] Missing `TestDecisionStore_LatestDecisionIDForRuns_AllSuperseded` [internal/db/decision_store_test.go]
- [x] [Review][Patch] `RecentCriticCalibrationTrend` outer ORDER lacks `id ASC` tie-breaker [internal/db/calibration_store.go:103]
- [x] [Review][Patch] `metricsStoreAdapter` constructed twice for same stores [cmd/pipeline/metrics.go:69,71]
- [x] [Review][Patch] Migration 006 silently omits spec's `DEFAULT (datetime('now'))` — add comment explaining the deliberate deviation [migrations/006_critic_calibration_snapshots.sql]
- [x] [Review][Defer] `MAX(id)` used as temporal proxy for "latest decision" [internal/db/decision_store.go:442] — deferred, autoincrement ordering matches insertion order in V1; reopen when backfill/import paths are introduced
- [x] [Review][Defer] No FK constraints on `window_start_run_id` / `window_end_run_id` / `latest_decision_id` [migrations/006_critic_calibration_snapshots.sql] — deferred, project-wide schema policy (other tables also skip explicit FKs)
- [x] [Review][Defer] `runIDs` > 999 hits SQLite parameter limit [internal/db/decision_store.go:441-446] — deferred, pre-existing in `KappaPairsForRuns`; CLI caps `--window` at 1000 so exposure is 1 over
- [x] [Review][Defer] NULL `scene_id` decisions can influence `MAX(id)` anchor [internal/db/decision_store.go:441-446] — deferred, depends on `decisions` schema clean-up outside story 4.3
- [x] [Review][Defer] `computed_at TEXT` has no CHECK for RFC3339 format [migrations/006_critic_calibration_snapshots.sql] — deferred, cross-cutting schema hardening
- [x] [Review][Defer] FR26/FR27/FR28 added with `annotation: null` and `total_frs` bumped 48→51 [testdata/fr-coverage.json] — deferred, introduced by concurrent stories 4.1/4.2/4.4, not 4.3's scope
- [x] [Review][Defer] No rollback/down migration for 006 [migrations/006_critic_calibration_snapshots.sql] — deferred, project convention is forward-only
- [x] [Review][Defer] No `EXPLAIN QUERY PLAN` test asserting `idx_critic_calibration_snapshots_window_computed_at` is used [internal/db/calibration_store_test.go] — deferred, testing policy decision

## Dev Notes

### Story 2.7 Already Fixed the V1 Semantics

The most important guardrail here is avoiding reinvention. Story 2.7 already chose the V1 interpretation of FR29:

- rolling window = most recent completed runs
- Critic pass/fail = `runs.critic_score >= threshold`
- operator side = dominant non-superseded approve/reject decision per run
- provisional = `window_count < window`

Story 4.3 must **persist that same result**, not redefine it.

### Do Not Invent Weighted Kappa or a New Verdict Model

The sprint prompt explicitly calls out code review on the weighted/unweighted distinction. V1 is the simple binary, unweighted formula. Do not add weighted categories, ordinal buckets, or a new `critic_verdict` column to `decisions`. That is a different product decision and would invalidate the already-shipped metrics CLI semantics.

### Why SQLite, Not Files

Golden / Shadow governance is file-backed because it is authorable, version-controlled test data. Calibration history is different: it is an operational time series derived from mutable runtime state (`runs` + `decisions`). That belongs in SQLite so the future Tuning tab and history views can query it without touching repo files.

### Why the Trigger Is `pipeline metrics`

Epic 8 will eventually own the richer review mutation paths that could refresh calibration automatically after every operator action. Today, those write paths are not implemented. Rather than inventing a background daemon or leaving FR29 half-operational, Story 4.3 should refresh the snapshot when the operator already asks for metrics. That gives us a deterministic, testable V1 trigger without extra UX scope.

### Idempotency Matters for Trends

If refresh appends a new row every time `pipeline metrics` is run against unchanged data, the trend will be full of duplicates and later charts will lie. That is why the migration includes `source_key` and the service includes a deterministic anchor built from the current run/decision boundary.

### Existing Code Paths This Story Extends

- `cmd/pipeline/metrics.go` — current manual metrics entrypoint
- `internal/service/metrics_service.go` — existing live metrics calculation
- `internal/service/kappa.go` — canonical unweighted formula
- `internal/db/run_store.go` — rolling completed-run window
- `internal/db/decision_store.go` — V1 runs/decisions join for calibration pairs
- `migrations/005_metrics_indexes.sql` — prior FR29 performance work; Story 4.3 should build on it, not replace it

### Explicit Non-Goals

- No Tuning-tab UI
- No `/api/calibration` handler yet
- No JSON export surface
- No weighted kappa
- No redesign of `decisions` or `runs` semantics beyond the minimal anchor query and snapshot table

## References

- [_bmad-output/planning-artifacts/epics.md:1314-1329 — Epic 4 / Story 4.3 scope](../planning-artifacts/epics.md#L1314)
- [_bmad-output/planning-artifacts/sprint-prompts.md:588-606 — Story 4.3 sprint prompt and review checklist](../planning-artifacts/sprint-prompts.md#L588)
- [_bmad-output/planning-artifacts/prd.md:258-287 — rolling 25-run window and Day-90 calibration target](../planning-artifacts/prd.md#L258)
- [_bmad-output/planning-artifacts/ux-design-specification.md:285-306 — calibration glossary and trend framing](../planning-artifacts/ux-design-specification.md#L285)
- [_bmad-output/implementation-artifacts/2-7-pipeline-metrics-cli-report.md](./2-7-pipeline-metrics-cli-report.md)
- [_bmad-output/implementation-artifacts/4-1-golden-eval-set-governance-validation.md](./4-1-golden-eval-set-governance-validation.md)
- [cmd/pipeline/metrics.go](../../cmd/pipeline/metrics.go)
- [internal/service/kappa.go](../../internal/service/kappa.go)
- [internal/service/metrics_service.go](../../internal/service/metrics_service.go)
- [internal/db/decision_store.go](../../internal/db/decision_store.go)
- [internal/db/run_store.go](../../internal/db/run_store.go)
- [migrations/001_init.sql](../../migrations/001_init.sql)
- [migrations/005_metrics_indexes.sql](../../migrations/005_metrics_indexes.sql)
- [testdata/fr-coverage.json](../../testdata/fr-coverage.json)

## Dev Agent Record

### Agent Model Used

GPT-5 Codex

### Debug Log References

- `go test ./internal/service -run 'CohensKappa|Calibration|Metrics'`
- `go test ./internal/db -run 'Calibration|DecisionStore|RunStore'`
- `go test ./cmd/pipeline -run Metrics`
- `go test ./...`
- `go build ./...`
- `go run scripts/lintlayers/main.go`

### Completion Notes List

- Added SQLite-backed calibration snapshot persistence via migration `006_critic_calibration_snapshots.sql` and the new `domain.CriticCalibrationSnapshot` / `CriticCalibrationTrendPoint` contracts.
- Implemented `CalibrationStore` with idempotent `source_key` upserts, oldest-to-newest trend reads, and migration/schema coverage for the new snapshot table and index.
- Added `CalibrationService.RefreshCriticCalibration` to reuse `RecentCompletedRunsForMetrics`, `KappaPairsForRuns`, `LatestDecisionIDForRuns`, and the canonical `CohensKappa` helper while persisting provisional and unavailable snapshots deterministically from the injected clock.
- Wired `pipeline metrics` to refresh persisted calibration snapshots after report computation without changing the existing human/JSON output shape.
- Expanded FR29 traceability to cover rolling-window persistence and trend history in SQLite, plus added command/store/service tests for idempotency, degenerate/no-data snapshots, and metrics-triggered persistence.

### File List

- `_bmad-output/implementation-artifacts/4-3-cohens-kappa-calibration-tracking.md`
- `_bmad-output/implementation-artifacts/sprint-status.yaml`
- `cmd/pipeline/metrics.go`
- `cmd/pipeline/metrics_test.go`
- `internal/db/calibration_store.go`
- `internal/db/calibration_store_test.go`
- `internal/db/decision_store.go`
- `internal/db/decision_store_test.go`
- `internal/db/sqlite_test.go`
- `internal/domain/calibration.go`
- `internal/service/calibration_service.go`
- `internal/service/calibration_service_test.go`
- `internal/service/kappa.go`
- `migrations/006_critic_calibration_snapshots.sql`
- `testdata/fr-coverage.json`

## Change Log

- 2026-04-18: Implemented Story 4.3 calibration snapshot persistence, trend query support, metrics-command refresh wiring, and FR29 coverage updates.
