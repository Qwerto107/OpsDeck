ALTER TABLE audits ADD COLUMN target_label text NOT NULL DEFAULT '';
ALTER TABLE audits ADD COLUMN target_details jsonb DEFAULT '[]';
CREATE INDEX tasks_audit ON tasks(audit_id) WHERE audit_id IS NOT NULL;
