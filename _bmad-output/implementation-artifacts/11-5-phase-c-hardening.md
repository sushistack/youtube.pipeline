# Story 11.5: Phase C Hardening

Status: review

## Story

As a developer,
I want to harden the Phase C output and validation path around metadata publication, short-shot transitions, and duration probing,
so that Phase C no longer carries the P0 data-integrity risks blocking the next smoke-test backfill.

## Acceptance Criteria

1. **Phase C hardening landed**: the Phase C metadata publish path guarantees pair-level atomicity for `metadata.json` and `manifest.json`, using one explicit publish strategy called out in Step 3 / deferred work:
   - staging-directory rename, or
   - completed-marker file strategy
   The system must never leave a success-path state where only one of the two files is published.
2. **Verification criterion carried forward exactly**: `SMOKE-05` is green (**both-or-neither invariant**). This is the exact blocker-release verification from Step 3 `test-design-epic-1-10-2026-04-25.md §12` for R-04 / Phase C hardening.
3. The `cross_dissolve` path in `buildMultiShotClip` is hardened for short shots:
   - when the effective pre-transition duration is `< 0.5s`, Phase C does not emit a negative `xfade` offset
   - the implementation degrades safely instead of relying on FFmpeg undefined behavior
   - regression coverage includes at least one unit/integration path for the `< 0.5s` case, matching the Step 3 mapping for R-09
4. The duration-validation path is hardened so `probeDuration <= 0` cannot silently pass concat validation:
   - non-positive probe results are treated as an explicit error / invalid artifact condition
   - `concatClips` must not accept a partial-failure clip or output whose probed duration is `0`
   - regression coverage includes the unit-level guard called out in Step 3 for R-10
5. Existing successful Phase C behavior remains intact apart from the hardening:
   - normal metadata generation still succeeds on retry / overwrite paths
   - existing real-FFmpeg Phase C tests stay green
   - scope does not expand into adjacent deferred items such as single-quote concat escaping, >20 clip tolerance tuning, `AcknowledgeMetadata` hardening, or symlink defenses

## Requested Planning Output

### 1. Acceptance Criteria for blocker release

- `Phase C hardening` landed: metadata+manifest pair publish is atomic; `xfade < 0.5s` no longer emits a negative offset; `probeDuration = 0` is rejected instead of silently passing validation
- `Verification`: `SMOKE-05` green (**both-or-neither invariant**)

### 2. Affected Files

Likely affected files for this story:

- `internal/pipeline/phase_c.go`
- `internal/pipeline/phase_c_metadata.go`
- `internal/pipeline/phase_c_test.go`
- `internal/pipeline/phase_c_metadata_test.go`

Possible new support files if the hardening is split cleanly:

- `internal/pipeline/smoke05_metadata_atomic_test.go`
- `internal/pipeline/fi/faulting_writer.go`

### 3. E2E scenarios unblocked when this story is done

- `SMOKE-05` — Metadata+Manifest Atomic Pair

### 4. Estimated effort

- Jay solo estimate: **6-10 hours** implementation + local verification
- Add **1-2 hours elapsed** if Step 6 is run immediately to prove `SMOKE-05` green on top of the unit/integration hardening checks

## Tasks / Subtasks

- [x] Task 1: Make metadata+manifest publication pair-atomic (AC: 1, 2, 5)
  - [x] Replace the current sequential two-file publish behavior with one explicit pair-atomic strategy: staging-dir rename or completed-marker publish.
  - [x] Preserve retriable failure semantics when the second write / publish step faults.
  - [x] Ensure retry after a failed publish cannot leave timestamp-mismatched or half-published metadata artifacts.

- [x] Task 2: Add fault-injection coverage for the atomicity contract (AC: 1, 2)
  - [x] Implement the Step 3 `SMOKE-05` support surface (`faulting_writer` or equivalent FI adapter).
  - [x] Prove the post-fix invariant: either both files exist and are internally consistent, or neither exists.
  - [x] Prove retry with a non-faulting writer publishes the same final content as a control run.

- [x] Task 3: Harden the short-shot `xfade` path (AC: 3, 5)
  - [x] Guard `cross_dissolve` assembly so the offset cannot go negative when the composed stream duration is below the dissolve window.
  - [x] Keep the generated clip valid for real FFmpeg execution in the `< 0.5s` boundary case.
  - [x] Add a regression test that exercises the short-shot path with real or near-real Phase C assembly behavior.

