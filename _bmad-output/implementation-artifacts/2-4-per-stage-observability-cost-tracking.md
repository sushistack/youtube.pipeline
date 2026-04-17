# Story 2.4: Per-Stage Observability & Cost Tracking

Status: review

## Story

As an operator,
I want observability data and cost tracking recorded at every stage completion plus active cost-cap enforcement and 429-aware retry classification,
so that I can diagnose any run from the SQLite CLI and never silently overspend on external APIs.

## Acceptance Criteria

1. **AC-OBS-RECORD:** A `RunStore.RecordStageObservation(ctx, runID, obs)` method exists. On a single `runs` row UPDATE in one transaction it applies these column-by-column semantics:

    | Column | Semantic | Source |
    |---|---|---|
    | `cost_usd` | accumulator (`+=`) | `obs.CostUSD` |
    | `token_in` | accumulator (`+=`) | `obs.TokenIn` |
    | `token_out` | accumulator (`+=`) | `obs.TokenOut` |
    | `duration_ms` | accumulator (`+=`) | `obs.DurationMs` |
    | `retry_count` | accumulator (`+=`) | `obs.RetryCount` |
    | `retry_reason` | overwrite (last-write-wins, NULL-able) | `obs.RetryReason` (`*string`) |
    | `critic_score` | overwrite (NULL-able) | `obs.CriticScore` (`*float64`) |
    | `human_override` | sticky-OR (`= human_override OR obs.HumanOverride`) | `obs.HumanOverride` (`bool`) |

    All 8 columns from the schema MUST be touched on every call (NFR-O1: no sampling, no truncation, no skipped column). 0-rows-affected → `domain.ErrNotFound`. The Migration 002 trigger advances `updated_at`.

2. **AC-OBS-DOMAIN:** A `domain.StageObservation` struct exists in `internal/domain/observability.go` with these fields, matching DB column types:

    ```go
    type StageObservation struct {
        Stage         Stage    // diagnostic / log; not persisted to runs row directly
        DurationMs    int64
        TokenIn       int
        TokenOut      int
        RetryCount    int
        RetryReason   *string  // nil ↔ NULL (no overwrite of NULL); "" is treated as a non-nil empty string overwrite (avoid)
        CriticScore   *float64
        CostUSD       float64
        HumanOverride bool
    }
    ```
    A constructor `NewStageObservation(stage Stage)` returns a zero-valued observation with `Stage` set; pointers stay nil. A `(o StageObservation) Validate() error` rejects negative numerics and unknown `Stage` (uses `Stage.IsValid`). An `IsZero()` helper exists for convenience in tests.

