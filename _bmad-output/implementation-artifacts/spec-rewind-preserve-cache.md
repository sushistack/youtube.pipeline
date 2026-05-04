---
title: 'Uniform Phase A cache control across pending, rewind, and resume'
type: 'feature'
created: '2026-05-04'
status: 'done'
context: []
baseline_commit: '2582ec4a358f1348d29ed5c8e6be77e2da673d96'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The operator can already keep/drop deterministic-agent caches (`_cache/research_cache.json`, `_cache/structure_cache.json`) on the pending screen, but two adjacent re-entry paths to Phase A still bypass that control. (1) Scenario rewind force-removes `{runDir}/_cache/`, so even after the run lands at `pending`, the panel sees nothing. (2) Resume of a failed Phase A stage skips the panel entirely and silently reuses any cache present, with no UI to selectively wipe before re-run. Both gaps cost either redundant LLM spend (~$0.05–0.20/run) or stale outputs the operator cannot easily evict.

**Approach:** Make Phase A cache control uniform across every re-entry path to Phase A — pending start (existing), post-scenario-rewind start, and resume from a failed/cancelled Phase A stage. Stop wiping `_cache/` on scenario rewind; extend the existing `drop_caches` body pattern to `POST /api/runs/{id}/resume` so the operator can selectively wipe before re-execution; surface the existing cache panel UI on the failure banner whenever the failed stage is a Phase A entry stage. Cache fingerprint invalidation (`tryLoadCache`) remains the safety net for stale envelopes; this spec only adds a uniform operator-facing surface, not new validity logic.

## Boundaries & Constraints

**Always:**
- `scenario` rewind must still reset `run.stage` to `pending`, clear DB pointers, and remove every other artifact subtree (`scenario.json`, images, TTS, clips, output MP4, metadata, manifest, traces). Only the cache-dir flag changes.
- The Resume `drop_caches` body must be validated identically to Advance's: every entry must be a key of the existing `cacheFiles` map; an unknown stage is a 400 with no partial deletion; deletions happen synchronously between `PrepareResume` and the goroutine `ExecuteResume` dispatch so the engine sees a clean slate.
- Cache fingerprint invalidation (`tryLoadCache`) remains the safety net: if any fingerprint input shifted, the cached envelope is rejected on load, so preserving the dir is safe under all paths.
- The cache panel on the failure banner must only render when the failed stage is a Phase A entry stage (`isPhaseAEntryStage` set). Phase B/C failures do not consult `_cache/`, so showing the panel there would be misleading noise.

**Never:**
- Adding a request body field to `POST /api/runs/{id}/rewind`. The pending panel + this spec's failure-banner panel cover both selective-wipe surfaces; a rewind toggle would fork the decision.
- Changing rewind behavior for `character`, `assets`, or `assemble` nodes. They already preserve cache dir and re-enter post-Phase-A stages where caches are not consulted; this spec only adjusts `scenario`.
- Touching `tryLoadCache` / fingerprint logic. Validity guarantees are out of scope.
- Migrating existing runs whose `_cache/` is already gone. Forward-only.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Rewind scenario, cache dir present | `_cache/research_cache.json` + `_cache/structure_cache.json` exist | Both files survive rewind; scenario.json/traces/images/etc. removed; run → `pending` | — |
| Rewind scenario, cache dir absent | `_cache/` does not exist | Rewind succeeds, no error; run → `pending` | — |
| Rewind scenario, then advance with default body | cache files still on disk after rewind | Phase A cache hits both deterministic agents (no LLM calls for researcher/structurer) | — |
| Rewind scenario, then advance with `drop_caches=["research"]` | cache files on disk | research cache file removed sync before goroutine; structurer cache hits | — |
| Resume failed `write` stage with default body | cache files on disk; writer prompt unchanged | Phase A re-runs from `write`; researcher + structurer cache hit; writer re-runs | — |
| Resume failed `research` stage with `drop_caches=["research"]` | cache files on disk | research cache removed sync before goroutine; researcher re-runs fresh; structurer cache hits | — |
| Resume with `drop_caches=["typo"]` | any state | 400 VALIDATION_ERROR; no files deleted; no goroutine dispatch | reject whole request |
| Resume failed Phase B (image/tts) stage | cache files on disk; UI in failed state | Cache panel NOT rendered on failure banner (Phase A caches irrelevant); Resume button works as before, no body | — |
| Rewind character / assets / assemble | unchanged from current behavior | Cache dir preserved (unchanged), no panel surface needed | — |

</frozen-after-approval>

## Code Map

