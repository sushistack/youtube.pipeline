# Story 10.3: Database Vacuum & Data Retention (Soft Archive)

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want old pipeline artifacts to be soft-archived and the SQLite database compacted during idle cleanup,
so that long-running local usage does not exhaust disk space while preserving all historical run and segment records.

## Prerequisites

**Hard reuse requirements from earlier stories:**

- **Story 1.2 is the migration/config foundation.** Reuse the existing embedded SQLite migration flow and `internal/domain.PipelineConfig`; do not introduce a second schema or out-of-band retention store.
- **Story 2.3 is the artifact-lifecycle source of truth.** Reuse the existing artifact cleanup patterns, run-directory boundary assumptions, and path-safety posture from `internal/pipeline/resume.go`, `internal/pipeline/consistency.go`, and `internal/db/segment_store.go`.
- **Story 9.1 introduced run-level output artifacts.** `runs.output_path` already exists in SQLite and must be included in the archive/null-ref story even though current read models do not surface it everywhere yet.
- **Story 10.1 explicitly deferred retention/VACUUM UI.** This story may add config keys and CLI behavior, but it must not expand into Settings-dashboard UI work unless a tiny follow-on read surface is absolutely required for validation.

**Canonical retention rules that must not regress:**

- **NFR-O2 remains binding.** `runs`, `segments`, and `decisions` rows are retained indefinitely. This story is file cleanup only, never database-row purge.
- Soft Archive means: delete artifact files under the run output tree, keep the DB records, and replace DB file references with `NULL` (or empty embedded `image_path` strings inside `shots` JSON where that is the established storage shape).
- Archived runs remain visible to `pipeline status`, metrics, history, and future exports. Only artifact file access is lost.

**Current codebase reality to account for:**

- `cmd/pipeline/main.go` currently registers `init`, `doctor`, `create`, `cancel`, `resume`, `status`, `metrics`, `serve`, and `golden`; there is no `clean` command yet.
- `internal/domain/config.go` currently has no retention/VACUUM settings.
- `runs` stores `scenario_path` and `output_path`; `segments` stores `tts_path`, `clip_path`, and image file references nested inside `shots` JSON.
- The codebase already has narrowly-scoped artifact clearing helpers (`ClearImageArtifactsByRunID`, `ClearTTSArtifactsByRunID`, `ClearClipPathsByRunID`) that should be extended/reused instead of inventing parallel SQL mutation paths.

## Acceptance Criteria

### AC-1: `pipeline clean` exists as an operator-facing CLI command with config-backed retention

**Given** the operator has configured an artifact retention period
**When** they run `pipeline clean`
**Then** the command loads the standard config, opens the SQLite DB, evaluates archive candidates, performs Soft Archive for eligible runs, and renders a structured success summary in human and JSON modes.

**Required command surface:**

- Add `pipeline clean`
- Register it in `cmd/pipeline/main.go`
- Reuse existing renderer conventions instead of custom stdout formatting

**Minimum config additions:**

- `artifact_retention_days` as a positive integer in `PipelineConfig`

**Rules:**

- A retention value `< 1` is rejected as `domain.ErrValidation`.
- The command is explicit/manual in V1. No background daemon, cron scheduler, or always-on cleaner is added in this story.
- The summary reports at least: retention cutoff, runs scanned, runs archived, files deleted, DB refs cleared, and whether `VACUUM` ran or was skipped.

**Tests:**

- CLI test verifies `pipeline clean --json` returns a versioned success envelope.
- Config test verifies `artifact_retention_days` default exists and rejects invalid values.

### AC-2: Only terminal runs older than the retention cutoff are eligible for Soft Archive

**Given** the retention cutoff has been calculated
**When** archive candidates are selected
**Then** only runs whose `status` is terminal and whose timestamp is older than the cutoff are eligible.

**Eligibility rules:**

