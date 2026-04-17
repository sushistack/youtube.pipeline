INSERT INTO runs (id, scp_id, stage, status, retry_count, cost_usd, token_in, token_out, duration_ms, human_override, created_at, updated_at)
VALUES ('scp-049-run-1', '049', 'batch_review', 'waiting', 0, 1.25, 15000, 3000, 45000, 0, '2026-01-01T00:00:00Z', '2026-01-01T00:30:00Z');

INSERT INTO segments (run_id, scene_index, narration, shot_count, status)
VALUES ('scp-049-run-1', 0, 'SCP-049 접근 장면', 2, 'completed'),
       ('scp-049-run-1', 1, 'SCP-049 실험 기록', 1, 'completed'),
       ('scp-049-run-1', 2, 'SCP-049 격리 절차', 1, 'pending');

INSERT INTO decisions (run_id, scene_id, decision_type, created_at)
VALUES ('scp-049-run-1', '0', 'approve', '2026-01-01T00:20:00Z'),
       ('scp-049-run-1', '1', 'approve', '2026-01-01T00:25:00Z');
