# Story 2.7: Pipeline Metrics CLI Report (Day-90 Gate)

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want to view rolling-window pipeline metrics via a CLI command,
so that I can evaluate Day-90 acceptance gates and diagnose quality trends without external tooling.

## Acceptance Criteria

Unless stated otherwise, new tests follow the project's `TestXxx_CaseName` convention, live beside the code under test, call `testutil.BlockExternalHTTP(t)`, and use inline fakes + `testutil.AssertEqual[T]` (no testify, no gomock). Module path `github.com/sushistack/youtube.pipeline`. CGO_ENABLED=0.

1. **AC-DOMAIN-METRICS-TYPES:** `internal/domain/metrics.go` (NEW) declares the V1 metric surface:

    ```go
    // MetricID enumerates the five Day-90 pipeline metrics (PRD §Technical Success).
    // Stored as string for stable JSON output; do NOT reorder — external tooling
    // may key off these IDs.
    type MetricID string

    const (
        MetricAutomationRate              MetricID = "automation_rate"
        MetricCriticCalibration           MetricID = "critic_calibration"
        MetricCriticRegressionDetection   MetricID = "critic_regression_detection"
        MetricDefectEscapeRate            MetricID = "defect_escape_rate"
        MetricResumeIdempotency           MetricID = "resume_idempotency"
    )

    // MetricComparator describes the direction of the Day-90 target comparison.
    // "gte" means pass iff value >= target (higher-is-better: automation_rate,
    // critic_calibration, critic_regression_detection, resume_idempotency);
    // "lte" means pass iff value <= target (lower-is-better: defect_escape_rate).
    type MetricComparator string

    const (
        ComparatorGTE MetricComparator = "gte"
        ComparatorLTE MetricComparator = "lte"
    )

    // Metric is one row in the metrics report. Value is nil iff Unavailable is true
    // (data absent — e.g., no decisions, no Critic scores). A provisional window
    // still reports Value.
    type Metric struct {
        ID          MetricID          `json:"id"`
        Label       string            `json:"label"`
        Value       *float64          `json:"value"`            // nil when Unavailable
        Target      float64           `json:"target"`
        Comparator  MetricComparator  `json:"comparator"`
        Pass        bool              `json:"pass"`             // false when Unavailable OR Value fails comparator
        Unavailable bool              `json:"unavailable"`      // true when the underlying data does not exist yet
        Reason      string            `json:"reason,omitempty"` // short human note when Unavailable
    }

    // MetricsReport is the CLI payload envelope (wrapped by render.Envelope as data).
    type MetricsReport struct {
        Window      int      `json:"window"`       // requested rolling-window size
        WindowCount int      `json:"window_count"` // actual completed runs counted (<= Window)
        Provisional bool     `json:"provisional"`  // true iff WindowCount < Window
        Metrics     []Metric `json:"metrics"`      // 5 rows, stable order matching MetricID constants above
        GeneratedAt string   `json:"generated_at"` // RFC3339 from clock.Clock
    }
    ```

    Order of `Metrics[]` must match the `MetricID` const block (automation, calibration, regression, escape, idempotency) — golden-tested. `Label` is a short human description for the table renderer (see AC-RENDER-HUMAN). Add a unit test `TestMetric_JSONShape` that marshals a zero-populated `MetricsReport` and asserts snake_case keys + the 5-row `metrics` array.

2. **AC-MIGRATION-005-METRICS-INDEXES:** `migrations/005_metrics_indexes.sql` (NEW) adds the NFR-O4 indexes required by metrics queries.

    ```sql
    -- Migration 005: NFR-O4 indexes for Day-90 metrics rolling-window queries.
    --
    -- Story 2.7's pipeline metrics CLI must complete within 1 second for 1000 runs.
    -- Existing indexes (migrations 003/004) cover the `runs` table ordering +
    -- filtering paths (idx_runs_created_at, idx_runs_status_created_at,
    -- idx_runs_retry_reason_created_at, idx_runs_stage). What's still missing:
    --
    --   1. decisions(run_id, decision_type, superseded_by) — the defect-escape
    --      and kappa queries filter by all three and by scene_id; a composite
    --      on (run_id, decision_type) covering superseded_by satisfies both
    --      SARGable predicates + the existing DecisionStore.DecisionCountsByRunID.
    --   2. decisions(created_at) — temporal filters that intersect with the
    --      runs rolling window need to seek directly into the date range.
    --
    -- Rationale omitted for runs table (already covered by 003/004). See
    -- story 2-7 AC-SQL-* for the exact query shapes pinned by EXPLAIN QUERY PLAN.

    CREATE INDEX IF NOT EXISTS idx_decisions_run_id_type
        ON decisions(run_id, decision_type, superseded_by);

    CREATE INDEX IF NOT EXISTS idx_decisions_created_at
        ON decisions(created_at);
    ```

    **Do NOT reuse or rename migration numbers.** `migrations/` already contains two `004_*.sql` files (`004_anti_progress_index.sql`, `004_hitl_sessions.sql`); choose **005** to avoid a third collision. Both existing 004s apply correctly on a fresh DB because `internal/db/migrate.go` reads `current` once at the top of `Migrate()` and does not re-read user_version between migrations, so all files with `ver > initial_user_version` are applied in sorted order. However, you MUST NOT rely on that quirk — version number collisions are a latent bug against incremental migrations and the deferred-work registry flags the issue (see `_bmad-output/implementation-artifacts/deferred-work.md`). Pick 005 and move on.

    Header comment MUST explain why we did NOT add an index on `decisions.scene_id` alone: the kappa/defect-escape queries always join scene_id with a run_id filter, and the composite `(run_id, decision_type, superseded_by)` index satisfies the selective path without the write overhead of a free-floating scene_id index.

    `migrations/embed.go` requires NO edit — the `embed.FS` globs `*.sql` automatically. Verify by diffing a fresh DB against the expected index set in AC-INTEGRATION-INDEXES-PRESENT.

3. **AC-RUNSTORE-METRICS-WINDOW:** `internal/db/run_store.go` gains a method to materialize the rolling window of most-recent completed runs:

    ```go
    // RunMetricsRow is the per-run slice of observability data required by
    // all five Day-90 metrics. One row per completed run in the window.
    // Returned by RecentCompletedRunsForMetrics in created_at-desc order.
    type RunMetricsRow struct {
        ID            string
        Status        string   // always "completed" by construction
        CriticScore   *float64 // nullable — may be unset for Phase A-failed runs
        HumanOverride bool     // aggregate sticky bit across all stages in the run
        RetryCount    int
        RetryReason   *string
        CreatedAt     string
    }

    // RecentCompletedRunsForMetrics returns up to `window` most-recent runs
    // where status = 'completed'. The `completed` filter + created_at DESC
    // ordering matches the rolling-window definition in PRD §Technical Success:
    // metrics are calibrated on finished runs, not cancelled/waiting/running
    // ones (which would skew the averages during active work).
    //
    // Uses idx_runs_status_created_at (Migration 003) for a composite seek —
    // NOT idx_runs_created_at + in-memory filter. EXPLAIN QUERY PLAN MUST
    // show `USING INDEX idx_runs_status_created_at` (AC-SQL-EXPLAIN-WINDOW).
    //
    // Returns (nil, nil) when no completed runs exist. Returns ErrValidation
    // when window <= 0 (mirrors AntiProgressFalsePositiveStats contract).
    func (s *RunStore) RecentCompletedRunsForMetrics(
        ctx context.Context, window int,
    ) ([]RunMetricsRow, error)
    ```

    SQL MUST be:
    ```sql
    SELECT id, status, critic_score, human_override, retry_count, retry_reason, created_at
      FROM runs
     WHERE status = 'completed'
     ORDER BY created_at DESC, id DESC
     LIMIT ?;
    ```

    The `id DESC` secondary sort matches Migration 004's precedent (`idx_runs_retry_reason_created_at` subquery in `AntiProgressFalsePositiveStats`) and is required so tie-broken fixtures produce deterministic order.

    Add `TestRunStore_RecentCompletedRunsForMetrics_ReturnsWindow` (fixture: `metrics_seed` — see AC-FIXTURE-METRICS-SEED), `TestRunStore_RecentCompletedRunsForMetrics_Empty` (empty DB → nil slice + nil error), `TestRunStore_RecentCompletedRunsForMetrics_ValidationError` (window=0 → `ErrValidation`), and `TestRunStore_RecentCompletedRunsForMetrics_UsesIndex` (EXPLAIN QUERY PLAN asserts `USING INDEX idx_runs_status_created_at`; absence of `SCAN runs`).

