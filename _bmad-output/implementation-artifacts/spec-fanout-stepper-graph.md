---
title: 'Production stepper expanded — DAG canvas (n8n-style graph)'
type: 'feature'
created: '2026-04-25'
status: 'done'
follows: 'spec-fanout-stepper.md'
baseline_commit: '4281d3cca93961e815ea4bd8784b74e833d23737'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/spec-fanout-stepper.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The expanded variant shipped in `spec-fanout-stepper` renders sub-stages as a vertical bullet list under each parent column — visually a list, not a graph. The Direction-B mockup target is an n8n-style DAG canvas with nodes connected by edges (branching + merging). Operators cannot see the parallel image/TTS fan-out at all because the previous spec collapsed it into a sequential chain to honor the polled `run.stage` strictly.

**Approach:** Replace the expanded variant's render path with a **`@xyflow/react`** + **`dagre`** graph canvas. All 15 RunStages render as nodes in a left-to-right DAG laid out by dagre. Edges follow the engine's `NextStage` state machine, with one deliberate divergence: `image` and `tts` are drawn as **parallel branches** from `character_pick`, merging at `batch_review`. This matches `phase_b.go`'s real errgroup parallelism (both tracks run within one PhaseBRunner.Run invocation) and is the strongest argument for a graph viz in the first place — without the fork the canvas adds nothing over a stepper. When `run.stage ∈ {image, tts}`, both branches render as `active` (we still cannot distinguish which track is currently busy from polled signal). Thin (`full`/`compact`) variant render path remains untouched.

## Boundaries & Constraints

**Always:**
- Preserve `variant: 'full' | 'compact'` rendering bit-for-bit. All `StageStepper` / `ProductionAppHeader` / `StatusBar` regression tests stay green without source modification.
- Source of node state is `run.stage` + optional `decisions_summary` only. No invented progress; no per-track counters.
- Reuse the `useUIStore.stage_stepper_expanded` toggle (already shipped).
- Honor `prefers-reduced-motion: reduce` — disable any pulse / flow animation on edges and nodes.
- Library footprint: add only `@xyflow/react` + `dagre` (+ `@types/dagre`). No other new runtime deps.

**Ask First:**
- Adding additional libraries (e.g., for icons beyond `lucide-react`).
- Touching files outside the Code Map.

**Never:**
- Render fabricated counters or invented states. The image/tts parallel render is justified by `phase_b.go` errgroup, not by fictitious per-track polling.
- Modify the thin/compact stepper layout, contract, tests.
- Persist the toggle per-run.
- Touch backend or `useRunStatus.ts`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output |
|---|---|---|
| Thin default | empty store | Existing 6-node thin stepper unchanged. Chevron-down visible. |
| Toggle expanded | click chevron | Header expands. Canvas renders 15 nodes laid out left-to-right by dagre. State persists across reload (existing `useUIStore` contract). |
| Scenario active | `stage=critic` | Nodes pending → research → … → review = `completed`; `critic` = `active`; `scenario_review` and downstream = `upcoming`. Edges entering completed nodes are `completed` styled; edge entering `critic` is `active` (animated). |
| Assets parallel active (image) | `stage=image` | `character_pick` = completed. Both `image` and `tts` branches = `active`. Edges from `character_pick` to both = `active`. Branches merge into `batch_review` = `upcoming`. |
| Assets parallel active (tts) | `stage=tts` | Same parallel render — both branches still `active` (cannot distinguish which is finishing). `batch_review` = `upcoming`. |
| Assets review | `stage=batch_review`, `decisions_summary={8,2,22}` | `image`, `tts` = `completed`. `batch_review` = `active` and node body shows "10/32 reviewed" derived from `decisions_summary`. No counter when summary missing or all zeros. |
| Failed mid-scenario | `stage=write`, `status=failed` | Predecessors = `completed`; `write` = `failed` (red); downstream = `upcoming`. Edge into `write` = `failed`. |
| reduced-motion | media query active | No animated edges; static dashed for `active` edges instead. |

</frozen-after-approval>

## Code Map

