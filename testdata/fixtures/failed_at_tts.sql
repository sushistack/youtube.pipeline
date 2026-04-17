-- Fixture: run failed mid Phase B at the TTS stage.
-- 5 segments: 3 with tts_path completed, 2 pending without audio.
-- Tests load this + materialize tts/*.wav files via t.TempDir() to
-- match the DB's tts_path expectations.

INSERT INTO runs (id, scp_id, stage, status, retry_count, retry_reason,
                  cost_usd, token_in, token_out, duration_ms, human_override,
                  scenario_path, created_at, updated_at)
VALUES ('scp-049-run-1', '049', 'tts', 'failed', 1, 'upstream_timeout',
        0.95, 12000, 2500, 38000, 0,
        'scenario.json', '2026-01-01T00:00:00Z', '2026-01-01T00:30:00Z');

INSERT INTO segments (run_id, scene_index, narration, shot_count, tts_path, status)
VALUES ('scp-049-run-1', 0, 'scene zero narration', 1, 'tts/scene_01.wav', 'completed'),
       ('scp-049-run-1', 1, 'scene one narration',  1, 'tts/scene_02.wav', 'completed'),
       ('scp-049-run-1', 2, 'scene two narration',  1, 'tts/scene_03.wav', 'completed'),
       ('scp-049-run-1', 3, 'scene three narration', 1, NULL, 'pending'),
       ('scp-049-run-1', 4, 'scene four narration',  1, NULL, 'pending');
