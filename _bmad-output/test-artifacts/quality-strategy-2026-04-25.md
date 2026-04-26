---
name: Epic 1–10 Quality Strategy (Step 2)
description: Risk-based quality strategy synthesizing the Step 1 retrospective, the 420-line deferred-work ledger, and architecture NFR drivers. Defines pipeline E2E smoke, UI E2E critical flows, P0/P1/P2 must-fix filter, NFR hotspots, and Step 3 test-design handoff.
type: quality-strategy
date: 2026-04-25
project: youtube.pipeline
user: Jay
author: Murat (bmad-tea)
upstream_input: _bmad-output/implementation-artifacts/epic-1-10-retro-2026-04-24.md
downstream_input_for: bmad-testarch-test-design (Step 3)
status: draft
---

# Epic 1–10 Quality Strategy — youtube.pipeline

**Scope.** Risk-based quality strategy for V1 ship-readiness. Synthesizes Step 1 retrospective (`epic-1-10-retro-2026-04-24.md`), the 420-line `deferred-work.md` ledger, and architecture NFR drivers. Output drives Step 3 (`/bmad-testarch-test-design`).

**Method.** Every recommendation below is risk-scored (probability × impact, 1–9 scale per `resources/knowledge/probability-impact.md`). Quality gate thresholds: score = 9 BLOCK, 6–8 MITIGATE, 4–5 MONITOR, 1–3 DOCUMENT. Priorities map: P0 = score ≥ 8 or CP-blocker, P1 = score 5–7 with mitigation, P2 = score ≤ 4 or document-and-defer.

**Posture going in.** Per-phase integration coverage is rich; cross-phase seam coverage is zero. CI signal integrity is eroded by three compounding mechanisms (`continue-on-error: true`, `test.todo()`, missing `ffmpeg` in `test-go`). No artifact anywhere verifies Phase A → B → C runs end-to-end. This shapes every recommendation.

---

## 1. Pipeline E2E Smoke Scope

Golden path: script → scene → image → audio → video → pre-upload gate. Eight scenarios, prioritized. Targets `internal/pipeline/e2e_test.go` + new integration files.

### P0 — Must pass before quality gate

| ID | Scenario | Blocks on | Test level |
|----|----------|-----------|-----------|
| **E2E-SMOKE-01** | **FR52-go Full Pipeline** — canonical SCP-049 seed → `pipeline create` → `pipeline resume` auto-advances all 15 stages → `ready_for_upload`. Asserts `scenario.json` validates, per-scene segments carry image+TTS paths, `output.mp4` non-zero, `metadata.json` + `manifest.json` internally consistent, `runs.stage=ready_for_upload`. | CP-1 (Engine.Advance wiring), CP-4 (ffmpeg in CI), CP-5 (text-LLM runtime), SD-4 (real Go version). **Unskips `TestE2E_FullPipeline`.** | Go E2E |
| **E2E-SMOKE-02** | **Phase A → B → C Handoff** — fixture `scenario.json` bypasses Phase A → Phase B with mocked image+TTS → Phase C with real FFmpeg. Asserts scene indices intact, shot count matches, frozen descriptor propagated verbatim, `segments.shots` JSON shape, duration tolerance holds. **10× faster iteration than SMOKE-01; orthogonal to Engine.Advance wiring.** | Independent of CP-1; needs CP-4 (ffmpeg). | Go integration |
| **E2E-SMOKE-03** | **Resume Idempotency (NFR-R1)** — execute SMOKE-01 to `stage=phase_b`, kill mid-stage, invoke `pipeline resume`, assert clean-slate cleanup + re-execution produces identical outputs (no duplicate rows, cost not double-counted, artifacts replaced not appended). | CP-1, CP-4. | Go E2E |
| **E2E-SMOKE-04** | **Cost Cap Circuit Breaker (NFR-C1/C2/C3)** — mock provider increments cost; seed `runs.cost_usd` near `cost_cap_per_run`; next call hard-stops with `ErrCostCapExceeded`; `runs.status=failed`; escalation surfaced in inspect. | CP-1. | Go integration |
| **E2E-SMOKE-05** | **Metadata + Manifest Atomic Pair Write (NFR-R3)** — fault-injection where first write succeeds and second fails; assert either (a) both files present and internally consistent, or (b) neither present with retriable failure. Currently non-atomic (deferred `9-2 W1`). | Requires atomicity fix (staging-dir-rename or completed-marker) before test can turn green. | Go integration |

