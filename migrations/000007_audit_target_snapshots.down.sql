ALTER TABLE audits DROP COLUMN target_details;
ALTER TABLE audits DROP COLUMN target_label;
DROP INDEX tasks_audit;
