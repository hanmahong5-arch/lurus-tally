-- 000041_import_rls_guc_safe.down.sql
-- Revert the import-table RLS policies to their original (crash-prone) cast form.

SET search_path TO tally;

DROP POLICY IF EXISTS tenant_isolation ON import_sku_map;
CREATE POLICY tenant_isolation ON import_sku_map
    USING (tenant_id = current_setting('app.tenant_id')::uuid);

DROP POLICY IF EXISTS tenant_isolation ON import_order_seen;
CREATE POLICY tenant_isolation ON import_order_seen
    USING (tenant_id = current_setting('app.tenant_id')::uuid);

DROP POLICY IF EXISTS tenant_isolation ON import_order_cancel_seen;
CREATE POLICY tenant_isolation ON import_order_cancel_seen
    USING (tenant_id = current_setting('app.tenant_id')::uuid);

DROP POLICY IF EXISTS tenant_isolation ON import_refund_seen;
CREATE POLICY tenant_isolation ON import_refund_seen
    USING (tenant_id = current_setting('app.tenant_id')::uuid);