- [x] Task 4: Reject `probeDuration = 0` as a valid media probe result (AC: 4, 5)
  - [x] Tighten `probeDuration` / `concatClips` so non-positive durations fail fast with a clear error.
  - [x] Add regression coverage that proves zero-duration probe results can no longer fake-pass validation.
  - [x] Keep the existing positive-duration concat validation path green.

- [x] Task 5: Re-run the existing Phase C regression surface (AC: 5)
  - [x] Keep all existing `phase_c*_test.go` coverage green after the hardening.
  - [x] Run the targeted real-FFmpeg tests plus the new atomicity smoke/integration test before handing off to Step 6.

## Dev Agent Record

### Implementation Plan

- **Task 1 (R-04 / AC 1, 2, 5)** — Replace `metadataBuilder.Write` with a
  staging-dir publish protocol:
  1. Marshal both payloads in memory; abort early on marshal error so the
     filesystem stays untouched on bad inputs.
  2. Stage `metadata.json` and `manifest.json` under
     `runDir/.metadata.staging/` via the configured `FileWriter`
     (atomic temp+rename per file). Defensive `RemoveAll` before the staging
     `MkdirAll` cleans up any partial state from a prior crashed attempt.
  3. Publish the staged files into `runDir` as a pair: snapshot any existing
     `metadata.json` to a `.rollback` sibling, rename staged metadata in,
     then rename staged manifest in. If the second rename fails, restore the
     metadata backup (or remove the just-published file) so the
     both-or-neither invariant holds even on partial publish.
  4. The injected `FileWriter` is the seam tests use to drive faults — the
     production seam stays `DefaultAtomicWriter`.
- **Task 2 (R-04 verification)** — Add `internal/pipeline/fi/faulting_writer.go`
  exposing `NewFaultingFileWriter(delegate, decide)`. The fault hook receives
  the destination path and a per-path 1-based attempt counter so SMOKE-05 can
  fault `manifest.json` while letting `metadata.json` succeed. The new
  `internal/pipeline/smoke05_metadata_atomic_test.go` codifies three
  scenarios: fault-then-retry, byte-equality vs control run, and
  rollback-on-second-rename-fault when a prior publish exists.
- **Task 3 (R-09 / AC 3, 5)** — Extract the xfade offset clamp into a pure
  helper `computeCrossDissolveOffset(streamDur, dissolveSec)` and a named
  constant `crossDissolveDurationSec = 0.5`. Direct concat fallback was
  rejected after a manual FFmpeg trial showed concat→xfade chains hit a
  timebase mismatch (`First input link main timebase (1/1000000) do not
  match … (1/25)`); the existing offset-clamp-to-zero behavior produces a
  valid MP4 in the boundary case (verified by direct ffmpeg run, 3.000 s
  output). White-box unit coverage in `phase_c_internal_test.go` plus a real
  FFmpeg integration test (`TestPhaseCRunner_3ShotCrossDissolve_ShortFirstShot`)
  with a 0.3 s leading shot.
- **Task 4 (R-10 / AC 4, 5)** — Add `validateProbedDuration(path, dur)`
  helper that returns an error wrapping `domain.ErrValidation` for any
  non-positive probe result, and call it from `probeDuration` so the guard
  applies uniformly to TTS probes and concat-validation probes. Unit-tested
  via the same internal test file (zero, negative, positive cases).
- **Task 5** — Full `go test ./...` run after every step; pipeline package
  finishes in ~20 s (real FFmpeg integration tests dominate).

### Completion Notes

- Pair-atomic publish: `metadata.json` and `manifest.json` are now published
  via a staging-dir rename protocol with explicit rollback on partial
  publish. Defensive cleanup of `runDir/.metadata.staging/` happens both
  before staging (handles a prior crashed attempt) and via `defer` after
  publish (handles success and failure paths uniformly).
- SMOKE-05 verification (both-or-neither invariant) green:
  - `TestSMOKE05_MetadataAtomicPair_FaultThenRetry` — manifest fault leaves
    neither file present; subsequent retry with the production writer
    publishes both.
  - `TestSMOKE05_MetadataAtomicPair_RetryMatchesControl` — retried publish
    is byte-identical to a fault-free control run with the same inputs.
  - `TestSMOKE05_MetadataAtomicPair_RollbackOnRenameFault` — when a prior
    publish already exists in `runDir`, a faulted retry restores the prior
    bundle instead of leaving a half-published new bundle behind.
- xfade R-09 hardening: the `< 0.5 s` boundary now goes through the named
  `computeCrossDissolveOffset` helper which clamps to zero. The existing
  filter graph (xfade with offset=0, duration=0.500) produces a valid MP4
  on real FFmpeg; degraded boundaries are surfaced as a structured warning
  log so operators can spot pathological inputs.
