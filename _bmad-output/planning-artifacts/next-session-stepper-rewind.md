# Next session — Stepper rewind: click completed step → confirm → rollback

Run with `/bmad-quick-plan` **first** (this is non-trivial — backend rewind primitive does
not exist yet, and the user explicitly asked for state-machine rigor). Then `/bmad-quick-dev`
once the plan is approved.

This document is self-contained — it captures the user decisions and a territory map of
the relevant code, but does **not** prescribe the design. The plan step owns that.

---

## Goal

In the run-detail stepper (Story / Cast / Media / Cut), make **completed** nodes clickable.
Clicking a completed node opens a confirmation modal ("이 스텝으로 되돌리시겠습니까?").
On confirm, the run is rewound to that stage:

1. If the run is mid-flight (`running` / `waiting`), cancel/stop the in-flight work first.
2. Reset the run's `stage` and `status` so it re-enters the chosen work-phase from its
   start (e.g. rewind-to-Story = back to `research`; rewind-to-Cast = back to `character_pick`;
   rewind-to-Media = back to `image`; rewind-to-Cut = back to `assemble`).
3. Delete **all** data produced after the target stage:
   - DB rows (segments, decisions, hitl_sessions, batch artifacts, etc.)
   - On-disk artifacts (per-run scenario.json, images/, tts/, clips/, output.mp4, metadata.json, manifest.json)
   - Any logs / instrumentation tied to the discarded stages
4. The user can then resume / advance from the rewound stage as if it had never run further.

---

## User-confirmed decisions (do not re-litigate)

These were agreed before the planning artifact was written:

1. **Click target:** `completed` nodes only. The currently-active node is **not** clickable
   (no "rerun current stage" affordance). Upcoming nodes are not clickable either.
2. **Mid-run rollback:** Allowed. If the run is `running` or `waiting`, the rewind path must
   cancel/abort in-flight work before performing the rewind. Equivalent to a Cancel followed
   by a stage reset and cleanup, but as a single atomic-feeling user action.
3. **Backend support:** Does **not** exist yet. Build it carefully. Jay's standing instruction
   is that pipeline / state-machine work must be rigorous and modeled on verified external
   patterns — no shortcuts, no "we'll fix it later" branches, no half-implementations.
4. **Data deletion scope:** Everything. DB rows, on-disk artifacts, decisions, HITL sessions,
   logs — anything produced *after* the target stage starts.

---

## Stepper → backend stage mapping (already in code)

The stepper has 4 work-phase nodes; each maps to a group of backend `Stage` values
(see [`internal/domain/types.go`](internal/domain/types.go) lines 4–22 and
[`web/src/lib/formatters.ts`](web/src/lib/formatters.ts) `STAGE_TO_NODE`).

| Stepper node | StageNodeKey | Backend stages it covers                                              | Rewind target = re-enter at |
|--------------|--------------|----------------------------------------------------------------------|-----------------------------|
| Story        | `scenario`   | `research`, `structure`, `write`, `visual_break`, `review`, `critic`, `scenario_review` | `research`         |
| Cast         | `character`  | `character_pick`                                                     | `character_pick`            |
| Media        | `assets`     | `image`, `tts`, `batch_review`                                       | `image`                     |
| Cut          | `assemble`   | `assemble`, `metadata_ack`                                           | `assemble`                  |

Re-entry stage choice is the planner's call (the table above is one reasonable default).
The `complete` node is hidden from the stepper after the previous trim — `complete` runs
that the user wants to "redo" should rewind via the *Cut* node (or a different affordance —
out of scope for this task).

---

## Territory map (from a fresh exploration, with line numbers)

### State machine

- **Engine:** [`internal/pipeline/engine.go`](internal/pipeline/engine.go) lines 9–90.
  `NextStage(current, event)` is a pure function with no DB side effects. Forward-only
  transitions; the only "backward" branch today is `EventRetry` bouncing `critic → write`
  on critic failure.
- **Stage / Status enums:** [`internal/domain/types.go`](internal/domain/types.go) lines
  4–22 (Stage) and 78–88 (Status: pending, running, waiting, completed, failed, cancelled).
- **HITL gates:** `IsHITLStage()` (line 93 in same file) — `scenario_review`, `character_pick`,
  `batch_review`, `metadata_ack` are blocking checkpoints.
- **No reverse primitive exists** — the planner needs to define one. Likely shape:
  a `PriorStage(target StageNode) Stage` helper (or a typed enum mapping per stepper node)
  plus a `RewindOrchestration` service that owns the cancel + cleanup + state-reset flow.

