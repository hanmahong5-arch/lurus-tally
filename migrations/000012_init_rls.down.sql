-- 000012_init_rls.down.sql
-- Removes RLS policies and disables RLS for all 11 tenant-scoped tables.
-- Reverse order of up migration.

DROP POLICY IF EXISTS org_department_rls ON tally.org_department;
ALTER TABLE tally.org_department DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS bill_sequence_rls ON tally.bill_sequence;
ALTER TABLE tally.bill_sequence DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS system_config_rls ON tally.system_config;
ALTER TABLE tally.system_config DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS audit_log_rls ON tally.audit_log;
ALTER TABLE tally.audit_log DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS payment_head_rls ON tally.payment_head;
ALTER TABLE tally.payment_head DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS bill_item_rls ON tally.bill_item;
ALTER TABLE tally.bill_item DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS bill_head_rls ON tally.bill_head;
ALTER TABLE tally.bill_head DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS stock_snapshot_rls ON tally.stock_snapshot;
ALTER TABLE tally.stock_snapshot DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS warehouse_rls ON tally.warehouse;
ALTER TABLE tally.warehouse DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS product_rls ON tally.product;
ALTER TABLE tally.product DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS partner_rls ON tally.partner;
ALTER TABLE tally.partner DISABLE ROW LEVEL SECURITY;
