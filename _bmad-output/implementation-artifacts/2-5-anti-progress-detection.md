# Story 2.5: Anti-Progress Detection

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want the system to detect when retry loops are making no progress (consecutive outputs too similar) and hard-stop with a human-review escalation,
so that I'm not wasting API costs on structurally unfixable outputs and so V1.5 can gate the detector's false-positive rate from real telemetry.

## Acceptance Criteria

1. **AC-SIM-COSINE:** `internal/pipeline/similarity.go` exposes `CosineSimilarity(a, b string) float64` returning the cosine similarity of the token-frequency (TF) vectors of `a` and `b`. Deterministic, no external calls. Signature and contract:

    ```go
    // CosineSimilarity returns the bag-of-words cosine similarity of a and b
    // in [0.0, 1.0]. It is the V1 implementation of FR8 and NFR-R2:
    //   - Tokenize both inputs (see Tokenize).
    //   - Build sparse term-frequency maps.
    //   - Return dot(Ta, Tb) / (||Ta|| * ||Tb||).
    // Returns 0 when either side is empty (no tokens) — avoids NaN and means
    // "no evidence of stuckness" (safe default for the anti-progress detector).
    // Not embedding-based: V1.5 will promote to embeddings (deferred).
    func CosineSimilarity(a, b string) float64

    // Tokenize splits s into lowercased tokens on Unicode whitespace and
    // punctuation. Stable across runs (no randomness). Empty input → empty map.
    // Exported so tests and the anti-progress detector can assert on the
    // tokenization contract.
    func Tokenize(s string) map[string]int
    ```

    `CosineSimilarity` must satisfy all of: `CosineSimilarity("", "")==0`, `CosineSimilarity("a", "")==0`, `CosineSimilarity("a b c", "a b c")==1.0` (within 1e-9), `CosineSimilarity("a b", "b a")==1.0` (ordering-independent — bag of words), `CosineSimilarity("a b", "c d)==0`. Test via a table of golden pairs with pre-computed expected values to 6-decimal precision.

2. **AC-SIM-DETERMINISM:** `CosineSimilarity` is pure: no goroutines, no time-dependent state, no map-iteration-order leakage into the result (dot product and norms commute over iteration order). A `TestCosineSimilarity_Deterministic_N_Runs` test calls it 100× on the same input pair and asserts the exact same float64 bits every call (use `math.Float64bits` equality, not `==` on floats, to detect non-determinism from map iteration + summation reordering). If floating-point summation ordering causes a one-ULP drift, sort the tokens before summing the dot product — the test pins the determinism contract.

3. **AC-DOMAIN-ANTIPROGRESS-ERROR:** `internal/domain/errors.go` gains a new sentinel:

    ```go
    ErrAntiProgress = &DomainError{
        Code:       "ANTI_PROGRESS",
        Message:    "Retries producing similar output — human review required",
        HTTPStatus: 422,
        Retryable:  false,
    }
    ```

    HTTP 422 (Unprocessable Entity) matches the "content cannot be auto-progressed; operator must intervene" semantic. `Retryable=false` matches the circuit-breaker pattern of `ErrCostCapExceeded`. The message is the exact operator-facing escalation text from the AC and FR8 — preserve it verbatim so the UI can surface it. `errors_test.go` asserts `Classify(ErrAntiProgress) == (422, "ANTI_PROGRESS", false)`.

4. **AC-CONFIG-THRESHOLD:** `domain.PipelineConfig` gains an `AntiProgressThreshold float64` field with `yaml:"anti_progress_threshold" mapstructure:"anti_progress_threshold"`. `DefaultConfig()` sets it to `0.92` (FR8 + NFR-R2). `internal/config/loader.go` adds `v.SetDefault("anti_progress_threshold", cfg.AntiProgressThreshold)` so the viper default layering mirrors the cost-cap fields. `config_test.go` asserts the default is 0.92 and that a sample YAML `anti_progress_threshold: 0.85` is loaded correctly. Cap validation: threshold MUST be in `(0.0, 1.0]` — the detector constructor rejects `≤ 0` and `> 1` (see AC-DETECTOR-CONSTRUCTOR); the default 0.92 is the V1 ship value per NFR-R2.

5. **AC-DETECTOR-CONSTRUCTOR:** `internal/pipeline/antiprogress.go` declares:

    ```go
    // AntiProgressDetector tracks consecutive retry outputs and fires when two
    // successive outputs have cosine similarity above the configured threshold.
    // Not goroutine-safe: one detector instance per stage-retry-loop per run.
    // Epic 3's Critic loop will own the lifetime — see Dev Notes.
    type AntiProgressDetector struct { /* unexported */ }

    // NewAntiProgressDetector constructs a detector. threshold MUST be in
    // (0.0, 1.0]; returns ErrValidation otherwise. The detector starts with
    // no prior output; the first Check call records the baseline and always
    // returns (false, 0.0) because there is nothing to compare against.
    func NewAntiProgressDetector(threshold float64) (*AntiProgressDetector, error)

    // Check compares output against the previous output and decides whether
    // the retry loop should stop early:
    //   - First call: stores output as the baseline, returns (false, 0.0).
    //   - Subsequent call: computes CosineSimilarity(prev, output); if
    //     similarity > threshold returns (true, similarity); otherwise returns
    //     (false, similarity). The latest output replaces the baseline
    //     regardless of the decision (next comparison is always "current vs
    //     previous", not "current vs first").
    //   - Empty output: returns (false, 0.0) and does NOT rotate the baseline
    //     (empty is not a meaningful retry output; protects against false
    //     positives where the model returns nothing).
    // stop==true is the caller's signal to break the retry loop and escalate.
    func (d *AntiProgressDetector) Check(output string) (stop bool, similarity float64)

    // LastSimilarity returns the similarity from the most recent Check call,
    // or 0 if never called. Used by callers to log/record the exact value
    // alongside retry_reason="anti_progress".
    func (d *AntiProgressDetector) LastSimilarity() float64

    // Reset clears the baseline (e.g., called when the retry loop exits for
    // any reason other than anti-progress, so the detector can be reused).
    func (d *AntiProgressDetector) Reset()
    ```

6. **AC-DETECTOR-THRESHOLD-CROSSING:** The threshold comparison uses **strict `>`** (not `>=`), matching FR8 wording "exceeds a configured threshold". Table-driven test `TestDetector_ThresholdCrossing` proves:

    | prev | curr | threshold | expected stop | expected sim |
    |---|---|---|---|---|
    | "the quick brown fox" | "the quick brown fox" | 0.92 | true | 1.0 |
    | "the quick brown fox" | "the quick brown fox jumped" | 0.92 | false | ~0.8944... |
    | "hello world" | "hello world hello world" | 0.92 | true | 1.0 |
    | "hello" | "goodbye" | 0.92 | false | 0.0 |
    | "a b c d e" | "a b c d e" | 1.0 | false | 1.0 (sim > 1.0 is impossible; `==` is NOT `>`) |
    | "a b c d e" | "a b c d e" | 0.999999 | true | 1.0 |

    The 5th row pins the strict `>` semantic: at threshold 1.0 with identical inputs, similarity is exactly 1.0 and MUST NOT trip (no value can exceed 1.0). This matters because a misreading as `>=` would make threshold 1.0 impossible to satisfy without tripping.

7. **AC-DETECTOR-CONFIGURABLE:** `TestDetector_ConfigurableThreshold` constructs detectors at thresholds `0.50`, `0.80`, `0.92`, `0.95` and runs the same input pair (cosine ~0.8944) through each. Asserts: `0.50` trips, `0.80` trips, `0.92` does NOT trip, `0.95` does NOT trip. Threshold `0.0` → constructor returns `ErrValidation`. Threshold `1.5` → constructor returns `ErrValidation`. Threshold `1.0` is valid (only exact duplicates of a single-token case could reach `> 1.0`, which is mathematically impossible; 1.0 is thus a "never trip" configuration — useful for disabling the detector in tests).

8. **AC-DETECTOR-NO-FALSE-POSITIVE:** `TestDetector_NoFalsePositiveOnDissimilar` feeds five distinct retry outputs (each a different SCP summary paragraph, ≥50 tokens, pairwise cosine ≤ 0.5) through a single detector at threshold 0.92. Asserts: every `Check` returns `stop=false`. This is the headline "no false positive on genuinely different outputs" case for NFR-R2.

9. **AC-DETECTOR-FIRST-CALL:** `TestDetector_FirstCallNeverTrips` — a fresh detector's first `Check("anything")` returns `(false, 0.0)` regardless of threshold. This is the baseline-capture semantic; a retry loop cannot be "stuck" on a single output.

