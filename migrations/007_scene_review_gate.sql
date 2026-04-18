ALTER TABLE segments
    ADD COLUMN review_status TEXT NOT NULL DEFAULT 'pending';

ALTER TABLE segments
    ADD COLUMN safeguard_flags TEXT NOT NULL DEFAULT '[]';

CREATE INDEX IF NOT EXISTS idx_segments_run_review_status
    ON segments(run_id, review_status, scene_index);
