---
stepsCompleted: ['step-01-load-context', 'step-02-discover-tests', 'step-03-map-criteria', 'step-04-analyze-gaps', 'step-05-gate-decision']
lastStep: 'step-05-gate-decision'
lastSaved: '2026-04-25'
workflowType: 'testarch-trace'
inputDocuments:
  - _bmad-output/planning-artifacts/prd.md
  - _bmad-output/planning-artifacts/epics.md
  - _bmad-output/test-artifacts/quality-strategy-2026-04-25.md
  - _bmad-output/test-artifacts/test-design-epic-1-10-2026-04-25.md
  - _bmad-output/implementation-artifacts/sprint-status.yaml
  - _bmad-output/implementation-artifacts/deferred-work.md
  - internal/pipeline/e2e_test.go (SMOKE-01)
  - internal/pipeline/smoke02_phase_handoff_test.go
  - internal/pipeline/smoke03_resume_test.go
  - internal/pipeline/smoke04_cost_cap_test.go
  - internal/pipeline/smoke05_metadata_atomic_test.go
  - internal/pipeline/smoke06_shadow_live_test.go
  - internal/pipeline/smoke08_export_test.go
  - internal/api/handler_run_test.go (SMOKE-07)
  - web/e2e/smoke.spec.ts (UI-E2E-01)
  - web/e2e/inline-narration-edit.spec.ts (UI-E2E-02)
  - web/e2e/character-pick.spec.ts (UI-E2E-03)
  - web/e2e/batch-review-chord.spec.ts (UI-E2E-04)
  - web/e2e/retry-exhausted.spec.ts (UI-E2E-05)
  - web/e2e/compliance-gate-ack.spec.ts (UI-E2E-06)
  - web/e2e/tuning-surface-deepseek.spec.ts (UI-E2E-07)
  - web/e2e/settings-save.spec.ts (UI-E2E-08)
  - web/e2e/run-inventory-search.spec.ts (UI-E2E-09)
  - web/e2e/failure-banner-resume.spec.ts (UI-E2E-10)
coverageBasis: 'acceptance_criteria'
oracleConfidence: 'high'
oracleResolutionMode: 'formal_requirements'
oracleSources:
  - _bmad-output/planning-artifacts/prd.md (FR1–FR53, NFR-C/R/P/A/T families)
  - _bmad-output/test-artifacts/quality-strategy-2026-04-25.md §3 + §5.7
  - _bmad-output/test-artifacts/test-design-epic-1-10-2026-04-25.md §8 (gate criteria)
externalPointerStatus: 'not_used'
tempCoverageMatrixPath: '/tmp/tea-trace-coverage-matrix-2026-04-25.json'
gateType: 'release'
gateDecision: 'PASS'
gateDecisionUpdatedAt: '2026-04-25T16:45:00+09:00'
gateDecisionEvidence:
  go_test: 'all packages green (22s)'
  playwright: '19/19 green (retries=1 per Step 3 §10)'
  epic_11_done: '6/6 (Jay 2026-04-25 single-operator close)'
  p1_waiver: '13 items waived to 2026-07-31 V1.5'
  root_e2e_removed: true
---

# Traceability Matrix & Gate Decision — V1 Ship Readiness (Epic 1–11)

**Target:** youtube.pipeline V1 release — Epic 1 ~ Epic 11
**Date:** 2026-04-25
**Evaluator:** Murat (TEA Agent) for Jay
**Coverage Oracle:** Acceptance Criteria (formal — PRD FR/NFR + Step 3 18-scenario set)
**Oracle Confidence:** HIGH (PRD + epics + test-design all present and current)
**Gate Type:** Release
**Gate Decision (initial 2026-04-25 07:26 UTC):** ⚠️ CONCERNS
**Gate Decision (final 2026-04-25 16:45 KST):** ✅ **PASS**

**Transition rationale.** Jay actioned all 4 CONCERNS resolution items in one session (2026-04-25):
1. **CI green verified** — `go test ./...` 22s all packages ok; `npx playwright test` 19/19 green (with `retries: 1` per Step 3 §10 absorbing known UI-E2E-04 batch-review-chord isolation flake).
2. **Epic 11 stories 11-1, 11-3, 11-4, 11-5, 11-6 marked `done`** in `sprint-status.yaml` — code-review intentionally skipped under single-operator authority. Epic 11 itself transitions `in-progress → done`.
3. **13 P1 deferred items batch-waived** to 2026-07-31 (V1.5) — recorded at top of `deferred-work.md` with re-evaluation triggers.
4. **Root `e2e/` directory removed** — `git rm -rf e2e/` (4 tracked files + node_modules); CI already used `cd web && npx playwright test` so no regression risk.

**No new gaps introduced; all evidence captured.**

---

## Executive Summary

| 항목 | 상태 |
|------|------|
| **18-scenario test files 존재 (Step 4/4.5/6 산출물)** | ✅ 18/18 — 모두 구현 완료 |
| **`t.Skip` / `test.todo` (P0 surface)** | ⚠️ 1건 잔존 — `e2e/smoke.spec.ts` (root) `test.todo()` 이지만 CI 가 `cd web && npx playwright test` 로 repoint 되어 functionally closed |
| **P0 scenarios (SMOKE-01..05, UI-E2E-01..06)** | ✅ 11/11 구현, skip 없음 — green 가정 (CI 실행 결과는 외부 검증 필요) |
| **P1 scenarios (SMOKE-06..08, UI-E2E-07..10)** | ✅ 7/7 구현 |
| **P0 deferred items (Step 2 §3)** | ⚠️ 12/12 functionally addressed — but 11-1/11-3/11-4/11-5/11-6 still in `review` (code-review pending) |
| **P1 deferred items** | ⚠️ 다수 OPEN, 일부는 owner 미지정 |
| **CI hygiene (CP-3/CP-4/SD-4)** | ✅ ffmpeg 설치, go-version 정상 (`1.25`), test-e2e `continue-on-error` 없음, `cd web && npx playwright test` 사용 |
| **Coverage Gap (FR/NFR 미매핑)** | 18 scenario 외부 unit/integration 으로 상당부분 보완 (FR8/29/30/47/48 등은 18-scenario 외 unit-only) |
| **Final Gate** | ⚠️ **CONCERNS** — 5개 Epic-11 P0 fix 가 review 단계, 다수 P1 가 owner 미지정 |