### P1 — Mitigate before shipping

| ID | Scenario | Dependency | Test level |
|----|----------|------------|-----------|
| **E2E-SMOKE-06** | **Shadow Eval Against Live Run** — fix `LoadShadowInput` production-path resolution (scenario_path against `{outputDir}/{runID}`, not `projectRoot`) → evaluate a Phase-A output from the live DB → assert verdict + diff. | AI-5 (`LoadShadowInput` fix). | Go integration |
| **E2E-SMOKE-07** | **Compliance Gate (FR23)** — full run completes → `POST /api/runs/{id}/ack-metadata` → `runs.stage=ready_for_upload`; pre-ack upload attempt blocked. | None. | Go integration |
| **E2E-SMOKE-08** | **Export Round-Trip + CSV Injection Guard** — complete run → `pipeline export --json --csv` → re-run → assert identical bytes (idempotent) + CSV fields starting with `=+-@` are quoted/escaped. | None. | Go integration |

**P2 for this surface:** Anti-progress detector firing in Korean content (known Korean-weak bag-of-words; V1.5 embedding swap); add only if English-fixture coverage exposes a real regression.

---

## 2. UI E2E Critical Flow Scope

Playwright, Chromium only (per architecture decision). Target `web/e2e/`. Remove `test-e2e` `continue-on-error: true` once these land.

### P0 — Must pass before quality gate

| ID | Flow | UX-DR refs | Depends on |
|----|------|-----------|-----------|
| **UI-E2E-01** | **FR52-web SPA Smoke** — replace root `e2e/smoke.spec.ts` `test.todo()` (or repoint CI to `cd web && npx playwright test`). Load `/production`, assert `StatusBar` + `StageStepper` + `Sidebar` + `RunCard` render. | — | CP-3. |
| **UI-E2E-02** | **Dashboard → Scenario Inspector → Inline Narration Edit → Save** — visit `/production` → select run → click scene → edit narration → blur-save → reload → verify persisted; exercise Ctrl+Z revert; **also fires the InlineNarrationEditor baseline re-sync bug** (deferred 7-2). | UX-DR26, 40 | — |
| **UI-E2E-03** | **Character Pick → Vision Descriptor → Freeze** — `CharacterGrid` → direct-select 1-9/0 → `VisionDescriptorEditor` prefill → save → `frozen_descriptor` persists; covers handoff to Phase B image track. | UX-DR17, 41, 62 | — |
| **UI-E2E-04** | **Batch Review Full Keyboard Chord** — seed ≥10 scenes in `batch_review` → J/K nav → Enter approve / Esc reject / Tab skip / S skip-and-remember / Shift+Enter batch-approve-all → Ctrl+Z across mixed decisions; assert `Focus-Follows-Selection` never-empty-detail + optimistic UI < 100ms. | UX-DR18, 23, 24, 33, 34, 38 | AI-2. **This is the core HITL value with zero Playwright today.** |
| **UI-E2E-05** | **Rejection + Regeneration + Retry-Exhausted** — reject with inline note → progress overlay → max 2 retries → retry-exhausted state disables "Manual edit" CTA with correct tooltip; verifies `RetryExhausted` threshold after the `>=`/`>` unification. | UX-DR39, 65 | AI-3 unification landed. |
| **UI-E2E-06** | **ComplianceGate Ack → Ready-for-Upload** — Phase C completes → `CompletionReward` → metadata-ack button → `POST /ack-metadata` → `runs.stage` transitions → next-action CTA visible. | UX-DR42, 66 | AI-4. FR23 hard gate currently has zero E2E coverage. |

### P1 — Mitigate before shipping