- `internal/pipeline/rewind_plan.go:255-275` -- `PlanRewind(StageNodeScenario)` sets `p.FSRemoveCacheDir = true`. Remove that line; update the doc comment to reflect the new contract.
- `internal/pipeline/rewind_plan_test.go:153-155` -- existing assertion that `FSRemoveCacheDir` is true for scenario rewind. Invert and add a non-cache-subtree positive assertion.
- `internal/pipeline/rewind_integration_test.go` -- exec-style rewind test. Add a case: prepopulate `_cache/research_cache.json` + `_cache/structure_cache.json`, run scenario rewind, assert both files survive.
- `internal/api/handler_run.go:351-399` -- `resumeRequest` struct + `Resume` handler. Add `DropCaches []string` field; reuse the existing `cacheFiles` validation + sync-delete pattern from `Advance` (lines 510-580). Place the deletion AFTER `PrepareResume` validates the run but BEFORE the `go func()` dispatch.
- `internal/api/handler_run_test.go` -- add tests mirroring the Advance drop_caches matrix for Resume (existing/missing/unknown stages).
- `web/src/lib/apiClient.ts:208-223` -- `advanceRun` accepts `{drop_caches}`. Mirror the same option shape in `resumeRun` (currently around line ~250+); send body only when the array is non-empty.
- `web/src/hooks/useRunCache.ts:15-23` -- gate currently `run_status === 'pending'`. Widen to also enable when `(run_status === 'failed' || run_status === 'cancelled')` AND the run's stage is a Phase A entry stage. Caller passes the stage so the hook can decide.
- `web/src/components/shared/FailureBanner.tsx` -- consume `useRunCache`; when the run is in failed/cancelled with Phase A entry stage AND there are cache entries, render the same row layout as ProductionShell's panel and pass `drop_caches` to `resumeRun`. Keep the keyboard shortcut for Resume working.
- `web/src/components/shared/FailureBanner.test.tsx` -- add tests: panel renders for failed Phase A stage with caches; panel does NOT render for failed Phase B stages; unchecking a row sends `drop_caches` body on Resume.
- `web/src/components/shells/ProductionShell.tsx:560-596` -- the existing pending-state panel + `dropped_cache_state` machinery. No change.

## Tasks & Acceptance

