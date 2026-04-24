# Story 10.5: Data Export to JSON

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want to export per-run decisions and artifact data to JSON or CSV files on demand,
so that I can analyze run history externally and archive offline-readable snapshots.

## Acceptance Criteria

1. Given a completed run exists, when `pipeline export --run-id scp-049-run-3 --type decisions` is executed, then all decisions for that run are exported to `{output_dir}/scp-049-run-3/export/decisions.json`, the JSON matches the versioned envelope schema `{"version": 1, "data": [...]}`, and each decision row includes target item, decision type, timestamp, optional note, and `superseded_by` when the decision was undone.
2. Given `pipeline export --run-id scp-049-run-3 --type artifacts` is executed, when the export completes, then artifact metadata is exported as JSON and includes relative paths for `scenario.json`, image files, TTS files, `metadata.json`, and `manifest.json`.
3. Given `--format csv` is passed, when the export executes, then the same logical data is exported in CSV form.
4. Integration coverage validates the JSON envelope/schema, export-then-read behavior, and CSV correctness for both export types.

## Tasks / Subtasks

- [x] Task 1: Add the new `pipeline export` Cobra command and flag surface (AC: 1, 2, 3)
  - [x] Create a dedicated `cmd/pipeline/export.go` command instead of overloading `status`; treat the older PRD note about `pipeline status <run-id> --export decisions` as superseded by Epic 10 and the current user brief.
  - [x] Add required flags `--run-id` and `--type`, plus optional `--format` defaulting to `json`.
  - [x] Keep the existing global `--json` renderer behavior unchanged; this story writes export files on disk and may optionally report a small success payload through the renderer.
- [x] Task 2: Implement decisions export from the existing SSOT (AC: 1, 4)
  - [x] Reuse `internal/db.DecisionStore` read paths rather than inventing a second SQL layer.
  - [x] Export all run decisions needed for archival, including undone rows so `superseded_by` remains meaningful in the exported dataset.
  - [x] Define a stable export row shape with explicit fields for `decision_id`, `run_id`, `scene_id`/target item, `decision_type`, `created_at`, `note`, and `superseded_by`.
- [x] Task 3: Implement artifact metadata export from current run + segment stores (AC: 2, 4)
  - [x] Reuse `RunStore.Get`/`SegmentStore.ListByRunID` plus run-directory conventions to derive artifact metadata.
  - [x] Emit paths relative to the run output directory only; do not leak absolute filesystem paths into the export file.
  - [x] Include run-level artifacts (`scenario.json`, `output.mp4` if present, `metadata.json`, `manifest.json`) and per-scene artifacts from `shots[].image_path`, `tts_path`, and `clip_path`.
- [x] Task 4: Write file output into the per-run export directory (AC: 1, 2, 3)
  - [x] Create `{output_dir}/{run_id}/export/` idempotently.
  - [x] For JSON, write `decisions.json` or `artifacts.json` using the versioned envelope shape `{"version":1,"data":[...]}`.
  - [x] For CSV, write `decisions.csv` or `artifacts.csv` with deterministic column order and one row per exported record.
- [x] Task 5: Add tests at the command and store-integration seam (AC: 1, 2, 3, 4)
  - [x] CLI integration test for `--type decisions` JSON output path/content.
  - [x] CLI integration test for `--type artifacts` JSON output path/content.
  - [x] CSV tests that verify headers, relative paths, and row shape remain stable.
  - [x] Regression test proving undone decisions still carry `superseded_by` in export output.

## Dev Notes