| ID | Flow | UX-DR refs | Depends on |
|----|------|-----------|-----------|
| **UI-E2E-07** | **Tuning Surface End-to-End** — `/tuning` → edit Critic prompt → save version → run Golden → Shadow gated behind Golden (AC-6) → diff view. | — | CP-5 Story 10-2 FULL. |
| **UI-E2E-08** | **Settings Save → Dynamic Phase-B Config** — `/settings` → change cost cap or model → Save → verify `dynamicPhaseBExecutor` picks up new config; surfaces DF1 (re-parse on every invocation) if regression. | — | — |
| **UI-E2E-09** | **Run Inventory Search + Filter** — multi-run DB → Sidebar search → filter by stage/status → select → `RunCard` scoped. | UX-DR63 | — |
| **UI-E2E-10** | **FailureBanner → Enter Resume** — seed failed run → `FailureBanner` → Enter → `runs.status=running`; verifies keyboard-driven resume. | UX-DR16 | — |

**P2 for this surface:** milestone banners (UX-DR57), responsive 1024–1279px maintain (UX-DR60), `prefers-reduced-motion` (UX-DR35). Document and defer.

---

## 3. `deferred-work.md` Filter — Quality-Gate Must-Fix

420 lines, ~250 distinct items. Classified below using probability × impact. P0 items **block** the quality gate; P1 items need a mitigation owner; P2 items are documented accept.

### P0 — BLOCK (score ≥ 6 and on the critical path)

| Item | Source | Risk | Owner / handoff |
|------|--------|------|-----------------|
| **Engine.Advance stub** | retro CP-1, deferred 2-4 / 3-1 / 3-5 | 9 (3×3) — no real E2E exists | Next `/bmad-create-story` (Story 11.1 or 3.6 expansion) |
| **LoadShadowInput production-path bug** | deferred 4-2 | 6 (3×2) — live-DB Shadow fails every case | AI-5 or Story 10-2 |
| **metadata.json + manifest.json non-atomic pair** | deferred 9-2 W1 | 6 (2×3) — data integrity on Phase C success path | Phase C hardening story |
| **`xfade` offset negative on shot < 0.5s** | deferred 9-2 W3 | 6 (2×3) — FFmpeg undefined behavior, silent corruption risk | Phase C hardening story |
| **`probeDuration = 0` silently passes tolerance** | deferred 9-2 W4 | 6 (2×3) — partial-failure clip fake-passes validation | Phase C hardening story |
| **Root `e2e/smoke.spec.ts` is `test.todo()`** | retro §4.2 + 6-5 | 6 (3×2) — FR52-web canonical smoke disabled | CP-3 |
| **`test-e2e` `continue-on-error: true`** | retro §4.2 + 2-1 | 6 (3×2) — masks real failures | CP-3 follow-up |
| **FFmpeg not installed in CI `test-go` job** | retro CP-4 | 6 (3×2) — Phase C tests skip silently | CP-4 (one-line YAML) |
| **`go-version: '1.25.7'` doesn't exist** | retro SD-4 + 10-4 | 4 (2×2) — `actions/setup-go` silent resolution; promoted to P0 because it pollutes every downstream test decision | CP-4 adjacent |
| **Migration 004 ordinal collision** | retro CP-6 / SD-5 + 2-5 + 2-6 | 6 (3×2) — fresh clones in inconsistent state | Next migration-touching story |
| **Text-LLM runtime missing** (DeepSeek/Gemini are docs only) | retro SD-3 | 9 (3×3) — FR12 Writer≠Critic unsatisfiable | CP-5 Story 10-2 FULL |
| **Story 10-2 tuning surface unbuilt** | retro CP-5 / SD-2 / PT-1 | 9 (3×3) — per Jay 2026-04-24 FULL scope | CP-5 |

### P1 — MITIGATE (score 5–7, needs mitigation owner + deadline)

