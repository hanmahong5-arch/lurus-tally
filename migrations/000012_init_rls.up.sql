-- 000012_init_rls.up.sql
-- Enables Row-Level Security and creates tenant isolation policies for all 11 tenant-scoped tables.
-- The second parameter `true` in current_setting('app.tenant_id', true) makes it return NULL
-- instead of raising an error when the setting is not set (e.g. during superuser migrations).
--
-- RLS tables: partner, product, warehouse, stock_snapshot, bill_head, bill_item,
--             payment_head, audit_log, system_config, bill_sequence, org_department

ALTER TABLE tally.partner ENABLE ROW LEVEL SECURITY;
CREATE POLICY partner_rls ON tally.partner
    USING (tenant_id = current_setting('app.tenant_id', true)::UUID);

ALTER TABLE tally.product ENABLE ROW LEVEL SECURITY;
CREATE POLICY product_rls ON tally.product
    USING (tenant_id = current_setting('app.tenant_id', true)::UUID);

ALTER TABLE tally.warehouse ENABLE ROW LEVEL SECURITY;
CREATE POLICY warehouse_rls ON tally.warehouse
    USING (tenant_id = current_setting('app.tenant_id', true)::UUID);

ALTER TABLE tally.stock_snapshot ENABLE ROW LEVEL SECURITY;
CREATE POLICY stock_snapshot_rls ON tally.stock_snapshot
    USING (tenant_id = current_setting('app.tenant_id', true)::UUID);

ALTER TABLE tally.bill_head ENABLE ROW LEVEL SECURITY;
CREATE POLICY bill_head_rls ON tally.bill_head
    USING (tenant_id = current_setting('app.tenant_id', true)::UUID);

ALTER TABLE tally.bill_item ENABLE ROW LEVEL SECURITY;
CREATE POLICY bill_item_rls ON tally.bill_item
    USING (tenant_id = current_setting('app.tenant_id', true)::UUID);

ALTER TABLE tally.payment_head ENABLE ROW LEVEL SECURITY;
CREATE POLICY payment_head_rls ON tally.payment_head
    USING (tenant_id = current_setting('app.tenant_id', true)::UUID);

ALTER TABLE tally.audit_log ENABLE ROW LEVEL SECURITY;
CREATE POLICY audit_log_rls ON tally.audit_log
    USING (tenant_id = current_setting('app.tenant_id', true)::UUID);

ALTER TABLE tally.system_config ENABLE ROW LEVEL SECURITY;
CREATE POLICY system_config_rls ON tally.system_config
    USING (tenant_id = current_setting('app.tenant_id', true)::UUID);

ALTER TABLE tally.bill_sequence ENABLE ROW LEVEL SECURITY;
CREATE POLICY bill_sequence_rls ON tally.bill_sequence
    USING (tenant_id = current_setting('app.tenant_id', true)::UUID);

ALTER TABLE tally.org_department ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_department_rls ON tally.org_department
    USING (tenant_id = current_setting('app.tenant_id', true)::UUID);
