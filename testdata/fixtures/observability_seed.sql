-- Fixture: 60 runs spanning the last 120 days for rolling-window testing.
--
-- Distribution:
--   42 completed, 9 failed, 6 cancelled, 3 running
--   created_at: deterministic offsets across -1 day .. -120 days
--   cost_usd: hard-coded spread from $0.01 to $4.50
--   critic_score: 30 NULL, 25 in [0.70, 1.00], 5 in [0.40, 0.70)
--   retry_reason set on ~9 failed rows ("rate_limit", "timeout", "stage_failed")
--   human_override=1 on 3 rows
--
-- Consumers: observability_query_test.go (EXPLAIN QUERY PLAN), diagnostic_query_test.go
-- (NFR-O3 queries). Do not change the distribution without updating assertions.

-- ── Completed runs (42) — spread across -1..-110 days ──
INSERT INTO runs (id, scp_id, stage, status, retry_count, cost_usd, token_in, token_out, duration_ms, human_override, critic_score, created_at, updated_at) VALUES
 ('scp-001-run-1','001','complete','completed',0,0.50,1000,500, 2000,0,0.82,datetime('now','-1 days'), datetime('now','-1 days')),
 ('scp-002-run-1','002','complete','completed',0,0.75,1500,300, 3000,0,0.91,datetime('now','-2 days'), datetime('now','-2 days')),
 ('scp-003-run-1','003','complete','completed',0,1.20,2000,800, 4500,0,0.88,datetime('now','-3 days'), datetime('now','-3 days')),
 ('scp-004-run-1','004','complete','completed',0,0.42,800, 250, 1800,0,0.77,datetime('now','-5 days'), datetime('now','-5 days')),
 ('scp-005-run-1','005','complete','completed',0,2.10,3000,1200,6000,1,0.95,datetime('now','-6 days'), datetime('now','-6 days')),
 ('scp-006-run-1','006','complete','completed',0,0.33,600, 200, 1500,0,NULL,datetime('now','-8 days'), datetime('now','-8 days')),
 ('scp-007-run-1','007','complete','completed',0,1.80,2500,900, 5000,0,0.86,datetime('now','-10 days'),datetime('now','-10 days')),
 ('scp-008-run-1','008','complete','completed',1,2.50,3200,1100,7000,0,NULL,datetime('now','-11 days'),datetime('now','-11 days')),
 ('scp-009-run-1','009','complete','completed',0,0.91,1800,400, 3200,0,0.79,datetime('now','-13 days'),datetime('now','-13 days')),
 ('scp-010-run-1','010','complete','completed',0,1.05,2100,600, 3800,0,NULL,datetime('now','-14 days'),datetime('now','-14 days')),
 ('scp-011-run-1','011','complete','completed',0,0.22,500, 150, 1200,0,0.68,datetime('now','-16 days'),datetime('now','-16 days')),
 ('scp-012-run-1','012','complete','completed',0,3.10,4000,1500,8500,0,NULL,datetime('now','-18 days'),datetime('now','-18 days')),
 ('scp-013-run-1','013','complete','completed',0,0.58,1100,350, 2400,0,0.83,datetime('now','-20 days'),datetime('now','-20 days')),
 ('scp-014-run-1','014','complete','completed',0,1.45,2400,700, 4200,0,NULL,datetime('now','-22 days'),datetime('now','-22 days')),
 ('scp-015-run-1','015','complete','completed',0,0.67,1300,420, 2700,0,0.92,datetime('now','-24 days'),datetime('now','-24 days')),
 ('scp-016-run-1','016','complete','completed',0,2.25,3100,1000,6500,0,NULL,datetime('now','-26 days'),datetime('now','-26 days')),
 ('scp-017-run-1','017','complete','completed',0,0.15,400, 100, 1000,0,0.70,datetime('now','-28 days'),datetime('now','-28 days')),
 ('scp-018-run-1','018','complete','completed',0,1.95,2700,950, 5500,0,NULL,datetime('now','-30 days'),datetime('now','-30 days')),
 ('scp-019-run-1','019','complete','completed',0,0.88,1700,550, 3100,0,0.81,datetime('now','-32 days'),datetime('now','-32 days')),
 ('scp-020-run-1','020','complete','completed',0,1.30,2200,720, 4000,0,NULL,datetime('now','-35 days'),datetime('now','-35 days')),
 ('scp-021-run-1','021','complete','completed',0,0.48,900, 310, 2100,0,0.75,datetime('now','-38 days'),datetime('now','-38 days')),
 ('scp-022-run-1','022','complete','completed',0,1.65,2600,830, 4800,0,NULL,datetime('now','-40 days'),datetime('now','-40 days')),
 ('scp-023-run-1','023','complete','completed',0,0.36,700, 240, 1600,0,0.72,datetime('now','-42 days'),datetime('now','-42 days')),
 ('scp-024-run-1','024','complete','completed',0,2.80,3500,1300,7500,1,NULL,datetime('now','-45 days'),datetime('now','-45 days')),
 ('scp-025-run-1','025','complete','completed',0,0.62,1200,380, 2600,0,0.89,datetime('now','-48 days'),datetime('now','-48 days')),
 ('scp-026-run-1','026','complete','completed',0,1.15,2000,650, 3700,0,NULL,datetime('now','-50 days'),datetime('now','-50 days')),
 ('scp-027-run-1','027','complete','completed',0,0.29,550, 180, 1400,0,0.73,datetime('now','-55 days'),datetime('now','-55 days')),
 ('scp-028-run-1','028','complete','completed',0,1.72,2550,810, 4600,0,NULL,datetime('now','-58 days'),datetime('now','-58 days')),
 ('scp-029-run-1','029','complete','completed',0,0.44,850, 270, 2000,0,0.84,datetime('now','-62 days'),datetime('now','-62 days')),
 ('scp-030-run-1','030','complete','completed',0,3.60,4500,1700,9500,0,NULL,datetime('now','-65 days'),datetime('now','-65 days')),
 ('scp-031-run-1','031','complete','completed',0,0.78,1600,500, 2900,0,0.80,datetime('now','-68 days'),datetime('now','-68 days')),
 ('scp-032-run-1','032','complete','completed',0,1.50,2300,740, 4300,0,NULL,datetime('now','-72 days'),datetime('now','-72 days')),
 ('scp-033-run-1','033','complete','completed',0,0.19,450, 120, 1100,0,0.74,datetime('now','-75 days'),datetime('now','-75 days')),
 ('scp-034-run-1','034','complete','completed',0,2.05,2850,1040,5800,0,NULL,datetime('now','-80 days'),datetime('now','-80 days')),
 ('scp-035-run-1','035','complete','completed',0,0.55,1050,340, 2300,0,0.93,datetime('now','-82 days'),datetime('now','-82 days')),
 ('scp-036-run-1','036','complete','completed',0,1.25,2150,680, 3900,0,NULL,datetime('now','-85 days'),datetime('now','-85 days')),
 ('scp-037-run-1','037','complete','completed',0,0.39,750, 260, 1700,0,0.76,datetime('now','-88 days'),datetime('now','-88 days')),
 ('scp-038-run-1','038','complete','completed',0,1.88,2650,900, 5200,0,NULL,datetime('now','-92 days'),datetime('now','-92 days')),
 ('scp-039-run-1','039','complete','completed',0,0.71,1400,430, 2800,0,0.85,datetime('now','-95 days'),datetime('now','-95 days')),
 ('scp-040-run-1','040','complete','completed',0,1.10,1950,620, 3600,0,NULL,datetime('now','-98 days'),datetime('now','-98 days')),
 ('scp-041-run-1','041','complete','completed',0,0.26,520, 170, 1300,0,0.87,datetime('now','-105 days'),datetime('now','-105 days')),
 ('scp-042-run-1','042','complete','completed',0,2.40,3300,1200,7200,0,NULL,datetime('now','-110 days'),datetime('now','-110 days'));