| Item | Source | Risk | Mitigation |
|------|--------|------|-----------|
| `RetryExhausted` `>` vs `>=` 3-site mismatch | 8.4 / 8.5 / 8.6 deferred | 6 (2×3) | Unify on `>=` via shared const + property test across `ListReviewItems` / `RecordSceneDecision` / `DispatchSceneRegeneration` |
| `BatchApprove` undo non-normalized `aggregate_command_id` | 8-6 deferred | 6 (2×3) | Server-side `aggregate_command_id` assembly in `RecordSceneDecision`; not caller-trusted |
| `approved_scene_indices` O(N²) storage | 8-5 deferred | 5 (1×5 est. — scaling cliff) | New `batch_commands` table + migration; duplication ceiling bounded |
| Undo stack never GC'd on run switch | 8-5 deferred | 4 (2×2) | Call `clear_undo_stack` on run-switch navigation |
| `Ctrl+Z` only, macOS `Cmd+Z` unmapped | 8-5 deferred | 4 (2×2) — macOS operators blocked | Map Cmd → Ctrl in `keyboardShortcuts.ts` |
| Concurrent `pipeline export` races | 10-5 deferred | 4 (2×2) | Write-to-temp + `os.Rename` for atomic publish |
| `RunGolden` mutates `testdata/golden/eval/manifest.json` | 10-4 deferred | 4 (2×2) | `--dry-run` flag for dev invocation |
| `warnings` field nesting drift Go ↔ fixture ↔ Zod | 6-5 deferred | 6 (3×2) — contract tests cover fixture, not handler | Fix 3 sites + add handler↔fixture round-trip test |
| `/api/runs/{id}/status` no Zod counterpart | 6-5 deferred | 4 (2×2) | Add when Timeline view consumes it |
| `spa.go` serves `index.html` 200 for `/assets/*` misses | 6-1 deferred | 6 (2×3) — stale-build masking | Return 404 for hashed asset misses |
| **Test-double fidelity audit** (tautological assertions) | retro §4.3 + 5-4 + 7-1 deferred | 6 (2×3) — false-confidence risk | Run `/bmad-testarch-test-review` scoped to `*_test.go` files flagged in deferred |
| FR53 cites cancelled/failed source runs | 8-4 deferred | 4 (2×2) | Add stage filter to `PriorRejectionForScene` join |
| Rate-limiter `fn` goroutine outlives `Do` on timeout | 5-1 deferred | 6 (2×3) — MaxConcurrent silently exceeded | Local fix: wait for `resultCh` before releasing, OR contract ctx honored |
| `BlockExternalHTTP` mutex missing (global DefaultTransport swap) | 1-4 / 1-7 / 2-1 deferred | 4 (2×2) | Mutex before `t.Parallel()` adoption |
| `AcknowledgeMetadata` no `MaxBytesReader` | 9-4 deferred | 4 (2×2) | Hardening; endpoint ignores body but reads per connection limits |
| `CountRegenAttempts` counts superseded reject rows | 8-4 / 8-5 / 8-6 deferred | 4 (2×2) — doc/behavior mismatch | Scope to active rejects OR document intentional ceiling |
| Shadow `normalizeCriticScore` silent clamp | 4-2 deferred | 4 (2×2) | Warn on out-of-range evaluator output |

### P2 — DOCUMENT (score ≤ 4, ship-through acceptable)

Selected representatives; full set stays in `deferred-work.md`:

- WAL sidecar 0600 permissions (localhost-only)
- Symlink `EvalSymlinks` guard in `handler_artifacts.go`
- Single-quote escape in FFmpeg concat list (runDir system-generated)
- `CharacterPick` "Press 1-9 or 0" hint misleading on < 10 candidates
- Zod `version: z.number().int().nonnegative()` accepts future v2 (forward-compat trap)
- NULL `scene_id` can anchor kappa computation
- `localStorage` `QuotaExceededError` handling
- SSR hydration two-pass guard in `useViewportCollapse`
- `prefers-reduced-motion`, AltGr keyboard-layout normalization
- Korean bag-of-words FP (V1.5 embedding swap)
- Duration tolerance accumulates codec-level rounding > 0.1s for > 20-clip videos
- `computed_at TEXT` no RFC3339 CHECK
- `runIDs > 999` hits SQLite bound-parameter limit
- Project-wide FK constraints absent (convention, not bug)
- Unicode `runID` filesystem normalization (APFS/HFS+)
- `command_kind` invariant when caller omits from `context_snapshot`
- `skip_and_remember` undo no `ApplyUndo` handler (works by accident)

**Intentionally deferred:** scope-leakage process issue (retro §4.5) is already mitigated by Jay's commit-scope discipline (memory `feedback_commit_scope.md`). No code change, no test — behavioral rule.

---

## 4. NFR Risk Hotspots

Scored against architecture NFR drivers (`architecture.md §Requirements Overview`). Categories: Performance (P3/P4), Reliability (R1/R2/R3/R4), Data Integrity (cross-cutting), Cost (C1/C2/C3), Accessibility (A1/A2).

