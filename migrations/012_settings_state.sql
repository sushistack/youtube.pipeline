-- Migration 012: Settings versioning and queued application state

-- settings_versions is the audit history of non-secret config snapshots.
-- NOTE: secrets (.env) are deliberately NOT stored here. Only a SHA-256
-- fingerprint of the env map is retained so operators can detect when
-- secrets changed between versions without exposing key material via
-- SQLite dumps / backups. Secrets remain file-backed in .env.
CREATE TABLE settings_versions (
    version         INTEGER PRIMARY KEY AUTOINCREMENT,
    config_json     TEXT NOT NULL,
    env_fingerprint TEXT NOT NULL,
    created_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE settings_state (
    id                INTEGER PRIMARY KEY CHECK (id = 1),
    effective_version INTEGER REFERENCES settings_versions(version),
    pending_version   INTEGER REFERENCES settings_versions(version),
    queued_at         TEXT,
    updated_at        TEXT NOT NULL DEFAULT (datetime('now'))
);

-- run_settings_assignments pins each run to the settings version that was
-- effective at its creation. This is how AC-4's "safe seam" guarantee holds
-- per-run: a run already in-flight keeps resolving to its pinned version
-- until it reaches a stage boundary that explicitly re-pins it, even if
-- pending_version → effective_version promotion happens concurrently.
CREATE TABLE run_settings_assignments (
    run_id            TEXT PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE,
    settings_version  INTEGER NOT NULL REFERENCES settings_versions(version),
    assigned_at       TEXT NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO settings_state (id, effective_version, pending_version, queued_at)
VALUES (1, NULL, NULL, NULL);
