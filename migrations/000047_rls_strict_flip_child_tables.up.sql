-- 000047_rls_strict_flip_child_tables.up.sql
-- Phase 3 (final increment): flip the short-circuit CASE arm from THEN true to
-- THEN false for the last tenant-scoped tables still running empty->true.
--
-- After 000045 (supplier/project) and 000046 (product/unit_def/money/stock) this
-- closes the remaining gap so that EVERY tenant table -- except the handful that
-- are relaxed BY DESIGN (see below) -- fails LOUD on an unpinned access: an
-- unset app.tenant_id yields 0 rows / a rejected write instead of silently
-- leaning on a hand-written WHERE. That is the whole point of the RLS backstop.
--
-- SCOPE (12 tables), each verified to be reached only by GUC-bound paths:
--   * partner            -- read by the pinned replenish JOIN + customer search
--                           (both /api/v1, pinned by middleware.TenantDB); the
--                           only writer is tenant bootstrap, which sets app.tenant_id
--                           tx-locally BEFORE its INSERTs (it already seeds the
--                           ALREADY-strict `warehouse` in the same tx without error,
--                           proving the GUC is set). The public webhook import does
--                           NOT touch partner.
--   * product_sku        -- read/written by the SKU + AI assistant use cases, all
--                           under the pinned /api/v1 group (exercised by the
--                           ai_confirm e2e and the read-path e2e).
--   * stock_initial      -- read by digest / replenish / stock repos and written by
--                           onboarding, all pinned /api/v1 paths (exercised by the
--                           read-path e2e: weekly-summary, replenish, dead-stock).
--   * org_user_rel, partner_bank, product_category, product_attribute, unit,
--     warehouse_bin, stock_serial, finance_account, finance_category
--                        -- no live reader/writer yet (forward-declared schema);
--                           flipping is pure structural hardening so that a future
--                           repo that forgets a WHERE on an unpinned path fails
--                           closed rather than leaking. They keep the canonical
--                           plain tenant_id CASE 000043 gave them, arm flipped.
--
-- RELAXED BY DESIGN -- intentionally NOT flipped here (must stay empty->true):
--   * shopify_shop_map, import_*          -- public webhook resolves shop->tenant
--                                            and dedups BEFORE the tenant is known.
--   * event_outbox, account_audit_log     -- background workers, no request GUC
--                                            (event_outbox uses its 'service' arm).
--   * nursery_dict / dict_type / dict_data -- startup seed / NULL-tenant global rows.
--   * tenant_profile, user_identity_mapping,
--     personal_access_token               -- pre-tenant auth (resolved before pin).
--   * user_session                        -- session-record runs before the pin.
--
-- All 12 tables declare tenant_id NOT NULL with no shared/global row, so the
-- strict canonical form is safe (no row needs cross-tenant visibility). FORCE was
-- already set by 000042 / 000043; DROP+CREATE POLICY leaves it intact.

SET search_path TO tally;

DO $$
DECLARE
    t text;
    flip_tables text[] := ARRAY[
        'partner',
        'product_sku','stock_initial',
        'org_user_rel','partner_bank','product_category','product_attribute',
        'unit','warehouse_bin','stock_serial','finance_account','finance_category'
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
        -- FORCE already set by 000042/000043; DROP/CREATE POLICY leaves it intact.
    END LOOP;
END $$;