-- ── Failed runs (9) — with retry reasons ──
INSERT INTO runs (id, scp_id, stage, status, retry_count, retry_reason, cost_usd, token_in, token_out, duration_ms, human_override, critic_score, created_at, updated_at) VALUES
 ('scp-100-run-1','100','tts',   'failed',2,'rate_limit',  0.45,1100,350, 2200,0,NULL,datetime('now','-4 days'),  datetime('now','-4 days')),
 ('scp-101-run-1','101','image', 'failed',3,'rate_limit',  1.20,2200,800, 4800,0,NULL,datetime('now','-9 days'),  datetime('now','-9 days')),
 ('scp-102-run-1','102','write', 'failed',1,'timeout',     0.34,850, 270, 1900,0,0.45,datetime('now','-15 days'), datetime('now','-15 days')),
 ('scp-103-run-1','103','critic','failed',4,'stage_failed',0.88,1750,500, 3400,0,0.52,datetime('now','-25 days'), datetime('now','-25 days')),
 ('scp-104-run-1','104','tts',   'failed',2,'timeout',     1.05,2050,650, 4100,0,NULL,datetime('now','-36 days'), datetime('now','-36 days')),
 ('scp-105-run-1','105','image', 'failed',2,'rate_limit',  2.40,3100,1100,7000,0,NULL,datetime('now','-52 days'), datetime('now','-52 days')),
 ('scp-106-run-1','106','assemble','failed',1,'stage_failed',0.09,100,0, 800,0,NULL,datetime('now','-70 days'), datetime('now','-70 days')),
 ('scp-107-run-1','107','write', 'failed',3,'stage_failed',0.48,950, 320, 2100,0,0.48,datetime('now','-88 days'), datetime('now','-88 days')),
 ('scp-108-run-1','108','tts',   'failed',5,'rate_limit',  1.35,2300,720, 5500,1,0.58,datetime('now','-100 days'),datetime('now','-100 days'));

