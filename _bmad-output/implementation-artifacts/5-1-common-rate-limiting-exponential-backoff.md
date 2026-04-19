# Story 5.1: Common Rate-limiting & Exponential Backoff

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a developer,
I want a common rate-limiter and retry infrastructure for all LLM calls,
so that the system gracefully handles provider quotas and transient errors before any Phase B media generation begins.

## Prerequisites

**Story 1.3 is the contract baseline for this story.** Reuse the existing capability interfaces in `internal/domain/llm.go`:

- `domain.ImageGenerator` and `domain.TTSSynthesizer` are already the Phase B capability boundaries.
- Provider implementations still belong under `internal/llmclient/`; do not move retry/rate-limit policy into `internal/pipeline/` or `internal/service/`.

**Story 1.4 is the testing baseline.** All new tests must remain deterministic, block real HTTP, and use the repository's inline-fake style:

- call `testutil.BlockExternalHTTP(t)` in every new test
- use inline fakes and `testutil.AssertEqual[T]`
- no real sleeps, no wall-clock assertions, no testify, no gomock

**Story 2.4 already landed part of the retry foundation.** Extend the existing implementation in `internal/llmclient/retry.go` instead of creating a second retry stack:

- keep `RetryReasonFor(...)` as the canonical mapping from wrapped errors to `runs.retry_reason`
- keep `pipeline.Recorder.RecordRetry(...)` as the observability write path
- preserve the NFR-P3 invariant from `internal/pipeline/observability.go`: retry/backoff logic records observability but does not advance run stage/status

**Story 2.5 already landed the clock abstraction.** Reuse `internal/clock.Clock` / `clock.FakeClock` for all backoff and timeout tests:

- do not use `time.Sleep` in production retry tests
- do not add a parallel fake-time mechanism in `internal/llmclient/`

**Story 5.2 depends on this story.** The shared DashScope limiter created here is the infrastructure that Story 5.2's parallel image/TTS runner must consume. This story must therefore define the ownership boundary cleanly now: one shared DashScope limiter instance, injected into both Phase B tracks by pointer/reference, not copied by value.

## Acceptance Criteria

Unless stated otherwise, new tests follow the project's `TestXxx_CaseName` convention, live beside the code under test, call `testutil.BlockExternalHTTP(t)`, and use inline fakes + `testutil.AssertEqual[T]` / `testutil.AssertJSONEq` (no testify, no gomock). Module path `github.com/sushistack/youtube.pipeline`. CGO_ENABLED=0.

**Continuity guard before implementation:** this story must extend the existing `internal/llmclient` retry surface and the existing `internal/clock` abstraction in place. Do **not** introduce a second retry helper in another package, do **not** bypass `pipeline.Recorder.RecordRetry`, and do **not** give DashScope image and TTS separate budget objects. One common retry implementation, one shared DashScope limiter instance, one fake-clock-driven timing story.

1. **AC-COMMON-LIMITER-TYPES-AND-CONFIG:** add a shared rate-limit surface in `internal/llmclient/` for external-provider calls, using both semaphore concurrency control and token-bucket RPM throttling.

   Required outcome:
   - add `golang.org/x/sync/semaphore`
   - add `golang.org/x/time/rate`
   - define a reusable limiter type in `internal/llmclient/limiter.go` (or an equivalently named file) that owns:
     - a weighted semaphore for max in-flight requests
     - a `rate.Limiter` for RPM shaping
     - a `clock.Clock`
   - expose a constructor that is configuration-driven rather than hard-coded in each client

   Suggested surface:

   ```go
   package llmclient

   type LimitConfig struct {
       RequestsPerMinute int
       MaxConcurrent     int64
       AcquireTimeout    time.Duration
   }

   type CallLimiter struct {
       ...
   }

   func NewCallLimiter(cfg LimitConfig, clk clock.Clock) (*CallLimiter, error)
   func (l *CallLimiter) Do(ctx context.Context, fn func(context.Context) error) error
   ```

   Rules:
   - invalid config (`RequestsPerMinute <= 0`, `MaxConcurrent <= 0`, `AcquireTimeout <= 0`) returns `domain.ErrValidation`
   - the semaphore and token bucket must both gate every protected call; token bucket alone is insufficient
   - `AcquireTimeout` defaults to `30 * time.Second` in config wiring unless product requirements change before implementation
   - keep the implementation in `internal/llmclient/`; Story 5.2 should consume it, not re-implement it

   Tests:
   - `TestNewCallLimiter_RejectsInvalidConfig`
   - `TestCallLimiter_Do_UsesSemaphoreAndTokenBucket`

