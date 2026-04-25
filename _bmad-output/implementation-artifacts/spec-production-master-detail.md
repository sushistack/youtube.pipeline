---
title: 'Production page — Direction B Master-Detail shell'
type: 'refactor'
created: '2026-04-25'
status: 'done'
baseline_commit: 'b2b3e444b2ded164c343beb01e48d003a178d56d'
context:
  - '{project-root}/_bmad-output/planning-artifacts/ux-design-directions.html'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The Production page is a single-column dashboard scattering run identity, stage progress, telemetry, and review actions across stacked cards. It does not match Direction B (Master-Detail Split) in `ux-design-directions.html`, the agreed UX target — which expects a sticky app header (run title + 6-stage stepper), a left/right body (scene list ↔ scene detail), and a bottom status bar.

**Approach:** Refactor `ProductionShell` into a 4-zone layout (top header / left scene queue / right stage-aware detail / bottom status bar), reusing existing parts (`StageStepper`, `StatusBar`, `BatchReview`, `SceneCard`, `DetailPanel`). Hoist stage progress + selected-run identity into a new `ProductionAppHeader`. The body is master-detail when scenes exist, otherwise the right pane falls back to today's stage-routed content. Augment `DetailPanel` with the 3×2 metric grid the mockup specifies (4 fields mapped from `critic_breakdown`, 2 placeholders). Strip Sidebar of search and dual section headers, switch nav letter-icons to SVG, compact run cards to a one-line dot+title row.

## Boundaries & Constraints

**Always:**
- Reuse existing components and contracts. No new HTTP endpoints, no contract changes.
- Keep keyboard model: J/K/Enter/Esc/S/Shift+Enter/Ctrl+Z continue working in `BatchReview` exactly as today.
- Preserve current stage-aware behavior when scenes are unavailable (pending Start-run panel, `ScenarioInspector`, `CharacterPick`, `ComplianceGate`, `CompletionReward`) — the redesign is a *frame*, not a logic rewrite.
- Keep bootstrap-once URL `?run=` selection invariant in `ProductionShell` (do not reintroduce the race fixed in `4255a4c`).
- Run name displayed as `Run #N` everywhere user-visible.

**Ask First:**
- Touching any non-Production-related dirty file (Go backend, `tuning/*`, `tuningContracts.ts`). Commit-scope rule forbids spillover.
- Adding endpoints or extending `reviewItemSchema` — append to `deferred-work.md` instead.

**Never:**
- Hide an actionable surface by stage-gating (e.g. dropping the `BatchReview` action bar in `batch_review/waiting`).
- Fabricate VISUAL/SCP_ACCURACY values to disguise missing critic fields. Render `—` with a tooltip explaining absence.
- Reintroduce the removed `mod+n` keybinding for New Run.
- Revert dirty Sidebar/`NewRunPanel` work — build on top.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|---|---|---|---|
| Run pending, never started | `stage=pending` & `status=pending` | Right pane: existing "Run Created / Start run" panel. Left pane: empty-scenes placeholder. Header stepper highlights `pending`. | Start-run mutation error inline (existing copy). |
| Batch review waiting | `stage=batch_review` & `status=waiting`, review-items fetched | Master-Detail: left = `SceneCard` list, right = `DetailPanel` + 6-metric grid + action bar. J/K/Enter/Esc/S/Shift+Enter/Ctrl+Z work. | Fetch error → existing `batch-review__error` block in right pane. |
| Non-review stage, no scenes yet | e.g. `stage=research`, fetch empty/404 | Left pane placeholder "Scenes will appear once Phase A finishes." Right pane: current stage-routed content. Status bar live. | Network error → header + status bar render; right pane shows error. |
| 6-metric mapping | `critic_breakdown.{hook_strength,fact_accuracy,emotional_variation,immersion}` present | NARRATION=hook_strength, COHERENCE=immersion, PACING=emotional_variation, SCP_ACCURACY=fact_accuracy, VISUAL=`—` (tooltip "metric not yet emitted by critic"), AUDIO=`—` (same tooltip). | Missing breakdown → all 6 render `—`. |
| Sidebar Recent Runs | `runs_query.data` returns N runs | Single "Recent runs" header. Each run = one line: status dot + `SCP-XXX Run #N`. Selected highlighted. | Empty list → "No runs yet." (existing). |

