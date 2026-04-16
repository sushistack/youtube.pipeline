-- Migration 001: Initial schema

CREATE TABLE runs (
    id           TEXT PRIMARY KEY,
    scp_id       TEXT NOT NULL,
    stage        TEXT NOT NULL DEFAULT 'pending',
    status       TEXT NOT NULL DEFAULT 'pending',
    retry_count  INTEGER NOT NULL DEFAULT 0,
    retry_reason TEXT,
    critic_score REAL,
    cost_usd     REAL NOT NULL DEFAULT 0.0,
    token_in     INTEGER NOT NULL DEFAULT 0,
    token_out    INTEGER NOT NULL DEFAULT 0,
    duration_ms  INTEGER NOT NULL DEFAULT 0,
    human_override INTEGER NOT NULL DEFAULT 0,
    scenario_path TEXT,
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE decisions (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id          TEXT NOT NULL REFERENCES runs(id),
    scene_id        TEXT,
    decision_type   TEXT NOT NULL,
    context_snapshot TEXT,
    outcome_link    TEXT,
    tags            TEXT,
    feedback_source TEXT,
    external_ref    TEXT,
    feedback_at     TEXT,
    superseded_by   INTEGER REFERENCES decisions(id),
    note            TEXT,
    created_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE segments (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id          TEXT NOT NULL REFERENCES runs(id),
    scene_index     INTEGER NOT NULL,
    narration       TEXT,
    shot_count      INTEGER NOT NULL DEFAULT 1,
    shots           TEXT,
    tts_path        TEXT,
    tts_duration_ms INTEGER,
    clip_path       TEXT,
    critic_score    REAL,
    critic_sub      TEXT,
    status          TEXT NOT NULL DEFAULT 'pending',
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(run_id, scene_index)
);
