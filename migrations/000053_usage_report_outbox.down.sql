-- 000053_usage_report_outbox.down.sql
DROP POLICY IF EXISTS usage_report_outbox_isolation ON tally.usage_report_outbox;
DROP TABLE IF EXISTS tally.usage_report_outbox;
