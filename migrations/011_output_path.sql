-- Migration 011: add output_path column to runs table.
--
-- Story 9.1 (FFmpeg Two‑Stage Assembly Engine) requires the engine to store the
-- final assembled video path (`{runDir}/output.mp4`) in the runs table for
-- observability and resume safety. This column is nullable; existing runs that
-- never reached StageAssemble will have NULL.

ALTER TABLE runs
    ADD COLUMN output_path TEXT;