</frozen-after-approval>

## Code Map

- `web/src/components/shells/ProductionShell.tsx` -- 4-zone layout; remove hero/stage-progress-panel/3-metric-cards/`ProductionShortcutPanel`; keep pending state, continuity & failure banners, bootstrap selection.
- `web/src/components/shared/ProductionAppHeader.tsx` -- **new**. Sticky header: run id + `StageStepper variant="full"` + `+ New Run` button (delegates to `useNewRunCoordinator`).
- `web/src/components/shared/ProductionMasterDetail.tsx` -- **new**. Slot wrapper rendering `<aside>` left + `<section>` right; scene-empty placeholder when left empty.
- `web/src/components/shared/DetailPanel.tsx` -- replace 5-row Critic-breakdown list with 3×2 metric grid per Matrix row 4; keep narration, audio, hero/clip, gallery, diff sections intact.
- `web/src/components/shared/Sidebar.tsx` -- remove search input + dual section headers; single "Recent runs" block; replace PR/TU/SE letter icons with `lucide-react` icons (Production=`LayoutGrid`, Tuning=`SlidersHorizontal`, Settings=`Settings`).
- `web/src/components/shared/RunCard.tsx` -- compact one-line: status dot + `SCP-{scp_id} Run #N` (selection highlight retained).
- `web/src/components/shared/StatusBar.tsx` -- ensure compact line surfaces decisions counts (`n approved · n pending · n rejected`); existing hover-expand keeps run id + token totals.
- `web/src/index.css` -- add `.production-shell__layout`, `.production-master-detail__*`, `.production-app-header__*`, `.detail-panel__metrics-grid`. Delete dead `.production-dashboard__hero*`, `.__metrics*`, `.__panel*` rules.
- `web/src/components/shells/ProductionShell.test.tsx` -- assert new header rendered, master-detail in batch_review, pending state intact, deleted hero/metric-cards no longer present.
- `web/src/components/shared/Sidebar.test.tsx` -- drop search-input cases; add Recent-runs single-section, lucide-icon, compact run-row assertions.
- `web/src/components/shared/DetailPanel.test.tsx` -- **new (or augment)**. Cover 6-metric grid mappings + `—` placeholders.
- `web/src/components/shared/ProductionAppHeader.test.tsx` -- **new**. Renders run id + stepper; New Run click invokes coordinator.
- `web/src/components/shared/ProductionMasterDetail.test.tsx` -- **new**. Renders left + right slots; empty-scene placeholder.
- `web/e2e/fixtures.ts` -- only adjust if existing fixture data breaks under the new layout.

## Tasks & Acceptance

**Execution:**
- [x] `web/src/components/shared/ProductionAppHeader.tsx` -- new component (run identity + StageStepper). Note: the New Run trigger originally proposed for the header was DROPPED during implementation because Sidebar already exposes the same affordance (with full aria-label match), and rendering it twice broke the existing post-create test by ambiguating the `Create a new pipeline run` button query. The Sidebar's button is the single source of truth. (See Spec Change Log entry SCL-1.)
- [x] `web/src/components/shared/ProductionMasterDetail.tsx` -- new slot wrapper (left scenes / right detail + empty placeholder).
- [x] `web/src/components/shells/ProductionShell.tsx` -- swapped dashboard for header+master-detail+status-bar; deleted hero/metrics/stage-panel/`ProductionShortcutPanel` render branches; preserved pending state, banners, bootstrap-once URL selection.
- [x] `web/src/components/shared/DetailPanel.tsx` -- introduced 3×2 metric grid; mapped 4 critic fields, rendered `—` w/ tooltip for VISUAL & AUDIO; null-mapped fields also render `—` (see SCL-2).
- [x] `web/src/components/shared/Sidebar.tsx` -- single Recent-runs section, lucide nav icons (LayoutGrid/SlidersHorizontal/Settings) replacing PR/TU/SE letters, search input removed, brand mark switched to play-icon SVG.
- [x] `web/src/components/shared/RunCard.tsx` -- compact one-line: dot + `SCP-XXX Run #N` only.
- [x] `web/src/components/shared/StatusBar.tsx` -- compact line now surfaces decisions counts (`n approved · n pending · n rejected`) and a `⌘K Command` hint.
- [x] `web/src/index.css` -- added new layout rules; deleted dead `.production-dashboard__hero/__panel/__metric*/__hero-copy/__hero-meta/__meta` rules and dead `.run-card__header/__footer/__scp/__summary/__critic*` rules.
- [x] `web/src/components/shells/ProductionShell.test.tsx` -- replaced search-filter test with Master-Detail layout assertions; added EventSource stub in beforeEach to keep SSE-based `useRunStatus` inert (this is a baseline test gap — see SCL-3).
- [x] `web/src/components/shared/Sidebar.test.tsx` -- no changes required: existing tests don't reference removed search input directly and continue to pass against the refactored Sidebar.
- [x] `web/src/components/shared/DetailPanel.test.tsx` -- added 6-metric grid coverage including critic-breakdown=null placeholder case; queries by `data-metric` attribute to avoid clashes with section heading text (SCL-2).
- [x] `web/src/components/shared/ProductionAppHeader.test.tsx` -- new (3 cases: identity formatting, empty-state, raw-id fallback).
- [x] `web/src/components/shared/ProductionMasterDetail.test.tsx` -- new (3 cases: both slots, empty-master placeholder, custom labels).

