-- 000060_pos_offline_client_ref.down.sql
SET search_path TO tally;

DROP INDEX IF EXISTS tally.bill_head_tenant_client_ref_uidx;

ALTER TABLE tally.bill_head
    DROP COLUMN IF EXISTS client_ref,
    DROP COLUMN IF EXISTS occurred_at;

-- Narrow source back to VARCHAR(10); values that no longer fit (pos_offline) fall back to 'pos'.
UPDATE tally.bill_head SET source = 'pos' WHERE length(source) > 10;
ALTER TABLE tally.bill_head
    ALTER COLUMN source TYPE VARCHAR(10);
