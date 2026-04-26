---
workflowStatus: completed
totalSteps: 5
stepsCompleted:
  - step-01-detect-mode
  - step-02-load-context
  - step-03-risk-and-testability
  - step-04-coverage-plan
  - step-05-generate-output
lastStep: step-05-generate-output
nextStep: ''
lastSaved: '2026-04-25'
name: Epic 1–10 Test Design (Step 3)
description: Given/when/then + fixture + effort for the 18 scenarios scoped in quality-strategy-2026-04-25.md. Covers 8 pipeline E2E (Go) + 10 UI E2E (Playwright), test-data strategy (golden fixture, mock boundary, external API), and CI readiness.
type: test-design
mode: epic-level
date: 2026-04-25
project: youtube.pipeline
user: Jay
author: Murat (bmad-testarch-test-design)
upstream_input: _bmad-output/test-artifacts/quality-strategy-2026-04-25.md
downstream_input_for: bmad-qa-generate-e2e-tests (Step 4)
inputDocuments:
  - _bmad-output/test-artifacts/quality-strategy-2026-04-25.md
  - _bmad-output/implementation-artifacts/epic-1-10-retro-2026-04-24.md
  - _bmad-output/implementation-artifacts/sprint-status.yaml
  - testdata/golden/eval/manifest.json
  - testdata/golden/eval/000001..000003
  - internal/pipeline/e2e_test.go
  - web/playwright.config.ts
  - web/e2e/smoke.spec.ts
  - web/e2e/new-run-creation.spec.ts
  - e2e/playwright.config.ts
  - e2e/smoke.spec.ts
status: draft
---

# Test Design — Epic 1–10 (V1 Ship Readiness)

**Date:** 2026-04-25
**Author:** Murat (bmad-testarch-test-design), for Jay
**Status:** Draft
**Mode:** Epic-level (multi-epic, pre-V1 ship gate)

---

## Executive Summary

**Scope.** Concrete test-case design for the 18 scenarios scoped in [quality-strategy-2026-04-25.md](quality-strategy-2026-04-25.md) §5.1: 8 pipeline E2E (SMOKE-01..08, Go) + 10 UI E2E (UI-E2E-01..10, Playwright). Every case carries given/when/then, fixture requirements, external-API boundary, estimated runtime, and estimated implementation effort (range).

**Risk summary (inherited, not re-scored).**

- Total inherited high risks (score ≥ 6): **11** (see §2)
- Critical categories: **TECH** (Engine.Advance stub, text-LLM runtime missing, CI `continue-on-error`), **DATA** (metadata+manifest non-atomic, RetryExhausted 3-site mismatch), **OPS** (ffmpeg missing in CI, migration 004 collision)

**Coverage summary.**

| Priority | Pipeline E2E | UI E2E | Total | Effort (range) | Runtime (per run) |
|----------|--------------|--------|-------|----------------|-------------------|
| **P0**   | 5 (SMOKE-01..05) | 6 (UI-E2E-01..06) | **11** | **32–58 h** | **≤ 6 min (combined)** |
| **P1**   | 3 (SMOKE-06..08) | 4 (UI-E2E-07..10) | **7**  | **14–26 h** | **≤ 3 min (combined)** |
| **Total**| **8**        | **10**  | **18** | **46–84 h** (~6–11 dev-days, Jay solo) | **≤ 10 min (NFR-T1)** |

**Critical prerequisite.** Six of 11 P0 cases depend on one or more Step 2 CPs landing first (CP-1 Engine.Advance, CP-3 smoke repoint, CP-4 ffmpeg+go-version, CP-5 Story 10-2, AI-5 LoadShadowInput). Step 3 produces the *design*; Step 4 (`/bmad-qa-generate-e2e-tests`) cannot turn all P0 green until those CPs close. This is called out per scenario below.

**Top recommendation.** Implement SMOKE-02 (Phase A→B→C handoff, fixture-driven, 10× faster than SMOKE-01) **first** — it is orthogonal to CP-1 and unblocks Phase C regression signal immediately. SMOKE-01 follows once CP-1 lands.

---

## 1. Not in Scope

| Item | Reasoning | Mitigation |
|------|-----------|------------|
| Korean bag-of-words anti-progress FP rate | Known V1.5 embedding swap (retro P2); English fixtures in SMOKE-02 are sufficient for detector smoke | Post-V1.5 re-scope |
| Full 15-stage × mid-kill resume fuzz | SMOKE-03 covers the golden resume path; per-stage fuzz is follow-up, not ship-blocking | New story post-V1 — `Per-stage Resume Fuzz (×15)` |
| Phase B real-load rate-limit under ≥ 30 concurrent scenes | Single-operator usage; not V1 observable | Flag for k6-lite post-V1 (AI-6 adjacent) |
| Cross-browser (Firefox/WebKit) | Architecture decision: Chromium-only for V1 | Re-open post-V1 if Safari users report |
| Responsive 1024–1279 px, `prefers-reduced-motion` | UX-DR P2 per Step 2 §2; document and defer | Visual-regression pass post-V1 |
| Milestone banners (UX-DR57) | Step 2 P2 | Deferred to V1.5 |

---

## 2. Risk Assessment (Inherited from Step 2)

Full matrix lives in [quality-strategy-2026-04-25.md §3](quality-strategy-2026-04-25.md). Reproduced in compact form with each risk mapped to the scenario(s) in this design that exercise it.

### High-Priority Risks (Score ≥ 6) — BLOCK / MITIGATE

| ID | Category | Description | P×I | Mitigated by scenario | CP dependency |
|----|----------|-------------|-----|------------------------|---------------|
| R-01 | TECH | `Engine.Advance` stub — no cross-phase E2E exists | 3×3=9 | **SMOKE-01, SMOKE-03** | CP-1 |
| R-02 | TECH | Text-LLM runtime missing (DeepSeek/Gemini doc-only), FR12 Writer≠Critic unsatisfiable | 3×3=9 | **SMOKE-01, UI-E2E-07** (Tuning), fixture dep of all pipeline E2E | CP-5 Story 10-2 |
| R-03 | TECH | Story 10-2 tuning surface unbuilt | 3×3=9 | **UI-E2E-07** | CP-5 |
| R-04 | DATA | metadata.json + manifest.json non-atomic pair write | 2×3=6 | **SMOKE-05** (fault-injection, needs atomicity fix first) | Phase C hardening story |
| R-05 | TECH | FFmpeg not installed in CI `test-go` | 3×2=6 | **SMOKE-01, SMOKE-02** | CP-4 |
| R-06 | TECH | `e2e/smoke.spec.ts` root is `test.todo()`; CI `test-e2e` has `continue-on-error: true` | 3×2=6 | **UI-E2E-01** | CP-3 |
| R-07 | DATA | `LoadShadowInput` production-path wrong (`projectRoot` vs `{outputDir}/{runID}`) | 3×2=6 | **SMOKE-06** | AI-5 / Story 10-2 |
| R-08 | OPS | Migration 004 ordinal collision on fresh clones | 3×2=6 | Assumed resolved before Step 4 run (seed-state precondition) | Next migration-touching story |
| R-09 | DATA | `xfade` offset negative on shot < 0.5 s (silent FFmpeg UB) | 2×3=6 | Covered by `Phase C hardening` unit/integration; not in 18-case set | Phase C hardening |
| R-10 | DATA | `probeDuration = 0` silently passes tolerance | 2×3=6 | Covered by `Phase C hardening` unit; not in 18-case set | Phase C hardening |
| R-11 | DATA | `RetryExhausted` `>=`/`>` 3-site mismatch | 2×3=6 | **UI-E2E-05** (UI-observable) + property test (called out as P1 integration, not in 18) | AI-3 unification |

