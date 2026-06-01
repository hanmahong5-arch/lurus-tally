-- 000043_subtable_rls_force_case.up.sql
-- Bind the RLS backstop to the remaining tenant_id child tables.
--
-- 000042 made every PARENT tenant table FORCE + short-circuit CASE so the owner
-- connection can no longer bypass isolation. But a handful of child / detail
-- tables (sub-rows hung off a parent, plus a couple of standalone dictionaries)
-- still carried NO row-level security at all -- they relied entirely on each
-- query's hand-written `WHERE tenant_id = $N`. A single dropped WHERE on any of
-- these leaks or corrupts another tenant's rows exactly as on the parents.
--
-- These tables all declare `tenant_id UUID NOT NULL`, so the strict canonical
-- form is safe: there is no global / shared row that needs cross-tenant
-- visibility. For EACH table this migration:
--   1. ENABLE ROW LEVEL SECURITY,
--   2. CREATE POLICY tenant_isolation with the proven short-circuit CASE form
--      (empty -> true) on both USING and WITH CHECK,
--   3. FORCE ROW LEVEL SECURITY so the policy also binds the owner connection.
--
-- The empty -> true arm is what keeps this non-breaking: writes happen either
-- inside an already-pinned transaction (app.tenant_id set, so the ELSE arm
-- enforces equality) or on an unpinned path that still relies on its WHERE
-- clause (GUC unset -> CASE returns true -> unchanged behaviour). The ::uuid
-- cast is never handed an empty string.
--
-- EXCLUDED: dict_type and dict_data. Their tenant_id is NULLABLE on purpose --
-- a NULL tenant_id row is a GLOBAL config dictionary entry shared by all
-- tenants. Strict `tenant_id = current_setting(...)::uuid` would evaluate to
-- NULL (not true) for those rows and hide global config from every tenant. They
-- need a tailored OR-global policy and are intentionally left out of this DRY
-- loop.

SET search_path TO tally;

-- Child / detail tenant tables with tenant_id NOT NULL and no prior RLS.
-- Canonical CASE (empty -> true) USING + WITH CHECK + FORCE, applied uniformly.
DO $$
DECLARE
    t   text;
    child_tables text[] := ARRAY[
        'org_user_rel','partner_bank','product_category','product_sku',
        'product_attribute','unit','warehouse_bin','stock_initial',
        'stock_serial','finance_account','finance_category','payment_item',
        'shopify_shop_map'
    ];
BEGIN
    FOREACH t IN ARRAY child_tables LOOP
        EXECUTE format('ALTER TABLE tally.%I ENABLE ROW LEVEL SECURITY', t);

        EXECUTE format('DROP POLICY IF EXISTS tenant_isolation ON tally.%I', t);
        EXECUTE format($q$
            CREATE POLICY tenant_isolation ON tally.%I
                USING (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                            THEN true
                            ELSE tenant_id = current_setting('app.tenant_id', true)::uuid END)
                WITH CHECK (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                                 THEN true
                                 ELSE tenant_id = current_setting('app.tenant_id', true)::uuid END)
        $q$, t);

        EXECUTE format('ALTER TABLE tally.%I FORCE ROW LEVEL SECURITY', t);
    END LOOP;
END $$;
