ALTER TABLE runs
    ADD COLUMN character_query_key TEXT;

ALTER TABLE runs
    ADD COLUMN selected_character_id TEXT;

CREATE TABLE character_search_cache (
    query_key   TEXT PRIMARY KEY,
    query_text  TEXT NOT NULL,
    result_json TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TRIGGER IF NOT EXISTS character_search_cache_updated_at
AFTER UPDATE ON character_search_cache
WHEN OLD.updated_at IS NEW.updated_at
BEGIN
    UPDATE character_search_cache
       SET updated_at = datetime('now')
     WHERE query_key = NEW.query_key;
END;