### Cancel path (closest existing primitive)

- **API route:** `POST /api/runs/{id}/cancel` registered in
  [`internal/api/routes.go`](internal/api/routes.go) line 38.
- **HTTP handler:** `RunHandler.Cancel` in
  [`internal/api/handler_run.go`](internal/api/handler_run.go) lines 208–222.
- **Service:** `RunService.Cancel` in
  [`internal/service/run_service.go`](internal/service/run_service.go) lines 172–177.
- **Store:** `RunStore.Cancel` in
  [`internal/db/run_store.go`](internal/db/run_store.go) lines 737–766.
  Currently does **only** `UPDATE runs SET status='cancelled' WHERE status IN ('running','waiting')`.
  Returns 409 (`ErrConflict`) for terminal states. **Does NOT clean any artifacts.** The rewind
  orchestration cannot just call Cancel and stop there.

### Resume cleanup (a useful pattern to learn from)

- **Resume:** [`internal/pipeline/resume.go`](internal/pipeline/resume.go) line 482.
  When resuming a failed run it already clears downstream DB rows + disk artifacts so the
  retried stage doesn't see stale outputs. The rewind code is the *inverse direction* — keep
  everything up to and including the target stage's prerequisites, delete everything after.
  The same primitives (`CleanStageArtifacts`, store-level clear helpers) are reusable.
- **Per-stage artifact cleaner:** `CleanStageArtifacts()` in
  [`internal/pipeline/artifact.go`](internal/pipeline/artifact.go) lines 23–42 — idempotent,
  no-op for HITL/Phase A.
- **Bulk archive helper:** `ArchiveRunArtifacts()` in same file lines 76–130 — deletes
  scenario.json, images/, tts/, clips/, output.mp4, metadata.json, manifest.json.
  Already knows the full artifact layout. Likely the cleanest model for "delete everything
  produced after stage X" — split this into stage-bucketed helpers if needed.

### DB schema (per-run-per-stage data)

Migrations under [`migrations/`](migrations/). Tables to consider for cascade deletion:

- **`runs`** — primary row. Columns the rewind must reset: `stage`, `status`, `retry_count`,
  `scenario_path`, `output_path`, `selected_character_id`, `frozen_descriptor`,
  `critic_prompt_version`. `RunStore.ClearRunArtifactPaths()` in
  [`internal/db/run_store.go`](internal/db/run_store.go) line 596 already handles the path-null part.
- **`segments`** — per-scene rows ([`internal/db/segment_store.go`](internal/db/segment_store.go)).
  Existing helpers:
  - `DeleteByRunID()` line 95 — full delete.
  - `ClearImageArtifactsByRunID()` line 116, `ClearTTSArtifactsByRunID()` line 191,
    `ClearClipPathsByRunID()` line 55 — null path columns only.
- **`decisions`** — append-only audit log
  ([`internal/db/decision_store.go`](internal/db/decision_store.go)). FK on `run_id` is **not**
  ON DELETE CASCADE — manual cleanup needed. Has `superseded_by` self-FK (handle order).
- **`hitl_sessions`** — invariant: row exists iff `status='waiting'` AND `stage ∈ HITL`.
  `DeleteSession()` in decision_store.go line 306. Rewind must clear any active session.
- **`run_settings_assignments`** — has `ON DELETE CASCADE` (Migration 012). Rewind probably
  should NOT delete these (settings are run-level config, not stage output) — but planner to confirm.

### Frontend

- **Page shell:** [`web/src/components/shells/ProductionShell.tsx`](web/src/components/shells/ProductionShell.tsx)
  line 70. Reads `?run=<id>` (line 80), subscribes via `useRunStatus()` SSE hook (line 16),
  hosts the StageStepper.
- **Stepper component:** [`web/src/components/shared/StageStepper.tsx`](web/src/components/shared/StageStepper.tsx)
  lines 37–117. Currently renders 4 `<li>` nodes; no click handler. Each node already has
  `data-state="completed|active|upcoming|failed"` — easy to wire a click handler conditionally.
- **API client:** [`web/src/lib/apiClient.ts`](web/src/lib/apiClient.ts).
  `cancelRun()` lines 158–169 — model the new `rewindRun(id, target_stage)` mutation on this.
- **Confirmation modal:** check existing patterns. There's already a Cancel-run flow that
  may use a modal/confirm; reuse that pattern (look near where `cancelRun` is invoked in
  ProductionShell or its child components).

### API surface

