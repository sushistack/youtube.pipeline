---
name: Epic 1–10 UI E2E Test Generation (Step 4 — Round 1)
description: Playwright spec scaffolds for UI-E2E-01/02/03/04/08/09/10. UI-E2E-05/06/07 deferred to Round 2 (post-blocker).
type: test-summary
date: 2026-04-25
project: youtube.pipeline
upstream_input: _bmad-output/test-artifacts/test-design-epic-1-10-2026-04-25.md
status: draft
---

# Test Generation Summary — UI E2E (Round 1)

**Scope.** 7 of 10 UI E2E cases from [test-design §5](test-design-epic-1-10-2026-04-25.md): UI-E2E-01, 02, 03, 04, 08, 09, 10. Cases 05, 06, 07 are excluded per upstream blocker dependencies (AI-3, AI-4, CP-5 Story 10-2) and will be addressed in Round 2.

**Out of scope (no scaffold/skip/TODO files created).** UI-E2E-05 (Retry Exhausted), UI-E2E-06 (ComplianceGate Ack), UI-E2E-07 (Tuning Surface).

---

## Generated Files

### Spec files (7 — one per case in scope)

| Case | Spec file |
|------|-----------|
| UI-E2E-01 SPA Smoke | [web/e2e/smoke.spec.ts](web/e2e/smoke.spec.ts) — replaces stale Tuning todo with the new `/assets/*.js` 404 regression guard |
| UI-E2E-02 Inline Narration Edit | [web/e2e/inline-narration-edit.spec.ts](web/e2e/inline-narration-edit.spec.ts) |
| UI-E2E-03 Character Pick → Vision Descriptor | [web/e2e/character-pick.spec.ts](web/e2e/character-pick.spec.ts) |
| UI-E2E-04 Batch Review Chord | [web/e2e/batch-review-chord.spec.ts](web/e2e/batch-review-chord.spec.ts) |
| UI-E2E-08 Settings Save | [web/e2e/settings-save.spec.ts](web/e2e/settings-save.spec.ts) |
| UI-E2E-09 Run Inventory Search | [web/e2e/run-inventory-search.spec.ts](web/e2e/run-inventory-search.spec.ts) |
| UI-E2E-10 FailureBanner → Enter Resume | [web/e2e/failure-banner-resume.spec.ts](web/e2e/failure-banner-resume.spec.ts) |

### Page objects (3 — TuningShell intentionally NOT created)

| Object | File | Used by |
|--------|------|---------|
| `ProductionShellPO` | [web/e2e/po/production-shell.po.ts](web/e2e/po/production-shell.po.ts) | UI-E2E-01, 02, 03, 09, 10 |
| `BatchReviewPO` | [web/e2e/po/batch-review.po.ts](web/e2e/po/batch-review.po.ts) | UI-E2E-04 |
| `SettingsShellPO` | [web/e2e/po/settings-shell.po.ts](web/e2e/po/settings-shell.po.ts) | UI-E2E-08 |

`TuningShellPO` was deliberately omitted — UI-E2E-07 is out of scope this round, and creating an unused PO would be scope leak.

### Shared infrastructure

| File | Purpose |
|------|---------|
| [web/e2e/fixtures.ts](web/e2e/fixtures.ts) | Per-test fixture: auto `resetStores` (clears localStorage/sessionStorage on every page load — addresses deferred 6-5 zustand singleton bleed); auto `consoleGuard` (collects pageerror + console.error); on-demand `skipOnboarding` (pre-seeds `onboarding_dismissed=true` in the persisted UI store envelope so most tests skip the "Continue to workspace" click) |
| [web/e2e/po/mock-api.ts](web/e2e/po/mock-api.ts) | `installApiMocks(page, {state, spies})` — registers `page.route()` handlers for every `/api/runs/**` and `/api/settings` endpoint the 7 cases touch. State is fixture-driven; spies expose array/counter handles so tests can assert request shape without reaching into the backend |

---

## Test → Risk Coverage

| Case | Test design risk link | Assertions |
|------|----------------------|------------|
| UI-E2E-01 | R-06, R-16 (deferred 6-1 / 6-2) | Shell visible (`Production` h1, `Create a new pipeline run`); Ctrl+N opens new-run dialog; Esc closes; `/assets/*.js` 404 returns real 404 (NOT 200 + index.html); zero console / pageerror |
| UI-E2E-02 | Deferred 7-2 baseline re-sync | Blur commits via `POST /scenes/:i/edit`; reload restores edited text; Ctrl+Z reverts to **last persisted** baseline (not original) |
| UI-E2E-03 | UX-DR 17, 41, 62 | Grid auto-focuses; digit `3` selects cand-3; Enter advances to descriptor; Save POSTs `{candidate_id, frozen_descriptor}`; second test verifies digits 1–9 and 0 each select the matching slot |
| UI-E2E-04 | AI-2 + UX-DR 18, 23, 24, 33, 34, 38 | `J×3 / K / Enter / S / Esc / Shift+Enter / Ctrl+Z` chord exercises decision recording, batch approve (single `aggregate_command_id` from server — deferred 8-6 normalization regression guard), undo dispatch; second test asserts Focus-Follows-Selection (detail pane never empty across 5 J presses) |
| UI-E2E-08 | DF1 re-parse regression | `cost_cap_per_run` 0.5 → 1.00 round-trips through `PUT /api/settings` with the If-Match ETag; refetch reflects saved value; second test asserts `critic_model` edit propagates in the same save |
| UI-E2E-09 | UX polish (UX-DR 63) | Substring filter narrows by SCP id (`alp` → alpha), stage (`image` → delta), status (`failed` → gamma); empty input restores all runs |
| UI-E2E-10 | UX-DR 16 (keyboard resume) | Failed run → FailureBanner visible with rate-limit heading; Enter dispatches `POST /api/runs/:id/resume`; banner unmounts after status flips to running; second test verifies Resume-button-click is the a11y fallback |