- Epic 10 Story 10.5 is an operator-tooling story, so keep the implementation in Go CLI/backend layers only. No frontend work is required. [Source: `_bmad-output/planning-artifacts/epics.md#Story-105-Data-Export-to-JSON-FR44`, `_bmad-output/planning-artifacts/architecture.md#Operator-Tooling`]
- The architecture already defines the CLI-wide JSON envelope contract as `{"version": 1, "data": ...}`. Reuse that exact V1 shape for the exported JSON files; do not invent a second unversioned export format. [Source: `_bmad-output/planning-artifacts/prd.md` JSON envelope section]
- The `decisions` table is the SSOT for decisions history. Export should read from SQLite, not from status snapshots, web contracts, or derived summaries. [Source: `_bmad-output/planning-artifacts/prd.md` decisions log note, `internal/db/decision_store.go`]
- `DecisionStore.ListByRunID` currently excludes superseded rows because it serves live-state consumers. That is correct for HITL/session logic but incomplete for archival export. Story 10.5 should add a separate read path for export instead of changing `ListByRunID` semantics and breaking existing pause/timeline behavior. [Source: `internal/db/decision_store.go`, `internal/pipeline/hitl_session.go`, `internal/service/hitl_service.go`]
- Artifact metadata must be derived from the current run schema:
  - `runs.scenario_path`
  - `runs.output_path` when present
  - `segments.shots` JSON containing `image_path`
  - `segments.tts_path`
  - `segments.clip_path`
  - filesystem-known run-level files `metadata.json` and `manifest.json`
  [Source: `migrations/001_init.sql`, `migrations/011_output_path.sql`, `internal/domain/types.go`, `internal/db/segment_store.go`, `internal/pipeline/phase_c.go`]
- The export file paths in the artifact payload must be relative to the run output directory, not config-relative and not absolute. Preserve the run directory as the boundary contract from the architecture artifact tree. [Source: `_bmad-output/planning-artifacts/architecture.md` Artifact File Structure Convention]
- Keep path handling defensive. `RunService.Create` already validates SCP IDs to prevent path escape, and export should preserve that same posture when joining `{output_dir}/{run_id}/export`. Avoid any logic that can write outside the run directory. [Source: `internal/service/run_service.go`]
- The codebase already uses Cobra plus a renderer abstraction. Follow the existing command pattern from `status.go`, `metrics.go`, and `golden` commands: load config, open DB, build stores/services, render success/error via `newRenderer`. Cobra's current command/flag model still fits this approach; no dependency upgrade or CLI framework change is needed. [Source: `cmd/pipeline/main.go`, `cmd/pipeline/status.go`, https://pkg.go.dev/github.com/spf13/cobra]

### Implementation Guardrails

- Do not modify `DecisionStore.ListByRunID` to include superseded rows. That would regress HITL summaries, undo behavior, and snapshot diff logic that explicitly expect non-superseded live decisions.
- Do not store export blobs back into SQLite. FR44 is on-demand file export, not a new persistence feature.
- Do not add a new database table or migration for exports in V1.
- Do not emit absolute paths in exported artifact records; convert persisted absolute/relative values into run-relative paths before writing.
- Do not assume every run has every artifact:
  - `output.mp4`/`output_path` may be missing for runs that never assembled.
  - `metadata.json` and `manifest.json` only exist after Story 9.2 / `metadata_ack`.
  - some segments may have empty `shots`, nil `tts_path`, or nil `clip_path`.
- Make JSON and CSV represent the same logical dataset. CSV can flatten complex fields, but it must not silently drop rows or omit `superseded_by`.

### Suggested Data Shapes

`decisions` export row:

```json
{
  "decision_id": 42,
  "run_id": "scp-049-run-3",
  "scene_id": "0",
  "target_item": "scene:0",
  "decision_type": "reject",
  "created_at": "2026-04-24T12:34:56Z",
  "note": "needs rewrite",
  "superseded_by": 43
}
```

`artifacts` export row examples:

```json
{
  "artifact_type": "scenario",
  "scene_index": null,
  "path": "scenario.json"
}
```

```json
{
  "artifact_type": "image",
  "scene_index": 1,
  "shot_index": 0,
  "path": "images/scene_02/shot_01.png"
}
```

These shapes are guidance, not mandatory field names, but the final implementation must keep row-level fields explicit and CSV-friendly.

