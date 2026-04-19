# Story 5.2: Parallel Media Generation Runner

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a developer,
I want a Phase B runner that executes image and TTS generation in parallel,
so that media generation finishes near the slower track's wall-clock time without sacrificing failure isolation or resumability.

## Prerequisites

**Story 5.1 is a hard dependency.** This story must consume the shared DashScope limiter and retry infrastructure defined in [_bmad-output/implementation-artifacts/5-1-common-rate-limiting-exponential-backoff.md](/home/jay/projects/youtube.pipeline/_bmad-output/implementation-artifacts/5-1-common-rate-limiting-exponential-backoff.md):

- reuse the shared `*CallLimiter` instance for DashScope image and TTS
- reuse `internal/llmclient.WithRetry(...)` and `pipeline.Recorder.RecordRetry(...)`
- do not add a second rate-limit or retry stack inside `internal/pipeline/`

**The current codebase already has Phase B resume semantics, but they are too coarse for this story.** `internal/pipeline/resume.go` and the architecture notes currently assume a clean-slate Phase B resume that deletes all segments for `image`/`tts`. Story 5.2 deliberately narrows that behavior for mixed track outcomes:

- if image fails and TTS succeeds, preserve successful TTS artifacts
- resume from `image` must re-run only the failed image track, not wipe the preserved TTS path(s)
- if TTS fails and image succeeds, apply the same principle symmetrically

**Do not break the existing stage model unless there is no viable alternative.** `internal/domain/types.go` already defines `image` and `tts` stages, and `internal/pipeline/engine_test.go` already locks the public transition graph. Prefer implementing parallelism inside a dedicated Phase B runner while preserving the operator-facing stage machine contract.

## Acceptance Criteria

Unless stated otherwise, new tests follow the project's `TestXxx_CaseName` convention, live beside the code under test, call `testutil.BlockExternalHTTP(t)`, and use inline fakes + `testutil.AssertEqual[T]` / `testutil.AssertJSONEq` (no testify, no gomock). Module path `github.com/sushistack/youtube.pipeline`. CGO_ENABLED=0.

**Continuity guard before implementation:** this story must introduce one canonical Phase B orchestrator in `internal/pipeline/phase_b.go`. Do **not** scatter parallel-track coordination across `service/`, provider clients, or ad-hoc goroutines in multiple files. One runner owns track fan-out, track result collation, observability recording, failure selection, and the "start assembly only after both tracks finish" rule.

1. **AC-PHASE-B-RUNNER-BOUNDARY:** add a dedicated Phase B runner in `internal/pipeline/phase_b.go` (plus tests in `internal/pipeline/phase_b_test.go`) that owns the image+TTS overlap behavior.

   Required outcome:
   - define a small injected surface for the two tracks rather than calling provider clients directly from tests
   - Phase B input comes from the authoritative `scenario.json` / segment state produced by earlier stages
   - the runner returns a typed result that preserves per-track success/failure details

   Suggested surface:

   ```go
   type ImageTrack func(ctx context.Context, req PhaseBRequest) (ImageTrackResult, error)
   type TTSTrack func(ctx context.Context, req PhaseBRequest) (TTSTrackResult, error)

   type PhaseBRunner struct {
       images   ImageTrack
       tts      TTSTrack
       recorder *Recorder
       clock    clock.Clock
       logger   *slog.Logger
   }

   func (r *PhaseBRunner) Run(ctx context.Context, req PhaseBRequest) (PhaseBResult, error)
   ```

   Rules:
   - keep orchestration in `internal/pipeline/`; provider-specific HTTP logic still belongs in `internal/llmclient/...`
   - design the result/error surface so the caller can tell which track failed without parsing strings
   - `phase_b.go` is the canonical home per architecture; do not hide this in `resume.go` or `service/run_service.go`

2. **AC-ERRGROUP-NO-AUTO-CANCEL:** use `errgroup.Group` and **not** `errgroup.WithContext` for track concurrency.

   Required behavior:
   - spawn the image and TTS tracks from the same runner call
   - one track failure must not cancel the sibling track
   - both tracks must be allowed to run to completion before the runner decides whether Phase B succeeded or failed

   Rules:
   - the parent `ctx` still flows into each track for per-call timeout and top-level cancellation
   - do not derive a child context from `errgroup.WithContext`; that would violate FR16 by canceling the sibling goroutine on the first error
   - a zero `errgroup.Group` is acceptable; `SetLimit` is optional and not a substitute for Story 5.1's provider limiter

   Tests:
   - `TestPhaseBRunner_Run_UsesErrgroupWithoutSiblingCancellation`
   - `TestPhaseBRunner_Run_WaitsForBothTracksBeforeReturning`

