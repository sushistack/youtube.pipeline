---
title: 'Production stepper — n8n-style fan-out / fan-in expansion'
type: 'feature'
created: '2026-04-25'
status: 'done'
baseline_commit: 'f35582ae847378582c0a805f428d4ad204e00fa9'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/spec-production-master-detail.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The Production header `StageStepper` (variant=full) collapses 14 `RunStage` values into 6 lumped nodes. Operators cannot tell which scenario sub-stage is active (research vs structure vs write vs critic …), or whether assets is currently in image generation, TTS, or batch review — they have to consult logs to know what the run is actually doing.

**Approach:** Add an `'expanded'` variant to `StageStepper` that reveals the real sub-stage topology already encoded in `STAGE_TO_NODE` (in `web/src/lib/formatters.ts`) and confirmed against `internal/pipeline/engine.go:NextStage`. Each lumped node fans into its sequential `RunStage` sub-stages — scenario into 7 sub-nodes, assets into 3 sub-nodes, assemble into 2 sub-nodes. Header gains a chevron toggle persisted in `useUIStore` as `stage_stepper_expanded`. Pre-audit confirmed per-modality counters (`tts_done/total`) and per-sub-agent live progress are absent from the status payload and would need a multi-day backend lift; those are deferred. The expanded view shows real topology + real per-RunStage progress, never mocked counters or fabricated parallelism.

## Boundaries & Constraints

**Always:**
- Preserve `variant: 'full' | 'compact'` rendering bit-for-bit. All current `StageStepper`, `ProductionAppHeader`, and `StatusBar` tests must remain green without source modification.
- Source of truth for sub-nodes is the `RunStage` enum + existing `STAGE_TO_NODE` constant. Sub-stage state is derived from `run.stage` only — no invented progress.
- Toggle persists across reloads via `useUIStore` `partialize` (same pattern as `sidebar_collapsed`). Default `false`.
- Honor `prefers-reduced-motion: reduce` — no pulsing/animated connectors.
- `StatusBar` compact stepper untouched — only the header expands.

**Ask First:**
- Adding a global keyboard shortcut to toggle (e.g. `Shift+P`). The current `SUPPORTED_SHORTCUT_KEYS` registry has no shift+letter slot; extending it is out-of-scope.
- Touching files outside the Code Map. Out-of-scope dirty files in the working tree must remain untouched.

**Never:**
- Render fabricated counters when the underlying signal is absent. Where data is missing, render topology only.
- Persist `stage_stepper_expanded` per-run — it is a global UX preference.
- Change the 6-node thin layout, contract, or test expectations.
- Touch any backend file or `useRunStatus.ts` SSE work — pre-audit verdict is pure-frontend.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output |
|---|---|---|
| Thin default | empty localStorage, `stage=write` | 6-node linear stepper, scenario active, chevron-down visible. |
| Toggle expanded, scenario active | `stage=critic`, click chevron | Scenario row reveals 7 sub-nodes [research, structure, write, visual_break, review, critic, scenario_review]. critic=active; research/structure/write/visual_break/review=completed; scenario_review=upcoming. Persisted. |
| Expanded, assets active (image) | `stage=image` | Assets row reveals 3 sub-nodes [image, tts, batch_review]. image=active; tts/batch_review=upcoming. No fabricated parallel rails. |
| Expanded, assets active (tts) | `stage=tts` | Same 3 sub-nodes. image=completed; tts=active; batch_review=upcoming. |
| Expanded, batch_review w/ decisions | `stage=batch_review`, decisions_summary={approved:8, rejected:2, pending:22} | image/tts=completed; batch_review sub-node = active and shows "10/32 reviewed" derived from approved+rejected over total. |
| Failed mid-scenario | `stage=write`, `status=failed` | Scenario parent=failed; research/structure=completed; write=failed; downstream (visual_break, review, critic, scenario_review)=upcoming. |
| reduced-motion | media query active | Connectors render static dashed (no pulse animation). |
| Persistence reload | expanded=true → reload | Header rehydrates expanded immediately; localStorage parse error → default false. |

</frozen-after-approval>

## Code Map