### Project Structure Notes

- Add the command under `cmd/pipeline/export.go`.
- Prefer a small export service or package-local helper over bloating `status.go`; the story is a new operator command, not a status variant.
- If a shared export helper is introduced, place it in backend Go packages (`internal/service` or `internal/db`) and keep dependency direction one-way:
  - DB stores fetch raw rows
  - service/export layer shapes records
  - cmd layer handles flags, file destination, and renderer output
- Reuse existing JSON marshal/write patterns already present in the repo for human-auditable files where appropriate (`json.MarshalIndent` with 2-space indentation is already used for Golden and compliance artifacts).

### Testing Requirements

- Command tests should cover:
  - missing `--run-id`
  - invalid `--type`
  - invalid `--format`
  - successful `decisions` JSON export
  - successful `artifacts` JSON export
  - successful CSV export for both types
- Export data tests should cover:
  - undone decision rows preserve `superseded_by`
  - `artifacts` export includes only run-relative paths
  - nil/missing artifact fields do not crash export
  - deterministic file names under `{output_dir}/{run_id}/export/`
- Keep fixtures lightweight. Reuse SQLite seed style already used in `internal/db/*_test.go` and CLI golden-style assertions already used in `cmd/pipeline/*_test.go`.

### Previous Story Intelligence

- Story 10.4 reinforced two patterns that apply directly here:
  - keep orchestration in CLI/YAML thin and push behavior into Go code
  - reuse existing truth sources instead of re-implementing business logic in shell glue
- Story 10.3 is also relevant: archived runs remain visible to status/history/exports even when artifact files are gone. Export code should therefore tolerate missing files and still emit metadata rows based on DB state when possible. [Source: `_bmad-output/implementation-artifacts/10-4-golden-shadow-ci-quality-gates.md`, `_bmad-output/implementation-artifacts/10-3-database-vacuum-data-retention.md`]

### Git Intelligence Summary

- Recent commits in this area are still story-document commits (`story 9-1` through `story 9-4`), so there is no newer implementation-specific export pattern to copy.
- Existing code conventions remain the authoritative guide: focused Cobra command files, store methods with tight query semantics, and renderer-mediated CLI output.

### Latest Technical Information

- Cobra continues to support the command-plus-local-flag pattern already used here, including child commands and local/non-persistent flags. The repo's current approach is still current and does not need a framework change for `pipeline export`. [Source: https://pkg.go.dev/github.com/spf13/cobra]

### Open Questions / Assumptions

- Assumption: `pipeline export` is a new top-level command, even though an older PRD sentence mentions `pipeline status <run-id> --export decisions`; Epic 10 AC and the user brief supersede that older shorthand.
- Assumption: decisions export should include superseded rows for archival completeness, while live status/timeline consumers keep their current semantics.
- Assumption: artifact export may include rows for files that are expected by metadata but currently absent on disk; if product wants strict "existing-files-only" behavior, decide that before implementation.
- Assumption: `output.mp4` should be included in artifact exports when present because `runs.output_path` exists and the output tree convention treats it as a first-class run artifact.

### References

- `_bmad-output/planning-artifacts/epics.md` — Story 10.5 acceptance criteria
- `_bmad-output/planning-artifacts/prd.md` — JSON envelope contract and FR44 context
- `_bmad-output/planning-artifacts/architecture.md` — Operator tooling, artifact tree, decisions schema
- `_bmad-output/planning-artifacts/ux-design-specification.md` — undo / `superseded_by` semantics
- `_bmad-output/implementation-artifacts/10-3-database-vacuum-data-retention.md` — archived runs remain exportable
- `_bmad-output/implementation-artifacts/10-4-golden-shadow-ci-quality-gates.md` — thin orchestration / reuse production code pattern
- `cmd/pipeline/main.go`
- `cmd/pipeline/status.go`
- `cmd/pipeline/render.go`
- `internal/config/loader.go`
- `internal/domain/config.go`
- `internal/domain/types.go`
- `internal/db/decision_store.go`
- `internal/db/run_store.go`
- `internal/db/segment_store.go`
- `internal/pipeline/hitl_session.go`
- `internal/service/hitl_service.go`
- `internal/service/run_service.go`
- `migrations/001_init.sql`
- `migrations/011_output_path.sql`
- https://pkg.go.dev/github.com/spf13/cobra

