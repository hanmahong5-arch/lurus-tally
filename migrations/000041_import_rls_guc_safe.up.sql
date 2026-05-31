-- 000041_import_rls_guc_safe.up.sql
-- Harden the four import-table RLS policies against the empty/unset-GUC crash.
--
-- import_sku_map + import_order_seen (000037) and import_order_cancel_seen +
-- import_refund_seen (000040) gate row visibility on
--     tenant_id = current_setting('app.tenant_id')::uuid
-- The public Shopify webhook + CSV import paths run with NO app.tenant_id set, so
-- current_setting('app.tenant_id') (missing_ok=false) raises "unrecognized
-- configuration parameter", and the empty-string GUC state raises
--     ERROR: invalid input syntax for type uuid: "" (SQLSTATE 22P02)
-- Either way the dedup read/write throws, breaking idempotent order/refund dedup
-- and producing duplicate sale/return bills (double stock movements) on webhook
-- retries -- the exact integrity guarantee these tables exist to provide.
--
-- Replace with the short-circuit-safe CASE form already used by 000027. The
-- import queries always carry an explicit WHERE tenant_id = $1, so RLS here is
-- defense-in-depth; the CASE keeps isolation without the crash.

SET search_path TO tally;

DROP POLICY IF EXISTS tenant_isolation ON import_sku_map;
CREATE POLICY tenant_isolation ON import_sku_map
    USING (
        CASE
            WHEN COALESCE(current_setting('app.tenant_id', true), '') = '' THEN true
            ELSE tenant_id = current_setting('app.tenant_id', true)::uuid
        END
    );

DROP POLICY IF EXISTS tenant_isolation ON import_order_seen;
CREATE POLICY tenant_isolation ON import_order_seen
    USING (
        CASE
            WHEN COALESCE(current_setting('app.tenant_id', true), '') = '' THEN true
            ELSE tenant_id = current_setting('app.tenant_id', true)::uuid
        END
    );

DROP POLICY IF EXISTS tenant_isolation ON import_order_cancel_seen;
CREATE POLICY tenant_isolation ON import_order_cancel_seen
    USING (
        CASE
            WHEN COALESCE(current_setting('app.tenant_id', true), '') = '' THEN true
            ELSE tenant_id = current_setting('app.tenant_id', true)::uuid
        END
    );

DROP POLICY IF EXISTS tenant_isolation ON import_refund_seen;
CREATE POLICY tenant_isolation ON import_refund_seen
    USING (
        CASE
            WHEN COALESCE(current_setting('app.tenant_id', true), '') = '' THEN true
            ELSE tenant_id = current_setting('app.tenant_id', true)::uuid
        END
    );