- `web/package.json` -- Add `@xyflow/react`, `dagre`, `@types/dagre` dependencies. Run `npm install`.
- `web/src/lib/formatters.ts` -- Add `buildStageDagTopology(stage, status, decisions_summary?)` returning `{ nodes: StageDagNodeModel[], edges: StageDagEdgeModel[] }`. Reuse `RunStage` enum + the engine ordering already verified in `SCENARIO_SUB_STAGES`/`ASSETS_SUB_STAGES`/`ASSEMBLE_SUB_STAGES`. The existing `buildStageGraph` (list-style) stays exported because the unit tests cover it; mark as deprecated in a comment.
- `web/src/lib/dagLayout.ts` -- New thin module wrapping `dagre`. Exports `layoutStageDag(nodes, edges, direction='LR')` returning nodes with `{ x, y }` positions. Pure function, deterministic, server-renderable for tests.
- `web/src/components/shared/StageGraphView.tsx` -- New component. Inputs `(stage, status, decisions_summary?)`. Calls `buildStageDagTopology` + `layoutStageDag`, renders `<ReactFlow>` with custom `StageGraphNode` for each node and styled edges. Disables drag/zoom (display-only). Reduced-motion guard via prop derived from `window.matchMedia`.
- `web/src/components/shared/StageGraphNode.tsx` -- Custom xyflow node. Status dot + label + optional counter (batch_review). Same data-state attributes for CSS.
- `web/src/components/shared/StageStepper.tsx` -- In `expanded` branch, replace the column-and-rail markup with `<StageGraphView … />`. Other variants untouched.
- `web/src/components/shared/StageGraphView.test.tsx` -- New tests: 15 nodes render, sequential states, parallel image/tts active when stage=image, decisions counter on batch_review, failed propagation. Use `@xyflow/react`'s `<ReactFlowProvider>` wrapper.
- `web/src/components/shared/StageStepper.test.tsx` -- Update expanded-variant tests: previous list-DOM-based selectors replaced with graph-DOM selectors (or moved to `StageGraphView.test.tsx`). Existing thin/compact tests preserved.
- `web/src/index.css` -- Replace the previous expanded list/rail/sub-node CSS with graph-canvas styles: `.stage-graph`, `.stage-graph__node`, `.stage-graph__edge`, state-derived colors, reduced-motion guard.

## Tasks & Acceptance

**Execution:**
- [x] `web/package.json` -- Add `@xyflow/react`, `dagre`, `@types/dagre`; commit `package-lock.json`.
- [x] `web/src/lib/formatters.ts` -- Add `StageDagNodeModel`, `StageDagEdgeModel`, `buildStageDagTopology`. Topology hard-coded from engine ordering with image/tts as parallel children of `character_pick` merging into `batch_review`.
- [x] `web/src/lib/formatters.test.ts` -- Cover topology: 15 nodes, parallel image/tts edges, decisions counter on batch_review.
- [x] `web/src/lib/dagLayout.ts` -- Wrap dagre LR layout, deterministic, exported.
- [x] `web/src/components/shared/StageGraphView.tsx` -- ReactFlow canvas; non-interactive; reduced-motion aware.
- [x] `web/src/components/shared/StageGraphNode.tsx` -- Custom node with status dot + label + optional counter.
- [x] `web/src/components/shared/StageGraphView.test.tsx` -- Render + state + counter + parallel + failed.
- [x] `web/src/components/shared/StageStepper.tsx` -- Expanded branch replaced with `<StageGraphView />`. Thin/compact intact.
- [x] `web/src/components/shared/StageStepper.test.tsx` -- Migrate / prune expanded-variant tests; keep the thin/compact two original tests verbatim.
- [x] `web/src/index.css` -- Remove previous expanded list/rail CSS, add graph-canvas CSS.

**Acceptance Criteria:**
- Given empty store, when Production loads, then thin stepper unchanged + chevron-down visible.
- Given a run on any stage, when the user toggles expanded, then a node-edge graph renders 15 RunStage nodes laid out left-to-right by dagre. Toggle persists across reload (existing contract).
- Given `stage=critic`, when expanded, then nodes [pending..review] = `completed`, `critic` = `active`, `scenario_review` and downstream = `upcoming`.
- Given `stage=image` (or `tts`), when expanded, then `image` AND `tts` both render as `active` parallel branches descending from `character_pick` and merging into `batch_review` = `upcoming`.
- Given `stage=batch_review` with `decisions_summary={8,2,22}`, when expanded, then `batch_review` node body shows "10/32 reviewed". With missing summary or all-zeros, no counter (regression preserved from previous spec).
- Given `stage=write`, `status=failed`, when expanded, then `write` = `failed`, predecessors completed, downstream upcoming.
- Given `prefers-reduced-motion: reduce`, when expanded renders, then no animation runs on edges or nodes.
- All existing thin/compact tests pass without source modification beyond list-style expanded tests being migrated; `npm run typecheck` and `npm run test:unit` green.

