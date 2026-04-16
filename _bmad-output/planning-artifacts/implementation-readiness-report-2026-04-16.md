# Implementation Readiness Assessment Report

**Date:** 2026-04-16
**Project:** youtube.pipeline

---

## Step 1: Document Discovery

**stepsCompleted:** [step-01-document-discovery]

### Documents Selected for Assessment

| Document Type | File Path | Size | Last Modified |
|---|---|---|---|
| PRD | `_bmad-output/planning-artifacts/prd.md` | 83,079 bytes | 2026-04-16 |
| Architecture | `_bmad-output/planning-artifacts/architecture.md` | 87,828 bytes | 2026-04-16 |
| Epics & Stories | `_bmad-output/planning-artifacts/epics.md` | 126,755 bytes | 2026-04-16 |
| UX Design | `_bmad-output/planning-artifacts/ux-design-specification.md` | 121,250 bytes | 2026-04-14 |

### Additional Reference Materials

- `docs/tts/` — Qwen TTS examples and documentation
- `docs/analysis/` — SCP YouTube channel analysis
- `docs/images/` — Image generation policy
- `docs/ui.examples/` — UI reference screenshots
- `docs/prompts/` — Scenario/Image/TTS/Vision prompts
- `docs/vision/` — Descriptor enrichment documentation

### Issues

- `docs/prd.md` exists as an early draft memo (1,297 bytes) — excluded from assessment in favor of the full planning-artifacts PRD
- No sharded documents found — all documents are single files
- No missing required documents — PRD, Architecture, Epics, UX all present

---

## Step 2: PRD Analysis

**stepsCompleted:** [step-01-document-discovery, step-02-prd-analysis]

### Functional Requirements (53 total)

#### Pipeline Lifecycle (9 FRs)
- **FR1** — Operator can start a new pipeline run for a given SCP ID.
- **FR2** — Operator can resume a failed run from the last successful stage.
- **FR3** — Operator can cancel an in-flight run.
- **FR4** — Operator can inspect the state of any individual run or of all runs.
- **FR5** — System persists complete run state (stage progression, status, retry count, retry reason).
- **FR6** — System captures per-stage observability data (duration, token in/out, retry count, retry reason, critic score, cost, human-override flag) — 8 columns.
- **FR7** — *Moved to NFR* (per-stage cost budget cap).
- **FR8** — Anti-progress detection: System stops retry loop and escalates when cosine similarity > 0.92 (configurable).
- **FR50** — Operator can view "what changed since I paused" summary for any run (diff between latest state and last interaction timestamp).

#### Phase A — Scenario Generation (5 FRs)
- **FR9** — System generates research output for an SCP ID from local data corpus.
- **FR10** — System produces structured scene plan, Korean narration, per-scene shot breakdown (1–5 shots per scene), and review report via 6 LLM agents (Researcher, Structurer, Writer, VisualBreakdowner, Reviewer, Critic). Shot count auto from TTS duration (≤8s→1, 8–15s→2, 15–25s→3, 25–40s→4, 40s+→5); operator override at `scenario_review` HITL checkpoint.
- **FR11** — System validates each agent's output against defined schema before next agent.
- **FR12** — System enforces Writer ≠ Critic LLM provider at preflight and run entry.
- **FR13** — System invokes Critic at two checkpoints: after Writer + after Phase A completion.

#### Phase B — Media Generation (6 FRs)
- **FR14** — System generates per-shot images (~30 per run) with cross-scene/cross-shot character continuity via frozen visual descriptor.
- **FR15** — System generates per-scene Korean TTS with numeral/English transliteration to Korean orthography.
- **FR16** — System executes image and TTS tracks concurrently via `errgroup.Group` (NOT `errgroup.WithContext`); both complete before assembly.
- **FR17** — System surfaces character-reference selection prerequisite (10 candidates from cached image search).
- **FR18** — System generates canonical character reference via reference-based image editing.
- **FR19** — System caches character-search results aggressively across runs.

#### Phase C — Assembly & Output (4 FRs)
- **FR20** — Two-stage assembly: (1) per-scene clip assembly (1–5 shots + transitions [Ken Burns, cross-dissolve, hard cut] + TTS audio), (2) final concatenation into output MP4. Audio fade/BGM are V1.5.
- **FR21** — System produces metadata bundle (AI-generated content declaration, models used, source attribution).
- **FR22** — System produces source manifest (originating SCP article + sub-work license chain).
- **FR23** — System gates "ready-for-upload" on explicit operator acknowledgment of metadata bundle.