-- ── Cancelled runs (6) ──
INSERT INTO runs (id, scp_id, stage, status, retry_count, cost_usd, token_in, token_out, duration_ms, human_override, created_at, updated_at) VALUES
 ('scp-200-run-1','200','write',     'cancelled',0,0.12,300, 80,  900, 0,datetime('now','-7 days'),  datetime('now','-7 days')),
 ('scp-201-run-1','201','image',     'cancelled',0,0.55,1000,380, 2500,0,datetime('now','-17 days'), datetime('now','-17 days')),
 ('scp-202-run-1','202','scenario_review','cancelled',0,0.08,200, 60, 700, 0,datetime('now','-33 days'), datetime('now','-33 days')),
 ('scp-203-run-1','203','batch_review','cancelled',0,1.40,2300,900, 4400,0,datetime('now','-60 days'), datetime('now','-60 days')),
 ('scp-204-run-1','204','tts',       'cancelled',0,0.32,700, 230, 1600,0,datetime('now','-78 days'), datetime('now','-78 days')),
 ('scp-205-run-1','205','assemble',  'cancelled',0,2.15,2900,1050,6200,0,datetime('now','-102 days'),datetime('now','-102 days'));

-- ── Running runs (3) ──
INSERT INTO runs (id, scp_id, stage, status, retry_count, cost_usd, token_in, token_out, duration_ms, human_override, created_at, updated_at) VALUES
 ('scp-300-run-1','300','image','running',0,0.85,1600,500,3100,0,datetime('now','-1 hours'),datetime('now','-1 hours')),
 ('scp-301-run-1','301','tts',  'running',0,0.25,450, 140,1250,0,datetime('now','-2 hours'),datetime('now','-2 hours')),
 ('scp-302-run-1','302','write','running',0,0.14,350, 110,1050,0,datetime('now','-6 hours'),datetime('now','-6 hours'));