- Terminal statuses for this story: `completed`, `failed`, `cancelled`
- Ineligible statuses: `pending`, `running`, `waiting`
- Use a stable run timestamp for cutoff comparison and document it in code and tests; prefer `updated_at` so paused or recently-mutated runs are not archived prematurely

**Rules:**

- Candidate selection is deterministic and ordered oldest-first, then run ID for tie-break stability.
- A run with already-null artifact references is treated as already archived and does not fail the command.
- Active/non-terminal runs are never mutated by `pipeline clean`.

**Tests:**

- Unit test verifies eligibility by status and cutoff timestamp.
- Integration test verifies active runs are skipped even if old.

### AC-3: Soft Archive deletes artifact files but preserves all `runs` and `segments` rows

**Given** a run is eligible for cleanup
**When** Soft Archive executes
**Then** artifact files under that run's output tree are removed
**And** no rows are deleted from `runs`, `segments`, or `decisions`
**And** DB references are cleared so the database no longer points at deleted files.

**Minimum DB mutation surface per archived run:**

- `runs.scenario_path -> NULL`
- `runs.output_path -> NULL`
- `segments.tts_path -> NULL`
- `segments.clip_path -> NULL`
- embedded `segments.shots[].image_path -> ""`

**Rules:**

- Preserve all non-file metadata: stage, status, retry counts, critic scores, narration, review decisions, safeguard flags, timestamps, frozen descriptors, and decisions history.
- Cleanup scope is limited to the target run's artifact tree under the configured output directory; do not delete outside that boundary.
- Missing files are tolerated as an idempotent archive state; the command still clears stale DB refs and continues.
- Re-running `pipeline clean` after a run is archived is a no-op for that run, not an error.

**Tests:**

- Integration test proves row counts in `runs`, `segments`, and `decisions` are unchanged after cleanup.
- Integration test proves the target file paths are removed and DB refs are null/cleared.
- Integration test proves another run's artifacts and DB refs remain untouched.

### AC-4: Archive implementation reuses existing artifact-clearing helpers instead of bespoke destructive SQL

**Given** the codebase already has stage-specific artifact cleanup helpers
**When** Story 10.3 is implemented
**Then** the cleaner reuses or extends those helpers so path-clearing logic stays consistent with resume semantics.

**Required implementation shape:**

- Prefer a dedicated cleanup service or package-level orchestrator over embedding all logic in the Cobra command
- Reuse `SegmentStore.ClearImageArtifactsByRunID`, `ClearTTSArtifactsByRunID`, and `ClearClipPathsByRunID`
- Add the minimal run-store support needed to clear `scenario_path` and `output_path`

**Rules:**

- Do not introduce a second path-encoding format for image refs.
- Do not `DELETE FROM segments` or rebuild archived rows from scratch.
- If a helper already enforces run scoping, keep that behavior and extend it rather than replacing it.

**Tests:**

- Store/service tests verify run-scope isolation for every DB mutation helper added by this story.

### AC-5: `VACUUM` executes only after cleanup and only when the system is idle

**Given** `pipeline clean` has finished all archive mutations
**When** no active runs are present
**Then** the command executes SQLite `VACUUM` to reclaim space and reports that it ran.

**Idle-time rule for V1:**

- Treat the system as idle when there are no runs in `pending`, `running`, or `waiting`

**Rules:**

- `VACUUM` runs after archive transactions commit; never inside an open transaction.
- If active runs exist, cleanup may still archive eligible old runs, but `VACUUM` is skipped with explicit summary output.
- `VACUUM` failure is surfaced clearly; do not silently swallow it.
- A no-op cleanup may still run `VACUUM` when the system is idle.

**Tests:**

- Service/integration test verifies `VACUUM` is attempted only when no active runs exist.
- Integration test verifies post-clean `PRAGMA freelist_count` returns to `0` after a successful vacuum on a seeded DB that produced free pages.

### AC-6: Archived runs degrade gracefully across CLI/API surfaces