### Performance — Lowest urgency, well-covered at unit level

- **Hotspot: CI ≤ 10 min budget** (NFR-T1 implied). Adding 8 pipeline E2E + 10 UI E2E lands roughly 3–5 min of additional runtime. Mitigation: canonical fixture for SMOKE-01 tuned to 60-second equivalent run; Playwright parallelism stays at Chromium-only; FFmpeg install adds ~30s one-time. Budget holds with margin.
- **Phase B rate-limit coordination under real load** is unmeasured. The `total ≤ max(image, TTS) × 1.2` overlap invariant has no production evidence. **Not blocking V1 ship** (single-operator, small batches), but flag for post-V1 k6-lite load test as AI-6-adjacent.
- **`fn` goroutine outliving `Do` on timeout** (5-1) is the concrete perf-correctness bug: can silently exceed `MaxConcurrent`. P1 above.

### Reliability — Highest urgency

- **NFR-R1 resume idempotency.** Per-stage resume tests exist. **Cross-phase resume has zero tests.** SMOKE-03 covers golden path; add per-stage fuzz in integration (each of 15 stages × mid-kill × resume) as follow-up.
- **NFR-R2 anti-progress FP rate.** Detector wired, but never fires in E2E today. SMOKE-P2 fixture seeds repeated outputs. Korean-weak bag-of-words is acknowledged V1.5 migration (P2).
- **NFR-R3 durable SQLite writes.** WAL + `busy_timeout=5000` holds. **metadata.json + manifest.json pair write is the concrete non-durability bug** — SMOKE-05 demands atomicity fix first. Migration 004 collision (CP-6) is a related durability concern (dev DB state divergence).
- **NFR-R4 web UI stateless.** `localStorage` quota/corruption untested (deferred 6-2). Not a ship blocker but UX regression risk on long sessions. P2.

### Data Integrity — Cross-cutting, highest signal

The largest concentration of silent-risk deferred items:

1. **Metadata ↔ manifest non-atomic pair** (9-2 W1) — P0
2. **`approved_scene_indices` O(N²) with logical duplication** (8-5) — P1
3. **`RetryExhausted` 3-site `>=`/`>` mismatch** (8-4/5/6) — P1
4. **`BatchApprove` cross-run `aggregate_command_id` blast radius** (8-6) — P1
5. **FR53 cites cancelled/failed source runs** (8-4) — P1
6. `decisions.scene_id` TEXT vs INT (long-term migration) — P2
7. `created_at` TEXT lexicographic ordering mixed-format — P2
8. Duplicate non-superseded approve+reject scene row counting (2-6) — masked by `PendingCount < 0` clamp — P2 pending Epic 8 enforcement

**Test strategy for data integrity:** property tests for `RetryExhausted` threshold consistency across 3 sites; fault-injection test for metadata+manifest atomicity; server-side normalization test for `BatchApprove` undo blast radius; join-stage-filter test for FR53 prior-rejection source run hygiene.

### Cost (NFR-C1/C2/C3)

Cost cap + accumulator are well-tested at unit + integration. E2E-SMOKE-04 closes the real-run gap. **No outstanding P0/P1 cost-correctness risks.**

### Security (implied, not a numbered NFR)

Defense-in-depth for external APIs holds (constructor injection → `BlockExternalHTTP` runtime guard → CI secret lockout). Two P1 items: (a) `BlockExternalHTTP` global-mutation mutex, (b) `AcknowledgeMetadata` `MaxBytesReader`. Symlink traversal in `handler_artifacts.go` is P2 (attacker model weak).

### Accessibility (NFR-A1/A2)

8-key shortcut set is V1 non-negotiable. Present at runtime; Playwright UI-E2E-04 covers the full chord. Listbox `aria-activedescendant` for J/K is P2 (UX polish, deferred 8-1).

---

## 5. Handoff to Step 3 — Concrete Test-Design Input

For `/bmad-testarch-test-design`, the Step 3 call should be seeded with the following:

### 5.1 Prioritized scenario list

18 scenarios tagged `@p0` / `@p1` / `@p2`:

- **Pipeline E2E:** SMOKE-01 … SMOKE-08 (see §1)
- **UI E2E:** UI-E2E-01 … UI-E2E-10 (see §2)