---

## Coverage Summary

| Priority | Total | FULL | PARTIAL | NONE | Coverage % | Status |
|----------|-------|------|---------|------|-----------|--------|
| P0 (11 scenarios) | 11 | 11 | 0 | 0 | **100%** | ✅ PASS |
| P1 (7 scenarios) | 7 | 7 | 0 | 0 | **100%** | ✅ PASS |
| P0/P1 합계 (test-design §8 gate denominator) | 18 | 18 | 0 | 0 | **100%** | ✅ PASS |
| FR1–FR53 (53 reqs, mapped to 18-scenario set) | 53 | 36 | 11 | 6 | **68%** | ⚠️ unit/integration 보완 |
| NFR (11 explicit reqs) | 11 | 7 | 2 | 2 | **64%** | ⚠️ NFR-R2/R4 V1.5 deferred |

`P0` and `P1` rows reflect the **Step 3 §8 ship-gate scope** (the 18 E2E scenarios).
The FR/NFR rows reflect requirement-level coverage; non-FULL items are explained in §Gap Analysis below — most have unit/integration coverage outside the 18-scenario set.

---

## PHASE 1: REQUIREMENTS TRACEABILITY

### 1.1 Functional Requirements (FR1–FR53)

#### Lifecycle (FR1–FR8, FR50) — Epic 2

| FR | Summary | Mapped Scenarios | Coverage | Priority | Notes |
|----|---------|------------------|----------|----------|-------|
| **FR1** | Start a new run | SMOKE-01, UI-E2E-01 (`Ctrl+N` new run panel) | FULL ✅ | P0 | `internal/pipeline/e2e_test.go:188` exercises `runStore.run.Stage=StagePending → StageScenarioReview`. |
| **FR2** | Resume failed run | SMOKE-03, UI-E2E-10 | FULL ✅ | P0 | `smoke03_resume_test.go:37` Phase C resume cycle; `failure-banner-resume.spec.ts` Enter→resume. |
| **FR3** | Cancel in-flight run | — | NONE ⚠️ | P2 | 18-scenario set 외 — `internal/service/run_service_test.go` unit-only. **Gap-FR3**. |
| **FR4** | Inspect runs | SMOKE-04 (cli inspect), UI-E2E-09 (sidebar inventory) | FULL ✅ | P1 | `smoke04_cost_cap_test.go` JSON envelope; `run-inventory-search.spec.ts` filter+search. |
| **FR5** | State persistence (15 stages, retry, status) | SMOKE-01, SMOKE-03 | FULL ✅ | P0 | All 15 stages observed in `runs.transitions`; existing `fr-coverage.json` lists 3 unit tests. |
| **FR6** | Per-stage observability | SMOKE-01, SMOKE-04 | FULL ✅ | P0 | Cost/retry_reason fields verified; existing `fr-coverage.json` 3 unit tests. |
| **FR7** | *Moved to NFR-C1* | — | N/A | — | NFR-C1 으로 이동. |
| **FR8** | Anti-progress (cosine ≥ 0.92) | — | INTEGRATION-ONLY ⚠️ | P1 | `antiprogress_integration_test.go` 충분; 18-scenario E2E 외부. **Gap-FR8** (V1.5 embedding). |
| **FR50** | "What changed since I paused" diff | SMOKE-03 (간접) | INTEGRATION-ONLY ⚠️ | P1 | `hitl_diff_test.go` unit; UI surface 부재. **Gap-FR50**. |

#### Phase A — Scenario Generation (FR9–FR13) — Epic 3

| FR | Summary | Mapped Scenarios | Coverage | Priority | Notes |
|----|---------|------------------|----------|----------|-------|
| **FR9** | Researcher draws from local corpus | SMOKE-01 (Phase A) | FULL ✅ | P0 | `e2eRunStore` Phase A path; existing 6 unit tests. |
| **FR10** | 6-agent chain + shot count from TTS | SMOKE-01, SMOKE-02 | FULL ✅ | P0 | Phase A→B→C handoff verifies shot counts; 4 unit tests existing. |
| **FR11** | Schema validation between agents | SMOKE-01 | FULL ✅ | P0 | scenario.json schema validated; 5 unit tests existing. |
| **FR12** | Writer ≠ Critic providers | SMOKE-01 (DashScope vs DeepSeek), UI-E2E-07 | FULL ✅ | P0 | Story 11-2 ship 이후 DeepSeek `internal/llmclient/deepseek/` 실재; UI-E2E-07 tuning swap surface. |
| **FR13** | Critic at 2 checkpoints | SMOKE-01 | FULL ✅ | P0 | Phase A 종료 + post-Writer 모두 dispatch. |

#### Phase B — Image & TTS (FR14–FR19) — Epic 5

| FR | Summary | Mapped Scenarios | Coverage | Priority | Notes |
|----|---------|------------------|----------|----------|-------|
| **FR14** | Per-shot images, frozen descriptor | SMOKE-01, SMOKE-02 | FULL ✅ | P0 | `e2e_test.go:281-302` frozen-descriptor verbatim invariant. |
| **FR15** | Korean TTS (numerals→hangul) | SMOKE-01, SMOKE-02 | FULL ✅ | P0 | `transliteration_test.go` unit + Phase B mocked TTS. |
| **FR16** | Concurrent image+TTS via `errgroup.Group` | SMOKE-01, SMOKE-02 | FULL ✅ | P0 | Phase B path verifies tracks complete before assemble. |
| **FR17** | Character-ref selection prerequisite | UI-E2E-03 | FULL ✅ | P0 | `character-pick.spec.ts` 1-9/0 direct-select. |
| **FR18** | Canonical char-ref via reference editing | UI-E2E-03 | FULL ✅ | P0 | Same — covers selection→freeze→Phase B propagation. |
| **FR19** | Search cache | — | UNIT-ONLY ⚠️ | P2 | `internal/cache/*_test.go` only. **Gap-FR19**. |

#### Phase C — Video Assembly & Compliance (FR20–FR23) — Epic 9

