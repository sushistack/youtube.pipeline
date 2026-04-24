-- Migration 013: tag runs with the active Critic prompt version at creation.
--
-- Story 10.2 AC-3 requires each newly-created run to record the Critic prompt
-- version and hash that were effective when the run was created, so later
-- metrics/quality tooling can group runs by prompt version. Existing runs
-- remain NULL; no backfill is performed. Tagging is immutable for the run.

ALTER TABLE runs
    ADD COLUMN critic_prompt_version TEXT;

ALTER TABLE runs
    ADD COLUMN critic_prompt_hash TEXT;