### 5.2 Test level allocation (per `test-levels-framework.md`)

| Concern | Unit | Integration | E2E (Go) | E2E (Playwright) |
|---------|------|-------------|----------|------------------|
| State machine transitions | ✅ rich (existing) | ✅ existing | SMOKE-01 covers golden | — |
| Cost accumulator / kappa / similarity | ✅ existing | — | SMOKE-04 | — |
| Agent chain handoff | ✅ existing | SMOKE-02 | SMOKE-01 | — |
| Stage resume (per-stage fuzz) | — | **new** (15 stages × mid-kill) | SMOKE-03 | — |
| Shadow live-DB path | — | SMOKE-06 | — | — |
| Metadata+manifest atomicity | — | SMOKE-05 (fault-injection) | — | — |
| Phase C real-MP4 render | — | SMOKE-02 | SMOKE-01 | — |
| RetryExhausted threshold | — | **new** property test × 3 sites | — | UI-E2E-05 exercises UI-observable behavior |
| Batch review keyboard chord | — | — | — | UI-E2E-04 |
| ComplianceGate ack → ready | — | SMOKE-07 | — | UI-E2E-06 |
| Tuning surface | — | — | — | UI-E2E-07 (post-CP-5) |
| Contract round-trip (handler↔fixture↔Zod) | — | **new** (`warnings` drift fix) | — | — |

### 5.3 Fixture strategy

1. **Canonical seed** — `testdata/e2e/scp-049-seed/` — raw corpus, per-stage mock-provider responses (DashScope text + **DeepSeek text** for Writer≠Critic pair, DashScope image, DashScope TTS bytes), expected output manifest. Drives SMOKE-01/02/03.
2. **Fault-injection adapters** — `internal/pipeline/fi/` (new) — failing writer, timeout rate-limiter, partial-probe FFmpeg fake, non-atomic metadata writer. Required for SMOKE-05 + reliability fuzz.
3. **Golden** — `testdata/golden/eval/` stays canonical. Add one "fail this gate" regression fixture to exercise the Critic-file-path filter end-to-end.
4. **Playwright** — page-object per shell (`ProductionShell`, `BatchReviewShell`, `TuningShell`), shared `renderWithProviders`-adjacent fixture for zustand reset between tests (fix deferred 6-5 singleton issue first).

### 5.4 Mock boundary (per architecture Tier-1)

- Interface-injected `TextGenerator` (two implementations: DashScope + **DeepSeek** per CP-5 decision 2026-04-25), `ImageGenerator`, `TTSSynthesizer`.
- Real: SQLite WAL (not `:memory:`), real filesystem, real FFmpeg binary (install in CI `test-go`).
- `PIPELINE_ENV=test` blocks real HTTP via `BlockExternalHTTP`. **Prerequisite:** add mutex (deferred 1-4) before enabling `t.Parallel()` in E2E.

### 5.5 CI wiring prerequisites (Step 3 must assume all true)

