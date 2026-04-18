-- Fixture for Story 2.7 metrics tests. Distribution:
--   25 × status='completed', 18 × human_override=0, 7 × human_override=1
--                                                     → automation_rate = 18/25 = 0.72
--   18 completed runs have critic_score >= 0.70, 7 are below 0.70.
--   Run-level dominant operator decisions yield:
--     a=16, b=2, c=1, d=6 → kappa = 0.714828897338403
--   segments: 40 scenes with critic_score >= 0.70 (auto-passed)
--             2 of those scenes have a later reject decision
--                                                     → defect_escape_rate = 2/40 = 0.05
--   5 × status='failed' are decoys and excluded by the completed-run window.
--
-- Expected window=25 results:
--   Provisional: false
--   Automation rate: 0.72 (72.0%), fail (target >= 0.80)
--   Critic calibration (kappa): 0.714828897338403, pass
--   Critic regression detection: external input
--   Defect escape rate: 0.05 (5.0%), pass (target <= 0.05)
--   Resume idempotency: external input

INSERT INTO runs (id, scp_id, stage, status, human_override, critic_score, created_at, updated_at) VALUES
  ('scp-049-run-01', '049', 'complete', 'completed', 0, 0.92, '2026-04-15T00:25:00Z', '2026-04-15T00:25:00Z'),
  ('scp-049-run-02', '049', 'complete', 'completed', 0, 0.91, '2026-04-15T00:24:00Z', '2026-04-15T00:24:00Z'),
  ('scp-049-run-03', '049', 'complete', 'completed', 0, 0.90, '2026-04-15T00:23:00Z', '2026-04-15T00:23:00Z'),
  ('scp-049-run-04', '049', 'complete', 'completed', 0, 0.89, '2026-04-15T00:22:00Z', '2026-04-15T00:22:00Z'),
  ('scp-049-run-05', '049', 'complete', 'completed', 0, 0.88, '2026-04-15T00:21:00Z', '2026-04-15T00:21:00Z'),
  ('scp-049-run-06', '049', 'complete', 'completed', 0, 0.87, '2026-04-15T00:20:00Z', '2026-04-15T00:20:00Z'),
  ('scp-049-run-07', '049', 'complete', 'completed', 0, 0.86, '2026-04-15T00:19:00Z', '2026-04-15T00:19:00Z'),
  ('scp-049-run-08', '049', 'complete', 'completed', 0, 0.85, '2026-04-15T00:18:00Z', '2026-04-15T00:18:00Z'),
  ('scp-049-run-09', '049', 'complete', 'completed', 0, 0.84, '2026-04-15T00:17:00Z', '2026-04-15T00:17:00Z'),
  ('scp-049-run-10', '049', 'complete', 'completed', 0, 0.83, '2026-04-15T00:16:00Z', '2026-04-15T00:16:00Z'),
  ('scp-049-run-11', '049', 'complete', 'completed', 0, 0.82, '2026-04-15T00:15:00Z', '2026-04-15T00:15:00Z'),
  ('scp-049-run-12', '049', 'complete', 'completed', 0, 0.81, '2026-04-15T00:14:00Z', '2026-04-15T00:14:00Z'),
  ('scp-049-run-13', '049', 'complete', 'completed', 0, 0.80, '2026-04-15T00:13:00Z', '2026-04-15T00:13:00Z'),
  ('scp-049-run-14', '049', 'complete', 'completed', 0, 0.79, '2026-04-15T00:12:00Z', '2026-04-15T00:12:00Z'),
  ('scp-049-run-15', '049', 'complete', 'completed', 0, 0.78, '2026-04-15T00:11:00Z', '2026-04-15T00:11:00Z'),
  ('scp-049-run-16', '049', 'complete', 'completed', 0, 0.77, '2026-04-15T00:10:00Z', '2026-04-15T00:10:00Z'),
  ('scp-049-run-17', '049', 'complete', 'completed', 1, 0.76, '2026-04-15T00:09:00Z', '2026-04-15T00:09:00Z'),
  ('scp-049-run-18', '049', 'complete', 'completed', 1, 0.75, '2026-04-15T00:08:00Z', '2026-04-15T00:08:00Z'),
  ('scp-049-run-19', '049', 'complete', 'completed', 1, 0.65, '2026-04-15T00:07:00Z', '2026-04-15T00:07:00Z'),
  ('scp-049-run-20', '049', 'complete', 'completed', 1, 0.64, '2026-04-15T00:06:00Z', '2026-04-15T00:06:00Z'),
  ('scp-049-run-21', '049', 'complete', 'completed', 1, 0.63, '2026-04-15T00:05:00Z', '2026-04-15T00:05:00Z'),
  ('scp-049-run-22', '049', 'complete', 'completed', 1, 0.62, '2026-04-15T00:04:00Z', '2026-04-15T00:04:00Z'),
  ('scp-049-run-23', '049', 'complete', 'completed', 1, 0.61, '2026-04-15T00:03:00Z', '2026-04-15T00:03:00Z'),
  ('scp-049-run-24', '049', 'complete', 'completed', 0, 0.60, '2026-04-15T00:02:00Z', '2026-04-15T00:02:00Z'),
  ('scp-049-run-25', '049', 'complete', 'completed', 0, 0.59, '2026-04-15T00:01:00Z', '2026-04-15T00:01:00Z'),
  ('scp-049-run-26', '049', 'complete', 'failed',    0, 0.55, '2026-04-14T23:59:00Z', '2026-04-14T23:59:00Z'),
  ('scp-049-run-27', '049', 'complete', 'failed',    0, 0.54, '2026-04-14T23:58:00Z', '2026-04-14T23:58:00Z'),
  ('scp-049-run-28', '049', 'complete', 'failed',    0, 0.53, '2026-04-14T23:57:00Z', '2026-04-14T23:57:00Z'),
  ('scp-049-run-29', '049', 'complete', 'failed',    0, 0.52, '2026-04-14T23:56:00Z', '2026-04-14T23:56:00Z'),
  ('scp-049-run-30', '049', 'complete', 'failed',    0, 0.51, '2026-04-14T23:55:00Z', '2026-04-14T23:55:00Z');