## Spec Change Log

- **2026-04-25 (supersedes spec-fanout-stepper expanded variant)**: Original spec's expanded mode rendered a list-style sub-rail; user feedback identified this as an intent_gap — the target was an n8n-style DAG canvas. New spec adopts `@xyflow/react` + `dagre` and revises the Assets parent from sequential `image → tts → batch_review` to parallel `[image, tts] → batch_review`, justified by `phase_b.go`'s errgroup parallelism (both tracks really do run concurrently within one PhaseBRunner.Run; only the persisted `run.stage` cycles through them sequentially). Honest data principle preserved: when `stage ∈ {image, tts}`, both parallel nodes render `active` because polled signal cannot distinguish. Thin/compact and the toggle persistence layer (already shipped) are unchanged.

## Design Notes

**Topology constants (hard-coded, engine-verified):**
- Linear chain: `pending → research → structure → write → visual_break → review → critic → scenario_review → character_pick`.
- Parallel fork from `character_pick`: `[image, tts]`. Both edges enter `batch_review`.
- Linear tail: `batch_review → assemble → metadata_ack → complete`.

**State derivation per node:** Compute the canonical position of `run.stage` in a flattened "progress index" — for the parallel pair, `image` and `tts` share the same progress level. State is `completed` if node's index < current; `active` if equal; `upcoming` if greater; `failed` if `(status='failed'|'cancelled')` and node is current (or both image/tts when current is one of them).

**Edge state:** Edge between `(u, v)` is `completed` if both endpoints are completed; `active` if either endpoint is active and the other is completed (the entering edge); `upcoming` otherwise. Failed edges follow the failed node.

**Why parallel for image/tts after spec-fanout-stepper said sequential:** That earlier decision was conservative — strictly mirror polled stage. Trade-off was a flat one-line graph that adds zero value over a stepper. With a graph canvas, the topology IS the value proposition; aligning topology with `phase_b.go`'s real concurrency is now the more honest call. We still don't fabricate per-track counters.

**Reduced-motion:** Pass `prefers-reduced-motion` boolean from `window.matchMedia` into `StageGraphView`; component sets `animated={false}` on all xyflow edges and disables CSS pulse keyframes.

## Verification

**Commands (from `web/`):**
- `npm install` -- expected: dependencies resolved without warnings.
- `npm run typecheck` -- expected: no TS errors.
- `npm run test:unit` -- expected: all green (thin/compact preserved + new graph tests).

**Manual checks:**
- `npm run dev`, run on `stage=write`: thin stepper unchanged.
- Toggle to expanded: graph canvas shows 15 nodes laid out left-to-right with edges; `write` highlighted as active; predecessors green.
- Force `stage=image` or `tts`: both render as active parallel branches.
- Force `stage=batch_review` + decisions_summary: counter renders inside batch_review node.
- OS "reduce motion": no animated edges; static rendering.

## Suggested Review Order

**Topology — entry point**

- Single source of truth for the DAG: 15 stages, parallel image/tts fork, decisions counter only when stage=batch_review and total>0.
  [`formatters.ts:415`](../../web/src/lib/formatters.ts#L415)

**Layout**

- Pure dagre LR wrapper — deterministic coordinates from nodes+edges.
  [`dagLayout.ts:17`](../../web/src/lib/dagLayout.ts#L17)

**Render — canvas + custom node**

- ReactFlow canvas, non-interactive, animated edges only when active and no reduced-motion.
  [`StageGraphView.tsx:57`](../../web/src/components/shared/StageGraphView.tsx#L57)

- Custom node carries the per-node aria-label that drives test assertions and screen readers.
  [`StageGraphNode.tsx:10`](../../web/src/components/shared/StageGraphNode.tsx#L10)

**StageStepper integration**

- `expanded` variant short-circuits to `<StageGraphView />`; `full`/`compact` paths preserved bit-for-bit.
  [`StageStepper.tsx:36`](../../web/src/components/shared/StageStepper.tsx#L36)

**Tests (peripheral)**

- Topology shape, parallel invariant, edge state derivation.
  [`formatters.test.ts:135`](../../web/src/lib/formatters.test.ts#L135)

- Expanded variant aria-label assertions migrated from list DOM to graph DOM.
  [`StageStepper.test.tsx:27`](../../web/src/components/shared/StageStepper.test.tsx#L27)
