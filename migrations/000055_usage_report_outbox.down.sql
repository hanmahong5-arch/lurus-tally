-- 000055_usage_report_outbox.down.sql (renumbered from 000053 — see the .up.sql header)
DROP POLICY IF EXISTS usage_report_outbox_isolation ON tally.usage_report_outbox;
DROP TABLE IF EXISTS tally.usage_report_outbox;
