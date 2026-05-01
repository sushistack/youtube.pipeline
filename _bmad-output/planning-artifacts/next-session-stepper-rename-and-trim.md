# Next session — Stepper rename + trim to 4 work stages

Run with `/bmad-quick-dev` (or any dev agent). This document is self-contained — it includes
all the context needed without exploring the repo.

---

## Goal

Rename the run-detail stepper labels and reduce it from **6 nodes** to **4 work-phase nodes**.
Pending and Complete are run-lifecycle *status*, not work phases — they should disappear
from the stepper but remain in the expanded graph view (which legitimately shows the full
pipeline including queue/terminal states).

**Final stepper:**

```
Story  →  Cast  →  Media  →  Cut
```

Mapping:

| Old (StageNodeKey) | Old label | New label |
|--------------------|-----------|-----------|
| `pending`          | Pending   | *(hidden in stepper, kept in expanded graph)* |
| `scenario`         | Scenario  | **Story** |
| `character`        | Character | **Cast**  |
| `assets`           | Assets    | **Media** |
| `assemble`         | Assemble  | **Cut**   |
| `complete`         | Complete  | *(hidden in stepper, kept in expanded graph)* |

Rationale agreed with the user (Jay):
- "Scenario" / "Script" sounds code-y; **Story** is neutral and matches research+writing+shot-planning.
- "Character" felt heavy; **Cast** (theatrical/film) is lighter and more familiar in Korean ("캐스팅").
- "Assets" is a generic noun; **Media** clearly captures image+TTS audio without the GPU/3D nuance of "Render".
- "Assemble" was OK but **Cut** (film term) is shorter and consistent with Story/Cast/Media as a film-vocabulary set.
- Pending and Complete are statuses (queue/terminal). Showing them as stepper steps conflates *progress* with *lifecycle state* — the run header / status badge already conveys "queued" or "completed".

---

## Constraints — what NOT to touch

These keep the expanded graph view (StageGraphView) and tests passing:

- `NODE_ORDER` in [web/src/lib/formatters.ts](web/src/lib/formatters.ts#L128) — leave the array of 6 keys as-is. Filtering happens in the component.
- `LANE_ORDER` in [web/src/lib/formatters.ts](web/src/lib/formatters.ts#L392) — used by `buildStageDagTopology` for the 6-lane DAG. Leave alone.
- `LANE_NODES` in [web/src/lib/formatters.ts](web/src/lib/formatters.ts#L401) — sub-stage groupings. Leave alone.
- `STAGE_LABELS` (the *leaf*-stage label map for `research`, `structure`, `write`, etc.) — leave alone.
- `STAGE_TO_NODE` — leave alone.
- `buildStageNodes()` function body — leave alone (still returns 6 entries; the component filters).
- `StageGraphView` component — the *expanded* variant should keep showing all 6 swim-lanes (Pending and Complete are legitimate parts of the full pipeline DAG).
- The recently-redesigned stepper *visual* (ripple animation, green→blue gradient connector, 1.5rem circles, label-below-only) — keep all of it. We're only renaming and reducing node count.

---

## Files to change (3)

### 1. `web/src/lib/formatters.ts`

Update `STAGE_NODE_LABELS` only — change 4 labels, keep 2:

```ts
const STAGE_NODE_LABELS: Record<StageNodeKey, string> = {
  assemble: 'Cut',       // was 'Assemble'
  assets: 'Media',       // was 'Assets'
  character: 'Cast',     // was 'Character'
  complete: 'Complete',  // unchanged (still appears in expanded graph)
  pending: 'Pending',    // unchanged (still appears in expanded graph)
  scenario: 'Story',     // was 'Scenario'
}
```

That's the only edit in this file.

### 2. `web/src/components/shared/StageStepper.tsx`

Filter out `pending` and `complete` for the stepper (`compact` and `full` variants).
The `expanded` variant returns early via `<StageGraphView />` and is unaffected.

Find this line (currently around line 55):

```ts
const nodes = buildStageNodes(stage, status)
```

Replace with:

```ts
const nodes = buildStageNodes(stage, status).filter(
  (node) => node.key !== 'pending' && node.key !== 'complete',
)
```

No other changes in this file.

### 3. `web/src/index.css`

Update grid columns from 6 to 4 in two places:

**a) Main rule (currently around line 1129):**

```css
.stage-stepper {
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));  /* → repeat(4, minmax(0, 1fr)) */
  gap: 0;
  padding: 0.25rem 0 0.1rem;
  margin: 0;
  list-style: none;
}
```

**b) Responsive rule under `@media (width < 1280px)` (currently around line 2125):**

```css
.stage-stepper {
  grid-template-columns: repeat(3, minmax(0, 1fr));  /* → repeat(2, minmax(0, 1fr)) */
  row-gap: 1rem;
}

/* In 3-col layout, hide connector on the last item of each row */
.stage-stepper[data-variant="full"] .stage-stepper__node:nth-child(3n)::after {
  display: none;          /* → :nth-child(2n) */
}
```

For the < 1280px breakpoint with 4 nodes, 2 columns × 2 rows is sensible. Adjust the
`:nth-child(3n)` connector-hide selector to `:nth-child(2n)` (every 2nd item is the row end).

---

## Behavior after the change

- **`stage=pending`** → all 4 nodes shown as `upcoming` (greyed circles with step numbers).
  The user reads this as "queued, not started yet". Pending status is still surfaced
  via the existing run-status UI (Cancel run button, etc.).
- **`stage=research/structure/write/...`** → `Story` is `active`, others appropriate.
- **`stage=character_pick`** → `Cast` is `active`, `Story` is `completed`, others `upcoming`.
- **`stage=image/tts/batch_review`** → `Media` is `active`.
- **`stage=assemble/metadata_ack`** → `Cut` is `active`.
- **`stage=complete`** → all 4 shown as `completed` (4 green checks). Visually conveys "done".
- **`status=failed` or `status=cancelled`** → the active node turns `failed` (red X).
  Already handled by `buildStageNodes`.

The aria-label on the `<ol>` (`Pipeline progress: ${getStageNodeLabel(active_node)}`) will
still say "Pending" or "Complete" when those are the active node — this is fine for screen
readers because the label still conveys lifecycle position even though it's not visually
rendered as a step.

---

## Verification

Run from `/mnt/work/projects/youtube.pipeline/web`:

1. **Typecheck**: `npx tsc --noEmit` — should pass with no errors.
2. **Unit tests**: `npm test -- formatters` (or `npx vitest run src/lib/formatters.test.ts`).
   - Expected: all existing tests pass without modification.
   - The `buildStageNodes('batch_review', 'failed')` test (around line 23) still expects
     6 entries because `buildStageNodes` itself didn't change — only the component filters.
   - The `buildStageDagTopology` tests still expect 6 lanes (pending..complete) because
     the expanded graph keeps the full topology.
3. **Visual smoke**: open the run-detail page in the browser
   ([dev server at `http://localhost:5173/`](http://localhost:5173/) — Vite + HMR).
   - Stepper shows exactly 4 circles with labels: `Story / Cast / Media / Cut`.
   - For an in-flight run, one circle is the blue ripple animation + label highlighted blue.
   - Completed circles to its left have green check marks; future ones are grey with numbers.
   - The chevron-down expand button still works and reveals the full 6-lane graph view
     (which keeps `Pending` and `Complete` lanes — that's intentional).
4. **No regression in expanded graph**: click the chevron — the DAG view should still
   render all 6 swim-lanes with the new labels (Story/Cast/Media/Cut for the work lanes,
   Pending/Complete still labeled the same).

---

## Pre-existing context (in case the agent wants to inspect)

- The stepper was just redesigned in the previous session with circle indicators (1.5rem),
  ripple-ring animation on active, green→blue gradient connectors, and labels below the
  circles only (no "STEP N" caption, no status text). All that styling stays.
- The `production-app-header` is a 3-column grid (identity | stepper | actions). It does not
  need layout changes — 4 narrower columns inside the stepper just makes each step roomier.
- Git status before this work should be clean (or only contain unrelated work). This change
  is small (~15 lines diff across 3 files) and self-contained.

---

## Out of scope (do not do these in this session)

- Do not remove `pending` / `complete` from `NODE_ORDER`, `LANE_ORDER`, `STAGE_TO_NODE`,
  `STAGE_NODE_LABELS`, or `LANE_NODES`. They're load-bearing for the DAG view and tests.
- Do not redesign the stepper visuals (animation, colors, shape).
- Do not touch the leaf-stage `STAGE_LABELS` map (research, structure, write, etc. labels).
- Do not add a new "Status badge" UI for Pending/Complete — the existing run-detail header
  already conveys that information through the Cancel-run button presence and other UI;
  if the user later wants a more explicit badge, that's a separate task.

---

## Commit

A single commit is fine. Suggested message:

```
ui(stepper): rename to Story/Cast/Media/Cut and trim to 4 work phases

Pending and Complete are run lifecycle states, not work phases — hide them
from the run-detail stepper. The expanded graph view still shows all 6
swim-lanes since the full pipeline DAG includes queue and terminal states.
```
