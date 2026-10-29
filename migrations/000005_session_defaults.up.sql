ALTER TABLE settings ALTER COLUMN idle_minutes SET DEFAULT 480;
ALTER TABLE settings ALTER COLUMN max_hours SET DEFAULT 72;
-- 仅替换旧默认值，保留管理员已设置的其他会话时长。
UPDATE settings SET
    idle_minutes = CASE WHEN idle_minutes = 30 THEN 480 ELSE idle_minutes END,
    max_hours = CASE WHEN max_hours = 8 THEN 72 ELSE max_hours END;