**Given** an archived run remains in the database
**When** an operator later inspects history or run state
**Then** the run still appears in list/detail surfaces without causing crashes
**And** artifact-serving endpoints naturally fail with not-found semantics because the file refs are absent or null.

**Rules:**

- This story does not require a new explicit `archived` run status.
- Existing status/history flows must tolerate null artifact refs.
- If a read model or contract currently assumes a non-null artifact path, update it narrowly enough to support archived runs without broad UI redesign.

**Tests:**

- Regression test verifies archived rows still render through existing run listing/status code paths.
- API/handler test verifies artifact fetches for archived runs fail safely rather than panic.

## Tasks / Subtasks

- [x] **T1: Add retention config + validation plumbing** (AC: 1, 2)
  - Extend `internal/domain/config.go` with `artifact_retention_days` and sensible default.
  - Add validation coverage in config tests and any loader/doctor paths that already enforce numeric config constraints.

- [x] **T2: Add `pipeline clean` command and output contract** (AC: 1, 5)
  - Create `cmd/pipeline/clean.go` and register it in `cmd/pipeline/main.go`.
  - Reuse the existing renderer/envelope conventions for summary output.

- [x] **T3: Implement archive candidate selection + idle detection** (AC: 2, 5)
  - Add a small service/orchestrator that queries eligible runs by retention cutoff and terminal status.
  - Add active-run detection for the idle/VACUUM gate.

- [x] **T4: Extend stores for Soft Archive DB mutations** (AC: 3, 4)
  - Add minimal run-store helpers to clear `scenario_path` and `output_path`.
  - Reuse existing segment-store clear helpers for image/TTS/clip refs.
  - Keep mutation scope strictly per-run.

- [x] **T5: Implement filesystem cleanup with run-directory boundary checks** (AC: 3, 4)
  - Delete only known artifact files/subtrees within `{cfg.OutputDir}/{runID}/`.
  - Make missing-file handling idempotent and non-fatal.

- [x] **T6: Execute post-clean `VACUUM` safely** (AC: 5)
  - Run `VACUUM` only after cleanup commits and only when idle.
  - Surface ran/skipped/failed state in the clean summary.

- [x] **T7: Add regression coverage for archived-run behavior** (AC: 6)
  - Ensure existing run listing/status and artifact-serving paths tolerate null refs after archival.

## Dev Notes

- A small `internal/service/clean_service.go` or `internal/pipeline/cleanup.go` style orchestrator is preferable to putting DB queries, file deletes, and `VACUUM` directly inside Cobra command code.
- Story 2.3 already solved many of the scary parts: run-scoped filesystem deletion, consistency expectations, and targeted DB ref cleanup. Follow that shape so archive behavior does not drift from resume behavior.
- Be careful with `runs.output_path`: the DB column exists, but current domain/read-model plumbing is not fully aligned. If tests expose that drift, fix it in the smallest coherent way needed for archive correctness.
- For image refs, the established storage contract is `shots` JSON with `image_path` strings, not a separate normalized table. Clearing those strings in-place is the correct V1 behavior.
- `VACUUM` is connection-sensitive and cannot run within a transaction. Keep the cleanup transaction boundaries obvious in code comments.

## Validation

- `go test ./cmd/pipeline ./internal/service ./internal/db ./internal/pipeline ./internal/api`
- Add a focused CLI test for `pipeline clean`
- Add DB integration coverage for row preservation + null-ref behavior
- Add a vacuum/idle gating test using a real temp SQLite file

## Open Questions / Assumptions

- Assumption: `artifact_retention_days` is sufficient for V1; retention is configured in `config.yaml`, not through the Settings UI in this story.
- Assumption: archive eligibility uses `runs.updated_at` rather than `created_at` so old-but-recently-touched runs are not archived prematurely.
- Assumption: `scenario.json` and final `output.mp4` are considered archiveable artifact files, so `scenario_path` and `output_path` are nulled during Soft Archive. If product wants old scenario JSON preserved for history, that should be called out explicitly before implementation.
- Assumption: `VACUUM` idle gating is based on absence of active runs, not a wall-clock scheduler or background maintenance loop.