| FR | Summary | Mapped Scenarios | Coverage | Priority | Notes |
|----|---------|------------------|----------|----------|-------|
| **FR20** | Two-stage FFmpeg assembly | SMOKE-01, SMOKE-02 | FULL ✅ | P0 | Real ffmpeg path; codec=h264/aac, duration ±0.2s. |
| **FR21** | Metadata bundle | SMOKE-01, SMOKE-05 | FULL ✅ | P0 | metadata.json 존재 + run_id 일치 검증; SMOKE-05 atomic pair-write. |
| **FR22** | Source manifest | SMOKE-01, SMOKE-05 | FULL ✅ | P0 | manifest.json 동일 path. |
| **FR23** | Compliance gate (ack required) | SMOKE-07, UI-E2E-06 | FULL ✅ | P0 | `TestRunHandler_SMOKE_07_ComplianceGate` (`handler_run_test.go:443`); `compliance-gate-ack.spec.ts`. |

#### Quality Infrastructure (FR24–FR30, FR51–FR52) — Epic 1, 4, 10

| FR | Summary | Mapped Scenarios | Coverage | Priority | Notes |
|----|---------|------------------|----------|----------|-------|
| **FR24** | Critic verdict (pass/retry/accept) | UI-E2E-07, SMOKE-06 | FULL ✅ | P1 | Tuning surface diff view; Shadow run produces verdicts. |
| **FR25** | Rule-based pre-checks (schema + forbidden-term regex) | — | UNIT-ONLY ⚠️ | P1 | `quality_test.go` + `review_gate_test.go` 7 unit tests. **Gap-FR25**. |
| **FR26** | Golden eval set governance + 1:1 ratio | UI-E2E-07 (간접) | INTEGRATION-ONLY ⚠️ | P1 | Tuning surface uses Golden; 13 unit tests in `fr-coverage.json`. |
| **FR27** | Critic vs Golden detection rate | UI-E2E-07 | FULL ✅ | P1 | Tuning surface "Run Golden" + recall display. |
| **FR28** | Critic shadow mode | SMOKE-06, UI-E2E-07 | FULL ✅ | P1 | `smoke06_shadow_live_test.go`; UI Shadow gated behind Golden. |
| **FR29** | Cohen's kappa rolling window | — | UNIT-ONLY ⚠️ | P2 | `decision_store_test.go` 14 unit tests; not in 18-scenario set. **Gap-FR29**. |
| **FR30** | Minor content flag + block | — | UNIT-ONLY ⚠️ | P1 | `quality_test.go` + `review_gate_test.go`. **Gap-FR30**. |
| **FR51** | Test-infra as first-class artifact | SMOKE-01..08, UI-E2E-01..10 | FULL ✅ | P0 | The 18-scenario suite IS the artifact. |
| **FR52** | E2E smoke (canonical seed) | SMOKE-01 (FR52-go), UI-E2E-01 (FR52-web) | FULL ✅ | P0 | Both surfaces covered post Story 11-1 + Epic-11 Playwright migration. |

#### HITL Surface (FR31a–FR37, FR53) — Epic 7, 8

| FR | Summary | Mapped Scenarios | Coverage | Priority | Notes |
|----|---------|------------------|----------|----------|-------|
| **FR31a** | Auto-approval at score threshold | — | UNIT-ONLY ⚠️ | P1 | `quality_test.go` thresholds; UI gating not in 18. **Gap-FR31a**. |
| **FR31b** | Batch-review per-scene cards | UI-E2E-04 | FULL ✅ | P0 | `batch-review-chord.spec.ts` J/K/Enter/Esc/Tab/S/Shift+Enter. |
| **FR31c** | Precision review (high-leverage scenes) | UI-E2E-02 (Inspector) | PARTIAL ⚠️ | P1 | Scenario Inspector covers; explicit "high-leverage" classification not exercised by E2E. |
| **FR32** | Approve/reject/edit | UI-E2E-02, UI-E2E-04, UI-E2E-05 | FULL ✅ | P0 | All three actions exercised. |
| **FR33** | Undo most recent | UI-E2E-02 (Ctrl+Z), UI-E2E-04 (undo stack) | FULL ✅ | P0 | Both editors + batch undo. |
| **FR34** | Batch approve all remaining | UI-E2E-04 (Shift+Enter) | FULL ✅ | P0 | Server-side `aggregate_command_id` normalization asserted. |
| **FR35** | Skip and remember | UI-E2E-04 (`S`) | FULL ✅ | P0 | Pattern recorded for future learning; spy on POST. |
| **FR36** | Decisions store (persistent) | UI-E2E-04, UI-E2E-05 | FULL ✅ | P0 | `decisions` rows asserted in mock spy. |
| **FR37** | Decisions history (read-only timeline) | — | UNIT-ONLY ⚠️ | P2 | Story 8.6 timeline view; not in 18-scenario E2E set. **Gap-FR37**. |
| **FR53** | Prior rejection warning | UI-E2E-05 | FULL ✅ | P1 | Rejection regen flow surfaces prior reject; AI-3 unification verified. |

#### Operator Surface (FR38–FR44) — Epic 1, 6, 10

| FR | Summary | Mapped Scenarios | Coverage | Priority | Notes |
|----|---------|------------------|----------|----------|-------|
| **FR38** | `pipeline init` | — | UNIT-ONLY ⚠️ | P2 | `cmd/pipeline/init_test.go`. **Gap-FR38**. |
| **FR39** | `pipeline doctor` (preflight) | — | UNIT-ONLY ⚠️ | P2 | `cmd/pipeline/doctor_test.go`. **Gap-FR39**. |
| **FR40** | Web UI server localhost-only | UI-E2E-01 | FULL ✅ | P0 | `127.0.0.1:4173` 바인드 — `playwright.config.ts`. |
| **FR41** | Production / Tuning / Settings tabs | UI-E2E-01, UI-E2E-07, UI-E2E-08 | FULL ✅ | P0 | 모든 3 탭 별도 spec. |
| **FR42** | JSON CLI envelope | SMOKE-04 (cli inspect JSON), SMOKE-08 | FULL ✅ | P1 | Versioned envelope; `fr-coverage.json` lists 0 — **annotation gap**. |
| **FR43** | Human-readable hierarchical CLI | — | DOC-ONLY ⚠️ | P3 | `fr-coverage.json` 명시: "not-directly-testable, covered by AC-HUMAN in Story 1.6". **Acceptable**. |
| **FR44** | Export decisions/artifacts JSON | SMOKE-08 | FULL ✅ | P1 | `smoke08_export_test.go` round-trip + CSV injection guard. |

#### Compliance / V1 governance (FR45–FR49) — Epic 4, 9, 10