## Dev Agent Record

### Agent Model Used

GPT-5 Codex

### Debug Log References

- Skill: `bmad-create-story`
- Workflow: `/mnt/work/projects/youtube.pipeline/.agents/skills/bmad-create-story/workflow.md`
- Skill: `bmad-dev-story`
- Validation: `go test ./...`

### Completion Notes List

- Story context created from Epic 10, PRD, architecture, UX, previous story notes, and current Go codebase reality.
- Guardrails added for the biggest likely failure mode in this story: breaking live HITL logic by changing `DecisionStore.ListByRunID` semantics instead of adding an export-specific query path.
- Guardrails added for path hygiene, archived-run behavior, and JSON/CSV parity.
- Added a dedicated `pipeline export` Cobra command with `--run-id`, `--type`, and `--format` flags plus renderer-backed success/error output.
- Added export-specific backend seams: `DecisionStore.ListByRunIDForExport`, `RunStore.GetExportRecord`, and `ExportService` for JSON/CSV writing.
- Implemented run-relative artifact normalization for mixed absolute/relative stored paths and deterministic CSV column ordering.
- Added CLI, service, and DB coverage for decisions/artifacts export, JSON envelope output, CSV headers, and superseded decision preservation.

### File List

- `_bmad-output/implementation-artifacts/10-5-data-export-to-json.md`
- `cmd/pipeline/export.go`
- `cmd/pipeline/export_test.go`
- `cmd/pipeline/main.go`
- `cmd/pipeline/render.go`
- `internal/db/decision_store.go`
- `internal/db/decision_store_test.go`
- `internal/db/run_store.go`
- `internal/db/run_store_test.go`
- `internal/service/export_service.go`
- `internal/service/export_service_test.go`

### Change Log

- 2026-04-24: Implemented Story 10.5 `pipeline export` command, export service/storage seams, and JSON/CSV coverage; story moved to review.
- 2026-04-24: Code review fixes applied per `bmad-code-review` (Blind Hunter / Edge Case Hunter / Acceptance Auditor); story moved to done.

## Review Findings

Three adversarial reviewers (Blind Hunter on diff only, Edge Case Hunter on diff + project, Acceptance Auditor against AC/spec) produced 25 raw findings. After triage — deduplicating overlaps and discarding Blind Hunter claims that the diff did not support — 10 actionable findings remained. All validated findings were patched in this commit.

### Applied patches