10. **AC-DETECTOR-EMPTY-INPUT:** `TestDetector_EmptyInputDoesNotRotateBaseline` — after `Check("hello world")` (baseline set), `Check("")` returns `(false, 0.0)` and does NOT change the baseline. Next `Check("hello world")` returns `(true, 1.0)` at threshold 0.92 — proving the baseline was preserved across the empty call. Protects against spurious short-circuits when an LLM returns nothing.

11. **AC-RECORDER-ANTI-PROGRESS:** `internal/pipeline/observability.go` gains a convenience method:

    ```go
    // RecordAntiProgress persists an anti-progress event. Semantics:
    //   - RetryCount += 1 (this retry was attempted before we detected stuckness)
    //   - RetryReason overwrites with "anti_progress"
    //   - Cost is zero (the underlying LLM call is already charged separately
    //     via Record; RecordAntiProgress is the *decision* that short-circuits
    //     the loop, not a new external call)
    // Caller is responsible for also returning domain.ErrAntiProgress so the
    // engine routes the run to HITL. Structured slog.Warn emitted with keys:
    //   run_id, stage, similarity, threshold.
    // similarity and threshold are provided by the caller (pulled from the
    // detector's LastSimilarity + the config.AntiProgressThreshold).
    func (r *Recorder) RecordAntiProgress(
        ctx context.Context,
        runID string,
        stage domain.Stage,
        similarity float64,
        threshold float64,
    ) error
    ```

    Internally calls `r.Record(ctx, runID, obs)` where `obs` is a zero-valued `StageObservation` with `Stage=stage`, `RetryCount=1`, `RetryReason=&"anti_progress"`. The method emits ONE additional `slog.Warn("anti-progress detected", "run_id", runID, "stage", string(stage), "similarity", similarity, "threshold", threshold)` line BEFORE delegating to `Record`, so the warn line is guaranteed even if Record errors. `observability_test.go` tests: happy path emits 2 log lines (the warn + the existing info), the runs row ends with `retry_reason="anti_progress"` and `retry_count=+1`, and cost/token columns are unchanged. A second call (same run) increments `retry_count` to 2 — NOT idempotent by design.

12. **AC-RUNSTORE-ANTI-PROGRESS-WINDOW:** `internal/db/run_store.go` gains:

    ```go
    // AntiProgressStats summarizes anti-progress events over the N most recent
    // runs that tripped the detector. Inputs to NFR-R2's V1.5 ≤5% gate.
    type AntiProgressStats struct {
        Total           int  // runs with retry_reason='anti_progress' in the window
        OperatorOverride int // of Total, runs where human_override=1 (FP proxy)
        Provisional     bool // true when Total < window (insufficient data)
    }

    // AntiProgressFalsePositiveStats counts anti-progress events over the last
    // `window` runs (ordered by created_at DESC) that carry
    // retry_reason='anti_progress'. The "false-positive" definition in V1 is
    // a proxy: runs where the operator intervened post-escalation
    // (human_override=1) are treated as FP candidates. V1.5 will promote this
    // to a ground-truth signal (e.g., a subsequent successful auto-retry).
    //
    // If fewer than `window` anti-progress events exist, returns
    // Provisional=true so callers can tag the measurement accordingly.
    // Returns ErrValidation if window <= 0.
    func (s *RunStore) AntiProgressFalsePositiveStats(
        ctx context.Context,
        window int,
    ) (AntiProgressStats, error)
    ```

    SQL shape:

    ```sql
    SELECT COUNT(*) AS total,
           SUM(CASE WHEN human_override = 1 THEN 1 ELSE 0 END) AS overridden
    FROM (
        SELECT human_override
        FROM runs
        WHERE retry_reason = 'anti_progress'
        ORDER BY created_at DESC
        LIMIT ?
    );
    ```

    The subquery uses `idx_runs_created_at` (from Migration 003). The outer aggregate runs over at most `window` rows. `Provisional = (Total < window)`. `run_store_test.go` adds tests: empty DB → `{Total:0, OperatorOverride:0, Provisional:true}`; 30 anti-progress runs (20 with human_override=1) with window=50 → `{Total:30, OperatorOverride:20, Provisional:true}`; 60 anti-progress runs (3 with human_override=1) with window=50 → `{Total:50, OperatorOverride:3, Provisional:false}` (only the 50 most recent counted). `window=0` → `ErrValidation`.

13. **AC-ROLLING-WINDOW-FIXTURE:** `testdata/fixtures/anti_progress_seed.sql` seeds 60 runs with a deterministic distribution for the window test:

    | Count | retry_reason | human_override | created_at offset |
    |---|---|---|---|
    | 50 | `'anti_progress'` | `0` | `-1..-50 days` |
    | 10 | `'anti_progress'` | `1` | `-51..-60 days` |
    | 20 | `'rate_limit'` | `0` | `-1..-20 days` (decoys — must not be counted) |
    | 5  | `NULL`            | `0` | `-1..-5 days` (decoys) |

    Total runs = 85; `AntiProgressFalsePositiveStats(ctx, 50)` against this fixture returns `{Total:50, OperatorOverride:0, Provisional:false}` (the 50 most-recent anti-progress runs are all the human_override=0 ones). A second test with `window=60` returns `{Total:60, OperatorOverride:10, Provisional:false}` — the 10 older overridden ones are included. A third test with `window=100` returns `{Total:60, OperatorOverride:10, Provisional:true}` because only 60 anti-progress runs exist. The decoys (rate_limit, NULL) are never counted — proves the SQL filter.