---

## Deviations from test-design (with rationale)

These are intentional. Each preserves the **intent** of the spec while staying inside what the current code actually exposes — no fabricated assertions.

| Case | Spec asks | Reality | Test asserts |
|------|----------|---------|--------------|
| UI-E2E-02 | "optimistic UI < 100 ms via `performance.mark`" | `InlineNarrationEditor` is **not** instrumented with `performance.mark` ([web/src/components/production/InlineNarrationEditor.tsx](web/src/components/production/InlineNarrationEditor.tsx) — verified) | Functional correctness only (blur commit + reload restore + Ctrl+Z baseline). Performance-mark assertion will be added once instrumentation lands. |
| UI-E2E-04 | "optimistic UI updates < 100 ms per keystroke" | Same — no perf marks in `BatchReview` | Functional correctness on chord transitions; perf assertion deferred. |
| UI-E2E-09 | "filter stage = `phase_b`" / "filter status = `failed`" as separate widgets | Sidebar exposes a **single** search input; stage/status are matched via the same client-side substring filter ([web/src/components/shared/Sidebar.tsx:51-66](web/src/components/shared/Sidebar.tsx#L51-L66)) | Type substrings that match stage and status fields respectively — same contract, single widget. |
| UI-E2E-09 | "`/` focuses search input" | The `/` keyboard shortcut for inventory search is **not wired** in the current Sidebar. Only `Mod+N` is registered. | Assertion omitted. Will be added once the shortcut ships. |
| Routes | spec writes `/production/:runId` | Real route is `/production?run={id}` (verified in [web/src/App.tsx](web/src/App.tsx) + [ProductionShell:82](web/src/components/shells/ProductionShell.tsx#L82)) | All POs/specs use the `?run=` query param. |

These deviations are documented inline in spec comments where they apply.

---

## Mocking strategy

- `serve:e2e` launches the **real Go backend** at `127.0.0.1:4173` against `.tmp/playwright/pipeline.db` ([web/scripts/serve-playwright.mjs:51-60](web/scripts/serve-playwright.mjs#L51-L60)). The DB is empty per run.
- For deterministic UI-E2E-02/03/04/08/09/10, every `/api/runs/**` and `/api/settings` request is intercepted via `page.route()` (`installApiMocks`). Unmocked paths fall through to the real backend.
- UI-E2E-01 is **not** API-mocked — it asserts the SPA-shell + asset-404 contract against the real binary, which is exactly what R-06 / deferred 6-1 ask for.
- Per-test isolation: the auto `resetStores` fixture clears `localStorage` (where the zustand persist envelope lives under key `youtube-pipeline-ui`) before every navigation. This is the deferred 6-5 zustand singleton-bleed mitigation called out in test-design §9.

---

## How to run

### Locally (web/ dir, real backend behind the scenes)

```bash
cd web && npx playwright test
```

To list without executing (sanity check):

```bash
cd web && npx playwright test --list
```

To run a single case:

```bash
cd web && npx playwright test smoke.spec.ts
cd web && npx playwright test batch-review-chord.spec.ts
```

Headed mode for debugging:

```bash
cd web && npx playwright test --headed --project=chromium
```

### CI

[.github/workflows/ci.yml](.github/workflows/ci.yml)'s `test-e2e` job already points at `cd web && npx playwright test` (Step 3.5 / commit 7e07c29 — CP-3 repoint). No further wiring is required for this batch. The `continue-on-error: true` removal that was part of CP-3 is also in place, so failures here block the merge.

---

## Coverage

- UI E2E in scope: **7 / 7** (UI-E2E-01, 02, 03, 04, 08, 09, 10)
- UI E2E out of scope (Round 2): UI-E2E-05 (AI-3 dependency), UI-E2E-06 (AI-4 dependency), UI-E2E-07 (CP-5 Story 10-2 FULL dependency)
- Page objects required by in-scope cases: **3 / 3**
- `playwright test --list` parse: **15 tests in 8 files** (7 new + 1 pre-existing `new-run-creation.spec.ts`) — all import-resolved.

---

## Next steps

1. Run `cd web && npx playwright test` end-to-end and triage any selector mismatches against the live build (specs are syntactically green but have not been executed yet).
2. Once CP-5 Story 10-2 + AI-3 + AI-4 land, generate UI-E2E-05/06/07 in a follow-up round (will need `TuningShellPO` and a `ComplianceGate` PO).
3. Pipeline E2E (SMOKE-01..08) is a separate generation task — Step 3.5 already prioritized SMOKE-02 first per the test-design recommendation.
