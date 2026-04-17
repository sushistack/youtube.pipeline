-- Migration 002: Add updated_at trigger for runs table
-- Resolves deferred work from Story 1.2: runs.updated_at had no AFTER UPDATE trigger.
--
-- The WHEN guard prevents infinite recursion if PRAGMA recursive_triggers is
-- ever set to ON (SQLite default is OFF): once the trigger sets updated_at,
-- the subsequent trigger invocation sees OLD.updated_at = NEW.updated_at and
-- becomes a no-op. Also avoids redundant updates when the caller already
-- touched updated_at explicitly.

CREATE TRIGGER IF NOT EXISTS runs_updated_at
AFTER UPDATE ON runs
WHEN OLD.updated_at IS NEW.updated_at
BEGIN
    UPDATE runs SET updated_at = datetime('now') WHERE id = NEW.id;
END;