- `web/src/lib/formatters.ts` -- Add ordered constants `SCENARIO_SUB_STAGES`, `ASSETS_SUB_STAGES`, `ASSEMBLE_SUB_STAGES`; `SubStageNodeModel` type; `buildStageGraph(stage, status, decisions_summary?)` returning `{ nodes, sub_nodes }`. Existing exports untouched.
- `web/src/components/shared/StageStepper.tsx` -- Accept `variant: 'full' | 'compact' | 'expanded'` and optional `decisions_summary`. Expanded renders parent row + sub-rails; reuses `NODE_ICONS`. SVG connector inline.
- `web/src/components/shared/StageStepper.test.tsx` -- Keep current 2 tests untouched. Add expanded-mode tests: scenario sub-state, assets sub-state (image/tts/batch_review), decisions counter, failed sub-state.
- `web/src/components/shared/ProductionAppHeader.tsx` -- Read `stage_stepper_expanded` + setter from store. Render chevron toggle on right edge. Accept optional `status_payload`; pass variant + decisions_summary down.
- `web/src/components/shared/ProductionAppHeader.test.tsx` -- Add tests: chevron renders, click toggles store, expanded variant flows down.
- `web/src/stores/useUIStore.ts` -- Add `stage_stepper_expanded: boolean` (default false), `toggle_stage_stepper_expanded()`, include in `partialize`.
- `web/src/stores/useUIStore.test.ts` -- Cover default, toggle, persisted shape.
- `web/src/index.css` -- Add `[data-variant="expanded"]`, `.stage-stepper__rail`, `.stage-stepper__sub-node`, `.stage-stepper__connector`, `.production-app-header__toggle` rules with `prefers-reduced-motion` guard.
- `web/src/components/shells/ProductionShell.tsx` -- (Conditional) thread `status_payload` into `ProductionAppHeader` if needed for the decisions counter; verify during impl — skip if header doesn't need it.

## Spec Change Log

- **2026-04-25 (post-approval correction, user-authorized [C])**: Frozen Intent claimed assets fans into "parallel TTS + Image rails", and Design Notes had scenario order `research → structure → write → review → critic → scenario_review → visual_break`. Both contradicted `internal/pipeline/engine.go:NextStage`, which encodes scenario as `research → structure → write → visual_break → review → critic → scenario_review` and assets as sequential `image → tts → batch_review`. Internal `phase_b.go` errgroup parallelism is not observable from the polled status payload. Corrected Intent, I/O matrix (split assets row by image/tts), Code Map (renamed `ASSETS_PARALLEL_STAGES` → `ASSETS_SUB_STAGES`), Acceptance Criteria, and Design Notes. **KEEP**: the no-fabrication principle is reinforced — sub-nodes only ever reflect what `run.stage` actually says.
- **2026-04-25 (review patches, no spec change)**: Step-04 review surfaced two patch-class findings: (1) batch_review counter rendered "0/0 reviewed" when `decisions_summary` was present with all zeros — added a `total > 0` guard in `buildStageGraph` and a regression test in `formatters.test.ts`; (2) under `prefers-reduced-motion: reduce` the active rail's `border-left-style` switched dashed→solid even though the spec promised "static dashed" — extended the reduced-motion media query to also revert `border-left-style: dashed`. Three findings deferred to `deferred-work.md` (counter loss past batch_review, aria-pressed/aria-label SR double-announce, sub-label ellipsis without title). All other review findings rejected (engine invariants or pre-existing patterns).

## Tasks & Acceptance

**Execution:**
- [x] `web/src/lib/formatters.ts` -- Add sub-stage constants + `SubStageNodeModel` + `buildStageGraph` helper.
- [x] `web/src/stores/useUIStore.ts` -- Add `stage_stepper_expanded` + `toggle_stage_stepper_expanded`; include in partialize.
- [x] `web/src/stores/useUIStore.test.ts` -- Cover default + toggle + persistence.
- [x] `web/src/components/shared/StageStepper.tsx` -- Add expanded variant rendering parent row + sub-rails; accept optional decisions_summary.
- [x] `web/src/components/shared/StageStepper.test.tsx` -- Add 4 expanded-variant tests; preserve existing 2.
- [x] `web/src/components/shared/ProductionAppHeader.tsx` -- Wire toggle + store; pass variant + decisions_summary down.
- [x] `web/src/components/shared/ProductionAppHeader.test.tsx` -- Add toggle/click/persist coverage.
- [x] `web/src/index.css` -- Add expanded styles + reduced-motion guard + toggle button.
- [x] `web/src/components/shells/ProductionShell.tsx` -- (Conditional) pass status_payload to header.

**Acceptance Criteria:**
- Given empty `useUIStore`, when Production loads, then thin stepper renders unchanged and chevron-down is visible.
- Given any run state, when the user clicks the toggle, then `stage_stepper_expanded` flips in store, chevron flips, stepper expands, and the state survives reload.
- Given `stage=critic`, when expanded, then under scenario 7 sub-nodes render in order [research, structure, write, visual_break, review, critic, scenario_review] with all earlier=completed, critic=active, scenario_review=upcoming.
- Given `stage=image`, when expanded, then assets reveals 3 sub-nodes [image, tts, batch_review] with image=active, tts/batch_review=upcoming. For `stage=tts`, image=completed and tts=active.
- Given `stage=batch_review` with decisions_summary={8,2,22}, when expanded, then batch_review sub-node is active and shows "10/32 reviewed".
- Given `prefers-reduced-motion: reduce`, when expanded renders, then no pulse animation runs on connectors.
- All existing vitest suites pass without source modification beyond additions noted; `npm run typecheck` and `npm run test:unit` green.

