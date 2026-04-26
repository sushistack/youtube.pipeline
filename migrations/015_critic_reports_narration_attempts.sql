-- Migration 015: critic_reports + narration_attempts (Phase A diagnostics persistence)
--
-- Critic checkpoint reports (PostWriter, PostReviewer) and the narration scripts
-- they evaluated were previously held in memory only. On a retry verdict the
-- pipeline returned ErrStageFailed and the diagnostic payload (rubric, feedback,
-- scene_notes, narration text) was discarded. These tables persist every
-- attempt so retry decisions can be audited and writer prompts iterated against
-- concrete failure cases.
--
-- attempt_number is the run's retry_count at the moment Phase A completed
-- (1-indexed: first attempt = 1).

CREATE TABLE IF NOT EXISTS critic_reports (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id           TEXT NOT NULL REFERENCES runs(id),
    checkpoint       TEXT NOT NULL,
    attempt_number   INTEGER NOT NULL,
    verdict          TEXT NOT NULL,
    retry_reason     TEXT,
    overall_score    INTEGER NOT NULL,
    rubric_json      TEXT NOT NULL,
    feedback         TEXT NOT NULL,
    scene_notes_json TEXT NOT NULL,
    precheck_json    TEXT NOT NULL,
    critic_model     TEXT,
    critic_provider  TEXT,
    source_version   TEXT,
    created_at       TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_critic_reports_run_checkpoint
    ON critic_reports(run_id, checkpoint, created_at DESC);

CREATE TABLE IF NOT EXISTS narration_attempts (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id         TEXT NOT NULL REFERENCES runs(id),
    attempt_number INTEGER NOT NULL,
    narration_json TEXT NOT NULL,
    created_at     TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_narration_attempts_run
    ON narration_attempts(run_id, created_at DESC);