4. **AC-DECISIONSTORE-METRICS-AGGREGATES:** `internal/db/decision_store.go` gains two methods, each targeted at a specific Day-90 metric. Keep them narrow — do NOT overload `ListByRunID` with new filters.

    ```go
    // KappaPair represents one (critic_verdict, operator_decision) observation
    // for the Cohen's kappa calibration metric. Produced for scenes that have
    // BOTH a Critic verdict (via runs.critic_score + threshold) AND an operator
    // decision in decisions (decision_type in 'approve'|'reject', non-superseded).
    // Approve/reject is the 2×2 cell; kappa is computed over run-aggregated pairs
    // per the V1 spec (one pair per reviewed scene, scoped to the window).
    type KappaPair struct {
        CriticPass      bool // true iff runs.critic_score >= calibrationThreshold (arg)
        OperatorApprove bool // true iff decision_type = 'approve' (reject → false)
    }

    // KappaPairsForRuns returns one pair per (run, scene) where:
    //   - run.id ∈ runIDs (the window)
    //   - decisions.scene_id IS NOT NULL and decision_type IN ('approve','reject')
    //     and superseded_by IS NULL
    //   - the run has a non-null critic_score
    //
    // Threshold: CriticPass = (run.critic_score >= calibrationThreshold).
    // V1 uses 0.70 as the pass threshold (see AC-SERVICE-KAPPA rationale).
    //
    // Uses idx_decisions_run_id_type (Migration 005) for the decisions-side filter
    // + idx_runs on the IN-clause runs. EXPLAIN QUERY PLAN assertion in
    // TestDecisionStore_KappaPairsForRuns_UsesIndex.
    //
    // Returns (nil, nil) when no pairs exist.
    func (s *DecisionStore) KappaPairsForRuns(
        ctx context.Context,
        runIDs []string,
        calibrationThreshold float64,
    ) ([]KappaPair, error)

    // DefectEscape represents one scene where the Critic auto-passed (above
    // threshold) but the operator subsequently rejected it. Used to compute
    // MetricDefectEscapeRate = escapes / auto_passed_scenes in the window.
    type DefectEscape struct {
        AutoPassedScenes int // count of scenes auto-passed by Critic in the window
        EscapedScenes    int // of those, subsequently rejected by operator
    }

    // DefectEscapeInRuns returns the (auto_passed, escaped) tallies for the
    // given window. A scene is "auto-passed" when segments.critic_score >=
    // calibrationThreshold (0.70 in V1). A scene is "escaped" when the same
    // (run_id, scene_id) has a non-superseded decision with decision_type='reject'.
    //
    // Uses idx_decisions_run_id_type (Migration 005) + segments UNIQUE(run_id,
    // scene_index). EXPLAIN QUERY PLAN assertion in
    // TestDecisionStore_DefectEscapeInRuns_UsesIndex.
    //
    // Returns DefectEscape{0, 0} when no segments have critic_score >= threshold.
    func (s *DecisionStore) DefectEscapeInRuns(
        ctx context.Context,
        runIDs []string,
        calibrationThreshold float64,
    ) (DefectEscape, error)
    ```

    Rationale for NOT folding these into `DecisionCountsByRunID`: that helper returns per-run counts used by Story 2.6's session summary; the metrics path wants window-wide aggregates across many runs and different WHERE shapes. Keeping them separate matches the "narrow-purpose method" pattern used by `AntiProgressFalsePositiveStats` vs `List`.

    **Segments table note:** `segments.critic_score` is the per-scene Critic score (migration 001). The run-level `runs.critic_score` is an aggregate and MUST NOT be used for per-scene defect-escape computation. Similarly, kappa in V1 operates on the run-level `runs.critic_score` (one observation per run, paired with the run's dominant operator decision) because V1 doesn't yet have per-scene Critic verdicts wired through the decisions table — see the dev notes for why this is acceptable and how V1.5 upgrades.

    **V1 Simplification callout (CRITICAL — read before implementing):** V1 has `runs.critic_score` (set once at the final Phase A Critic checkpoint) but no `decisions.critic_verdict` column recording each Critic verdict paired with the operator override. This limits V1 kappa calibration to a 2×2 over `(run_critic_pass, dominant_operator_decision)` — one observation per completed run with at least one operator scene decision. The dominant operator decision per run is the mode of `{approve, reject}` decisions for that run's scenes; ties break to `reject` (conservative, avoids over-crediting the Critic). Same holds for defect-escape: a "auto-pass" scene is `segments.critic_score >= 0.70`, and "escape" means a subsequent `reject` decision for that scene. This will feel fragile — it is. V1.5 with full decision-capture (Epic 8) resolves it; until then both metrics will often be `provisional: true` or `unavailable: true`, which is correct behavior per PRD §Success Criteria. **Do not invent richer data sources.**

    Add: `TestDecisionStore_KappaPairsForRuns_FiltersSupersededAndNonScene`, `TestDecisionStore_KappaPairsForRuns_EmptyRunIDs` (returns nil + nil), `TestDecisionStore_DefectEscapeInRuns_CountsOnlyAutoPassedRejects`, `TestDecisionStore_DefectEscapeInRuns_EmptyRunIDs`, `TestDecisionStore_KappaPairsForRuns_UsesIndex`, `TestDecisionStore_DefectEscapeInRuns_UsesIndex`.

5. **AC-SERVICE-METRICS-TYPES:** `internal/service/metrics_service.go` (NEW) composes the five metrics. Layer lint rules [scripts/lintlayers/main.go:27](../../scripts/lintlayers/main.go#L27) already allow `service → db → domain` — no edge additions needed.

    ```go
    package service

    import (
        "context"
        "fmt"
        "time"

        "github.com/sushistack/youtube.pipeline/internal/clock"
        "github.com/sushistack/youtube.pipeline/internal/db"
        "github.com/sushistack/youtube.pipeline/internal/domain"
    )

    // MetricsStore is the narrow consumer interface satisfied by *db.RunStore
    // + *db.DecisionStore via embedding in a local adapter. Declared at the
    // service layer so tests can substitute a fake without touching SQL.
    type MetricsStore interface {
        RecentCompletedRunsForMetrics(ctx context.Context, window int) ([]db.RunMetricsRow, error)
        KappaPairsForRuns(ctx context.Context, runIDs []string, threshold float64) ([]db.KappaPair, error)
        DefectEscapeInRuns(ctx context.Context, runIDs []string, threshold float64) (db.DefectEscape, error)
    }

    // MetricsService computes the Day-90 metrics report.
    type MetricsService struct {
        store MetricsStore
        clk   clock.Clock
    }

    // NewMetricsService constructs a MetricsService.
    func NewMetricsService(store MetricsStore, clk clock.Clock) *MetricsService

    // Report returns the rolling-window metrics report. calibrationThreshold is
    // the critic_score cutoff used to classify Critic pass/fail (V1: 0.70).
    // regressionDetection carries the CI-measured Golden-eval detection rate —
    // the CLI fills this from a file or flag (see AC-CLI-REGRESSION-SOURCE).
    // idempotencyRate is the measured stage-level resume idempotency rate;
    // the CLI fills this from a file or flag (see AC-CLI-IDEMPOTENCY-SOURCE).
    //
    // Returns ErrValidation when window <= 0.
    func (s *MetricsService) Report(
        ctx context.Context,
        window int,
        calibrationThreshold float64,
        regressionDetection *float64,
        idempotencyRate *float64,
    ) (*domain.MetricsReport, error)
    ```

    The service orchestrates:
    1. `RecentCompletedRunsForMetrics(ctx, window)` → window of runs.
    2. Extract `runIDs` slice for downstream queries.
    3. Compute **automation_rate** = (1 − mean(human_override across window)). Unavailable when `WindowCount == 0`. V1 proxy: per-run human_override sticky bit IS the "any stage was operator-overridden" signal; absence (=0) means auto-completed run. The metric approximates PRD's "auto-completed stages / total stages" at run-level granularity because V1 has no per-stage observability rows (see deferred-work.md: "Per-stage cost history for a single run" for the V1.5 expansion path).
    4. Compute **critic_calibration** (Cohen's kappa) from `KappaPairsForRuns`. Unavailable when `len(pairs) == 0` OR when either marginal (critic pass/fail, operator approve/reject) has zero observations (kappa is undefined — the 2×2 degenerates). Formula pinned in AC-SERVICE-KAPPA.
    5. Compute **critic_regression_detection** — Unavailable when `regressionDetection == nil`; otherwise Value = *regressionDetection, Pass = Value >= 0.80.
    6. Compute **defect_escape_rate** from `DefectEscapeInRuns`. Unavailable when `AutoPassedScenes == 0` (cannot compute a rate). Otherwise rate = EscapedScenes / AutoPassedScenes, Pass = rate <= 0.05.
    7. Compute **resume_idempotency** — Unavailable when `idempotencyRate == nil`; otherwise Value = *idempotencyRate, Pass = Value >= 1.0 (per PRD target "100% functional equivalence").

    `GeneratedAt` comes from `s.clk.Now().UTC().Format(time.RFC3339)` — injectable for tests.

    Unit tests: `TestMetricsService_Report_FullWindow` (25 runs, all metrics computed + passing), `TestMetricsService_Report_ProvisionalWhenShort` (10 runs → `Provisional=true`), `TestMetricsService_Report_HandlesEmptyDB` (0 runs → all metrics `Unavailable=true`, `Reason="no completed runs in window"`, `Provisional=true`, `Pass=false`), `TestMetricsService_Report_DegenerateKappa_Unavailable` (all decisions are approves OR all critic scores pass → kappa undefined → Unavailable), `TestMetricsService_Report_ValidationOnZeroWindow`.

6. **AC-SERVICE-KAPPA:** Cohen's kappa formula (2×2) — unit-test-pinned.

    Given a list of `KappaPair` observations:
    ```
    a = count of (CriticPass=true,  OperatorApprove=true)   // TP
    b = count of (CriticPass=true,  OperatorApprove=false)  // FP (Critic over-credited)
    c = count of (CriticPass=false, OperatorApprove=true)   // FN (Critic under-credited)
    d = count of (CriticPass=false, OperatorApprove=false)  // TN
    n = a + b + c + d

    p_o  = (a + d) / n                       // observed agreement
    p_yes = ((a + b) * (a + c)) / (n * n)    // expected "yes" agreement
    p_no  = ((c + d) * (b + d)) / (n * n)    // expected "no" agreement
    p_e  = p_yes + p_no                      // expected agreement by chance
    kappa = (p_o - p_e) / (1 - p_e)
    ```

    Edge cases (Unavailable=true, Value=nil, Reason set):
    - `n == 0`: "no paired observations".
    - `p_e == 1.0` (denominator zero — both raters always agree or always disagree on one class): "degenerate — no variance to calibrate against".
    - `p_o == 1.0 && p_e < 1.0`: emit `kappa = 1.0` normally (perfect agreement).
    - Negative kappa is reported as-is (does not coerce to 0); Pass = (value >= 0.70).

    Threshold for CriticPass: V1 fixes `calibrationThreshold = 0.70` (see [_bmad-output/planning-artifacts/prd.md:271](../planning-artifacts/prd.md#L271)). CLI accepts `--calibration-threshold` (hidden flag, default 0.70) for ad-hoc sensitivity analysis; do NOT advertise in `--help` (defer to UX polish in Epic 10).

    `internal/service/kappa.go` (NEW, or inline function in `metrics_service.go`): `func CohensKappa(a, b, c, d int) (kappa float64, ok bool, reason string)`. Pure function; no DB access; unit-tested with PRD-mentioned target kappa ≥ 0.7 cases + degenerate cases + canonical textbook example (e.g., the Wikipedia Cohen's-kappa 2-rater 50-subject example).

    Test file: `internal/service/kappa_test.go` with `TestCohensKappa_PerfectAgreement`, `TestCohensKappa_Chance`, `TestCohensKappa_KnownTextbookExample` (use the Wikipedia/GitHub example values — include citation comment), `TestCohensKappa_Degenerate_AllOneClass`, `TestCohensKappa_Empty`, `TestCohensKappa_NegativeAgreement`.

7. **AC-CLI-METRICS-COMMAND:** `cmd/pipeline/metrics.go` (NEW) registers the `pipeline metrics` subcommand.

    ```go
    package main

    import (
        "fmt"

        "github.com/spf13/cobra"
        "github.com/sushistack/youtube.pipeline/internal/clock"
        "github.com/sushistack/youtube.pipeline/internal/config"
        "github.com/sushistack/youtube.pipeline/internal/db"
        "github.com/sushistack/youtube.pipeline/internal/service"

        _ "github.com/ncruces/go-sqlite3/driver"
    )

    var (
        metricsWindow               int
        metricsCalibrationThreshold float64
        metricsRegressionFile       string // optional: --regression-rate path to file w/ single float
        metricsIdempotencyFile      string // optional: --idempotency-rate path to file w/ single float
    )

    func newMetricsCmd() *cobra.Command {
        cmd := &cobra.Command{
            Use:   "metrics",
            Short: "Report rolling-window pipeline metrics (Day-90 gate).",
            Args:  cobra.NoArgs,
            RunE:  runMetrics,
        }
        cmd.Flags().IntVar(&metricsWindow, "window", 25, "rolling-window size (number of most-recent completed runs)")
        cmd.Flags().Float64Var(&metricsCalibrationThreshold, "calibration-threshold", 0.70, "Critic pass cutoff used for kappa + defect-escape classification")
        cmd.Flags().StringVar(&metricsRegressionFile, "regression-rate", "", "path to a text file containing a single float — the Golden-eval detection rate; when omitted the metric reports Unavailable")
        cmd.Flags().StringVar(&metricsIdempotencyFile, "idempotency-rate", "", "path to a text file containing a single float — the stage-level resume idempotency rate; when omitted the metric reports Unavailable")

        // The calibration-threshold flag is exposed but not advertised in --help
        // footer; keep it visible in `--help` output (cobra default) so operators
        // can find it, but document V1 default of 0.70 in the Short.
        return cmd
    }

    func runMetrics(cmd *cobra.Command, _ []string) error {
        cfg, err := config.Load(cfgPath, config.DefaultEnvPath())
        if err != nil {
            return fmt.Errorf("load config: %w", err)
        }
        database, err := db.OpenDB(cfg.DBPath)
        if err != nil {
            return fmt.Errorf("open database: %w", err)
        }
        defer database.Close()

        runStore := db.NewRunStore(database)
        decStore := db.NewDecisionStore(database)
        adapter := newMetricsStoreAdapter(runStore, decStore)
        svc := service.NewMetricsService(adapter, clock.NewSystemClock())

        renderer := newRenderer(cmd.OutOrStdout())

        regression, err := readOptionalFloatFile(metricsRegressionFile)
        if err != nil {
            renderer.RenderError(err)
            return &silentErr{err}
        }
        idempotency, err := readOptionalFloatFile(metricsIdempotencyFile)
        if err != nil {
            renderer.RenderError(err)
            return &silentErr{err}
        }

        report, err := svc.Report(cmd.Context(), metricsWindow, metricsCalibrationThreshold, regression, idempotency)
        if err != nil {
            renderer.RenderError(err)
            return &silentErr{err}
        }

        renderer.RenderSuccess(report)
        return nil
    }
    ```

    Register in `cmd/pipeline/main.go` via `rootCmd.AddCommand(newMetricsCmd())` — add one line next to the existing 7 AddCommand calls at [cmd/pipeline/main.go:42-48](../../cmd/pipeline/main.go#L42-L48). `metricsStoreAdapter` is a tiny local struct in `metrics.go` that embeds both stores and forwards calls — it exists solely to satisfy the service's narrow `MetricsStore` interface without exposing a god-object.

    `readOptionalFloatFile` (also in `metrics.go`): returns `(nil, nil)` for empty path; otherwise reads the file, strips whitespace, parses a single float, returns `(*float64, nil)`. Malformed content → `fmt.Errorf("parse %s: %w", path, err)`. No `error` wrapping for the happy path.

8. **AC-RENDER-HUMAN:** Extend `cmd/pipeline/render.go` with human-rendering for `*domain.MetricsReport`. Add a case branch to `HumanRenderer.RenderSuccess` and a private `renderMetrics(m *domain.MetricsReport)` helper.

    Output format (byte-exact, golden-tested — newlines are `\n`):

    ```
    Pipeline metrics — rolling window: <N> (<M> completed runs)
    [provisional — n < <WINDOW>]                    (line omitted when Provisional=false)

    METRIC                       VALUE        TARGET     STATUS
    ---------------------------  -----------  ---------  ----------
    Automation rate              72.0%        ≥ 80%      ✗ fail
    Critic calibration (kappa)   0.65         ≥ 0.70     ✗ fail
    Critic regression detection  —            ≥ 80%      unavailable
    Defect escape rate           3.0%         ≤ 5%       ✓ pass
    Stage-level resume idempot.  —            100%       unavailable

    Generated at: 2026-04-18T12:34:56Z
    ```

    Rules:
    - Label width 27 chars, value width 11, target width 9, status width 10 (left-aligned, space-separator). Column headers use `-`-underline second row.
    - Percentage-like metrics (automation_rate, defect_escape_rate, critic_regression_detection, resume_idempotency) render value as `<pct>%` where pct is `Value * 100` rounded to 1 decimal via `%.1f`. `critic_calibration` renders as a bare `%.2f` float (it's not a rate).
    - Pass/fail symbols: pass = green `✓ pass`; fail = red `✗ fail`; unavailable = yellow `unavailable` (no symbol, no color = yellow per existing palette).
    - Provisional line appears only when `Provisional=true`. Use yellow color for the bracketed line.
    - Generated-at line is dim/default; suffix on a blank line.

    Add `Label` defaults in `internal/domain/metrics.go` next to the MetricID consts (package-level `var metricLabels = map[MetricID]string{...}` + `Label(id MetricID) string` helper) so the service fills them deterministically and the renderer never invents label strings. The renderer MUST use `Metric.Label` verbatim — if the service forgets to set it, the golden test fails.

    Golden test: `cmd/pipeline/metrics_test.go#TestMetricsCmd_HumanOutput_Golden` (compares stdout byte-for-byte against `testdata/golden/cli_metrics_human.txt`).

9. **AC-RENDER-JSON-ENVELOPE:** JSON output via `--json` MUST match the PRD envelope precisely:

    ```json
    {
      "version": 1,
      "data": {
        "window": 25,
        "window_count": 25,
        "provisional": false,
        "generated_at": "2026-04-18T12:34:56Z",
        "metrics": [
          {"id": "automation_rate", "label": "Automation rate", "value": 0.72, "target": 0.80, "comparator": "gte", "pass": false, "unavailable": false},
          {"id": "critic_calibration", "label": "Critic calibration (kappa)", "value": 0.65, "target": 0.70, "comparator": "gte", "pass": false, "unavailable": false},
          {"id": "critic_regression_detection", "label": "Critic regression detection", "value": null, "target": 0.80, "comparator": "gte", "pass": false, "unavailable": true, "reason": "not provided via --regression-rate"},
          {"id": "defect_escape_rate", "label": "Defect escape rate", "value": 0.03, "target": 0.05, "comparator": "lte", "pass": true, "unavailable": false},
          {"id": "resume_idempotency", "label": "Stage-level resume idempot.", "value": null, "target": 1.0, "comparator": "gte", "pass": false, "unavailable": true, "reason": "not provided via --idempotency-rate"}
        ]
      }
    }
    ```

    `JSONRenderer.RenderSuccess` ALREADY wraps `data` in `Envelope{Version: 1, Data: data}` — do NOT introduce a second wrapper. The `*domain.MetricsReport` pointer flows through as-is.

    Test: `cmd/pipeline/metrics_test.go#TestMetricsCmd_JSONOutput_Golden` (parse stdout, assert `version==1`, `data.window==25`, `len(data.metrics)==5`, key order of MetricID matches the constant block, `unavailable:true` rows have `value:null` and non-empty `reason`). Use the `testdata/golden/cli_metrics_json.json` fixture in the same style as Story 2.6's `testdata/golden/cli_status_paused.json`.

10. **AC-CLI-REGRESSION-SOURCE / AC-CLI-IDEMPOTENCY-SOURCE:** Critic-regression detection and resume idempotency are NOT derivable from V1 DB state.

    - **Regression detection rate**: produced by `go test ./internal/critic -run Golden` (or the equivalent Golden-eval test harness). V1 doesn't have a Critic package yet (see grep for "critic" — no package exists), so the test harness is Epic 4 territory. For 2.7, the CLI exposes the metric via the `--regression-rate` flag (path to file). When the flag is omitted, the metric reports `Unavailable=true`, `Reason="not provided via --regression-rate"`, `Pass=false`. This is a faithful V1 compromise: the metric exists in the report, reports "we don't have this yet", and the operator fills it in when they run Golden eval manually or via a wrapper script.

    - **Resume idempotency rate**: measured by a future resume-idempotency harness (not in scope for 2.7). Same pattern — `--idempotency-rate` file flag, omitted → `Unavailable=true`, `Reason="not provided via --idempotency-rate"`.

    This pattern is explicitly V1: the CLI surfaces all five metrics, two of them as pass-through from an external harness. Do NOT invent a new quality-gate runner to fabricate values. Document this in `docs/cli-diagnostics.md` (see AC-DOCS).

11. **AC-SQL-EXPLAIN-WINDOW:** Extend `internal/db/observability_query_test.go`'s `rollingWindowQueries` table with three new entries — the metrics CLI's SQL shapes. Each entry runs through the existing `TestRollingWindowQueries_UseIndexes` harness.

    ```go
    {
        name:  "metrics_recent_completed_runs",
        sql:   "SELECT id, status, critic_score, human_override, retry_count, retry_reason, created_at FROM runs WHERE status = ? ORDER BY created_at DESC, id DESC LIMIT ?",
        args:  []any{"completed", 25},
        index: "idx_runs_status_created_at",
    },
    {
        name:  "metrics_decisions_by_run",
        sql:   "SELECT scene_id, decision_type FROM decisions WHERE run_id = ? AND superseded_by IS NULL AND decision_type IN ('approve','reject') AND scene_id IS NOT NULL",
        args:  []any{"scp-049-run-1"},
        index: "idx_decisions_run_id_type",
    },
    {
        name:  "metrics_auto_passed_scenes",
        sql:   "SELECT COUNT(*) FROM segments WHERE run_id = ? AND critic_score >= ?",
        args:  []any{"scp-049-run-1", 0.70},
        index: "sqlite_autoindex_segments_1",
    },
    ```

    The third case uses the `UNIQUE(run_id, scene_index)` auto-index from Migration 001 — SQLite's planner picks `sqlite_autoindex_segments_1` for the `run_id` prefix seek. Substring match on `"USING INDEX"` remains the durable assertion; the `index` field is a log-only hint per the existing harness's `t.Logf` on mismatch (see [observability_query_test.go:102-104](../../internal/db/observability_query_test.go#L102-L104)). Add a test assertion that `EXPLAIN QUERY PLAN` for each new query contains `USING INDEX` and does NOT contain `SCAN runs` or `SCAN decisions` without a preceding `SEARCH`.

12. **AC-INTEGRATION-INDEXES-PRESENT:** Add `TestMigration005_DecisionsIndexesCreated` to `internal/db/observability_query_test.go` (parallel to `TestMigration003_IndexesCreated` — same sqlite_master pattern):

    ```go
    wanted := map[string]bool{
        "idx_decisions_run_id_type":    false,
        "idx_decisions_created_at":     false,
    }
    // Query sqlite_master WHERE type='index' AND tbl_name='decisions'.
    ```

13. **AC-FIXTURE-METRICS-SEED:** `testdata/fixtures/metrics_seed.sql` (NEW) seeds a deterministic 30-run dataset for `TestMetricsService_Report_FullWindow` and CLI golden tests. Header comment MUST describe the expected distribution + derived metric values:

    ```sql
    -- Fixture for Story 2.7 metrics tests. Distribution:
    --   25 × status='completed', 18 × human_override=0, 7 × human_override=1
    --                                                        → automation_rate = 18/25 = 0.72
    --   25 runs have critic_score in [0.55..0.95], 18 with >=0.70 ("Critic pass")
    --   decisions table: 50 scene-level approves + 18 scene-level rejects + 3 superseded
    --                    pairing yields kappa ≈ 0.65 (between-rater marginal-unbalanced case)
    --   segments: 40 scenes with critic_score >= 0.70 (auto-passed)
    --                    6 of those have a later 'reject' decision
    --                    → defect_escape_rate = 6/40 = 0.15 (fails target <=0.05)
    --     [adjust to 2/40 = 0.05 PASS if you want the golden JSON to show pass;
    --      MUST match the golden files — keep in sync across fixture + golden]
    --   5 × status='failed' (decoy, excluded from window by status filter)
    --
    -- All created_at values are fixed offsets from a frozen date:
    --   INSERT ... datetime('2026-04-15 00:00:00', '-N hours') pattern
    -- so fixture results are reproducible across machines.
    --
    -- Golden test pairing (do NOT desync):
    --   TestMetricsService_Report_FullWindow → expects the values noted above.
    --   TestMetricsCmd_HumanOutput_Golden → testdata/golden/cli_metrics_human.txt
    --   TestMetricsCmd_JSONOutput_Golden → testdata/golden/cli_metrics_json.json
    --
    -- EXPECTED WINDOW RESULTS (window=25):
    --   Provisional: false (25 completed)
    --   Automation rate: 0.72 (72.0%), fail (target >=0.80)
    --   Critic calibration (kappa): <pin-exact-value-from-unit-test>, pass/fail per exact n
    --   Critic regression detection: Unavailable
    --   Defect escape rate: <pin 0.05 or 0.15 — see above>
    --   Resume idempotency: Unavailable
    ```

    Dev task: compute the exact kappa value from the fixture's 2×2 and hard-code it in `TestMetricsService_Report_FullWindow`'s expected output + the golden files. Do NOT use `math.Round(kappa*100)/100` — the test asserts via `testutil.AssertEqual` on a float64 with `math.Abs(got-want) < 1e-9` tolerance (extend `testutil/assert.go` if missing; prefer a new `AssertFloatNear(t, got, want, eps)` helper).

14. **AC-FR-COVERAGE:** Update `testdata/fr-coverage.json` to add entries for FR29 (calibration metric), NFR-O4 index coverage is already recorded but the new Migration 005 index list MUST be added to the annotation for FR8/FR29 cross-reference.

    New entry (append before the closing `]`):
    ```json
    {
      "fr_id": "FR29",
      "test_ids": [
        "TestCohensKappa_KnownTextbookExample",
        "TestCohensKappa_Degenerate_AllOneClass",
        "TestMetricsService_Report_FullWindow",
        "TestMetricsService_Report_ProvisionalWhenShort",
        "TestDecisionStore_KappaPairsForRuns_FiltersSupersededAndNonScene",
        "TestMetricsCmd_HumanOutput_Golden",
        "TestMetricsCmd_JSONOutput_Golden"
      ],
      "annotation": "Cohen's kappa calibration, rolling 25-run window, provisional label when n<25 (PRD §Technical Success; NFR-O4 indexes via Migration 005)"
    }
    ```

    Update `meta.last_updated` to `"2026-04-18"`. `meta.total_frs` unchanged. Update existing NFR-O3/NFR-O4 narrative in FR5/FR6 annotations is NOT required here (they already reference NFR-O4 via Migration 003).

15. **AC-DOCS:** Append a "Pipeline metrics" appendix to `docs/cli-diagnostics.md`:

    - One-paragraph overview: what `pipeline metrics --window 25` computes, when to read `provisional`/`unavailable`, the V1 limitations (regression + idempotency flags).
    - Inline example of human output (byte-for-byte from the golden).
    - SQL queries backing each metric, annotated with the index each picks (parallel to the HITL + anti-progress appendix patterns).
    - Cross-reference to the deferred-work items that V1.5 resolves.

    Keep the appendix under 60 lines. Korean narrative is fine (matches existing doc style) — SQL + code blocks stay English.

16. **AC-DEFERRED-WORK:** Append a new section to `_bmad-output/implementation-artifacts/deferred-work.md`:

    ```
    ## Deferred from: implementation of 2-7-pipeline-metrics-cli-report (2026-04-18)

    - **V1 automation-rate granularity is run-level, not stage-level.** `runs.human_override` is a sticky bit across all stages of a run — the metric reports "fraction of runs with zero operator intervention at any stage", not PRD's literal "auto-completed stages / total stages". Unblocks Day-90 gating; V1.5 with per-stage observability rows (see Story 2.4 deferred "Per-stage cost history for a single run") fixes this natively.
    - **V1 kappa is run-level, not scene-level.** A per-scene Critic verdict column on `decisions` would raise kappa fidelity substantially. Epic 8 owns this when the decision-capture write path lands.
    - **`--regression-rate` and `--idempotency-rate` are file-based pass-throughs.** A CI wrapper that runs `go test ./... -run Golden` and writes the detection rate to a known path is the Day-90 companion script — not in scope for 2.7. Epic 10 may graduate this to an internal harness.
    - **Calibration threshold is flag-based, not config.** `--calibration-threshold` defaults to 0.70 (PRD target). Add a `calibration_threshold` field to `pipeline_config.yaml` + `internal/domain/config.go` once Epic 4 (calibration tracking) locks the final value.
    - **Kappa float precision.** `TestCohensKappa_KnownTextbookExample` asserts to 1e-9; IEEE-754 rounding on large n could break this on non-x86 architectures. Re-verify if a CI matrix is ever introduced.
    - **Migration number collision pattern.** The repo already has two `004_*.sql` files. If a third story collides on 005, the deferred-work registry will grow — consider renaming to a `005_005_*.sql` scheme OR enforcing uniqueness via a migration-lint test.
    ```

## Tasks / Subtasks

- [x] **T1 — Domain types + labels map** (AC-DOMAIN-METRICS-TYPES, AC-RENDER-HUMAN)
  - [x] Create `internal/domain/metrics.go` with `MetricID` const block, `MetricComparator` consts, `Metric`, `MetricsReport`, package-level `metricLabels` map + `Label(MetricID) string`.
  - [x] Create `internal/domain/metrics_test.go` with `TestMetric_JSONShape` + `TestLabel_AllMetricIDs`.

- [x] **T2 — Migration 005** (AC-MIGRATION-005-METRICS-INDEXES, AC-INTEGRATION-INDEXES-PRESENT)
  - [x] Create `migrations/005_metrics_indexes.sql` with header comment + `idx_decisions_run_id_type` + `idx_decisions_created_at`.
  - [x] Add `TestMigration005_DecisionsIndexesCreated` to `internal/db/observability_query_test.go` following the `TestMigration003_IndexesCreated` pattern.
  - [x] Verify `migrations/embed.go` auto-picks it up (no edit expected — sanity check only).

- [x] **T3 — RunStore window query** (AC-RUNSTORE-METRICS-WINDOW, AC-SQL-EXPLAIN-WINDOW)
  - [x] Add `RunMetricsRow` struct + `RecentCompletedRunsForMetrics(ctx, window)` to `internal/db/run_store.go`.
  - [x] Tests: `TestRunStore_RecentCompletedRunsForMetrics_{ReturnsWindow,Empty,ValidationError,UsesIndex}`.
  - [x] Extend `rollingWindowQueries` table in `internal/db/observability_query_test.go` with the three new SQL shapes; verify `TestRollingWindowQueries_UseIndexes` still passes.

- [x] **T4 — DecisionStore aggregates** (AC-DECISIONSTORE-METRICS-AGGREGATES)
  - [x] Add `KappaPair`, `DefectEscape` + `KappaPairsForRuns`, `DefectEscapeInRuns` to `internal/db/decision_store.go`.
  - [x] Tests: `TestDecisionStore_KappaPairsForRuns_{FiltersSupersededAndNonScene,EmptyRunIDs,UsesIndex}`, `TestDecisionStore_DefectEscapeInRuns_{CountsOnlyAutoPassedRejects,EmptyRunIDs,UsesIndex}`.

- [x] **T5 — Kappa formula** (AC-SERVICE-KAPPA)
  - [x] Create `internal/service/kappa.go` with `CohensKappa(a,b,c,d) (kappa float64, ok bool, reason string)`.
  - [x] Create `internal/service/kappa_test.go` with `TestCohensKappa_{PerfectAgreement,Chance,KnownTextbookExample,Degenerate_AllOneClass,Empty,NegativeAgreement}`.
  - [x] Include Wikipedia 2-rater-50-subject textbook example with source-comment citation.

- [x] **T6 — MetricsService orchestration** (AC-SERVICE-METRICS-TYPES)
  - [x] Create `internal/service/metrics_service.go` with `MetricsStore` interface + `MetricsService` + `NewMetricsService` + `Report(ctx, window, threshold, regression, idempotency)`.
  - [x] Create `internal/service/metrics_service_test.go` with `TestMetricsService_Report_{FullWindow,ProvisionalWhenShort,HandlesEmptyDB,DegenerateKappa_Unavailable,ValidationOnZeroWindow}`.
  - [x] Use `testutil.BlockExternalHTTP(t)` + `testutil.LoadRunStateFixture(t, "metrics_seed")` for the full-window test.

- [x] **T7 — CLI command + renderer** (AC-CLI-METRICS-COMMAND, AC-CLI-REGRESSION-SOURCE, AC-CLI-IDEMPOTENCY-SOURCE, AC-RENDER-HUMAN, AC-RENDER-JSON-ENVELOPE)
  - [x] Create `cmd/pipeline/metrics.go` with `newMetricsCmd()`, `runMetrics`, `metricsStoreAdapter`, `readOptionalFloatFile`.
  - [x] Register in `cmd/pipeline/main.go#42-48`.
  - [x] Extend `cmd/pipeline/render.go` — add `case *domain.MetricsReport:` to `HumanRenderer.RenderSuccess` + `renderMetrics` helper.
  - [x] Create `testdata/golden/cli_metrics_human.txt` + `testdata/golden/cli_metrics_json.json`.
  - [x] Tests: `cmd/pipeline/metrics_test.go` with `TestMetricsCmd_{HumanOutput_Golden,JSONOutput_Golden,ValidationErrorOnZeroWindow,RegressionFile_ParsedAndPassed,IdempotencyFile_ParsedAndPassed,RegressionFileMissing_PropagatesError}`.

- [x] **T8 — Fixture + golden files** (AC-FIXTURE-METRICS-SEED)
  - [x] Create `testdata/fixtures/metrics_seed.sql` with the 30-run distribution + decisions + segments. Header comment MUST match the actual data.
  - [x] Populate golden files (`cli_metrics_human.txt`, `cli_metrics_json.json`) from the fixture-derived metric values. Use the T6 unit tests to pin exact kappa + defect-escape values; copy-paste into the goldens.

- [x] **T9 — FR-coverage + docs + deferred-work** (AC-FR-COVERAGE, AC-DOCS, AC-DEFERRED-WORK)
  - [x] Append FR29 entry to `testdata/fr-coverage.json`; bump `meta.last_updated`.
  - [x] Append "Pipeline metrics" appendix to `docs/cli-diagnostics.md`.
  - [x] Append Story 2.7 section to `_bmad-output/implementation-artifacts/deferred-work.md`.

- [x] **T10 — Sweep + verification**
  - [x] `go test ./... -count=1` passes.
  - [x] `go run scripts/lintlayers/main.go` passes.
  - [x] `go build ./...` passes with `CGO_ENABLED=0`.
  - [x] Manual smoke: `pipeline init && pipeline metrics --window 25` on an empty DB → prints the full report with all metrics marked Unavailable + Provisional=true + WindowCount=0.
  - [x] Manual smoke: seeded DB (apply `testdata/fixtures/metrics_seed.sql` via `sqlite3` or a one-off script) → `pipeline metrics --window 25 --json` output matches `testdata/golden/cli_metrics_json.json` (modulo `generated_at`). Use `--config` to point at a test DB rather than mutating `~/.youtube-pipeline/pipeline.db`.

## Dev Notes

### What the developer MUST reuse, not reinvent

- **Envelope + Renderer abstraction.** [cmd/pipeline/render.go:27-39](../../cmd/pipeline/render.go#L27-L39) already defines `Envelope{Version, Data, Error}` and `JSONRenderer.RenderSuccess` already wraps in that envelope. Just add a new case for `*domain.MetricsReport` on the human renderer side.
- **`newRenderer` + `--json` plumbing.** [cmd/pipeline/main.go:18-23](../../cmd/pipeline/main.go#L18-L23). Never call `json.Encode` directly in a subcommand.
- **`silentErr` wrapping.** On error, call `renderer.RenderError(err)` then `return &silentErr{err}` — see [cmd/pipeline/status.go:43-46](../../cmd/pipeline/status.go#L43-L46).
- **`testutil.NewTestDB` + `testutil.LoadRunStateFixture`** for all new DB-backed tests. [internal/testutil/db.go:16-46](../../internal/testutil/db.go#L16-L46), [internal/testutil/fixture.go:28-64](../../internal/testutil/fixture.go#L28-L64).
- **`testutil.BlockExternalHTTP(t)`** in every new `_test.go` file (strict).
- **`config.Load(cfgPath, envPath)` → `db.OpenDB(cfg.DBPath)` → `store := db.NewRunStore(database)`** — the canonical CLI bootstrap sequence. Repeated across `status.go`, `cancel.go`, `create.go`, `resume.go`.
- **`EXPLAIN QUERY PLAN` harness.** [internal/db/observability_query_test.go:49-107](../../internal/db/observability_query_test.go#L49-L107) already iterates a `rollingWindowQueries` table; extend it instead of adding a parallel harness.
- **Narrow interface declaration at consumer (not provider).** `service.MetricsStore` is declared in `internal/service/metrics_service.go`; `db.RunStore` + `db.DecisionStore` satisfy it structurally via the tiny `metricsStoreAdapter` struct in `cmd/pipeline/metrics.go`. Pattern: Story 2.6 `service.DecisionReader`, `pipeline.HITLSessionStore`.
- **Clock injection.** Use `clock.Clock` for `GeneratedAt` — do NOT call `time.Now()` directly in the service. [internal/clock/] provides `SystemClock` + `FakeClock` for tests.

### What the developer MUST NOT do

- **Do NOT create a sixth migration number 004.** Use **005**. Both existing 004s already cause a latent version-collision concern; compounding it is how bugs get shipped.
- **Do NOT query `pipeline_runs` through `SELECT * FROM runs` without a `status='completed'` filter.** Incomplete runs have noisy observability data that would skew all five metrics.
- **Do NOT invent a new JSON envelope shape.** Reuse `Envelope{Version:1, Data: ...}` via `JSONRenderer.RenderSuccess`. PRD specifies `{"version": 1, "data": {...}}` verbatim.
- **Do NOT fabricate regression / idempotency values.** If the flag is absent, the metric is Unavailable. Report it. Don't default to 1.0 or 0.0.
- **Do NOT add an `/api/metrics` HTTP handler.** Architecture [architecture.md:1882-1883](../../_bmad-output/planning-artifacts/architecture.md#L1882-L1883) defers this to V1.5 — CLI is the V1 surface.
- **Do NOT mock the database in service tests.** Use `testutil.LoadRunStateFixture` — consistent with Feedback memory "integration tests must hit a real database, not mocks" and Stories 2.4/2.5/2.6.
- **Do NOT implement "live" regression detection by running Golden eval from the CLI.** Epic 4 owns Golden eval orchestration; 2.7 reads a pre-computed value.
- **Do NOT introduce a `calibration_threshold` field in `pipeline_config.yaml`.** Flag-based pass-through for V1 (deferred-work item).
- **Do NOT touch `Recorder`, `Engine`, or `Resume` code paths.** Metrics is read-only.

### Previous Story Learnings Applied

- **Story 2.4** — `migrations/003_observability_indexes.sql` + `EXPLAIN QUERY PLAN` test pattern. 2.7 mirrors the pattern for Migration 005. Recorder + 8-column observability stays untouched.
- **Story 2.5** — `AntiProgressFalsePositiveStats` subquery style (ORDER BY DESC, id DESC LIMIT) is the shape template for `RecentCompletedRunsForMetrics`. `TestAntiProgressFalsePositiveStats_RollingWindow` is the fixture-based test template. `testutil.AssertEqual` for non-float, new `AssertFloatNear` for kappa.
- **Story 2.6** — `DecisionStore` + `metricsStoreAdapter` pattern for grouping read-only DB methods behind a service-declared interface. Golden file pattern (`testdata/golden/cli_status_paused.json`) for CLI byte-exact assertions. Do NOT reuse `HITLService` — metrics is orthogonal.
- **FR-coverage schema** uses `fr_id` (not `nfr_id`). NFRs referenced in annotations only.
- **snake_case JSON** everywhere. No testify, no gomock. CGO_ENABLED=0.

### Project Structure After This Story

```
internal/
  domain/
    metrics.go                      # NEW — MetricID, Metric, MetricsReport, labels map
    metrics_test.go                 # NEW
  db/
    run_store.go                    # MODIFIED — RunMetricsRow + RecentCompletedRunsForMetrics
    run_store_test.go               # MODIFIED — window + EXPLAIN tests
    decision_store.go               # MODIFIED — KappaPair, DefectEscape + two aggregators
    decision_store_test.go          # MODIFIED — aggregator tests + EXPLAIN tests
    observability_query_test.go     # MODIFIED — 3 new rollingWindowQueries entries + TestMigration005_DecisionsIndexesCreated
  service/
    metrics_service.go              # NEW — MetricsStore interface + MetricsService + Report
    metrics_service_test.go         # NEW
    kappa.go                        # NEW — CohensKappa pure function
    kappa_test.go                   # NEW
  testutil/
    assert.go                       # MAYBE-MODIFIED — add AssertFloatNear if missing
cmd/pipeline/
  main.go                           # MODIFIED — rootCmd.AddCommand(newMetricsCmd())
  metrics.go                        # NEW — newMetricsCmd + runMetrics + metricsStoreAdapter + readOptionalFloatFile
  metrics_test.go                   # NEW — golden + flag-behavior tests
  render.go                         # MODIFIED — case *domain.MetricsReport + renderMetrics
migrations/
  005_metrics_indexes.sql           # NEW
testdata/
  fixtures/
    metrics_seed.sql                # NEW
  golden/
    cli_metrics_human.txt           # NEW
    cli_metrics_json.json           # NEW
  fr-coverage.json                  # MODIFIED — FR29 entry added
docs/
  cli-diagnostics.md                # MODIFIED — "Pipeline metrics" appendix
_bmad-output/
  implementation-artifacts/
    deferred-work.md                # MODIFIED — Story 2.7 deferrals
    sprint-status.yaml              # MODIFIED — 2-7 backlog → ready-for-dev (by create-story) → in-progress (by dev-story)
```

### Critical Constraints

- **Exactly 5 metrics, in stable order** matching the `MetricID` const block. Stable order is pinned by the golden JSON test — key reordering fails the test.
- **`Value` is `*float64` (nullable)** — `Unavailable=true` means `Value=nil`. JSON omits nothing via `omitempty`; `value` renders as `null`.
- **`Provisional=true` iff `WindowCount < Window`** — applies to the whole report even if some metrics are Unavailable.
- **`Pass=false` when `Unavailable=true`** — an unavailable metric cannot pass a Day-90 gate.
- **Cohen's kappa 2×2 formula is unit-test-pinned.** Negative kappa is NOT clamped to 0.
- **Calibration threshold = 0.70** in V1 (config-flag, not config-file).
- **`status='completed'` filter** applies to the rolling window — partial/failed/cancelled runs are excluded.
- **Indexes required:** `idx_runs_status_created_at` (existing, Migration 003), `idx_decisions_run_id_type` + `idx_decisions_created_at` (NEW, Migration 005). EXPLAIN QUERY PLAN assertion MUST pass for each metric query.
- **1000-run performance:** manual smoke on a 1000-run seed MUST complete `pipeline metrics --window 25 --json` in <1 second (PRD FR for 2.7). Pin an explicit `TestMetricsService_Report_Performance1000Runs` test with `testing.B`-style timing (or a simple wall-clock assertion with a loose 5s ceiling to avoid CI flakes; the real target is empirical on the operator's machine).
- **`metricsStoreAdapter` is a tiny struct, not a package.** Lives in `cmd/pipeline/metrics.go` only.
- **No new layer-lint edges.** `service → db`, `service → domain`, `service → clock` are all allowed. `cmd/pipeline` is not under layer-lint (already established).

### Project Structure Notes

- All new files fit inside allowed-import edges at [scripts/lintlayers/main.go:21-33](../../scripts/lintlayers/main.go#L21-L33). No edits to the lint rule file.
- Files land in existing packages: `internal/domain/`, `internal/db/`, `internal/service/`, `cmd/pipeline/`. No new package created.
- Module path `github.com/sushistack/youtube.pipeline`. Go 1.25.7. SQLite driver `github.com/ncruces/go-sqlite3/driver` (NO CGO). Cobra v1.x via `github.com/spf13/cobra`.

### References

- Story 2.7 AC (BDD): [epics.md:1102-1130](../planning-artifacts/epics.md#L1102-L1130)
- Epic 2 overview: [epics.md:378-399](../planning-artifacts/epics.md#L378-L399)
- FR29 (calibration metric, rolling 25 + provisional): [prd.md:51, 1285](../planning-artifacts/prd.md#L51)
- NFR-O4 (rolling-window index coverage): [prd.md:1435-1439](../planning-artifacts/prd.md#L1435-L1439), [epics.md:103](../planning-artifacts/epics.md#L103)
- PRD §Technical Success (5 metrics table + targets + provisional rationale): [prd.md:257-288](../planning-artifacts/prd.md#L257-L288)
- PRD §Mode 5 Reviewer journey (example CLI output shape): [prd.md:592-621](../planning-artifacts/prd.md#L592-L621)
- Architecture — SQLite single-writer + WAL: [architecture.md:172](../planning-artifacts/architecture.md#L172)
- Architecture — `/api/metrics` deferred to CLI-only: [architecture.md:1882-1883](../planning-artifacts/architecture.md#L1882)
- Migration 003 (existing runs indexes): [migrations/003_observability_indexes.sql](../../migrations/003_observability_indexes.sql)
- Migration 004 (anti_progress composite + hitl_sessions): [migrations/004_anti_progress_index.sql](../../migrations/004_anti_progress_index.sql), [migrations/004_hitl_sessions.sql](../../migrations/004_hitl_sessions.sql)
- `db.Migrate` runner (`current` read once, not between migrations): [internal/db/migrate.go:16-65](../../internal/db/migrate.go#L16-L65)
- Existing `AntiProgressFalsePositiveStats` (window query template): [internal/db/run_store.go:304-356](../../internal/db/run_store.go#L304-L356)
- `DecisionStore` (extend here): [internal/db/decision_store.go:1-146](../../internal/db/decision_store.go#L1-L146)
- `TestRollingWindowQueries_UseIndexes` harness: [internal/db/observability_query_test.go:49-107](../../internal/db/observability_query_test.go#L49-L107)
- CLI scaffolding pattern: [cmd/pipeline/status.go:14-79](../../cmd/pipeline/status.go#L14-L79), [cmd/pipeline/main.go:25-64](../../cmd/pipeline/main.go#L25-L64)
- JSON Envelope + HumanRenderer: [cmd/pipeline/render.go:27-257](../../cmd/pipeline/render.go#L27-L257)
- `testutil.NewTestDB`: [internal/testutil/db.go:16-46](../../internal/testutil/db.go#L16-L46)
- `testutil.LoadRunStateFixture`: [internal/testutil/fixture.go:28-64](../../internal/testutil/fixture.go#L28-L64)
- Layer-lint rules (no edits required): [scripts/lintlayers/main.go:21-33](../../scripts/lintlayers/main.go#L21-L33)
- FR coverage tracker: [testdata/fr-coverage.json](../../testdata/fr-coverage.json)
- Sprint review checkpoints for 2.7: [sprint-prompts.md:371-396](../planning-artifacts/sprint-prompts.md#L371-L396)
- Previous stories: [2-4-per-stage-observability-cost-tracking.md](2-4-per-stage-observability-cost-tracking.md), [2-5-anti-progress-detection.md](2-5-anti-progress-detection.md), [2-6-hitl-session-pause-resume-change-diff.md](2-6-hitl-session-pause-resume-change-diff.md)
- Deferred-work registry: [deferred-work.md](deferred-work.md)

## Dev Agent Record

### Agent Model Used

Claude Opus 4.7 (1M context), 2026-04-18

### Debug Log References

- `go test ./... -count=1`
- `go run scripts/lintlayers/main.go`
- `CGO_ENABLED=0 go build ./...`
- `CGO_ENABLED=0 go run ./cmd/pipeline metrics --config <tmp>/config.yaml --window 25`

### Completion Notes List

- Added the Day-90 metrics domain surface, Migration 005 indexes, rolling-window DB queries, and the kappa/defect-escape aggregations needed for the report.
- Implemented `MetricsService`, the `pipeline metrics` CLI command, human/JSON rendering, deterministic fixture data, and golden tests for both output modes.
- Verified the story with full regression, layer-lint, CGO-disabled build, and an empty-database CLI smoke showing `provisional` plus all metrics as `unavailable`.

### File List

- _bmad-output/implementation-artifacts/2-7-pipeline-metrics-cli-report.md
- _bmad-output/implementation-artifacts/deferred-work.md
- _bmad-output/implementation-artifacts/sprint-status.yaml
- cmd/pipeline/main.go
- cmd/pipeline/metrics.go
- cmd/pipeline/metrics_test.go
- cmd/pipeline/render.go
- docs/cli-diagnostics.md
- internal/db/decision_store.go
- internal/db/decision_store_test.go
- internal/db/observability_query_test.go
- internal/db/run_store.go
- internal/db/run_store_test.go
- internal/db/sqlite_test.go
- internal/domain/metrics.go
- internal/domain/metrics_test.go
- internal/service/kappa.go
- internal/service/kappa_test.go
- internal/service/metrics_service.go
- internal/service/metrics_service_test.go
- internal/testutil/assert.go
- migrations/005_metrics_indexes.sql
- testdata/fixtures/metrics_seed.sql
- testdata/fr-coverage.json
- testdata/golden/cli_metrics_human.txt
- testdata/golden/cli_metrics_json.json

### Review Findings

Sources: **Blind Hunter** (diff-only adversarial), **Edge Case Hunter** (boundary enumeration with project read access), **Acceptance Auditor** (spec↔diff reconciliation of 16 ACs + prohibitions). Auditor verdict: **ACCEPTED-WITH-NOTES** — all 16 ACs substantively satisfied, kappa formula byte-for-byte, index-backed SQL verified, prohibition list honored, JSON envelope correct.

#### Decision Needed

*(none — D1 resolved as defer)*

#### Patch

- [x] [Review][Patch][blocker] **readOptionalFloatFile accepts NaN/±Inf** — `cmd/pipeline/metrics.go` — `strconv.ParseFloat("NaN"/"Inf"/"-Inf", 64)` succeeds; NaN breaks `encoding/json.Marshal` and makes Pass comparison nonsensical. Reject with explicit error.
- [x] [Review][Patch][high] **readOptionalFloatFile accepts out-of-range rates** — `cmd/pipeline/metrics.go` — negative values or values > 1.0 parse fine and render as `-50.0% / 150.0%`. Spec says these are *rates*. Add `[0, 1]` range check.
- [x] [Review][Patch][high] **KappaPairsForRuns uses COUNT(\*) not COUNT(DISTINCT scene_id)** — `internal/db/decision_store.go` — sibling `DecisionCountsByRunID` uses `COUNT(DISTINCT scene_id)` to dedupe. A scene with multiple non-superseded decisions (possible via undo/redo chain) inflates the approve/reject tally and tips the `approve > reject` tiebreaker incorrectly.
- [x] [Review][Patch][high] **Kappa tie-break on `agg.approve == agg.reject` lacks a test** — `internal/db/decision_store.go` / `kappa_test.go` — implementation maps ties to `OperatorApprove=false` (spec: "conservatively break to reject") but no test fixture exercises a tie. Add a decision_store test with balanced approve/reject counts.
- [x] [Review][Patch][medium] **`--window` upper bound missing → SQL variable limit exhaustion** — `cmd/pipeline/metrics.go` + `internal/db/decision_store.go` — `IN (?,?,…)` with `len(runIDs)` over SQLite's `SQLITE_MAX_VARIABLE_NUMBER` (999 old / 32766 new) fails opaquely. Clamp `--window` to a sane ceiling (e.g., 1000 matches PRD NFR-O4).
- [x] [Review][Patch][medium] **BOM not stripped in readOptionalFloatFile** — `cmd/pipeline/metrics.go` — UTF-8 BOM (`\ufeff`) from Windows Notepad makes `ParseFloat` return a non-obvious error. `strings.TrimSpace` strips \r\n but not BOM. Strip explicitly.
- [x] [Review][Patch][medium] **Empty / whitespace-only file → confusing `ParseFloat` error** — `cmd/pipeline/metrics.go` — operator-facing message should distinguish "file empty" from "content malformed".
- [x] [Review][Patch][medium] **Render `≥ 100%` for resume_idempotency target deviates from spec example** — `cmd/pipeline/render.go` — spec's AC-RENDER-HUMAN golden shows bare `100%` for the target column; current renderer emits `≥ 100%` (comparator auto-prepended). Either update renderer to suppress the comparator on idempotency, or update the golden to match reality and fix AC example drift.
- [x] [Review][Patch][medium] **Missing `TestMetricsService_Report_Performance1000Runs`** — Critical Constraints block pins a 1000-run timing test (<1s / <5s CI ceiling). No such test exists. Without it, migration 005's NFR-O4 motivation is unverified.
- [x] [Review][Patch][low] **`TestMetricsCmd_IdempotencyFile_ParsedAndPassed` substring assertion too loose** — `cmd/pipeline/metrics_test.go:1135` — asserts `"value":1` appears, but `"target":1` also satisfies the substring for other metrics. Parse JSON and assert on the specific metric's `value`.
- [x] [Review][Patch][medium] **`renderMetrics` column padding breaks on multibyte `≥`/`≤`** — dismissed: Go's `%-9s` already pads by rune count, not byte count; golden confirms correct alignment. — `cmd/pipeline/render.go` — Go's `%-9s` pads by byte count, not rune width. `≥`/`≤` are 3 bytes each in UTF-8, so the target column visually under-pads by 2 characters. Previously flagged in the 2.6 review under "Story 2.7 scope". Use `runewidth.FillRight` or pre-compute rune-width padding.

#### Defer (pre-existing / out of scope / low risk)

- [x] [Review][Defer][high] **Float-precision edge in file-sourced rate vs target comparison** — theoretical; both sides parse identically in practice. Documented for epsilon-tolerance future work.
- [x] [Review][Defer][high] **`ORDER BY created_at DESC, id DESC` lexical sort on run IDs containing numbers** — `internal/db/run_store.go` — "scp-049-run-10" sorts before "scp-049-run-2" lexically. Spec explicitly pinned this pattern (matches Migration 004 precedent). Deterministic but surprising — revisit alongside ID schema rework.
- [x] [Review][Defer][medium] **Provisional line appears alongside all-unavailable rows** — `cmd/pipeline/render.go` — when WindowCount=0 the human output shows `[provisional — n < 25]` AND five "unavailable" rows; redundant. UX polish, not a correctness issue.
- [x] [Review][Defer][medium] **Negative kappa (−1.0 … 0) renders without a "worse than chance" banner** — `cmd/pipeline/render.go` — pass/fail symbol is enough for V1; Epic 10 UX polish.
- [x] [Review][Defer][low] **`metricsClock` is a package-level mutable seam for tests** — `cmd/pipeline/metrics.go` — parallel metrics invocations would race. CLI is not invoked in parallel; tests already save/restore the var.
- [x] [Review][Defer][low] **`valueMetric` default comparator branch falls through to GTE** — `internal/service/metrics_service.go` — no future comparators planned. Mitigated by type-checked `MetricComparator` const block.
- [x] [Review][Defer][low] **AntiProgressFalsePositiveStats `Provisional: total < window` semantics** — `internal/db/run_store.go` — counterintuitive but pre-existing in Story 2.5. Not in 2.7 scope.
- [x] [Review][Defer][low] **Golden files only exercise the all-available row shape** — `testdata/golden/cli_metrics_json.json` — the `unavailable:true + value:null + reason:"..."` shape is covered by service unit tests, not a JSON golden. Add a second golden if regressions appear.
- [x] [Review][Defer][low] **KappaPairsForRuns silently assumes per-run critic_score invariance under GROUP BY** — `internal/db/decision_store.go` — GROUP BY includes `r.critic_score`, so assumption holds structurally. Documented.

#### Dismissed (noise / false positive)

- Golden JSON emits `"value":1` not `"value":1.0` — Go `encoding/json` standard behavior; test matches.
- `DefectEscapeInRuns` excludes NULL `critic_score` segments — correct per spec intent ("auto-passed" requires a score).
- `RunStore.Cancel` explicit `tx.Rollback()` is redundant with deferred rollback — not in Story 2.7 scope.