- [ ] Remove `test-e2e` `continue-on-error: true` (after UI-E2E-01 lands) — CP-3
- [ ] Replace root `e2e/smoke.spec.ts` OR repoint CI to `cd web && npx playwright test` — CP-3
- [ ] Install FFmpeg in `test-go` job via `- run: sudo apt-get update && sudo apt-get install -y ffmpeg` before the test step — CP-4 (decided 2026-04-25)
- [ ] Pin `go-version` to a real Go release (1.25.7 doesn't exist; use latest 1.25.x confirmed) — SD-4
- [ ] Unskip `TestE2E_FullPipeline` — CP-2 (after CP-1 wiring)
- [ ] Flip `fr-coverage.json` to strict mode after CP-5 lands — AI-8

### 5.6 Gate decision criteria

Per `risk-governance.md`:

| Decision | Criteria |
|----------|----------|
| **PASS** | All 8 P0 pipeline scenarios green; all 6 P0 UI flows green; all 12 P0 deferred items closed; no P1 item OPEN without mitigation owner + deadline. |
| **CONCERNS** | P0 green, but P1 items OPEN without owner; OR one P0 deferred item waived with expiry. |
| **FAIL** | Any P0 pipeline/UI scenario red; OR any of 12 P0 deferred items unresolved; OR `TestE2E_FullPipeline` / `FR52-web smoke` still skipped. |
| **WAIVED** | All P0 items resolved; P1 items batch-waived by Jay with expiry (single-operator project — Jay is the authorized approver). |

### 5.7 Traceability seed for Step 5

If `/bmad-testarch-trace` runs in Step 5, map:

- **FR1–FR53** → scenarios above (SMOKE-01 covers the core FR1–FR52 golden path; UI-E2E-02/03/04/05/06 cover FR31a–FR37, FR53)
- **NFR-C1/C2/C3** → SMOKE-04
- **NFR-R1** → SMOKE-03 + new per-stage resume fuzz
- **NFR-R2** → deferred (V1.5 embedding); P2 fixture fires detector
- **NFR-R3** → SMOKE-05 (after atomicity fix)
- **NFR-R4** → partial (deferred; not ship-blocking)
- **NFR-P3/P4** → existing unit + integration (FakeClock, 429 backoff tests)
- **NFR-A1/A2** → UI-E2E-04 (full 8-key chord)
- **NFR-T1** → CI ≤ 10 min, monitored post-E2E land

---

## 6. Summary — What Step 3 Should Produce

1. Per-scenario given/when/then + fixture requirements for the 18 scenarios in §5.1
2. Implementation effort estimate per scenario (conservative; Jay operates solo)
3. Test-data plan for `testdata/e2e/scp-049-seed/` + `internal/pipeline/fi/`
4. CI YAML diff for §5.5 prerequisites
5. Explicit Step 4 handoff: "these N UI scenarios go to `/bmad-qa-generate-e2e-tests`"

---

## 7. Resolved Decisions (Jay, 2026-04-25)

| # | Decision | Downstream effect |
|---|----------|-------------------|
| 1 | **Second text provider for FR12 = DeepSeek (primary), Gemini (fallback/second-tier).** | Step 3 fixture strategy targets DeepSeek as the real second adapter. `internal/llmclient/deepseek/` ships as part of CP-5. Gemini stays as doc-only in V1, scheduled for V1.5. Writer≠Critic enforcement: DashScope (Writer) vs DeepSeek (Critic), or swap. |
| 2 | **FFmpeg CI install = `apt-get install ffmpeg` in the `test-go` job step.** | One-line YAML add. ~30s cold install per run. Simpler than container caching; acceptable given CI budget headroom. |
| 3 | **Step 3 scope = all 18 scenarios in one pass.** | Step 3 produces given/when/then + fixture + effort estimate for P0 + P1 + P2 together. Step 4 (`/bmad-qa-generate-e2e-tests`) still picks which UI flows to generate first based on Step 3 effort ranking. |
| 4 | **Tautological-double audit (`/bmad-testarch-test-review`) = serialized after Step 4.** | Not a Step 3 side-task. Runs after UI E2E tests land so the review has the full picture. Step 3 still flags candidate files in §5.3 fixture strategy; the actual audit is a later command. |

---

## 8. Risk Matrix Visualization

```
           Probability →
Impact ↓   Unlikely (1)    Possible (2)         Likely (3)
Critical   ● E2E-SMOKE-05  ● xfade<0.5s         ● Engine.Advance (9)
(3)        probe=0         metadata-atomic      ● Text-LLM missing (9)
           goroutine-lifetime                   ● 10-2 unbuilt (9)

Degraded   ● WAL perms     ● RetryExhausted     ● Shadow path (6)
(2)        ● symlink       3-site (4)           ● FR52-web todo (6)
                           ● warnings drift (4) ● CI continue-on-error (6)
                           ● spa.go 404 mask(6) ● ffmpeg missing (6)
                                                ● migration 004 (6)

Minor      ● hint text     ● perf polish        ● Cmd+Z unmapped (4)
(1)                        ● N+1 queries
```

All "Critical × Likely" cells are P0 — 3 blockers (Engine.Advance, Text-LLM, 10-2). All are covered by retro CP-1/CP-5 and addressed by the Step 3 test plan above.

---

_Artifact produced by Murat (bmad-tea) on 2026-04-25. Primary input: `epic-1-10-retro-2026-04-24.md`. Next slash command: `/bmad-testarch-test-design` with §5 as the prompt seed._