Route registration in [`internal/api/routes.go`](internal/api/routes.go) lines 26–92.
Existing run lifecycle endpoints (lines 32–40):

```
POST   /api/runs                              create
GET    /api/runs                              list
GET    /api/runs/{id}                         get
GET    /api/runs/{id}/status                  status + HITL + decisions summary
GET    /api/runs/{id}/status/stream           SSE
POST   /api/runs/{id}/cancel                  cancel running/waiting
POST   /api/runs/{id}/resume                  resume failed/waiting
POST   /api/runs/{id}/advance                 kick pending → Phase A
POST   /api/runs/{id}/scenario/approve        HITL transition
POST   /api/runs/{id}/batch-review/approve    HITL transition
POST   /api/runs/{id}/metadata/ack            HITL transition
POST   /api/runs/{id}/characters/pick         HITL transition
```

**New endpoint to design:** `POST /api/runs/{id}/rewind` with body `{target_stage_node: "scenario" | "character" | "assets" | "assemble"}` (use the existing `StageNodeKey` vocabulary, not raw backend Stage values — keeps the API stable if backend stages get renamed).

---

## Architectural risks the planner must address explicitly

These are the parts that "꼼꼼하게" applies to. The plan step should call each of them out
with a concrete approach:

1. **Race with mid-flight worker.** A worker is mid-stage when the rewind request lands.
   Marking the row `cancelled` is not enough — the worker may still write outputs after the
   cancel is observed. Need a clear protocol: how does the worker check for cancellation,
   how does the rewind wait for the worker to acknowledge before deleting artifacts?
   (Look at how Cancel is currently consumed by the worker loop — probably under
   [`internal/pipeline/`](internal/pipeline/) — to design the rewind variant.)
2. **Atomicity.** DB updates + filesystem deletes cannot share a transaction. Define the
   ordering that leaves the system in a recoverable state if the process crashes mid-rewind.
   (Likely: mark `status='cancelled'` first, then DB cleanup in a tx, then disk cleanup last
   with idempotent helpers — but planner to confirm.)
3. **Decision-log handling.** Decisions are an audit log. "Delete all decisions after stage X"
   needs a precise definition — by `created_at`, by `decision_type` mapped to stage, or by
   tracking a per-decision stage tag? Confirm with the existing schema.
4. **HITL session cleanup.** If the user rewinds while `status='waiting'` at e.g. `batch_review`,
   the HITL session for that stage must be torn down before the run-row is reset, or the
   `hitl_sessions` invariant breaks.
5. **Idempotency.** A rewind request that is retried (network failure, double-click) must not
   corrupt state. The endpoint should be safe to call twice with the same target.
6. **Authorization / safety.** This is destructive. The frontend modal is one safeguard; the
   API should also require an explicit confirm token or only accept the request when the run
   is in a state where rewind makes sense. (Explicit "are you sure" UX is in scope; rate-limit
   is probably not.)
7. **Test strategy.** Per Jay's TDD-centered preference: a state-machine test (rewind from each
   work-node from each starting state — running, waiting, failed, completed) is the spine.
   Add integration-level tests that exercise DB + disk cleanup against a temp dir.

---

## Out of scope for this feature

- Click on `active` or `upcoming` nodes (not clickable).
- "Restart from beginning" (rewind to Story already covers it).
- Restoring previously-deleted data (rewind is destructive — that's the user's intent).
- Undo of a rewind. Once data is deleted, it's gone.
- Multi-run batch rewind.
- Time-travel through individual decisions inside a stage (rewind is stage-granular).

---

## Suggested next-session entry

1. Run `/bmad-quick-plan` with this artifact's path. Let the planner produce a spec that
   addresses each architectural risk above before any code is written.
2. Confirm the plan with Jay (especially the cancel/race protocol and the decision-log
   strategy — both have multiple reasonable answers).
3. Then `/bmad-quick-dev` against the approved spec.

The change touches state-machine code, DB stores, filesystem helpers, an API handler, the
frontend client, and the StageStepper component — i.e. all the layers. Expect roughly a
day's work even with a clean plan; do not rush it.

---

## Pre-work assumed already in place

- Stepper renders Story / Cast / Media / Cut as 4 work-phase nodes — done in prior session
  (see [`_bmad-output/implementation-artifacts/spec-stepper-rename-and-trim.md`](_bmad-output/implementation-artifacts/spec-stepper-rename-and-trim.md)).
- Expanded DAG view also drops `pending`/`complete` lanes — done in same prior session.
- `data-state` attribute on stepper nodes — already present, ready for a click-handler.