## Dev Agent Record

### Implementation Plan

- **Config foundation (T1).** `internal/domain/config.go` adds `ArtifactRetentionDays` (default 30). `internal/config/loader.go` gains a `>= 1` validation alongside the existing `golden_staleness_days` / `shadow_eval_window` rules so the Settings files writer stays symmetric. `internal/config/settings_files.go` includes the new field in the ordered YAML writer so `pipeline init`-authored files round-trip cleanly.
- **Store surface (T4).** `internal/db/run_store.go` gains three helpers that keep archive logic out of bespoke SQL:
  - `ListArchiveCandidates(ctx, cutoff)` — terminal-only, `updated_at < cutoff`, ordered oldest-first with run-ID tie-break.
  - `HasActiveRuns(ctx)` — `EXISTS` probe over pending/running/waiting, used as the VACUUM gate.
  - `ClearRunArtifactPaths(ctx, runID)` — single UPDATE setting `scenario_path = NULL, output_path = NULL`; preserves all other columns.
- **Service orchestration (T3/T5/T6).** `internal/service/clean_service.go` is the Cobra-free orchestrator. For each candidate: call `pipeline.ArchiveRunArtifacts` (new helper in `internal/pipeline/artifact.go`) to delete known subtrees/files under `{outputDir}/{runID}/`, then reuse `SegmentStore.ClearImageArtifactsByRunID / ClearTTSArtifactsByRunID / ClearClipPathsByRunID` for per-segment refs, then `ClearRunArtifactPaths` for run-level refs. Post-archive, `HasActiveRuns` decides whether VACUUM runs — executed outside any transaction via the raw `*sql.DB`. Per-run errors are logged and skipped; VACUUM failure is surfaced via `VacuumFailed` + `VacuumError` instead of bubbled as a fatal return.
- **CLI (T2).** `cmd/pipeline/clean.go` loads config, opens DB, constructs `CleanService`, runs, and renders via the existing `Renderer`. `CleanOutput` is added to `cmd/pipeline/render.go` and both `HumanRenderer` + `JSONRenderer` already dispatch on the new type. A `cleanClock clock.Clock` package-level variable mirrors the `metricsClock` pattern for deterministic tests.
- **Regression coverage (T7).** Handler-level test at `internal/api/handler_artifacts_test.go` seeds a `(complete, completed)` run with no artifact files (the archived shape) and asserts 404 across `/video`, `/metadata`, `/manifest`. Store-level test at `internal/db/run_store_test.go` asserts `Get` + `List` keep working after `ClearRunArtifactPaths`.

### Completion Notes

- **No DB row purge** — per NFR-O2 and AC-3, runs/segments/decisions rows are retained indefinitely. Soft Archive touches only the path columns inside those rows and the artifact files under each run directory.
- **Missing-file tolerance** — `ArchiveRunArtifacts` tolerates missing entries via `os.Stat` + `errors.Is(err, fs.ErrNotExist)` before counting; re-running clean on an already-archived run reports 0 files deleted with no error.
- **Run-dir preservation for unknown files** — the archive helper tries to `os.Remove` the run directory after deleting the allowlist of known artifacts, but ignores the "directory not empty" error. Operator notes (e.g., `NOTES.md`) survive archive.
- **VACUUM outside transactions** — `applyVacuum` issues `VACUUM` via `rawDB.ExecContext` and never inside a Tx. The cleanup phase's mutations each commit immediately through the existing per-helper semantics, so by the time VACUUM runs the DB has no open writer.
- **`updated_at` drift from archive trigger** — the `runs_updated_at` AFTER UPDATE trigger bumps `updated_at` on our `UPDATE … SET scenario_path = NULL …`. The per-service idempotency test encodes this by running a far-future `now` on the second sweep so the archived run again falls past the cutoff. This matches AC-2/AC-3 "re-running clean is a no-op for an archived run, not an error".
- **Writer/Critic drift for `output_path`** — `domain.Run` still does not expose `output_path`, so the Story 10.3 store test verifies it via raw SQL. No domain-type change is required for this story, but the drift is now covered by the test so a future Run contract update won't accidentally regress it.

