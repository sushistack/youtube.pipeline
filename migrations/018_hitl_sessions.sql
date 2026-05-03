-- Migration 018: HITL session pause state (Story 2.6, FR49 + FR50).
-- Originally authored as 004 alongside 004_anti_progress_index.sql; the
-- duplicate version number caused the migrate runner (which records progress
-- via PRAGMA user_version) to apply only the first 004 alphabetically and
-- silently skip this one on every fresh install. Renumbered to 018; the
-- CREATE TABLE below uses IF NOT EXISTS so re-running on any environment
-- that already managed to apply the original file is a no-op.
--
-- One row per run that is currently paused at a HITL checkpoint
-- (scenario_review, character_pick, batch_review, metadata_ack with
-- status='waiting'). The row is upserted on every decision event and
-- deleted when the run exits the HITL state (status moves away from
-- "waiting" via Resume, or the run is cancelled).
--
-- Lifecycle invariant: row exists iff run.status='waiting' AND
-- run.stage ∈ HITL stages. Kept in sync via DecisionStore.UpsertSession
-- (creation/update) and DecisionStore.DeleteSession (cleanup on state
-- exit). Orphan rows should never exist in steady state; the
-- BuildStatus handler defensively logs a Warn when it encounters
-- status≠waiting but session row present.
--
-- snapshot_json stores a domain.DecisionSnapshot JSON blob used by the
-- FR50 change-diff computation: the stored snapshot is the state
-- captured at the operator's last interaction (T1) and is compared
-- against the current live state to produce before/after entries.

CREATE TABLE IF NOT EXISTS hitl_sessions (
    run_id                     TEXT    PRIMARY KEY REFERENCES runs(id),
    stage                      TEXT    NOT NULL,
    scene_index                INTEGER NOT NULL,
    last_interaction_timestamp TEXT    NOT NULL,
    snapshot_json              TEXT    NOT NULL DEFAULT '{}',
    created_at                 TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at                 TEXT    NOT NULL DEFAULT (datetime('now'))
);

-- Trigger to advance updated_at on every row update (mirrors Migration
-- 002 for runs). The WHEN clause prevents infinite recursion when the
-- trigger body itself issues the UPDATE that advances updated_at.
CREATE TRIGGER IF NOT EXISTS hitl_sessions_updated_at
AFTER UPDATE ON hitl_sessions
WHEN OLD.updated_at IS NEW.updated_at
BEGIN
    UPDATE hitl_sessions SET updated_at = datetime('now') WHERE run_id = NEW.run_id;
END;