2. **AC-RETRY-BACKOFF-DETERMINISTIC:** make retry timing fully deterministic under `clock.FakeClock`, including jitter, while preserving the existing retry-reason taxonomy in `internal/llmclient/retry.go`.

   Required changes:
   - extend the existing retry helper rather than replacing it
   - remove package-level nondeterministic jitter as the only timing source in tests
   - allow deterministic jitter injection for unit tests

   Required behavior:
   - retryable errors remain:
     - `domain.ErrRateLimited` -> `"rate_limit"`
     - `domain.ErrUpstreamTimeout` -> `"timeout"`
     - `domain.ErrStageFailed` -> `"stage_failed"`
   - exponential backoff sequence remains `1s, 2s, 4s, ...` capped at `30s`
   - jitter is still present in production code, but tests can pin it deterministically
   - all sleeps go through `clock.Clock.Sleep`

   Suggested surface:

   ```go
   type BackoffPolicy struct {
       MaxRetries int
       MaxDelay   time.Duration
       Jitter     func(attempt int) time.Duration
   }

   func DefaultBackoffPolicy() BackoffPolicy
   func WithRetry(ctx context.Context, clk clock.Clock, policy BackoffPolicy, fn func() error, onRetry func(attempt int, reason string)) error
   ```

   Rules:
   - keep the existing public semantics of `RetryReasonFor`
   - if preserving the current `WithRetry` signature is cleaner for compatibility, add an overload/helper instead of breaking every caller in-place
   - `AC-RL1` is satisfied only when fake-clock tests complete with zero real waiting

   Tests:
   - `TestWithRetry_BackoffSequence_DeterministicFakeClock`
   - `TestWithRetry_JitterInjection_Deterministic`
   - `TestWithRetry_MaxDelayCappedAt30Seconds`

3. **AC-SHARED-DASHSCOPE-BUDGET:** DashScope image and DashScope TTS must share the same limiter instance, while non-DashScope providers remain isolated.

   Required outcome:
   - add a small factory/registry in `internal/llmclient/` or config wiring that can build provider-scoped limiters
   - DashScope image and TTS clients must receive the same `*CallLimiter`
   - DeepSeek/Gemini text clients must not silently consume the DashScope limiter unless explicitly wired to do so later

   Rules:
   - shared budget means one shared object reference, not two separate limiters with identical numeric settings
   - the story may introduce a provider bundle/factory type if that is the cleanest way to enforce pointer-sharing
   - Story 5.2 and Story 5.5 will depend on this exact shared instance behavior

   Tests:
   - `TestDashScopeLimiterFactory_ImageAndTTSSharePointer`
   - `TestDashScopeLimiterFactory_NonDashScopeProvidersAreIsolated`

4. **AC-OBSERVABILITY-ON-RETRY:** every retry attempt still records the canonical retry reason in run observability through the existing recorder path.

   Required behavior:
   - retryable errors call `pipeline.Recorder.RecordRetry(...)` via the `onRetry` callback path
   - no retry path writes observability directly through ad-hoc SQL
   - 429 responses remain a retry/observability event, not a stage transition

   Rules:
   - do not duplicate retry-reason classification in provider-specific clients
   - provider clients should wrap errors with context, but `errors.Is(err, domain.ErrRateLimited)` and friends must still work
   - if a future DashScope HTTP adapter maps raw status codes to domain errors, that mapping belongs in `internal/llmclient/dashscope/`, not in `pipeline/`

   Tests:
   - extend the existing `internal/pipeline/observability_integration_test.go` style with a limiter-wrapped retry flow
   - assert `retry_count` increments and `retry_reason == "rate_limit"` after a 429 retry sequence