INSERT INTO segments (run_id, scene_index, critic_score) VALUES
  ('scp-049-run-01', 0, 0.80), ('scp-049-run-01', 1, 0.80), ('scp-049-run-01', 2, 0.80), ('scp-049-run-01', 3, 0.80), ('scp-049-run-01', 4, 0.80), ('scp-049-run-01', 5, 0.80),
  ('scp-049-run-02', 0, 0.80), ('scp-049-run-02', 1, 0.80),
  ('scp-049-run-03', 0, 0.80), ('scp-049-run-03', 1, 0.80),
  ('scp-049-run-04', 0, 0.80), ('scp-049-run-04', 1, 0.80),
  ('scp-049-run-05', 0, 0.80), ('scp-049-run-05', 1, 0.80),
  ('scp-049-run-06', 0, 0.80), ('scp-049-run-06', 1, 0.80),
  ('scp-049-run-07', 0, 0.80), ('scp-049-run-07', 1, 0.80),
  ('scp-049-run-08', 0, 0.80), ('scp-049-run-08', 1, 0.80),
  ('scp-049-run-09', 0, 0.80), ('scp-049-run-09', 1, 0.80),
  ('scp-049-run-10', 0, 0.80), ('scp-049-run-10', 1, 0.80),
  ('scp-049-run-11', 0, 0.80), ('scp-049-run-11', 1, 0.80),
  ('scp-049-run-12', 0, 0.80), ('scp-049-run-12', 1, 0.80),
  ('scp-049-run-13', 0, 0.80), ('scp-049-run-13', 1, 0.80),
  ('scp-049-run-14', 0, 0.80), ('scp-049-run-14', 1, 0.80),
  ('scp-049-run-15', 0, 0.80), ('scp-049-run-15', 1, 0.80),
  ('scp-049-run-16', 0, 0.80), ('scp-049-run-16', 1, 0.80),
  ('scp-049-run-17', 0, 0.80), ('scp-049-run-17', 1, 0.80),
  ('scp-049-run-18', 0, 0.80), ('scp-049-run-18', 1, 0.80),
  ('scp-049-run-19', 0, 0.60),
  ('scp-049-run-20', 0, 0.60),
  ('scp-049-run-21', 0, 0.60),
  ('scp-049-run-22', 0, 0.60),
  ('scp-049-run-23', 0, 0.60),
  ('scp-049-run-24', 0, 0.60),
  ('scp-049-run-25', 0, 0.60);

