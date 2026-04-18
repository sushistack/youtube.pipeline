-- Fixture for TestIntegration_BuildStatus_PausedWithChanges (Story 2.6, FR50).
-- Represents a paused run where a background event completed scene 2 AFTER
-- T1. The snapshot (captured at T1 = 2026-01-02T00:25:00Z) still shows
-- scene 2 as "waiting_for_review", but the live decisions table now has an approve
-- for scene 2 at T2 = 2026-01-02T00:45:00Z (AFTER T1).
--
-- Expected FR50 diff: one scene_status_flipped entry for scene_id="2",
-- before="waiting_for_review", after="approved", timestamp="2026-01-02T00:45:00Z".

INSERT INTO runs (id, scp_id, stage, status, retry_count, cost_usd, token_in, token_out, duration_ms, human_override, created_at, updated_at)
VALUES ('scp-049-run-2', '049', 'batch_review', 'waiting', 0, 1.50, 18000, 3500, 50000, 0, '2026-01-02T00:00:00Z', '2026-01-02T01:00:00Z');

INSERT INTO segments (run_id, scene_index, narration, shot_count, status)
VALUES ('scp-049-run-2', 0, 'Scene 0', 1, 'completed'),
       ('scp-049-run-2', 1, 'Scene 1', 1, 'completed'),
       ('scp-049-run-2', 2, 'Scene 2', 1, 'completed');

INSERT INTO decisions (run_id, scene_id, decision_type, created_at)
VALUES ('scp-049-run-2', '0', 'approve', '2026-01-02T00:15:00Z'),
       ('scp-049-run-2', '1', 'approve', '2026-01-02T00:25:00Z'),
       ('scp-049-run-2', '2', 'approve', '2026-01-02T00:45:00Z');

-- Snapshot captured at T1 = 2026-01-02T00:25:00Z, BEFORE scene 2 approval.
INSERT INTO hitl_sessions (run_id, stage, scene_index, last_interaction_timestamp, snapshot_json, created_at, updated_at)
VALUES (
  'scp-049-run-2',
  'batch_review',
  2,
  '2026-01-02T00:25:00Z',
  '{"total_scenes":3,"approved_count":2,"rejected_count":0,"pending_count":1,"scene_statuses":{"0":"approved","1":"approved","2":"waiting_for_review"}}',
  '2026-01-02T00:00:00Z',
  '2026-01-02T00:25:00Z'
);
