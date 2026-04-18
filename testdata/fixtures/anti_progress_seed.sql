-- Fixture for TestAntiProgressFalsePositiveStats_RollingWindow (Story 2.5, NFR-R2).
-- Distribution:
--   50 × retry_reason='anti_progress', human_override=0, created_at in [-1..-50 days]
--   10 × retry_reason='anti_progress', human_override=1, created_at in [-51..-60 days]
--   20 × retry_reason='rate_limit',   human_override=0, created_at in [-1..-20 days] (decoys)
--    5 × retry_reason=NULL,           human_override=0, created_at in [-1..-5 days]  (decoys)
-- Expected:
--   window=50  → {Total:50, OperatorOverride:0,  Provisional:false}
--   window=60  → {Total:60, OperatorOverride:10, Provisional:false}
--   window=100 → {Total:60, OperatorOverride:10, Provisional:true}
-- Deterministic: every created_at is a fixed offset; no random functions used.

-- 50 anti-progress runs without operator override, days -1..-50.
INSERT INTO runs (id, scp_id, stage, status, retry_count, retry_reason, cost_usd, human_override, created_at) VALUES
  ('ap-no-001', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-1 days')),
  ('ap-no-002', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-2 days')),
  ('ap-no-003', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-3 days')),
  ('ap-no-004', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-4 days')),
  ('ap-no-005', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-5 days')),
  ('ap-no-006', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-6 days')),
  ('ap-no-007', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-7 days')),
  ('ap-no-008', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-8 days')),
  ('ap-no-009', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-9 days')),
  ('ap-no-010', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-10 days')),
  ('ap-no-011', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-11 days')),
  ('ap-no-012', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-12 days')),
  ('ap-no-013', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-13 days')),
  ('ap-no-014', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-14 days')),
  ('ap-no-015', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-15 days')),
  ('ap-no-016', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-16 days')),
  ('ap-no-017', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-17 days')),
  ('ap-no-018', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-18 days')),
  ('ap-no-019', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-19 days')),
  ('ap-no-020', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-20 days')),
  ('ap-no-021', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-21 days')),
  ('ap-no-022', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-22 days')),
  ('ap-no-023', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-23 days')),
  ('ap-no-024', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-24 days')),
  ('ap-no-025', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-25 days')),
  ('ap-no-026', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-26 days')),
  ('ap-no-027', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-27 days')),
  ('ap-no-028', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-28 days')),
  ('ap-no-029', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-29 days')),
  ('ap-no-030', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-30 days')),
  ('ap-no-031', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-31 days')),
  ('ap-no-032', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-32 days')),
  ('ap-no-033', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-33 days')),
  ('ap-no-034', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-34 days')),
  ('ap-no-035', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-35 days')),
  ('ap-no-036', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-36 days')),
  ('ap-no-037', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-37 days')),
  ('ap-no-038', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-38 days')),
  ('ap-no-039', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-39 days')),
  ('ap-no-040', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-40 days')),
  ('ap-no-041', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-41 days')),
  ('ap-no-042', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-42 days')),
  ('ap-no-043', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-43 days')),
  ('ap-no-044', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-44 days')),
  ('ap-no-045', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-45 days')),
  ('ap-no-046', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-46 days')),
  ('ap-no-047', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-47 days')),
  ('ap-no-048', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-48 days')),
  ('ap-no-049', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-49 days')),
  ('ap-no-050', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 0, datetime('now', '-50 days'));

-- 10 anti-progress runs with operator override (FP candidates), days -51..-60.
INSERT INTO runs (id, scp_id, stage, status, retry_count, retry_reason, cost_usd, human_override, created_at) VALUES
  ('ap-yes-001', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 1, datetime('now', '-51 days')),
  ('ap-yes-002', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 1, datetime('now', '-52 days')),
  ('ap-yes-003', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 1, datetime('now', '-53 days')),
  ('ap-yes-004', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 1, datetime('now', '-54 days')),
  ('ap-yes-005', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 1, datetime('now', '-55 days')),
  ('ap-yes-006', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 1, datetime('now', '-56 days')),
  ('ap-yes-007', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 1, datetime('now', '-57 days')),
  ('ap-yes-008', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 1, datetime('now', '-58 days')),
  ('ap-yes-009', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 1, datetime('now', '-59 days')),
  ('ap-yes-010', '049', 'write', 'failed', 1, 'anti_progress', 0.10, 1, datetime('now', '-60 days'));

-- 20 rate-limit decoys — MUST be ignored by the query.
INSERT INTO runs (id, scp_id, stage, status, retry_count, retry_reason, cost_usd, human_override, created_at) VALUES
  ('rl-001', '049', 'write', 'failed', 1, 'rate_limit', 0.10, 0, datetime('now', '-1 days')),
  ('rl-002', '049', 'write', 'failed', 1, 'rate_limit', 0.10, 0, datetime('now', '-2 days')),
  ('rl-003', '049', 'write', 'failed', 1, 'rate_limit', 0.10, 0, datetime('now', '-3 days')),
  ('rl-004', '049', 'write', 'failed', 1, 'rate_limit', 0.10, 0, datetime('now', '-4 days')),
  ('rl-005', '049', 'write', 'failed', 1, 'rate_limit', 0.10, 0, datetime('now', '-5 days')),
  ('rl-006', '049', 'write', 'failed', 1, 'rate_limit', 0.10, 0, datetime('now', '-6 days')),
  ('rl-007', '049', 'write', 'failed', 1, 'rate_limit', 0.10, 0, datetime('now', '-7 days')),
  ('rl-008', '049', 'write', 'failed', 1, 'rate_limit', 0.10, 0, datetime('now', '-8 days')),
  ('rl-009', '049', 'write', 'failed', 1, 'rate_limit', 0.10, 0, datetime('now', '-9 days')),
  ('rl-010', '049', 'write', 'failed', 1, 'rate_limit', 0.10, 0, datetime('now', '-10 days')),
  ('rl-011', '049', 'write', 'failed', 1, 'rate_limit', 0.10, 0, datetime('now', '-11 days')),
  ('rl-012', '049', 'write', 'failed', 1, 'rate_limit', 0.10, 0, datetime('now', '-12 days')),
  ('rl-013', '049', 'write', 'failed', 1, 'rate_limit', 0.10, 0, datetime('now', '-13 days')),
  ('rl-014', '049', 'write', 'failed', 1, 'rate_limit', 0.10, 0, datetime('now', '-14 days')),
  ('rl-015', '049', 'write', 'failed', 1, 'rate_limit', 0.10, 0, datetime('now', '-15 days')),
  ('rl-016', '049', 'write', 'failed', 1, 'rate_limit', 0.10, 0, datetime('now', '-16 days')),
  ('rl-017', '049', 'write', 'failed', 1, 'rate_limit', 0.10, 0, datetime('now', '-17 days')),
  ('rl-018', '049', 'write', 'failed', 1, 'rate_limit', 0.10, 0, datetime('now', '-18 days')),
  ('rl-019', '049', 'write', 'failed', 1, 'rate_limit', 0.10, 0, datetime('now', '-19 days')),
  ('rl-020', '049', 'write', 'failed', 1, 'rate_limit', 0.10, 0, datetime('now', '-20 days'));

-- 5 NULL retry_reason decoys — MUST be ignored by the query.
INSERT INTO runs (id, scp_id, stage, status, retry_count, retry_reason, cost_usd, human_override, created_at) VALUES
  ('nul-001', '049', 'write', 'completed', 0, NULL, 0.20, 0, datetime('now', '-1 days')),
  ('nul-002', '049', 'write', 'completed', 0, NULL, 0.20, 0, datetime('now', '-2 days')),
  ('nul-003', '049', 'write', 'completed', 0, NULL, 0.20, 0, datetime('now', '-3 days')),
  ('nul-004', '049', 'write', 'completed', 0, NULL, 0.20, 0, datetime('now', '-4 days')),
  ('nul-005', '049', 'write', 'completed', 0, NULL, 0.20, 0, datetime('now', '-5 days'));
