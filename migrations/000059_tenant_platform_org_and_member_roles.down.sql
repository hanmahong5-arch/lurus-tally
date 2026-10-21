-- 000059_tenant_platform_org_and_member_roles.down.sql
-- Restores the 000013 role CHECK. New roles fold back into the closest legacy value first.
SET search_path TO tally;

DROP TABLE IF EXISTS tally.dev_org_invite;
DROP TABLE IF EXISTS tally.dev_org_member;
DROP TABLE IF EXISTS tally.dev_org;
DROP TABLE IF EXISTS tally.dev_account;

UPDATE tally.user_identity_mapping SET role = 'admin' WHERE role = 'owner';
UPDATE tally.user_identity_mapping SET role = 'clerk' WHERE role IN ('cashier', 'finance');
ALTER TABLE tally.user_identity_mapping DROP CONSTRAINT IF EXISTS user_identity_mapping_role_check;
ALTER TABLE tally.user_identity_mapping ADD CONSTRAINT user_identity_mapping_role_check
    CHECK (role IN ('admin', 'manager', 'clerk', 'viewer'));

DROP INDEX IF EXISTS tally.uim_tenant_account_idx;
ALTER TABLE tally.user_identity_mapping DROP COLUMN IF EXISTS platform_account_id;

DROP INDEX IF EXISTS tally.tenant_platform_org_id_uidx;
ALTER TABLE tally.tenant DROP COLUMN IF EXISTS platform_org_id;
