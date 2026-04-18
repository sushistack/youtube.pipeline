-- Fixture for TestRunHandler_Status_JSONSchemaStable (Story 2.6, FR49 canonical).
-- Demonstrates the spec's byte-exact example: "reviewing scene 5 of 10, 4 approved, 0 rejected".
-- Scenes 0-9; scenes 0-3 approved at T1; scene_index=4 (next pending).
-- Snapshot matches live state exactly, so FR50 diff is empty.

INSERT INTO runs (id, scp_id, stage, status, retry_count, cost_usd, token_in, token_out, duration_ms, human_override, created_at, updated_at)
VALUES ('scp-049-run-golden', '049', 'batch_review', 'waiting', 0, 2.50, 30000, 6000, 90000, 0, '2026-02-01T00:00:00Z', '2026-02-01T01:00:00Z');

INSERT INTO segments (run_id, scene_index, narration, shot_count, status)
VALUES ('scp-049-run-golden', 0, 'Scene 0', 1, 'completed'),
       ('scp-049-run-golden', 1, 'Scene 1', 1, 'completed'),
       ('scp-049-run-golden', 2, 'Scene 2', 1, 'completed'),
       ('scp-049-run-golden', 3, 'Scene 3', 1, 'completed'),
       ('scp-049-run-golden', 4, 'Scene 4', 1, 'pending'),
       ('scp-049-run-golden', 5, 'Scene 5', 1, 'pending'),
       ('scp-049-run-golden', 6, 'Scene 6', 1, 'pending'),
       ('scp-049-run-golden', 7, 'Scene 7', 1, 'pending'),
       ('scp-049-run-golden', 8, 'Scene 8', 1, 'pending'),
       ('scp-049-run-golden', 9, 'Scene 9', 1, 'pending');

INSERT INTO decisions (run_id, scene_id, decision_type, created_at)
VALUES ('scp-049-run-golden', '0', 'approve', '2026-02-01T00:10:00Z'),
       ('scp-049-run-golden', '1', 'approve', '2026-02-01T00:20:00Z'),
       ('scp-049-run-golden', '2', 'approve', '2026-02-01T00:30:00Z'),
       ('scp-049-run-golden', '3', 'approve', '2026-02-01T00:40:00Z');

-- HITL session: snapshot matches live state at T1.
-- scene_index=4 (next pending after scenes 0-3 approved).
INSERT INTO hitl_sessions (run_id, stage, scene_index, last_interaction_timestamp, snapshot_json, created_at, updated_at)
VALUES (
  'scp-049-run-golden',
  'batch_review',
  4,
  '2026-02-01T00:40:00Z',
  '{"total_scenes":10,"approved_count":4,"rejected_count":0,"pending_count":6,"scene_statuses":{"0":"approved","1":"approved","2":"approved","3":"approved","4":"waiting_for_review","5":"waiting_for_review","6":"waiting_for_review","7":"waiting_for_review","8":"waiting_for_review","9":"waiting_for_review"}}',
  '2026-02-01T00:00:00Z',
  '2026-02-01T00:40:00Z'
);