**Acceptance Criteria:**
- Given a pending run, when I open `/production?run={id}`, then the new top header shows run identity + stepper, the right pane renders "Run Created / Start run", and the left pane shows the empty-scenes placeholder.
- Given a `batch_review/waiting` run, when I open the page, then the right pane renders `DetailPanel` with the 3×2 metric grid, and J/K/Enter/Esc keystrokes drive navigation/approve/reject exactly as today.
- Given the sidebar is open on `/production`, when I look at the inventory, then I see a single "Recent runs" heading (no search), each run is a one-line `● SCP-XXX Run #N` row, and selection highlight matches the URL `?run=`.
- Given a `running`/`waiting` run, when I look at the bottom, then `StatusBar` compact line shows `Stage · Elapsed · Cost · decisions counts` and expands on hover/focus.
- Given the redesign ships, when I run vitest, then all suites pass and no test still asserts the deleted hero / 3-metric-card / `ProductionShortcutPanel` structure.

## Spec Change Log

**SCL-1** — Trigger: vitest run revealed two buttons matching `aria-label="Create a new pipeline run"` (one in Sidebar, one in the new ProductionAppHeader), causing `getByRole('button', { name: 'Create a new pipeline run' })` to throw `multiple elements found` in the existing post-create flow test. Amendment: dropped the New Run trigger from `ProductionAppHeader` and removed its CSS rules; Sidebar remains the single source of truth (matches the Direction B mockup which has no header-bound New Run button). KEEP: header zone is purely identity + 6-stage stepper, not action-bearing.

**SCL-2** — Trigger: vitest assertion `getByText('Narration')` matched both the metric label and the existing narration section heading inside the same panel. Amendment: tests now query metric cards by `[data-metric="…"]` attribute. Implementation also extended the missing-value rule so any null mapped score (not only the explicit VISUAL/AUDIO placeholders) renders `—`, keeping the grid consistent when `critic_breakdown` is fully null. KEEP: the labels themselves stay in plain English (Visual/Narration/Coherence/Pacing/SCP Accuracy/Audio) — do NOT rename them to disambiguate.

**SCL-3** — Trigger: the dirty `useRunStatus.ts` introduced an `EventSource(...)` call in a `useEffect`, but `ProductionShell.test.tsx` never received an EventSource stub (only `useRunStatus.test.tsx` has one). Six baseline tests therefore failed with `ReferenceError: EventSource is not defined`. Amendment: added a no-op `EventSource` stub via `vi.stubGlobal` in this test's `beforeEach` (and `vi.unstubAllGlobals` in `afterEach`). This is a surgical test-side fix; the global polyfill belongs in `src/test/setup.ts` but that file is shared with other tests outside this scope, so it stays as deferred work. KEEP: do not add EventSource polyfill to other unrelated tests in this commit.

**SCL-4** — Step-04 adversarial review patches (9 issues, all classified `patch`, no `intent_gap` or `bad_spec` loopback):

