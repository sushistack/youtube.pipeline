-- Fixture: run failed mid Phase A at the `write` stage.
-- Zero segments; no on-disk artifacts expected for Phase A failures.

INSERT INTO runs (id, scp_id, stage, status, retry_count, retry_reason,
                  cost_usd, token_in, token_out, duration_ms, human_override,
                  created_at, updated_at)
VALUES ('scp-049-run-1', '049', 'write', 'failed', 0, 'rate_limit',
        0.05, 1200, 200, 4500, 0,
        '2026-01-01T00:00:00Z', '2026-01-01T00:05:00Z');
