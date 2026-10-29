ALTER TABLE settings ADD COLUMN app_url text NOT NULL DEFAULT '';
ALTER TABLE settings ADD COLUMN trusted_proxies jsonb NOT NULL DEFAULT '[]';