| FR | Summary | Mapped Scenarios | Coverage | Priority | Notes |
|----|---------|------------------|----------|----------|-------|
| **FR45** | Provider audit logging | SMOKE-04 (cost row carries provider) | PARTIAL ⚠️ | P1 | Cost path stores provider; full audit trail (every generation) is unit-only. **Gap-FR45**. |
| **FR46** | Reject Writer == Critic at preflight + run-entry | SMOKE-01 (preflight), 11-2 ship | FULL ✅ | P0 | `provider_guard_test.go`; runtime guard on Writer stage entry. |
| **FR47** | Blocked voice-ID list | — | UNIT-ONLY ⚠️ | P1 | TTS adapter unit. **Gap-FR47**. |
| **FR48** | Forbidden-term lists (KCSC) | — | UNIT-ONLY ⚠️ | P1 | `quality_test.go` 2 tests. **Gap-FR48**. |
| **FR49** | Replay paused HITL session | SMOKE-03, UI-E2E-10 | FULL ✅ | P0 | `hitl_session_test.go` 8 unit + integration; UI resume covered. |

---

### 1.2 Non-Functional Requirements (요청 범위)

| NFR | Summary | Mapped Scenarios | Coverage | Priority | Notes |
|-----|---------|------------------|----------|----------|-------|
| **NFR-C1** | Per-stage cost cap | SMOKE-04 | FULL ✅ | P0 | `smoke04_cost_cap_test.go:60` `ErrCostCapExceeded`. |
| **NFR-C2** | Per-run cost cap | SMOKE-04 | FULL ✅ | P0 | RunTotal accumulator hard-stop. |
| **NFR-C3** | Cost data persistence (`cost_usd`/`token_in`/`token_out`) | SMOKE-04 | FULL ✅ | P0 | NFR-C3 invariant explicit assertion in test (line 70). |
| **NFR-R1** | Resume idempotency (no double-count, no dup rows) | SMOKE-03 | FULL ✅ | P0 | `smoke03_resume_test.go` clean-slate verified. |
| **NFR-R2** | Anti-progress detector FP rate | — | NONE — V1.5 ⚠️ | P2 | V1 bag-of-words Korean-weak; explicitly deferred to V1.5 embedding swap. **Gap-NFR-R2**. |
| **NFR-R3** | Durable SQLite + atomic metadata/manifest pair | SMOKE-05 | FULL ✅ | P0 | Story 11-5 staging-dir-rename or completed-marker; `smoke05_metadata_atomic_test.go` codifies. |
| **NFR-R4** | Web UI client-state stateless (canonical = server) | — | NONE ⚠️ | P2 | localStorage quota/corruption untested; not ship-blocker. **Gap-NFR-R4**. |
| **NFR-P3** | Rate-limit backoff | — | INTEGRATION ✅ | P1 | `internal/llmclient/retry_test.go` + FakeClock + 429 backoff tests; not in 18-scenario set but adequate. |
| **NFR-P4** | First-video wall-clock captured | SMOKE-01 (간접) | PARTIAL ⚠️ | P2 | Duration measured via FFmpeg probe; NFR-P4 is operational metric (k6-lite post-V1). |
| **NFR-A1** | 8-key keyboard shortcuts | UI-E2E-04 | FULL ✅ | P0 | `batch-review-chord.spec.ts` full chord J/K/Enter/Esc/Tab/S/Shift+Enter/Ctrl+Z. |
| **NFR-A2** | WCAG broad compliance | UI-E2E-01, UI-E2E-04 | PARTIAL ⚠️ | P2 | Console-error gate + `aria-live` partial; full WCAG audit deferred. **Gap-NFR-A2**. |
| **NFR-T1** | CI ≤ 10 min hard-cap | — (suite-level) | FULL ✅ | P0 | Step 3 runtime budget: PR ≤ 4 min, full suite ≤ 10 min. |

> **NFR-S1..S4 / T2..T6 / O1..O4 / M1..M4 / L1..L4** are out of explicit Step 5 §7 scope (request 매트릭스에 없음) but are partially covered by existing infra tests. Not gate-blocking.

---

### 1.3 Per-Scenario Coverage Reverse Map (18 scenarios → which FR/NFR)

| Scenario | File | FR/NFR Covered | Priority |
|----------|------|----------------|----------|
| **SMOKE-01** | `internal/pipeline/e2e_test.go:188` (`TestE2E_FullPipeline`) | FR1, FR5, FR6, FR9–FR16, FR20–FR22, FR40, FR46, FR51, FR52 | P0 |
| **SMOKE-02** | `internal/pipeline/smoke02_phase_handoff_test.go` | FR10, FR14, FR15, FR16, FR20 | P0 |
| **SMOKE-03** | `internal/pipeline/smoke03_resume_test.go:37` (`TestE2E_SMOKE03_ResumeIdempotency`) | FR2, FR5, FR49, FR50, NFR-R1 | P0 |
| **SMOKE-04** | `internal/pipeline/smoke04_cost_cap_test.go` | FR4, FR42, NFR-C1, NFR-C2, NFR-C3 | P0 |
| **SMOKE-05** | `internal/pipeline/smoke05_metadata_atomic_test.go` | FR21, FR22, NFR-R3 | P0 |
| **SMOKE-06** | `internal/pipeline/smoke06_shadow_live_test.go:21` | FR24, FR28 | P1 |
| **SMOKE-07** | `internal/api/handler_run_test.go:443` (`TestRunHandler_SMOKE_07_ComplianceGate`) | FR23, NFR-L1 (간접) | P1 |
| **SMOKE-08** | `internal/pipeline/smoke08_export_test.go` | FR42, FR44 | P1 |
| **UI-E2E-01** | `web/e2e/smoke.spec.ts` | FR40, FR41, FR52 (web), FR1 (Ctrl+N) | P0 |
| **UI-E2E-02** | `web/e2e/inline-narration-edit.spec.ts` | FR31c, FR32, FR33 | P0 |
| **UI-E2E-03** | `web/e2e/character-pick.spec.ts` | FR17, FR18 | P0 |
| **UI-E2E-04** | `web/e2e/batch-review-chord.spec.ts` | FR31b, FR32, FR33, FR34, FR35, FR36, NFR-A1 | P0 |
| **UI-E2E-05** | `web/e2e/retry-exhausted.spec.ts` | FR32, FR36, FR53 | P0 |
| **UI-E2E-06** | `web/e2e/compliance-gate-ack.spec.ts` | FR23 | P0 |
| **UI-E2E-07** | `web/e2e/tuning-surface-deepseek.spec.ts` | FR12, FR24, FR26, FR27, FR28, FR41 | P1 |
| **UI-E2E-08** | `web/e2e/settings-save.spec.ts` | FR41, NFR-C1/C2 (cost cap edit) | P1 |
| **UI-E2E-09** | `web/e2e/run-inventory-search.spec.ts` | FR4 | P1 |
| **UI-E2E-10** | `web/e2e/failure-banner-resume.spec.ts` | FR2, FR49, NFR-A1 | P1 |

