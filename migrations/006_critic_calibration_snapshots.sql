-- computed_at has no DEFAULT (datetime('now')); callers must always supply it
-- from the injected clock so tests and trend ordering stay deterministic.
CREATE TABLE critic_calibration_snapshots (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    source_key            TEXT NOT NULL UNIQUE,
    window_size           INTEGER NOT NULL,
    window_count          INTEGER NOT NULL,
    provisional           INTEGER NOT NULL DEFAULT 0,
    calibration_threshold REAL NOT NULL,
    kappa                 REAL,
    reason                TEXT,
    agreement_yes_yes     INTEGER NOT NULL DEFAULT 0,
    disagreement_yes_no   INTEGER NOT NULL DEFAULT 0,
    disagreement_no_yes   INTEGER NOT NULL DEFAULT 0,
    agreement_no_no       INTEGER NOT NULL DEFAULT 0,
    window_start_run_id   TEXT,
    window_end_run_id     TEXT,
    latest_decision_id    INTEGER NOT NULL DEFAULT 0,
    computed_at           TEXT NOT NULL
);

CREATE INDEX idx_critic_calibration_snapshots_window_computed_at
    ON critic_calibration_snapshots(window_size, computed_at DESC, id DESC);