3. **AC-DEPENDENCY-ON-STORY-5-1-LIMITER:** both tracks must use the shared DashScope limiter and retry infrastructure from Story 5.1 instead of creating local throttling.

   Required behavior:
   - the Phase B runner receives image/TTS dependencies already wired to the shared DashScope limiter
   - retry observability continues to flow through `pipeline.Recorder.RecordRetry(...)`
   - the runner must not bypass or duplicate `internal/llmclient.WithRetry(...)`

   Rules:
   - no `time.Sleep`, no ad-hoc semaphore, no track-local token bucket inside `phase_b.go`
   - if the Phase B wiring layer constructs provider clients, pass the shared limiter by pointer/reference
   - Story 5.5 will depend on the same DashScope sharing for Korean TTS, so preserve that ownership boundary now

4. **AC-MIXED-OUTCOME-FAILURE-HANDLING:** if one track fails and the other succeeds, the successful track's artifacts and observability must survive, while the run fails with the failed track's error.

   Required behavior for image-failed / TTS-succeeded:
   - record image-track failure observability, including canonical `retry_reason` when applicable and final `cost_usd` accumulated so far
   - preserve written TTS artifacts and any persisted segment `tts_path` / `tts_duration_ms`
   - return or persist failure in a way that leaves the run at `stage=image`, `status=failed`
   - do not start assembly

   Required behavior for TTS-failed / image-succeeded:
   - preserve generated image artifacts and shot metadata
   - leave the run at `stage=tts`, `status=failed`
   - do not roll back successful image work

   Rules:
   - if both tracks fail, the runner may return an aggregated error, but it still must wait for both completions and must not run assembly
   - partial success is a first-class result, not something to erase in cleanup
   - observability rows/deltas for both tracks must be recorded even on failure

   Tests:
   - `TestPhaseBRunner_Run_ImageFailsTTSSucceeds_PreservesTTSArtifacts`
   - `TestPhaseBRunner_Run_TTSFailsImageSucceeds_PreservesImages`
   - `TestPhaseBRunner_Run_BothTrackObservabilityRecordedOnMixedFailure`

5. **AC-ASSEMBLY-GATE-AFTER-BOTH-TRACKS:** Phase C assembly must begin only after both Phase B tracks finish, and only when both succeed.

   Required behavior:
   - no early assembly trigger when the first track succeeds
   - mixed success/failure returns a failed Phase B result without invoking assembly
   - full success returns a complete `PhaseBResult` that downstream assembly can consume

   Rules:
   - keep the assembly trigger outside the individual track functions
   - if the current engine/service wiring already advances `image -> tts -> batch_review` sequentially, this story may introduce an internal "Phase B complete" orchestration step while keeping external stages stable

   Tests:
   - `TestPhaseBRunner_Run_DoesNotAssembleUntilBothTracksSucceed`
   - `TestPhaseBRunner_Run_NoAssemblyOnMixedFailure`

6. **AC-WALL-CLOCK-METRIC-CAPTURE:** capture total Phase B wall-clock time as an operational metric in the runner, using the injected clock.

   Required behavior:
   - measure elapsed wall-clock from just before track fan-out to just after both tracks join
   - persist or surface the elapsed time through the existing observability path, without inventing a second telemetry store
   - this is an operational metric for NFR-P4, not a CI gate

   Rules:
   - use `clock.Clock` so tests can be deterministic
   - avoid wall-clock assertions tied to real time
   - do not overwrite per-track durations; the wall-clock metric is an additional Phase B summary signal

   Tests:
   - `TestPhaseBRunner_Run_CapturesWallClockElapsed`

