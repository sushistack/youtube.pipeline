-- Migration 004: composite index for the NFR-R2 rolling-window query.
--
-- Justification: Story 2.5's AC-RUNSTORE-ANTI-PROGRESS-WINDOW promises that
--   SELECT human_override FROM runs
--     WHERE retry_reason = 'anti_progress'
--     ORDER BY created_at DESC
--     LIMIT ?
-- uses an index to avoid a full table scan.
--
-- Migration 003's single-column idx_runs_created_at DOES sort the index in
-- date order, but SQLite's planner prefers a full-table scan + in-memory
-- B-tree sort when the WHERE filter (retry_reason = ?) is more selective
-- than the sort and no index covers both columns. Verified empirically:
-- EXPLAIN QUERY PLAN showed `SCAN runs` + `USE TEMP B-TREE FOR ORDER BY`.
--
-- A composite index on (retry_reason, created_at DESC) lets SQLite:
--   1. Jump directly to retry_reason = 'anti_progress' in the index.
--   2. Walk the index backwards to satisfy ORDER BY created_at DESC.
--   3. Terminate early on LIMIT N.
-- No temp B-tree, no full scan, O(log total + N) instead of O(total).
--
-- Narrow-purpose index (retry_reason has ≤5 distinct values in V1):
-- small physical size, write overhead only on the UPDATE that sets
-- retry_reason (rare event: one per stage retry).

CREATE INDEX IF NOT EXISTS idx_runs_retry_reason_created_at
    ON runs(retry_reason, created_at DESC);
