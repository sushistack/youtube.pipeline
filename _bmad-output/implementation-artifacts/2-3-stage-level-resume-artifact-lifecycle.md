# Story 2.3: Stage-Level Resume & Artifact Lifecycle

Status: done

## Story

As an operator,
I want to resume a failed run from the last successful stage with filesystem/DB state verified and partial artifacts cleaned,
so that I don't lose completed work and don't accumulate orphaned resources across retries.

## Acceptance Criteria

1. **AC-RESUME-CLI:** `pipeline resume <run-id>` exists as a Cobra command. It loads config, opens DB, calls `RunService.Resume(ctx, runID)`, and renders a `RunOutput` (or `ResumeOutput`) on success. Unknown run → `domain.ErrNotFound`. Terminal run (`status=completed|cancelled`) → `domain.ErrConflict`. Non-failed, non-waiting run in progress → `domain.ErrConflict`.

2. **AC-RESUME-POINT:** `RunService.Resume(ctx, runID)` re-enters the **failed stage** for a run where `status=failed`. `runs.stage` is not advanced — the same stage that failed is the stage that resumes. For `status=waiting` (HITL paused), Resume acts as a re-entry that leaves `runs.stage` unchanged and restores `status` to the stage's natural operational status via `pipeline.StatusForStage(stage)`. Completed stages are **never** re-executed.

3. **AC-RESUME-API:** `POST /api/runs/{id}/resume` replaces the Story 2.2 501 stub. On success: 200 + `{"version":1,"data":<run>}`. On `ErrNotFound`: 404. On `ErrConflict`: 409. On `ErrValidation` (e.g. FS/DB inconsistency not confirmed): 400. All via existing `writeDomainError`.

4. **AC-FS-DB-CONSISTENCY:** Before any artifact cleanup, Resume runs a filesystem↔DB consistency check against the per-run output directory (`{cfg.OutputDir}/{run-id}/`):
   - For every `segments` row with a non-null `tts_path` / `clip_path` / any `shots[].image_path`, verify the file exists on disk.
   - For every **completed** stage whose canonical artifacts are expected (e.g. `scenario.json` for post-Phase-A stages), verify presence.
   - On any inconsistency, Resume returns a `domain.InconsistencyReport` describing each mismatch **and** aborts unless `--force` (CLI) or `"confirm_inconsistent": true` (API body) is supplied. Mismatches are emitted at `slog.Warn` level regardless.

5. **AC-PHASE-B-CLEAN-SLATE:** If the failed stage is Phase B (`image` or `tts`), Resume executes — in a single transaction — `DELETE FROM segments WHERE run_id = ?`, scoped **exclusively** to the target run. Re-insertion occurs on the next real execution; Resume itself does not pre-populate rows. The `UNIQUE(run_id, scene_index)` constraint still guards against duplicates. `DELETE` scope MUST be asserted by test (segments of other runs untouched).

6. **AC-ARTIFACT-CLEANUP:** Resume cleans partial artifact files on disk, scoped to the failed stage:
   - `image` → remove `images/` subtree under the run directory.
   - `tts` → remove `tts/` subtree under the run directory.
   - `assemble` → remove `clips/` subtree and any `output.mp4` at run root.
   - `metadata_ack` → remove `metadata.json` and `manifest.json` at run root.
   - Phase A stages (`research`..`critic`) → no on-disk artifacts exist yet (Phase A is in-memory until `scenario_review`). Cleanup is a no-op, and a `scenario.json` present in an inconsistent Phase A state is reported via AC-FS-DB-CONSISTENCY, not silently deleted.
   - HITL stages (`scenario_review`, `character_pick`, `batch_review`, `metadata_ack`) have no partial artifacts of their own; cleanup is a no-op for these.
   - Cleanup MUST NOT touch: `scenario.json` when the failed stage is Phase B (Phase A output is completed work), `characters/` when failed stage is not `character_pick`, or the `rejected/` subdirectory ever.

7. **AC-STATUS-RESET:** After consistency check passes (or is force-overridden) and artifacts are cleaned, Resume performs — in a single DB transaction:
   - `UPDATE runs SET status = ?, retry_reason = NULL WHERE id = ?` where the new status is `pipeline.StatusForStage(run.Stage)` (→ `running` for automated stages, `waiting` for HITL).
   - Optionally, `retry_count` is incremented by 1 (semantic: a resume is one retry attempt).
   - The `updated_at` trigger from Migration 002 must fire — verified by a post-resume `updated_at > pre-resume updated_at` test assertion.

8. **AC-IDEMPOTENCY-NFR-R1:** Running `pipeline resume <run-id>` twice in a row on the same failed run (without any external mutation between calls) MUST produce the same terminal state: same `runs.stage`, same `runs.status`, same (empty after Phase B clean) `segments` row count, same on-disk artifact tree. The second Resume either (a) re-cleans the already-clean state or (b) no-ops because the run is already in an equivalent resumable state. Integration test verifies this explicitly.

9. **AC-SEGMENT-STORE:** `internal/db/segment_store.go` implements `service.SegmentStore` with at least: `ListByRunID(ctx, runID) ([]*domain.Episode, error)`, `DeleteByRunID(ctx, runID) (int64, error)` returning rows deleted. Co-located test `internal/db/segment_store_test.go` covers: list empty, list populated, delete scope (other-run segments untouched), delete on empty run returns 0.

10. **AC-ENGINE-RESUME:** `internal/pipeline/engine.go` gains a real implementation of the `pipeline.Runner` interface's `Resume(ctx, runID) error` method (the interface was declared in Story 2.1). The engine struct is constructed with `NewEngine(runStore, segmentStore, clock, outputDir, logger)`. The `Advance` method remains a stub returning `fmt.Errorf("advance not implemented: epic 3 scope")` — Story 2.3 does NOT wire automated stage execution. `Resume` contains the orchestration: load run → classify stage → consistency check → artifact cleanup → (if Phase B) segments DELETE → status reset.

11. **AC-CONFIRM-FLAG:** The `pipeline resume` command accepts `--force` (bool, default false). When set, `RunService.Resume` bypasses the inconsistency abort and proceeds with cleanup even if mismatches are found. The inconsistency report is still emitted on stderr in human mode / as an additional JSON field (`warnings`) in `--json` mode.

12. **AC-TESTS-INTEGRATION:** Integration tests in `internal/pipeline/engine_test.go` (or a new `resume_test.go`) and `internal/service/run_service_test.go` cover:
    - (a) Resume from Phase A failure (e.g. `write`): no disk cleanup performed, status reset.
    - (b) Resume from Phase B failure (e.g. `tts`) with 3 existing segments: all segments DELETEd, `tts/` dir removed, `images/` preserved if only tts failed? → **Clarification:** Per AC-ARTIFACT-CLEANUP the cleanup is scoped to the failed stage; DELETE-all-segments on Phase B resume is because the whole Phase B re-runs (architecture decision). Test verifies: segments=0, only the failed-stage artifact tree removed.
    - (c) Resume from `assemble` failure: `clips/` and `output.mp4` removed, segments preserved, status = running.
    - (d) FS↔DB mismatch without `--force`: Resume returns an `ErrValidation`-classified error containing the inconsistency description; no cleanup or DB mutation is performed.
    - (e) FS↔DB mismatch with `--force`: Resume logs warnings, proceeds with cleanup, succeeds.
    - (f) Resume of a `completed` run: `ErrConflict`.
    - (g) Resume of a nonexistent run: `ErrNotFound`.
    - (h) Idempotency: resume twice → identical terminal state.
    - (i) DELETE scope: a second run with its own segments is present in the DB; resume of run A does not touch run B's segments.
    - (j) `updated_at` advances: post-resume `updated_at` > pre-resume `updated_at` (trigger verification).

