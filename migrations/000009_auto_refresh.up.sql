ALTER TABLE settings ADD COLUMN auto_refresh boolean NOT NULL DEFAULT false;
ALTER TABLE resources ADD COLUMN monitor_refreshed_at timestamptz;
