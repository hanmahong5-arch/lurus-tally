-- 000003_init_org.down.sql
-- Drop in reverse FK dependency order.
DROP TABLE IF EXISTS tally.org_user_rel;
DROP TABLE IF EXISTS tally.org_department;