### Medium-Priority Risks (Score 3–5)

| ID | Category | Description | P×I | Mitigated by |
|----|----------|-------------|-----|--------------|
| R-12 | DATA | `BatchApprove` undo non-normalized `aggregate_command_id` | 2×3=6 | Server-side normalization + **UI-E2E-04** undo step |
| R-13 | PERF | `approved_scene_indices` O(N²) storage cliff | 1×5≈5 | Migration to `batch_commands`; **not in 18-case E2E** (unit + integration) |
| R-14 | OPS | Concurrent `pipeline export` races | 2×2=4 | **SMOKE-08** (round-trip) |
| R-15 | BUS | CSV injection via export | 2×2=4 | **SMOKE-08** (`=+-@` guard) |
| R-16 | TECH | `spa.go` serves `index.html` 200 for `/assets/*` misses | 2×3=6 | Covered by `UI-E2E-01` console-error gate + CP-3 fix |
| R-17 | BUS | FR23 compliance gate has zero E2E coverage | 2×3=6 | **SMOKE-07, UI-E2E-06** |

### Low-Priority Risks (Score ≤ 2) — DOCUMENT

Full list in Step 2 §3 P2. Not exercised by the 18-case E2E set; monitored only.

### Risk Category Legend

- **TECH**: integration, scalability, framework
- **SEC**: access control, data exposure
- **PERF**: SLA, degradation, resource limits
- **DATA**: loss, corruption, inconsistency
- **BUS**: UX harm, logic errors
- **OPS**: deployment, config, monitoring

---

## 3. Entry / Exit Criteria

### Entry Criteria (for Step 4 `/bmad-qa-generate-e2e-tests`)

- [ ] **CP-3 landed**: root `e2e/smoke.spec.ts` replaced OR CI repointed to `cd web && npx playwright test`; `test-e2e` `continue-on-error: true` removed (unlocks UI-E2E-01..10)
- [ ] **CP-4 landed**: `apt-get install ffmpeg` step in `.github/workflows/*test-go*`; `go-version` pinned to a real 1.25.x (unlocks SMOKE-01, SMOKE-02)
- [ ] **CP-1 landed**: `Engine.Advance` wired end-to-end; `TestE2E_FullPipeline` `t.Skip` removed (unlocks SMOKE-01, SMOKE-03)
- [ ] **CP-5 Story 10-2 landed**: DeepSeek adapter + tuning surface (unlocks SMOKE-06, UI-E2E-07)
- [ ] **AI-5 landed**: `LoadShadowInput` reads from `{outputDir}/{runID}` (unlocks SMOKE-06)
- [ ] Canonical fixture `testdata/e2e/scp-049-seed/` produced (see §6.1)
- [ ] Fault-injection adapter package `internal/pipeline/fi/` scaffolded (see §6.2)

### Exit Criteria

- [ ] All 11 P0 scenarios green
- [ ] All 7 P1 scenarios green OR waived by Jay with expiry
- [ ] P0 deferred-work items (Step 2 §3) all closed
- [ ] Full 18-case suite runtime ≤ **10 min** aggregate (NFR-T1)
- [ ] Zero `test.todo()` and zero `continue-on-error: true` in the entire e2e surface
- [ ] CI `test-go` FFmpeg probe green; `test-e2e` Playwright install probe green

---

## 4. Pipeline E2E Test Coverage (Go) — 8 scenarios