7. **AC-RESUME-ONLY-FAILED-TRACK:** resume semantics must become track-aware for mixed-result Phase B failures so the failed track re-runs without destroying preserved sibling artifacts.

   Required outcome:
   - update `internal/pipeline/resume.go` and, if needed, `internal/db/segment_store.go` with track-scoped cleanup instead of unconditional `DeleteByRunID(...)` for every Phase B failure
   - preserve successful sibling artifacts on disk and in `segments`
   - make the failed track's stage the persisted resume entry point (`image` or `tts`)

   Guidance:
   - image retry likely needs a helper that clears shot/image paths while keeping `tts_path` intact
   - TTS retry likely needs a helper that clears `tts_path` / `tts_duration_ms` while preserving generated images
   - this story should not regress the existing clean-slate behavior for full-Phase-B reruns when both tracks are invalid or when no partial success exists

   Tests:
   - `TestResume_PhaseBMixedFailure_ImageStagePreservesTTS`
   - `TestResume_PhaseBMixedFailure_TTSStagePreservesImages`

8. **AC-NO-REGRESSIONS:** `go test ./... -race && go build ./...` pass. Existing `engine`, `resume`, `observability`, and `segment_store` tests remain green after the Phase B parallelism change.

## Tasks / Subtasks

- [x] **T1: Add canonical Phase B runner in `internal/pipeline/phase_b.go`** (AC: #1, #2, #5, #6)
  - [x] Define injected track interfaces/functions and typed `PhaseBRequest` / `PhaseBResult` / per-track result structs.
  - [x] Implement `Run(...)` with one `errgroup.Group` and shared result collation.
  - [x] Ensure the runner always waits for both track goroutines before returning.

- [x] **T2: Wire shared Story 5.1 rate-limit/retry infrastructure into both tracks** (AC: #2, #3)
  - [x] Reuse the shared DashScope limiter by reference.
  - [x] Reuse `llmclient.WithRetry(...)` and recorder-based retry observability.
  - [x] Keep provider-specific HTTP details under `internal/llmclient/dashscope/`.

- [x] **T3: Implement mixed-outcome persistence and failure selection** (AC: #4, #5)
  - [x] Persist successful track artifacts even when the sibling track fails.
  - [x] Keep failure attribution stage-specific (`image` or `tts`).
  - [x] Ensure assembly is gated on two-track success only.

- [x] **T4: Make Phase B resume track-aware** (AC: #4, #7)
  - [x] Refine `resume.go` cleanup rules for `stage=image` vs `stage=tts`.
  - [x] Add any needed `SegmentStore` helpers to clear only failed-track fields.
  - [x] Preserve sibling artifacts and DB pointers that remain valid.

- [x] **T5: Add deterministic tests for overlap, observability, and resume safety** (AC: #2, #4, #5, #6, #7, #8)
  - [x] Add `internal/pipeline/phase_b_test.go`.
  - [x] Extend `resume_test.go` / integration coverage for mixed-result Phase B failures.
  - [x] Prove wall-clock capture without real sleeps.
  - [x] Run `go test ./... -race` and `go build ./...`.

## Dev Notes

### Architecture Alignment

- Architecture explicitly places the Phase B orchestrator in `internal/pipeline/phase_b.go` and calls for `errgroup.Group` without context cancel. [Source: _bmad-output/planning-artifacts/architecture.md, Phase B Parallelism / source tree]
- PRD FR16 requires image and TTS to overlap, prohibits sibling cancellation, and says assembly waits for both tracks. [Source: _bmad-output/planning-artifacts/prd.md, FR16]
- Story 5.1 is the required shared limiter/retry substrate. Do not begin this story by inventing an alternative runner-local limiter. [Source: _bmad-output/implementation-artifacts/5-1-common-rate-limiting-exponential-backoff.md]

### Existing Code to Extend, Not Replace

- `internal/pipeline/resume.go` already treats Phase B as a special cleanup boundary. Story 5.2 must refine that logic rather than bypass it.
- `internal/pipeline/observability.go` already defines the recorder as the single write path for stage observations; keep it that way.
- `internal/db/segment_store.go` already owns segment row mutation. If resume needs track-scoped field clearing, add it there instead of writing SQL from `pipeline/`.
- `internal/domain/types.go` already defines `StageImage`, `StageTTS`, `Run`, and `Episode`; preserve those contracts unless a change is unavoidable and justified.

### Critical Design Tension to Resolve Deliberately

- The architecture notes currently say Phase B resume deletes all segments and re-generates all work.
- Story 5.2 acceptance criteria now require preserving successful sibling-track artifacts on mixed failure.
- Implementation guidance: treat Story 5.2 as the narrower, newer rule for mixed outcomes. Update the old clean-slate assumption only as far as needed to preserve successful track output safely.

### File Structure Notes

- Expected implementation files:
  - `internal/pipeline/phase_b.go`
  - `internal/pipeline/phase_b_test.go`
  - `internal/pipeline/resume.go`
  - `internal/pipeline/resume_test.go`
  - `internal/db/segment_store.go`
  - `internal/db/segment_store_test.go`
- Likely supporting providers:
  - `internal/llmclient/dashscope/image.go`
  - `internal/llmclient/dashscope/tts.go`
- Artifact layout to preserve:
  - `~/.youtube-pipeline/output/{run-id}/images/...`
  - `~/.youtube-pipeline/output/{run-id}/tts/...`

### Testing Requirements

- Every new test must call `testutil.BlockExternalHTTP(t)`.
- Prefer inline fakes for image/TTS tracks so overlap and failure ordering are deterministic.
- Assert artifact preservation on disk and in `segments` rows together; preserving only one side is insufficient.
- Add race-safe tests for mixed completion ordering because Phase B now depends on true concurrency.

### Latest Technical Note

- Official Go package docs confirm that a zero `errgroup.Group` is valid and does not cancel on error, while `errgroup.WithContext` couples goroutine errors to context cancellation. That distinction is load-bearing for this story's FR16 guardrail. Source: https://pkg.go.dev/golang.org/x/sync/errgroup

### References

- Epic definition and ACs: [epics.md](/home/jay/projects/youtube.pipeline/_bmad-output/planning-artifacts/epics.md)
- Sprint prompt shorthand: [sprint-prompts.md](/home/jay/projects/youtube.pipeline/_bmad-output/planning-artifacts/sprint-prompts.md)
- Architecture source tree and Phase B behavior: [architecture.md](/home/jay/projects/youtube.pipeline/_bmad-output/planning-artifacts/architecture.md)
- PRD FR16 / NFR-P4: [prd.md](/home/jay/projects/youtube.pipeline/_bmad-output/planning-artifacts/prd.md)
- Story 5.1 dependency: [5-1-common-rate-limiting-exponential-backoff.md](/home/jay/projects/youtube.pipeline/_bmad-output/implementation-artifacts/5-1-common-rate-limiting-exponential-backoff.md)

## Dev Agent Record

### Agent Model Used

GPT-5 Codex

### Debug Log References

- Create-story workflow analysis on 2026-04-18
- `go test ./internal/pipeline -run 'TestPhaseBRunner|TestResume_PhaseBMixedFailure'`
- `go test ./internal/db -run 'TestSegmentStore_Clear(TTS|Image)Artifacts'`
- `go test ./...`
- `go test ./... -race`
- `go build ./...`

### Completion Notes List

- Story file created for explicit user-requested target `5-2-parallel-media-generation-runner`
- Dependency on Story 5.1 called out explicitly
- Added `internal/pipeline/phase_b.go` as the canonical Phase B orchestrator with typed per-track outcomes, zero-value `errgroup.Group` fan-out, scenario loading from `scenario.json`, typed track errors, and assembly gating after both tracks succeed.
- Kept Story 5.1 rate-limit/retry ownership at the injected track boundary so future DashScope-backed image/TTS executors can pass the shared limiter and `llmclient.WithRetry(...)` stack into one canonical runner without duplicating throttling in `internal/pipeline/`.
- Extended Phase B resume semantics to preserve successful sibling artifacts on mixed failures while retaining clean-slate deletion when no partial-success artifacts exist.
- Added track-scoped segment cleanup helpers in `internal/db/segment_store.go` for clearing only image shot paths or only TTS columns.
- Added deterministic tests for sibling non-cancellation, join-before-return behavior, mixed-failure preservation, assembly gating, wall-clock capture, and track-aware resume cleanup.
- Verified the full repo with `go test ./...`, `go test ./... -race`, and `go build ./...`.

## File List

- cmd/pipeline/resume.go
- internal/db/segment_store.go
- internal/db/segment_store_test.go
- internal/pipeline/engine_test.go
- internal/pipeline/phase_b.go
- internal/pipeline/phase_b_test.go
- internal/pipeline/resume.go
- internal/pipeline/resume_test.go

## Change Log

- 2026-04-18: Implemented Story 5.2 Phase B parallel runner, mixed-outcome resume preservation, and deterministic regression coverage.
- Mixed-result resume conflict versus older Phase B clean-slate architecture documented for the implementer

### File List

- _bmad-output/implementation-artifacts/5-2-parallel-media-generation-runner.md
