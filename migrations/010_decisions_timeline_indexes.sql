-- Migration 010: cross-run decisions timeline index.
--
-- Story 8.6 introduces a global read-only history surface ordered by
-- created_at DESC, id DESC with an optional cross-run decision_type filter.
-- Migration 005's indexes cover:
--   1. decisions(created_at) for the default timeline ordering
--   2. decisions(run_id, decision_type, superseded_by) for run-scoped metrics
--
-- The optional V1 timeline filter is not run-scoped, so the existing
-- run_id-leading composite cannot satisfy WHERE decision_type = ? across all
-- runs without scanning. This narrow composite lets SQLite seek by
-- decision_type and preserve created_at ordering without adding speculative
-- indexes for scene_id/note (reason search is client-side over a bounded page).

CREATE INDEX IF NOT EXISTS idx_decisions_type_created_at
    ON decisions(decision_type, created_at, id);