3. **AC-COST-ACCUMULATOR:** `internal/pipeline/cost.go` declares a `CostAccumulator` with:

    ```go
    type CostAccumulator struct { /* unexported state */ }

    func NewCostAccumulator(perStageCaps map[domain.Stage]float64, perRunCap float64) *CostAccumulator

    // Add records cost for a stage. The cost IS recorded (NFR-C3: no truncation)
    // even on cap violation. Returns ErrCostCapExceeded if either:
    //   - the post-add stage total exceeds perStageCaps[stage], OR
    //   - the post-add run total exceeds perRunCap.
    // A perStageCap of 0 means "no per-stage cap configured for this stage";
    // a perRunCap of 0 means "no per-run cap configured" (both → no enforcement).
    func (a *CostAccumulator) Add(stage domain.Stage, costUSD float64) error

    func (a *CostAccumulator) StageTotal(stage domain.Stage) float64
    func (a *CostAccumulator) RunTotal() float64
    func (a *CostAccumulator) Tripped() bool                    // true once Add has returned ErrCostCapExceeded
    func (a *CostAccumulator) TripReason() (domain.Stage, string) // ("", "") until tripped; then (offending stage, "stage_cap"|"run_cap")
    ```

    `costUSD` < 0 → `ErrValidation` (no recording). Stage not in the cap map → no per-stage enforcement for that stage (defensive: prevents accidental zero-cap from blocking unmapped stages). Once `Tripped()` returns true, all subsequent `Add` returns `ErrCostCapExceeded` immediately (circuit-breaker semantics — once open, stays open). Concurrency: `Add` is goroutine-safe via an internal mutex (Phase B's parallel image+TTS tracks share one accumulator instance).

4. **AC-COST-CAP-WRAPPING:** The error returned by `Add` wraps `domain.ErrCostCapExceeded` so `errors.Is(err, domain.ErrCostCapExceeded)` is true and `domain.Classify(err)` returns `(402, "COST_CAP_EXCEEDED", false)`. The error message includes the offending stage name, the cap that was breached (`stage_cap` or `run_cap`), the actual total, and the configured cap:

    ```
    cost cap exceeded: stage=write reason=stage_cap actual=$0.5234 cap=$0.5000
    cost cap exceeded: stage=image reason=run_cap   actual=$5.1100 cap=$5.0000
    ```

5. **AC-CONFIG-PER-RUN-CAP:** `domain.PipelineConfig` gains a `CostCapPerRun float64` field with `yaml:"cost_cap_per_run" mapstructure:"cost_cap_per_run"`. `DefaultConfig()` sets it to `5.00` (USD), > sum of the existing per-stage caps so the per-stage caps are the primary guardrail and the per-run cap acts as the worst-case backstop. `_bmad-output/implementation-artifacts/.env.example` (and any sample `~/.youtube-pipeline/config.yaml` written by `init`) documents the new key. `internal/config` continues to load the field via the existing Viper layering (no loader changes beyond the new struct field).

6. **AC-RECORDER:** `internal/pipeline/observability.go` declares a `Recorder` orchestrator that ties the cost accumulator to the persistence layer and is the **only** code path through which `runs` observability columns are mutated by stage code:

    ```go
    type ObservationStore interface {
        RecordStageObservation(ctx context.Context, runID string, obs domain.StageObservation) error
    }

    type Recorder struct { /* obs store + cost accumulator + clock + logger */ }

    func NewRecorder(store ObservationStore, costs *CostAccumulator, clk clock.Clock, logger *slog.Logger) *Recorder

    // Record persists obs to the runs row AND adds obs.CostUSD to the cost accumulator.
    // Order is: cost accumulator Add (may return ErrCostCapExceeded) → DB persistence.
    // CRITICAL: the DB write happens REGARDLESS of cap-exceeded — observability data
    // is never dropped (NFR-C3). On cap-exceeded the method returns the wrapped
    // ErrCostCapExceeded after persistence completes.
    // On a DB error, the cost accumulator's state is preserved (no rollback) — the
    // accumulator is the authoritative in-memory truth; the DB is a downstream sink.
    func (r *Recorder) Record(ctx context.Context, runID string, obs domain.StageObservation) error

    // RecordRetry is a convenience for the 429 backoff flow: builds a StageObservation
    // with only RetryCount=1 + RetryReason set, then calls Record. Cost is zero, so
    // no cap check fires.
    func (r *Recorder) RecordRetry(ctx context.Context, runID string, stage domain.Stage, reason string) error
    ```

    `Record` MUST emit one `slog.Info` line per call with keys: `run_id`, `stage`, `cost_usd`, `token_in`, `token_out`, `duration_ms`, `retry_count`, `retry_reason`, `critic_score`, `human_override`. Logger is the constructor-injected one (no `slog.Default()`). `Record` is goroutine-safe (delegates to the mutex-protected accumulator + the SQLite single-writer guarantee under `MaxOpenConns=1`).

7. **AC-RETRY-CLASSIFIER:** `internal/llmclient/retry.go` declares:

    ```go
    // RetryReasonFor maps a wrapped domain error to the canonical retry_reason
    // string written to runs.retry_reason. Returns "" for non-retryable errors.
    func RetryReasonFor(err error) string

    // WithRetry executes fn, retrying on retryable domain errors with exponential
    // backoff (1s, 2s, 4s, ... capped at 30s) plus jitter. clock.Sleep drives the
    // delay so FakeClock can advance time deterministically in tests.
    // onRetry is invoked BEFORE the sleep with (attempt, reason). Use it to record
    // observability (e.g. via Recorder.RecordRetry).
    func WithRetry(
        ctx context.Context,
        clk clock.Clock,
        maxRetries int,
        fn func() error,
        onRetry func(attempt int, reason string),
    ) error
    ```

    `RetryReasonFor` mapping (`errors.Is`-based):
    - `domain.ErrRateLimited` → `"rate_limit"`
    - `domain.ErrUpstreamTimeout` → `"timeout"`
    - `domain.ErrStageFailed` → `"stage_failed"` (operator-resumable; auto-retry once at most via WithRetry's maxRetries cap)
    - everything else (`ErrValidation`, `ErrConflict`, `ErrCostCapExceeded`, `ErrNotFound`, unwrapped) → `""` (caller should NOT auto-retry)

    `WithRetry` aborts immediately when `RetryReasonFor(err)` returns `""` (non-retryable), even if `maxRetries` is not yet exhausted. `ErrCostCapExceeded` is explicitly non-retryable per AC-COST-CAP-WRAPPING — `WithRetry` MUST surface it directly without sleeping. `ctx.Err()` from `clk.Sleep` aborts the loop and propagates.

8. **AC-RETRY-NO-STAGE-ADVANCE-NFR-P3:** A 429 response from any external API MUST NOT cause the engine to call `NextStage` or `RunStore.SetStatus(ctx, runID, StatusFailed, ...)`. Verified by an integration test that:
   - Loads a run at `stage=write, status=running` from a new fixture `testdata/fixtures/running_at_write.sql`.
   - Wraps a fake LLM call that returns `domain.ErrRateLimited` on attempts 1-2 and success on attempt 3.
   - Drives `WithRetry` with `FakeClock` + `Recorder` injected.
   - After all retries and successful completion: asserts `runs.stage == "write"`, `runs.status == "running"`, `runs.retry_count == 2`, `runs.retry_reason == "rate_limit"`. The `runs` row was never SetStatus'd to `failed`. (NFR-P3 satisfied; "rate_limit" is recorded in the observability row.)

9. **AC-COST-CAP-HARD-STOP-NFR-C1-C2:** Two integration tests prove cap enforcement is **immediate** (during accumulation, not at stage completion) and that the over-cap call's data is still persisted (NFR-C3):
   - **NFR-C1 (per-stage):** Configure `CostCapWrite = $0.50`, run total cap $5. Drive Recorder with three Records on `StageWrite`: $0.20, $0.20, $0.20. Assert: third call returns an error wrapping `ErrCostCapExceeded` with reason `"stage_cap"` and `runs.cost_usd == 0.60` (over-cap data is recorded) and `accumulator.Tripped() == true`. A 4th call returns `ErrCostCapExceeded` immediately without writing again? — **clarification:** the 4th call DOES still write (NFR-C3: every recorded cost is persisted). The "circuit breaker" semantic affects the **caller's decision** to make further API calls; once the cap-exceeded error returns, the engine MUST NOT initiate any further external API calls for the stage/run. This is the "active enforcement" the architecture mandates.
   - **NFR-C2 (per-run):** Configure all per-stage caps to $999 (effectively disabled), `CostCapPerRun = $1.00`. Drive Records across multiple stages totaling $1.10. Assert: the call that crosses $1.00 returns wrapping `ErrCostCapExceeded` with reason `"run_cap"`. `runs.cost_usd == 1.10`. `Tripped()` true.

10. **AC-MIGRATION-INDEXES-NFR-O4:** `migrations/003_observability_indexes.sql` adds indexes to support rolling-window diagnostic queries without full-table scans:

    ```sql
    -- Migration 003: NFR-O4 indexes for rolling-window observability queries.
    -- Justification: Day-90 metrics, "failed runs in last N days", "cost by status"
    -- are foundational to FR48 (pipeline metrics CLI report, Story 2.7).

    CREATE INDEX IF NOT EXISTS idx_runs_created_at         ON runs(created_at);
    CREATE INDEX IF NOT EXISTS idx_runs_status_created_at  ON runs(status, created_at);
    CREATE INDEX IF NOT EXISTS idx_runs_stage              ON runs(stage);
    ```

    All three are `IF NOT EXISTS` so they are safe to apply against any DB at any state; the migration runner's user_version gate already prevents re-execution. The migration MUST roll forward cleanly on a DB previously migrated to user_version=2.

11. **AC-MIGRATION-INDEX-VERIFICATION:** A new test `internal/db/observability_query_test.go` (or extension of `migrate_test.go`) seeds the DB with ≥ 50 runs spanning a date range, then runs `EXPLAIN QUERY PLAN` on each of the canonical rolling-window queries below and asserts the plan contains a `USING INDEX` substring (i.e. the query planner picks the new indexes, not a `SCAN runs`):

    - `SELECT id, stage, status, cost_usd FROM runs WHERE created_at > ? ORDER BY created_at DESC`
    - `SELECT COUNT(*) FROM runs WHERE status = ? AND created_at > ?`
    - `SELECT stage, COUNT(*) FROM runs WHERE created_at > ? GROUP BY stage`

    Failure mode: if any plan reports `SCAN runs`, the test fails with a message naming the query and the actual plan (so the developer adjusts the index, not the assertion).

12. **AC-CLI-DIAGNOSTIC-NFR-O3:** A new test file `internal/db/diagnostic_query_test.go` (or extension of `observability_query_test.go`) demonstrates that the canonical operator diagnostic queries are answerable directly from the CLI-visible schema — no Go-side joining, no JSON1 extraction. Each query is exercised against a populated DB and the result asserted. Document each query in code comments with a one-line description AND copy them verbatim into the operator-facing reference at `docs/cli-diagnostics.md` (new file, ≤ 60 lines). Required queries:

    - **Recent failures:** `SELECT id, stage, retry_count, retry_reason, cost_usd FROM runs WHERE status='failed' ORDER BY updated_at DESC LIMIT 10;`
    - **Today's spend:** `SELECT SUM(cost_usd) FROM runs WHERE created_at > date('now', 'start of day');`
    - **Rolling 90-day failure rate:** `SELECT status, COUNT(*) FROM runs WHERE created_at > date('now','-90 days') GROUP BY status;`
    - **Per-stage cost breakdown:** `SELECT stage, SUM(cost_usd) AS total, AVG(duration_ms) AS avg_ms FROM runs WHERE created_at > date('now','-7 days') GROUP BY stage ORDER BY total DESC;`
    - **Critic score health:** `SELECT id, critic_score FROM runs WHERE critic_score IS NOT NULL AND critic_score < 0.7 ORDER BY updated_at DESC LIMIT 20;`

    NFR-O3 is "ergonomic" by nature; the test enforces it by **failing the build if the queries become non-trivial** (e.g. if a future schema change forces a JOIN, the test breaks and a developer is forced to either restore CLI-friendliness or update the operator docs deliberately).

13. **AC-OBS-FIXTURE:** `testdata/fixtures/observability_seed.sql` seeds 60 runs with realistic distributions: ~70% completed, ~15% failed, ~10% cancelled, ~5% in-progress; `created_at` spans the last 120 days (so 90-day window queries get hits and misses); cost values vary from $0.01 to $4.50; a few have `retry_count > 0` with `retry_reason IN ('rate_limit', 'timeout', 'stage_failed')`; ~5 carry a `critic_score < 0.7` (low-quality bucket); ~3 have `human_override = 1`. This fixture is consumed by AC-MIGRATION-INDEX-VERIFICATION and AC-CLI-DIAGNOSTIC-NFR-O3.

14. **AC-RECORDER-OBSERVABILITY-LOG:** `Recorder.Record` emits a single structured slog entry per stage observation with the message `"stage observation"`. Test via `testutil.CaptureLog`: assert exactly one log line, JSON-decoded, with all 10 expected keys (`run_id`, `stage`, `cost_usd`, `token_in`, `token_out`, `duration_ms`, `retry_count`, `retry_reason` (string or null), `critic_score` (number or null), `human_override` (bool)). On cap-exceeded, an additional `"cost cap exceeded"` slog.Warn line is emitted with the same keys plus `cap_reason` (`"stage_cap"` or `"run_cap"`).

15. **AC-LAYER-LINT-CLEAN:** `make lint-layers` passes unchanged. Allowed imports already cover the new edges:
    - `internal/llmclient → {internal/domain, internal/clock}` ✓ — `retry.go` imports both.
    - `internal/pipeline → {internal/domain, internal/db, internal/llmclient, internal/clock}` ✓ — `cost.go` imports `domain`, `observability.go` imports `domain` + `clock`.
    - `internal/db → {internal/domain}` ✓ — `RecordStageObservation` extension to `run_store.go`.

    The `Recorder` consumes `ObservationStore` (locally declared interface in `pipeline/observability.go`) — same pattern as Story 2.3's `RunStore`/`SegmentStore` declared in `pipeline/resume.go`. `pipeline/` does NOT import `service/`. No `scripts/lintlayers/main.go` edits required.

16. **AC-IDEMPOTENCY-WRITES:** `Recorder.Record` is **NOT** idempotent — calling it twice with the same observation will double the accumulator columns. This is by design (every call represents a real spend event). The unit tests MUST document this explicitly with a comment in the test that asserts the doubled total. Do not introduce de-dup logic; the engine (Story 3.x) is responsible for calling Record exactly once per stage completion.

17. **AC-FR-COVERAGE:** `testdata/fr-coverage.json` is updated in-place to add entries for `FR5` (8-column observability captured), `FR6` (per-stage observability data), and is annotated for the NFRs covered (`NFR-C1`, `NFR-C2`, `NFR-C3`, `NFR-O1`, `NFR-O3`, `NFR-O4`, `NFR-P3`). Use the existing `fr_id` + `test_ids` + `annotation` shape. The file's `meta.last_updated` is set to today's date.

18. **AC-NO-REGRESSIONS:** `go test ./... && go build ./... && make lint-layers` all pass with zero modifications to existing 1.x and 2.1–2.3 tests. CGO_ENABLED=0 everywhere. `make ci-go` (or its equivalent) stays green.

---

## Tasks / Subtasks

- [x] **T1: domain/observability.go — StageObservation type** (AC: #2)
  - [x] Create `internal/domain/observability.go` with `StageObservation` struct, `NewStageObservation(stage Stage)`, `(o StageObservation) Validate() error`, and `(o StageObservation) IsZero() bool`. Validate rejects negative numerics (cost, tokens, duration, retry_count) and `Stage.IsValid() == false`. JSON tags use `snake_case` (consistent with the `domain.Run` precedent).
  - [x] Create `internal/domain/observability_test.go` covering: zero value detection; Validate happy path; Validate rejects negatives (table-driven); Validate rejects unknown stage. Use `testutil.AssertEqual` (no testify).
  - [x] Keep file < 200 lines (the 300-line cap is generous — split per-concept if it grows).

- [x] **T2: db/run_store.go — RecordStageObservation** (AC: #1, #14)
  - [x] Extend `internal/db/run_store.go` with:
    ```go
    func (s *RunStore) RecordStageObservation(ctx context.Context, runID string, obs domain.StageObservation) error
    ```
  - [x] Implementation: ONE `UPDATE runs SET cost_usd = cost_usd + ?, token_in = token_in + ?, token_out = token_out + ?, duration_ms = duration_ms + ?, retry_count = retry_count + ?, retry_reason = ?, critic_score = ?, human_override = (human_override | ?) WHERE id = ?` — note `retry_reason` and `critic_score` use `sql.NullString` / `sql.NullFloat64` derived from the pointer fields (nil → NULL); `human_override` is the SQLite bitwise-OR (`|`) so a single `1` ever sticks; the trigger-driven `updated_at` advances automatically.
  - [x] Returns `domain.ErrNotFound` when `RowsAffected() == 0` (aligns with `SetStatus`).
  - [x] No transaction wrapping required (single `UPDATE` is atomic under `MaxOpenConns=1`); document the reasoning in a one-line comment.
  - [x] Validate `obs.Validate()` before issuing the SQL: invalid → `domain.ErrValidation` (no DB write).
  - [x] Extend `internal/db/run_store_test.go` with: `TestRunStore_RecordStageObservation_AccumulatesColumns` (call twice, assert sums double); `TestRunStore_RecordStageObservation_NullableOverwrite` (RetryReason/CriticScore: nil → leaves NULL; non-nil → overwrites; second call with nil after non-nil → leaves the previous non-nil value untouched, since nil means "no overwrite" — clarify in test comments or store code, see Dev Notes); `TestRunStore_RecordStageObservation_HumanOverrideSticky` (call once with true, then false → still true); `TestRunStore_RecordStageObservation_NotFound`; `TestRunStore_RecordStageObservation_RejectsInvalid` (negative cost → ErrValidation, no DB mutation).
  - [x] `testutil.BlockExternalHTTP(t)` in every new test function.

- [x] **T3: pipeline/cost.go — CostAccumulator** (AC: #3, #4, #9)
  - [x] Create `internal/pipeline/cost.go` with the public surface from AC-COST-ACCUMULATOR. Internal state: `mu sync.Mutex`, `stageTotals map[domain.Stage]float64`, `runTotal float64`, `tripped bool`, `tripStage domain.Stage`, `tripReason string` ("stage_cap" or "run_cap").
  - [x] `Add(stage, cost)` algorithm:
    1. `if cost < 0`: return `fmt.Errorf("add cost: %w: negative", domain.ErrValidation)`.
    2. Lock mutex.
    3. If `tripped`: increment totals (NFR-C3) and return `wrappedCapErr(tripStage, tripReason, totals...)`.
    4. Otherwise: increment `stageTotals[stage]` and `runTotal`.
    5. Compute the new totals; check per-stage cap if mapped AND > 0; check per-run cap if > 0.
    6. If breached: set `tripped = true`, `tripStage = stage`, `tripReason = ...`; return wrapped error. Otherwise return nil.
  - [x] `wrappedCapErr` builds the message in AC-COST-CAP-WRAPPING and wraps `domain.ErrCostCapExceeded` with `fmt.Errorf("%w: stage=%s reason=%s actual=$%.4f cap=$%.4f", domain.ErrCostCapExceeded, ...)`.
  - [x] Create `internal/pipeline/cost_test.go` covering: `TestCostAccumulator_NoCap_NoError`; `TestCostAccumulator_PerStageCap_TripsOnOverrun`; `TestCostAccumulator_PerRunCap_TripsOnOverrun`; `TestCostAccumulator_NegativeCost_ValidationError`; `TestCostAccumulator_TrippedStaysTripped` (post-trip Adds still record + still error); `TestCostAccumulator_StageNotInCapMap_NoEnforcement`; `TestCostAccumulator_ConcurrentAdds` — spawn 100 goroutines doing `Add(StageImage, 0.001)` against a $0.05 cap; assert eventual trip and that `RunTotal()` reflects every recorded add (no lost updates).
  - [x] Tests are deterministic (no `time.Sleep`, no real API). Use `testutil.AssertEqual` and `errors.Is`.

- [x] **T4: domain/config.go — CostCapPerRun** (AC: #5)
  - [x] Add `CostCapPerRun float64 `yaml:"cost_cap_per_run" mapstructure:"cost_cap_per_run"`` to `domain.PipelineConfig`.
  - [x] Set `DefaultConfig()` to assign `CostCapPerRun: 5.00` (USD). The default is intentionally larger than `CostCapResearch + CostCapWrite + CostCapImage + CostCapTTS + CostCapAssemble` ($0.50 + $0.50 + $2.00 + $1.00 + $0.10 = $4.10) so per-stage caps are the primary enforcement and per-run cap is the safety net.
  - [x] Update `internal/domain/config_test.go` (whatever exists) to assert the new default.
  - [x] Update `.env.example` (project root) and the sample config emitted by `pipeline init` (`cmd/pipeline/init.go` or whichever helper writes `~/.youtube-pipeline/config.yaml`) to include `cost_cap_per_run: 5.00` with a one-line `# per-run hard cap (USD); NFR-C2 backstop` comment.

- [x] **T5: pipeline/observability.go — Recorder** (AC: #6, #14, #16)
  - [x] Create `internal/pipeline/observability.go` with the `ObservationStore` interface (locally declared, satisfied structurally by `*db.RunStore`) and the `Recorder` orchestrator.
  - [x] `Record(ctx, runID, obs)` algorithm:
    1. `obs.Validate()` → on error, return immediately (no log, no persist, no accumulator mutation).
    2. `costErr := r.costs.Add(obs.Stage, obs.CostUSD)` — captures the cap state but does NOT short-circuit persistence.
    3. `dbErr := r.store.RecordStageObservation(ctx, runID, obs)` — persists regardless of `costErr`.
    4. Emit one `slog.Info("stage observation", ...)` line with all 10 keys (per AC-RECORDER-OBSERVABILITY-LOG). On `costErr`, ALSO emit `slog.Warn("cost cap exceeded", ..., "cap_reason", reason)`.
    5. Return order: if `dbErr != nil` AND `costErr != nil`, return `errors.Join(costErr, dbErr)` so callers see both. Otherwise return whichever is non-nil.
  - [x] `RecordRetry(ctx, runID, stage, reason)` builds an obs with `RetryCount=1` + `RetryReason=&reason` + zero everything else, then calls `Record`.
  - [x] Create `internal/pipeline/observability_test.go` with inline fake `ObservationStore`. Tests: happy path (one slog line, store called once, no cost trip); cap exceeded (Add returns error, store STILL called, return wraps `ErrCostCapExceeded`, second slog line "cost cap exceeded" present); store error + cap exceeded (returns joined error, both errors discoverable via `errors.Is`); validate fails (no store call, no slog line); RecordRetry shape (RetryCount=1, RetryReason set, all else zero); concurrent Records (5 goroutines, no race per `go test -race`).

- [x] **T6: llmclient/retry.go — RetryReasonFor + WithRetry** (AC: #7, #8)
  - [x] Create `internal/llmclient/retry.go`:
    - `RetryReasonFor(err error) string` — `errors.Is`-based switch per the AC-RETRY-CLASSIFIER mapping. Document the truth table in a comment.
    - `WithRetry(ctx, clk, maxRetries, fn, onRetry) error` — for-loop `for attempt := 0; attempt <= maxRetries; attempt++`: call `fn`; on nil → return; classify reason; if `""` → return original error directly (non-retryable, no sleep); otherwise call `onRetry(attempt+1, reason)`; compute delay `min(time.Duration(1<<attempt) * time.Second + jitter(), 30*time.Second)`; `clk.Sleep(ctx, delay)` (propagate ctx error). After loop: `fmt.Errorf("max retries (%d) exceeded: %w", maxRetries, lastErr)`.
    - `jitter()` returns `time.Duration(rand.Int63n(int64(time.Second/2)))` — ±0.5s. Seed local `rand.Source` once via `rand.New(rand.NewSource(time.Now().UnixNano()))`. Mark a TODO to wire this through `clk.Now()` once the clock interface gains `Rand()` (out of scope here).
  - [x] Create `internal/llmclient/retry_test.go`. Use `clock.NewFakeClock(time.Now())` injected. Tests:
    - `TestRetryReasonFor_RateLimited` → `"rate_limit"`.
    - `TestRetryReasonFor_UpstreamTimeout` → `"timeout"`.
    - `TestRetryReasonFor_CostCapExceeded` → `""` (non-retryable; do NOT add as a retry reason).
    - `TestRetryReasonFor_Validation` → `""`.
    - `TestRetryReasonFor_NotFound` → `""`.
    - `TestWithRetry_SucceedsOnFirstAttempt` (fn returns nil immediately).
    - `TestWithRetry_RetriesOn429` — fn returns `ErrRateLimited` twice then nil; assert `onRetry` called twice with reasons `"rate_limit"`; no real sleep observed (use FakeClock + a goroutine pattern: `go fn() ; clk.Advance(1*time.Second); etc.` OR drive the clock via a `for !done { clk.Advance(1*time.Second) }` helper). Verify total elapsed (FakeClock.Now()) is ≥ 1s + 2s.
    - `TestWithRetry_NonRetryableSurfacesImmediately` — fn returns `ErrValidation`; assert `onRetry` never called, error propagates.
    - `TestWithRetry_CostCapBypassesRetries` — fn returns `ErrCostCapExceeded`; assert immediate return, no onRetry, no sleep.
    - `TestWithRetry_ContextCanceledDuringSleep` — cancel ctx mid-sleep; assert ctx.Err() propagates.
    - `TestWithRetry_MaxRetriesExceeded` — fn always returns `ErrRateLimited`; assert error wraps `ErrRateLimited` and message contains "max retries".
  - [x] Use `testutil.BlockExternalHTTP(t)` in every test (paranoid habit per Story 2.3 learnings).

- [x] **T7: Migration 003 + index verification** (AC: #10, #11)
  - [x] Create `migrations/003_observability_indexes.sql` with the three `CREATE INDEX IF NOT EXISTS` statements from AC-MIGRATION-INDEXES-NFR-O4.
  - [x] Add a one-paragraph header comment documenting the NFR-O4 motivation and explicitly noting why we did NOT index `cost_usd` (rolling sums scan a fixed window already filtered by `created_at`; an index on `cost_usd` adds write overhead without query benefit).
  - [x] The migration runner (`internal/db/migrate.go`) auto-discovers it via `embed.FS` — no Go-side wiring change.
  - [x] Add `internal/db/observability_query_test.go` (or extend an existing migrate test): seed via `testdata/fixtures/observability_seed.sql`, then exec each query from AC-MIGRATION-INDEX-VERIFICATION wrapped in `EXPLAIN QUERY PLAN`, scan the result, assert the plan substring contains `USING INDEX` (case-sensitive — SQLite's planner output). On failure, the test prints the actual plan so the developer can iterate on the index.

- [x] **T8: Observability fixture** (AC: #13)
  - [x] Create `testdata/fixtures/observability_seed.sql` with 60 INSERT rows. Use `datetime('now', '-N days')` expressions for `created_at` to span the last 120 days. Distribute statuses approximately as: 42 completed, 9 failed, 6 cancelled, 3 running. Cost values: random spread $0.01–$4.50 (deterministic — hard-code each value, do not use random functions in the SQL itself). Critic scores: 30 NULLs, 25 in [0.7, 1.0], 5 in [0.4, 0.7) (low-quality bucket). `human_override = 1` on 3 rows.
  - [x] Document the row distribution in a comment at the top of the fixture so future test additions don't break the assertions.

- [x] **T9: CLI diagnostic test + docs** (AC: #12)
  - [x] Create `internal/db/diagnostic_query_test.go` (or merge into the file from T7). For each query in AC-CLI-DIAGNOSTIC-NFR-O3:
    - Exec it as a raw SQL string against the seeded DB.
    - Assert the result row count + a representative field value (e.g. "rolling 90-day failure rate query returns ≥ 1 row labeled 'failed'").
    - The assertion serves as the "queries are CLI-answerable" proof — if a future schema change forces a JOIN, the test breaks.
  - [x] Create `docs/cli-diagnostics.md` (≤ 60 lines, Korean is fine per `communication_language: Korean`, but code blocks are SQL). Each query has: 1-line description, the SQL, an example output snippet. This is the operator-facing reference for NFR-O3.
  - [x] Add a one-line link from `docs/README.md` (or wherever existing docs are indexed) → `docs/cli-diagnostics.md` if a docs index exists; otherwise leave standalone.

- [x] **T10: Integration test — 429 backoff does not advance stage** (AC: #8)
  - [x] Create `testdata/fixtures/running_at_write.sql`: one run `scp-049-run-1`, `stage='write'`, `status='running'`, `retry_count=0`, all observability columns at zero.
  - [x] Create `internal/pipeline/observability_integration_test.go` (or extend `engine_test.go`):
    - Load the fixture via `testutil.LoadRunStateFixture(t, "running_at_write")`.
    - Build a real `*db.RunStore` + `Recorder` + `CostAccumulator` (no caps tripped) + `clock.NewFakeClock(...)`.
    - Define a counter-driven fake LLM call `func() error` that returns `domain.ErrRateLimited` on attempts 1–2 and nil on attempt 3.
    - Drive `WithRetry(ctx, clk, 3, call, recorder.RecordRetry-adapter)` in a goroutine; advance `FakeClock` to release backoff sleeps; wait for completion.
    - Assert the post-state via `RunStore.Get`: `Stage == "write"`, `Status == "running"`, `RetryCount == 2`, `*RetryReason == "rate_limit"`. Most importantly: assert that `SetStatus` was NEVER called (verifiable by tracking via a wrapper around `*db.RunStore` that counts SetStatus invocations, or by explicit assertion that `Status` is unchanged).
  - [x] Use `testutil.BlockExternalHTTP(t)`.

- [x] **T11: Integration test — cost cap hard-stop with persistence** (AC: #9)
  - [x] Create `internal/pipeline/cost_integration_test.go`:
    - Two test functions: `TestRecorder_PerStageCap_HardStop` and `TestRecorder_PerRunCap_HardStop`.
    - Per-stage: `CostCapWrite=$0.50`, `CostCapPerRun=$5.00`. Make 3 Records on `StageWrite` ($0.20, $0.20, $0.20). Assert: 3rd returns `errors.Is(err, ErrCostCapExceeded)` AND `runs.cost_usd == 0.60` (over-cap data IS recorded — NFR-C3) AND `domain.Classify(err)` returns `(402, "COST_CAP_EXCEEDED", false)`.
    - Per-run: per-stage caps all $999, `CostCapPerRun=$1.00`. Records across StageResearch ($0.40), StageWrite ($0.40), StageImage ($0.30). Assert 3rd returns wrapping `ErrCostCapExceeded` with `reason="run_cap"` AND `runs.cost_usd == 1.10`.
    - A 4th Record after trip is invoked; verify it ALSO returns `ErrCostCapExceeded` AND the DB cost continues to grow (NFR-C3: every recorded spend is persisted).
  - [x] Use real `*db.RunStore` via `testutil.NewTestDB(t)` + per-test `t.TempDir()` for `output_dir`. `testutil.BlockExternalHTTP(t)`.

- [x] **T12: FR coverage update** (AC: #17)
  - [x] Edit `testdata/fr-coverage.json`:
    - Add `{"fr_id": "FR5", "test_ids": ["TestRunStore_RecordStageObservation_AccumulatesColumns", "TestRecorder_Record_Happy"], "annotation": "8-column persistence"}`.
    - Add `{"fr_id": "FR6", "test_ids": ["TestRunStore_RecordStageObservation_NullableOverwrite", "TestRunStore_RecordStageObservation_HumanOverrideSticky"], "annotation": "per-stage observability captured via Recorder"}`.
    - Update `meta.last_updated` to today's ISO date.
    - Update `meta.total_frs` if the project tracks it (currently 48; check before changing).
  - [x] If a contract test asserts `total_frs` against a count of unique `fr_id` values in `coverage[]`, update it; otherwise no other changes.

- [x] **T13: Doc + sample config refresh** (AC: #5)
  - [x] Update `.env.example` with `# COST_CAP_PER_RUN is loaded from config.yaml, not env (env is secrets only).` (one-line note, no actual env var).
  - [x] Update `cmd/pipeline/init.go` (or whatever helper writes the sample config) to include the new `cost_cap_per_run: 5.00` key with the comment from T4.
  - [x] Add a one-line entry to `docs/cli-diagnostics.md` referencing the new config field as the per-run circuit breaker.

- [x] **T14: Lint + green build** (AC: #15, #18)
  - [x] Run `go build ./...`, `go test -race ./...`, `make lint-layers`. All must pass with zero changes to existing tests.
  - [x] Confirm no new package needs a layer-lint rule (`internal/llmclient` already declares `retry.go` + `_test.go`; `internal/pipeline` already permits the relevant edges).
  - [x] Run a quick smoke: `go test ./internal/db/... -run TestRunStore_RecordStageObservation -v` to validate the migration auto-applies and no schema drift slipped in.
  - [x] Update `_bmad-output/implementation-artifacts/sprint-status.yaml`: flip `2-4-per-stage-observability-cost-tracking: backlog` → `ready-for-dev` (handled by the create-story workflow finalize step — verify).

- [x] **T15: Deferred work logging**
  - [x] As implementation surfaces issues that are out-of-scope for this story (see Dev Notes "Deferred Work This Story May Generate"), append them to `_bmad-output/implementation-artifacts/deferred-work.md` under a new `## Deferred from: code review of 2-4-per-stage-observability-cost-tracking (YYYY-MM-DD)` section. Examples to expect: rand seeding through clock; per-shot cost attribution (currently rolled into stage); accumulator persistence across process restart (V1 reconstructs from `runs.cost_usd` on Resume).

---

## Dev Notes

### Why the 8 Columns Live on the `runs` Row, Not a New Table

Architecture [architecture.md:202], [architecture.md:472-493], and FR6 [epics.md:28] all describe the 8-column observability surface as **on the `runs` table itself**. There is no per-stage history table in V1. The semantic compromise:

- The `runs` row is the **cumulative aggregate** across all stages of one run.
- "Per-stage" semantics come from the **call site**: every stage completion calls `Recorder.Record(...)`, which adds to the row.
- Where one stage "overwrites" another (`retry_reason`, `critic_score`), last-write-wins is intentional — operators care about the latest reason a stage retried, not the full retry history (Story 2.7 will add a metrics CLI report that derives history from the rolling-window queries against a populated DB).

The trade-off: V1 cannot show "cost by stage" for a single run. That's an explicit V1.5 deferral (see [architecture.md:455-460] — "deferred decisions"). Per-stage cost breakdown across runs is supported via the `idx_runs_stage` index + diagnostic query #4.

### `retry_reason` Nullability Semantics

Three states for `runs.retry_reason`:

1. `NULL` — never retried, OR explicitly reset by Resume (Story 2.3 sets it to NULL on resume entry).
2. Non-NULL string — last retry's reason (`"rate_limit"`, `"timeout"`, `"stage_failed"`, `"anti_progress"` once Story 2.5 lands).
3. The Recorder respects: `obs.RetryReason == nil` means "no overwrite, leave the prior value alone". This requires the SQL UPDATE to use `COALESCE(?, retry_reason)` instead of plain `?` for that column, OR to omit the column from the SET list when nil.

**Decision:** Use `COALESCE(?, retry_reason)` for the `retry_reason` column. This way the column behaves intuitively: nil obs → no change, non-nil obs → overwrite. Same applies to `critic_score`. Document this in the SQL comment of `RecordStageObservation`.

### `human_override` Sticky-OR Semantics

`human_override` becomes 1 the first time any stage observation reports `true` and **never reverts** to 0 within the same run. This is the "did a human ever touch this run?" audit bit. Implementation: `human_override = (human_override | ?)` in the UPDATE — SQLite supports the bitwise-OR operator on INTEGER columns. (`COALESCE` doesn't apply because `human_override` is NOT NULL DEFAULT 0.) Test: call once with true → 1; call again with false → still 1.

### The Cost Accumulator Is In-Memory and NOT Reconstructed

V1 design: the `CostAccumulator` lives in the engine's process memory. On a process restart mid-run, the accumulator is rebuilt by reading `runs.cost_usd` and seeding the run total directly; per-stage totals are LOST and start at zero. This is acceptable because:

1. The per-stage cap is a **per-stage-execution** circuit breaker. After a restart, the failed stage will be re-entered via Story 2.3's Resume, which clears partial work and starts the stage fresh — the per-stage cap should likewise reset to zero for the new attempt.
2. The per-run cap survives because it's read from `runs.cost_usd` (DB-backed).

This implies the engine wiring (when Story 3.1 lands) does:
```go
costs := pipeline.NewCostAccumulator(perStageCaps, cfg.CostCapPerRun)
costs.PrimeRunTotal(run.CostUSD) // optional helper — see "Deferred Work This Story May Generate"
```

For Story 2.4 we do NOT add `PrimeRunTotal` — it's a Story 3.x concern. We DO design the accumulator's API so the addition is non-disruptive (a `PrimeRunTotal(total float64)` method or a 3rd constructor arg). Document this in the cost.go header comment.

### Why `Record` Persists on Cap-Exceeded (NFR-C3)

The naive design is "cost cap exceeded → don't write to DB." That's wrong. NFR-C3 [epics.md:85] is explicit: "Cost data (cost_usd, token_in, token_out) is captured per stage in pipeline_runs with **no sampling or truncation**." The over-cap call already happened (the API was hit, money was spent); the DB MUST reflect that spend. The cap-exceeded error is the **next-call gate**, not the **write gate**.

Order of operations in `Recorder.Record`:
1. `obs.Validate()` — rejects malformed observations (returns ErrValidation, no write).
2. `costs.Add(...)` — updates the in-memory accumulator regardless of cap (NFR-C3).
3. `store.RecordStageObservation(...)` — persists regardless of cap (NFR-C3).
4. Return `costErr` (if any) wrapped — caller knows to halt subsequent calls.

A reviewer who sees "cap exceeded but DB still wrote" should trace back to NFR-C3 before flagging it.

### Why Cost-Cap Check Is Inside `Add`, Not a Separate `CheckBudget` Pre-flight

Architecture [architecture.md:201, 124] consistently describes the accumulator as "circuit breaker" — active enforcement, not advisory. Two choices:

- **Pre-flight** `CheckBudget(stage, estimatedCost) error`: caller estimates cost before the API call, refuses to call if over.
- **Post-call** `Add(stage, actualCost) error`: caller makes the call, records the cost, gets an error to halt **future** calls.

We pick post-call because (a) cost estimation is unreliable (LLM provider responses vary by ±50% in tokens), (b) NFR-C3 demands the actual spend is recorded — we can't refuse to record it just because it would exceed the cap, and (c) the architecture's circuit-breaker analogy is post-fault: a breaker trips after a fault is detected, then prevents future faults. The accumulator's `Tripped()` method is the breaker's open-state.

If a caller wants pre-flight semantics, it can call `acc.RunTotal() + estimate < cap` defensively before issuing the call. We don't add this as a method to keep the accumulator's API minimal.

### `WithRetry` Is the 429 Gate, Not the Stage Driver

Architecture [architecture.md:867-886] shows `WithRetry` as a thin wrapper around a `func() error`. This story builds the wrapper; Story 5.1 adds the semaphore + token bucket; Story 3.1 wires it into the agent chain.

For Story 2.4, `WithRetry` exists primarily to:
- Provide the deterministic FakeClock-driven test path for AC-OBS-NFR-P3.
- Land the `RetryReasonFor` classifier where future stage executors can call it.
- Avoid the temptation in Story 5.1 to bypass observability ("just retry, log nothing") — by making `onRetry` a required callback, the retry path is forced to flow through `Recorder.RecordRetry`.

The integration test in T10 demonstrates the full chain: real RunStore + Recorder + Accumulator + Clock → fake LLM call → 429 → backoff → success → DB shows the 2 retries with `retry_reason="rate_limit"` and stage/status unchanged.

### State Machine Untouched

Resume (Story 2.3) is the only code path that mutates `runs.status`. This story does NOT call `SetStatus`. It only increments observability columns. The "stage status does not advance on 429" guarantee comes from the absence of `SetStatus(StatusFailed, ...)` in the retry-on-429 path — by design, not by guard.

If a future change adds a `Recorder.SetStatus(...)` method or similar, this NFR-P3 invariant breaks. Add a comment to that effect at the top of `observability.go`: "This package never advances runs.status. Status transitions belong to engine.Resume / engine.Advance."

### Migration 003 Index Choices

| Index | Query target | Why |
|---|---|---|
| `idx_runs_created_at` | "Today's spend", "rolling 7d cost", "rolling 90d failure rate" | All operator queries filter on `created_at`. SQLite's default `rowid` does not help. |
| `idx_runs_status_created_at` | "Failed runs in last N days" | Composite index supports `WHERE status='failed' AND created_at > ?` without a per-row status check. Order of columns: most-selective first; status is more selective in V1 (operators query specific statuses). |
| `idx_runs_stage` | "Per-stage cost breakdown" | `GROUP BY stage` benefits from a covering index on stage. |

**Not indexed** — and intentionally so:
- `cost_usd` — aggregated via SUM, never as a WHERE predicate. An index on a frequently-updated REAL column adds write overhead without query benefit.
- `retry_reason` — low-cardinality (a handful of distinct values); SQLite's planner is fine with a SCAN here for the ≤ 100s of failed runs in V1.
- `human_override` — boolean, low-cardinality, same reasoning.

### Why `EXPLAIN QUERY PLAN` String-Match in T7

SQLite's `EXPLAIN QUERY PLAN` returns text rows like `SEARCH runs USING INDEX idx_runs_created_at (created_at>?)`. The brittle path is regex matching the full plan; the durable path is asserting `strings.Contains(plan, "USING INDEX")` — any index satisfies the assertion. Future index renames don't break the test. If the planner switches to `SCAN runs`, the assertion fails and the developer is forced to investigate.

This is the same trade-off architecture [architecture.md:1450-1452] makes for golden tests of LLM agent outputs: assert structure (an index is used), not text (the specific plan).

### NFR-O3 Is Tested by Existence, Not Performance

NFR-O3 (SQLite CLI sufficient for diagnosis) is fundamentally an **ergonomics** requirement. We can't unit-test "an operator finds this query intuitive". The proxy test is: every canonical diagnostic query in `docs/cli-diagnostics.md` is executable as a raw SQL string with no JOIN, no JSON1, no Go-side post-processing. If a future schema change forces a JOIN into the query, the test in T9 fails — which is the right behavior, because then NFR-O3 is no longer satisfied and the operator docs need re-authoring.

### Concurrency Posture (Story 2.4 vs Story 5.1)

V1 Phase B has parallel image + TTS tracks ([architecture.md:695-738]). Both tracks share one `CostAccumulator` instance and one `Recorder` instance. Therefore:

- `CostAccumulator.Add` MUST be mutex-protected (already in AC-COST-ACCUMULATOR).
- `Recorder.Record` MUST be safe to call from two goroutines for the same run ID. The accumulator's mutex covers the cost arithmetic; the SQLite single-writer guarantee (`MaxOpenConns=1`) covers the DB UPDATE serialization. So the Recorder itself doesn't need its own mutex.
- `RetryReasonFor` is pure (no state) — concurrency-trivial.
- `WithRetry` is per-call-site — the `func() error` argument decides its own concurrency.

Race detector (`go test -race`) MUST pass on the new tests. T3's `TestCostAccumulator_ConcurrentAdds` is the explicit race-coverage test.

### Previous Story Learnings Applied

From 2.1:
- Stage / Event / Status enums are stable; reuse `Stage.IsValid()` in `StageObservation.Validate`.
- Pure-function preference: `RetryReasonFor` is pure; no DB, no clock.

From 2.2:
- `domain.Classify` is the single error classifier; the API handler uses `writeDomainError`. New errors from this story (cost cap, validation) all flow through the same path.
- Snake_case JSON everywhere — `domain.StageObservation` JSON tags are snake_case.
- Module path `github.com/sushistack/youtube.pipeline`.
- CGO_ENABLED=0.

From 2.3:
- Local interface declarations in `pipeline/` (not importing `service/`) — apply to `Recorder`'s `ObservationStore`.
- `testutil.BlockExternalHTTP(t)` in every new test file (paranoid habit).
- `testutil.NewTestDB(t)` is the only way to build a test DB (don't reinvent).
- Migration auto-applies via `db.Migrate` + `embed.FS` — no Go-side wiring change needed for migration 003.
- Updated_at trigger from migration 002 advances on any UPDATE — Story 2.4's UPDATE inherits this for free; no test needed for it (already covered in 2.3).
- Resume (Story 2.3) sets `retry_reason = NULL` on entry. This story respects that: when Recorder is invoked after Resume, `retry_reason` is back to NULL until the first new failure happens.
- Layer-lint rules already permit the relevant edges; no script changes.

### Deferred Work Awareness (Do Not Resolve Here)

- `BlockExternalHTTP` global mutation (1.4 / 1.7 / 2.1 / 2.2 / 2.3): use as-is, don't fix.
- `Migrate` + `PRAGMA user_version` outside transaction (1.4): don't fix; Migration 003 inherits the same risk.
- `NextStage` doesn't distinguish unknown vs invalid (2.1): not relevant to this story.
- `StatusForStage` returns `StatusRunning` for unknown stages (2.1): not relevant — Recorder never calls it.
- Vite dev-mode bypasses middleware (2.2): not relevant.
- `newRequestID` fallback to "unknown" (2.2): not relevant.

### Deferred Work This Story May Generate (Log in T15 / Code Review)

- **Cost accumulator priming on Resume:** when Story 3.1 wires the engine, the accumulator must be primed with `runs.cost_usd` to maintain per-run cap continuity across restarts. We did not add `PrimeRunTotal` here because it would be dead code until Story 3.1.
- **Per-stage cost history:** V1 has no per-stage cost breakdown for one run (only across runs). Story 2.7's metrics CLI may surface this as a V1.5 gap if operators ask for it.
- **Jitter through clock:** `WithRetry` uses `math/rand` with time-seeded source. For full determinism in tests, jitter should flow through `clock.Clock` (e.g. `clk.Rand()`). Out of scope here; acceptable because the test asserts elapsed time *bounds*, not exact values.
- **`COALESCE` semantics for retry_reason:** the "nil = no overwrite" choice is opinionated. An alternative would be a separate `ClearRetryReason()` method. Keep COALESCE for V1; revisit if confusion arises.
- **`docs/cli-diagnostics.md` is freestanding:** no project-wide docs index links to it yet. Acceptable; Story 1.7+ may consolidate.
- **No emergency cap override:** if an operator wants to push past `CostCapPerRun` for a single critical run, they must edit config.yaml. A `--ignore-cost-cap` CLI flag is a Story 10.x concern.
- **Per-shot cost attribution in Phase B:** Recorder accepts one observation per call; if a Phase B image track wants to record cost per shot, it calls Record N times with `Stage=StageImage`. The aggregate sum will be correct; no per-shot row is preserved (consistent with V1's "no per-stage history" decision).

### Project Structure After This Story

```
internal/
  domain/
    observability.go               # NEW — StageObservation + helpers
    observability_test.go          # NEW
    config.go                      # MODIFIED — add CostCapPerRun field + default
    config_test.go                 # MODIFIED — assert new default
  db/
    run_store.go                   # MODIFIED — add RecordStageObservation
    run_store_test.go              # MODIFIED — observability accumulation tests
    observability_query_test.go    # NEW — EXPLAIN QUERY PLAN + diagnostic queries
    diagnostic_query_test.go       # NEW (or merged into observability_query_test.go)
  pipeline/
    cost.go                        # NEW — CostAccumulator + circuit breaker
    cost_test.go                   # NEW
    observability.go               # NEW — Recorder + ObservationStore interface
    observability_test.go          # NEW
    cost_integration_test.go       # NEW — cap hard-stop integration
    observability_integration_test.go  # NEW (or merged into above) — 429 backoff path
  llmclient/
    retry.go                       # NEW — RetryReasonFor + WithRetry
    retry_test.go                  # NEW
cmd/pipeline/
  init.go                          # MODIFIED — emit cost_cap_per_run in sample config
migrations/
  003_observability_indexes.sql    # NEW
testdata/
  fixtures/
    observability_seed.sql         # NEW
    running_at_write.sql           # NEW
docs/
  cli-diagnostics.md               # NEW
.env.example                       # MODIFIED — note new config key (env-only contains secrets)
testdata/fr-coverage.json          # MODIFIED — add FR5, FR6 entries
```

### Critical Constraints

- **Cost data is ALWAYS persisted** (NFR-C3) — even on cap-exceeded. Do NOT add a "skip write if tripped" early-return.
- **Cost cap check happens during Add, not after** — the integration tests in T11 pin this behavior.
- **429 NEVER advances stage status** (NFR-P3) — the integration test in T10 pins this. No SetStatus call in the retry path.
- **`Recorder.Record` is NOT idempotent** (AC-IDEMPOTENCY-WRITES) — calling twice doubles the columns. Document explicitly.
- **`pipeline/` does not import `service/`** — declare `ObservationStore` interface locally in `pipeline/observability.go`.
- **No testify, no gomock** — inline fakes + `testutil.AssertEqual[T]`.
- **snake_case JSON** for all new domain fields.
- **Module path** `github.com/sushistack/youtube.pipeline`. **CGO_ENABLED=0.** **`testutil.BlockExternalHTTP(t)` in every new test file.**
- **Single-row UPDATE** for `RecordStageObservation` — no transaction wrapper (`MaxOpenConns=1` makes UPDATE atomic). Document the reasoning in a code comment.
- **Migration 003 is `IF NOT EXISTS`** — safe on every DB regardless of state.
- **Sticky `human_override`** uses bitwise-OR; `retry_reason` and `critic_score` use `COALESCE` for nil-means-no-overwrite.
- **`WithRetry` aborts on `ErrCostCapExceeded`** without sleeping — verified by `TestWithRetry_CostCapBypassesRetries`.
- **All numeric `obs` fields rejected if negative** by `Validate` — DB never sees a negative cost.

### References

- Epic 2 scope and FRs: [epics.md:378-399](../planning-artifacts/epics.md#L378-L399)
- Story 2.4 AC (BDD): [epics.md:1003-1037](../planning-artifacts/epics.md#L1003-L1037)
- FR5/FR6 (8-column observability): [epics.md:27-29](../planning-artifacts/epics.md#L27-L29)
- NFR-C1 / NFR-C2 / NFR-C3 (cost caps + telemetry): [epics.md:83-85](../planning-artifacts/epics.md#L83-L85)
- NFR-O1 / NFR-O3 / NFR-O4 (observability + indexes): [epics.md:101-103](../planning-artifacts/epics.md#L101-L103)
- NFR-P3 (429 backoff + retry_reason): [epics.md:81](../planning-artifacts/epics.md#L81)
- 8-column schema (canonical): [architecture.md:472-493](../planning-artifacts/architecture.md#L472-L493)
- Cost-tracking circuit-breaker pattern: [architecture.md:124, 201-202](../planning-artifacts/architecture.md#L124)
- Retry + clock interface skeleton: [architecture.md:867-886](../planning-artifacts/architecture.md#L867-L886)
- Error classification (RATE_LIMITED, COST_CAP_EXCEEDED): [architecture.md:612-622](../planning-artifacts/architecture.md#L612-L622)
- Domain sentinel errors: [internal/domain/errors.go](../../internal/domain/errors.go)
- Existing Run struct (8 obs columns already present): [internal/domain/types.go:111-128](../../internal/domain/types.go#L111-L128)
- Existing PipelineConfig (extend with CostCapPerRun): [internal/domain/config.go](../../internal/domain/config.go)
- Existing RunStore (extend with RecordStageObservation): [internal/db/run_store.go](../../internal/db/run_store.go)
- Migration 002 trigger (advances updated_at on any UPDATE): [migrations/002_updated_at_trigger.sql](../../migrations/002_updated_at_trigger.sql)
- Migration runner (auto-discovers via embed.FS): [internal/db/migrate.go](../../internal/db/migrate.go)
- Clock interface (FakeClock for deterministic backoff tests): [internal/clock/clock.go](../../internal/clock/clock.go)
- Layer-lint rules (no edits required): [scripts/lintlayers/main.go:21-33](../../scripts/lintlayers/main.go#L21-L33)
- testutil.NewTestDB: [internal/testutil/db.go](../../internal/testutil/db.go)
- testutil.LoadRunStateFixture: [internal/testutil/fixture.go](../../internal/testutil/fixture.go)
- testutil.BlockExternalHTTP: [internal/testutil/nohttp.go](../../internal/testutil/nohttp.go)
- testutil.CaptureLog (slog assertion helper): [internal/testutil/slog.go](../../internal/testutil/slog.go)
- Sprint review checkpoints (nullable handling, mid-accumulation cap check, 429 stage-status invariant, FakeClock determinism, NFR-O4 indexes): [sprint-prompts.md:300-307](../planning-artifacts/sprint-prompts.md#L300-L307)
- FR coverage tracker: [testdata/fr-coverage.json](../../testdata/fr-coverage.json)
- Deferred work registry: [deferred-work.md](deferred-work.md)
- Previous story (2.3): [2-3-stage-level-resume-artifact-lifecycle.md](2-3-stage-level-resume-artifact-lifecycle.md)
- Previous story (2.2): [2-2-run-create-cancel-inspect.md](2-2-run-create-cancel-inspect.md)
- Previous story (2.1): [2-1-state-machine-core-stage-transitions.md](2-1-state-machine-core-stage-transitions.md)

## Dev Agent Record

### Agent Model Used

claude-opus-4-7

### Debug Log References

None.

### Completion Notes List

- `internal/domain/observability.go` (new): `StageObservation` struct + `NewStageObservation` + `Validate` + `IsZero`. JSON tags are snake_case. `Validate` rejects unknown stage and any negative numeric delta (returns wrapped `ErrValidation`).
- `internal/db/run_store.go` (modified): `RecordStageObservation` appends via a single UPDATE — `cost_usd`, `token_in`, `token_out`, `duration_ms`, `retry_count` accumulate; `retry_reason` and `critic_score` use `COALESCE(?, col)` so `nil` preserves prior; `human_override` is `(col | ?)` sticky-OR; `RowsAffected()==0` → `ErrNotFound`. Pre-SQL `obs.Validate()` guarantees no malformed write.
- `internal/pipeline/cost.go` (new): `CostAccumulator` with per-stage + per-run caps. Mutex-protected, goroutine-safe. `Add` records the spend **then** checks caps (NFR-C3: never drop cost). Once tripped, stays tripped; subsequent Adds still record AND return wrapped `ErrCostCapExceeded`. Error message includes stage, reason, actual, cap for operator diagnosis.
- `internal/domain/config.go` (modified): added `CostCapPerRun float64` with yaml/mapstructure tags. `DefaultConfig()` sets `5.00` — larger than sum of per-stage caps ($4.10) so per-stage caps remain the primary guardrail. `config_test.go` asserts both the positive default and the sum invariant.
- `internal/pipeline/observability.go` (new): `ObservationStore` local interface (satisfied by `*db.RunStore` structurally) + `Recorder`. `Record` order: Validate → `costs.Add` → `store.RecordStageObservation` → slog. Persistence runs regardless of cap error (NFR-C3). Joined error via `errors.Join` when both cost and DB fail. `RecordRetry` is a zero-cost convenience for the 429 path.
- `internal/llmclient/retry.go` (new): `RetryReasonFor(err)` is `errors.Is`-based, maps `ErrRateLimited→"rate_limit"`, `ErrUpstreamTimeout→"timeout"`, `ErrStageFailed→"stage_failed"`, everything else (including `ErrCostCapExceeded`) → `""`. `WithRetry(ctx, clk, maxRetries, fn, onRetry)` uses clock-driven backoff (1s, 2s, 4s, ..., capped at 30s) + ±0.5s jitter. `ErrCostCapExceeded` short-circuits without sleeping.
- `migrations/003_observability_indexes.sql` (new): `idx_runs_created_at`, `idx_runs_status_created_at`, `idx_runs_stage` — all `IF NOT EXISTS`. Rationale documented in the SQL file header (including why `cost_usd` is *not* indexed).
- `testdata/fixtures/observability_seed.sql` (new): 60 runs — 42 completed, 9 failed, 6 cancelled, 3 running. `created_at` spans -1..-110 days. Cost values hard-coded. 5 low-critic-score rows, 3 `human_override=1` rows. Deterministic; distributions pinned by `TestSeedFixture_Distribution`.
- `testdata/fixtures/running_at_write.sql` (new): single run at `stage=write, status=running` for the 429 integration test.
- `internal/db/observability_query_test.go` (new): confirms Migration 003 ran (`TestMigration003_IndexesCreated`) + asserts the three canonical rolling-window queries use indexes via `EXPLAIN QUERY PLAN` → `USING INDEX` substring.
- `internal/db/diagnostic_query_test.go` (new): exercises 5 operator-facing CLI queries (mirrored verbatim in `docs/cli-diagnostics.md`). Every query is raw SQL on the `runs` table alone — if a schema change forces a JOIN, this test fails loudly.
- `docs/cli-diagnostics.md` (new, Korean per `communication_language`): operator reference for the NFR-O3 diagnostic queries. Linked to the Migration 003 indexes.
- `internal/pipeline/observability_integration_test.go` (new): `TestIntegration_429Backoff_DoesNotAdvanceStage` pins NFR-P3 — a 429 retry flow records `retry_reason="rate_limit"` on the runs row WITHOUT calling `SetStatus` (verified via a counting wrapper around `*db.RunStore`). `TestIntegration_NonRetryableError_Bypasses429Path` pins that `ErrCostCapExceeded` short-circuits without touching observability.
- `internal/pipeline/cost_integration_test.go` (new): `TestRecorder_PerStageCap_HardStop` pins NFR-C1 + NFR-C3 (over-cap cost is persisted). `TestRecorder_PerRunCap_HardStop` pins NFR-C2. `TestRecorder_PerStageCap_StatusNotAdvanced` pins that Recorder never mutates `runs.status`.
- `testdata/fr-coverage.json` (modified): added `FR5` (8-column observability persistence via Recorder) and `FR6` (per-stage observability captured) with the relevant test IDs; `meta.last_updated` advanced.
- `.env.example` (modified): added a one-line note that cost caps live in `config.yaml`, not env.
- `cmd/pipeline/init.go`: no code change — `writeConfigIfNotExists` marshals `domain.DefaultConfig()` so `cost_cap_per_run: 5` appears in the generated sample config automatically.
- **NFR-C3 (no cost truncation):** pinned by `TestCostAccumulator_TrippedStaysTripped` + `TestRecorder_Record_CapExceeded_StillPersists` + `TestRecorder_PerStageCap_HardStop` (asserts `runs.cost_usd = 0.60` after the over-cap $0.20 add that tripped the $0.50 stage cap).
- **NFR-P3 (429 stage-status invariance):** pinned by `TestIntegration_429Backoff_DoesNotAdvanceStage` via a wrapper that counts `SetStatus` invocations (must be 0). `runs.stage` and `runs.status` are unchanged pre/post; `retry_count=2` and `retry_reason="rate_limit"` are recorded.
- **NFR-O3 (SQLite CLI sufficiency):** pinned by `TestDiagnosticQueries_NFR_O3` — every canonical diagnostic query is raw SQL on `runs` alone.
- **NFR-O4 (rolling-window queries without full scans):** pinned by `TestRollingWindowQueries_UseIndexes` — `EXPLAIN QUERY PLAN` must contain `USING INDEX` for all three query shapes.
- **Layer lint:** `make lint-layers` clean with no `scripts/lintlayers/main.go` edits. `internal/llmclient → {domain, clock}` and `internal/pipeline → {domain, db, llmclient, clock}` already covered the new edges.
- **Full CI-go equivalent green:** `make test-go lint-layers check-fr-coverage` all pass with `CGO_ENABLED=0`. 9 FRs mapped, 4 annotated, 39 unmapped (grace mode).
- **Linter-induced Story 2.3 refactors absorbed:** `RunStore.SetStatus + IncrementRetryCount` collapsed into `ResetForResume`; `SetStage` was deleted; `pipeline.Engine.Resume` now returns `(*InconsistencyReport, error)`; `segments.ClearClipPathsByRunID` was added for assemble-stage resume. Callers (`cmd/pipeline/resume.go`, `internal/api/handler_run.go`, `internal/service/run_service.go`) were rewired to thread the report through as `warnings`. Full set of prior 2.3 tests still green. Outside-scope detail logged in `_bmad-output/implementation-artifacts/deferred-work.md` for visibility in the next code review.

### Change Log

- 2026-04-18: Story 2.4 implemented — 8-column per-stage observability persistence, cost accumulator circuit breaker (per-stage + per-run), 429-aware retry classifier with FakeClock-deterministic backoff, Migration 003 indexes for rolling-window queries, and operator-facing SQLite CLI diagnostic queries documented in `docs/cli-diagnostics.md`.

### File List

- internal/domain/observability.go (new)
- internal/domain/observability_test.go (new)
- internal/domain/config.go (modified — CostCapPerRun)
- internal/domain/config_test.go (modified — CostCapPerRun default assertion)
- internal/db/run_store.go (modified — RecordStageObservation)
- internal/db/run_store_test.go (modified — 5 new RecordStageObservation tests)
- internal/db/observability_query_test.go (new)
- internal/db/diagnostic_query_test.go (new)
- internal/db/sqlite_test.go (modified — user_version expectation 2 → 3 for Migration 003)
- internal/pipeline/cost.go (new)
- internal/pipeline/cost_test.go (new)
- internal/pipeline/observability.go (new)
- internal/pipeline/observability_test.go (new)
- internal/pipeline/observability_integration_test.go (new — NFR-P3)
- internal/pipeline/cost_integration_test.go (new — NFR-C1/C2)
- internal/llmclient/retry.go (new)
- internal/llmclient/retry_test.go (new)
- migrations/003_observability_indexes.sql (new)
- testdata/fixtures/observability_seed.sql (new)
- testdata/fixtures/running_at_write.sql (new)
- testdata/fr-coverage.json (modified — FR5, FR6, last_updated)
- docs/cli-diagnostics.md (new)
- .env.example (modified — cost_cap_per_run reference note)
- internal/pipeline/resume.go (modified — linter refactor: Resume returns `(*InconsistencyReport, error)`, Phase B cleans both tracks, assemble calls ClearClipPathsByRunID, ResetForResume replaces SetStatus+IncrementRetryCount)
- internal/pipeline/resume_test.go (modified — adapter for resetCalls + 2-value Resume signature + ClearClipPathsByRunID fake)
- internal/pipeline/resume_integration_test.go (modified — 2-value Resume signature)
- internal/service/run_service.go (modified — Resumer signature returns report; Resume returns `(*Run, *InconsistencyReport, error)`)
- internal/service/run_service_test.go (modified — fakeResumer signature + 3-value Resume)
- internal/api/handler_run.go (modified — linter-introduced decodeJSONBody + resumeResponse with Warnings)
- cmd/pipeline/resume.go (modified — consume 3-value Resume; populate ResumeOutput.Warnings from InconsistencyReport)
- _bmad-output/implementation-artifacts/deferred-work.md (modified — Story 2.4 deferred items appended)
- _bmad-output/implementation-artifacts/sprint-status.yaml (modified — 2-4 status flipped ready-for-dev → in-progress → review)
