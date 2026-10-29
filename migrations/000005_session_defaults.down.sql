ALTER TABLE settings ALTER COLUMN idle_minutes SET DEFAULT 30;
ALTER TABLE settings ALTER COLUMN max_hours SET DEFAULT 8;
UPDATE settings SET
    idle_minutes = CASE WHEN idle_minutes = 480 THEN 30 ELSE idle_minutes END,
    max_hours = CASE WHEN max_hours = 72 THEN 8 ELSE max_hours END;