INSERT INTO decisions (run_id, scene_id, decision_type, created_at) VALUES
  ('scp-049-run-01', '0', 'approve', '2026-04-15T00:25:10Z'),
  ('scp-049-run-01', '1', 'approve', '2026-04-15T00:25:20Z'),
  ('scp-049-run-02', '0', 'approve', '2026-04-15T00:24:10Z'),
  ('scp-049-run-02', '1', 'approve', '2026-04-15T00:24:20Z'),
  ('scp-049-run-03', '0', 'approve', '2026-04-15T00:23:10Z'),
  ('scp-049-run-03', '1', 'approve', '2026-04-15T00:23:20Z'),
  ('scp-049-run-04', '0', 'approve', '2026-04-15T00:22:10Z'),
  ('scp-049-run-04', '1', 'approve', '2026-04-15T00:22:20Z'),
  ('scp-049-run-05', '0', 'approve', '2026-04-15T00:21:10Z'),
  ('scp-049-run-05', '1', 'approve', '2026-04-15T00:21:20Z'),
  ('scp-049-run-06', '0', 'approve', '2026-04-15T00:20:10Z'),
  ('scp-049-run-06', '1', 'approve', '2026-04-15T00:20:20Z'),
  ('scp-049-run-07', '0', 'approve', '2026-04-15T00:19:10Z'),
  ('scp-049-run-07', '1', 'approve', '2026-04-15T00:19:20Z'),
  ('scp-049-run-08', '0', 'approve', '2026-04-15T00:18:10Z'),
  ('scp-049-run-08', '1', 'approve', '2026-04-15T00:18:20Z'),
  ('scp-049-run-09', '0', 'approve', '2026-04-15T00:17:10Z'),
  ('scp-049-run-09', '1', 'approve', '2026-04-15T00:17:20Z'),
  ('scp-049-run-10', '0', 'approve', '2026-04-15T00:16:10Z'),
  ('scp-049-run-10', '1', 'approve', '2026-04-15T00:16:20Z'),
  ('scp-049-run-11', '0', 'approve', '2026-04-15T00:15:10Z'),
  ('scp-049-run-11', '1', 'approve', '2026-04-15T00:15:20Z'),
  ('scp-049-run-12', '0', 'approve', '2026-04-15T00:14:10Z'),
  ('scp-049-run-12', '1', 'approve', '2026-04-15T00:14:20Z'),
  ('scp-049-run-13', '0', 'approve', '2026-04-15T00:13:10Z'),
  ('scp-049-run-13', '1', 'approve', '2026-04-15T00:13:20Z'),
  ('scp-049-run-14', '0', 'approve', '2026-04-15T00:12:10Z'),
  ('scp-049-run-14', '1', 'approve', '2026-04-15T00:12:20Z'),
  ('scp-049-run-15', '0', 'approve', '2026-04-15T00:11:10Z'),
  ('scp-049-run-15', '1', 'approve', '2026-04-15T00:11:20Z'),
  ('scp-049-run-16', '0', 'approve', '2026-04-15T00:10:10Z'),
  ('scp-049-run-16', '1', 'approve', '2026-04-15T00:10:20Z'),
  ('scp-049-run-17', '0', 'reject',  '2026-04-15T00:09:10Z'),
  ('scp-049-run-17', '1', 'approve', '2026-04-15T00:09:20Z'),
  ('scp-049-run-18', '0', 'reject',  '2026-04-15T00:08:10Z'),
  ('scp-049-run-18', '1', 'approve', '2026-04-15T00:08:20Z'),
  ('scp-049-run-19', '0', 'approve', '2026-04-15T00:07:10Z'),
  ('scp-049-run-20', '0', 'reject',  '2026-04-15T00:06:10Z'),
  ('scp-049-run-21', '0', 'reject',  '2026-04-15T00:05:10Z'),
  ('scp-049-run-22', '0', 'reject',  '2026-04-15T00:04:10Z'),
  ('scp-049-run-23', '0', 'reject',  '2026-04-15T00:03:10Z'),
  ('scp-049-run-24', '0', 'reject',  '2026-04-15T00:02:10Z'),
  ('scp-049-run-25', '0', 'reject',  '2026-04-15T00:01:10Z');
