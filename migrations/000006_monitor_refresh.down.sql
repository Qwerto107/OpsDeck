ALTER TABLE tasks DROP COLUMN refresh_results;
ALTER TABLE tasks DROP COLUMN resource_ids;
ALTER TABLE settings ADD COLUMN refresh_seconds integer NOT NULL DEFAULT 60;
ALTER TABLE settings DROP COLUMN stale_minutes;