## Design Notes

**Sub-stage order (verified against `internal/pipeline/engine.go:NextStage`, NOT invented):**
- Scenario sequential (7): `research → structure → write → visual_break → review → critic → scenario_review`.
- Assets sequential (3): `image → tts → batch_review`. The decisions_summary counter renders on the `batch_review` sub-node when present.
- Assemble sequential (2): `assemble → metadata_ack`.

**Why no parallel rails for assets:** `phase_b.go` runs image_track and tts_track in parallel via errgroup *internally*, but the persisted `domain.Run.stage` (which is what the operator-visible status payload exposes) cycles through `image → tts → batch_review` sequentially per the state machine. Drawing parallel rails would fabricate concurrency the polled signal cannot express. If per-modality progress is added in a future epic, parallel rails can be reconsidered then.

**Why no fabricated counters:** Pre-audit confirmed `domain.Run` has no `tts_done/total`, `image_done/total`, or `current_sub_agent`. Adding them is multi-day backend work spanning `phase_b.go`, `domain/types.go`, persistence, SSE — beyond the "1-2 fields + 1-2 events" budget the user authorized. Topology + parent-derived state is enough operator value to ship now; granular counters belong in a separate epic and go to deferred-work.

**Commit split (user-confirmed):**
1. `feat(stepper): formatters helpers + useUIStore toggle (regression-safe)` — adds helpers + store field; `StageStepper` render path unchanged (expanded variant accepted but falls through to current behavior). Confirms thin mode is bit-identical.
2. `feat(stepper): expanded fan-out variant + header toggle + CSS` — implements expanded rendering, header toggle, CSS, expanded-variant tests.

## Verification

**Commands (from `web/`):**
- `npm run typecheck` -- expected: no TS errors.
- `npm run test:unit -- StageStepper ProductionAppHeader useUIStore StatusBar ProductionShell` -- expected: all green.

**Manual checks:**
- `npm run dev`, run on `stage=write`: thin stepper unchanged, chevron toggles to expanded, scenario sub-rail shows correct active/completed states.
- Reload after toggling: expanded state persists.
- OS "reduce motion" toggle: connectors stop pulsing.
- Seed `stage=batch_review` with decisions_summary: "10/32 reviewed" renders; without decisions_summary, no counter (not "0/0").

## Suggested Review Order

**Data layer — entry point**

- `buildStageGraph` is the single source of truth for sub-stage state, derived from `run.stage` + `decisions_summary` only (no fabrication).
  [`formatters.ts:387`](../../web/src/lib/formatters.ts#L387)

- Sub-stage ordering constants verified against the engine's `NextStage` state machine.
  [`formatters.ts:113`](../../web/src/lib/formatters.ts#L113)

**Render — expanded variant**

- Variant fork: `expanded` runs a different render branch; `full`/`compact` paths are bit-identical for regression safety.
  [`StageStepper.tsx:37`](../../web/src/components/shared/StageStepper.tsx#L37)

**UX wiring — toggle + payload thread**

- Header reads `stage_stepper_expanded` from the store, flips chevron + variant, and threads `decisions_summary` down.
  [`ProductionAppHeader.tsx:23`](../../web/src/components/shared/ProductionAppHeader.tsx#L23)

- Shell now passes `status_payload` so the batch_review counter has a data source.
  [`ProductionShell.tsx:325`](../../web/src/components/shells/ProductionShell.tsx#L325)

**Persistence**

- New global UX preference field with `partialize` — defaults to `false`, survives reload.
  [`useUIStore.ts:76`](../../web/src/stores/useUIStore.ts#L76)
  [`useUIStore.ts:129`](../../web/src/stores/useUIStore.ts#L129)

**Styling — pulse + reduced-motion**

- Active rail gets a CSS opacity pulse via `:has()`; reduced-motion guard zeroes the animation and reverts the dashed border.
  [`index.css:1218`](../../web/src/index.css#L1218)
  [`index.css:1293`](../../web/src/index.css#L1293)

**Tests (peripheral)**

- `buildStageGraph` derivation: every parent group, the failed sub-state, the all-zeros counter suppression.
  [`formatters.test.ts:130`](../../web/src/lib/formatters.test.ts#L130)

- Component tests cover the expanded sub-rails, sequential image→tts→batch_review, and toggle store binding.
  [`StageStepper.test.tsx:28`](../../web/src/components/shared/StageStepper.test.tsx#L28)
  [`ProductionAppHeader.test.tsx:28`](../../web/src/components/shared/ProductionAppHeader.test.tsx#L28)