- probeDuration R-10 hardening: `validateProbedDuration` rejects zero or
  negative durations with `domain.ErrValidation` so the guard runs uniformly
  on TTS probes (Phase C clip assembly) and on concat input/output probes
  (final assembly validation).
- Out-of-scope deferreds NOT touched (per story Dev Notes): single-quote
  escaping in concat list, accumulated duration tolerance for >20 clips,
  `AcknowledgeMetadata` `MaxBytesReader`, symlink hardening / `EvalSymlinks`.

### File List

**Modified**
- `internal/pipeline/phase_c.go` — Extracted `crossDissolveDurationSec`
  constant + `computeCrossDissolveOffset` helper; named, log-warned the
  short-shot boundary; added `validateProbedDuration` helper and wired it
  into `probeDuration`.
- `internal/pipeline/phase_c_metadata.go` — Added `FileWriter` type +
  `DefaultAtomicWriter`; extended `MetadataBuilderConfig.Writer`; replaced
  `Write` with staging-dir pair-atomic publish + rollback; added internal
  `fileExists` helper.
- `internal/pipeline/phase_c_test.go` — Added
  `TestPhaseCRunner_3ShotCrossDissolve_ShortFirstShot` real-FFmpeg
  integration test for R-09.

**New**
- `internal/pipeline/fi/faulting_writer.go` — Path-aware fault-injecting
  `FileWriter` adapter (Step 3 `SMOKE-05` support surface).
- `internal/pipeline/smoke05_metadata_atomic_test.go` — SMOKE-05
  fault-injection coverage (3 scenarios codifying the both-or-neither
  invariant + retry idempotency + rollback).
- `internal/pipeline/phase_c_internal_test.go` — White-box unit coverage
  for `computeCrossDissolveOffset` (R-09) and `validateProbedDuration`
  (R-10).

### Change Log

| Date       | Change                                                          |
|------------|-----------------------------------------------------------------|
| 2026-04-25 | Pair-atomic metadata+manifest publish via staging-dir rename + rollback (R-04, SMOKE-05). |
| 2026-04-25 | Added `internal/pipeline/fi` fault-injection package and SMOKE-05 test (3 scenarios). |
| 2026-04-25 | Extracted `computeCrossDissolveOffset` clamp + warning log; added short-shot regression test (R-09). |
| 2026-04-25 | Added `validateProbedDuration` guard so non-positive ffprobe results fail fast (R-10). |
| 2026-04-25 | Story 11-5 status: ready-for-dev → in-progress → review.        |

## Dev Notes

- This story clears the three linked P0 risks grouped in the post-epic quality prompts as Story 11-5:
  - `R-04` metadata+manifest non-atomic pair
  - `R-09` negative `xfade` offset on shot `< 0.5s`
  - `R-10` `probeDuration = 0` silently passing tolerance
- Step 3 dependency table calls this dependency out explicitly: `Phase C hardening: metadata+manifest atomicity + xfade guard + probe=0 guard`, unlocking `SMOKE-05` by **2026-05-13**.
- Exact blocker-release verification available in the artifacts is only for the atomicity portion, and it must be preserved verbatim: `SMOKE-05 green (both-or-neither invariant)`.
- Current codebase reality to preserve while hardening:
  - `phase_c_metadata.go` already does per-file atomic write, but not pair-level atomic publish
  - `phase_c.go` already comments that `xfade` offset should be clamped to `>= 0`; keep the contract explicit and regression-tested at the short-shot boundary
  - `concatClips` validates duration mismatch, but a `0` probe result can still fake-pass if not rejected as invalid media
- Do not absorb adjacent Epic 9 deferreds into this story:
  - single-quote escaping in concat lists
  - accumulated duration tolerance for large videos
  - `AcknowledgeMetadata` `MaxBytesReader`
  - symlink hardening / `EvalSymlinks`

## References

- `_bmad-output/test-artifacts/test-design-epic-1-10-2026-04-25.md` §10, §11, §12
- `_bmad-output/test-artifacts/quality-strategy-2026-04-25.md` §3 P0
- `_bmad-output/implementation-artifacts/deferred-work.md` (`9-2` W1, W3, W4)
- `_bmad-output/implementation-artifacts/epic-1-10-retro-2026-04-24.md`
- `_bmad-output/planning-artifacts/post-epic-quality-prompts.md`
- `internal/pipeline/phase_c.go`
- `internal/pipeline/phase_c_metadata.go`
- `internal/pipeline/phase_c_test.go`
- `internal/pipeline/phase_c_metadata_test.go`
