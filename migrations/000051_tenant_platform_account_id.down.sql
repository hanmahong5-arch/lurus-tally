-- 000051_tenant_platform_account_id.down.sql
-- Reverse 000051: drop the platform account linkage column.

SET search_path TO tally;

ALTER TABLE tally.tenant
    DROP COLUMN IF EXISTS platform_account_id;
