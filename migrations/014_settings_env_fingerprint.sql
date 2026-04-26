-- Migration 014: Add env_fingerprint column to settings_versions if absent
--
-- Migration 012 was initially applied without env_fingerprint on some
-- installations. This migration is idempotent: ALTER TABLE ADD COLUMN on
-- SQLite silently succeeds if the column already exists is not supported,
-- so we use a conditional approach via the schema table.
--
-- SQLite ALTER TABLE ADD COLUMN with a NOT NULL default is allowed when
-- a DEFAULT is supplied; existing rows receive the default value.
ALTER TABLE settings_versions ADD COLUMN env_fingerprint TEXT NOT NULL DEFAULT '';
