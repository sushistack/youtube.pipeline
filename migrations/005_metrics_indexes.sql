-- Migration 005: NFR-O4 indexes for Day-90 metrics rolling-window queries.
--
-- Story 2.7's metrics CLI must query decisions by run window without a full
-- scan. Existing runs-table indexes from Migrations 003/004 already cover the
-- completed-run window lookup. What remains is the decisions-side filtering:
--
--   1. decisions(run_id, decision_type, superseded_by) supports the metrics
--      queries that scope to a run window, filter to approve/reject decisions,
--      and ignore superseded rows.
--   2. decisions(created_at) supports direct temporal seeks when a future
--      window query intersects decisions by timestamp.
--
-- We intentionally do NOT add a standalone decisions(scene_id) index. The V1
-- kappa and defect-escape queries always constrain scene_id inside a selective
-- run_id window, so the composite run_id/decision_type/superseded_by path is
-- sufficient without the extra write overhead of a free-floating scene index.

CREATE INDEX IF NOT EXISTS idx_decisions_run_id_type
    ON decisions(run_id, decision_type, superseded_by);

CREATE INDEX IF NOT EXISTS idx_decisions_created_at
    ON decisions(created_at);