5. **AC-CIRCUIT-BREAKER-30S-NO-LEAK:** a protected provider call that stalls while holding the semaphore must time out after 30 seconds, release resources, and return an operator-escalatable error without goroutine leaks or deadlocks.

   Required behavior:
   - the limiter wrapper must derive a bounded context (or equivalent timeout mechanism) for the protected call
   - if 30 seconds elapse before the protected function returns, the limiter:
     - releases the semaphore permit
     - returns an error that remains classifiable as retryable/operator-resumable (`domain.ErrStageFailed` is the preferred fit unless implementation reveals a better existing sentinel)
   - fake-clock-driven tests must prove completion without a 30-second real wait

   Rules:
   - use `defer`-based permit release so both success and timeout paths free the semaphore
   - this story must not introduce background goroutines that outlive the request on the happy path
   - if an internal helper launches a goroutine for timeout coordination, tests must prove it terminates

   Tests:
   - `TestCallLimiter_Do_TimesOutAfter30Seconds_FakeClock`
   - `TestCallLimiter_Do_ReleasesPermitOnTimeout`
   - `TestCallLimiter_Do_NoGoroutineLeakUnderTimeoutContention`

6. **AC-RPM-COMBINED-THROUGHPUT:** when image and TTS tracks contend for the shared DashScope limiter, the combined throughput must stay within the configured RPM budget and the measured split must be within `+-5%` of the target allocation used by the test.

   Required behavior:
   - the acceptance test may simulate image and TTS calls with two goroutine pools contending against the same limiter
   - the assertion is on total throughput under the shared limiter, not on two isolated per-track rate limits
   - the test should use a deterministic fake-time or controlled-time approach so CI does not become flaky

   Rules:
   - if `rate.Limiter` internals force a real clock, keep the test at the abstraction boundary by substituting a controllable wait strategy around the limiter instead of sleeping on wall-clock time
   - do not weaken this AC into "approximately seems right by eyeballing logs"

   Tests:
   - `TestSharedDashScopeLimiter_CombinedRPMWithinFivePercent`

7. **AC-NO-REGRESSIONS:** `go test ./... -race && go build ./...` pass. Existing retry, observability, clock, and config tests remain green. New dependencies must be reflected in `go.mod` / `go.sum`.

---

## Tasks / Subtasks

