CREATE TABLE scp_image_library (
    scp_id              TEXT PRIMARY KEY,
    file_path           TEXT NOT NULL,
    source_ref_url      TEXT NOT NULL,
    source_query_key    TEXT NOT NULL,
    source_candidate_id TEXT NOT NULL,
    frozen_descriptor   TEXT NOT NULL,
    prompt_used         TEXT NOT NULL,
    seed                INTEGER NOT NULL,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at          TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TRIGGER IF NOT EXISTS scp_image_library_updated_at
AFTER UPDATE ON scp_image_library
WHEN OLD.updated_at IS NEW.updated_at
BEGIN
    UPDATE scp_image_library
       SET updated_at = datetime('now')
     WHERE scp_id = NEW.scp_id;
END;
