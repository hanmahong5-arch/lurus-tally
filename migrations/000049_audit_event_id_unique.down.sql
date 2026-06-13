-- 000049_audit_event_id_unique.down.sql
-- Reverse 000049: drop the dedup index and the event_id column.

SET search_path TO tally;

DROP INDEX IF EXISTS tally.ux_account_audit_log_event_id;

ALTER TABLE tally.account_audit_log
    DROP COLUMN IF EXISTS event_id;