### Debug Log

- `go test ./cmd/pipeline ./internal/service ./internal/db ./internal/pipeline ./internal/api` — all green (see Validation section).
- `go test ./...` — full tree green; no unrelated regressions.
- `go vet ./...` — clean.

### File List

**Added**

- `cmd/pipeline/clean.go`
- `cmd/pipeline/clean_test.go`
- `internal/service/clean_service.go`
- `internal/service/clean_service_test.go`

**Modified**

- `cmd/pipeline/main.go` — register `newCleanCmd()`.
- `cmd/pipeline/render.go` — add `CleanOutput` + `CleanArchivedRun` + `renderClean` dispatch.
- `internal/domain/config.go` — add `ArtifactRetentionDays` field + default 30.
- `internal/domain/config_test.go` — default coverage for `ArtifactRetentionDays`.
- `internal/config/loader.go` — viper default + `>= 1` validation.
- `internal/config/loader_test.go` — default/override/reject-invalid coverage.
- `internal/config/settings_files.go` — extend `orderedPipelineConfig` so config.yaml writes round-trip the new key.
- `internal/db/run_store.go` — add `ArchiveCandidate`, `ListArchiveCandidates`, `HasActiveRuns`, `ClearRunArtifactPaths`.
- `internal/db/run_store_test.go` — six new unit tests covering candidate selection, idle probe, path-clear scope, idempotency, not-found, and post-archive List/Get.
- `internal/pipeline/artifact.go` — add `ArchiveRunArtifacts` run-tree cleanup helper + `pathExists`.
- `internal/pipeline/artifact_test.go` — four new tests for the archive helper (full cleanup, idempotency, unknown-file preservation, missing-runDir tolerance).
- `internal/api/handler_artifacts_test.go` — archived-run graceful-404 regression across all three artifact endpoints.

### Change Log

- **2026-04-24** — Implemented Story 10.3 Soft Archive: new `pipeline clean` CLI, `CleanService` orchestrator, per-run filesystem + per-segment DB path cleanup, idle-gated `VACUUM`, and AC-6 regression coverage for archived-run rendering.

### Review Findings

- [x] [Review][Patch] P1: VACUUM failure exits 0 — AC-5 "do not silently swallow" violated [cmd/pipeline/clean.go]
- [x] [Review][Patch] P2: MarkComplete writes RFC3339Nano; ListArchiveCandidates compares space-format — boundary-day runs skipped [internal/db/run_store.go:471]
- [x] [Review][Patch] P3: `os.Remove(runDir)` swallows EACCES/EIO as "non-empty" [internal/pipeline/artifact.go:115]
- [x] [Review][Patch] P4: `DBRefsCleared += 2` unconditional — overstates count for already-null runs [internal/service/clean_service.go:191]
- [x] [Review][Patch] P5: Tie-break test insertion order matches sort order — ORDER BY id not exercised [internal/db/run_store_test.go:1292]
- [x] [Review][Patch] P6: `TestArchiveRunArtifacts_IsIdempotent` recreates runDir — doesn't test true idempotency [internal/pipeline/artifact_test.go:161]
- [x] [Review][Patch] P7: `insertSeedRunDirect` uses `StagePending` for all terminal status seeds [internal/db/run_store_test.go:1233]
- [x] [Review][Defer] D1: Path traversal via run ID containing `..` [internal/service/clean_service.go:151] — deferred, run IDs are system-generated `scp-X-run-N`; no external injection path
- [x] [Review][Defer] D2: Empty `cfg.OutputDir` not validated before clean [cmd/pipeline/clean.go] — deferred, pre-existing loader gap; `DefaultConfig()` always provides a value
