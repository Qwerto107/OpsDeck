ALTER TABLE settings ADD COLUMN stale_minutes integer NOT NULL DEFAULT 10;
ALTER TABLE settings DROP COLUMN refresh_seconds;
ALTER TABLE tasks ADD COLUMN resource_ids jsonb DEFAULT '[]';
ALTER TABLE tasks ADD COLUMN refresh_results jsonb DEFAULT '[]';
