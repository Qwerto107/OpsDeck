ALTER TABLE settings ALTER COLUMN auto_refresh SET DEFAULT true;
UPDATE settings SET auto_refresh = true WHERE id = 1;
