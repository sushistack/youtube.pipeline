-- Migration 016: Collapse two-stage settings architecture.
--
-- Removes per-run settings pinning (run_settings_assignments) and the
-- pending/effective two-stage promotion (settings_state.pending_version,
-- queued_at). Both were built around a single design intent — protect an
-- in-flight run from a concurrent settings change — that costs more than it
-- saves in single-operator usage: a failed run on resume reads the stale
-- settings of its first attempt, which is the opposite of what an operator
-- wants after fixing a misconfiguration. Mid-execution safety is already
-- delivered by each phase executor snapshotting settings into memory at the
-- start of its Run call, so the on-disk pin adds no protection beyond
-- friction.
--
-- Post-migration: settings.Save writes directly to a new effective version
-- and to disk; all runtime readers use the current effective version.

-- 1. Preserve operator intent: promote any outstanding pending version to
--    effective before the column is dropped.
UPDATE settings_state
   SET effective_version = pending_version,
       updated_at        = datetime('now')
 WHERE pending_version IS NOT NULL;

-- 2. Drop pending_version + queued_at from settings_state.
ALTER TABLE settings_state DROP COLUMN pending_version;
ALTER TABLE settings_state DROP COLUMN queued_at;

-- 3. Drop run_settings_assignments table (per-run pinning).
DROP TABLE IF EXISTS run_settings_assignments;
