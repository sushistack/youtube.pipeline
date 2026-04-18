-- Fixture for TestLoadRunStateFixture_QueryRun (Story 1.4) and
-- TestIntegration_BuildStatus_PausedNoChanges (Story 2.6).
-- Represents a run paused at batch_review after 2 scenes were approved.
-- The hitl_sessions row's snapshot_json matches the live decisions state
-- exactly, so the FR50 diff is empty (no changes since T1).

INSERT INTO runs (id, scp_id, stage, status, retry_count, cost_usd, token_in, token_out, duration_ms, human_override, created_at, updated_at)
VALUES ('scp-049-run-1', '049', 'batch_review', 'waiting', 0, 1.25, 15000, 3000, 45000, 0, '2026-01-01T00:00:00Z', '2026-01-01T00:30:00Z');

INSERT INTO segments (run_id, scene_index, narration, shot_count, status)
VALUES ('scp-049-run-1', 0, 'SCP-049 접근 장면', 2, 'completed'),
       ('scp-049-run-1', 1, 'SCP-049 실험 기록', 1, 'completed'),
       ('scp-049-run-1', 2, 'SCP-049 격리 절차', 1, 'pending');

INSERT INTO decisions (run_id, scene_id, decision_type, created_at)
VALUES ('scp-049-run-1', '0', 'approve', '2026-01-01T00:20:00Z'),
       ('scp-049-run-1', '1', 'approve', '2026-01-01T00:25:00Z');

-- Story 2.6: HITL session snapshot.
-- Snapshot reflects state AT T1 (last interaction = 2026-01-01T00:25:00Z).
-- scene_index = 2 (next pending after scenes 0, 1 approved).
INSERT INTO hitl_sessions (run_id, stage, scene_index, last_interaction_timestamp, snapshot_json, created_at, updated_at)
VALUES (
  'scp-049-run-1',
  'batch_review',
  2,
  '2026-01-01T00:25:00Z',
  '{"total_scenes":3,"approved_count":2,"rejected_count":0,"pending_count":1,"scene_statuses":{"0":"approved","1":"approved","2":"waiting_for_review"}}',
  '2026-01-01T00:00:00Z',
  '2026-01-01T00:25:00Z'
);