All pipeline E2E cases live under [internal/pipeline/](internal/pipeline/) — either in the existing [e2e_test.go](internal/pipeline/e2e_test.go) (SMOKE-01, SMOKE-03) or in new files (`smoke02_phase_handoff_test.go`, `smoke04_cost_cap_test.go`, etc.). Mock providers (`mockTextGenerator`, `mockImageGenerator`, `mockTTSSynthesizer`) are reused from [e2e_test.go:13-49](internal/pipeline/e2e_test.go#L13-L49).

### Test Level Selection Rationale

- **Go E2E** (drives the real pipeline binary/runner end-to-end, real SQLite WAL, real FFmpeg, real filesystem, mocked LLM/image/TTS at `domain.*Generator` interface seam): SMOKE-01, SMOKE-03 — cross-phase orchestration + resume correctness.
- **Go integration** (single-phase or narrow surface, fixture-driven, real DB + FS, mocked external I/O): SMOKE-02, SMOKE-04, SMOKE-05, SMOKE-06, SMOKE-07, SMOKE-08 — faster iteration, orthogonal to Engine.Advance wiring.

### SMOKE-01 — FR52-go Full Pipeline (P0)

| Field | Value |
|-------|-------|
| **Risk link** | R-01, R-02, R-05 |
| **Test level** | Go E2E |
| **File** | [internal/pipeline/e2e_test.go](internal/pipeline/e2e_test.go) — unskip `TestE2E_FullPipeline` + extend |
| **Runtime budget** | ≤ 120 s |
| **Effort** | **5–8 h** (assertions + fixture wiring + resume-state verification; *does not* include CP-1 wiring) |
| **Blocked by** | CP-1, CP-4, CP-5 Story 10-2, SD-4 |

**Given**

- `testdata/e2e/scp-049-seed/` contains raw corpus + per-stage recorded responses (DashScope text, DeepSeek text, DashScope image, DashScope TTS)
- `PIPELINE_ENV=test`, `testutil.BlockExternalHTTP(t)` active
- `t.TempDir()` as `outputDir`; empty SQLite DB migrated through latest
- Injected mocks: `mockTextGenerator` (writer+critic), `mockImageGenerator`, `mockTTSSynthesizer`

**When**

1. `runner.PipelineCreate(ctx, "scp-049", outputDir)` returns `runID`
2. `runner.PipelineResume(ctx, runID)` executes until `runs.stage == ready_for_upload` or error
3. No external HTTP call fires (guarded by `BlockExternalHTTP`)

**Then**

- All 15 stages in `runs.transitions` observed in canonical order
- Artifacts present at `{outputDir}/{runID}/`: `scenario.json`, `images/` (N scenes, one PNG each), `tts/` (N WAV), `output.mp4` (non-zero, FFmpeg-probed), `metadata.json`, `manifest.json`
- `scenario.json` validates against Phase-A schema; per-scene `segments.shots` has frozen descriptor verbatim; `duration_tolerance` within 0.1 s
- `runs.cost_usd > 0`, `runs.status == succeeded`, `runs.stage == ready_for_upload`
- `metadata.json` ↔ `manifest.json` internally consistent (scene count, SHA)

**Fixture**

- `testdata/e2e/scp-049-seed/raw.txt` (source corpus)
- `testdata/e2e/scp-049-seed/responses/text-writer.json`, `text-critic.json`, `images/*.png`, `tts/*.wav` (recorded mock outputs)
- `testdata/e2e/scp-049-seed/expected-manifest.json` (golden for assertion)

**Assertion strategy**

- Structural (file existence + shape) via `os.Stat` + JSON schema
- Semantic (cost, stage, descriptor propagation) via DB query helpers
- NOT byte-for-byte on MP4 (FFmpeg nondeterminism) — use `ffprobe` for duration + codec

---

### SMOKE-02 — Phase A → B → C Handoff (P0)

| Field | Value |
|-------|-------|
| **Risk link** | R-05 (ffmpeg); partial R-01 (independent of Engine.Advance wiring) |
| **Test level** | Go integration |
| **File** | `internal/pipeline/smoke02_phase_handoff_test.go` (new) |
| **Runtime budget** | ≤ 15 s |
| **Effort** | **3–5 h** |
| **Blocked by** | CP-4 only (ffmpeg) |
| **Priority rationale** | 10× faster than SMOKE-01, orthogonal to CP-1 — implement first |

**Given**

- Pre-baked `scenario.json` (output of Phase A for SCP-049) loaded from `testdata/e2e/scp-049-seed/scenario.json`
- Mocked `ImageGenerator` returns pre-generated PNGs; mocked `TTSSynthesizer` returns pre-generated WAVs
- Real `ffmpegBinary` resolved via PATH

**When**

1. Invoke `phase_b.Run(ctx, scenario, cfg)` to produce `segments`
2. Invoke `phase_c.Assemble(ctx, segments, cfg)` to produce `output.mp4` + metadata + manifest

**Then**

- Scene indices in `segments.shots` exactly match `scenario.scenes[*].shots`
- Shot count per scene preserved (no drop / no duplicate)
- `segments.shots[i].frozen_descriptor == scenario.scenes[i].shots[j].visual_descriptor` (verbatim propagation)
- `output.mp4` probe: duration ≈ Σ(scene durations) ± 0.1 s, container `mp4`, video codec `h264`, audio codec `aac`
- `metadata.json` and `manifest.json` both present, internally consistent

**Fixture**

- Reuses SCP-049 seed; no DB needed (pure function test on pre-built scenario)
- Pre-generated PNGs are 256×256 solid color (cheap to bundle; enough to exercise ffmpeg concat)

**Assertion strategy**

- Structural via JSON + `ffprobe`
- Negative control: intentionally drop one scene from scenario input → assert error (not silent)

---

### SMOKE-03 — Resume Idempotency (P0, NFR-R1)

| Field | Value |
|-------|-------|
| **Risk link** | R-01 |
| **Test level** | Go E2E |
| **File** | `internal/pipeline/smoke03_resume_test.go` (new) |
| **Runtime budget** | ≤ 60 s |
| **Effort** | **4–6 h** (kill-signal harness + diff helper) |
| **Blocked by** | CP-1, CP-4 |

**Given**

- SMOKE-01 baseline run completed to `runs.stage == phase_b` snapshot (seeded from DB fixture)
- Artifact directory partially populated (some images present, no TTS)
- Run record has `cost_usd > 0` reflecting Phase A cost

**When**

1. Invoke `runner.PipelineResume(ctx, runID)` a first time; interrupt via `ctx.Cancel()` mid-Phase-B
2. Re-invoke `runner.PipelineResume(ctx, runID)` to completion

**Then**

- Final DB state: no duplicate `runs.transitions` rows for Phase B; `cost_usd` is not double-counted (Phase A cost preserved, Phase B cost counted exactly once)
- Final FS state: images directory contains exactly N PNGs (old half-written images replaced, not appended); no stray tmp files
- `runs.status == succeeded`, `runs.stage == ready_for_upload`
- Resume log contains `"clean_slate"` event for Phase B

**Fixture**

- DB snapshot `testdata/e2e/snapshots/phase_b_mid.sql` (sqlite dump)
- FS snapshot `testdata/e2e/snapshots/phase_b_mid/` (partial artifacts)

**Assertion strategy**

- Direct SQL via `DB.QueryRow` for row counts + cost
- `cost_before == cost_after_first_resume` for Phase A lines; `Δ > 0` exactly once for Phase B
- `filepath.Walk` to count artifacts

---

### SMOKE-04 — Cost Cap Circuit Breaker (P0, NFR-C1/C2/C3)

| Field | Value |
|-------|-------|
| **Risk link** | Hardening of cost accumulator; no new R-* |
| **Test level** | Go integration |
| **File** | `internal/pipeline/smoke04_cost_cap_test.go` (new) |
| **Runtime budget** | ≤ 5 s |
| **Effort** | **2–3 h** |
| **Blocked by** | CP-1 |

**Given**

- `runs.cost_usd` seeded at `cost_cap_per_run - 0.001` USD
- Mock `TextGenerator` returns response with cost delta `0.01` USD
- `cost_cap_per_run = 0.50` USD (project default)

**When**

- Invoke next writer call via `runner.callWriter(ctx, runID, prompt)`

**Then**

- Returns `errors.Is(err, pipeline.ErrCostCapExceeded)`
- `runs.status == failed`, `runs.stage` does NOT advance
- `runs.cost_usd` reflects pre-call value (not incremented post-cap)
- `cli inspect runID` surfaces the escalation as a visible error state

**Fixture**

- In-memory DB seed via helper `fixture.SeedRunAtCost(t, db, runID, cost)`
- No filesystem dependency

**Assertion strategy**

- `errors.Is` equality on sentinel error
- Direct SQL assertions
- CLI output snapshot (JSON mode)

---

### SMOKE-05 — Metadata+Manifest Atomic Pair (P0, NFR-R3)

| Field | Value |
|-------|-------|
| **Risk link** | R-04 |
| **Test level** | Go integration (fault-injection) |
| **File** | `internal/pipeline/smoke05_metadata_atomic_test.go` (new) |
| **Runtime budget** | ≤ 10 s |
| **Effort** | **4–6 h** (FI adapter + atomicity fix verification) |
| **Blocked by** | Phase C hardening story (atomicity fix must land first; test codifies the fix) |

**Given**

- Fault-injecting `metadataWriter` that succeeds
- Fault-injecting `manifestWriter` that returns `errors.New("disk full simulated")` on first call
- Phase C completed Phase-B segments; video produced

**When**

- Invoke `phase_c.FinalizeMetadata(ctx, run, writers)`

**Then** (post-fix: staging-dir-rename or completed-marker)

- Either (a) both files present and internally consistent, OR (b) neither present
- NEVER: only `metadata.json` present without `manifest.json`
- Error returned from `FinalizeMetadata` is retriable (not wrapped in `ErrUnrecoverable`)
- On retry with non-faulting writer, both files present, identical content to a control run

**Fixture**

- `internal/pipeline/fi/faulting_writer.go` (new FI adapter package)
- No DB needed (pure FS test)

**Assertion strategy**

- `os.Stat` existence matrix (both / neither; never one)
- Retry idempotency: run twice, diff output bytes

---

### SMOKE-06 — Shadow Eval Against Live Run (P1)

| Field | Value |
|-------|-------|
| **Risk link** | R-07 |
| **Test level** | Go integration |
| **File** | `internal/pipeline/smoke06_shadow_live_test.go` (new) |
| **Runtime budget** | ≤ 10 s |
| **Effort** | **3–4 h** |
| **Blocked by** | AI-5 (`LoadShadowInput` path fix), CP-5 Story 10-2 (shadow runner) |

**Given**

- A completed Phase A run exists in the test DB with `runID = "shadow-probe-001"`
- Artifacts at `{outputDir}/shadow-probe-001/scenario.json`
- Shadow evaluator configured with a known critic prompt version

**When**

- Invoke `shadow.Run(ctx, runID, promptVersion)`

**Then**

- `shadow_results` row written with non-null `verdict ∈ {pass,retry}`
- `LoadShadowInput` succeeds (no "file not found" — the pre-fix failure mode)
- Diff view serializes `{before, after, delta}` for each scene
- `normalizeCriticScore` warning emitted if any score falls outside `[0,1]` (regression guard on deferred item)

**Fixture**

- Seeded DB + FS matching real production path layout
- Pre-recorded critic response JSON

**Assertion strategy**

- DB assertion on `shadow_results`
- Log capture via `slogtest` for clamp warning

---

### SMOKE-07 — Compliance Gate (P1, FR23)

| Field | Value |
|-------|-------|
| **Risk link** | R-17 |
| **Test level** | Go integration (handler-level) |
| **File** | `internal/server/handler_ack_test.go` (extend existing) |
| **Runtime budget** | ≤ 3 s |
| **Effort** | **2–3 h** |
| **Blocked by** | None |

**Given**

- Completed run with `runs.stage == phase_c_done` (post-metadata-ack pending)
- Authenticated test client

**When**

1. `POST /api/runs/{id}/upload` → expect 409 or 403
2. `POST /api/runs/{id}/ack-metadata` with valid body
3. `GET /api/runs/{id}/status`

**Then**

- Step 1 returns `409 Conflict` with body `{"error":"metadata_not_acknowledged"}`
- Step 2 returns `200` and `runs.stage == ready_for_upload`
- Step 3 reflects `ready_for_upload`
- `AcknowledgeMetadata` respects `MaxBytesReader` on body (P1 hardening regression guard)

**Fixture**

- DB-seeded run via helper; no FS artifacts needed beyond metadata.json stub

**Assertion strategy**

- HTTP status + JSON body match
- Before/after DB snapshot

---

### SMOKE-08 — Export Round-Trip + CSV Injection Guard (P1)

| Field | Value |
|-------|-------|
| **Risk link** | R-14, R-15 |
| **Test level** | Go integration |
| **File** | `internal/pipeline/smoke08_export_test.go` (new) |
| **Runtime budget** | ≤ 5 s |
| **Effort** | **2–3 h** |
| **Blocked by** | None |

**Given**

- Completed run `export-rt-001` with at least one scene whose narration starts with `=SUM(A1)` (CSV injection payload)
- Empty `exportDir`

**When**

1. `pipeline export --run export-rt-001 --json --csv --out exportDir` (first invocation)
2. Second identical invocation

**Then**

- First invocation: `run.json` + `run.csv` written
- CSV rows whose field starts with `=`, `+`, `-`, `@` are prefixed with `'` or wrapped in quotes per existing guard
- Second invocation: byte-identical `run.json` AND `run.csv` (round-trip idempotent; no timestamp drift inside files)
- Atomic publish: no `.tmp` detritus in `exportDir`

**Fixture**

- Seed DB with injection-payload narration
- Compare via `bytes.Equal`

**Assertion strategy**

- File digest (`sha256.Sum256`) before/after
- Regex guard on each CSV line starting with `[=+\-@]`

---

## 5. UI E2E Test Coverage (Playwright) — 10 scenarios

All UI cases live under [web/e2e/](web/e2e/). The config at [web/playwright.config.ts](web/playwright.config.ts) drives Chromium-only, `testDir: ./e2e`, port `4173`, `npm run serve:e2e` webServer. Existing patterns in [web/e2e/smoke.spec.ts](web/e2e/smoke.spec.ts) and [web/e2e/new-run-creation.spec.ts](web/e2e/new-run-creation.spec.ts) are the style template (`page.on('console','pageerror')` guards, `getByRole('alertdialog')`, `Continue to workspace` onboarding click).

### Test Level Selection Rationale

All 10 are Playwright E2E against the built SPA served by Go (or `serve:e2e`). API seams mocked via `page.route()` for deterministic backend state; zustand reset in a per-test fixture to avoid singleton bleed (deferred 6-5).

### Page-Object Scaffolds (shared across cases)

| Page object | File | Covers cases |
|-------------|------|--------------|
| `ProductionShell` | `web/e2e/po/production-shell.po.ts` | UI-E2E-01, 02, 03, 06, 09, 10 |
| `BatchReviewShell` | `web/e2e/po/batch-review.po.ts` | UI-E2E-04, 05 |
| `TuningShell` | `web/e2e/po/tuning.po.ts` | UI-E2E-07 |
| `SettingsShell` | `web/e2e/po/settings.po.ts` | UI-E2E-08 |

Effort for page objects: ~4 h, reused across all 10 — included in UI-E2E-01 (bootstrap).

---

### UI-E2E-01 — FR52-web SPA Smoke (P0)

| Field | Value |
|-------|-------|
| **Risk link** | R-06 |
| **Runtime** | ≤ 15 s |
| **Effort** | **4–6 h** (*includes page-object bootstrap reused by 02-10*) |
| **Blocked by** | CP-3 |
| **File** | [web/e2e/smoke.spec.ts](web/e2e/smoke.spec.ts) — replace root `e2e/smoke.spec.ts` todo; expand existing test |

**Given** a fresh SPA build served on `127.0.0.1:4173`
**When** `page.goto('/production')` and click `Continue to workspace`
**Then** `StatusBar`, `StageStepper`, `Sidebar`, `RunCard` all visible; zero `console.error`; zero `pageerror`; `/assets/*.js` 404 returns 404 (not 200 with `index.html` — regression on deferred 6-1).

**Fixture**: Seed a run via API `POST /api/runs` before `page.goto`. Mock `/api/runs` list response only if DB is empty.

---

### UI-E2E-02 — Dashboard → Scenario Inspector → Inline Narration Edit (P0)

| Field | Value |
|-------|-------|
| **Risk link** | Deferred 7-2 (InlineNarrationEditor baseline re-sync) |
| **Runtime** | ≤ 20 s |
| **Effort** | **3–4 h** |
| **Blocked by** | None |
| **UX-DR** | 26, 40 |

**Given** run `edit-001` with 3 scenes seeded, narration `"original-text"` for scene 1
**When**

1. Navigate `/production/edit-001`
2. Click scene 1 card → Inspector opens
3. Click `Edit narration` → type `"edited-text"` → blur
4. Reload page (`page.reload()`)
5. Re-open scene 1 Inspector
6. Press `Ctrl+Z`

**Then**

- Step 3: optimistic UI updates < 100 ms (measure via `performance.mark`)
- Step 4 post-reload: narration still `"edited-text"` (persisted to DB)
- Step 6: reverts to `"original-text"` AND baseline is `"edited-text"` (re-sync bug negative control — regression will *fail* this step)

**Fixture**: API mock `PATCH /api/runs/edit-001/scenes/1/narration` returns 200 with new body.

---

### UI-E2E-03 — Character Pick → Vision Descriptor → Freeze (P0)

| Field | Value |
|-------|-------|
| **Risk link** | Phase A→B handoff integrity (UX-DR 17, 41, 62) |
| **Runtime** | ≤ 15 s |
| **Effort** | **3–4 h** |
| **Blocked by** | None |

**Given** run `char-001` at stage `character_selection` with 5 candidate characters seeded
**When**

1. Navigate to Character Grid
2. Press `3` (keyboard shortcut select)
3. Expect `VisionDescriptorEditor` prefilled with character metadata
4. Click `Save frozen descriptor`

**Then**

- `frozen_descriptor` persists via `PATCH /api/runs/char-001/character` with selected character id
- Navigating away and back → same frozen descriptor shown (DB truth)
- Direct-select chord `1-9`, `0` all functional (table-driven test per candidate)

**Fixture**: API mocks for character candidates (`GET /api/runs/:id/characters`).

---

### UI-E2E-04 — Batch Review Full Keyboard Chord (P0)

| Field | Value |
|-------|-------|
| **Risk link** | AI-2; core HITL value |
| **Runtime** | ≤ 40 s |
| **Effort** | **6–10 h** (most complex UI test; fixture seeding + undo matrix) |
| **Blocked by** | None |
| **UX-DR** | 18, 23, 24, 33, 34, 38 |

**Given** run `batch-001` with 10 scenes in `batch_review` stage
**When** (chord sequence)

1. `J` 3 times (focus moves scene 1 → 2 → 3 → 4)
2. `K` 1 time (focus back to 3)
3. `Enter` (approve scene 3)
4. `J` → `Esc` (reject scene 4, open note prompt)
5. `J` → `Tab` (skip scene 5)
6. `J` → `S` (skip-and-remember scene 6)
7. `Shift+Enter` (batch-approve remaining: 1,2,7,8,9,10)
8. `Ctrl+Z` 3 times across mixed decisions

**Then**

- After step 3: scene 3 `decisions.action='approve'`
- After step 7: scenes 1,2,7,8,9,10 all `action='approve'` with the same `aggregate_command_id` (server-normalized — regression on deferred 8-6)
- After step 8: last 3 decisions in undo stack reverted in LIFO order, correct `decisions.superseded_at` set
- `Focus-Follows-Selection`: detail panel never empty at any key press (assert after each step)
- Optimistic UI updates < 100 ms per keystroke (perf mark)

**Fixture**: Seed DB with 10 scenes; API mocks for `POST /api/runs/:id/decisions` (echo server to observe `aggregate_command_id`).

**Assertion strategy**

- Per-step screenshot at failure (`test.step` wrapper)
- API mock spy validates `aggregate_command_id` identity across batch

---

### UI-E2E-05 — Rejection + Regeneration + Retry-Exhausted (P0)

| Field | Value |
|-------|-------|
| **Risk link** | R-11 (`>=`/`>` threshold unification) |
| **Runtime** | ≤ 20 s |
| **Effort** | **3–5 h** |
| **Blocked by** | AI-3 threshold unification |
| **UX-DR** | 39, 65 |

**Given** run `retry-001`, scene 1 with `retry_count=0`
**When**

1. Reject with inline note `"too dark"`
2. Wait for progress overlay → regeneration completes
3. Reject again (`retry_count=1`)
4. Reject third time (`retry_count=2`, at threshold `>=`)

**Then**

- After step 3: regeneration triggered, `retry_count=2`
- After step 4: state transitions to `retry_exhausted` (threshold is `>=` per AI-3 fix)
- `Manual edit` CTA becomes enabled, `Retry` CTA disabled with tooltip `"Retry budget exhausted"`
- `CountRegenAttempts` returns 3 (scope regression on deferred)

**Fixture**: API mock for regeneration returns deterministic response; DB spy on `retry_count`.

---

### UI-E2E-06 — ComplianceGate Ack → Ready for Upload (P0)

| Field | Value |
|-------|-------|
| **Risk link** | R-17 |
| **Runtime** | ≤ 15 s |
| **Effort** | **3–4 h** |
| **Blocked by** | AI-4 FR23 hard gate |
| **UX-DR** | 42, 66 |

**Given** run `gate-001` at stage `phase_c_done`
**When**

1. Navigate `/production/gate-001`
2. Observe `CompletionReward` banner
3. Click `Acknowledge metadata` button
4. Observe next-action CTA

**Then**

- Step 3 fires `POST /api/runs/gate-001/ack-metadata` (spy validates single call, idempotent on double-click)
- `runs.stage` transitions to `ready_for_upload` (observed via API polling or WS event)
- Next-action CTA text: `"Ready for upload"`, navigates to upload page
- Pre-ack: `Upload` CTA is disabled with tooltip `"Acknowledge metadata first"`

**Fixture**: DB-seeded run at `phase_c_done`; API mock echoes ack.

---

### UI-E2E-07 — Tuning Surface End-to-End (P1)

| Field | Value |
|-------|-------|
| **Risk link** | R-02, R-03 |
| **Runtime** | ≤ 45 s |
| **Effort** | **4–6 h** |
| **Blocked by** | CP-5 Story 10-2 FULL |
| **Covers** | Existing partial in [web/e2e/smoke.spec.ts](web/e2e/smoke.spec.ts#L55-L87) — expand |

**Given** `/tuning` loaded, critic prompt v1 exists
**When**

1. Edit critic prompt body to `"...v2 variant"` → `Save version`
2. Click `Run Golden` (synchronous, ≤ 15 s)
3. Observe `Shadow Eval` section transitions from disabled → enabled (AC-6 gate)
4. Click `Run Shadow` → observe diff view populated
5. Click scene 1 in diff → observe before/after narration

**Then**

- Step 1: `prompt_versions` DB row created with incremented `version`
- Step 2: Golden recall = 1.0 displayed; `Shadow` button enabled only AFTER Golden passes (AC-6 regression guard)
- Step 3: Shadow disabled state message cleared; previously-disabled element detectable via `toBeDisabled` → `toBeEnabled`
- Step 4: diff view shows `{pass, retry}` verdict counts matching `last_report`
- Zero console errors throughout

**Fixture**: Test DB seeded with 3 golden pairs (leverage existing `testdata/golden/eval/000001..000003`); mock DeepSeek critic endpoint via `page.route()`.

---

### UI-E2E-08 — Settings Save → Dynamic Phase-B Config (P1)

| Field | Value |
|-------|-------|
| **Risk link** | DF1 re-parse regression |
| **Runtime** | ≤ 20 s |
| **Effort** | **2–3 h** |
| **Blocked by** | None |

**Given** `/settings` loaded, `cost_cap_per_run = 0.50`
**When**

1. Change `cost_cap_per_run` to `1.00` → `Save settings`
2. Navigate to `/production` and start a new run (API level)
3. Observe Phase B executor logs

**Then**

- Step 1: `PUT /api/settings` 200 response; subsequent `GET /api/settings` returns `1.00`
- Step 3: Phase B log entry `cost_cap_per_run=1.00` (dynamic pick-up; regression if 0.50 persists = DF1 re-gressed)
- Model change (DashScope → DeepSeek) propagates to next run's writer identity

**Fixture**: Fresh settings DB; log capture via WS or `/api/logs` polling.

---

### UI-E2E-09 — Run Inventory Search + Filter (P1)

| Field | Value |
|-------|-------|
| **Risk link** | None — UX polish |
| **Runtime** | ≤ 15 s |
| **Effort** | **2–3 h** |
| **Blocked by** | None |
| **UX-DR** | 63 |

**Given** DB seeded with 5 runs: 2 `succeeded`, 1 `failed`, 1 `running`, 1 `paused`; names `alpha`, `beta`, `gamma`, `delta`, `epsilon`
**When**

1. Open Sidebar → type `alp` in search
2. Clear → filter stage = `phase_b`
3. Clear → filter status = `failed`

**Then**

- Step 1: only `alpha` RunCard visible
- Step 2: only runs at `phase_b` visible
- Step 3: only `failed` run visible; `RunCard` content scoped correctly
- Keyboard: `/` focuses search input (regression on shortcut contract)

---

### UI-E2E-10 — FailureBanner → Enter Resume (P1)

| Field | Value |
|-------|-------|
| **Risk link** | None — keyboard resume contract |
| **Runtime** | ≤ 15 s |
| **Effort** | **2–3 h** |
| **Blocked by** | None |
| **UX-DR** | 16 |

**Given** run `fail-001` with `runs.status == failed`
**When**

1. Navigate `/production/fail-001`
2. `FailureBanner` visible
3. Press `Enter` while banner focused

**Then**

- `POST /api/runs/fail-001/resume` fires
- `runs.status` transitions to `running`
- Banner replaced with active progress UI
- `Tab` reaches banner CTA in reasonable focus order (a11y regression guard)

---

## 6. Test Data Strategy

### 6.1 Canonical Seed — `testdata/e2e/scp-049-seed/`

Single canonical corpus drives SMOKE-01/02/03 and is the reference for UI page-object test DB seeds.

**Layout:**

```
testdata/e2e/scp-049-seed/
├── raw.txt                              # Source corpus (~800 words, "SCP-049 — The Plague Doctor")
├── scenario.json                        # Expected Phase-A output (pre-baked for SMOKE-02)
├── responses/
│   ├── text-writer.json                 # DashScope recorded response for writer step
│   ├── text-critic.json                 # DeepSeek recorded response for critic step (Writer≠Critic, per Jay 2026-04-25)
│   ├── images/
│   │   ├── scene-01-shot-01.png         # 256×256 solid-color test PNGs
│   │   └── ...
│   └── tts/
│       ├── scene-01.wav                 # 1-second silence WAV (small)
│       └── ...
├── expected-manifest.json               # Golden assertion target for SMOKE-01
└── snapshots/
    ├── phase_b_mid.sql                  # DB snapshot for SMOKE-03
    └── phase_b_mid/                     # Partial FS artifacts for SMOKE-03
```

**Volume:** ~5 MB total (PNGs cheap; WAVs are ≤ 20 KB each for silence).

### 6.2 Fault-Injection Adapters — `internal/pipeline/fi/` (NEW)

**Purpose:** SMOKE-05 and follow-up reliability fuzz.

```
internal/pipeline/fi/
├── faulting_writer.go        # io.Writer that errors after N bytes or on nth call
├── timeout_rate_limiter.go   # Wraps rate-limiter.Do with context deadline
├── partial_probe_ffmpeg.go   # ffprobe fake that returns duration=0
└── nonatomic_metadata.go     # Writer that succeeds first, fails second (pair-write fault)
```

**Testability impact:** adapters are constructor-injected; production code unchanged.

### 6.3 Golden Fixture — `testdata/golden/eval/`

**Current state** (verified): `manifest.json` v1, pairs 1–3, last recall 1.0, all verdicts correct. Pairs are Korean (SCP-173). Structure is stable; do **not** mutate existing pairs.

**Addition for UI-E2E-07:** add one pair 000004 crafted to **fail** critic (score `retry` but test expects `pass`, or vice versa) so the Shadow gate shows a non-trivial diff. Fixture regression-guards the Critic-file-path code.

**Invariants codified:**

- `last_refreshed_at` and `last_successful_prompt_hash` MUST be updated together (atomic pair)
- Test runs must use `--dry-run` (deferred 10-4) — never mutate real `manifest.json`

### 6.4 Mock Boundary (per Step 2 §5.4)

| Tier | Component | Real or mock in tests |
|------|-----------|-----------------------|
| External | DashScope text / image / TTS | **Mocked** at `domain.TextGenerator` / `domain.ImageGenerator` / `domain.TTSSynthesizer` interface |
| External | DeepSeek text (Critic) | **Mocked** at `domain.TextGenerator` — separate impl for Writer≠Critic fidelity |
| External | Gemini | Not used in V1 (deferred V1.5) |
| System | SQLite | **Real**, WAL mode, file-based (not `:memory:` — matches production) |
| System | Filesystem | **Real**, `t.TempDir()` |
| System | FFmpeg binary | **Real** — requires CP-4 CI install |
| Boundary | HTTP egress | **Blocked** via `testutil.BlockExternalHTTP(t)` (regression guard on deferred 1-4 mutex fix) |
| Clock | `time.Now` | **FakeClock** where rate-limit / retry timing matters |

### 6.5 External API Strategy — DashScope-only

**Durable rule** (per memory `feedback_api_dashscope_only.md`): real production uses DashScope for Qwen only; SiliconFlow never used. Tests follow the same boundary: **no real HTTP fires in any test under any mode**. Recorded DashScope/DeepSeek response JSONs in `testdata/e2e/scp-049-seed/responses/` are the only "external" surface. Refreshing a response is a manual ops task (document as `docs/test-fixture-refresh.md` in Step 4 scope).

### 6.6 CI Wiring Prerequisites (summary, details in Step 2 §5.5)

| # | Change | Status needed before Step 4 | Owner |
|---|--------|------------------------------|-------|
| 1 | Remove `test-e2e` `continue-on-error: true` | **P0** | CP-3 |
| 2 | Replace root `e2e/smoke.spec.ts` todo OR repoint CI to `cd web` | **P0** | CP-3 |
| 3 | `apt-get install -y ffmpeg` in `test-go` job | **P0** | CP-4 |
| 4 | Pin `go-version` to real 1.25.x | **P0** | SD-4 |
| 5 | Unskip `TestE2E_FullPipeline` | **P0** | CP-2 (after CP-1) |
| 6 | `fr-coverage.json` → strict mode after CP-5 | **P1** | AI-8 |

---

## 7. Execution Strategy (PR / Nightly / Weekly)

### PR (on every commit, `.github/workflows/test-go.yml` + `test-e2e.yml`)

- **SMOKE-02** (15 s — fastest, orthogonal to CP-1) — highest signal-per-second
- **SMOKE-04** (5 s) — cost accumulator regression cheap to catch
- **SMOKE-07** (3 s)
- **SMOKE-08** (5 s)
- **UI-E2E-01** (15 s) — smoke + console-error gate
- **UI-E2E-02** (20 s)
- **UI-E2E-06** (15 s)
- **UI-E2E-09, 10** (30 s combined)
- **Total PR budget:** ~110 s pipeline + ~100 s UI = **≤ 4 min** ✅ (fits NFR-T1)

### Nightly (cron, `.github/workflows/test-nightly.yml` — new)

- **SMOKE-01** (120 s) — full pipeline
- **SMOKE-03** (60 s) — resume idempotency
- **SMOKE-05** (10 s) — metadata atomicity
- **SMOKE-06** (10 s) — shadow live
- **UI-E2E-03, 04, 05** (75 s combined) — character, batch, retry
- **UI-E2E-07, 08** (65 s combined) — tuning, settings
- **Total nightly:** ~6 min ✅

### Weekly (optional, after V1 ship)

- Per-stage resume fuzz (×15 stages × mid-kill) — follow-up story, not in 18-case set
- Phase B real-load rate-limit (k6-lite) — AI-6 adjacent

---

## 8. Quality Gates

### Pass/Fail Thresholds

| Gate | Threshold | Enforcement |
|------|-----------|-------------|
| P0 pass rate | **100%** (no exceptions) | CI job fails → block merge |
| P1 pass rate | **≥ 95%** | Waiver by Jay with expiry date |
| Smoke runtime | **≤ 4 min** PR budget | Alert if drifts above 5 min |
| Full suite runtime | **≤ 10 min** (NFR-T1) | Alert if drifts above 12 min |
| High-risk mitigations (R-01..R-11) | **100%** closed before V1 tag | Checklist in release PR |
| Console errors / pageerror in UI-E2E | **0** (zero tolerance) | Assertion in every UI spec |
| External HTTP during test | **0** (`BlockExternalHTTP` in all Go tests) | Assertion panics test |

### Gate Decision Criteria

Inherited from Step 2 §5.6:

| Decision | Criteria |
|----------|----------|
| **PASS** | All 11 P0 scenarios green; all 12 P0 deferred items closed; no P1 OPEN without mitigation owner + deadline |
| **CONCERNS** | P0 green, but some P1 OPEN without owner; OR one P0 deferred item waived with expiry |
| **FAIL** | Any P0 scenario red; OR any P0 deferred item unresolved; OR `TestE2E_FullPipeline` / root `e2e/smoke.spec.ts` still skipped |
| **WAIVED** | All P0 items resolved; P1 items batch-waived by Jay with expiry (single-operator — Jay is the approver) |

---

## 9. Resource Estimates (Ranges Only)

### Per-scenario effort

| ID | Scenario | Effort (h) |
|----|----------|------------|
| SMOKE-01 | Full Pipeline | 5–8 |
| SMOKE-02 | Phase A→B→C | 3–5 |
| SMOKE-03 | Resume Idempotency | 4–6 |
| SMOKE-04 | Cost Cap | 2–3 |
| SMOKE-05 | Metadata Atomicity | 4–6 |
| SMOKE-06 | Shadow Live | 3–4 |
| SMOKE-07 | ComplianceGate | 2–3 |
| SMOKE-08 | Export Round-Trip | 2–3 |
| UI-E2E-01 | SPA Smoke (+PO bootstrap) | 4–6 |
| UI-E2E-02 | Inline Narration Edit | 3–4 |
| UI-E2E-03 | Character → Vision | 3–4 |
| UI-E2E-04 | Batch Review Chord | 6–10 |
| UI-E2E-05 | Retry Exhausted | 3–5 |
| UI-E2E-06 | ComplianceGate Ack | 3–4 |
| UI-E2E-07 | Tuning Surface | 4–6 |
| UI-E2E-08 | Settings Save | 2–3 |
| UI-E2E-09 | Inventory Search | 2–3 |
| UI-E2E-10 | FailureBanner Resume | 2–3 |
| **Total** | | **57–96** |

**Plus shared infra:**

| Infra | Effort (h) |
|-------|------------|
| Canonical seed `testdata/e2e/scp-049-seed/` (bundle + docs) | 3–5 |
| FI adapter package `internal/pipeline/fi/` | 3–4 |
| Playwright page objects (ProductionShell, BatchReviewShell, TuningShell, SettingsShell) | 4–6 |
| CI YAML diff (CP-3 + CP-4 + SD-4 consolidated) | 1–2 |
| Per-test zustand reset fixture (deferred 6-5 singleton) | 1–2 |
| Fixture refresh docs (`docs/test-fixture-refresh.md`) | 1 |
| **Subtotal** | **13–20** |

### Total

**70–116 hours ≈ 9–15 dev-days (Jay solo, realistic pace given existing deferred story load).**

Honest timeline note: Step 2 CPs (CP-1, CP-3, CP-4, CP-5) are **separate story work**, not included above. Expect 2–3 weeks real calendar between "Step 3 accepted" and "all P0 green in CI" once CP work starts.

---

## 10. Assumptions and Dependencies

### Assumptions

1. Jay operates solo; roles in template ("PM", "Tech Lead", "QA Lead") collapse to Jay with self-review.
2. External APIs are DashScope + DeepSeek only for V1 (durable rule from memory).
3. Fixture refresh is **manual** — no nightly "re-record production responses" automation in V1.
4. `testdata/e2e/scp-049-seed/` is committed to repo (not `.gitignore`d); ~5 MB acceptable.
5. CI runner has 2+ CPU cores for Playwright parallelism (default GitHub Actions runners qualify).
6. FFmpeg version installed via `apt-get` is ≥ 4.x (Ubuntu 22.04 runner default).

### Dependencies (ordered, with required-by date for a V1 ship in ~4 weeks)

| # | Dependency | Unlocks | Required by |
|---|------------|---------|-------------|
| 1 | **CP-3**: `test-e2e` `continue-on-error` removed + root smoke replaced | UI-E2E-01..10 | 2026-04-29 |
| 2 | **CP-4**: ffmpeg in CI + `go-version` pinned | SMOKE-01, 02 | 2026-04-29 |
| 3 | **CP-1**: `Engine.Advance` wired | SMOKE-01, 03 | 2026-05-06 |
| 4 | **CP-5 Story 10-2** FULL: DeepSeek adapter + tuning | SMOKE-06, UI-E2E-07 | 2026-05-13 |
| 5 | **AI-5**: `LoadShadowInput` path fix | SMOKE-06 | 2026-05-06 (bundle with CP-5) |
| 6 | **Phase C hardening**: metadata+manifest atomicity + xfade guard + probe=0 guard | SMOKE-05 (test codifies the fix) | 2026-05-13 |
| 7 | **AI-3**: `RetryExhausted` `>=` unification | UI-E2E-05 | 2026-05-06 |

### Risks to the Plan

- **Risk:** CP-1 Engine.Advance takes longer than 1 week.
  - **Impact:** SMOKE-01, SMOKE-03 remain skipped; V1 gate blocked.
  - **Contingency:** SMOKE-02 delivers 80% of the cross-phase signal. Ship with SMOKE-01 as P1 if absolutely necessary. Document waiver with expiry.
- **Risk:** Playwright flake on batch-review chord (UI-E2E-04).
  - **Impact:** CI flakiness erodes trust.
  - **Contingency:** Use `test.step` per keystroke + screenshot on failure; retry count = 1 in PR (not 0 like current `web/playwright.config.ts`). Move to 0 retries only after 50 consecutive green runs.
- **Risk:** Fixture drift (DashScope API response format changes).
  - **Impact:** Tests break unrelated to code changes.
  - **Contingency:** `docs/test-fixture-refresh.md` documents the record-mode procedure; fixture schema-validates on load to catch drift fast.

---

## 11. Interworking & Regression

| Service/Component | Impact | Regression Scope |
|-------------------|--------|------------------|
| **internal/pipeline/engine.go** | CP-1 rewires Engine.Advance | All existing `engine_test.go` must stay green |
| **internal/pipeline/phase_c.go** | Atomicity fix (SMOKE-05 driver) | All existing `phase_c*_test.go` |
| **internal/llmclient/** | DeepSeek adapter (SMOKE-06 + UI-E2E-07) | `llmclient/*_test.go` contract tests; DashScope stays untouched |
| **web/src/stores/** (zustand) | Per-test reset fixture (deferred 6-5) | Every existing unit test using zustand must still pass after singleton tear-down change |
| **.github/workflows/** | 3 YAML diffs (CP-3, CP-4, SD-4) | Matrix build sanity; ensure no cross-job pollution |

---

## 12. Mitigation Plans (High-Risk Only)

### R-01: `Engine.Advance` stub (score 9)

- **Mitigation:** CP-1 wires the advance loop; SMOKE-01 + SMOKE-03 codify correctness.
- **Owner:** Jay via new story (Story 11-1 or Story 3-6 expansion)
- **Timeline:** 2026-05-06
- **Verification:** `TestE2E_FullPipeline` green in CI for 3 consecutive runs.

### R-02: Text-LLM runtime missing (score 9)

- **Mitigation:** CP-5 Story 10-2 FULL ships DeepSeek adapter; `internal/llmclient/deepseek/` contract-tested.
- **Owner:** Jay via Story 10-2
- **Timeline:** 2026-05-13
- **Verification:** SMOKE-06 green; UI-E2E-07 tuning surface loads DeepSeek as critic.

### R-03: Story 10-2 tuning surface unbuilt (score 9)

- **Mitigation:** Full-scope Story 10-2 (not MVP-only per Jay 2026-04-24).
- **Owner:** Jay via Story 10-2
- **Timeline:** 2026-05-13
- **Verification:** UI-E2E-07 green.

### R-04: metadata+manifest non-atomic (score 6)

- **Mitigation:** Phase C hardening — staging-dir-rename or completed-marker file.
- **Owner:** Jay via Phase C hardening story
- **Timeline:** 2026-05-13
- **Verification:** SMOKE-05 green (both-or-neither invariant).

### R-05, R-06: CI hygiene (ffmpeg + continue-on-error + todo)

- **Mitigation:** One-line YAML diffs per CP-3/CP-4.
- **Owner:** Jay
- **Timeline:** 2026-04-29 (this week)
- **Verification:** CI `test-go` passes with ffmpeg probe; `test-e2e` has no `continue-on-error`.

### R-07: `LoadShadowInput` path bug (score 6)

- **Mitigation:** AI-5 — resolve scenario_path against `{outputDir}/{runID}`.
- **Owner:** Jay via AI-5 or Story 10-2
- **Timeline:** 2026-05-06
- **Verification:** SMOKE-06 green against a real live-DB Phase-A output.

### R-11: `RetryExhausted` `>=`/`>` mismatch (score 6)

- **Mitigation:** AI-3 — shared const + property test across 3 sites (`ListReviewItems`, `RecordSceneDecision`, `DispatchSceneRegeneration`).
- **Owner:** Jay via AI-3
- **Timeline:** 2026-05-06
- **Verification:** UI-E2E-05 green; property test 100 iterations green.

### R-17: FR23 compliance gate zero E2E coverage (score 6)

- **Mitigation:** SMOKE-07 + UI-E2E-06 pair.
- **Owner:** This test-design document (Step 3 delivers the spec; Step 4 implements).
- **Timeline:** 2026-05-06
- **Verification:** Both green; pre-ack upload blocked with 409.

---

## 13. Follow-on Workflows (Manual)

- **Step 4:** Run `/bmad-qa-generate-e2e-tests` with this document as input. Recommended order: UI-E2E-01 → SMOKE-02 → SMOKE-04 → UI-E2E-02 → batch of remaining P0, then P1.
- **Post-Step 4:** Run `/bmad-testarch-test-review` scoped to files flagged in Step 2 §5.3 fixture strategy (tautological-double audit — per Jay 2026-04-25 decision #4, serialized after Step 4 UI tests land).
- **Pre-V1 tag:** Run `/bmad-testarch-trace` to verify the FR1–FR53 + NFR traceability map in Step 2 §5.7 is fully satisfied.
- **Atdd:** *Not applicable here* — these are E2E for already-implemented features (Epics 1–10 shipped code), not red-phase ATDD.

---

## 14. Approval

- [ ] Jay (Owner / PM / Tech Lead / QA Lead collapsed) — Date: __________

**Comments:**

---

## 15. Step 4 Handoff Checklist

When invoking `/bmad-qa-generate-e2e-tests`, seed it with:

- [ ] This document as primary input
- [ ] Step 2 quality strategy as secondary reference
- [ ] Explicit scope: **all 11 P0 first, then 7 P1** (Step 2 Decision #3)
- [ ] Confirm CP-1, CP-3, CP-4, CP-5-Story-10-2, AI-5 all landed (if any outstanding, pick only cases not blocked by that CP)
- [ ] Verify `testdata/e2e/scp-049-seed/` exists; if not, Step 4 generates the seed as its first action

---

## Appendix

### Knowledge Base References (loaded by this workflow)

- `risk-governance.md` — risk classification framework
- `probability-impact.md` — risk scoring methodology
- `test-levels-framework.md` — test level selection
- `test-priorities-matrix.md` — P0–P3 prioritization

### Related Documents

- **Step 1 input:** [_bmad-output/implementation-artifacts/epic-1-10-retro-2026-04-24.md](_bmad-output/implementation-artifacts/epic-1-10-retro-2026-04-24.md)
- **Step 2 input (primary):** [_bmad-output/test-artifacts/quality-strategy-2026-04-25.md](_bmad-output/test-artifacts/quality-strategy-2026-04-25.md)
- **Sprint status:** [_bmad-output/implementation-artifacts/sprint-status.yaml](_bmad-output/implementation-artifacts/sprint-status.yaml)
- **Golden fixture:** [testdata/golden/eval/manifest.json](testdata/golden/eval/manifest.json)
- **Pipeline E2E base:** [internal/pipeline/e2e_test.go](internal/pipeline/e2e_test.go)
- **Playwright config:** [web/playwright.config.ts](web/playwright.config.ts)
- **Existing UI smoke:** [web/e2e/smoke.spec.ts](web/e2e/smoke.spec.ts), [web/e2e/new-run-creation.spec.ts](web/e2e/new-run-creation.spec.ts)

---

**Generated by:** BMad TEA Agent — Test Architect Module (bmad-testarch-test-design)
**Workflow version:** 4.0 (BMad v6)
**Next command:** `/bmad-qa-generate-e2e-tests` with §15 handoff checklist
