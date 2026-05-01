---
title: 'Stepper rename to Story/Cast/Media/Cut and trim to 4 work phases'
type: 'feature'
created: '2026-05-01'
status: 'done'
route: 'one-shot'
---

# Stepper rename to Story/Cast/Media/Cut and trim to 4 work phases

## Intent

**Problem:** The run-detail stepper showed 6 nodes (Pending → Scenario → Character → Assets → Assemble → Complete), conflating run lifecycle states (Pending, Complete) with work phases. Labels also leaned code-y / generic ("Scenario", "Assets") rather than reading as a film-vocabulary set.

**Approach:** Rename the four work-phase labels to a film vocabulary (Story / Cast / Media / Cut) and filter `pending` and `complete` out of the compact and full stepper variants only. The expanded graph view (`StageGraphView`) still renders all 6 swim-lanes since the full pipeline DAG legitimately includes queue and terminal states.

## Suggested Review Order

**Trim behavior (entry point)**

- Filter applied after `buildStageNodes` for compact/full variants — `expanded` returns earlier and is unaffected.
  [`StageStepper.tsx:55-57`](../../web/src/components/shared/StageStepper.tsx#L55-L57)

**Label rename**

- Single map edit: 4 renames, `pending`/`complete` keys retained for the expanded DAG.
  [`formatters.ts:119-126`](../../web/src/lib/formatters.ts#L119-L126)

**Layout adjustments**

- Main grid moves from 6 → 4 columns to match the trimmed node count.
  [`index.css:1131`](../../web/src/index.css#L1131)

- Sub-1280px responsive grid moves from 3 → 2 columns; connector-hide selector adjusts to `:nth-child(2n)` for the 2-row layout.
  [`index.css:2282-2288`](../../web/src/index.css#L2282-L2288)

**Test contract**

- Component test updated to assert the new 4-node contract (length, new labels, Pending/Complete absent).
  [`StageStepper.test.tsx:7-25`](../../web/src/components/shared/StageStepper.test.tsx#L7-L25)