1. **`superseded_by` and `scene_id` serialized as explicit `null` in JSON** — Severity: High (Acceptance Auditor #1 + #2). The spec-suggested JSON shape shows `"superseded_by": null` for non-superseded rows; `omitempty` on `*int64` / `*string` dropped the key entirely when nil, producing inconsistent envelope shape across rows. Removed `omitempty` from `ExportDecision.SceneID`, `ExportDecision.SupersededBy`, and `ExportDecision.Note` so downstream consumers see a stable key set. CSV behavior unchanged (empty string for nil).

2. **Regression test pins JSON null representation** — Severity: High (Acceptance Auditor #3). Previous tests unmarshalled into `*int64` so a missing key and `null` both produced `nil`, hiding the absence-vs-null distinction. Added `TestExportService_DecisionsJSON_EmitsExplicitNullForNonSuperseded` which asserts on raw JSON bytes that `"superseded_by": null` is present for the non-superseded row and `"scene_id": null` would be present for a run-level decision.

3. **`safeExportDirs` rejects `.`, `..`, leading `.`, and null bytes** — Severity: High (Blind Hunter #7 + Edge Case Hunter #2). Previous guard only rejected literal `..` substring and path separators. A `runID` of `"."` silently resolved to `exportDir = base/export` (top-level), clobbering unrelated run exports; `"scp\x00evil"` bypassed the substring checks entirely and produced a misleading `mkdir EINVAL`. Added explicit rejections for `.`, `..`, any leading `.`, and null-byte containment with a clear `invalid run_id` error.

4. **`writeCSVFile` captures and returns the `file.Close()` error** — Severity: Medium (Blind Hunter #5 + Edge Case Hunter #1). `defer file.Close()` silently discarded close failures that can indicate data loss on NFS/full-disk/buffered-flush paths. Switched to named return + explicit close-error capture that only overrides a nil error, preserving write-error primacy.

5. **Single `GetExportRecord` DB call across validation and artifact build** — Severity: Medium (Blind Hunter #6 + Edge Case Hunter #5). `Export()` did the existence check and discarded the result; `buildArtifactRows` re-queried. Plumbed the record through `buildArtifactRows(ctx, runID, runDir, run)` so the check is not duplicated and no TOCTOU window exists between the two lookups.

6. **`--run-id` and `--type` marked required in Cobra** — Severity: Medium (Blind Hunter #12). Missing flags previously reached the service layer and produced a validation error only after config load + DB open. Switched to `cmd.MarkFlagRequired(...)` so Cobra's usage-style error fires before any IO. Updated the existing `TestExportCmd_RejectsMissingRunID` assertion to match Cobra's required-flag message.

7. **Regression test for non-existent `--run-id`** — Severity: Medium (Acceptance Auditor #4). Added `TestExportCmd_RejectsNonexistentRunID` which seeds a valid DB then calls `export --run-id ghost-run --type decisions` and asserts a NotFound-style error surface.

8. **Regression test for runID guard (`.`, `..`, null byte)** — Severity: High (matches patch #3). Added `TestExportService_RejectsDangerousRunIDs` covering `.`, `..`, `foo/bar`, `foo\x00evil`, and leading-dot variants.

### Deferred (non-blocking)

- **Empty-string `scene_id` producing `TargetItem = "scene:"`** (Edge Case Hunter #4, severity Low). DB schema allows `scene_id = ''` but no code path writes that; guarded by producer invariants. Tracked in `_bmad-output/implementation-artifacts/deferred-work.md` as a future `NOT NULL OR != ''` CHECK constraint rather than a defensive read-path branch.
- **`shot.ImagePath = ""` silently skipped with no warning** (Edge Case Hunter #3, severity Low). Silent skip preserves tolerance for not-yet-generated shots; logging at Warn would require threading a logger into the service. Deferred.
- **Idempotent re-run test coverage** (Acceptance Auditor #5, severity Low). `os.WriteFile` / `os.Create` overwrite semantics are correct by inspection; adding an end-to-end re-run assertion is cosmetic. Deferred.
- **CSV-vs-JSON presence asymmetry for artifact `scene_index` / `shot_index`** (Acceptance Auditor #6, severity Low). `omitempty` is retained on `ExportArtifact` because run-level artifacts (scenario, output, metadata) genuinely have no scene. CSV emits empty strings for those columns. Spec allows this.
- **Blind Hunter findings #1–#4, #8 (`os.MkdirAll` / `writeDecisionExport` / `writeArtifactExport` / `writeJSONFile` / `writeCSVFile.WriteAll` errors ignored; `filepath.Rel` error unchecked)** were verified against the diff and rejected as false positives — all listed errors are in fact checked and returned at `export_service.go:117`, `:128-130`, `:138-140`, `:363-369`, `:381-386`, `:264-266`, `:285-287`.
- **`ExportEnvelope.Data` typed as `any`, `CreatedAt` raw string** (Blind Hunter #10, #11, severity Low). Design-by-choice, not bugs. No change.
