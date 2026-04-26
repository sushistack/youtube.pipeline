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
mode: epic-level
outputFile: _bmad-output/test-artifacts/test-design-epic-1-10-2026-04-25.md
inputDocuments:
  - _bmad-output/test-artifacts/quality-strategy-2026-04-25.md
  - _bmad-output/implementation-artifacts/epic-1-10-retro-2026-04-24.md
  - _bmad-output/implementation-artifacts/sprint-status.yaml
  - testdata/golden/eval/manifest.json
  - internal/pipeline/e2e_test.go
  - web/playwright.config.ts
  - web/e2e/smoke.spec.ts
  - web/e2e/new-run-creation.spec.ts
---

# Step 3 Test Design — Progress Log

## Step 1: Mode Detection (completed)

- **Chosen mode:** Epic-Level (multi-epic, pre-V1 ship gate)
- **Rationale:** User provided `quality-strategy-2026-04-25.md` with 18 pre-scoped scenarios (Step 2 output); `sprint-status.yaml` exists → Epic-Level mode per step-01 rule B.

## Step 2: Load Context (completed)

**Verified infra:**

- Playwright config: `web/playwright.config.ts` — Chromium-only, `testDir: ./e2e`, port 4173, `npm run serve:e2e` webServer
- Existing web/e2e specs: `smoke.spec.ts` (3 tests — SPA shell, settings, tuning), `new-run-creation.spec.ts`
- Root `e2e/smoke.spec.ts` — confirmed `test.todo()` (R-06)
- Pipeline E2E: `internal/pipeline/e2e_test.go` has mock providers ready; `TestE2E_FullPipeline` at line 51 is `t.Skip`
- Golden fixture: `testdata/golden/eval/{manifest.json, 000001/, 000002/, 000003/}` — Korean SCP-173 content
- Sprint status: Epic 1–10 consolidated retrospective done 2026-04-24; Story 10-2 pending FULL

**Inherited from Step 2 quality-strategy:**

- 18 scenarios (SMOKE-01..08 + UI-E2E-01..10)
- 12 P0 deferred items
- 7 resolved decisions (Jay 2026-04-25) — notably DeepSeek as second text provider, ffmpeg via apt-get in CI

## Step 3: Risk & Testability (completed)

- Risk matrix inherited from Step 2 §3 (no re-scoring)
- Mapped each risk to the scenario(s) exercising it (see §2 of output doc)
- Testability extensions: verified mock-provider seam in `e2e_test.go`; page-object layer needed for UI (deferred 6-5 zustand singleton is a pre-test infra fix)

## Step 4: Coverage Plan (completed)

- 11 P0 + 7 P1 scenarios with given/when/then, fixture, runtime, effort
- PR vs Nightly vs Weekly split (PR ≤ 4 min, Nightly ~6 min → full suite ≤ 10 min per NFR-T1)
- Quality gate thresholds bound to Step 2 §5.6
- Total effort: 70–116 h ≈ 9–15 dev-days solo (plus 13–20 h shared infra)

## Step 5: Output Generation (completed)

- Output: `_bmad-output/test-artifacts/test-design-epic-1-10-2026-04-25.md`
- Validated against checklist.md (risk matrix + coverage + execution + estimates + gates all populated)
- No subagent orchestration needed (epic-level single-document output)

## Completion Report

- **Mode used:** Epic-Level, sequential single-worker
- **Output file:** `_bmad-output/test-artifacts/test-design-epic-1-10-2026-04-25.md`
- **Key risks covered:** R-01..R-17 (11 high-priority inherited from Step 2) each mapped to ≥ 1 test case
- **Gate thresholds:** P0 100% / P1 ≥ 95% / full-suite ≤ 10 min / zero external HTTP / zero console errors
- **Open assumptions:**
  1. CP-1, CP-3, CP-4, CP-5-Story-10-2, AI-5 land before Step 4 can fully execute
  2. `testdata/e2e/scp-049-seed/` does not yet exist — Step 4 must create it
  3. FI adapter package `internal/pipeline/fi/` does not yet exist — Step 4 must scaffold it
- **Next command:** `/bmad-qa-generate-e2e-tests` (see §15 of the output doc for handoff checklist)
