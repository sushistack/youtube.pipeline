-- Migration 003: NFR-O4 indexes for rolling-window observability queries.
--
-- Justification: Day-90 metrics, "failed runs in last N days", "cost by
-- status" are foundational to FR48 (pipeline metrics CLI report, Story 2.7)
-- and to ad-hoc operator diagnosis via the SQLite CLI (NFR-O3).
--
-- These indexes support queries like:
--   SELECT ... FROM runs WHERE created_at > ? ORDER BY created_at DESC
--   SELECT COUNT(*) FROM runs WHERE status = ? AND created_at > ?
--   SELECT stage, COUNT(*) FROM runs WHERE created_at > ? GROUP BY stage
--
-- Not indexed — and intentionally so:
--   * cost_usd: aggregated via SUM, never a WHERE predicate. Indexing a
--     frequently-updated REAL column adds write overhead without query
--     benefit.
--   * retry_reason / human_override: low-cardinality; the planner's SCAN
--     is fine for the <=100s of rows in V1.

CREATE INDEX IF NOT EXISTS idx_runs_created_at        ON runs(created_at);
CREATE INDEX IF NOT EXISTS idx_runs_status_created_at ON runs(status, created_at);
CREATE INDEX IF NOT EXISTS idx_runs_stage             ON runs(stage);