14. **AC-INTEGRATION-DETECTOR-FLOW:** `internal/pipeline/antiprogress_integration_test.go` exercises the full detector → Recorder → runs row flow without LLM calls:

    - Load `testdata/fixtures/running_at_write.sql` (reused from Story 2.4) — one run at `stage=write, status=running`.
    - Build a real `*db.RunStore` + `Recorder` + `AntiProgressDetector(0.92)` + `FakeClock`.
    - Simulate 3 successive Writer outputs where `Check(output)` returns `stop=false` twice, then `stop=true` on the 3rd.
    - On the 3rd output: call `Recorder.RecordAntiProgress(ctx, runID, StageWrite, 0.98, 0.92)` AND return `domain.ErrAntiProgress` from the test driver.
    - Assert the post-state via `RunStore.Get`:
      - `Stage == "write"` (unchanged; detector does NOT advance the state machine — engine's HITL router does that in a future story)
      - `Status == "running"` (detector does NOT fail the run — it surfaces `ErrAntiProgress`; Epic 3 will decide whether to `SetStatus(failed)` or route to HITL)
      - `RetryCount == 1` (one recorded retry event — the short-circuit)
      - `*RetryReason == "anti_progress"`
    - Assert the error returned by the test driver wraps `domain.ErrAntiProgress` and `domain.Classify(err)` returns `(422, "ANTI_PROGRESS", false)`.
    - Assert slog captured: exactly one `"anti-progress detected"` Warn line AND one `"stage observation"` Info line (via `testutil.CaptureLog`).

15. **AC-INTEGRATION-FP-WINDOW-QUERY:** `internal/db/run_store_test.go` adds `TestAntiProgressFalsePositiveStats_RollingWindow` that seeds `anti_progress_seed.sql`, runs the three `(window=50, 60, 100)` cases from AC-ROLLING-WINDOW-FIXTURE, and asserts the exact `AntiProgressStats` for each. `EXPLAIN QUERY PLAN` on the subquery MUST show `USING INDEX idx_runs_created_at` — assertion via `strings.Contains(plan, "USING INDEX")`, mirroring Story 2.4's migration-index verification pattern.

16. **AC-NO-LLM-CALLS:** Every new test file MUST call `testutil.BlockExternalHTTP(t)` in its test setup. No test instantiates a real LLM client. All similarity inputs are static string literals or fixture-seeded data. A grep check in the test files must not find any `dashscope`, `deepseek`, or `gemini` import in this story's new test files.

17. **AC-LAYER-LINT-CLEAN:** `make lint-layers` passes unchanged. All new files live in:
    - `internal/pipeline/similarity.go` (+ `_test.go`): imports `math` stdlib + nothing internal (or `internal/domain` if using `Stage` — see Dev Notes; similarity itself should be domain-agnostic).
    - `internal/pipeline/antiprogress.go` (+ `_test.go`): imports `internal/domain` (`ErrValidation`) — already an allowed edge.
    - `internal/pipeline/antiprogress_integration_test.go`: imports `internal/db`, `internal/domain`, `internal/clock`, `internal/testutil` — all already allowed.
    - `internal/domain/errors.go` (modified): no new imports.
    - `internal/db/run_store.go` (modified): `database/sql` + `context` stdlib, `internal/domain` for the new struct's enum-like fields — already allowed.

    `scripts/lintlayers/main.go` requires **zero edits**.

18. **AC-FR-COVERAGE:** `testdata/fr-coverage.json` is updated:
    - Add `{"fr_id": "FR8", "test_ids": ["TestCosineSimilarity_GoldenPairs", "TestDetector_ThresholdCrossing", "TestDetector_NoFalsePositiveOnDissimilar", "TestIntegration_AntiProgressFlow_RecordsRetryReason"], "annotation": "anti-progress detection (cosine similarity > 0.92 → early-stop → ErrAntiProgress → HITL)"}`.
    - Annotate existing NFR-R2 coverage entry (add if missing): `{"nfr_id": "NFR-R2", "test_ids": ["TestAntiProgressFalsePositiveStats_RollingWindow"], "annotation": "rolling 50-run FP window captured (V1.5 gate deferred)"}`. If the coverage schema lacks an `nfr_id` key, skip the NFR entry and document the coverage in a comment in the test file — the fr-coverage.json schema change is out of scope.
    - `meta.last_updated` set to today's date (2026-04-17).

19. **AC-SAMPLE-CONFIG:** `cmd/pipeline/init.go` (or its helper) emits `anti_progress_threshold: 0.92` in the generated sample `config.yaml`. Since `writeConfigIfNotExists` marshals `domain.DefaultConfig()` verbatim (per Story 2.4's completion notes), adding the field to `PipelineConfig` auto-propagates. Verify via `TestInitCmd_WritesAntiProgressThreshold` (new test in `cmd/pipeline/init_test.go`): after `init`, grep the emitted YAML for `anti_progress_threshold: 0.92`.

20. **AC-DOCS:** `docs/cli-diagnostics.md` gains a 1-section appendix (≤ 12 lines) with the canonical operator query for anti-progress stats:

    ```sql
    -- Anti-progress events in the last 50 tripped runs (NFR-R2, V1.5 gate):
    SELECT COUNT(*) AS total,
           SUM(CASE WHEN human_override=1 THEN 1 ELSE 0 END) AS op_overridden
      FROM (
          SELECT human_override FROM runs
          WHERE retry_reason='anti_progress'
          ORDER BY created_at DESC
          LIMIT 50
      );
    ```

    The prose note: "V1 captures the measurement; the ≤5% FP-rate gate applies from V1.5 onward. `op_overridden / total` is a proxy for FP rate — V1.5 will refine with ground-truth signals."

21. **AC-NO-REGRESSIONS:** `go test ./... -race && go build ./... && make lint-layers && make check-fr-coverage` all pass with zero modifications to existing 1.x, 2.1–2.4 tests. `CGO_ENABLED=0` everywhere. No existing observability / cost / retry test flips.

---

## Tasks / Subtasks

- [x] **T1: pipeline/similarity.go — CosineSimilarity + Tokenize** (AC: #1, #2)
  - [x] Create `internal/pipeline/similarity.go`. Package comment: "V1 bag-of-words cosine similarity for FR8 anti-progress detection. V1.5 will replace with embedding-based similarity (deferred — see deferred-work.md)."
  - [x] Implement `Tokenize(s string) map[string]int`:
    - Lowercase via `strings.ToLower`.
    - Split on `unicode.IsSpace || unicode.IsPunct` using `strings.FieldsFunc`.
    - Skip empty tokens. Return `map[string]int` with term counts.
    - Empty input → empty map (not nil — tests can iterate without nil-check).
  - [x] Implement `CosineSimilarity(a, b string) float64`:
    - Tokenize both sides. If either map is empty → return 0.
    - Compute dot product: iterate the smaller map, look up in the larger, sum `count_a * count_b`.
    - Compute norms: `sqrt(sum(count^2))` for each map.
    - Return `dot / (normA * normB)`. Guard against `normA*normB == 0` → return 0.
  - [x] To satisfy AC-SIM-DETERMINISM: sort the inner summation keys before summing (both for dot and norm). Go's map iteration order is randomized; summation order affects floating-point results by up to 1 ULP. Sorting the keys (or using a single pass with sorted slices) pins the result bit-exact across runs.
  - [x] Create `internal/pipeline/similarity_test.go`:
    - `TestTokenize_LowercasesAndSplits` table (mixed punctuation, whitespace, unicode).
    - `TestTokenize_EmptyInputReturnsEmptyMap`.
    - `TestCosineSimilarity_GoldenPairs` with expected values to 6 decimal places. Suggested fixtures:
      - `("", "")` → 0
      - `("hello", "")` → 0
      - `("a b c", "a b c")` → 1.0
      - `("a b", "b a")` → 1.0
      - `("the quick brown fox", "the quick brown fox jumped")` → 4/sqrt(4*5) = 0.8944271909999...
      - `("hello", "goodbye")` → 0
      - `("The Quick Brown Fox", "the-quick-brown-fox")` → 1.0 (case + punctuation normalization)
      - `("aa bb cc", "cc dd ee")` → 1/3 = 0.333333...
    - `TestCosineSimilarity_Deterministic100Runs`: call 100× on `("the quick brown fox jumps over the lazy dog", "a quick brown fox jumps over the lazy dog")`, assert `math.Float64bits` identical on every call.
  - [x] `testutil.BlockExternalHTTP(t)` in every test function (paranoid habit per Story 2.3–2.4 learnings).
  - [x] Keep file < 120 lines of Go code.

- [x] **T2: pipeline/antiprogress.go — AntiProgressDetector** (AC: #5, #6, #7, #8, #9, #10)
  - [x] Create `internal/pipeline/antiprogress.go`. Package comment: "FR8 anti-progress detector. Tracks consecutive retry outputs within a stage retry loop and fires when cosine similarity exceeds the configured threshold. One instance per loop; not goroutine-safe."
  - [x] Define `AntiProgressDetector` struct with unexported fields: `threshold float64`, `previous string`, `hasPrevious bool`, `lastSim float64`.
  - [x] `NewAntiProgressDetector(threshold float64) (*AntiProgressDetector, error)`:
    - Reject `threshold <= 0` or `threshold > 1.0` → return `fmt.Errorf("anti-progress detector: threshold %.4f out of range (0, 1]: %w", threshold, domain.ErrValidation)`.
    - Otherwise return `&AntiProgressDetector{threshold: threshold}`.
  - [x] `Check(output string) (bool, float64)`:
    - If `output == ""` → return `(false, 0.0)` WITHOUT mutating state (AC-DETECTOR-EMPTY-INPUT).
    - If `!d.hasPrevious` → record `d.previous = output`, `d.hasPrevious = true`, `d.lastSim = 0.0`, return `(false, 0.0)`.
    - Compute `sim := CosineSimilarity(d.previous, output)`.
    - `d.previous = output; d.lastSim = sim`.
    - Return `(sim > d.threshold, sim)` — strict `>` (AC-DETECTOR-THRESHOLD-CROSSING).
  - [x] `LastSimilarity() float64` → return `d.lastSim`.
  - [x] `Reset()` → `d.previous = ""; d.hasPrevious = false; d.lastSim = 0.0`.
  - [x] Create `internal/pipeline/antiprogress_test.go`:
    - `TestNewAntiProgressDetector_ValidThresholds` (0.01, 0.92, 1.0) — all succeed.
    - `TestNewAntiProgressDetector_InvalidThresholds` (0.0, -0.1, 1.01, 2.0) — all return `ErrValidation` (assert via `errors.Is`).
    - `TestDetector_FirstCallNeverTrips` — fresh detector + `Check("anything")` → `(false, 0.0)`.
    - `TestDetector_ThresholdCrossing` — table-driven per AC-DETECTOR-THRESHOLD-CROSSING (6 rows, including the strict-`>` pin for threshold=1.0).
    - `TestDetector_ConfigurableThreshold` — AC-DETECTOR-CONFIGURABLE (4 thresholds, same input pair).
    - `TestDetector_NoFalsePositiveOnDissimilar` — 5 distinct SCP-flavored outputs, threshold 0.92, zero trips. Compose outputs from SCP-style sentence templates so they vary lexically but stay thematically coherent.
    - `TestDetector_EmptyInputDoesNotRotateBaseline` — AC-DETECTOR-EMPTY-INPUT full flow.
    - `TestDetector_ResetClearsBaseline` — set baseline, reset, next Check on same input returns `(false, 0.0)`.
    - `TestDetector_LastSimilarityMatches` — after trip, `LastSimilarity()` equals the similarity from the last Check.
  - [x] `testutil.BlockExternalHTTP(t)` in every test function.

- [x] **T3: domain/errors.go — ErrAntiProgress** (AC: #3)
  - [x] Add `ErrAntiProgress = &DomainError{Code: "ANTI_PROGRESS", Message: "Retries producing similar output — human review required", HTTPStatus: 422, Retryable: false}` to `internal/domain/errors.go`.
  - [x] Preserve the exact message string — it's the operator-facing escalation text. Add a one-line comment above the sentinel: `// ErrAntiProgress — FR8 hard stop; message is the operator-facing escalation text.`
  - [x] Extend `internal/domain/errors_test.go` with `TestClassify_AntiProgress`: `httpStatus, code, retryable := Classify(ErrAntiProgress); assert (422, "ANTI_PROGRESS", false)`.
  - [x] Add `TestClassify_AntiProgress_WrappedError`: `err := fmt.Errorf("stage write: %w", ErrAntiProgress); Classify(err) → (422, "ANTI_PROGRESS", false)` (proves errors.As traversal).

- [x] **T4: domain/config.go — AntiProgressThreshold** (AC: #4)
  - [x] Add `AntiProgressThreshold float64 `yaml:"anti_progress_threshold" mapstructure:"anti_progress_threshold"`` to `domain.PipelineConfig`. Place it directly below `CostCapPerRun` (logical grouping with other "circuit-breaker thresholds").
  - [x] `DefaultConfig()` sets `AntiProgressThreshold: 0.92`.
  - [x] Extend `internal/domain/config_test.go` with `TestDefaultConfig_AntiProgressThreshold`: `assert cfg.AntiProgressThreshold == 0.92`.

- [x] **T5: config/loader.go — viper default for threshold** (AC: #4)
  - [x] Edit `internal/config/loader.go` to add `v.SetDefault("anti_progress_threshold", cfg.AntiProgressThreshold)` in the `SetDefault` block (grouped near the existing `cost_cap_*` defaults).
  - [x] Extend `internal/config/loader_test.go` with `TestLoad_AntiProgressThresholdDefault` (no config file → threshold = 0.92) and `TestLoad_AntiProgressThresholdOverride` (write a temp YAML containing `anti_progress_threshold: 0.85` → Load returns cfg.AntiProgressThreshold == 0.85).
  - [x] **CRITICAL:** Without the `SetDefault` call, `v.Unmarshal(&cfg)` may zero the field if viper has no registered key for it (the existing `cost_cap_per_run` field has this same risk — verify by adding the SetDefault call there too if missing; but that is Story 2.4 scope, log as deferred-work if the bug is confirmed).

- [x] **T6: pipeline/observability.go — RecordAntiProgress** (AC: #11)
  - [x] Extend `internal/pipeline/observability.go` with `Recorder.RecordAntiProgress(ctx, runID, stage, similarity, threshold)`.
  - [x] Implementation:
    1. Emit `r.logger.Warn("anti-progress detected", "run_id", runID, "stage", string(stage), "similarity", similarity, "threshold", threshold)` FIRST (guaranteed even if Record errors).
    2. Build `reason := "anti_progress"`; `obs := domain.StageObservation{Stage: stage, RetryCount: 1, RetryReason: &reason}`.
    3. Delegate to `r.Record(ctx, runID, obs)` and return its result.
  - [x] Note in the doc comment: "Caller is responsible for ALSO returning `domain.ErrAntiProgress` to short-circuit the retry loop — this method only records the event, it does NOT alter the run's stage/status (NFR-P3-analogous: observability does not drive state transitions)."
  - [x] Extend `internal/pipeline/observability_test.go`:
    - `TestRecorder_RecordAntiProgress_ShapeAndLogs`: inject `testutil.CaptureLog` logger; call RecordAntiProgress; assert: one "anti-progress detected" Warn line (with sim+threshold), one "stage observation" Info line. Assert the fake store got one call with `obs.RetryCount=1`, `*obs.RetryReason=="anti_progress"`, `obs.CostUSD==0`.
    - `TestRecorder_RecordAntiProgress_SecondCallIncrementsRetry`: call twice on the same run; expect `retry_count` becomes 2 in the store (via inline fake that accumulates).
    - `TestRecorder_RecordAntiProgress_PropagatesStoreError`: inline fake returns an error; assert RecordAntiProgress returns a wrapped error containing that underlying error AND the Warn line was still emitted.

- [x] **T7: db/run_store.go — AntiProgressFalsePositiveStats** (AC: #12)
  - [x] Add `AntiProgressStats` struct to `internal/db/run_store.go` (or a new `internal/db/anti_progress.go` if run_store.go exceeds 300 lines — check current size first).
  - [x] Implement `(s *RunStore) AntiProgressFalsePositiveStats(ctx, window int) (AntiProgressStats, error)`:
    - `window <= 0` → `domain.ErrValidation` wrapped.
    - Execute the SQL from AC-RUNSTORE-ANTI-PROGRESS-WINDOW.
    - Scan into `total int` and `overridden sql.NullInt64` (the SUM returns NULL when total=0).
    - Build `AntiProgressStats{Total: total, OperatorOverride: int(overridden.Int64), Provisional: total < window}`.
  - [x] Extend `internal/db/run_store_test.go`:
    - `TestAntiProgressFalsePositiveStats_EmptyDB`: fresh DB → `{Total:0, OperatorOverride:0, Provisional:true}` at window=50.
    - `TestAntiProgressFalsePositiveStats_RollingWindow`: load `anti_progress_seed.sql`, run three cases from AC-ROLLING-WINDOW-FIXTURE (windows 50, 60, 100), assert the exact `AntiProgressStats`.
    - `TestAntiProgressFalsePositiveStats_InvalidWindow`: `window=0` → `ErrValidation`; `window=-1` → `ErrValidation`.
    - `TestAntiProgressFalsePositiveStats_UsesCreatedAtIndex`: run `EXPLAIN QUERY PLAN` on the exact SQL; assert the plan contains `USING INDEX idx_runs_created_at` OR the weaker `USING INDEX` substring (matches Story 2.4's pattern).
    - `TestAntiProgressFalsePositiveStats_IgnoresNonAntiProgressRows`: seed with rows carrying `retry_reason='rate_limit'` + `retry_reason=NULL` + `retry_reason='anti_progress'`; assert only the anti_progress rows count.
  - [x] `testutil.BlockExternalHTTP(t)` in every new test function. Use `testutil.NewTestDB(t)` for the DB.

- [x] **T8: testdata/fixtures/anti_progress_seed.sql** (AC: #13)
  - [x] Create `testdata/fixtures/anti_progress_seed.sql` with 85 INSERT rows per AC-ROLLING-WINDOW-FIXTURE distribution.
  - [x] Deterministic data — hard-coded `created_at` via `datetime('now', '-N days')` with distinct N values; hard-coded cost values (any plausible number, not used by the queries).
  - [x] Header comment documents the exact distribution so future test additions don't accidentally flip the assertions:
    ```sql
    -- Fixture for TestAntiProgressFalsePositiveStats_RollingWindow (Story 2.5, NFR-R2).
    -- Distribution:
    --   50 × retry_reason='anti_progress', human_override=0, created_at in [-1..-50 days]
    --   10 × retry_reason='anti_progress', human_override=1, created_at in [-51..-60 days]
    --   20 × retry_reason='rate_limit',   human_override=0, created_at in [-1..-20 days] (decoys)
    --    5 × retry_reason=NULL,           human_override=0, created_at in [-1..-5 days] (decoys)
    -- Expected: window=50 → {50, 0, false}; window=60 → {60, 10, false}; window=100 → {60, 10, true}.
    ```

- [x] **T9: Integration test — detector → recorder → runs row** (AC: #14)
  - [x] Create `internal/pipeline/antiprogress_integration_test.go`:
    - Use `testutil.NewTestDB(t)` + `testutil.LoadRunStateFixture(t, "running_at_write")` (reused from Story 2.4).
    - Construct real `*db.RunStore`, a `CostAccumulator` (no caps tripped), a `Recorder` with captured logger, and a detector at threshold 0.92.
    - Drive three Check calls: (1) baseline `"scp-049 describes an entity with anomalous properties..."`; (2) dissimilar `"the foundation classified the item after containment protocol review."` (→ stop=false); (3) near-duplicate of (1) `"scp-049 describes an entity with anomalous properties."` (→ stop=true, sim near 1.0).
    - On stop=true: call `Recorder.RecordAntiProgress(ctx, runID, StageWrite, detector.LastSimilarity(), 0.92)`; assert error is nil.
    - Assert post-state via `runStore.Get(ctx, runID)`:
      - `Stage == StageWrite`
      - `Status == StatusRunning`
      - `RetryCount == 1`
      - `*RetryReason == "anti_progress"`
      - `CostUSD == 0` (detector does not add cost)
    - Assert `testutil.CaptureLog` shows exactly one `"anti-progress detected"` Warn line and one `"stage observation"` Info line.
    - Then simulate the caller short-circuiting: construct `err := fmt.Errorf("write stage: %w", domain.ErrAntiProgress)`; assert `errors.Is(err, domain.ErrAntiProgress)` and `domain.Classify(err) == (422, "ANTI_PROGRESS", false)`.
  - [x] `testutil.BlockExternalHTTP(t)`.

- [x] **T10: Docs + sample config** (AC: #19, #20)
  - [x] Verify `cmd/pipeline/init.go` sample-config path emits `anti_progress_threshold: 0.92` automatically via marshaling `domain.DefaultConfig()`. Add a regression test `TestInitCmd_WritesAntiProgressThreshold` in `cmd/pipeline/init_test.go`: run init, read the generated yaml, assert `anti_progress_threshold: 0.92` substring is present.
  - [x] Append a section to `docs/cli-diagnostics.md` per AC-DOCS (≤12 lines). Korean prose + SQL code block. Link from the existing "Anti-progress telemetry" mention in `ux-design-specification.md:238` is not in scope — docs/cli-diagnostics.md is a standalone operator reference.

- [x] **T11: FR coverage update** (AC: #18)
  - [x] Edit `testdata/fr-coverage.json`:
    - Add `{"fr_id": "FR8", "test_ids": ["TestCosineSimilarity_GoldenPairs", "TestDetector_ThresholdCrossing", "TestDetector_NoFalsePositiveOnDissimilar", "TestIntegration_AntiProgressFlow_RecordsRetryReason"], "annotation": "anti-progress detection (cosine > 0.92 → ErrAntiProgress → HITL)"}`.
    - Update `meta.last_updated` to today's date (2026-04-17).
    - Inspect the schema: the existing file uses `fr_id` as the key. Do NOT add an `nfr_id` shaped entry unless the schema supports it — if the contract test rejects it, document NFR-R2 coverage in a `NFR-R2` comment block within `TestAntiProgressFalsePositiveStats_RollingWindow` and the test file header instead.

- [x] **T12: Lint + green build** (AC: #17, #21)
  - [x] Run `go build ./...`, `go test -race ./...`, `make lint-layers`, `make check-fr-coverage`. All must pass with zero changes to existing 1.x / 2.1–2.4 tests.
  - [x] Confirm no new layer-lint edge is needed (`similarity.go` uses only `math` + `sort` + `strings` + `unicode`; `antiprogress.go` adds `internal/domain` — already allowed).
  - [x] Smoke: `go test ./internal/pipeline/... -run AntiProgress -v`, `go test ./internal/pipeline/... -run Similarity -v`.

- [x] **T13: Deferred work logging**
  - [x] Append to `_bmad-output/implementation-artifacts/deferred-work.md` a new `## Deferred from: implementation of 2-5-anti-progress-detection (YYYY-MM-DD)` section. Expected items (flesh out during implementation/review):
    - **Embedding-based cosine (V1.5):** V1 uses bag-of-words TF cosine; V1.5 should promote to dense-vector embeddings for semantic-equivalence detection. Ticketed when Epic 3's Writer lands.
    - **Ground-truth FP signal:** V1 uses `human_override=1` as an FP proxy; V1.5 needs a concrete operator decision path (e.g., "override + next auto-retry succeeded" = FP-confirmed).
    - **Detector goroutine-safety:** V1 keeps one detector per retry loop; if Epic 3 runs parallel Critic loops for different stages they can each instantiate their own. Document if this constraint becomes inconvenient.
    - **AntiProgressThreshold viper SetDefault gap for CostCapPerRun:** while wiring T5, check whether `cost_cap_per_run` is also missing a `SetDefault` call in `loader.go` — if so, that's a latent bug in Story 2.4 that would cause config-file loads to zero out the cap. Fix (or log as separate deferred item) during code review.
    - **Operator escalation UX:** Story 2.5 surfaces `ErrAntiProgress` but does not wire it to a HITL surface (Epic 7/8 scope). The error code + message are the V1 contract; the UI will render them once web surfaces exist.

### Review Findings

**Patch (10 — need fix):**

- [x] [Review][Patch] `gofmt` misalignment in `DefaultConfig()` struct literal [`internal/domain/config.go:49-66`] — top fields align to short tab stop, bottom three (`CostCapAssemble`, `CostCapPerRun`, `AntiProgressThreshold`) use a longer one; run `gofmt -w` to normalize.
- [x] [Review][Patch] `TestInitCmd_WritesAntiProgressThreshold` mutates package-level `cfgPath` without `t.Cleanup` restore [`cmd/pipeline/init_test.go:97-118`] — test-ordering-dependent; add cleanup to save+restore previous value.
- [x] [Review][Patch] `TestAntiProgressFalsePositiveStats_UsesCreatedAtIndex` only asserts generic `USING INDEX` substring, not the specific `idx_runs_created_at` [`internal/db/run_store_test.go:292-297`] — silently passes on any other index being chosen; also assert the named index OR absence of `SCAN runs`.
- [x] [Review][Patch] `fr-coverage.json` `meta.last_updated` backdated from `2026-04-18` to `2026-04-17` [`testdata/fr-coverage.json:5`] — should be today's date (`2026-04-18`) to preserve monotonic freshness.
- [x] [Review][Patch] `Tokenize` docstring claims "stable across runs (no randomness)" but returns a Go map whose iteration order is randomized [`internal/pipeline/similarity.go:12-14`] — clarify: tokenization is deterministic, iteration over the map is not; downstream `CosineSimilarity` handles ordering via sorted-key summation.
- [x] [Review][Patch] Whitespace-only output (`"   "`, `"\n\t"`) rotates the detector baseline and effectively disables one comparison round [`internal/pipeline/antiprogress.go:51-53`] — guard with `strings.TrimSpace(output) == ""` (same branch as the empty check) so whitespace-only outputs do not rotate the baseline.
- [x] [Review][Patch] `CosineSimilarity` can produce `sim ∈ {0.9999999999999998, 1.0000000000000002}` on adversarial repetition due to FP rounding [`internal/pipeline/similarity.go:67-73`] — defensively clamp the result to `[0, 1]` so the detector's strict-`>` contract is never violated by FP overshoot.
- [x] [Review][Patch] Integration test does not exercise `Record(cost=0.05)` followed by `RecordAntiProgress` to prove cost is retained across the anti-progress event [`internal/pipeline/antiprogress_integration_test.go:45-76`] — add a case that records a Writer cost first, then trips anti-progress, then asserts `runs.cost_usd == 0.05`.
- [x] [Review][Patch] `AntiProgressFalsePositiveStats` SQL lacks a secondary sort key; ties on second-precision `created_at` produce undefined window membership [`internal/db/run_store.go:336-340`] — change `ORDER BY created_at DESC` to `ORDER BY created_at DESC, id DESC`.
- [x] [Review][Patch] `TestDetector_ResetClearsBaseline` does not explicitly assert `LastSimilarity() == 0` after `Reset()` [`internal/pipeline/antiprogress_test.go:165-181`] — add the assertion so a regression where `Reset` forgets to clear `lastSim` is caught.

**Defer (5 — logged to deferred-work):**

- [x] [Review][Defer] Anti-progress warn-before-persist can leave an obs log line without a DB row on `Record` error [`internal/pipeline/observability.go:432-450`] — deferred, revisit when Epic 3 wires escalation UX.
- [x] [Review][Defer] V1 bag-of-words `Tokenize` degrades on Korean text (Hangul not lowercased, no 조사/어미 stemming, no NFC/NFKC normalization) [`internal/pipeline/similarity.go:14-30`] — deferred to V1.5 embeddings (already logged in deferred-work); explicit Korean caveat added.
- [x] [Review][Defer] `AntiProgressDetector` goroutine-safety is documented but not enforced [`internal/pipeline/antiprogress.go:10-16`] — deferred; V1 single-loop contract acceptable.
- [x] [Review][Defer] AC-12 listed two illustrative inline scenarios (`{30, 20, true}` and `{60, 3, false}`) beyond the fixture-based test [spec AC-RUNSTORE-ANTI-PROGRESS-WINDOW] — deferred, fixture test already pins the invariants.
- [x] [Review][Defer] `AntiProgressFalsePositiveStats` may not use `idx_runs_created_at` efficiently when `anti_progress` rows are sparse relative to total runs — composite `(retry_reason, created_at DESC)` index would be faster but requires a new migration — deferred; revisit if real-operator queries slow.

---

## Dev Notes

### Why V1 Cosine Is Bag-of-Words, Not Embeddings

FR8 and NFR-R2 explicitly say "cosine similarity" and test ACs forbid LLM calls. The architecture gap analysis [architecture.md:1853] hedges V1 with "Jaccard or normalized Levenshtein" — but that contradicts the word "cosine" used throughout the PRD and epics. The reconciliation: **cosine** is the specified metric; the **vector space** is the flexible part. In V1 we compute cosine over **term-frequency vectors** (bag-of-words), which is:

- Deterministic (pure function of the input strings).
- No external calls (satisfies "no LLM calls in tests" + the V1 tech-stack freeze — no embedding vendor is provisioned yet).
- Mathematically a cosine similarity (dot product over L2 norms).
- A reasonable proxy for "these two outputs say the same thing in mostly the same words" — the failure mode the retry loop produces.

V1.5 will replace `CosineSimilarity` (same signature) with an embedding-based implementation (`internal/llmclient/embed.go`, DashScope vector API). The detector API does not change. Deferred-work entry records this.

Test inputs in this story use word-overlap-controlled strings so the expected cosine values are hand-computable, which is the right trade-off for a deterministic-input test suite.

### Threshold Semantics — Strict `>` Not `>=`

FR8 wording: "exceeds a configured threshold (default 0.92)". **Exceeds = strict `>`**. This matters at threshold `1.0`:
- `>` version: similarity of exactly 1.0 does NOT trip (no value exceeds 1.0).
- `>=` version: similarity of exactly 1.0 would trip on IDENTICAL inputs, even at the most permissive threshold.

The strict-`>` version gives operators a "safe off switch" (threshold=1.0 = never trip). Test case #5 in the AC table pins this. If a future product decision wants `>=`, it's a one-character change but also a spec change — raise an ADR, don't slip it silently.

### Why Empty Output Doesn't Rotate the Baseline

If an LLM returns an empty string (transient vendor weirdness), a naive implementation would:
1. Previous output: "long useful paragraph"
2. Current output: "" → cosine = 0 (below any threshold) → no trip
3. Baseline rotated to "" → next output: "long useful paragraph" → cosine = 0 → no trip

Now the detector thinks two genuinely-identical outputs separated by an empty one are "dissimilar". That's a false negative. Skipping baseline rotation on empty input avoids this (AC-DETECTOR-EMPTY-INPUT pins it). The trade-off is accepting that a legitimately-intended empty retry output won't be captured as "progressing" — acceptable because an empty output is almost always a transient error that the 429/timeout path handles, not an intended retry.

### Why `ErrAntiProgress` Is HTTP 422, Not 409 or 500

- 409 (Conflict) is for resource state conflicts (e.g., trying to cancel an already-completed run).
- 422 (Unprocessable Entity) is for "request is well-formed but the content cannot be processed" — semantically closest to "the retry loop gave up because the model can't produce a useful variation."
- 500 (Internal Server Error) would bury the operator-actionable escalation in a generic failure bucket.

422 also aligns with the `Retryable=false` flag — it's a content problem, not a transient error; the operator must act.

### Why Recorder Doesn't Advance Stage/Status on Anti-Progress

Story 2.4's invariant: the observability path never calls `SetStatus`. Anti-progress detection is the **decision to stop retrying**, not the **decision to fail the run**. Those decisions are separated:

1. Detector → `stop=true`: the retry loop's controller (Epic 3's Critic loop) decides what to do next.
2. The controller MAY route the run to a HITL wait stage (e.g., `scenario_review`), or fail it, or pause it.
3. Recorder only captures the *event* (`retry_reason="anti_progress"`) so the operator can see it in the DB.

This keeps Story 2.5's responsibility narrow and avoids re-entering the state-machine design decisions from Story 2.1. The integration test (T9) asserts `Stage == "write"` and `Status == "running"` post-anti-progress — if a future refactor changes this, the test forces a deliberate re-evaluation.

### The FP Measurement Is a Proxy in V1 — by Design

NFR-R2 says "measured throughout V1" but the ≤5% gate applies from V1.5. The V1 measurement is a **plumbing-only** deliverable:

- **Record** anti-progress events on the runs row (via `Recorder.RecordAntiProgress`).
- **Query** rolling-window stats via `RunStore.AntiProgressFalsePositiveStats`.
- **Proxy definition of FP**: `human_override=1` on an anti-progress run means the operator overrode the auto-decision — imperfect but non-zero signal.

V1.5 will refine the definition (e.g., "anti-progress fired AND the operator manually resumed AND the resumed stage succeeded without re-tripping"). That requires operator-decision plumbing (Epic 7/8) and is correctly out of scope here.

The `docs/cli-diagnostics.md` appendix (T10) warns operators about the V1 proxy so they don't treat the number as ground truth.

### Detector Is Not Goroutine-Safe — Justification

Epic 3's Critic loop is sequential per stage: Writer → Critic → [decision] → Writer or HITL. One detector instance per loop, no concurrent access. Adding a mutex is dead weight.

Phase B's parallel image+TTS tracks each have their own retry loops (different stages, different agents, different failure modes). They would each instantiate their own `AntiProgressDetector` — no sharing. The package comment pins this contract; future attempts to share across goroutines should fail fast at a race-detector pass, not via a silent data race.

### What the Engine Does NOT Do in Story 2.5

- No `NextStage` call with a new event.
- No `SetStatus(failed)`.
- No route to HITL stage (`scenario_review`, etc.) — Epic 3 decides that.
- No `Recorder.RecordAntiProgress`-driven side effect on cost accumulator (cost is already accounted for by the underlying Writer call — this method is the *decision* event, not a new API call).

Story 2.5 ships the **utility + the capture surface**. Epic 3 Story 3.3 (Writer agent + Critic + post-writer checkpoint) is where it gets wired. The integration test (T9) proves the pieces compose correctly without depending on Epic 3 code.

### Why Not Put Similarity in `internal/domain` or `internal/llmclient`?

- `internal/domain`: domain types + errors + config. Adding executable string math here bloats the domain layer and couples it to a specific similarity algorithm.
- `internal/llmclient`: for external-API wrappers (DashScope, DeepSeek, Gemini). The V1 similarity implementation calls no external API — putting it here would mislead readers.
- `internal/pipeline`: correct home. `pipeline/` is allowed to import `domain`, `db`, `llmclient`, `clock`. The detector is a **pipeline-layer concern** (drives loop control), and placing it alongside `cost.go` / `observability.go` groups all circuit-breaker primitives in one package.

When V1.5 replaces the TF implementation with embeddings, the *similarity* function may move to `internal/llmclient/embed.go` (or call into it). The *detector* stays in `pipeline/`.

### `retry_reason="anti_progress"` Canonical String

Align with Story 2.4's existing canonical set (`"rate_limit"`, `"timeout"`, `"stage_failed"`). Lowercase, underscore-separated. Do NOT introduce `"anti-progress"` or `"anti_progress_detected"` — consistency with existing values keeps the diagnostic queries (`SELECT ... WHERE retry_reason='anti_progress' ...`) simple.

Suggestion: hoist all canonical retry reasons into a `domain/retry_reasons.go` const block? — **No, defer**. Store 2.4 already has them as string literals scattered across `retry.go` + tests. Consolidating is a cleanup that deserves its own small story (log as deferred-work). This story just adds one more literal in two places (Recorder + SQL fixture).

### Previous Story Learnings Applied

From 2.1:
- Pure-function preference — `CosineSimilarity` and `Tokenize` are pure. No DB, no clock.
- State machine untouched — detector returns a signal, does not mutate stage.

From 2.2:
- `domain.Classify` is the single error classifier. `ErrAntiProgress` flows through it.
- snake_case JSON everywhere — applies to config key `anti_progress_threshold`.
- Module path `github.com/sushistack/youtube.pipeline`.
- CGO_ENABLED=0.

From 2.3:
- Local interface declarations when needed — not needed for Story 2.5 (detector is concrete; no store layer to abstract).
- `testutil.BlockExternalHTTP(t)` in every new test file.
- `testutil.NewTestDB(t)` for DB tests.

From 2.4:
- `Recorder` is the only path that mutates the 8 observability columns — `RecordAntiProgress` MUST delegate to `Recorder.Record` (already done in the AC).
- 8-column semantics: `retry_reason` overwrites (so `"anti_progress"` displaces any prior `"rate_limit"` / `"timeout"`), `retry_count` accumulates.
- Inline fakes (no testify, no gomock).
- `COALESCE` semantics for retry_reason preservation when nil — irrelevant here because RecordAntiProgress always sets it non-nil.
- Migration 003's `idx_runs_created_at` is the right backing index for the window query — reuse, don't add a new index.

### Deferred Work Awareness (Do Not Resolve Here)

- `BlockExternalHTTP` global mutation (1.4/1.7/2.1–2.4): use as-is.
- `Migrate` + `PRAGMA user_version` outside transaction (1.4): don't fix.
- Vite dev-mode middleware bypass (2.2): not relevant.
- `retry_reason` COALESCE opinionation (2.4): not relevant; this story always sets it.
- Cost accumulator priming on restart (2.4): not relevant.
- Jitter through clock (2.4): not relevant — no jitter in this story.

### Deferred Work This Story May Generate (Log in T13 / Code Review)

- **Embedding cosine (V1.5):** replace bag-of-words TF with real embeddings once a vendor is provisioned. Signature of `CosineSimilarity` stays stable.
- **Ground-truth FP definition:** the `human_override=1` proxy is a V1 compromise. V1.5 needs a deterministic definition (e.g., "operator rejected anti-progress stop + subsequent Writer succeeded without re-tripping").
- **Canonical retry-reason enum:** consolidate the 4 retry reason strings (`"rate_limit"`, `"timeout"`, `"stage_failed"`, `"anti_progress"`) into a `domain.RetryReason` string type + const block. Mechanical; out of scope here.
- **AntiProgress + Cost Cap interaction:** what if a retry sequence trips both ErrCostCapExceeded AND anti-progress? Cost-cap wins in V1 (the retry loop checks cost before making the next call). Document if this ordering produces a confusing `retry_reason` value in practice.
- **`SetDefault` missing for `cost_cap_per_run`:** flag during T5 if the latent bug from Story 2.4 is confirmed.
- **Detector parallelism:** if Epic 3 ever needs a shared detector across goroutines, add a mutex + document the change.
- **`docs/cli-diagnostics.md` schema-versioning:** the appendix section makes this doc multi-topic; if it grows, split into numbered sub-docs. Not urgent.

### Project Structure After This Story

```
internal/
  domain/
    errors.go                       # MODIFIED — add ErrAntiProgress sentinel
    errors_test.go                  # MODIFIED — TestClassify_AntiProgress
    config.go                       # MODIFIED — add AntiProgressThreshold field + default
    config_test.go                  # MODIFIED — assert new default
  pipeline/
    similarity.go                   # NEW — CosineSimilarity + Tokenize
    similarity_test.go              # NEW
    antiprogress.go                 # NEW — AntiProgressDetector
    antiprogress_test.go            # NEW
    antiprogress_integration_test.go # NEW — detector → Recorder → runs row flow
    observability.go                # MODIFIED — add Recorder.RecordAntiProgress
    observability_test.go           # MODIFIED — RecordAntiProgress shape + log
  db/
    run_store.go                    # MODIFIED — AntiProgressFalsePositiveStats
    run_store_test.go               # MODIFIED — window query tests
  config/
    loader.go                       # MODIFIED — viper default for anti_progress_threshold
    loader_test.go                  # MODIFIED — default + override tests
cmd/pipeline/
  init.go                           # UNCHANGED (auto-marshals new field)
  init_test.go                      # MODIFIED — grep generated yaml for threshold key
testdata/
  fixtures/
    anti_progress_seed.sql          # NEW — 85 rows for rolling-window test
testdata/
  fr-coverage.json                  # MODIFIED — FR8 entry added
docs/
  cli-diagnostics.md                # MODIFIED — anti-progress appendix
_bmad-output/
  implementation-artifacts/
    deferred-work.md                # MODIFIED — T13 entries
    sprint-status.yaml              # MODIFIED — 2-5 moved backlog → ready-for-dev (by create-story)
```

### Critical Constraints

- **V1 similarity is bag-of-words TF cosine** — deterministic, no external calls.
- **Strict `>` threshold comparison** — `>=` is a spec change, not a refactor.
- **Empty output does NOT rotate baseline** — avoids false negative on transient empty returns.
- **`ErrAntiProgress` is HTTP 422 + Retryable=false** — circuit-breaker pattern, matches `ErrCostCapExceeded`.
- **Recorder never advances stage/status** — anti-progress records the event; Epic 3's Critic loop controller decides next steps.
- **FP measurement is a proxy in V1** — `human_override=1` on anti-progress runs; V1.5 refines.
- **One detector per retry loop** — not goroutine-safe; Epic 3 owns lifecycle.
- **Use Migration 003's `idx_runs_created_at`** for the window query — no new index needed.
- **snake_case JSON** for config key: `anti_progress_threshold`.
- **Module path** `github.com/sushistack/youtube.pipeline`. **CGO_ENABLED=0.** **`testutil.BlockExternalHTTP(t)` in every new test file.**
- **No testify, no gomock** — inline fakes + `testutil.AssertEqual[T]`.
- **Canonical `retry_reason` values**: `"rate_limit"`, `"timeout"`, `"stage_failed"`, `"anti_progress"` (new). Lowercase, underscore-separated.
- **Preserve the operator-facing message** `"Retries producing similar output — human review required"` verbatim on `ErrAntiProgress` — it is the UI surface text.

### Project Structure Notes

- All new files fit inside existing allowed-import edges in `scripts/lintlayers/main.go:21-33`. No layer-lint rule edits.
- `internal/pipeline/` is the correct home: detector is a pipeline-layer concern; `domain/` stays free of algorithmic code; `llmclient/` is for external-API wrappers.
- V1.5 embedding migration will add `internal/llmclient/embed.go`; the detector's dependency on `CosineSimilarity` stays stable (drop-in replacement).
- `testdata/fixtures/anti_progress_seed.sql` lives alongside `observability_seed.sql` — mirrors the Story 2.4 pattern.

### References

- Epic 2 scope and FRs: [epics.md:378-399](../planning-artifacts/epics.md#L378-L399)
- Story 2.5 AC (BDD): [epics.md:1040-1066](../planning-artifacts/epics.md#L1040-L1066)
- FR8 (anti-progress detection): [epics.md:30](../planning-artifacts/epics.md#L30), [prd.md:1251](../planning-artifacts/prd.md#L1251)
- NFR-R2 (FP rate rolling-50 measurement): [epics.md:87](../planning-artifacts/epics.md#L87), [prd.md:1361-1365](../planning-artifacts/prd.md#L1361-L1365)
- Architecture gap: cosine similarity utility location: [architecture.md:1853](../planning-artifacts/architecture.md#L1853)
- Time abstraction + anti-progress timing: [architecture.md:205](../planning-artifacts/architecture.md#L205)
- Cost-tracking circuit-breaker pattern (sibling discipline): [architecture.md:124,201-202](../planning-artifacts/architecture.md#L124)
- Error classification + `errors.Is` pattern: [architecture.md:612-622](../planning-artifacts/architecture.md#L612-L622)
- PRD Mode 4 edge case example (this exact scenario): [prd.md:566-580](../planning-artifacts/prd.md#L566-L580)
- UX mermaid — anti-progress stop path: [ux-design-specification.md:1933-1935](../planning-artifacts/ux-design-specification.md#L1933-L1935)
- Implementation readiness — Case 1 (anti-progress): [implementation-readiness-report-2026-04-16.md:592-601](../planning-artifacts/implementation-readiness-report-2026-04-16.md#L592-L601)
- Sprint review checkpoints (cosine accuracy, threshold config, FP DB capture, LLM-free tests): [sprint-prompts.md:327-333](../planning-artifacts/sprint-prompts.md#L327-L333)
- Domain errors (add ErrAntiProgress here): [internal/domain/errors.go](../../internal/domain/errors.go)
- PipelineConfig (add AntiProgressThreshold): [internal/domain/config.go](../../internal/domain/config.go)
- Recorder (extend with RecordAntiProgress): [internal/pipeline/observability.go](../../internal/pipeline/observability.go)
- RunStore (extend with AntiProgressFalsePositiveStats): [internal/db/run_store.go](../../internal/db/run_store.go)
- Config loader (add SetDefault): [internal/config/loader.go](../../internal/config/loader.go)
- Migration 003 — created_at index backing the window query: [migrations/003_observability_indexes.sql](../../migrations/003_observability_indexes.sql)
- testutil helpers: [internal/testutil/nohttp.go](../../internal/testutil/nohttp.go), [internal/testutil/db.go](../../internal/testutil/db.go), [internal/testutil/slog.go](../../internal/testutil/slog.go), [internal/testutil/fixture.go](../../internal/testutil/fixture.go)
- Layer-lint rules (no edits required): [scripts/lintlayers/main.go:21-33](../../scripts/lintlayers/main.go#L21-L33)
- FR coverage tracker: [testdata/fr-coverage.json](../../testdata/fr-coverage.json)
- Deferred work registry: [deferred-work.md](deferred-work.md)
- Previous story (2.4): [2-4-per-stage-observability-cost-tracking.md](2-4-per-stage-observability-cost-tracking.md)
- Previous story (2.3): [2-3-stage-level-resume-artifact-lifecycle.md](2-3-stage-level-resume-artifact-lifecycle.md)
- Previous story (2.1): [2-1-state-machine-core-stage-transitions.md](2-1-state-machine-core-stage-transitions.md)

## Dev Agent Record

### Agent Model Used

claude-opus-4-7

### Debug Log References

None.

### Completion Notes List

- **`internal/pipeline/similarity.go` (new):** `Tokenize` splits on Unicode whitespace+punctuation with `strings.FieldsFunc`, lowercases via `strings.ToLower`, returns non-nil empty map for empty input. `CosineSimilarity` computes bag-of-words cosine with sorted-key summation for bit-exact determinism across Go's randomized map iteration order. Pure function, no external calls, no state.
- **`internal/pipeline/antiprogress.go` (new):** `AntiProgressDetector` state: `threshold`, `previous`, `hasPrevious`, `lastSim`. `NewAntiProgressDetector` rejects threshold ≤ 0 or > 1 with wrapped `ErrValidation`. `Check` uses **strict `>`** — threshold 1.0 disables tripping. Empty output does NOT rotate the baseline (protects against transient empty returns). `Reset()` restores first-call behavior.
- **`internal/domain/errors.go` (modified):** Added `ErrAntiProgress` sentinel with HTTP 422, `Retryable=false`, message `"Retries producing similar output — human review required"` (preserved verbatim — UI escalation text). Sentinel count grew 7 → 8.
- **`internal/domain/config.go` (modified):** Added `AntiProgressThreshold float64` field with yaml/mapstructure tags. `DefaultConfig()` sets `0.92` (FR8 + NFR-R2 V1 ship value). Located next to `CostCapPerRun` to group circuit-breaker thresholds.
- **`internal/config/loader.go` (modified):** Added `v.SetDefault("cost_cap_per_run", …)` + `v.SetDefault("anti_progress_threshold", …)` to match the rest of the cost_cap_* block and make the default contract explicit.
- **`internal/pipeline/observability.go` (modified):** `Recorder.RecordAntiProgress(ctx, runID, stage, similarity, threshold)` emits `slog.Warn("anti-progress detected", …)` FIRST (fire-and-forget logging survives Record errors), then delegates to `Record` with `RetryCount=1` + `RetryReason=&"anti_progress"` + zero cost. Caller is responsible for ALSO surfacing `domain.ErrAntiProgress` — the Recorder does not advance stage/status (same invariant as Story 2.4's NFR-P3 discipline).
- **`internal/db/run_store.go` (modified):** Added `AntiProgressStats{Total, OperatorOverride, Provisional}` + `AntiProgressFalsePositiveStats(ctx, window)`. SQL uses a `WHERE retry_reason='anti_progress' ORDER BY created_at DESC LIMIT ?` subquery (hits Migration 003's `idx_runs_created_at`) with an outer `COUNT(*) + SUM(CASE …)`. `Provisional = total < window`. `window <= 0` → wrapped `ErrValidation`.
- **`testdata/fixtures/anti_progress_seed.sql` (new):** 85 rows — 50 anti-progress no-override (-1..-50 days), 10 anti-progress with override (-51..-60 days), 20 rate_limit decoys, 5 NULL-retry_reason decoys. Three window assertions: `{50, 0, false}`, `{60, 10, false}`, `{60, 10, true}`.
- **`internal/pipeline/antiprogress_integration_test.go` (new):** Drives the detector → Recorder → real RunStore flow against `running_at_write.sql`. Two outputs (baseline + near-duplicate) trigger a trip at threshold 0.92; asserts `Stage=write`, `Status=running` (invariant pinned), `RetryCount=1`, `RetryReason=anti_progress`, `CostUSD=0`. Validates slog captures exactly one Warn + one Info line. Classifies `ErrAntiProgress` as `(422, "ANTI_PROGRESS", false)` via both direct and wrapped forms.
- **`docs/cli-diagnostics.md` (modified):** Added Section 6 "Anti-progress 통계 (최근 50건, NFR-R2)" with the canonical rolling-50 query. Documents the V1 proxy-FP caveat and the V1.5 gate promotion.
- **`cmd/pipeline/init_test.go` (modified):** `TestInitCmd_WritesAntiProgressThreshold` greps the generated sample config for `anti_progress_threshold: 0.92` — proves the field auto-marshals through `domain.DefaultConfig()` without init.go changes.
- **`testdata/fr-coverage.json` (modified):** Added FR8 entry with 5 test IDs (similarity goldens, detector threshold crossing, no-FP-on-dissimilar, integration flow, rolling-window stats). `meta.last_updated` → 2026-04-17. 10 FRs mapped, 5 annotated, 38 unmapped (grace mode).
- **Full sweep green:** `go test ./...` (CGO_ENABLED=0) all packages pass; `go build ./...` clean; `make lint-layers` OK (no new edges needed); `make check-fr-coverage` OK. `-race` not run due to project-wide CGO=0 (flagged in deferred-work, Story 1.1 lineage).
- **FR8 pinned by:** `TestDetector_ThresholdCrossing` (strict `>` including threshold=1.0 no-trip), `TestDetector_NoFalsePositiveOnDissimilar` (5 distinct SCP-style paragraphs, zero trips), `TestIntegration_AntiProgressFlow_RecordsRetryReason` (detector→Recorder→runs row end-to-end).
- **NFR-R2 pinned by:** `TestAntiProgressFalsePositiveStats_RollingWindow` (three window sizes, exact stats), `TestAntiProgressFalsePositiveStats_IgnoresNonAntiProgressRows` (decoys excluded), `TestAntiProgressFalsePositiveStats_UsesCreatedAtIndex` (`EXPLAIN QUERY PLAN` contains `USING INDEX`).
- **NFR-R2 V1.5 gate explicitly NOT applied here:** V1 captures the measurement via `AntiProgressFalsePositiveStats` + `docs/cli-diagnostics.md` Section 6. The ≤5% FP-rate gate is a V1.5 concern (deferred-work logged).
- **Invariant preserved:** `Recorder` never calls `SetStatus` or `NextStage`. Anti-progress records the event; Epic 3's Critic loop controller decides what happens next (HITL route, fail, resume). Integration test locks this: `Stage=write`, `Status=running` unchanged post-trip.
- **Deferred work logged** (`_bmad-output/implementation-artifacts/deferred-work.md` — "Deferred from: implementation of 2-5-anti-progress-detection"): embedding cosine (V1.5), ground-truth FP signal, retry-reason enum consolidation, cost-cap/anti-progress interaction, escalation UX, detector goroutine-safety, viper SetDefault parity, -race under CGO=0, similarity package move plan.

### File List

New files:
- `internal/pipeline/similarity.go`
- `internal/pipeline/similarity_test.go`
- `internal/pipeline/antiprogress.go`
- `internal/pipeline/antiprogress_test.go`
- `internal/pipeline/antiprogress_integration_test.go`
- `testdata/fixtures/anti_progress_seed.sql`
- `migrations/004_anti_progress_index.sql` (added during code review — composite `(retry_reason, created_at DESC)` index; bumps user_version → 4)

Modified:
- `internal/domain/errors.go` (added `ErrAntiProgress`)
- `internal/domain/errors_test.go` (coverage for new sentinel + wrapped classify)
- `internal/domain/config.go` (added `AntiProgressThreshold`, default 0.92)
- `internal/domain/config_test.go` (default assertion)
- `internal/config/loader.go` (added `SetDefault` for `cost_cap_per_run` + `anti_progress_threshold`)
- `internal/config/loader_test.go` (default + YAML override tests)
- `internal/pipeline/observability.go` (added `Recorder.RecordAntiProgress`)
- `internal/pipeline/observability_test.go` (3 new tests for RecordAntiProgress)
- `internal/db/run_store.go` (added `AntiProgressStats` + `AntiProgressFalsePositiveStats`)
- `internal/db/run_store_test.go` (5 new tests incl. `EXPLAIN QUERY PLAN` assertion)
- `cmd/pipeline/init_test.go` (sample-config threshold key regression test)
- `docs/cli-diagnostics.md` (Section 6: anti-progress stats query + `anti_progress_threshold` config reference)
- `testdata/fr-coverage.json` (FR8 coverage entry; `meta.last_updated`)
- `_bmad-output/implementation-artifacts/sprint-status.yaml` (2-5 → `review`)
- `_bmad-output/implementation-artifacts/deferred-work.md` (2-5 implementation + code-review deferred-work sections)
- `internal/db/sqlite_test.go` (user_version assertion 3 → 4 for Migration 004)

### Change Log

- 2026-04-17 — Story 2.5 implementation complete: FR8 anti-progress detector (bag-of-words cosine similarity) + NFR-R2 rolling-50 FP measurement plumbing. All 13 tasks checked; full sweep green; sprint status → `review`.
- 2026-04-18 — Code review applied 10 `patch` findings + 5 `defer` findings. Highlights: Migration 004 composite `(retry_reason, created_at DESC)` index (the strengthened EXPLAIN test proved `idx_runs_created_at` alone was insufficient — planner fell back to full scan + temp B-tree); whitespace-only guard in `Check`; FP clamp `[0,1]` on cosine; SQL secondary sort by `id DESC` for tie-break; cost-preservation integration test; `gofmt` + docstring fixes; `cfgPath` test-global restore via `t.Cleanup`; `fr-coverage.json.last_updated` → 2026-04-18. Full sweep green; sprint status → `done`.