13. **AC-FIXTURES:** A new fixture `testdata/fixtures/failed_at_tts.sql` seeds a run with `stage=tts`, `status=failed`, 5 segments (3 completed with `tts_path` set, 2 pending), and matching on-disk `tts/*.wav` files. Tests that need a disk-backed scenario use this fixture + `t.TempDir()` to materialize the output tree. A second fixture `testdata/fixtures/failed_at_write.sql` seeds a Phase A failure (no segments, no disk artifacts expected) for scenario (a).

14. **AC-CONTRACT-FIXTURE:** Add `testdata/contracts/run.resume.response.json` with envelope `{"version":1,"data":<run>}` + optional `warnings` array. Contract test parses this fixture and asserts snake_case fields match.

15. **AC-LINT-LAYERS-CLEAN:** `make lint-layers` passes unchanged. The layer-lint rules in `scripts/lintlayers/main.go` already permit `internal/pipeline → {domain, db, llmclient, clock}` and `internal/service → {domain, db, pipeline, clock}`, so no rule edits are required. Resume's implementation MAY import `internal/db` for concrete stores, but this story chooses to declare minimal `RunStore` / `SegmentStore` interfaces **inside** `internal/pipeline/engine.go` and accept them by interface at `NewEngine` — purely for unit-test ergonomics (inline fake stores). The `db.RunStore` and `db.SegmentStore` concrete types satisfy these interfaces structurally. `cmd/pipeline/resume.go` imports `service`, `config`, `db`, `pipeline`, consistent with other command files.

16. **AC-NO-REGRESSIONS:** `go test ./... && go build ./... && make lint-layers` passes. All 2.1 / 2.2 tests continue to pass without modification. CGO remains disabled.

---

## Tasks / Subtasks

