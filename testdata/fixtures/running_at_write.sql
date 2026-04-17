-- Fixture: run at stage=write, status=running with zero observability deltas.
-- Consumed by the 429 backoff integration test to prove NFR-P3:
--   the stage/status is not advanced by the retry path.

INSERT INTO runs (id, scp_id, stage, status, retry_count,
                  cost_usd, token_in, token_out, duration_ms, human_override,
                  created_at, updated_at)
VALUES ('scp-049-run-1', '049', 'write', 'running', 0,
        0.00, 0, 0, 0, 0,
        '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