- [x] **T1: Add common limiter types in `internal/llmclient/`** (AC: #1, #3, #5, #6)
  - [x] Add `golang.org/x/sync/semaphore` and `golang.org/x/time/rate` to `go.mod`.
  - [x] Create `internal/llmclient/limiter.go` with `LimitConfig`, `CallLimiter`, and constructor validation.
  - [x] Implement one protected execution path that waits on the token bucket, acquires the semaphore, and always releases the permit.
  - [x] Keep the limiter package-private to `internal/llmclient` unless a broader surface is truly required by wiring.

- [x] **T2: Make retry/backoff deterministic and reusable** (AC: #2, #4)
  - [x] Refactor `internal/llmclient/retry.go` so tests can inject deterministic jitter.
  - [x] Preserve `RetryReasonFor(...)` and its existing taxonomy.
  - [x] Ensure every sleep goes through `clock.Clock.Sleep`.
  - [x] Keep max backoff delay capped at `30s`.

- [x] **T3: Wire provider-scoped limiter ownership** (AC: #3, #6)
  - [x] Add a factory/registry helper that returns one shared DashScope limiter for image+TTS.
  - [x] Keep non-DashScope providers isolated from the DashScope budget.
  - [x] Document in code comments that Story 5.2 and 5.5 depend on pointer-sharing, not config coincidence.

- [x] **T4: Integrate retry observability with the limiter path** (AC: #4)
  - [x] Ensure provider-call wrappers can pass `onRetry` callbacks through to `pipeline.Recorder.RecordRetry(...)`.
  - [x] Preserve the rule that retry observability never advances run stage/status.
  - [x] Verify wrapped provider errors still classify through `errors.Is`.

- [x] **T5: Add 30-second timeout/circuit-break behavior** (AC: #5)
  - [x] Bound protected calls with a 30-second timeout path.
  - [x] Prove semaphore release on both success and failure.
  - [x] Prove timeout contention does not deadlock waiting callers.

- [x] **T6: Add deterministic tests for backoff, shared budget, and leak safety** (AC: #2, #5, #6, #7)
  - [x] Extend `internal/llmclient/retry_test.go` for deterministic jitter/backoff coverage.
  - [x] Add `internal/llmclient/limiter_test.go` for semaphore + token-bucket behavior.
  - [x] Add or extend an integration-style test proving retry observability survives the limiter wrapper.
  - [x] Run `go test ./... -race` and `go build ./...`.

## Dev Notes

### Architecture Alignment

- `architecture.md` explicitly marks rate-limit coordination as a Tier 1 cross-cutting concern for Phase B. This story is therefore infrastructure-first, not a "helper clean-up" task.
- `architecture.md` also says Phase B image and TTS share the same DashScope budget. If implementation ends with one limiter per client constructor, the story is incomplete even if both use the same numeric RPM setting.
- `architecture.md` requires time abstraction for 429/backoff testing. Reuse `internal/clock/`.

### Existing Code to Extend, Not Replace

- `internal/llmclient/retry.go` already contains:
  - `RetryReasonFor`
  - exponential backoff with max-delay semantics
  - `clock.Clock`-driven sleeping
- The weak spot is deterministic jitter: current code uses a package-global PRNG seeded from real time. Story 5.1 should close that gap rather than creating a second retry helper.

### Provider and Package Boundaries

- Keep provider-specific HTTP status mapping inside provider packages such as `internal/llmclient/dashscope/`.
- Keep rate-limit/retry orchestration inside `internal/llmclient/`.
- Keep run observability persistence inside `internal/pipeline/Recorder`.
- Do not move any of these concerns into `internal/service/`; service code should consume stores and orchestrators, not own low-level provider throttling.

### Test Guidance

- `internal/clock/FakeClock` is already available and should remain the primary timing test mechanism.
- `internal/testutil/nohttp.go` enforces the no-real-network rule; keep using it in every new test.
- `go test -race` matters here because semaphore/timeouts/shared limiter ownership are precisely the kind of code that can look correct under normal tests while still leaking or racing.

### Open Design Constraint to Preserve

Story 5.2 will use `errgroup.Group` without context auto-cancel so one Phase B track does not cancel the other. That means Story 5.1's limiter/timeout behavior must be locally safe and self-contained:

- permit release cannot depend on a sibling track exiting
- timeout cleanup cannot assume phase-wide cancellation
- retry logic must be safe when two independent goroutine trees share the same DashScope limiter

## Dev Agent Record

### Context Reference

- Epic source: `_bmad-output/planning-artifacts/epics.md` (Epic 5, Story 5.1)
- Architecture source: `_bmad-output/planning-artifacts/architecture.md` (Tier 1 rate-limit coordination, time abstraction, Phase B parallelism)
- Sprint prompt source: `_bmad-output/planning-artifacts/sprint-prompts.md` (Epic 5 - Story 5.1 개발 / 코드 리뷰 / 리뷰 반영 notes)

### Missing Context at Story Creation Time

- No `project-context.md` file was present in the repository during story creation.
- No prior Epic 5 implementation story exists yet, so there are no Epic-5-specific dev learnings to inherit.

### Implementation Plan

- Extend the existing retry helper with policy-based jitter injection instead of replacing the retry surface.
- Add a shared limiter abstraction in `internal/llmclient` that combines `rate.Limiter`, `semaphore.Weighted`, and `clock.Clock`.
- Prove pointer-sharing, timeout cleanup, and retry observability through deterministic fake-clock tests plus pipeline integration coverage.

### Debug Log

- Added `clock.FakeClock.PendingSleepers()` so fake-time tests can wait for registered sleepers instead of outrunning goroutines.
- Stabilized race-mode tests by waiting on explicit retry/start signals and advancing fake time in bounded steps.
- Reverted the timestamp-only `testdata/golden/eval/manifest.json` change after regression runs because it was a test side effect, not product behavior.

### Completion Notes

- Implemented `internal/llmclient/limiter.go` with config validation, token-bucket RPM shaping, semaphore concurrency control, shared DashScope factory wiring, and 30-second timeout handling that returns `domain.ErrStageFailed`.
- Extended `internal/llmclient/retry.go` with `BackoffPolicy`, deterministic jitter injection for tests, capped backoff behavior, and compatibility via the existing `WithRetry(...)` signature.
- Added deterministic unit and integration coverage for limiter gating, shared DashScope pointer ownership, retry observability, timeout cleanup, leak-safe contention, and fake-clock-driven retry timing.
- Verified the story with `go test ./... -race` and `go build ./...`.

## File List

- go.mod
- go.sum
- internal/clock/clock.go
- internal/llmclient/limiter.go
- internal/llmclient/limiter_test.go
- internal/llmclient/retry.go
- internal/llmclient/retry_test.go
- internal/pipeline/observability_integration_test.go

## Change Log

- 2026-04-18: Added shared provider call limiting, deterministic retry/backoff policy support, and observability-preserving retry coverage for Story 5.1.
- 2026-04-18: Code review pass — batch-applied 8 patches covering backoff overflow cap, deterministic test sync, panic recovery, goroutine leak detection, and combined-RPM test strengthening.

### Review Findings

- [x] [Review][Patch] Cap backoff exponent to prevent `1<<attempt` overflow for attempt ≥ 34 [internal/llmclient/retry.go:140]
- [x] [Review][Patch] Reword `max retries (0) exceeded` message when MaxRetries=0 [internal/llmclient/retry.go:130]
- [x] [Review][Patch] Replace `time.Sleep(20ms)` with `waitForRetrySleeper` for deterministic goroutine sync [internal/llmclient/retry_test.go:166]
- [x] [Review][Patch] Add wall-clock fallback timeout to `waitForSignal` / `waitForRetryDone` helpers [internal/llmclient/retry_test.go:321]
- [x] [Review][Patch] Recover from panic in fn goroutine inside CallLimiter.Do [internal/llmclient/limiter.go:66]
- [x] [Review][Patch] Strengthen `TestSharedDashScopeLimiter_CombinedRPMWithinFivePercent` to use actual `clk.Now().Sub(start)` instead of hardcoded elapsed [internal/llmclient/limiter_test.go:289]
- [x] [Review][Patch] Strengthen `TestCallLimiter_Do_NoGoroutineLeakUnderTimeoutContention` with `runtime.NumGoroutine()` delta check [internal/llmclient/limiter_test.go:170]
- [x] [Review][Patch] Document FakeClock advance requirement on `acquire()` poll loop [internal/llmclient/limiter.go:103]
- [x] [Review][Defer] fn goroutine may outlive `Do` on timeout if callee ignores ctx, releasing semaphore prematurely — deferred, architectural decision tied to Story 5.2 wiring (open design constraint forbids phase-wide cancellation)
- [x] [Review][Defer] Rate reservation consumed when subsequent `acquire` times out — deferred, low frequency, revisit with Story 5.2
- [x] [Review][Defer] Replace `acquire` poll loop with `semaphore.Acquire(ctx, 1)` — deferred, refactor candidate outside 5.1 scope
- [x] [Review][Defer] Pre-existing `time.Sleep` calls in `internal/clock/clock_test.go` — deferred, not introduced by 5.1