---

## PHASE 1 — Gap Analysis

### Critical Gaps (P0 / 18-scenario set) ❌

**0건.** 모든 P0 (11) + P1 (7) scenario 가 구현되어 있고, `t.Skip` / `test.todo` 가 production CI path 상에 없음.

### High Priority Gaps (P1) ⚠️

| Gap | 요구사항 | 현재 커버리지 | 권장 조치 |
|-----|---------|--------------|----------|
| **Gap-FR8** | 안티-프로그레스 검출 | integration-only (`antiprogress_integration_test.go`) | V1.5 embedding swap 시 SMOKE-09 추가 권장 (현재 V1 ship 차단 아님) |
| **Gap-FR45** | Provider audit logging full trail | SMOKE-04 partial | V1.5 audit log 별도 SMOKE 추가 |
| **Gap-FR50** | "What changed since I paused" UI surface | unit-only | UI surface 가 Epic 11 이후 ship 되면 spec 추가 |
| **Gap-FR53 owner** | RetryExhausted ≥ 3-site unification | UI-E2E-05 backfill 완료, 11-3 review | **Story 11-3 코드리뷰 통과 필요** |

### Medium Priority Gaps (P2 / Document) ℹ️

| Gap | Reason | Mitigation |
|-----|--------|-----------|
| Gap-FR3 (cancel) | cli unit-only | 18-scenario에 추가 불필요 — service test 충분 |
| Gap-FR19 (search cache) | unit-only | 동일 |
| Gap-FR25 (rule-based pre-checks) | review_gate unit | 동일 |
| Gap-FR29 (Cohen's kappa) | decision_store unit (14 tests) | 동일 |
| Gap-FR30 (minor content flag) | quality unit | 동일 |
| Gap-FR31a (auto-approval) | quality unit | 동일 |
| Gap-FR37 (decisions history view) | timeline unit | 동일 |
| Gap-FR38/39 (init/doctor) | cli unit | 동일 |
| Gap-FR47 (blocked voice-ID) | TTS adapter unit | 동일 |
| Gap-FR48 (forbidden-term) | quality unit | 동일 |
| Gap-NFR-R2 | V1.5 embedding swap | explicit deferral, 차단 아님 |
| Gap-NFR-R4 | localStorage corrupt path | post-V1 |
| Gap-NFR-A2 (broad WCAG) | console-error gate only | post-V1 audit |

### Coverage Heuristics

- **Endpoint coverage (API)**: `POST /api/runs/{id}/metadata/ack` ✅ (SMOKE-07), `PUT /api/settings` ✅ (UI-E2E-08), `POST /api/runs/{id}/decisions` ✅ (UI-E2E-04), `POST /api/runs/{id}/resume` ✅ (UI-E2E-10), `/api/tuning/critic-prompt` ✅ (UI-E2E-07). **0 critical endpoint gap**.
- **Auth/authz negative-path**: V1 localhost-only single-operator (NFR-S2/S4). 미적용.
- **Error-path coverage**: SMOKE-04 (cap exceed), SMOKE-05 (write fault), UI-E2E-05 (retry-exhausted), UI-E2E-01 (404 asset). 적정 수준.
- **UI state coverage**: console-error gate 모든 spec; loading/empty 일부 spec; full WCAG audit 미실시 (P2).

### CI / Operational Hygiene Status

| 항목 | 상태 | 출처 |
|------|------|------|
| `test-go` ffmpeg 설치 | ✅ `apt-get install -y ffmpeg` (`.github/workflows/ci.yml:18-19`) | CP-4 closed |
| `go-version` 정상 | ✅ `1.25` (3 sites) — 1.25.7 fictional 제거됨 | SD-4 closed |
| `test-e2e continue-on-error` | ✅ 미사용 (검색 결과 0건) | CP-3 closed |
| Root `e2e/smoke.spec.ts` | ⚠️ 여전히 `test.todo()` — 단 CI 가 `cd web && npx playwright test` 사용하므로 functionally bypass | CP-3 functionally closed (literal todo not removed) |
| Web `e2e/smoke.spec.ts` | ✅ 실제 spec, UI-E2E-01 구현 | CP-3 closed |
| `TestE2E_FullPipeline` skip 제거 | ✅ skip 없음, 본문 완전 구현 (line 188-344) | CP-2 closed |

---

## PHASE 2: QUALITY GATE DECISION

**Gate Type:** release
**Decision Mode:** deterministic (Step 3 §8 criteria)

### Evidence Summary

#### Test Implementation Status (run results 외부 CI 검증 필요)

- **Pipeline E2E test files (Go):** 8/8 implemented — SMOKE-01 (`e2e_test.go:188`) + SMOKE-02..08 (8 spec files). No `t.Skip` in production code path.
- **UI E2E test files (Playwright):** 10/10 implemented — `web/e2e/{smoke,inline-narration-edit,character-pick,batch-review-chord,retry-exhausted,compliance-gate-ack,tuning-surface-deepseek,settings-save,run-inventory-search,failure-banner-resume}.spec.ts`. No `test.todo()` / `test.skip` / `test.fixme` in production CI path.
- **Total active test cases (de-duplicated):** 18 scenarios + supporting page-objects.

> **CAVEAT:** Gate evaluator does NOT have live CI run output. Decision below assumes test files compile and pass against current `main` HEAD (`39f8a0e`). Latest 5 commits (`8af4fa4` through `39f8a0e`) are Epic-11 P0 fix backfills + Story 11-1 SMOKE-01/03 main implementation, all merged. CI 통과 여부는 외부 검증 (Actions run 결과) 으로 확인 필요.

#### P0 Deferred-Work Closure Status (Step 2 §3 — 12 items)

| # | Item | 출처 | 상태 | Closure 근거 |
|---|------|------|------|------|
| 1 | Engine.Advance stub | CP-1 / Story 11-1 | ✅ Closed | `e2e_test.go:205-265` Engine.Advance dispatch 검증; sprint-status `11-1: review` |
| 2 | LoadShadowInput production-path bug | AI-5 / Story 11-2 | ✅ Closed | `11-2: done` (sprint-status); `RuntimeEvaluator` 추가 |
| 3 | metadata.json + manifest.json non-atomic pair | Phase C hardening / Story 11-5 | ✅ Closed | `phase_c_metadata.go` staging-dir-rename; SMOKE-05 codifies |
| 4 | xfade offset negative on shot < 0.5s | Story 11-5 | ✅ Closed | `xfade_test.go` guard added |
| 5 | probeDuration = 0 silently passes | Story 11-5 | ✅ Closed | Phase C probe ≥ threshold guard |
| 6 | Root `e2e/smoke.spec.ts` test.todo() | CP-3 | ⚠️ Functionally closed | Root file 그대로 `test.todo`; CI 가 `cd web && npx playwright test` 로 repoint (real `web/e2e/smoke.spec.ts` 실재) |
| 7 | `test-e2e` `continue-on-error: true` | CP-3 | ✅ Closed | `.github/workflows/ci.yml` 검색 0건 |
| 8 | FFmpeg not installed in CI test-go | CP-4 | ✅ Closed | `apt-get install -y ffmpeg` step at line 18-19 |
| 9 | go-version `1.25.7` fictional | SD-4 | ✅ Closed | 3 sites: `go-version: '1.25'` |
| 10 | Migration 004 ordinal collision | CP-6 / SD-5 | ⚠️ Pre-existing accept | Both `004_anti_progress_index.sql` + `004_hitl_sessions.sql` apply correctly per `db.Migrate` semantics; sprint-planning 차후 normalize (deferred line 238 documents) |
| 11 | Text-LLM runtime missing (DeepSeek/Gemini docs only) | CP-5 / Story 11-2 | ✅ Closed | `internal/llmclient/deepseek/text.go` + `text_test.go` ship 완료 |
| 12 | Story 10-2 tuning surface unbuilt | CP-5 | ✅ Closed | `10-2: done`, `11-2: done` (FULL scope shipped) |

**P0 Deferred Score:** 11/12 fully closed, 1/12 functionally-closed (root smoke todo bypassed by CI repoint).

#### Epic 11 Story Status (5/6 Stories in `review` — 코드리뷰 진행중)

| Story | Status | P0 deferred mapped |
|-------|--------|---------|
| 11-1 Engine.Advance | review | #1 |
| 11-2 DeepSeek adapter + tuning surface FULL | **done** | #2, #11, #12 |
| 11-3 RetryExhausted >= unification | review | (P1 deferred) |
| 11-4 FR23 compliance-gate handler | review | (FR23 hard gate — UI-E2E-06 covers) |
| 11-5 Phase C hardening (atomic + xfade + probe) | review | #3, #4, #5 |
| 11-6 ProductionShell URL race fix | review | (P1 / UX) |

**Risk:** 5 of 6 Epic-11 stories are in `review` (code-review pending). Implementation done, but Acceptance Criteria sign-off (`done`) is gated behind `bmad-code-review`. Per Step 3 §8 strict reading, "P0 deferred items closed" 는 **review 통과 후 done** 상태를 요구하는 것으로 해석 가능.

#### P1 Items OPEN without Owner+Deadline (test-design §8 CONCERNS criterion)

| Item | Source | Owner | Deadline | Status |
|------|--------|-------|----------|--------|
| `RetryExhausted` `>=` 3-site unification | 8.4/8.5/8.6 deferred | Story 11-3 (review) | Pre-V1 ship | ⚠️ review 통과 대기 |
| `BatchApprove` undo non-normalized aggregate_command_id | 8-6 deferred | — | — | ❌ OPEN no owner |
| `approved_scene_indices` O(N²) storage | 8-5 deferred | — | post-V1 | ⚠️ documented defer |
| Undo stack GC on run switch | 8-5 deferred | — | — | ❌ OPEN no owner |
| `Cmd+Z` macOS unmapped | 8-5 deferred | — | — | ❌ OPEN no owner |
| Concurrent `pipeline export` race | 10-5 deferred | SMOKE-08 codifies expected behavior | — | ⚠️ test only |
| `RunGolden` mutates manifest.json | 10-4 deferred | — | post-V1 | ❌ OPEN no owner |
| `warnings` field nesting drift | 6-5 deferred | — | — | ❌ OPEN no owner |
| `spa.go` 200 for `/assets/*` misses | 6-1 deferred | UI-E2E-01 regression guard | — | ⚠️ test guard but root cause not fixed |
| Test-double fidelity audit (tautological doubles) | retro §4.3 | post Step-4 (Jay's decision #4) | — | ⚠️ scheduled |
| FR53 cites cancelled/failed source runs | 8-4 deferred | — | — | ❌ OPEN no owner |
| Rate-limiter `fn` goroutine outlives `Do` on timeout | 5-1 deferred | — | — | ❌ OPEN no owner |
| `BlockExternalHTTP` mutex missing | 1-4/1-7 deferred | — | when `t.Parallel()` introduced | ⚠️ conditional |
| `AcknowledgeMetadata` no `MaxBytesReader` | 9-4 deferred | — | hardening | ❌ OPEN no owner |
| `CountRegenAttempts` counts superseded rows | 8-4/8-5/8-6 | — | doc-or-fix | ❌ OPEN no owner |
| Shadow `normalizeCriticScore` silent clamp | 4-2 deferred | — | — | ❌ OPEN no owner |
| **DeepSeek 429 `Retry-After` dropped** | 11-2 deferred (2026-04-25) | — | post-V1 (cross-provider rate-limit error type) | ❌ NEW OPEN |
| **RuntimeEvaluator rebuilds DeepSeek client per call** | 11-2 deferred (2026-04-25) | — | hot-reload spec | ❌ NEW OPEN |
| **DeepSeek `finish_reason: "length"` treated as success** | 11-2 deferred (2026-04-25) | — | ergonomic | ❌ NEW OPEN |

**Total P1 OPEN without owner+deadline:** **≥ 10 items.** Per Step 3 §8: "**no P1 OPEN without mitigation owner + deadline**" 가 PASS 의 조건이지만, 다수 P1 가 owner 미지정 → **CONCERNS** 트리거.

---

### Decision Criteria Evaluation

#### P0 Criteria

| Criterion | Threshold | Actual | Status |
|-----------|-----------|--------|--------|
| P0 scenarios implemented | 11 | 11 | ✅ MET |
| P0 scenarios skip-free in CI path | 0 skips | 0 | ✅ MET |
| P0 deferred items closed | 12 (all) | 11 closed + 1 functionally-closed (root e2e todo) | ⚠️ MET (with caveat) |
| `TestE2E_FullPipeline` skip removed | yes | yes (line 188 active) | ✅ MET |
| Web `e2e/smoke.spec.ts` real | yes | yes (`web/e2e/smoke.spec.ts` UI-E2E-01) | ✅ MET |
| FFmpeg in CI | yes | yes | ✅ MET |
| go-version real | yes | `1.25` | ✅ MET |
| `test-e2e continue-on-error` removed | yes | yes (0 hits) | ✅ MET |
| Critical NFRs (C1/C2/C3, R1/R3, A1) green | all | all FULL | ✅ MET |
| Epic-11 stories `done` | 6 | 1 done + 5 review | ⚠️ NOT FULLY MET |

**P0 Evaluation:** ✅ ALL THRESHOLDS MET (with one functionally-closed root-smoke caveat + 5 Epic-11 stories pending code-review sign-off).

#### P1 Criteria

| Criterion | Threshold | Actual | Status |
|-----------|-----------|--------|--------|
| P1 scenarios implemented | 7 | 7 | ✅ MET |
| P1 scenarios skip-free | 0 skips | 0 | ✅ MET |
| P1 OPEN without owner+deadline | 0 | ≥ 10 | ❌ FAIL |
| P1 NFRs (P3/P4/A2) green | all | partial (P3 ✅, P4 ⚠️, A2 ⚠️) | ⚠️ PARTIAL |

**P1 Evaluation:** ⚠️ SOME CONCERNS — coverage 100% 이지만 deferred 다수 OPEN.

---

### GATE DECISION: ⚠️ **CONCERNS**

### Rationale

**Why not PASS:**
1. Step 3 §8 PASS 조건 "**no P1 OPEN without mitigation owner + deadline**" 미충족 — 최소 10개의 P1 deferred 항목이 owner 미지정 (예: Cmd+Z macOS unmapped, BatchApprove undo aggregate_command_id, AcknowledgeMetadata MaxBytesReader, FR53 stage filter, rate-limiter goroutine lifetime 등).
2. Epic-11 stories 5/6 (11-1, 11-3, 11-4, 11-5, 11-6) 가 `review` 상태 — 구현은 완료되었으나 `bmad-code-review` 승인 대기. P0 deferred item closure 는 review 통과 시 명실상부 closed.
3. Root `e2e/smoke.spec.ts` 의 `test.todo()` 가 literal 로 남아있음 (CI 는 우회하지만 retro 가 명시한 closure 형태가 아님).

**Why not FAIL:**
1. 11/11 P0 scenarios 가 구현 완료, skip-free, CI-runnable.
2. 7/7 P1 scenarios 가 구현 완료.
3. 12/12 P0 deferred items 가 functionally closed (11 fully + 1 via CI repoint bypass).
4. CI hygiene (CP-3/CP-4/SD-4) 모두 정상.
5. NFR-C1/C2/C3, NFR-R1, NFR-R3, NFR-A1, NFR-T1 모두 FULL 커버리지.
6. Critical FRs (FR12 Writer≠Critic, FR20 FFmpeg assembly, FR21/22 metadata bundle, FR23 compliance gate) 모두 E2E green path 보유.

**Why not WAIVED:**
- Jay 가 명시적 expiry-bound waiver 를 발동하지 않음. 본 결정은 deterministic Step 3 §8 적용 결과.

**Confidence:** HIGH — formal requirements oracle (PRD + epics + Step 3 test-design) 존재, test files 18/18 검증 완료, sprint-status 최신 상태 반영.

---

### Critical Issues to Address Before V1 Ship

#### MUST-DO (CONCERNS → PASS 전환 조건)

| Priority | Issue | Owner | Suggested Deadline | Status |
|----------|-------|-------|----------------------|--------|
| **P0-equivalent** | Run `bmad-code-review` on Story 11-1 (Engine.Advance) — sprint-status `done` 으로 전환 | Jay | 2026-04-29 | review |
| **P0-equivalent** | Run `bmad-code-review` on Story 11-3 (RetryExhausted >=) | Jay | 2026-04-29 | review |
| **P0-equivalent** | Run `bmad-code-review` on Story 11-4 (FR23 compliance-gate) | Jay | 2026-04-29 | review |
| **P0-equivalent** | Run `bmad-code-review` on Story 11-5 (Phase C hardening — atomicity + xfade + probe) | Jay | 2026-04-29 | review |
| **P0-equivalent** | Run `bmad-code-review` on Story 11-6 (ProductionShell URL race) | Jay | 2026-04-29 | review |
| **Cleanliness** | Replace root `e2e/smoke.spec.ts` `test.todo()` 또는 root `e2e/` 디렉토리 자체 제거 (CI 가 더 이상 쓰지 않음) | Jay | 2026-04-29 | functional bypass |
| **Test execution** | Run full CI (`go test ./... && cd web && npx playwright test`) on `main` HEAD `39f8a0e` and confirm green | Jay | 2026-04-26 | not-yet-verified |

#### SHOULD-DO (P1 OPEN — owner 지정 또는 명시적 defer-with-expiry waiver)

| Priority | Issue | Suggested Owner | Suggested Action |
|----------|-------|-----------------|------------------|
| P1 | `BatchApprove` undo aggregate_command_id non-normalized | post-V1 batch_commands migration | Owner-assign or batch-waive with V1.5 expiry |
| P1 | Undo stack never GC'd on run switch | UI follow-up | Owner-assign |
| P1 | `Cmd+Z` macOS unmapped | UI 6.3 follow-up | Owner-assign or document "Linux/Win only V1" |
| P1 | `RunGolden` mutates `testdata/golden/eval/manifest.json` | `--dry-run` flag | Owner-assign (low effort) |
| P1 | `warnings` field nesting drift Go ↔ fixture ↔ Zod | contract test | Owner-assign |
| P1 | `spa.go` serves `index.html` 200 for `/assets/*` misses | server hardening | UI-E2E-01 guards regression; root fix owner needed |
| P1 | FR53 cites cancelled/failed source runs | join stage filter | Owner-assign |
| P1 | Rate-limiter `fn` goroutine lifetime | local fix or contract | Owner-assign |
| P1 | `AcknowledgeMetadata` no `MaxBytesReader` | hardening | Owner-assign |
| P1 | `CountRegenAttempts` counts superseded rows | doc-or-scope-to-active | Owner-assign |
| P1 | DeepSeek 429 `Retry-After` dropped | cross-provider rate-limit error type | Owner-assign post-V1 |
| P1 | RuntimeEvaluator rebuilds DeepSeek client per call | constructor-hoist | Owner-assign |
| P1 | DeepSeek `finish_reason: "length"` treated as success | distinct error signal | Owner-assign |
| P1 | Test-double fidelity audit (tautological doubles) | `/bmad-testarch-test-review` | Already scheduled per Jay decision #4 — runs after Step 4 ✅ |

> Per `feedback_meta_principles.md` ("정직 timeline"): waiver 시 expiry 명시 필수. Jay 가 P1 batch waiver 를 V1.5 milestone 으로 발동 가능 (single-operator 권한자).

---

## PHASE 2 — Recommendations

### Immediate Actions (next 24-48 hours)

1. **Verify CI green on `main` HEAD `39f8a0e`** — `bmad-testarch-trace` 는 implementation 존재만 확인; pass/fail 은 GitHub Actions run 결과로 외부 검증.
2. **Schedule 5 × `bmad-code-review` runs** (Stories 11-1, 11-3, 11-4, 11-5, 11-6). 각 story 가 `done` 으로 전환되면 P0 deferred closure 가 명실상부 100% 달성.
3. **Replace root `e2e/smoke.spec.ts`** — 단순 `import './smoke'` re-export 또는 디렉토리 자체 제거. `test.todo()` 잔존은 audit-trace 노이즈.

### Short-term Actions (next milestone)

1. **P1 owner 일괄 지정 또는 batch-waive** — Step 3 §8 CONCERNS → PASS 의 핵심.
2. **Test-double fidelity audit (`/bmad-testarch-test-review`)** — Jay decision #4 일정대로 Step 4 종료 후 실행.
3. **fr-coverage.json 갱신** — 최신 18-scenario 매핑 반영, FR42/45/47/48 annotation 추가.

### Long-term Actions (V1.5 backlog)

1. **NFR-R2 — embedding-based anti-progress (Korean strong)** — V1.5 가장 큰 quality leverage.
2. **NFR-A2 broad WCAG audit** — operator 1명 단계에서는 차단 아님.
3. **Per-stage resume fuzz (×15)** — Story 3 §1 P2 follow-up.

---

## Integrated YAML Snippet (CI/CD)

```yaml
traceability_and_gate:
  traceability:
    target: "youtube.pipeline V1 — Epic 1~11"
    date: "2026-04-25"
    coverage:
      overall_p0_p1_scenarios: 100%   # 18/18 implemented
      p0: 100%                         # 11/11
      p1: 100%                         # 7/7
      fr_coverage_overall: 68%         # 36/53 FULL via 18-scenario set; rest unit-only
      nfr_coverage_overall: 64%        # 7/11 FULL; R2/R4 V1.5
    gaps:
      critical: 0
      high: 4    # Gap-FR8, Gap-FR45, Gap-FR50, Gap-FR53-owner
      medium: 13 # FR3/19/25/29/30/31a/37/38/39/47/48 + NFR-R2/R4/A2/P4
      low: 1     # FR43 doc-only (acceptable)
    quality:
      total_test_files: 18
      skipped_active_paths: 0
      blocker_issues_p0: 0
      warning_issues_p1: 10  # P1 deferred OPEN no owner

  gate_decision:
    decision: "CONCERNS"
    gate_type: "release"
    decision_mode: "deterministic"
    criteria:
      p0_scenarios_implemented: 11/11
      p1_scenarios_implemented: 7/7
      p0_deferred_closed: 11/12 (1 functionally-closed via CI repoint)
      epic_11_stories_done: 1/6
      epic_11_stories_review: 5/6
      p1_open_without_owner: ">= 10"
    thresholds:
      p0_scenarios_required: 11
      p0_deferred_required_closed: 12
      p1_must_have_owner_or_waiver: true
    evidence:
      test_files: "internal/pipeline/{e2e,smoke02..08}_test.go + internal/api/handler_run_test.go + web/e2e/*.spec.ts (18 specs)"
      sprint_status: "_bmad-output/implementation-artifacts/sprint-status.yaml@2026-04-25"
      deferred_work: "_bmad-output/implementation-artifacts/deferred-work.md@2026-04-25"
      ci_workflow: ".github/workflows/ci.yml"
    next_steps: "5 bmad-code-review on stories 11-1/11-3/11-4/11-5/11-6 + P1 owner assignment OR batch-waive"
    waiver_status: not-issued
```

---

## Sign-Off

**Phase 1 — Traceability Assessment:**

- 18-scenario implementation: **18/18 (100%)**
- P0 (11) coverage: **100% FULL**
- P1 (7) coverage: **100% FULL**
- Critical Gaps (P0): **0**
- High-Priority Gaps (P1): **4** (FR8 V1.5, FR45 partial, FR50 unit-only, FR53 owner = Story 11-3 review)

**Phase 2 — Gate Decision:**

- **Decision:** ⚠️ **CONCERNS**
- **P0 Evaluation:** ✅ ALL THRESHOLDS MET (with minor caveats)
- **P1 Evaluation:** ⚠️ SOME CONCERNS (≥ 10 P1 OPEN without owner+deadline)

**Overall Status:** ⚠️ CONCERNS — V1 ship blocked until 5 Epic-11 code-reviews land + P1 owner sweep completes; alternatively Jay invokes single-operator P1 batch-waive with expiry.

**Generated:** 2026-04-25
**Workflow:** bmad-testarch-trace v6.3 (BMAD Test Architect Module)

<!-- Powered by BMAD-CORE™ -->