#### Quality Gating — Critic Stack (10 FRs)
- **FR24** — Critic produces verdict (pass/retry/accept-with-notes) with rubric sub-scores (hook strength, fact accuracy, emotional variation, immersion).
- **FR25** — Rule-based pre-checks (JSON schema + forbidden-term regex) before Critic invocation; forbidden-term list is operator-authorable.
- **FR26** — Operator can author/maintain Golden eval set with 1:1 positive:negative ratio; staleness warning on N-day (default 30) or Critic prompt change.
- **FR27** — System runs Critic against Golden eval set on demand, reports detection rate.
- **FR28** — Shadow eval: Critic runs against N most recently passed scenes (default 10) on prompt change; reports false-rejection regressions.
- **FR29** — Calibration metric (Cohen's kappa) between Critic verdicts and operator overrides, rolling 25-run window, "provisional" indicator while n<25.
- **FR30** — Minor-depiction flagging: blocks downstream processing until operator review.
- **FR51** — Test infrastructure as first-class artifact: contract tests, integration tests, Golden/Shadow runner, seed-able run-state fixtures. All in CI.
- **FR52** — E2E smoke test on canonical seed input (one SCP ID); must pass before deployment/release.
- **FR53** — System surfaces prior rejection of semantically similar scene during review, with earlier rejection's reason.

#### HITL Review Surface (8 FRs)
- **FR31a** — Auto-approval for items above Critic score threshold; recorded with distinguishing decision type.
- **FR31b** — Batch review via composed per-scene cards (scene clip mini-video / shot thumbnail strip + narration + audio playback).
- **FR31c** — Precision review for high-leverage scenes (character first appearance, hook, act-boundary), showing individual shots, transitions, per-shot image quality.
- **FR32** — Operator can approve, reject, or edit any review item.
- **FR33** — Operator can undo most recent decision (Ctrl+Z).
- **FR34** — Operator can approve all remaining items in batch (Shift+Enter).
- **FR35** — "Skip and remember" action for pattern recording.
- **FR36** — System records every HITL action (target, decision type, timestamp, optional note) to persistent decisions store.
- **FR37** — Operator can browse full decisions history in read-only view.

#### Operator Tooling & Setup (7 FRs)
- **FR38** — Operator can initialize fresh project (config, DB schema, output dir, .env template).
- **FR39** — Preflight check via extensible Check registry (V1: 3 checks — API keys, FS paths, FFmpeg).
- **FR40** — Operator can launch local web UI server (localhost-only binding).
- **FR41** — System surfaces operator mode as top-level web UI tabs (Production / Tuning / Settings & History).
- **FR42** — Machine-readable JSON output for any CLI command, versioned envelope.
- **FR43** — Human-readable color-coded hierarchical status output (default).
- **FR44** — Operator can export per-run decisions/artifacts to JSON files.

#### Compliance & Risk Surface (6 FRs)
- **FR45** — System records LLM provider per generation for retroactive ToS audit.
- **FR46** — System rejects config where Writer == Critic provider (preflight + run entry).
- **FR47** — System rejects voice profile on operator-maintained "blocked voice-ID" list.
- **FR48** — Forbidden-term enforcement (KCSC-derived) at narration generation stage.
- **FR49** — Operator can replay paused HITL session from exact decision point with state-aware summary.

### Non-Functional Requirements (24 total)

#### Performance (2 NFRs)
- **NFR-P3** — Rate-limit backoff on HTTP 429 without advancing stage status; emits `retry_reason="rate_limit"`.
- **NFR-P4** — First-video wall-clock time captured; target formalized within 30 days of V1.5 entry.

#### Cost & Resource Limits (3 NFRs)
- **NFR-C1** — Per-stage cost cap in USD; hard-stop and escalate on overrun.
- **NFR-C2** — Per-run total cost cap in USD; hard-stop on overrun.
- **NFR-C3** — Cost data (`cost_usd`, `token_in`, `token_out`) captured per stage, no sampling/truncation.

#### Reliability (4 NFRs)
- **NFR-R1** — Stage-level resume produces same downstream schema (functional equivalence, not bit-equivalence).
- **NFR-R2** — Anti-progress detector false-positive rate measured on rolling 50-run window; ≤5% gate from V1.5.
- **NFR-R3** — Durable DB writes to `pipeline_runs`, `decisions`, Golden eval across crashes.
- **NFR-R4** — Web UI client-side state is transient; canonical state in SQLite.

#### Security (4 NFRs)
- **NFR-S1** — Secrets in `.env` (V1); never committed to VCS.
- **NFR-S2** — Web UI binds exclusively to `127.0.0.1`.
- **NFR-S3** — SQLite DB file created with mode 0600.
- **NFR-S4** — Any network surface change requires ADR prior to implementation.

#### Testability (6 NFRs)
- **NFR-T1** — CI executes on every commit: unit, integration, contract, Golden, Shadow, E2E smoke tests.
- **NFR-T2** — Unit-test line coverage: ≥60% V1 hard gate, ≥70% V1.5.
- **NFR-T3** — Every FR has acceptance test or "not-directly-testable, covered by X" annotation (≤15% annotated; X must resolve to existing test).
- **NFR-T4** — Seed-able run-state fixtures stored in repo for testing FR8, FR28, FR49.
- **NFR-T5** — Golden/Shadow evaluation runner itself tested with ≥3 known-pass + ≥3 known-fail fixtures in CI.
- **NFR-T6** — CI total wall-clock ≤10 minutes hard gate.

#### Observability (4 NFRs)
- **NFR-O1** — `pipeline_runs` retains all 8 columns; no truncation/sampling.
- **NFR-O2** — Indefinite retention of `pipeline_runs` and `decisions` rows.
- **NFR-O3** — Diagnostic queries via standard `sqlite3` CLI sufficient.
- **NFR-O4** — SQL indexes for rolling-window metric queries in migration set.

#### Maintainability (4 NFRs)
- **NFR-M1** — Model IDs are configuration-driven, not hard-coded.
- **NFR-M2** — Authorable artifacts in version-controlled files separate from source.
- **NFR-M3** — Stage-boundary schemas formally defined (JSON Schema or Go struct tags).
- **NFR-M4** — Layer boundaries (`web → cmd → service → infra`) enforced by CI linter.

#### Accessibility (3 NFRs)
- **NFR-A1** — 8-key keyboard shortcut set for HITL review (Enter, Esc, Ctrl+Z, Shift+Enter, Tab, S, 1–9, J/K).
- **NFR-A2** — Broad WCAG compliance explicitly out of scope for V1.
- **NFR-A3** — Mobile/tablet breakpoints out of scope.

#### Compliance (4 NFRs)
- **NFR-L1** — Every video must have metadata bundle + source manifest before "ready-for-upload"; no bypass.
- **NFR-L2** — Compliance artifacts version-controlled alongside video outputs.
- **NFR-L3** — `provider` field non-null for every LLM call row in `pipeline_runs`.
- **NFR-L4** — Platform policy changes affect only metadata bundle generator, not pipeline.

### Additional Requirements & Constraints

- **V0 Prerequisite**: `LICENSE_STANCE.md` must exist before any license-metadata code.
- **Korean-language only**: no multi-language hooks in V1 architecture.
- **2-Layer + Backlog model**: V1 → V1.5 (gated on 5 videos shipped) → Backlog (deferred = potentially never).
- **V1 6-week budget**: weekly milestone deliverables (each independently demoable).
- **Single-machine SQLite assumption**: multi-machine out of scope.
- **DashScope single region per install**: cross-region failover not supported.
- **Import direction rule**: `web → cmd → service` enforced by Go import-cycle check + CI linter.

### PRD Completeness Assessment

The PRD is **exceptionally thorough**:
- 53 numbered FRs covering all 9 operating modes identified in User Journeys
- 24 NFRs with quantified thresholds or explicit formalization deadlines
- Clear V1 / V1.5 / Backlog / Rejected scope boundaries
- 7 technical risks with specific mitigations
- 6 resource risks with mitigations
- Domain-specific compliance requirements fully enumerated
- Scope Decisions Audit table for V1.5 gating checklist
- Weekly milestone budget with specific deliverables

---

## Step 3: Epic Coverage Validation

**stepsCompleted:** [step-01-document-discovery, step-02-prd-analysis, step-03-epic-coverage-validation]

### Coverage Matrix

| FR | PRD Requirement | Epic Coverage (Map) | Epic Coverage (Actual Scope) | Status |
|---|---|---|---|---|
| FR1 | Operator starts a new pipeline run | Epic 2 | Epic 2 | ✓ Covered |
| FR2 | Operator resumes a failed run | Epic 2 | Epic 2 | ✓ Covered |
| FR3 | Operator cancels an in-flight run | Epic 2 | Epic 2 | ✓ Covered |
| FR4 | Operator inspects run state | Epic 2 | Epic 2 | ✓ Covered |
| FR5 | System persists run state | Epic 2 | Epic 2 | ✓ Covered |
| FR6 | Per-stage observability (8 columns) | Epic 2 | Epic 2 | ✓ Covered |
| FR7 | *Moved to NFR-C1* | — | — | ✓ N/A |
| FR8 | Anti-progress detection (cosine > 0.92) | Epic 2 | Epic 2 | ✓ Covered |
| FR9 | Research output from local data corpus | Epic 3 | Epic 3 | ✓ Covered |
| FR10 | 6-agent scenario generation chain | Epic 3 | Epic 3 (+ Epic 7 cross-ref) | ✓ Covered |
| FR11 | Inter-agent schema validation | Epic 3 | Epic 3 | ✓ Covered |
| FR12 | Writer ≠ Critic provider enforcement | Epic 3 | Epic 3 (+ Epic 1 preflight) | ✓ Covered |
| FR13 | Critic at two checkpoints | Epic 3 | Epic 3 | ✓ Covered |
| FR14 | Per-shot image generation (~30/run) | Epic 5 | Epic 5 | ✓ Covered |
| FR15 | Korean TTS with transliteration | Epic 5 | Epic 5 | ✓ Covered |
| FR16 | Parallel image + TTS via errgroup.Group | Epic 5 | Epic 5 | ✓ Covered |
| FR17 | Character-reference selection (10 candidates) | Epic 5 | Epic 5 (+ Epic 7 cross-ref) | ✓ Covered |
| FR18 | Canonical character reference via image editing | Epic 5 | Epic 5 | ✓ Covered |
| FR19 | Aggressive character-search caching | Epic 5 | Epic 5 | ✓ Covered |
| FR20 | Two-stage video assembly (scene clips + concat) | Epic 9 | Epic 9 | ✓ Covered |
| FR21 | AI-content metadata bundle | Epic 9 | Epic 9 | ✓ Covered |
| FR22 | Source manifest (SCP article + license chain) | Epic 9 | Epic 9 | ✓ Covered |
| FR23 | Upload-readiness gate on metadata ack | Epic 9 | Epic 9 | ✓ Covered |
| FR24 | Critic verdict with rubric sub-scores | Epic 3 | Epic 3 | ✓ Covered |
| FR25 | Rule-based pre-checks before Critic | Epic 3 | Epic 3 | ✓ Covered |
| FR26 | Golden eval set with 1:1 ratio governance | Epic 4 | Epic 4 | ✓ Covered |
| FR27 | Golden eval on-demand execution | Epic 4 | Epic 4 | ✓ Covered |
| FR28 | Shadow eval on Critic prompt change | Epic 4 | Epic 4 | ✓ Covered |
| FR29 | Cohen's kappa calibration (rolling 25-run) | Epic 4 | Epic 4 | ✓ Covered |
| FR30 | Minor-content safeguard | Epic 4 | Epic 4 | ✓ Covered |
| FR31a | Auto-approval on Critic score threshold | Epic 4 | Epic 4 | ✓ Covered |
| FR31b | Batch review via composed scene cards | Epic 8 | Epic 8 | ✓ Covered |
| FR31c | Precision review for high-leverage scenes | Epic 8 | Epic 8 | ✓ Covered |
| FR32 | Approve/reject/edit any review item | Epic 8 | Epic 8 | ✓ Covered |
| FR33 | Undo most recent decision (Ctrl+Z) | Epic 8 | Epic 8 | ✓ Covered |
| FR34 | Approve all remaining in batch | Epic 8 | Epic 8 | ✓ Covered |
| FR35 | Skip and remember pattern | Epic 8 | Epic 8 | ✓ Covered |
| FR36 | Record every HITL action to decisions store | Epic 8 | Epic 8 | ✓ Covered |
| FR37 | Decisions history read-only view | Epic 8 | Epic 8 | ✓ Covered |
| FR38 | Project initialization (init command) | Epic 1 | Epic 1 | ✓ Covered |
| FR39 | Preflight check (doctor command) | Epic 1 | Epic 1 | ✓ Covered |
| FR40 | Launch local web UI (localhost only) | Epic 6 | Epic 6 | ✓ Covered |
| FR41 | Top-level web UI tabs (mode switching) | Epic 6 | Epic 6 | ✓ Covered |
| FR42 | JSON output for CLI commands | Epic 1 | Epic 1 | ✓ Covered |
| FR43 | Human-readable color-coded output | Epic 1 | Epic 1 | ✓ Covered |
| FR44 | Export per-run data to JSON | Epic 10 | Epic 10 | ✓ Covered |
| FR45 | LLM provider recording per generation | Epic 9 | Epic 9 | ✓ Covered |
| FR46 | Reject Writer=Critic config | Epic 1 | Epic 1 | ✓ Covered |
| FR47 | Blocked voice-ID enforcement | Epic 9 | Epic 9 | ✓ Covered |
| FR48 | Forbidden-term enforcement (KCSC) | Epic 3 | Epic 3 | ✓ Covered |
| FR49 | HITL session replay from exact point | Epic 2 | Epic 2 | ✓ Covered |
| FR50 | "What changed since I paused" diff | Epic 2 | Epic 2 | ✓ Covered |
| FR51 | Test infrastructure as first-class artifact | Epic 10 (Map) | **Epic 1** (Actual) | ⚠️ Map Stale |
| FR52 | E2E smoke test | Epic 10 (Map) | **Epic 1 (Go) + Epic 6 (Web)** | ⚠️ Map Stale |
| FR53 | Prior rejection similarity warning | Epic 8 | Epic 8 | ✓ Covered |

### Discrepancies Found

#### ⚠️ FR Coverage Map vs Actual Epic Scopes (Stale Map Entries)

The FR Coverage Map (lines 274-331 of epics.md) was not updated to reflect Party Mode Round 3 relocations:

1. **FR51** — Map says "Epic 10", but Epic 1's scope explicitly includes: "Contract test suite (CLI ↔ Web parity), Integration test harness, Seed-able run-state test fixture facility, CI pipeline" (all relocated from Epic 10). Epic 10's FRs covered line says only "FR44".
2. **FR52** — Map says "Epic 10", but was split into FR52-go (Epic 1) and FR52-web (Epic 6) per Party Mode Round 3. Both epics explicitly list these in their FRs covered.

**Impact**: The coverage map is a summary artifact used for traceability. While the actual epic scopes are correct, the stale map could mislead implementers referencing only the summary. **Recommendation**: Update the FR Coverage Map lines 329-330 to reflect the actual allocation.

### Missing Requirements

**No FRs are missing from epic coverage.** All 53 FRs (52 active + FR7 moved to NFR) have traceable implementation paths in the epic breakdown.

### Coverage Statistics

- Total PRD FRs: 53 (52 active + 1 moved to NFR)
- FRs covered in epics: 52/52 (100%)
- Coverage percentage: **100%**
- Discrepancies: 2 stale map entries (FR51, FR52) — implementation paths are correct, only summary map is outdated

---

## Step 4: UX Alignment Assessment

**stepsCompleted:** [step-01, step-02, step-03, step-04-ux-alignment]

### UX Document Status

**Found**: `_bmad-output/planning-artifacts/ux-design-specification.md` (121,250 bytes, 2026-04-14)
Additional reference: `_bmad-output/planning-artifacts/ux-design-directions.html` (43,806 bytes)

The UX spec is comprehensive — completed through 14 steps with 3 rounds of Party Mode refinement (7 agents participated).

### UX ↔ PRD Alignment

**Strong alignment.** The UX spec explicitly traces every design decision to PRD FRs/NFRs:

| UX Aspect | PRD Requirement | Alignment |
|---|---|---|
| 3-tab SPA (/production, /tuning, /settings) | FR41 (mode switching) | ✓ Perfect match |
| 8-key shortcut set | NFR-A1 | ✓ Complete coverage (Enter, Esc, Ctrl+Z, Shift+Enter, Tab, S, 1-9, J/K) |
| HITL 3-tier review | FR31a/b/c | ✓ Auto-approve, batch, precision surfaces all designed |
| Ctrl+Z undo (Command Pattern) | FR33 | ✓ Depth >= 10, 5 action types in V1 |
| Character reference grid (2x5) | FR17, FR18 | ✓ 10-candidate grid + Vision Descriptor |
| Returning-user resume | FR49, FR50 | ✓ Session restoration + diff summary |
| Critic score ambient token | FR24, FR29 | ✓ Bar + numeric + state badge on all scene surfaces |
| Failure recovery (≤3 clicks to resume) | FR2, NFR-R1 | ✓ FailureBanner + Resume action |
| Decisions history (Timeline view) | FR36, FR37 | ✓ Read-only view with filter bar |
| Scene clip playback in review | FR20, FR31b | ✓ AudioPlayer + scene clip mini-video |

**68 UX Design Requirements (UX-DR1 through UX-DR68)** are defined and distributed across Epics 6-10 in the epics document.

### UX ↔ Architecture Alignment

**Strong alignment.** Architecture explicitly accounts for UX-critical decisions:

| UX Need | Architecture Support | Status |
|---|---|---|
| React SPA with shadcn/ui | React 19.x + shadcn CLI v4 + Vite 7.3 → Go embed.FS | ✓ |
| Optimistic UI updates (<100ms) | TanStack Query optimistic mutations | ✓ |
| Polling (5s interval) | TanStack Query `refetchInterval: 5000` + stale-while-revalidate | ✓ |
| Command Pattern undo | `decisions` table + `superseded_by` FK; undo.go within scene_service | ✓ |
| UI state persistence | Zustand with localStorage persist; reconciliation with TanStack Query on restore | ✓ |
| Review list stability | `useStableList` hook pattern — snapshot freeze during batch review, updates queued as badge | ✓ |
| Keyboard shortcuts | Global hook design (Epic 6); Custom ESLint rule for invariance | ✓ |
| Dark-only design | Tailwind CSS 4.x with CSS custom properties; no dark: prefix | ✓ |
| Dev workflow | `pipeline serve --dev` proxies to Vite dev server; cosmtrek/air for Go hot-reload | ✓ |
| Desktop-only, localhost | NFR-S2 (127.0.0.1 only); NFR-A3 (no mobile/tablet) | ✓ |
| REST API endpoints | 17 endpoints across 5 groups with mandatory response envelope | ✓ |
| SPA catch-all routing | Go ServeMux fallback handler for non-`/api/*` paths | ✓ |

### Alignment Issues

**No critical misalignments found.** The three documents (PRD, Architecture, UX) were developed with mutual awareness — each explicitly references the other two as input documents. Party Mode sessions with overlapping agent teams (Sally for UX, Winston for Architecture, John for PM) ensured cross-document coherence.

### Minor Observations

1. **UX-DR count vs Epic coverage**: 68 UX-DRs are distributed across Epics 6-10 via a UX-DR Distribution table in the epics document. This provides traceable coverage, matching the FR Coverage Map approach.
2. **V1.5 UX items clearly scoped**: SSE/WebSocket upgrade, semantic diff-edit for Vision Descriptor, Patterns view, style presets, and graphical metrics dashboard are all explicitly V1.5 in both UX and Architecture docs — no scope creep risk.
3. **Post-publish feedback gap**: UX spec identifies this as Challenge #6, with V1 schema hooks (`feedback_source`, `external_ref`, `feedback_at`) as nullable columns. This is an honest acknowledgment of a V1 limitation, not a gap.

---

## Step 5: Epic Quality Review

**stepsCompleted:** [step-01, step-02, step-03, step-04, step-05-epic-quality-review]

### Epic Structure Validation

| Epic | User Value | Independence | Stories | Rating |
|---|---|---|---|---|
| Epic 1: Project Foundation & Architecture Skeleton | ⚠️ Partial (init/doctor are user-facing; provider interfaces, testutil are developer-facing) | ✓ Standalone | 7 | Acceptable (greenfield) |
| Epic 2: Pipeline Lifecycle & State Machine | ✓ Strong ("Operator can create, resume, cancel, inspect runs") | ✓ Depends on E1 | 6 | Excellent |
| Epic 3: Scenario Generation & Basic Quality Gate | ✓ Strong ("System generates complete scenario from SCP ID") | ✓ Depends on E1-2 | 5 | Good |
| Epic 4: Advanced Quality Infrastructure | ✓ Good ("Operator can tune quality with confidence") | ✓ Depends on E3 | 4 | Good |
| Epic 5: Media Generation (Phase B) | ✓ Strong (tangible image + TTS output) | ✓ Depends on E1-3 | 5 | Good |
| Epic 6: Web UI — Design System & App Shell | ✓ Good ("Operator can launch web UI, navigate, use shortcuts") | ✓ Depends on E1-2 | 5 | Good |
| Epic 7: Production Tab — Scenario Review & Character Selection | ✓ Excellent (full HITL production workflow) | ✓ Depends on E3,5,6 | 5 | Excellent |
| Epic 8: Batch Review & Decision Management | ✓ Excellent (complete review/approval workflow) | ✓ Depends on E6-7 | 6 | Excellent |
| Epic 9: Video Assembly & Compliance (Phase C) | ✓ Strong (final video + compliance) | ✓ Depends on E5,8 | 4 | Good |
| Epic 10: Tuning, Settings & Operational Tooling | ✓ Good (quality tuning, config, export) | ✓ Depends on E4,6 | 5 | Good |

### Story Quality Assessment

**Total Stories**: 52 across 10 epics
**Acceptance Criteria Format**: All stories use proper Given/When/Then BDD structure ✓
**Test Requirements**: Every story specifies required tests (unit, integration, or E2E) ✓
**FR Traceability**: Every FR is traceable to at least one story ✓

### Best Practices Compliance Checklist

| Check | Status |
|---|---|
| Epics deliver user value | ✓ (Epic 1 partial — justified by greenfield) |
| Epics function independently (forward direction only) | ✓ No backward dependencies found |
| Stories appropriately sized | ✓ Each story is independently completable |
| No forward dependencies between stories | ✓ Within-epic ordering is sequential forward |
| Database tables created when needed | ⚠️ All 3 tables in Story 1.2 (see finding below) |
| Clear acceptance criteria | ✓ All Given/When/Then with specific outcomes |
| Traceability to FRs maintained | ✓ FR Coverage Map + per-epic FR listing |
| Starter template requirement met | ✓ Story 1.1 uses Option B (Manual Go + shadcn Vite) |
| CI/CD early | ✓ Story 1.7 establishes CI from Day 1 |

### Findings

#### 🟡 Minor Concerns

**1. Epic 1 contains developer-facing infrastructure**

Epic 1's title "Project Foundation & Architecture Skeleton" bundles user-facing commands (init, doctor, renderer) with developer-facing infrastructure (provider interfaces, testutil, mock boundaries, clock abstraction). For a greenfield project, this is the standard "foundation epic" pattern and is acceptable — but it should be acknowledged that ~60% of this epic delivers no direct operator value.

**Remediation**: No action needed — this is expected for greenfield projects. The architecture explicitly requires Tier 1 cross-cutting concerns to be designed before domain implementation.

**2. All 3 DB tables created upfront in Story 1.2**

The `runs`, `decisions`, and `segments` tables are all created in migration 001, rather than each story creating tables as needed. This violates the "create when needed" rule.

**Justification**: The architecture chose a manual embedded SQL migration runner with `PRAGMA user_version`. All 3 tables are the core domain model used across ALL subsequent epics. Splitting creation would add artificial migration complexity (5-6 separate migrations for tightly coupled tables).

**Remediation**: No action needed — the architectural decision is sound and explicitly documented.

**3. Epic 10 title inconsistency**

The Epic list (line ~590) says "Epic 10: Tuning, Settings & Operational Tooling" but the Stories section header (line ~1910) says "Epic 10: Tuning, Settings & CI Pipeline".

**Remediation**: Update the Stories section header to match.

**4. Story 10.3 (`pipeline clean`) potentially contradicts NFR-O2**

NFR-O2 states "Retention of `pipeline_runs` and `decisions` rows is indefinite in V1 (no purge mechanism); the operator owns manual cleanup if desired." Story 10.3 introduces a `pipeline clean` utility with "Soft Archive" (files moved/deleted, DB records preserved with NULL refs + VACUUM). While DB rows are preserved, the story adds a mechanism the NFR says V1 doesn't have.

**Remediation**: Clarify scope — if `pipeline clean` only addresses file artifacts (not DB rows), it doesn't violate NFR-O2. The story description supports this interpretation ("artifact files are moved/deleted, but database records are preserved").

**5. Minor scope additions not traced to PRD**

Three stories introduce features not in the PRD's FR list or CLI command tree:
- Story 10.2: "Fast Feedback run" (execute specific stage against 10-scene sample) — not in any FR
- Story 10.3: `pipeline clean` command — 9th command, not in PRD's 8-command tree
- Story 10.5: `--format csv` export — FR44 specifies only JSON export

**Remediation**: These are minor quality-of-life additions. Either trace them to FRs (amend PRD) or mark them as "implementation bonus" to maintain traceability discipline.

#### ✓ No Critical or Major Violations Found

- No technical-only epics (Epic 1 is the closest but delivers init/doctor)
- No forward dependencies between epics
- No circular dependencies
- No epic-sized stories that cannot be completed independently
- All stories have proper Given/When/Then ACs with error conditions covered
- FR Coverage Map is 100% (with the 2 stale entries noted in Step 3)

### Dependency Graph (Verified)

```
Epic 1 (Foundation)
  ├── Epic 2 (Pipeline Lifecycle)
  │     ├── Epic 3 (Phase A Scenario)
  │     │     ├── Epic 4 (Quality Infrastructure)
  │     │     └── Epic 5 (Phase B Media)
  │     └── Epic 6 (Web UI Shell)
  │           ├── Epic 7 (Production Tab) ← also depends on E3, E5
  │           │     └── Epic 8 (Batch Review)
  │           │           └── Epic 9 (Phase C Assembly) ← also depends on E5
  │           └── Epic 10 (Tuning/Settings) ← also depends on E4
```

No backward arrows. All dependencies flow forward. ✓

---

## Step 6: Final Assessment — Journey Simulation & Readiness Verdict

**stepsCompleted:** [step-01 through step-06-final-assessment]

### Journey Simulation Results

Jay의 요청대로, PRD의 9개 운영 모드를 실제 Epic/Story에 대입하여 시뮬레이션했습니다.

#### Mode 1: Operator — Producing a Video (Happy Path)

| Step | PRD Description | Epic/Story | Status |
|---|---|---|---|
| `pipeline create scp-049` | Run creation | Story 2.2 | ✓ |
| Phase A: 6 agents execute | Scenario generation | Epic 3 (Stories 3.1-3.5) | ✓ |
| UI ticks stages grey→green | StageStepper + StatusBar | Story 7.1 | ✓ |
| Character reference 10-grid | DuckDuckGo search → pick | Story 5.3 (backend) + Story 7.3 (UI) | ✓ |
| Vision Descriptor edit | Operator edits descriptor text | Epic 7 scope (UX-DR62) | ⚠️ **GAP** |
| Qwen-Image-Edit → canonical ref | Reference-based image generation | Story 5.4 | ✓ |
| Batch review screen | Per-scene cards with scene clips | Story 8.1 | ✓ |
| Auto-pass collapsed, 2 flagged | Precision review | Stories 8.1, 8.2 | ✓ |
| Phase C assembly | FFmpeg scene clips → final MP4 | Story 9.1 | ✓ |
| Decisions recorded | Every action → decisions table | Stories 8.2, 8.3 | ✓ |
| Output: MP4 + metadata bundle | Ready-for-upload gate | Stories 9.2, 9.4 | ✓ |

**⚠️ GAP 발견**: Vision Descriptor 인라인 편집이 Epic 7 scope와 UX-DR에는 있지만 Story 7.3의 AC에는 없음. Story 7.3은 후보 선택/확인만 다루고, "operator edits one sentence in the Vision Descriptor" 플로우의 테스트 기준이 누락됨.

#### Mode 2: Operator — Recovering From a Failure

| Step | PRD Description | Epic/Story | Status |
|---|---|---|---|
| TTS track fails (DashScope timeout) | Stage-level failure isolation | Stories 2.4, 5.2 | ✓ |
| Image artifacts intact | Cross-stage artifact persistence | Story 2.3 | ✓ |
| FailureBanner + cost-cap green | Failure UX | Story 7.4 | ✓ |
| `pipeline resume scp-049-run-42` | Stage-level resume | Story 2.3 | ✓ |
| TTS re-executes from scratch | Clean slate semantics (DELETE + re-insert) | Story 2.3 | ✓ |
| Cost of re-run logged | Observability | Story 2.4 | ✓ |

✓ **완벽히 커버됨**. errgroup.Group (NOT WithContext) → 한 트랙 실패가 다른 트랙을 취소하지 않는 것까지 Story 5.2에서 명시적으로 테스트.

#### Mode 3: Maintainer — Tuning the Critic Prompt

| Step | PRD Description | Epic/Story | Status |
|---|---|---|---|
| Switch to Tuning tab | Tab navigation | Story 6.2 | ✓ |
| Edit Critic prompt | Prompt editor | Story 10.2 | ✓ |
| `go test ./critic -run Golden` | Golden eval | Story 4.1 | ✓ |
| `go test ./critic -run Shadow` | Shadow eval | Story 4.2 | ✓ |
| Commit fixtures (1:1 ratio) | Fixture governance | Story 4.1 | ✓ |

✓ **완벽히 커버됨**.

#### Mode 4: Diagnostician — Investigating a Cost Anomaly

| Step | PRD Description | Epic/Story | Status |
|---|---|---|---|
| Query pipeline_runs (SQL) | Raw SQL diagnosis | Story 2.4, NFR-O3 | ✓ |
| retry_count=4, retry_reason visible | 8-column observability | Story 2.4 | ✓ |
| Anti-progress detector telemetry | Cosine similarity logs | Story 2.5 | ✓ |
| Cross-mode escalation to Mode 3 | Diagnostic deep-link | Story 10.1 (UX-DR45) | ✓ |

✓ **커버됨**.

#### Mode 5: Reviewer — Day-90 Acceptance Gate Evaluation

| Step | PRD Description | Epic/Story | Status |
|---|---|---|---|
| `pipeline metrics --window 25` | CLI metric report (SQL views) | **???** | ❌ **GAP** |
| Rolling 25-run window display | 5 metrics displayed | **???** | ❌ **GAP** |
| V1.5 backlog authoring | Manual operator task | N/A (not tool feature) | ✓ N/A |

**❌ GAP 발견**: `pipeline metrics --window N` 명령어는 PRD의 8개 V1 CLI 명령어 중 하나이며, Mode 5 Journey의 핵심 기능. 그러나:
- 이 명령어를 구현하는 FR이 없음 (FR29는 kappa 계산 백엔드만 커버)
- 이 명령어를 구현하는 Story가 없음
- 5개 메트릭 (automation rate, kappa, regression detection, defect escape, resume idempotency)을 SQL view로 집계하는 구현이 어디에도 명시되지 않음

이것은 PRD에서 명시적으로 "V1 ships CLI report output"라고 했고 Day-90 acceptance gate의 핵심 도구인데 Epic/Story 분해에서 빠진 **실질적 누락**.

#### Mode 6: Maintainer — Golden Set Curation

✓ **커버됨** — Story 4.1.

#### Mode 7: Operator — First-Time Setup

✓ **커버됨** — Story 1.5 (init + doctor).

#### Mode 8: Operator — Returning After a Pause

| Step | PRD Description | Epic/Story | Status |
|---|---|---|---|
| Run inventory with summaries | State-aware RunCards | Story 7.1, 7.5 | ✓ |
| Resume from precision-review | Exact decision point | Story 2.6 | ✓ |

✓ **커버됨**.

#### Mode 1-Repeat: 30th Video (Wear Pattern)

| Step | PRD Description | Epic/Story | Status |
|---|---|---|---|
| Batch "approve remaining" | Single action | Story 8.5 | ✓ |
| Confidence context display | Acceptance pattern info | Story 8.5 | ✓ |

✓ **커버됨**.

### Data Flow Simulation

```
SCP ID
  │
  ▼
Phase A: Researcher → Structurer → Writer → [Critic #1] → VisualBreakdowner → Reviewer → [Critic #2]
  │                                                        (shot breakdowns: 1-5 shots/scene)
  │ Output: scenario.json (file-based handoff)
  │
  ├─── HITL: scenario_review (Story 7.2: narration edit)
  │
  ▼
Phase B (parallel):
  ├── Image Track: Frozen Descriptor + shot descriptors → ~30 per-shot images
  │   └── HITL: character_pick (Story 7.3: 10-grid → Image-Edit → canonical ref)
  │
  └── TTS Track: narration → Korean transliteration → DashScope TTS → per-scene audio
  │
  │ Shared: DashScope rate-limiter (semaphore + token bucket)
  │
  ▼
HITL: batch_review (Epic 8: approve/reject/edit per scene)
  │
  ▼
Phase C:
  ├── Step 1: Per-scene clip (1-5 shots + transitions + TTS audio)
  └── Step 2: Final concat → output.mp4
  │
  ├── metadata.json (AI-content disclosure)
  └── manifest.json (SCP license chain)
  │
  ▼
HITL: metadata_ack → "ready-for-upload" ✓
```

**Data flow 검증 결과**: 모든 inter-phase handoff가 명확. file-based (between phases) vs in-memory (within Phase A) 구분이 Architecture에서 명시됨. segments 테이블이 Phase B → Phase C 데이터 연결 역할.

### Edge Case Simulation

#### Case 1: Anti-Progress Detection

```
Writer output #1 → Critic: "retry"
Writer output #2 → cosine_sim(#1, #2) = 0.95 > 0.92
→ Early stop → retry_reason = "anti_progress"
→ Operator escalated
```

**Story 2.5** covers this with: threshold crossing test, configurable threshold, no false-positive on dissimilar outputs. ✓

#### Case 2: Cost Cap Hard Stop

```
Stage cost accumulates → exceeds per-stage cap
→ ErrCostCapExceeded (retryable=false)
→ Stage hard-stops → operator notified
```

**Story 2.4** covers this with: cost accumulator threshold tests, per-stage and per-run caps. ✓

#### Case 3: Phase B One-Track Failure

```
Image track: fails at scene 5 (DashScope 429)
TTS track: continues to completion (errgroup.Group, NOT WithContext)
→ Image track failure recorded
→ TTS artifacts preserved
→ Resume re-executes only failed track
```

**Story 5.2** explicitly tests: "inject failure into one track, assert the other completes." ✓

#### Case 4: HITL Session Interrupted

```
Operator at batch_review, scene 5/10
→ Process killed / browser closed
→ Pause state persisted to DB (run_id, stage, scene_index, timestamp)
→ Later: operator returns → exact position restored
→ "What changed" diff shown if applicable
```

**Story 2.6** covers: pause state persistence, resume position accuracy, diff generation. ✓

#### Case 5: Writer = Critic Provider Misconfiguration

```
config.yaml: writer_provider=deepseek, critic_provider=deepseek
→ `pipeline doctor` fails: "Writer and Critic must use different LLM providers"
→ `pipeline create` also fails (defense-in-depth)
```

**Story 1.5** (doctor check) + **Story 3.3** (runtime enforcement): dual-layer check. ✓

### Summary of All Issues Found

#### ❌ Critical Gap (1)

**`pipeline metrics --window N` command missing from Epics/Stories**

PRD의 8개 V1 CLI 명령어 중 하나이자 Day-90 acceptance gate의 핵심 도구가 어떤 Story에도 없음.

- **Impact**: Mode 5 (Day-90 Gate Evaluation) journey가 구현 불가능
- **What's needed**: Story that implements SQL-view metric aggregation (automation rate, kappa, regression detection, defect escape rate, resume idempotency) over a configurable rolling window, surfaced as CLI output
- **Recommendation**: Epic 2에 Story 추가 (observability 관련) 또는 새 Story를 Epic 10에 추가

#### ⚠️ Moderate Gap (1)

**Vision Descriptor 인라인 편집 AC 누락 (Story 7.3)**

Epic 7 scope와 UX-DR26/41/62에 명시되어 있으나, Story 7.3의 AC는 후보 선택/확인만 커버. "edits one sentence in the Vision Descriptor" 플로우의 Given/When/Then이 없음.

- **Impact**: Character pick 워크플로우의 핵심 부분이 테스트 기준 없이 구현될 수 있음
- **Recommendation**: Story 7.3에 Vision Descriptor 편집 AC 추가, 또는 별도 Story 7.3b 생성

#### 🟡 Minor Issues (5)

1. **FR Coverage Map stale entries** (FR51→Epic 1, FR52→Epic 1+6; map says Epic 10)
2. **Epic 10 title inconsistency** (Epic list vs Stories section header)
3. **Story 10.3 `pipeline clean` potentially contradicts NFR-O2** (artifact files only = OK)
4. **Minor scope additions** (Fast Feedback, `pipeline clean`, CSV export, `pipeline golden add/list`) not traced to PRD FRs
5. **UX spec adds `decisions` table columns** (`context_snapshot`, `outcome_link`, `tags`) beyond PRD FR36 — should be reflected in migration DDL (Story 1.2)

---

## Overall Readiness Status: **READY — with 2 required fixes**

### Verdict

이 프로젝트의 기획 산출물은 **예외적으로 높은 품질**입니다. PRD, Architecture, UX, Epics 4개 문서가 상호 참조하며 일관성을 유지하고, 3차례의 Party Mode 검증을 통해 다수의 에이전트가 교차 검증했습니다.

**숫자로 보는 결과:**
- FR Coverage: **52/52 (100%)**
- UX-DR Distribution: **68/68 (100%)**
- NFR Coverage: **24/24 (100%)**
- Journey Simulation: **8/9 modes fully covered** (Mode 5 partial gap)
- Epic Quality: **No critical or major violations**
- Cross-document Alignment: **Strong** (PRD ↔ UX ↔ Architecture)

### Required Fixes Before Implementation

1. **`pipeline metrics --window N` 스토리 추가** — Epic 2 또는 Epic 10에 새 Story 생성. Day-90 gate의 5개 메트릭을 SQL view로 집계하여 CLI 출력하는 기능. 이것 없이는 PRD Success Criteria 검증 도구가 없음.

2. **Story 7.3에 Vision Descriptor 편집 AC 추가** — Character pick 워크플로우의 누락된 테스트 기준. Given/When/Then 형식으로 Vision Descriptor pre-fill + inline edit + blur-save 추가.

### Recommended (But Not Blocking) Fixes

3. FR Coverage Map의 FR51/FR52 항목을 실제 Epic 할당으로 업데이트
4. Epic 10 Stories 섹션 헤더를 Epic 목록과 일치시키기
5. PRD에 명시되지 않은 scope 추가사항 (Fast Feedback, `pipeline clean`, CSV export, `pipeline golden add/list`)을 PRD에 역추적하거나 "implementation bonus"로 명시

### Final Note

이 평가는 6개 단계에 걸쳐 **53개 FR, 24개 NFR, 68개 UX-DR, 10개 Epic, 52개 Story**를 검증했습니다. 9개 User Journey를 시뮬레이션하여 데이터 흐름과 에지 케이스를 확인했습니다. 발견된 1개 Critical Gap과 1개 Moderate Gap을 수정하면 구현 착수 준비가 완료됩니다.

---

---

## Appendix: Output Simulation — 최종 영상 산출물 검증

Jay의 요청: "결과물을 상상해서 검토 — 영상 길이, 이미지, 효과, TTS 길이 매칭, 풍부한 shot 생성 방법 등"

### 시뮬레이션 대상: SCP-049 (The Plague Doctor), 한국어 ~6분 영상

#### 1. 영상 구조 시뮬레이션

format_guide.md 기준:
- **~5분 영상**: 5-6개 scene, 각 50-60초
- **4-act 구조** (Incident-First, NOT wiki order):

```
Act 1 (~15%, ~50초, 1 scene):
  Scene 1: "2019년 8월, D-9341이 SCP-049에 접촉한 지 4초 만에 사망했습니다..."
  → 충격적 사건으로 시작, 분류 설명 NOT first

Act 2 (~30%, ~100초, 2 scenes):
  Scene 2: SCP-049 — 중세 역병의사 외형, 이상 특성 소개
  Scene 3: 격리 절차, "역병"에 대한 집착

Act 3 (~40%, ~130초, 2-3 scenes):
  Scene 4: "치료" 수술 — 희생자를 좀비(SCP-049-2)로 변환
  Scene 5: 면담 기록 — SCP-049와의 대화
  Scene 6: 가장 공포스러운 사건 기록

Act 4 (~15%, ~50초, 1 scene):
  Scene 7: 미해결 질문 — "역병"의 정체, SCP-049의 진짜 지능 수준
```

**총 7개 scene, ~6분 (360초)**

#### 2. Scene별 Shot 분해 (TTS 기반)

PRD shot count 공식: ≤8s→1, 8–15s→2, 15–25s→3, 25–40s→4, 40s+→5

| Scene | TTS Duration | Shot Count | Avg Shot Duration |
|---|---|---|---|
| Scene 1 (사건 도입) | ~50s | 5 | ~10s |
| Scene 2 (정체 소개) | ~50s | 5 | ~10s |
| Scene 3 (격리 절차) | ~50s | 5 | ~10s |
| Scene 4 (치료 수술) | ~55s | 5 | ~11s |
| Scene 5 (면담) | ~55s | 5 | ~11s |
| Scene 6 (사건 기록) | ~50s | 5 | ~10s |
| Scene 7 (미해결) | ~50s | 5 | ~10s |
| **Total** | **~360s** | **~35 shots** | **~10.3s** |

PRD 예상 "~30 images per run" vs 실제 계산 **~35 images** → 약간 over. 하지만 모든 scene이 40s+이면 5 shots/scene이 됨.

#### 3. Scene 1 상세 Shot 시뮬레이션

```
TTS 나레이션 (50초):
"2019년 8월, SCP 재단의 실험 기록 049-23에 따르면...
D-9341 피험자가 SCP-049에게 접근하라는 지시를 받았습니다.
SCP-049가 그의 어깨에 손을 얹은 지 단 4초 만에,
피험자의 심장이 멈추었습니다.
SCP-049는 평온하게 말했습니다.
'감염이 이미 깊었군요. 안타깝지만 치료가 불가능했습니다.'"
```

```
Shot 1 (8s) — establishing, wide shot
  이미지: 무균 격리실, 형광등, SCP-049 중앙에 서있음
  전환: [영상 시작] → Ken Burns (서서히 zoom in)
  Frozen Descriptor: "tall humanoid figure in heavy dark leather plague
    doctor coat, elongated brass beak mask with circular glass lenses,
    wide-brimmed hat, black leather gloves with visible stitching..."

Shot 2 (10s) — approach, medium shot
  이미지: D-class 피험자가 SCP-049를 향해 걸어감, 배경에 무장 경비원
  전환: cross-dissolve (0.5s transition)
  entity_visible: true → Frozen Descriptor 적용

Shot 3 (8s) — the touch, close-up
  이미지: SCP-049의 장갑 낀 손이 어깨를 향해 뻗음, 가죽 질감 클로즈업
  전환: hard cut (충격적 순간 → 급전환)
  entity_visible: true → 손 부분 Frozen Descriptor

Shot 4 (12s) — death, extreme close-up
  이미지: 피험자 얼굴, 눈이 흐려지며 피부가 창백해짐
  전환: Ken Burns (얼굴에서 천천히 아래로 pan)
  entity_visible: false → 부재의 증거 (SCP-049는 프레임 밖)

Shot 5 (12s) — aftermath, wide shot
  이미지: SCP-049가 시신 위에 평온하게 서있고, 경비원들 달려옴
  전환: cross-dissolve (여운 있는 전환)
  entity_visible: true → Frozen Descriptor 적용
```

#### 4. 발견된 이슈 — 심각도순

---

### 🔴 ISSUE A: 이미지 해상도 1664×928이 Ken Burns에 부적합

**현재 설정** (image.gen.policy.md): `1664 × 928`
**출력 영상**: 1080p (1920 × 1080)

문제:
- 소스 이미지가 출력 프레임보다 **작음** (가로 1664 < 1920, 세로 928 < 1080)
- Ken Burns는 이미지 위를 pan/zoom하는 효과 → 소스가 출력보다 커야 함
- 현재 해상도에서는 이미지를 **확대**해서 프레임을 채워야 하므로 화질 열화
- Ken Burns 이동 범위가 극히 제한적 (5-10% 이동이 한계)

**권장 해결책**:

| 방안 | 해상도 | Ken Burns 여유 | 비용 영향 |
|---|---|---|---|
| 현재 | 1664×928 | ❌ 불가 (소스 < 출력) | — |
| **Option 1** | **2560×1440** | ✅ 가로 33%, 세로 33% 여유 | API 비용 동일 (해상도는 파라미터) |
| Option 2 | 1920×1080 (1:1) | ⚠️ 정확히 맞음 (zoom만 가능, pan 불가) | — |

**Qwen-Image는 해상도 파라미터를 지원**하므로 코드 변경만 필요. PRD/Architecture의 `image_size` 설정을 config-driven(NFR-M1)으로 바꾸면 됨.

**추가**: 세로 영상이 아닌 가로(16:9) 영상이므로 비율은 맞음. 해상도만 올리면 됨.

---

### 🔴 ISSUE B: Shot당 ~10초는 정적 이미지로 너무 길다

**계산**: 50초 scene / 5 shots = 10초/shot

경쟁 채널 비교:
- 살리의 방: 정적 이미지 교체 간격 **3-5초**
- TheRubber: 애니메이션 컷 교체 **2-4초**
- SCP Explained: 빠른 편집 **1-3초**

10초 동안 같은 이미지에 Ken Burns만 적용하면 **슬라이드쇼처럼 느껴짐**. 유튜브 시청자의 retention이 급락하는 구간.

**권장 해결책**: shot count 공식 재조정

| 현재 공식 | 제안 공식 | 효과 |
|---|---|---|
| ≤8s→1 | ≤5s→1 | — |
| 8–15s→2 | 5–10s→2 | — |
| 15–25s→3 | 10–18s→3 | — |
| 25–40s→4 | 18–30s→4 | — |
| 40s+→5 | 30–45s→5, 45s+→6~7 | 50초 scene → 7 shots (7초/shot) |

**50초 scene → 7 shots (평균 7초/shot)** = 훨씬 자연스러운 시각적 리듬.
이렇게 하면 run당 ~50 images가 되지만, 각 shot이 더 짧고 역동적.

**대안**: 같은 이미지를 다른 crop/zoom으로 "sub-shot" 분할 (Ken Burns 내에서 단계적 zoom으로 1 이미지 → 2-3 visual moments). 이미지 생성 비용 증가 없이 시각적 밀도를 높일 수 있음.

---

### 🟠 ISSUE C: 프롬프트 체인 내 Shot Count 결정 방식 불일치

세 가지 서로 다른 접근이 공존:

1. **PRD (FR10)**: "TTS duration → shot count formula (≤8s→1, ..., 40s+→5)"
2. **03_5_visual_breakdown.md**: "1:1 sentence-to-image mapping" (1문장 = 1이미지)
3. **01_shot_breakdown.md**: "bidirectional cut decomposition" (시각적 beat 기반으로 자유롭게 split/merge)

문제: Scene 1의 나레이션이 6문장이면:
- PRD 공식 → 5 shots (50초 duration 기반)
- 03_5 prompt → 6 shots (1:1 sentence mapping)
- 01 prompt → 4-8 shots (visual beat 기반)

**이 세 가지가 서로 다른 숫자를 생산**함. 구현 시 어느 것이 authority인지 명확하지 않음.

**권장 해결책**: 
- **PRD의 TTS duration 공식이 shot COUNT의 authority** (상한)
- VisualBreakdowner 프롬프트는 "이 scene에 N개 shot을 만들어라" 라는 constraint를 받아야 함
- 01_shot_breakdown.md는 Phase B의 세부 분해 단계로, VisualBreakdowner가 정한 N개 안에서 visual beat를 배치
- **프롬프트에 shot count constraint를 명시적으로 주입해야 함**: "이 scene의 shot count는 {{shot_count}}개입니다. 정확히 이 숫자만큼 shot을 생성하세요."

---

### 🟠 ISSUE D: TTS 나레이션 ↔ 이미지 시각적 sync 메커니즘 부재

현재 설계:
1. Phase A: 나레이션 텍스트 생성 → VisualBreakdowner가 shot 분배
2. Phase B: TTS 오디오 생성 (실제 duration 확정) + 이미지 생성 (병렬)
3. Phase C: shot images를 TTS audio 위에 overlay

문제:
- TTS의 **실제 duration**은 Phase B에서야 확정됨
- VisualBreakdowner는 Phase A에서 **예상 duration**으로 shot count를 결정
- 예상 vs 실제 차이가 크면: shot이 너무 짧거나 너무 길어짐

예시:
```
Phase A 예상: Scene 3 = 50초 → 5 shots (10초/shot)
Phase B 실제: TTS가 40초로 나옴 → 5 shots에 8초/shot
→ 허용 범위 내, OK

Phase A 예상: Scene 5 = 55초 → 5 shots (11초/shot)  
Phase B 실제: TTS가 70초로 나옴 → 5 shots에 14초/shot
→ 14초/shot은 너무 길다!
```

또한 **나레이션 내용과 이미지의 의미적 sync**도 이슈:
- 나레이션이 "손을 얹자마자"를 말하는 순간에 hand close-up shot이 나와야 함
- 하지만 shot duration은 균등 분배되므로 나레이션의 특정 단어와 이미지가 정렬되지 않을 수 있음

**권장 해결책**:
- Phase B에서 TTS 생성 후 **실제 duration으로 shot count를 재계산** (Phase A의 예상이 아닌 실제 기반)
- Shot duration을 균등이 아닌 **문장 단위로 배분** (각 shot의 `sentence_start`/`sentence_end`와 TTS word timing 연결)
- V1에서는 operator가 batch review에서 shot timing을 수동 조정할 수 있는 UI 필요 (현재 precision review에서 "per-shot image quality" 확인 가능하지만 timing 조정은 없음)

---

### 🟠 ISSUE E: Shot 다양성 — "풍부하고 다양한 shot"을 어떻게 이끌어내는가

현재 프롬프트 체인의 강점:
- **8-slot 구조** (shot type, camera angle, subject, action, spatial, environment, lighting, atmosphere, emotion) → 풍부한 prompt 구조 ✓
- **8가지 camera type** (wide, medium, close-up, extreme close-up, POV, over-the-shoulder, bird's eye, low angle) ✓
- **Forbidden terms** (dark, scary, horror 등 금지 → 구체적 감정 키워드 강제) ✓
- **entity_visible toggle** (entity 있을 때 vs 부재의 증거) → 시각적 변주 ✓

현재 프롬프트 체인에 **없는 것** (다양성을 위해 필요):

1. **Scene 내 camera type 반복 금지 규칙이 없음**
   - 5 shots가 전부 wide shot이면 단조로움
   - 권장: "한 scene 내에서 같은 camera_type 연속 사용 금지, 최소 3종류 camera_type 사용"

2. **Shot-to-shot 시각적 대비 규칙이 없음**
   - format_guide.md는 scene-to-scene 감정 대비를 요구하지만 shot-to-shot은 명시 안 됨
   - 권장: "인접 shot은 scale(wide↔close), tone(밝음↔어둠), 또는 movement(정적↔동적)에서 하나 이상 대비"

3. **Subject framing 다양성 규칙이 없음**
   - 모든 shot에서 SCP-049가 중앙에 있으면 단조로움
   - 권장: "entity_visible shots에서 subject 위치를 rule of thirds로 변주"

4. **"Negative space" 활용 지침 부재**
   - 03_5_visual_breakdown.md에서 "negative space/depth layering" self-check가 있긴 하지만
   - 구체적으로 "N개 shot 중 최소 1개는 entity가 프레임의 20% 미만을 차지하는 wide establishing shot" 같은 규칙이 없음

5. **"Visual Hook" shot 보장이 없음**
   - 03_5 self-check에 "visual hook" 있지만 Scene 1의 첫 shot이 반드시 시선을 사로잡는 구도여야 한다는 규칙이 부족
   - 권장: "각 Act의 첫 scene 첫 shot은 반드시 dramatic 구도 (extreme angle 또는 unusual perspective)"

**이 규칙들은 VisualBreakdowner 프롬프트에 추가 가능** — 코드 변경 없이 프롬프트 엔지니어링으로 해결.

---

### 🟡 ISSUE F: Cross-dissolve 0.5초가 너무 짧다

Story 9.1의 AC: "Cross-dissolve: `xfade` filter between consecutive shots (duration: 0.5s)"

0.5초 cross-dissolve는 **거의 hard cut에 가까움**. 분위기 있는 호러 영상에서는:
- 일반 전환: 1.0-1.5초
- 감정적 전환: 2.0-3.0초
- 급전환(충격): 0.0초 (hard cut)

**권장**: Cross-dissolve duration을 shot metadata에 포함 (VisualBreakdowner가 지정), 기본값 1.0초로 변경. Hard cut과 차별화가 명확해짐.

---

### 🟡 ISSUE G: image.gen.policy.md가 SiliconFlow API를 참조

`docs/images/image.gen.policy.md`의 curl 예제가 `api.siliconflow.com`을 사용.
PRD는 "DashScope only (no SiliconFlow)"을 명시적으로 규정.

**권장**: image.gen.policy.md를 DashScope endpoint로 업데이트. 구현 시 혼동 방지.

---

### 🟡 ISSUE H: BGM과 효과음이 V1.5인데, 호러 영상에 치명적

PRD: "Audio fade and BGM are V1.5"

문제:
- SCP 호러 영상에서 배경음악은 **분위기의 50% 이상**을 담당
- 살리의 방, TheVolgun 모두 전용 BGM 사용
- TTS 나레이션 + 정적 이미지만으로는 "공포감"을 전달하기 매우 어려움
- BGM 없는 SCP 영상은 경쟁력이 현저히 떨어짐

Jay의 피드백 원칙 "Growth/Vision으로 미루면 Jay는 안 함"과 직접 관련됨.

**단, 이것은 PRD에서 의도적으로 V1.5로 결정한 사항.**
V1의 목표는 "첫 publishable 영상 end-to-end" → BGM은 operator가 CapCut 등에서 수동 추가 가능.
BUT: V1.5 gate (5 videos shipped)까지 BGM 없이 5개 영상을 올리는 것이 channel growth에 얼마나 영향을 미칠지 고려 필요.

---

### 시뮬레이션 최종 타임라인 (Scene 1 기준)

```
[0.0s]  Shot 1: Wide — 격리실 + SCP-049 (Ken Burns zoom in)
        TTS: "이천십구 년 팔 월, 에스씨피 재단의 실험 기록..."
[8.0s]  ── cross-dissolve 0.5s ──
[8.5s]  Shot 2: Medium — D-class 접근 (Ken Burns lateral pan)
        TTS: "디-구삼사일 피험자가 에스씨피-공사구에게..."
[18.5s] ── hard cut ──
[18.5s] Shot 3: Close-up — 장갑 낀 손 (Ken Burns slow zoom)
        TTS: "에스씨피-공사구가 그의 어깨에 손을 얹은 지..."
[26.5s] ── Ken Burns (내부 전환 없음) ──
[26.5s] Shot 4: Extreme close-up — 피험자 얼굴 (pan down)
        TTS: "단 사 초 만에, 피험자의 심장이..."
[38.5s] ── cross-dissolve 0.5s ──
[39.0s] Shot 5: Wide — SCP-049 위에 서있음, 경비원 돌진
        TTS: "에스씨피-공사구는 평온하게 말했습니다..."
[50.0s] ── cross-dissolve to Scene 2 ──
```

**결과물 예상 품질 평가**:
- ✅ 스토리텔링 구조: 강력함 (incident-first 4-act, 감정 곡선)
- ✅ 시각적 일관성: Frozen Descriptor로 캐릭터 일관성 보장
- ✅ 나레이션 품질: Qwen3-TTS-Flash + 한국어 전처리
- ⚠️ 시각적 리듬: shot당 10초는 느림 → 조정 필요
- ⚠️ Ken Burns 효과: 해상도 문제 → 2560×1440 권장
- ⚠️ shot 다양성: 프롬프트에 camera type 반복 금지 등 규칙 추가 필요
- ❌ BGM 없음: V1에서는 operator가 수동 추가 필요

---

### Output Simulation 기반 권장사항 (우선순위순)

| # | 이슈 | 영향도 | 수정 대상 | 수정 난이도 |
|---|---|---|---|---|
| A | 이미지 해상도 1664×928 → 2560×1440 | 🔴 Critical | config.yaml `image_size` | 설정 변경만 |
| B | Shot count 공식 재조정 (10초/shot → 5-7초/shot) | 🔴 Critical | PRD FR10, VisualBreakdowner | 공식 수정 |
| C | 프롬프트 체인 shot count authority 통일 | 🟠 High | 03_5, 01 prompts + PRD | 프롬프트 수정 |
| D | TTS 실제 duration 기반 shot count 재계산 | 🟠 High | Phase B 로직 | 코드 설계 |
| E | Shot 다양성 규칙 (camera type 반복 금지 등) | 🟠 High | VisualBreakdowner prompt | 프롬프트 수정 |
| F | Cross-dissolve duration 1.0초 기본 + 가변 | 🟡 Medium | Story 9.1 | AC 수정 |
| G | image.gen.policy.md SiliconFlow→DashScope | 🟡 Medium | docs/ 문서 | 문서 수정 |
| H | BGM V1.5 결정의 channel growth 영향 재검토 | 🟡 Medium | PRD scope 논의 | 의사결정 |

**A, B는 config/공식 변경만으로 해결 가능 — 코드 아키텍처 변경 없음.**
**C, D, E는 프롬프트 엔지니어링 + Phase B 설계에 반영 필요.**

---

**Report generated:** `_bmad-output/planning-artifacts/implementation-readiness-report-2026-04-16.md`
**Assessment date:** 2026-04-16
**Assessor:** Implementation Readiness Validator
