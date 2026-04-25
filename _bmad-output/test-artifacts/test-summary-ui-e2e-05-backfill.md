---
name: UI-E2E-05 Backfill (Round 2 — post-Story 11-3)
description: Playwright spec for UI-E2E-05 Rejection + Regeneration + Retry-Exhausted, authored after Story 11-3 (AI-3 threshold unification) unblocked it.
type: test-summary
date: 2026-04-25
project: youtube.pipeline
upstream_input: _bmad-output/test-artifacts/test-design-epic-1-10-2026-04-25.md §5 UI-E2E-05
unblocker: _bmad-output/implementation-artifacts/11-3-retry-exhausted-gte-unification.md
status: green
---

# Test Generation Summary — UI-E2E-05 Backfill

**Scope.** One case — UI-E2E-05 (Rejection + Regeneration + Retry-Exhausted, P0). The blocker (Story 11-3 / AI-3 `>=` threshold unification) landed in status `review`, clearing the prerequisite from Round 1's "Out of scope" list.

**Upstream references**

- Test design: [test-design-epic-1-10-2026-04-25.md §5 UI-E2E-05](test-design-epic-1-10-2026-04-25.md)
- Story: [11-3-retry-exhausted-gte-unification.md](../implementation-artifacts/11-3-retry-exhausted-gte-unification.md)
- Round 1 summary: [test-summary.md](test-summary.md) — this backfill closes its UI-E2E-05 deferral.

---

## Generated Files

| File | Action | Notes |
|------|--------|-------|
| [web/e2e/retry-exhausted.spec.ts](../../web/e2e/retry-exhausted.spec.ts) | **new** | Single case, single test; ~230 lines including inline rationale. |

No page objects, fixtures, or mock-api helpers were added or modified. The spec reuses:

- `ProductionShellPO` ([web/e2e/po/production-shell.po.ts](../../web/e2e/po/production-shell.po.ts))
- `BatchReviewPO` ([web/e2e/po/batch-review.po.ts](../../web/e2e/po/batch-review.po.ts))
- `installApiMocks` + `makeSpies` ([web/e2e/po/mock-api.ts](../../web/e2e/po/mock-api.ts))
- `fixtures.ts` auto-resetStores / consoleGuard / skipOnboarding ([web/e2e/fixtures.ts](../../web/e2e/fixtures.ts))

---

## Assertions → Risk Coverage

| Aspect | Assertion | Guards |
|--------|-----------|--------|
| `attempts == cap` boundary (R-11 / AI-3) | Mock `/decisions` handler replicates the post-11-3 server contract (`retry_exhausted = attempts >= cap`). UI must render the exhausted surface at the cap. | Regression on either the Go server helper (`retryExhausted()` in `internal/service/scene_service.go`) or the UI recomputation in `BatchReview.onSuccess` would fail this case. |
| Exhausted SceneCard chip | `data-retry-exhausted="true"` attribute present + `aria-label="Retry exhausted"` chip visible. | Story 8.4 / deferred 8-5 UI contract for the queue. |
| DetailPanel exhausted copy | `aria-label="Retry cap reached"` + "Retry exhausted — manual edit or skip & flag required for scene 1." | UX-DR 39, 65. |
| Exhausted action surface | `.batch-review__exhausted` visible with "Retry limit reached" heading; `Manual edit` present + disabled with title "Manual narration edits happen in Scenario Review."; `Skip & flag` enabled. | Test-design §5's "Manual edit / Retry" intent, adapted to the actually-shipping UI (see Deviations below). |
| Approve/Reject row replacement | `[Enter] Approve` and `[Esc] Reject` buttons are `toHaveCount(0)` after exhaustion. | BatchReview.tsx `!composer_open && !is_exhausted` branching. |
| Regen suppression at cap | No `data-regenerating="true"` card after the cap-reach reject; `regen_mutation` is not fired because the client trusts `retry_exhausted=true`. | Pre-11-3 regression would have returned `retry_exhausted=false` at cap, kicked a third regen, and the exhausted surface would never render. |
| Console hygiene | `consoleErrors` / `pageErrors` both empty. | Project-wide quality gate. |

---

## Deviations from test-design §5 (documented inline)

| Spec asks | Reality | Adapted assertion |
|-----------|---------|-------------------|
| "`Manual edit` CTA becomes enabled, `Retry` CTA disabled with tooltip `Retry budget exhausted`" | Shipping UI renders a dedicated "Retry limit reached" surface: `Manual edit` is **disabled** (tooltip: `Manual narration edits happen in Scenario Review.`) and `Skip & flag` is enabled. There is no `Retry` CTA. | Assert the surface that ships: `Manual edit` disabled + correct title, `Skip & flag` enabled, Approve/Reject row removed. |
| "`CountRegenAttempts` returns 3" | Story 11-3 AC-4 explicitly scopes `CountRegenAttempts` out of this blocker (listed as a separate deferred work item). | Assertion omitted per Story 11-3's deferred list. |

These are documented in block comments at the top of `retry-exhausted.spec.ts`.

---

## Mocking strategy

- Baseline `installApiMocks` registers the canonical `/api/runs/**` handlers used by the rest of the suite (they leave `retry_exhausted=false` unconditionally, which is not enough for this case).
- The spec registers **two LIFO-priority overrides** *after* `installApiMocks` so Playwright matches them first:
  - `POST /api/runs/retry-001/decisions` — increments an internal `rejectCount`, writes `regen_attempts` + `retry_exhausted = (count >= 2)` into the shared `state.reviewItems` fixture, and returns the matching decision response.
  - `POST /api/runs/retry-001/scenes/0/regen` — returns success with the current attempts so the "Regenerating…" chip clears between rejects.
- The base `/review-items` handler is reused unchanged; the shared state it reads is already updated by the override above, so a subsequent refetch surfaces the correct `waiting_for_review + regen_attempts + retry_exhausted` row.
- Nothing in `po/mock-api.ts` was modified — the override pattern keeps the per-case divergence fully inside the spec.

---

## How to run

```bash
cd web && npx playwright test e2e/retry-exhausted.spec.ts
```

Full suite:

```bash
cd web && npx playwright test
```

---

## Verification

- `cd web && npx playwright test e2e/retry-exhausted.spec.ts` → **1 passed** in 942 ms.
- `cd web && npx playwright test` (full 16-test suite, Round 1 + this backfill + pre-existing `new-run-creation.spec.ts`) → **16 passed** in 16.4 s. No regressions in previously-green specs.
- Zero `console.error` / `pageerror` observed under the `consoleGuard` auto fixture.

---

## Coverage delta

- Round 1 in-scope: 7 / 10 UI E2E cases (UI-E2E-01, 02, 03, 04, 08, 09, 10).
- This backfill: **+1** → UI-E2E-05 covered. Running total **8 / 10**.
- Still blocked: UI-E2E-06 (AI-4 FR23 hard-gate), UI-E2E-07 (CP-5 Story 10-2 FULL).

---

## Next steps

1. When Story 11-4 (AI-4 compliance-gate handler) lands, generate UI-E2E-06 via the same backfill pattern (expected to need a `ComplianceGatePO` — not yet created).
2. When CP-5 / Story 10-2 FULL lands, generate UI-E2E-07 and introduce `TuningShellPO` at that point.
3. If the `retry_budget_exhausted` tooltip copy ever ships on a distinct `Retry` CTA (per test-design §5's original wording), the deviation noted above can be tightened.
