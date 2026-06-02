-- 000046_rls_strict_flip_money_tables.up.sql
-- Phase 3 (second increment): flip the short-circuit CASE arm to THEN false for
-- the core product/money/stock tables. Now safe because EVERY writer of these
-- tables is pinned:
--   * /api/v1 endpoints  -> middleware.TenantDB pins the request connection;
--   * CSV import (/api/v1/imports/orders) -> same middleware;
--   * the PUBLIC shopify webhook import   -> pinned via BuildShopifyHandler's
--     WithPinner / dbscope.WithPinnedConn (migration prerequisite, added in the
--     preceding change).
-- With every path pinned, an UNPINNED query on these tables means a forgotten
-- pin — and now fails loud (0 rows / rejected write) instead of silently leaning
-- on a hand-written WHERE.
--
-- End-to-end validated under this strict policy by the non-superuser app-boot
-- e2e: TestRLS_E2E_{CrossTenantIsolation (product), EntityCRUDIsolation
-- (warehouse/exchange_rate/unit), MoneyFlowIsolation (bill/stock/payment),
-- WebhookImportIsolation (import bill/stock)} all still serve correctly, plus
-- TestRLS_StrictFlip (unpinned -> 0).
--
-- Still relaxed (kept empty->true; out of scope here): import_*/shopify_shop_map
-- (webhook cross-tenant), event_outbox/account_audit_log (background workers),
-- nursery_dict (startup seed), tenant_profile/user_identity_mapping/
-- personal_access_token (pre-tenant auth), user_session (session-record pre-pin),
-- and the child tables with no e2e endpoint coverage yet (partner, product_unit,
-- product_sku/category/attribute, finance_*, stock_initial/serial, warehouse_bin,
-- org_*, audit_log, system_config).

SET search_path TO tally;

-- Strict tables: empty-GUC arm -> false on USING and WITH CHECK.
DO $$
DECLARE
    t text;
    flip_tables text[] := ARRAY[
        'product','warehouse','exchange_rate',
        'bill_head','bill_item','bill_sequence',
        'stock_snapshot','stock_movement','stock_lot',
        'payment_head','payment_item'
    ];
BEGIN
    FOREACH t IN ARRAY flip_tables LOOP
        EXECUTE format('DROP POLICY IF EXISTS tenant_isolation ON tally.%I', t);
        EXECUTE format($q$
            CREATE POLICY tenant_isolation ON tally.%I
                USING (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                            THEN false
                            ELSE tenant_id = current_setting('app.tenant_id', true)::uuid END)
                WITH CHECK (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                                 THEN false
                                 ELSE tenant_id = current_setting('app.tenant_id', true)::uuid END)
        $q$, t);
    END LOOP;
END $$;

-- unit_def: keep cross-tenant visibility of seeded system units in the ELSE arm;
-- empty-GUC arm -> false. (All unit_def reads are pinned via the unit repo.)
DROP POLICY IF EXISTS tenant_isolation ON unit_def;
CREATE POLICY tenant_isolation ON unit_def
    USING (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                THEN false
                ELSE tenant_id = current_setting('app.tenant_id', true)::uuid OR is_system = true END)
    WITH CHECK (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                     THEN false
                     ELSE tenant_id = current_setting('app.tenant_id', true)::uuid OR is_system = true END);