**Execution:**
- [ ] `internal/pipeline/rewind_plan.go` -- in the `case StageNodeScenario:` block, remove `p.FSRemoveCacheDir = true`. Update the doc comment to record the new contract: scenario rewind preserves `_cache/` so the pending-screen drop_caches panel sees the entries; fingerprint invalidation handles staleness automatically.
- [ ] `internal/pipeline/rewind_plan_test.go` -- invert the existing `if !plan.FSRemoveCacheDir { t.Errorf(...) }` assertion to its inverse with a comment citing fingerprint invalidation. Other scenario-rewind invariants (status reset, FSRemove* for non-cache subtrees, FSRemoveTracesDir) remain unchanged.
- [ ] `internal/pipeline/rewind_integration_test.go` -- add a fixture case: prepopulate `_cache/research_cache.json` and `_cache/structure_cache.json`, exercise scenario rewind, assert both files still exist on disk afterwards.
- [ ] `internal/api/handler_run.go` -- add `DropCaches []string` to `resumeRequest`; in `Resume`, validate every entry against `cacheFiles` (mirror Advance's loop and 400 message), then delete listed cache files synchronously between `PrepareResume` and the goroutine dispatch. Idempotent on missing files (mirror Advance).
- [ ] `internal/api/handler_run_test.go` -- add `TestRunHandler_Resume_DropCaches_*` covering existing/missing/unknown stages, mirroring the Advance matrix.
- [ ] `web/src/lib/apiClient.ts` -- extend `resumeRun(run_id, options?: { drop_caches?: string[] })`. Send body only when array is non-empty.
- [ ] `web/src/hooks/useRunCache.ts` -- widen the enabled gate to also include `(failed|cancelled)` runs whose stage is a Phase A entry stage. Add a `run_stage` parameter; the caller decides whether the stage qualifies (use a small exported helper `isPhaseAEntryStage(stage)` in `web/src/lib/runStages.ts` or similar — co-locate with existing stage helpers; mirror the backend list).
- [ ] `web/src/components/shared/FailureBanner.tsx` -- when `useRunCache` returns entries, render the same keep/drop row layout used by ProductionShell. Pass `drop_caches` on Resume click.
- [ ] `web/src/components/shared/FailureBanner.test.tsx` -- panel renders for Phase A entry stages with caches; panel does NOT render for Phase B/C stages; unchecking a row sends `{drop_caches: [...]}` body on Resume.

**Acceptance Criteria:**

- Given a run that completed Phase A and has both `_cache/research_cache.json` and `_cache/structure_cache.json` on disk, when the operator rewinds to `scenario`, then both files still exist on disk and the run's stage is `pending`.
- Given a fresh-restart scenario rewind, when the operator immediately calls `Advance` with no body, then Phase A reuses both cached envelopes (verified via the `agent cache hit` log line), no researcher/structurer LLM calls fire, and writer/critic/visual stages run fresh.
- Given a run failed at a Phase A entry stage with caches present on disk, when the failure banner renders, then the cache panel surfaces with the same row layout used on the pending screen, and the operator can keep or drop each cache independently.
- Given the operator unchecks the `research` row on the failure banner and clicks Resume, when the request fires, then the body is `{"drop_caches":["research"]}`, the research file is removed before the executor goroutine starts, and Resume re-runs the researcher fresh while reusing the structurer cache.
- Given a run failed at a Phase B (image/tts) or Phase C stage, when the failure banner renders, then the cache panel does NOT appear (Phase A caches are not consulted there).
- Given a Resume request body `{"drop_caches":["typo"]}`, when it is received, then the server returns 400 VALIDATION_ERROR, no cache file is deleted, and `ExecuteResume` is not dispatched.
- Given a `character`, `assets`, or `assemble` rewind, when it executes, then `_cache/` is preserved exactly as before this spec (no regression to non-scenario rewind paths).

## Spec Change Log

**2026-05-04 — Step-04 review patches (no spec amendment needed; all findings classified as `patch`).**

Findings applied as code patches (no frozen-block change):
- BH-2 / EC-2 / EC-8: `FailureBanner` mutation success now resets `dropped_cache_stages`, and the banner is keyed by `run.id` at its mount site so the keep/drop set cannot leak across runs.
- BH-1 / AA-3 / AA-4: Phase B failure-banner test now positively asserts the panel never renders AND that Resume still fires the legacy single-arg call (no body), matching the I/O matrix's Phase B row.
- BH-4 / EC-3: `Advance` and `Resume` drop_caches loops now abort with 500 on non-ENOENT delete errors instead of swallowing — the operator's drop intent must not be silently violated.
- EC-1: `Resume` rejects `drop_caches=["scenario"]` with 400. `scenario.json` is a Phase A output, not a deterministic-agent envelope; dropping it on a Phase B/C resume would silently corrupt downstream stages. Pending-start (`Advance`) still allows it because the run is restarting from zero.
- AA-2: Resume drop_caches "existing" test now seeds both `research` and `structure` envelopes and asserts the structure cache survives, pinning the per-row keep/drop contract.
- AA-5: `ProductionShell` now passes `current_run.stage` to `useRunCache` so the hook's failure-stage gate has the data it needs uniformly.

Findings deferred to `deferred-work.md` (pre-existing patterns or out-of-scope per `Always: Only the cache-dir flag changes`):
- EC-4 cancelRegistry race; EC-5 confirm_inconsistent not surfaced from apiClient; EC-6 retry_reason not cleared on rewind; EC-9 duplicate drop_caches dedup; EC-10 goroutine launched after cancel race; AA-1 end-to-end advance-after-rewind cache-hit assertion; AA-6 rewind-modal copy audit.

Findings rejected (verified false / explicitly out-of-scope per Boundaries):
- BH-3 (imports already present); BH-5 (`tryLoadCache` fingerprint coverage explicitly out-of-scope); BH-6 (Phase A entry stage list verified to mirror backend); BH-7 (test dead-code verify-only, no defect); EC-7 (defensive guard for unreachable invariant violation).

KEEP: the synchronous-deletion-before-goroutine pattern, the `cacheFiles` shared map between Advance and Resume validators, and the `CachePanel` component's `id_prefix` hook for surface isolation are load-bearing — must survive any future re-derivation.

## Design Notes

**Why this is one cohesive feature, not three:** the user goal is a single sentence — "operator controls Phase A cache at every re-entry to Phase A". Each surface is a different doorway to the same room. Splitting into three specs would force the same `cacheFiles` validation logic to appear in three commits, the same panel UI in two, and would leave at least one doorway shipped without the rest, which is the bug we are fixing.

**Why not add `drop_caches` to `/rewind` instead:** the rewind body would fire BEFORE the operator sees what caches exist. The pending and failure-banner panels render the live cache list (size + source_version + mtime), which is what lets the operator make an informed keep/drop choice. The body field would either duplicate that surface or force the operator to guess.

**Why fingerprint invalidation is sufficient as the safety net:** `FingerprintInputs` includes `PromptTemplateSHA`, `FewshotSHA`, `Model`, `Provider`, `SchemaVersion`, `SourceVersion`. Any drift across the rewind/resume window invalidates the envelope on load (`StalePromptTemplateChanged`, etc.), so a preserved `_cache/` cannot silently leak old outputs.

**Out of scope — UI copy:** the rewind confirmation modal may currently mention "Cache will be cleared." If it does, that copy is now misleading; update in a follow-up if `git grep -i "cache will\|캐시" web/src/` finds a confirmation modal hit.

## Verification

**Commands:**
- `go test ./internal/pipeline/...` -- expected: pass (inverted assertion + new exec assertion).
- `go test ./internal/api/...` -- expected: pass (new Resume drop_caches matrix; existing Advance/Resume tests untouched).
- `cd web && npm test -- --run` -- expected: pass (new FailureBanner panel tests + existing ProductionShell panel tests untouched).
- `go test ./...` -- expected: pass (full suite green).

**Manual checks:**
- Run pipeline through Phase A to scenario_review, rewind to scenario via UI. Verify `ls output/{run_id}/_cache/` still shows both cache files. Click Start run with both checkboxes checked → observe `agent cache hit` in audit.log for researcher and structurer.
- Force a Phase A failure (e.g. invalid prompt template); the failure banner appears with the cache panel; uncheck `research`, click Resume → researcher re-runs, structurer cache hits.
- Force a Phase B failure (image stage); the failure banner appears WITHOUT the cache panel; Resume button still works as before.
