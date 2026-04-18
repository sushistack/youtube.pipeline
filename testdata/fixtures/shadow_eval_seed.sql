-- Fixture: Shadow eval replay candidates (Story 4.2).
--
-- Distribution (15 rows):
--   12 eligible completed runs: status='completed', critic_score >= 0.70,
--      scenario_path NOT NULL. created_at spans -1..-12 days so recency
--      ordering is deterministic and the default window (10) is exceeded.
--   3 decoys that MUST be excluded by the WHERE clause:
--      * scp-shadow-decoy-failed-1        — status='failed'
--      * scp-shadow-decoy-below-threshold — completed but critic_score=0.65
--      * scp-shadow-decoy-null-scenario   — completed but scenario_path=NULL
--
-- scenario_path values use repo-relative paths under
-- testdata/fixtures/shadow_scenarios/<group>/scenario.json. Three distinct
-- artifact files are shared across the 12 eligible rows so the at-least-3
-- artifacts requirement is satisfied without bloating test data.
--
-- Consumers: shadow_source_test.go, shadow_integration_test.go, and
-- internal/critic/eval shadow replay unit tests.

-- ── Eligible completed runs (12), ordered so scp-shadow-run-01 is newest ──
INSERT INTO runs (id, scp_id, stage, status, retry_count, cost_usd, token_in, token_out, duration_ms, human_override, critic_score, scenario_path, created_at, updated_at) VALUES
 ('scp-shadow-run-01','shadow-01','complete','completed',0,0.50,1000,500,2000,0,0.92,'testdata/fixtures/shadow_scenarios/pass/scenario.json',           datetime('now','-1 days'),  datetime('now','-1 days')),
 ('scp-shadow-run-02','shadow-02','complete','completed',0,0.52,1050,520,2100,0,0.88,'testdata/fixtures/shadow_scenarios/accept/scenario.json',         datetime('now','-2 days'),  datetime('now','-2 days')),
 ('scp-shadow-run-03','shadow-03','complete','completed',0,0.55,1100,540,2200,0,0.81,'testdata/fixtures/shadow_scenarios/retry/scenario.json',          datetime('now','-3 days'),  datetime('now','-3 days')),
 ('scp-shadow-run-04','shadow-04','complete','completed',0,0.58,1150,560,2300,0,0.85,'testdata/fixtures/shadow_scenarios/pass/scenario.json',           datetime('now','-4 days'),  datetime('now','-4 days')),
 ('scp-shadow-run-05','shadow-05','complete','completed',0,0.60,1200,580,2400,0,0.76,'testdata/fixtures/shadow_scenarios/accept/scenario.json',         datetime('now','-5 days'),  datetime('now','-5 days')),
 ('scp-shadow-run-06','shadow-06','complete','completed',0,0.62,1250,600,2500,0,0.90,'testdata/fixtures/shadow_scenarios/retry/scenario.json',          datetime('now','-6 days'),  datetime('now','-6 days')),
 ('scp-shadow-run-07','shadow-07','complete','completed',0,0.64,1300,620,2600,0,0.71,'testdata/fixtures/shadow_scenarios/pass/scenario.json',           datetime('now','-7 days'),  datetime('now','-7 days')),
 ('scp-shadow-run-08','shadow-08','complete','completed',0,0.66,1350,640,2700,0,0.83,'testdata/fixtures/shadow_scenarios/accept/scenario.json',         datetime('now','-8 days'),  datetime('now','-8 days')),
 ('scp-shadow-run-09','shadow-09','complete','completed',0,0.68,1400,660,2800,0,0.95,'testdata/fixtures/shadow_scenarios/retry/scenario.json',          datetime('now','-9 days'),  datetime('now','-9 days')),
 ('scp-shadow-run-10','shadow-10','complete','completed',0,0.70,1450,680,2900,0,0.78,'testdata/fixtures/shadow_scenarios/pass/scenario.json',           datetime('now','-10 days'),datetime('now','-10 days')),
 ('scp-shadow-run-11','shadow-11','complete','completed',0,0.72,1500,700,3000,0,0.80,'testdata/fixtures/shadow_scenarios/accept/scenario.json',         datetime('now','-11 days'),datetime('now','-11 days')),
 ('scp-shadow-run-12','shadow-12','complete','completed',0,0.74,1550,720,3100,0,0.73,'testdata/fixtures/shadow_scenarios/retry/scenario.json',          datetime('now','-12 days'),datetime('now','-12 days'));

-- ── Decoys that must NOT be returned by RecentPassedCases ───────────────
INSERT INTO runs (id, scp_id, stage, status, retry_count, cost_usd, token_in, token_out, duration_ms, human_override, critic_score, retry_reason, scenario_path, created_at, updated_at) VALUES
 ('scp-shadow-decoy-failed-1',       'decoy-f','write',   'failed',   2,0.30, 500,200,1800,0,0.82,'stage_failed','testdata/fixtures/shadow_scenarios/pass/scenario.json', datetime('now','-1 hours'), datetime('now','-1 hours')),
 ('scp-shadow-decoy-below-threshold','decoy-b','complete','completed',0,0.40, 800,300,2100,0,0.65, NULL,         'testdata/fixtures/shadow_scenarios/pass/scenario.json', datetime('now','-2 hours'), datetime('now','-2 hours')),
 ('scp-shadow-decoy-null-scenario',  'decoy-n','complete','completed',0,0.45, 900,360,2300,0,0.88, NULL,          NULL,                                                   datetime('now','-3 hours'), datetime('now','-3 hours'));