- **P-1** Pending-state heading rendered the raw `current_run.id` ("scp-049-run-2") instead of the spec-mandated `Run #N` format. Amendment: pending state heading now reads `SCP-{scp_id} Run #{seq}` via `getRunSequence`, with the raw id moved to the summary line. Test assertions updated to match. (Acceptance auditor MAJOR-2.)
- **P-2** Sidebar.test.tsx promised additive coverage in the spec but only the search-input drop was honored; added two new tests asserting the single "Recent runs" heading + lucide SVG nav icons + compact dot+title rows. (Acceptance auditor MAJOR-4.)
- **P-3** `ScenarioInspector` rendered without `key={current_run.id}` while every sibling stage branch keys on run id. Switching between two scenario-review runs reused the same instance and leaked `active_index` state. Amendment: added the key. (Edge-case hunter MAJOR-1.)
- **P-4** `⌘K Command` hint in StatusBar advertised a command palette that has no handler anywhere. Amendment: removed the hint span and its CSS rule until/unless a Cmd+K handler is wired. (Blind hunter major-5, edge-case major-7.)
- **P-5** `ProductionAppHeader` reserved a `1fr` stepper column even when `run` is null, leaving an empty band on the empty-state page. Amendment: header now sets `data-empty="true"` and the CSS collapses to a single auto column when no run is selected. (Edge-case minor-14.)
- **P-6** `ProductionMasterDetail` always rendered "Scenes will appear once Phase A finishes." even for stages long after Phase A (scenario_review / character_pick / metadata_ack). Amendment: ProductionShell now passes a stage-aware `master_empty_message` — Phase-A copy only when stage=pending, otherwise a "{stage_label} in progress" message. (Acceptance auditor MINOR-8.)
- **P-7** Critic's `aggregate_score` was silently dropped from the new 6-metric grid. Amendment: DetailPanel header now renders the aggregate as a tone-coded badge next to the scene title, alongside a review-status badge — matching the Direction B mockup's "Scene N — title [score] [status]" header row. (Blind hunter major-2, edge-case major-5.)
- **P-8** Missing-metric placeholders used `title="metric not yet emitted by critic"` only — keyboard and SR users had no signal. Amendment: each metric `<li>` carries an `aria-label` covering the missing/unavailable/scored cases. (Blind hunter minor-12, edge-case minor-12.)
- **P-9** `ProductionShortcutPanel.tsx` had no remaining importers after the shell refactor — orphan file. Amendment: deleted the file. (Acceptance auditor NIT-11.)

KEEP for future re-derivation: the 6-metric mapping, `BatchReview` rendered as peer of `ProductionMasterDetail`, single Sidebar Recent-runs section, lucide nav icons. These are validated design choices.

REJECTED findings (not real defects in this story):
- All "non-Production files modified" findings (Go backend, tuning, fixtures, NewRunPanel modal, advance endpoint) — pre-existing dirty baseline, user explicitly accepted in step-01 ("dirty를 starting baseline으로 흡수").
- "Search input removed" / "mod+n shortcut removed" — explicit spec requirements (Sidebar cleanup, never-reintroduce-mod+n).
- Critic field remapping (hook_strength→Narration etc.) — spec-defined; UX accepts the lossiness with `aria-label` clarity.
- BatchReview renders as peer of ProductionMasterDetail — per Design Notes ("internal master-detail wrapper is collapsed into the new shell to avoid double-framing").
- Decision summary `rejected_count` "missing field" — verified present in `decisionsSummarySchema`.

DEFERRED to `deferred-work.md`:
- `advance_mutation` race conditions (run-switch double-dispatch, 202-before-DB-commit, error message length) — pre-existing in dirty advance flow.
- Score=0 vs missing-field tooltip distinction — rare edge case.
- `EventSource` polyfill belongs in `setup.ts`, not per-test — already documented.
- Phone/narrow viewport tab-toggle for Master-Detail — already documented.
- 6-metric VISUAL/AUDIO backend extension — already documented.

## Design Notes

The "DIRECTION B" badge in the mockup is a per-page label in the design HTML and is NOT carried into the implementation. Header run identity is `SCP-{scp_id} Run #{n}` where `n` is derived from the sorted inventory (newest=highest); if the seq cannot be derived cheaply, fall back to the full run id rather than inventing a number.

6-metric mapping is intentionally lossy: VISUAL and AUDIO have no critic source today and render `—` with a `title` "metric not yet emitted by critic". File the surfacing of these as a deferred backend item in `deferred-work.md`.

