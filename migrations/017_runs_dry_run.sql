-- Migration 017: snapshot DryRun mode on the runs row at creation.
--
-- When PipelineConfig.DryRun is true at the moment a run is created, the
-- Phase B image and TTS clients are swapped for in-process fakes that
-- produce placeholder PNG and silent WAV files at zero cost. To make this
-- per-run state durable (so toggling Settings mid-execution does not
-- retroactively reinterpret a run's artifacts), the boolean is snapshotted
-- here. The Phase D / StageAssemble guard in Engine.Advance reads this
-- column to refuse ffmpeg assembly for dry-run rows. Existing runs default
-- to 0 (real-mode) — no backfill required.

ALTER TABLE runs
    ADD COLUMN dry_run INTEGER NOT NULL DEFAULT 0;