- [x] **T1: domain additions** (AC: #4, #11)
  - [x] Add `InconsistencyReport` struct to `internal/domain/types.go` (or a new `internal/domain/resume.go` if types.go approaches 300-line cap):
    ```go
    type InconsistencyReport struct {
        RunID    string
        Stage    Stage
        Mismatches []Mismatch
    }
    type Mismatch struct {
        Kind     string // "missing_file", "orphan_segment", "unexpected_scenario_json"
        Path     string
        Expected string
        Detail   string
    }
    ```
  - [x] Add `(r InconsistencyReport) Error() string` so it can wrap into `ErrValidation` via `fmt.Errorf("%w: %s", domain.ErrValidation, report.Error())`.
  - [x] Add domain test cases in `types_test.go` (or new `resume_test.go`) for Error() formatting stability.

- [x] **T2: db/segment_store.go — SegmentStore CRUD** (AC: #5, #9)
  - [x] Create `internal/db/segment_store.go` with `type SegmentStore struct { db *sql.DB }` + `NewSegmentStore(*sql.DB) *SegmentStore`.
  - [x] Implement `ListByRunID(ctx, runID) ([]*domain.Episode, error)` — SELECT * WHERE run_id=? ORDER BY scene_index ASC. Use a `scanEpisode` helper analogous to `scanRun`. JSON-decode `shots` TEXT column into `[]domain.Shot` (nil on empty/NULL).
  - [x] Implement `DeleteByRunID(ctx, runID) (int64, error)` — `DELETE FROM segments WHERE run_id = ?`; return `res.RowsAffected()`. Critical: **no other predicates** — the WHERE clause is the scope guard.
  - [x] Create `internal/db/segment_store_test.go` — `testutil.NewTestDB(t)` + `testutil.BlockExternalHTTP(t)`. Tests: list empty → `nil, nil`; list populated with 3 segments in scene_index order; delete scope isolation (seed two runs, delete one, assert the other's rows remain); delete on empty returns `(0, nil)`.

- [x] **T3: pipeline/artifact.go — artifact cleanup helpers** (AC: #6)
  - [x] Create `internal/pipeline/artifact.go`:
    ```go
    // CleanStageArtifacts removes on-disk artifacts scoped to a failed stage.
    // runDir is the absolute path to the per-run output directory.
    // Unknown stage or HITL stage → no-op, nil error.
    func CleanStageArtifacts(runDir string, stage domain.Stage) error
    ```
  - [x] Per-stage mapping (hard-coded switch; no config indirection for V1):
    - `StageImage` → `os.RemoveAll(filepath.Join(runDir, "images"))`
    - `StageTTS` → `os.RemoveAll(filepath.Join(runDir, "tts"))`
    - `StageAssemble` → RemoveAll `clips/` + `os.Remove` on `output.mp4` (ignore NotExist)
    - `StageMetadataAck` → `os.Remove` on `metadata.json` and `manifest.json` (ignore NotExist)
    - default → nil
  - [x] `errors.Is(err, fs.ErrNotExist)` → swallow (cleanup is idempotent; missing files are fine).
  - [x] Create `internal/pipeline/artifact_test.go` — table-driven: set up `t.TempDir()` with fake files for each stage, call CleanStageArtifacts, assert expected files gone + others preserved. Include an idempotency test (call twice, both succeed).

- [x] **T4: pipeline/consistency.go — FS↔DB consistency checker** (AC: #4)
  - [x] Create `internal/pipeline/consistency.go`:
    ```go
    // CheckConsistency verifies filesystem state matches DB-recorded artifact paths.
    // Non-nil InconsistencyReport is returned WITH nil error when mismatches are
    // found (caller decides whether to abort); non-nil error is for I/O failures.
    func CheckConsistency(runDir string, run *domain.Run, segments []*domain.Episode) (*domain.InconsistencyReport, error)
    ```
  - [x] Checks performed:
    - For each segment's `TTSPath` (non-nil): `os.Stat` the file; on NotExist add `Mismatch{Kind:"missing_file", Path:*seg.TTSPath}`.
    - For each segment's `ClipPath` (non-nil): same check.
    - For each shot in `seg.Shots`: `os.Stat` `shot.ImagePath`; on NotExist record mismatch.
    - If `run.Stage` is post-`critic` AND `run.ScenarioPath` is non-nil: verify the file exists.
    - If cleanup will target a stage but stray files for OTHER stages are absent when they should be present (e.g. resume-from-tts but `images/` is missing entirely), record an orphan/missing warning — **but** do not block on this; cleanup is still safe.
  - [x] Shot paths may be stored either absolute or relative to `runDir` — the function resolves relative paths against `runDir` before stat.
  - [x] Create `internal/pipeline/consistency_test.go` — happy path (all files present → report is empty, not nil, Mismatches has length 0); missing TTS file; missing image shot; malformed `shots` JSON edge case (expect graceful handling). Use `t.TempDir()` for filesystem scaffolding.

- [x] **T5: pipeline/engine.go — Engine struct + Resume()** (AC: #2, #5, #7, #10)
  - [x] In `internal/pipeline/engine.go`, add (alongside the existing pure functions):
    ```go
    // RunStore and SegmentStore are the minimal store dependencies for the engine.
    // Mirrors service-layer interfaces to avoid an import cycle through service/.
    type RunStore interface {
        Get(ctx context.Context, id string) (*domain.Run, error)
        SetStatus(ctx context.Context, id string, status domain.Status, retryReason *string) error
        IncrementRetryCount(ctx context.Context, id string) error
    }
    type SegmentStore interface {
        ListByRunID(ctx context.Context, runID string) ([]*domain.Episode, error)
        DeleteByRunID(ctx context.Context, runID string) (int64, error)
    }

    type Engine struct {
        runs      RunStore
        segments  SegmentStore
        clock     clock.Clock
        outputDir string
        logger    *slog.Logger
    }

    func NewEngine(runs RunStore, segments SegmentStore, clk clock.Clock, outputDir string, logger *slog.Logger) *Engine
    func (e *Engine) Advance(ctx context.Context, runID string) error // stub: returns "not implemented: epic 3 scope"
    func (e *Engine) Resume(ctx context.Context, runID string) error
    func (e *Engine) ResumeWithOptions(ctx context.Context, runID string, opts ResumeOptions) error
    ```
  - [x] `ResumeOptions{ Force bool }` — exposed so service/CLI can pass `--force`.
  - [x] Resume orchestration order (strict; deviation breaks AC-IDEMPOTENCY and AC-FS-DB-CONSISTENCY):
    1. Load run via `runs.Get`. If not found → return `domain.ErrNotFound` wrapped.
    2. Validate resumable state: `status IN (failed, waiting)`. Else → `domain.ErrConflict` wrapped with descriptive message.
    3. Terminal states (`completed`, `cancelled`) → `domain.ErrConflict`.
    4. Load segments via `segments.ListByRunID` (needed for consistency).
    5. `report, err := CheckConsistency(runDir, run, segments)`. On I/O error, abort.
    6. If `len(report.Mismatches) > 0` and `!opts.Force`: log warnings + return `fmt.Errorf("%w: %s", domain.ErrValidation, report.Error())`.
    7. If `report` has mismatches and `opts.Force` set: log at `Warn`; proceed.
    8. `CleanStageArtifacts(runDir, run.Stage)` — I/O error aborts.
    9. If `run.Stage` is `StageImage` or `StageTTS`: `segments.DeleteByRunID(runID)` — DB error aborts.
    10. `runs.SetStatus(ctx, runID, pipeline.StatusForStage(run.Stage), nil)` (nil clears retry_reason).
    11. `runs.IncrementRetryCount(ctx, runID)`.
    12. Return nil.
  - [x] Import discipline: `pipeline/engine.go` imports `domain/`, `clock/`, `log/slog`, `context`, `os`, `path/filepath`, stdlib only. **No import of `service/`.** `db/` import is allowed by the lint rules but deliberately avoided here — stores are supplied via the locally declared interfaces.
  - [x] Tests in `internal/pipeline/engine_test.go` (extend) or new `resume_test.go`:
    - Stubbed `RunStore`/`SegmentStore` via inline struct fakes (no gomock). Each fake tracks call log + returns configured values.
    - Cover AC-TESTS-INTEGRATION scenarios (a)–(j) as table-driven where practical.

- [x] **T6: db/run_store.go — SetStatus + IncrementRetryCount** (AC: #7)
  - [x] Extend existing `internal/db/run_store.go`:
    ```go
    func (s *RunStore) SetStatus(ctx context.Context, id string, status domain.Status, retryReason *string) error
    func (s *RunStore) IncrementRetryCount(ctx context.Context, id string) error
    ```
  - [x] `SetStatus`: `UPDATE runs SET status=?, retry_reason=? WHERE id=?`. `retryReason=nil` → `NULL` via `sql.NullString{Valid:false}`. 0 rows affected → `domain.ErrNotFound`. Verified the Migration 002 trigger advances `updated_at`.
  - [x] `IncrementRetryCount`: `UPDATE runs SET retry_count = retry_count + 1 WHERE id = ?`. 0 rows → `ErrNotFound`.
  - [x] Extend `internal/db/run_store_test.go`: `TestRunStore_SetStatus_Success`, `TestRunStore_SetStatus_ClearsRetryReason`, `TestRunStore_SetStatus_NotFound`, `TestRunStore_IncrementRetryCount_Success`, `TestRunStore_IncrementRetryCount_NotFound`, `TestRunStore_SetStatus_UpdatedAtAdvances` (sleep 1s between create and SetStatus OR use the clock interface — see Dev Notes → "Testing updated_at trigger").

- [x] **T7: service/run_service.go — Resume method** (AC: #1, #11, #15)
  - [x] Extend `service.RunStore` interface with the new methods:
    ```go
    type RunStore interface {
        Create(ctx context.Context, scpID, outputDir string) (*domain.Run, error)
        Get(ctx context.Context, id string) (*domain.Run, error)
        List(ctx context.Context) ([]*domain.Run, error)
        Cancel(ctx context.Context, id string) error
        SetStatus(ctx context.Context, id string, status domain.Status, retryReason *string) error
        IncrementRetryCount(ctx context.Context, id string) error
    }
    ```
  - [x] Add `service.SegmentStore` interface (consumer-defines):
    ```go
    type SegmentStore interface {
        ListByRunID(ctx context.Context, runID string) ([]*domain.Episode, error)
        DeleteByRunID(ctx context.Context, runID string) (int64, error)
    }
    ```
  - [x] Change `RunService` to accept an `Engine` dependency (or a smaller `Resumer` interface exposing only `Resume(ctx, runID, opts) error`) to avoid re-implementing orchestration in both `pipeline/` and `service/`:
    ```go
    type Resumer interface {
        ResumeWithOptions(ctx context.Context, runID string, opts pipeline.ResumeOptions) error
    }
    type RunService struct {
        store   RunStore
        resumer Resumer
    }
    func NewRunService(store RunStore, resumer Resumer) *RunService
    func (s *RunService) Resume(ctx context.Context, id string, force bool) (*domain.Run, error)
    ```
  - [x] `Resume` implementation: call `s.resumer.ResumeWithOptions(ctx, id, pipeline.ResumeOptions{Force: force})`, then `s.store.Get(ctx, id)` to return the updated run.
  - [x] Extend `internal/service/run_service_test.go`: fake `Resumer` that records invocations + returns configured error. Test the happy path + validation error propagation + not-found propagation.
  - [x] Adjust all existing `NewRunService` call sites (`cmd/pipeline/{create,cancel,status,serve}.go`) to thread the new constructor signature. Keep the CLI wiring identical — build the engine where we already build the DB/store, pass it in. Missing resumer → fail fast with panic OR accept nil with degraded resume endpoint returning ErrValidation (prefer the explicit panic: no dead layers).

- [x] **T8: cmd/pipeline/resume.go** (AC: #1, #11)
  - [x] Create `cmd/pipeline/resume.go` mirroring `cancel.go` structure: `newResumeCmd()` → `cobra.Command{ Use: "resume <run-id>", Args: cobra.ExactArgs(1), RunE: runResume }` with `--force` flag bound to a local bool.
  - [x] `runResume`: load config → open DB → build `RunStore`, `SegmentStore`, `Engine`, `RunService` (with resumer) → call `svc.Resume(ctx, runID, force)`. Render via `newRenderer`.
  - [x] Register the command in `cmd/pipeline/main.go`.
  - [x] Add a `ResumeOutput` struct to `cmd/pipeline/render.go` (or reuse `RunOutput` + a `Warnings []string` extension; architecture prefers one small dedicated type — introduce `ResumeOutput{ Run RunOutput; Warnings []string }` and add `renderResume` on HumanRenderer + JSON envelope handling).

- [x] **T9: api/handler_run.go — replace Resume stub** (AC: #3)
  - [x] Replace the current 501 stub in `internal/api/handler_run.go` with:
    ```go
    func (h *RunHandler) Resume(w http.ResponseWriter, r *http.Request) {
        id := r.PathValue("id")
        var body struct {
            ConfirmInconsistent bool `json:"confirm_inconsistent"`
        }
        _ = json.NewDecoder(r.Body).Decode(&body) // empty body OK
        run, err := h.svc.Resume(r.Context(), id, body.ConfirmInconsistent)
        if err != nil {
            h.logger.Error("resume run", "run_id", id, "error", err)
            writeDomainError(w, err)
            return
        }
        h.logger.Info("run resumed", "run_id", id)
        writeJSON(w, http.StatusOK, toRunResponse(run))
    }
    ```
  - [x] Extend `internal/api/handler_run_test.go`: happy path with fake service; 404 on `ErrNotFound`; 409 on `ErrConflict`; 400 on `ErrValidation`; `confirm_inconsistent: true` is forwarded to the service fake.
  - [x] Update or retire `testdata/contracts/run.resume.response.json` contract test (add a new fixture file and matching test).

- [x] **T10: Fixtures + wiring** (AC: #13, #14)
  - [x] Create `testdata/fixtures/failed_at_tts.sql` — one run `scp-049-run-1` with `stage=tts, status=failed, retry_count=1, retry_reason='upstream_timeout'`, plus 5 segments (3 completed with `tts_path='tts/scene_01.wav'`…, 2 pending with null tts_path) plus optional decisions. Follow the existing `paused_at_batch_review.sql` style.
  - [x] Create `testdata/fixtures/failed_at_write.sql` — Phase A failure: `stage=write, status=failed`; zero segments.
  - [x] Create `testdata/contracts/run.resume.response.json` — envelope `{"version":1,"data":{…run…},"warnings":[]}` with a realistic run snapshot. Ensure snake_case JSON keys match `toRunResponse`.
  - [x] Extend `internal/testutil/contract_test.go` (or a new dedicated test) to assert the contract JSON parses into the `runResponse` + `warnings` shape without drift.

- [x] **T11: Integration coverage in internal/pipeline/e2e_test.go** (AC: #12)
  - [x] Add a Resume-focused integration test inside the existing `internal/pipeline/e2e_test.go` OR create `internal/pipeline/resume_integration_test.go`: uses `testutil.LoadRunStateFixture(t, "failed_at_tts")` + materializes the fixture's on-disk `tts/*.wav` files under a `t.TempDir()`-based output directory. Exercises the full CLI-equivalent path: `NewEngine → service.Resume`. Asserts segments deleted, tts/ removed, status=running, updated_at advanced, retry_count incremented.

- [x] **T12: Lint + green build** (AC: #15, #16)
  - [x] Run `go build ./... && go test ./... && make lint-layers`.
  - [x] Verify the layer-lint script's rules cover `pipeline/` not importing `service/` or `db/`. If new packages are introduced, update `scripts/lintlayers/main.go` to match (document the change in Completion Notes).
  - [x] Confirm zero regressions on all prior 2.x and 1.x tests.

---

## Dev Notes

### Why Stage-Level Resume, Not Per-Scene Resume

Architecture [architecture.md:179,455-457] explicitly marks per-scene resume as **V1.5 deferred**. In V1, a Phase B failure at scene 4/10 causes Resume to DELETE all segments and re-run the entire phase. This is documented as an intentional simplification — per-scene resume requires UPSERT semantics and scene-level checkpointing, both out of scope. Do NOT attempt to be clever with partial re-execution; architecture/epics/NFR-R1 are all aligned on clean-slate Phase B restart.

### Phase A vs Phase B Artifact Semantics

| Stage group | On-disk artifacts during execution | Resume cleanup |
|---|---|---|
| Phase A (research → critic) | None. `PipelineState` is in-memory. `scenario.json` is written only when Phase A **completes** (i.e. after critic→scenario_review transition). | No-op. If `scenario.json` exists while `run.Stage` is pre-`scenario_review`, report it as inconsistency. |
| HITL stages (scenario_review, character_pick, batch_review, metadata_ack) | None of their own. | No-op. |
| character_pick (prerequisite) | `characters/references/*` + `characters/canonical.png` — these are **completed work** before the HITL gate, not partial. | Do NOT clean on resume unless the failed stage IS `character_pick`. |
| Phase B (image, tts) | `images/scene_NN/shot_NN.png`, `tts/scene_NN.wav`. | Stage-scoped RemoveAll. Also DELETE segments (clean slate). |
| Phase C (assemble) | `clips/scene_NN.mp4`, `output.mp4`. | RemoveAll clips/ + remove output.mp4. Segments preserved (Phase B work is done). |
| metadata_ack | `metadata.json`, `manifest.json`. | Remove both files. |

Reference: [architecture.md:793-822] for the canonical directory tree.

### Interface Placement for Resume

The layer-lint rules in `scripts/lintlayers/main.go` explicitly allow `internal/pipeline → internal/db` and `internal/service → {internal/db, internal/pipeline}` — so strictly speaking, the Engine could take `*db.RunStore` / `*db.SegmentStore` concretely. We deliberately do NOT do this. Instead, the wiring is:

- `service/run_service.go` defines `service.RunStore` and `service.SegmentStore` — the **service** is the primary consumer. It also defines a 1-method `service.Resumer` interface satisfied by `*pipeline.Engine`.
- `pipeline/engine.go` declares its OWN minimal `RunStore` / `SegmentStore` interfaces locally (containing only what `Resume` actually calls). `*db.RunStore` and `*db.SegmentStore` satisfy both the `service.*` and `pipeline.*` flavors structurally — Go doesn't require explicit implements declarations.
- `cmd/pipeline/*.go` wiring: build `*sql.DB` → build `*db.RunStore`, `*db.SegmentStore` → build `*pipeline.Engine` with NewEngine(store, store, clock, cfg.OutputDir, logger) → build `*service.RunService` with NewRunService(store, engine).

```
cmd/pipeline/resume.go → service, pipeline (NewEngine), db, config
service/run_service.go → service.{RunStore,SegmentStore,Resumer}
pipeline/engine.go     → pipeline.{RunStore,SegmentStore} (local, minimal)
db/run_store.go        → satisfies both service.RunStore and pipeline.RunStore structurally
db/segment_store.go    → satisfies both service.SegmentStore and pipeline.SegmentStore structurally
```

Why two sets of interfaces instead of pipeline re-using `service.*`? Avoiding `pipeline → service` keeps the layering direction one-way and preserves `pipeline`'s independence from `service` — exactly the reason `Runner` was declared in `pipeline/` back in Story 2.1 (architecture.md:1714-1727). This story is the dual: `pipeline/` declares minimal interfaces for what `Resume` consumes.

### State Machine — Resume is NOT a NextStage event

The existing `NextStage(current, event)` pure function in `engine.go` is for forward transitions. Resume does NOT go through `NextStage`; it re-enters the same stage. The distinction matters because:

- There is no `EventResume` in `domain.Event` (and there must not be — it's not a state transition, it's a re-entry of the same state).
- Resume's job is to restore `runs.status` to the **natural status of the existing stage** via `StatusForStage(run.Stage)`. This is why automated stages become `running` and HITL stages become `waiting` after resume.
- The failing stage's side effects (partial artifacts, partial segments) are wiped; the stage itself is un-changed in the `runs.stage` column.

### Testing updated_at Trigger

The Migration 002 trigger updates `updated_at` to `datetime('now')` which is second-precision. A naive test that measures pre/post in the same second will see identical timestamps. Two tactics:

1. **Sleep(1s)** — acceptable for one trigger test; tags it with `t.Skip()` under `-short`.
2. **Inject a clock** for SetStatus — then the test fixture can advance the fake clock by 1s before the SetStatus call. The trigger still uses `datetime('now')` (SQLite built-in), so this only works for the Go-side stamping. For the trigger itself, Tactic 1 is the only option.

Pick tactic 1 for the single trigger-advance test. Document with a comment referencing this Dev Note.

### Resumable State Set

`status IN (failed, waiting)` — both are valid resume entry points:

- `failed` — automated stage error; the operator runs `resume` to retry.
- `waiting` — HITL gate; `resume` is a no-op-like re-entry (no cleanup, no segment delete) because no artifacts are partial — the stage was waiting for human input. Useful when the UI needs to "re-enter" the HITL state or for test scenarios. But per architecture [architecture.md:1237] HITL approvals normally go through the dedicated `approve` endpoint. Resume-of-waiting is a belt-and-suspenders path, not the primary HITL exit.

`pending` — never resumable; the run never started. Return `ErrConflict` with message "run has not started; use create to begin".
`running` — never resumable (state machine does not allow concurrent re-entry). Return `ErrConflict`.
`completed`, `cancelled` — terminal; return `ErrConflict`.

### DELETE Scope Assertion (Critical)

The single most dangerous bug in this story would be a `DELETE FROM segments` without the `WHERE run_id = ?` predicate, which would wipe every run's segments. Story 2.2's `Cancel` made an analogous mistake possible (UPDATE without ID); the test there explicitly seeded two runs and asserted the second was untouched. Follow the same pattern:

```go
func TestSegmentStore_DeleteByRunID_ScopeIsolation(t *testing.T) {
    // seed run A with 3 segments, run B with 2 segments
    // delete run A → assert run B's 2 segments still present
}
```

Also: `sprint-prompts.md:277` explicitly lists this as a review checkpoint — **guarantee** the DELETE is scoped. Prefer passing a positional parameter via `sql.NamedArg` or plain `?` placeholder; never interpolate.

### FS↔DB Consistency Check Ordering (Review Checkpoint)

`sprint-prompts.md:278` calls out: consistency check must run **BEFORE** stage start (i.e. at resume entry, not after cleanup or after any state mutation). The orchestration order in AC-ENGINE-RESUME step 5 is deliberate:

1. Load run
2. Check state is resumable
3. Load segments
4. **Consistency check** ← here, BEFORE cleanup
5. Cleanup
6. DELETE segments (if Phase B)
7. Status reset

A reviewer will fail the story if 4 is done after 5. Do not "fix" a flaky consistency test by re-ordering.

### Idempotency Test Pattern

```go
func TestResume_Idempotent(t *testing.T) {
    // arrange: failed run + partial artifacts
    engine := NewEngine(...)
    AssertEqual(t, nil, engine.Resume(ctx, runID))
    snapshot1 := captureState(t, db, runDir) // stage, status, segments, file tree
    AssertEqual(t, nil, engine.Resume(ctx, runID))
    snapshot2 := captureState(t, db, runDir)
    AssertEqual(t, snapshot1.stage, snapshot2.stage)
    AssertEqual(t, snapshot1.status, snapshot2.status)
    AssertEqual(t, snapshot1.segmentCount, snapshot2.segmentCount)
    AssertEqual(t, snapshot1.fileTree, snapshot2.fileTree)
}
```

The second Resume may increment `retry_count` again — that's **not** part of idempotency for NFR-R1 (which is about schema/stage-status progression, not audit counters). Document the retry_count delta explicitly in the test to avoid confusion.

### Why No engine.Advance() in This Story

The `Runner` interface declares `Advance`, and architecture says `engine.go` has "State machine: Advance(), Resume()". But:

- Automated stage execution (the body of `Advance`) requires agent chains (Epic 3), Phase B parallel runner (Epic 5), FFmpeg assembly (Epic 9).
- Wiring `Advance` as a stub here is aligned with 2.2's stub of `Resume`; it matches the skeleton-first phasing.
- Returning `fmt.Errorf("advance not implemented: epic 3 scope")` is the right path — NOT removing the method, since the interface contract requires it.

Story 2.4 (per-stage observability) also does not wire `Advance`. The full orchestration happens when Epic 3's `phase_a.go` is implemented.

### Previous Story Learnings Applied

From 2.1:
- `NextStage` + `StatusForStage` + `IsHITLStage` are stable pure functions. Reuse `StatusForStage` for AC-STATUS-RESET step.
- `Runner` interface already exists; just implement it.
- 45 invalid-transition tests pattern — similar table-driven style for resume scenarios.

From 2.2:
- `testutil.NewTestDB(t)` and `testutil.LoadRunStateFixture(t, fixture)` are the canonical test DB helpers — use them.
- `testutil.BlockExternalHTTP(t)` in every new test file (paranoid habit).
- `domain.Classify` is the single error classifier; API handler uses `writeDomainError` — do NOT reintroduce `mapDomainError`.
- Snake_case everywhere in JSON, including any new `warnings` field.
- Cancel's `Get → Update` pattern (verify existence first, then mutate) is the pattern for Resume's state-check + mutation sequence.
- `domain.ErrConflict` is the right error for "state not resumable" cases; it maps to HTTP 409.
- Module path `github.com/sushistack/youtube.pipeline`.
- CGO_ENABLED=0 everywhere.
- Contract fixtures never auto-update; manually author `run.resume.response.json`.

### Deferred Work Awareness (Do Not Resolve in This Story)

- **From 1.4 / 1.7:** `BlockExternalHTTP` mutates global DefaultTransport without sync — parallel test issue. Don't fix here; call it in every test file like prior stories do.
- **From 2.1:** `NextStage` error message does not distinguish "unknown stage" vs "invalid transition". Don't fix here.
- **From 2.1:** `StatusForStage` returns StatusRunning for unknown stages without error. Don't fix here — Resume only calls it with stages loaded from the DB (already validated).
- **From 1.2:** WAL sidecar files inherit default umask. Not relevant to this story.

### Deferred Work This Story May Generate (Log in Code Review)

- `retry_count` semantics: does every Resume-from-failed increment? What if user runs resume twice by accident? Current design: yes, always +1. A debounce (only increment on actual cleanup) is a V1.5 concern.
- `--force` for HTTP — exposing it as `{"confirm_inconsistent": true}` is terse; a richer API would return the report in the 400 body so the client can display mismatches and then re-POST with confirm. Document this limitation in Completion Notes if skipped.
- Phase C partial-scene recovery (resume mid-FFmpeg): current design wipes clips/ entirely on assemble failure. An operator who already has 9/10 clips rendered might want to preserve them. Architecture V1 explicitly says don't bother — each scene clip re-renders in tens of seconds.
- `characters/` artifact cleanup on `character_pick` resume: not handled in V1 because character_pick is a HITL re-entry, not a compute-redo. Document as V1.5.

### Project Structure After This Story

```
internal/
  domain/
    types.go                 # MODIFIED — add InconsistencyReport, Mismatch (or split to resume.go if over 300 lines)
    types_test.go            # MODIFIED — InconsistencyReport.Error() tests
  db/
    run_store.go             # MODIFIED — add SetStatus, IncrementRetryCount
    run_store_test.go        # MODIFIED — new method tests + updated_at trigger test
    segment_store.go         # NEW — ListByRunID, DeleteByRunID
    segment_store_test.go    # NEW
  pipeline/
    engine.go                # MODIFIED — Engine struct + NewEngine + Advance stub + Resume/ResumeWithOptions
    engine_test.go           # MODIFIED — resume scenarios (or split to resume_test.go if large)
    artifact.go              # NEW — CleanStageArtifacts
    artifact_test.go         # NEW
    consistency.go           # NEW — CheckConsistency
    consistency_test.go      # NEW
    # optional: resume_integration_test.go
  service/
    run_service.go           # MODIFIED — Resume method, RunStore interface extended, SegmentStore interface added, Resumer interface added, NewRunService signature changed
    run_service_test.go      # MODIFIED — Resume tests
  api/
    handler_run.go           # MODIFIED — real Resume implementation
    handler_run_test.go      # MODIFIED — Resume tests
cmd/pipeline/
  main.go                    # MODIFIED — register newResumeCmd; refit NewRunService wiring for create/cancel/status/serve
  create.go                  # MODIFIED — NewRunService signature threading
  cancel.go                  # MODIFIED — NewRunService signature threading
  status.go                  # MODIFIED — NewRunService signature threading
  serve.go                   # MODIFIED — NewRunService signature threading (Engine built from stores, passed as resumer)
  resume.go                  # NEW — pipeline resume <run-id> with --force
  render.go                  # MODIFIED — ResumeOutput type + renderResume
testdata/
  fixtures/
    failed_at_tts.sql        # NEW
    failed_at_write.sql      # NEW
  contracts/
    run.resume.response.json # NEW
migrations/                  # NO CHANGES (002 trigger already sufficient)
```

### Critical Constraints

- **No testify, no gomock.** Inline fake interfaces + `testutil.AssertEqual[T]`.
- **DELETE scope:** every segments DELETE must be `WHERE run_id = ?` — enforced by scope-isolation test.
- **Consistency check BEFORE cleanup.** Order is a correctness invariant, not a stylistic choice.
- **Phase A / HITL cleanup is a no-op**, not an error.
- **Artifact cleanup is idempotent.** `fs.ErrNotExist` swallowed.
- **`pipeline/` does not import `service/`.** Importing `db/` is allowed by the lint rules but deliberately skipped in this story — declare minimal store interfaces locally instead, for unit-test ergonomics.
- **Engine `Advance` stays a stub** — `return fmt.Errorf("advance not implemented: epic 3 scope")`. Do not attempt stage execution.
- **snake_case JSON** for all new fields (`confirm_inconsistent`, `warnings`).
- **Module path:** `github.com/sushistack/youtube.pipeline`. **CGO_ENABLED=0.**
- **`testutil.BlockExternalHTTP(t)` in every new test file.**
- **localhost-only** binding is already in serve.go; Resume path does not reopen this.
- **Cost, retries, per-stage 8-column observability are NOT this story's scope** — they belong to Story 2.4.

### References

- Epic 2 scope and FRs: [epics.md:378-399](../_bmad-output/planning-artifacts/epics.md#L378-L399)
- Story 2.3 AC (BDD): [epics.md:969-999](../_bmad-output/planning-artifacts/epics.md#L969-L999)
- NFR-R1 resume idempotency: [epics.md:86](../_bmad-output/planning-artifacts/epics.md#L86)
- Phase B segments DELETE + reinsert: [epics.md:129](../_bmad-output/planning-artifacts/epics.md#L129), [architecture.md:541-554](../_bmad-output/planning-artifacts/architecture.md#L541-L554)
- FS↔DB consistency at resume entry: [epics.md:153](../_bmad-output/planning-artifacts/epics.md#L153), [architecture.md:1738-1744](../_bmad-output/planning-artifacts/architecture.md#L1738-L1744)
- Per-run artifact directory tree: [architecture.md:793-822](../_bmad-output/planning-artifacts/architecture.md#L793-L822)
- Data Boundary Integrity (Resume 3-step recipe): [architecture.md:1736-1744](../_bmad-output/planning-artifacts/architecture.md#L1736-L1744)
- Runner interface (already declared in 2.1): [internal/pipeline/runner.go](../../internal/pipeline/runner.go)
- State machine Stage / Event / Status constants: [internal/domain/types.go:1-109](../../internal/domain/types.go#L1-L109)
- NextStage + StatusForStage + IsHITLStage: [internal/pipeline/engine.go](../../internal/pipeline/engine.go)
- Existing RunStore (extend, don't rewrite): [internal/db/run_store.go](../../internal/db/run_store.go)
- Existing RunService (extend, don't rewrite): [internal/service/run_service.go](../../internal/service/run_service.go)
- Resume handler 501 stub to replace: [internal/api/handler_run.go:127-129](../../internal/api/handler_run.go#L127-L129)
- Existing waiting-state fixture pattern: [testdata/fixtures/paused_at_batch_review.sql](../../testdata/fixtures/paused_at_batch_review.sql)
- Migration 002 trigger: [migrations/002_updated_at_trigger.sql](../../migrations/002_updated_at_trigger.sql)
- domain.Classify error taxonomy: [internal/domain/errors.go](../../internal/domain/errors.go)
- testutil.NewTestDB: [internal/testutil/db.go](../../internal/testutil/db.go)
- testutil.LoadRunStateFixture: [internal/testutil/fixture.go](../../internal/testutil/fixture.go)
- testutil.BlockExternalHTTP: [internal/testutil/nohttp.go](../../internal/testutil/nohttp.go)
- Layer lint script: [scripts/lintlayers/main.go](../../scripts/lintlayers/main.go)
- Sprint review checkpoints (scope isolation, order, idempotency): [sprint-prompts.md:274-288](../_bmad-output/planning-artifacts/sprint-prompts.md#L274-L288)
- Deferred work registry: [deferred-work.md](deferred-work.md)
- Previous story (2.2): [2-2-run-create-cancel-inspect.md](2-2-run-create-cancel-inspect.md)
- Previous story (2.1): [2-1-state-machine-core-stage-transitions.md](2-1-state-machine-core-stage-transitions.md)

## Review Findings

### [x] [Review][Patch] HIGH — Non-transactional Engine.Resume orchestration [internal/pipeline/resume.go:117-136]

The 4-step sequence `CleanStageArtifacts → DeleteByRunID → SetStatus → IncrementRetryCount` spans filesystem + multiple DB statements without any transaction. A failure after step 1 (e.g. SetStatus succeeds but IncrementRetryCount fails, or context cancellation mid-cleanup) leaves torn state: disk cleaned, segments wiped, but `runs.status` still `failed` — or worse, status reset without retry_count increment. Merge `SetStatus`+`IncrementRetryCount` into a single UPDATE (`SET status=?, retry_reason=NULL, retry_count = retry_count + 1`). Filesystem is inherently non-transactional — accept that, but collapse the two DB round-trips. (Blind+Edge merged.)

### [x] [Review][Patch] HIGH — Phase B resume cleans only the failed stage's directory, but DELETEs both Phase B segments [internal/pipeline/artifact.go:24-27 + internal/pipeline/resume.go:121-128]

When Phase B fails at `tts`, CleanStageArtifacts removes `tts/` only — but the engine then DELETEs every segments row including ones that reference `images/scene_NN/shot_*.png`. The images directory is left on disk as orphan files. Symmetrical problem when failure is at `image`: `tts/` survives with no DB pointers. Disk leaks grow each Phase B retry. Fix: for Phase B stages, clean BOTH `images/` AND `tts/` (the clean-slate boundary is the entire phase, not just the failed track).

### [x] [Review][Patch] MED — AC-CONFIRM-FLAG warnings payload never populated [internal/pipeline/resume.go:85-141 + cmd/pipeline/render.go:99 + internal/api/handler_run.go:138-156]

`ResumeOutput.Warnings` and the contract fixture's `warnings: []` field exist structurally, but no mismatch data ever flows back to callers. Engine discards the `InconsistencyReport` after logging. `service.Resume` signature returns only `(*domain.Run, error)`. CLI `--json` omits warnings (omitempty + always nil). API response has no `warnings` key. Fix: return the report from `ResumeWithOptions` (or surface via a service.Resume return), plumb through to `ResumeOutput.Warnings` and the API envelope.

### [x] [Review][Patch] MED — Non-Phase-B resume leaves stale DB paths pointing to deleted files [internal/pipeline/artifact.go:28-32 + internal/pipeline/resume.go:120-128]

`CleanStageArtifacts(StageAssemble)` removes `clips/` + `output.mp4` but does NOT null the corresponding `segments.clip_path` column. Next `CheckConsistency` will flag every scene as `missing_file` forever. Same category risk for `metadata_ack` (metadata.json/manifest.json paths are not tracked in segments, so lower impact). Fix: after a non-Phase-B cleanup that removes per-segment files, null the matching columns (e.g. `UPDATE segments SET clip_path = NULL WHERE run_id = ?` on assemble resume).

### [x] [Review][Patch] MED — HTTP Resume decodes body with no size limit, no strict field check, silent malformed handling [internal/api/handler_run.go:141-145]

`json.NewDecoder(r.Body).Decode(&body)` ignores its error — a client sending `{"force": true}` (typo for `confirm_inconsistent`) silently falls back to `false` and the user sees a confusing "validation failed" response. No `http.MaxBytesReader` bound (multi-GB POST is accepted). No `DisallowUnknownFields`. Fix: wrap body with `http.MaxBytesReader(w, r.Body, 1<<16)` + `dec.DisallowUnknownFields()`; accept a truly empty body (len=0) as valid default but reject malformed or oversized payloads with 400. Mirror in `Create` handler.

### [x] [Review][Patch] MED — `run.Stage.IsValid()` not checked before resume orchestration [internal/pipeline/resume.go:85-89]

A manually-corrupted row with `stage='foobar'` passes `validateResumable` (which checks Status only). Downstream consistency logic treats unknown stages as post-Phase-A (via `isPostPhaseA` returning true for the unknown default), produces a misleading "scenario.json missing" mismatch, and `StatusForStage('foobar')` silently returns `StatusRunning` per a documented domain quirk. Fix: guard `if !run.Stage.IsValid() { return ErrValidation with message }` immediately after Get.

### [x] [Review][Patch] MED — `stage=complete` combined with `status=failed` is treated as resumable [internal/pipeline/resume.go:143-159]

`validateResumable` permits any run with `status ∈ {failed, waiting}` regardless of Stage. A row with `stage=complete/status=failed` (e.g. a crash mid-prior-SetStatus) flows through: cleanup is no-op, `StatusForStage(complete)` = `StatusCompleted`, retry_count increments on a "completed" row. Fix: reject `stage=complete` at the top of validateResumable with `ErrConflict: run already complete`.

### [x] [Review][Patch] MED — ScenarioPath empty string bypasses consistency check [internal/pipeline/consistency.go:75-85 + internal/db/run_store.go:scanRun]

`sql.NullString{Valid:true, String:""}` produces `run.ScenarioPath = &""`, which joins to exactly `runDir`. `runDir` always exists → no mismatch flagged, even though `scenario.json` truly is missing. Fix: in CheckConsistency, treat `*run.ScenarioPath == ""` as equivalent to `nil` (fall through to "expected scenario.json at runDir/scenario.json").

### [x] [Review][Patch] MED — Path traversal via segment paths not sandboxed to runDir [internal/pipeline/consistency.go:114-119]

`resolvePath` accepts both absolute paths and relative `../` paths without verifying the result is inside `runDir`. Exploit impact is limited to stat probing (file-existence info-disclosure), not write — CleanStageArtifacts uses fixed subpaths. Still a defense-in-depth gap. Fix: after `filepath.Clean(resolved)`, assert it's under `filepath.Clean(runDir)`; otherwise record a `suspicious_path` mismatch.

### [x] [Review][Patch] MED — runDir missing produces per-file noise instead of one diagnostic [internal/pipeline/consistency.go:24-29]

If the run's output directory was deleted, every segment path + scenario.json + every shot becomes a separate `missing_file` mismatch — potentially hundreds of lines. Fix: Stat runDir first; if missing, return a single `run_directory_missing` mismatch and skip per-file iteration.

### [x] [Review][Patch] MED — Dead layers: `SetStage` + `service.SegmentStore` [internal/db/run_store.go:177-194 + internal/service/run_service.go:24-28]

`RunStore.SetStage` method and its 2 tests were added "for forward-compat" but no caller invokes it. `service.SegmentStore` interface is declared but never referenced by any service method (segments are wired directly from `db` into `pipeline.Engine` via the CLI). Both are dead layers per project principle. Fix: delete both — they add maintenance burden without consumers. If Story 2.4 needs them, introduce when the consumer lands.

### [x] [Review][Patch] MED — Test fakes silently return nil for unknown IDs [internal/pipeline/resume_test.go:45-75]

`fakeRunStore.SetStatus` and `fakeRunStore.IncrementRetryCount` return `nil` when `f.run == nil || f.run.ID != id`, while the real `*db.RunStore` returns `ErrNotFound`. This contract drift hides a class of bugs. Fix: fake returns `domain.ErrNotFound` when the run does not match, matching real store semantics.

### [x] [Review][Patch] LOW — Idempotency integration-level coverage missing [internal/pipeline/resume_integration_test.go]

`TestResume_Idempotent` is fake-backed unit-level. `TestIntegration_Resume_FailedAtTTS` runs Resume only once against the fixture. AC-IDEMPOTENCY-NFR-R1 text specifies integration coverage. Fix: extend the integration test to re-set `status=failed` after the first Resume and call again, asserting identical terminal state against real SQLite + real filesystem.

### [x] [Review][Defer] Permission-denied mid-CleanStageArtifacts leaves partial disk state [internal/pipeline/artifact.go:43-48] — inherent FS limitation; re-run Resume after permission fix completes cleanup (idempotent). V1 accept.

### [x] [Review][Defer] Context cancellation not honored inside `os.RemoveAll` [internal/pipeline/artifact.go + internal/pipeline/resume.go] — stdlib constraint; documented torn-state risk ties into the atomicity patch. Covered by the HIGH-severity atomicity patch.

### [x] [Review][Defer] `scanEpisode` fails completely on malformed shots JSON [internal/db/segment_store.go:86-89] — V1 acceptable; row becomes unrecoverable until manual intervention. Graceful-skip semantics deferred to V1.5.

### [x] [Review][Defer] DB lock under concurrent `pipeline serve` + `pipeline resume` [cmd/pipeline/resume.go + internal/db/db.go] — single-operator tool; busy_timeout already applied at OpenDB. Low real impact.

### [x] [Review][Defer] Reload error after successful engine mutation silently presents as resume-failure [internal/service/run_service.go:111-114] — subtle semantic question: client sees error but state was committed. Retry produces ErrConflict. Acceptable for V1 single-operator; add "idempotent acknowledgment" semantics in a future revision.

### [x] [Review][Defer] `isPKCollision` string-match fragility [internal/db/run_store.go:93-102] — pre-existing from Story 2.2; out of scope.

### [x] [Review][Defer] `Create` output-dir rollback not strictly atomic [internal/db/run_store.go:75-88] — pre-existing from Story 2.2; best-effort `os.Remove` documented.

### [x] [Review][Defer] `Cancel` rejects `StatusPending` runs [internal/db/run_store.go:215-224] — pre-existing product decision from Story 2.2.

## Dev Agent Record

### Agent Model Used

claude-opus-4-7

### Debug Log References

None.

### Completion Notes List

- `internal/domain/resume.go` (new): `Mismatch` struct + `InconsistencyReport` aggregator with `Error()` method that wraps cleanly via `fmt.Errorf("%w: %s", ErrValidation, report.Error())`. Kept out of `types.go` to keep resume-specific types discoverable.
- `internal/db/segment_store.go` (new): `SegmentStore` with `ListByRunID` + `DeleteByRunID`. `DELETE` uses the sole `WHERE run_id = ?` predicate; scope-isolation is asserted by `TestSegmentStore_DeleteByRunID_ScopeIsolation` with two runs. `scanEpisode` helper decodes `shots` JSON TEXT column into `[]domain.Shot`. The runs FK requires the run row to exist before segments can be inserted — tests seed `runs` first.
- `internal/pipeline/artifact.go` (new): `CleanStageArtifacts(runDir, stage)` hard-codes the per-stage directory/file mapping. Uses `os.RemoveAll` for dirs, `os.Remove` for files (swallowing `fs.ErrNotExist` for idempotency). Phase A, pending, complete, and HITL stages are no-ops.
- `internal/pipeline/consistency.go` (new): `CheckConsistency(runDir, run, segments)` returns a non-nil `*InconsistencyReport` even when clean. Checks `tts_path`, `clip_path`, `shots[].image_path`, plus `scenario.json` presence/absence expectations tied to `isPrePhaseA` / `isPostPhaseA`. Relative paths resolved against `runDir`.
- `internal/pipeline/resume.go` (new): `Engine` + `NewEngine` + `Advance` stub + `Resume` / `ResumeWithOptions`. Orchestration order is strict: load → validate state (`failed` or `waiting`) → list segments → consistency check → abort-on-mismatch unless `Force` → cleanup → Phase B segments DELETE → `SetStatus(StatusForStage(stage), nil)` → `IncrementRetryCount`. `pipeline` package declares its own minimal `RunStore` / `SegmentStore` interfaces locally — `db.RunStore` / `db.SegmentStore` satisfy them structurally. `pipeline` does not import `service` or `db`.
- `internal/db/run_store.go` (extended): added `SetStatus`, `SetStage`, and `IncrementRetryCount`. All three map `RowsAffected()==0` to `ErrNotFound`. `SetStatus` accepts a nullable `*string` for `retry_reason` (nil → SQL NULL). `SetStage` was added for forward-compat even though V1 Resume leaves `stage` unchanged.
- `internal/service/run_service.go` (extended): `RunStore` interface unchanged. Added `SegmentStore` interface for centralized wiring + `Resumer` 1-method interface (`ResumeWithOptions`). `NewRunService` signature changed to `NewRunService(store RunStore, resumer Resumer)` — `resumer` may be nil for CLI paths that don't resume (create/cancel/status). `Resume(ctx, id, force)` delegates to the engine, then reloads via `store.Get`.
- `internal/api/handler_run.go` (modified): Resume 501 stub replaced with real implementation that decodes optional `{"confirm_inconsistent": bool}` body and delegates to `svc.Resume`. Nil resumer surfaces as `ErrValidation` → HTTP 400 (no panic).
- `cmd/pipeline/resume.go` (new): Cobra `resume <run-id> [--force]` command. Builds DB → `RunStore` → `SegmentStore` → `pipeline.Engine` (with `clock.RealClock{}`) → `RunService` with the engine injected, then calls `svc.Resume(ctx, runID, force)` and renders `ResumeOutput`.
- `cmd/pipeline/serve.go` (modified): `runServe` now constructs the `pipeline.Engine` and threads it into `NewRunService` so the `POST /api/runs/{id}/resume` endpoint has a resumer at request time.
- `cmd/pipeline/create.go` / `cancel.go` / `status.go` (modified): pass `nil` resumer to `NewRunService` (these commands never resume). Keeps the constructor signature consistent without over-wiring.
- `cmd/pipeline/render.go` (modified): added `ResumeOutput{Run, Warnings}` type + `renderResume` method on `HumanRenderer` + type-switch entry.
- `cmd/pipeline/main.go` (modified): registered `newResumeCmd()` alongside the other subcommands.
- `testdata/fixtures/failed_at_tts.sql` + `testdata/fixtures/failed_at_write.sql` (new): Phase B / Phase A failure seeds used by integration tests.
- `testdata/contracts/run.resume.response.json` (new): contract snapshot consumed by `TestContract_RunResumeResponse`.
- Unit coverage: `domain/resume_test.go`, `db/segment_store_test.go`, `pipeline/artifact_test.go`, `pipeline/consistency_test.go`, `pipeline/resume_test.go` (fake stores + 12 scenarios including idempotency, scope isolation, and state conflicts). Integration coverage: `pipeline/resume_integration_test.go` using both fixtures, verifies tt dir removal, segments=0, scenario.json preservation, retry_count increment, and updated_at trigger.
- **NFR-R1 idempotency:** explicitly tested in `TestResume_Idempotent` — stage, status, segment count, and file tree snapshots match across two sequential resumes. `retry_count` legitimately drifts (+1 each call) and is documented as NOT part of NFR-R1.
- **DELETE scope isolation:** `TestSegmentStore_DeleteByRunID_ScopeIsolation` seeds two runs (`scp-049-run-1`, `scp-096-run-1`) with 3 segments each, DELETEs one, and asserts the other's 3 segments remain.
- **Consistency-before-cleanup ordering:** `TestResume_InconsistencyWithoutForce_Aborts` asserts that `SetStatus`, `IncrementRetryCount`, and `DeleteByRunID` are never called when consistency fails without `--force`.
- All tests pass: `go build ./...` clean, `go test -count=1 ./...` 100% green across `domain`, `db`, `pipeline`, `service`, `api`, `cmd/pipeline`, `testutil`, `lintlayers`, `frcoverage`, `clock`, `config`. `make lint-layers`: OK.
- No changes required to `scripts/lintlayers/main.go` — existing rules already permit `pipeline → domain/db/llmclient/clock` and `service → domain/db/pipeline/clock`. Architecture had anticipated these edges.

### Change Log

- 2026-04-17: Story 2.3 implemented — stage-level Resume with FS↔DB consistency check, per-stage artifact cleanup, Phase B clean-slate segments DELETE, idempotent status reset + retry_count increment. `Runner` interface realized; CLI `pipeline resume --force` + API `POST /api/runs/{id}/resume` wired end-to-end.
- 2026-04-18: Addressed code review findings — 13 items resolved. Engine Resume signature now returns `(*domain.InconsistencyReport, error)` and plumbs warnings through service/API/CLI. `RunStore.ResetForResume` collapses status + retry_count mutation into a single UPDATE (removes torn-state window). Phase B resume now cleans BOTH image and tts directories (single-phase boundary). Assemble resume clears `segments.clip_path` via new `SegmentStore.ClearClipPathsByRunID`. Engine guards: `Stage.IsValid()` + explicit reject of `stage=complete`. Consistency hardening: runDir-missing short-circuit (gated on expected artifacts), empty `ScenarioPath` treated as nil, path-sandbox check for recorded paths that escape `runDir`. HTTP body guards: `MaxBytesReader(64KB)` + `DisallowUnknownFields` on Create and Resume. Dead layers removed: `RunStore.SetStage` method and `service.SegmentStore` interface. Fake stores in tests now mirror real-store contracts (ErrNotFound on missing IDs). Integration-level idempotency test added (`TestIntegration_Resume_IdempotentAgainstRealStores`).

### File List

- internal/domain/resume.go (new)
- internal/domain/resume_test.go (new)
- internal/db/segment_store.go (new)
- internal/db/segment_store_test.go (new)
- internal/db/run_store.go (modified — SetStatus, SetStage, IncrementRetryCount)
- internal/db/run_store_test.go (modified — new method tests + updated_at trigger test)
- internal/pipeline/artifact.go (new)
- internal/pipeline/artifact_test.go (new)
- internal/pipeline/consistency.go (new)
- internal/pipeline/consistency_test.go (new)
- internal/pipeline/resume.go (new — Engine struct + Resume/ResumeWithOptions + local RunStore/SegmentStore interfaces)
- internal/pipeline/resume_test.go (new — unit tests with inline fakes)
- internal/pipeline/resume_integration_test.go (new — fixture-driven integration test)
- internal/service/run_service.go (modified — SegmentStore interface, Resumer interface, Resume method, NewRunService signature change)
- internal/service/run_service_test.go (modified — Resume tests)
- internal/api/handler_run.go (modified — real Resume implementation, confirm_inconsistent body)
- internal/api/handler_run_test.go (modified — Resume success/not-found/conflict/no-engine tests, contract test for run.resume.response.json)
- cmd/pipeline/resume.go (new)
- cmd/pipeline/serve.go (modified — engine wiring into RunService)
- cmd/pipeline/create.go (modified — NewRunService(store, nil))
- cmd/pipeline/cancel.go (modified — NewRunService(store, nil))
- cmd/pipeline/status.go (modified — NewRunService(store, nil))
- cmd/pipeline/render.go (modified — ResumeOutput + renderResume)
- cmd/pipeline/main.go (modified — register newResumeCmd)
- testdata/fixtures/failed_at_tts.sql (new)
- testdata/fixtures/failed_at_write.sql (new)
- testdata/contracts/run.resume.response.json (new)