The 4-zone *frame* is always visible. Body content adapts: scenes list is empty before `scenario_review`; we render a single placeholder in the left pane and keep the existing stage-routed component (pending banner / `ScenarioInspector` / `CharacterPick` / `ComplianceGate` / `CompletionReward`) in the right. At `batch_review/waiting`, the right pane is the full `BatchReview` detail surface (its internal master-detail wrapper is collapsed into the new shell to avoid double-framing).

## Verification

**Commands:**
- `cd web && npm run typecheck` — expected: no errors
- `cd web && npm run test -- --run` — expected: all vitest suites green
- `cd web && npm run e2e -- --reporter=line` — expected: Playwright smoke green when local stack available

**Manual checks:**
- Open `/production?run={any}` in dev: confirm 4-zone layout. Switch runs via sidebar to confirm header + right pane swap. At a `batch_review/waiting` run, press J/K/Enter/Esc and verify keyboard model.

## Suggested Review Order

**Shell skeleton — start here**

- 4-zone layout (header / banners / master-detail / status bar) — entry point for the redesign.
  [`ProductionShell.tsx:341`](../../web/src/components/shells/ProductionShell.tsx#L341)

- Sticky run identity + 6-stage stepper, empty state collapses to single column (P-5 fix; SCL-1: no header-bound New Run).
  [`ProductionAppHeader.tsx:25`](../../web/src/components/shared/ProductionAppHeader.tsx#L25)

- Slot wrapper (left scenes / right detail) with parameterized empty-master copy.
  [`ProductionMasterDetail.tsx:8`](../../web/src/components/shared/ProductionMasterDetail.tsx#L8)

**Stage routing inside the detail slot**

- `renderStageDetail` dispatches per stage; ScenarioInspector now keys on `run.id` (P-3 fix).
  [`ProductionShell.tsx:273`](../../web/src/components/shells/ProductionShell.tsx#L273)

- Pending-state heading rendered as `SCP-XXX Run #N` (P-1 fix).
  [`ProductionShell.tsx:214`](../../web/src/components/shells/ProductionShell.tsx#L214)

- Stage-aware empty-master message helper (P-6 fix).
  [`ProductionShell.tsx:309`](../../web/src/components/shells/ProductionShell.tsx#L309)

**Detail panel: 6-metric grid + scene title row**

- Aggregate score badge + status badge in title row — mockup match (P-7 fix).
  [`DetailPanel.tsx:75`](../../web/src/components/shared/DetailPanel.tsx#L75)

- 3×2 metric grid with 4 mapped critic fields + 2 placeholders + per-card aria-label (P-8 fix).
  [`DetailPanel.tsx:161`](../../web/src/components/shared/DetailPanel.tsx#L161)

**Sidebar cleanup**

- Single `Recent runs` section header (no search input, no dual headers).
  [`Sidebar.tsx:184`](../../web/src/components/shared/Sidebar.tsx#L184)

- Compact dot+title row replaces verbose card; tone derived from run status.
  [`RunCard.tsx:23`](../../web/src/components/shared/RunCard.tsx#L23)

**Status bar: telemetry tightening**

- Decisions counts on compact line; ⌘K hint removed (P-4 fix).
  [`StatusBar.tsx:72`](../../web/src/components/shared/StatusBar.tsx#L72)

**Styling — layout grid + dead-rule purge**

- New `.production-shell`/`.production-app-header`/`.production-master-detail`/`.detail-panel__metrics-grid` rules; legacy hero/panel/metric-card rules deleted.
  [`index.css:1256`](../../web/src/index.css#L1256)

**Tests — what locks the design**

- Master-Detail layout assertions + EventSource stub (SCL-3) + Run #N pending heading (P-1).
  [`ProductionShell.test.tsx:86`](../../web/src/components/shells/ProductionShell.test.tsx#L86)

- 6-metric grid + null-breakdown placeholder coverage (SCL-2).
  [`DetailPanel.test.tsx:53`](../../web/src/components/shared/DetailPanel.test.tsx#L53)

- Recent-runs section + lucide-icon + dot+title assertions (P-2 fix).
  [`Sidebar.test.tsx:101`](../../web/src/components/shared/Sidebar.test.tsx#L101)

