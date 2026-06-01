-- 000042_rls_force_case_short_circuit.up.sql
-- Make Row-Level Security a real database backstop instead of a flag the
-- production connection silently bypasses.
--
-- Audit #2 finding: the tenant tables carried only `ENABLE ROW LEVEL SECURITY`,
-- and the application connects to PostgreSQL as the table OWNER. ENABLE does not
-- apply to the owner -- so RLS never actually ran in production, and cross-tenant
-- isolation rested entirely on every query carrying a hand-written
-- `WHERE tenant_id = $N`. A single missing or injection-stripped WHERE leaks or
-- corrupts another tenant's data.
--
-- Fix, in two structural moves applied to every tenant-scoped table:
--   1. Rewrite each policy into the short-circuit-safe CASE form already proven
--      by 000027 / 000041:
--          USING / WITH CHECK (
--            CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
--                 THEN true                                -- GUC unset -> rely on WHERE
--                 ELSE tenant_id = current_setting('app.tenant_id', true)::uuid
--            END)
--      The empty->true arm is what makes step 2 non-breaking: any code path that
--      has not yet been migrated to set app.tenant_id keeps working (it falls to
--      its WHERE clause), and the ::uuid cast can never be handed an empty
--      string (the bug 000027/000041 fixed).
--   2. `FORCE ROW LEVEL SECURITY`, so the policy also binds the owner connection.
--
-- Net effect: where a request DOES set app.tenant_id (see middleware.TenantDB +
-- internal/adapter/repo/dbscope), the database itself now refuses cross-tenant
-- reads and writes. Where it does not, behaviour is unchanged. Special
-- visibility branches are preserved: event_outbox's 'service' identity,
-- unit_def.is_system, and nursery_dict's shared seed rows.
--
-- WITH CHECK is stated explicitly (not left to default to USING) so write
-- semantics are unambiguous: with app.tenant_id = A, inserting or updating a row
-- owned by tenant B is rejected.
--
-- Already-relaxed auth tables (tenant_profile, user_identity_mapping,
-- personal_access_token) are intentionally left untouched: they are already
-- FORCE + short-circuit safe and the auth chain depends on their relaxed
-- pre-tenant read path (migrations 000025/000027/000031).

SET search_path TO tally;

-- Part 1 -- strict tenant tables: canonical CASE (empty->true) USING + WITH CHECK
-- + FORCE. Every existing policy on the table is dropped first (policy names vary
-- by the migration that created them: e.g. product_rls, stock_lot_tenant_isolation,
-- the duplicate 1-arg tenant_isolation on warehouse from 000033) and replaced by a
-- single canonical tenant_isolation policy.
DO $$
DECLARE
    t   text;
    pol text;
    strict_tables text[] := ARRAY[
        'partner','product','warehouse','stock_snapshot','bill_head','bill_item',
        'payment_head','audit_log','system_config','bill_sequence','org_department',
        'stock_lot','stock_movement','exchange_rate','project','product_unit',
        'supplier','user_session','account_audit_log','user_profile'
    ];
BEGIN
    FOREACH t IN ARRAY strict_tables LOOP
        FOR pol IN
            SELECT policyname FROM pg_policies
            WHERE schemaname = 'tally' AND tablename = t
        LOOP
            EXECUTE format('DROP POLICY IF EXISTS %I ON tally.%I', pol, t);
        END LOOP;

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

-- Part 2 -- unit_def: keep cross-tenant visibility of seeded system units.
DROP POLICY IF EXISTS unit_def_rls ON unit_def;
DROP POLICY IF EXISTS tenant_isolation ON unit_def;
CREATE POLICY tenant_isolation ON unit_def
    USING (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                THEN true
                ELSE tenant_id = current_setting('app.tenant_id', true)::uuid OR is_system = true END)
    WITH CHECK (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                     THEN true
                     ELSE tenant_id = current_setting('app.tenant_id', true)::uuid OR is_system = true END);
ALTER TABLE unit_def FORCE ROW LEVEL SECURITY;

-- Part 3 -- nursery_dict: keep shared seed rows (nil-uuid tenant) visible.
DROP POLICY IF EXISTS nursery_dict_rls ON nursery_dict;
DROP POLICY IF EXISTS tenant_isolation ON nursery_dict;
CREATE POLICY tenant_isolation ON nursery_dict
    USING (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                THEN true
                ELSE tenant_id = current_setting('app.tenant_id', true)::uuid
                     OR tenant_id = '00000000-0000-0000-0000-000000000000'::uuid END)
    WITH CHECK (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                     THEN true
                     ELSE tenant_id = current_setting('app.tenant_id', true)::uuid
                          OR tenant_id = '00000000-0000-0000-0000-000000000000'::uuid END);
ALTER TABLE nursery_dict FORCE ROW LEVEL SECURITY;

-- Part 4 -- event_outbox: preserve the 'service' identity the drain worker sets.
DROP POLICY IF EXISTS event_outbox_isolation ON event_outbox;
DROP POLICY IF EXISTS tenant_isolation ON event_outbox;
CREATE POLICY tenant_isolation ON event_outbox
    USING (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                THEN true
                WHEN current_setting('app.tenant_id', true) = 'service'
                THEN true
                ELSE tenant_id::text = current_setting('app.tenant_id', true) END)
    WITH CHECK (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                     THEN true
                     WHEN current_setting('app.tenant_id', true) = 'service'
                     THEN true
                     ELSE tenant_id::text = current_setting('app.tenant_id', true) END);
ALTER TABLE event_outbox FORCE ROW LEVEL SECURITY;

-- Part 5 -- import dedup tables: already CASE-safe (000041); add the explicit
-- WITH CHECK + FORCE so the backstop also binds the owner connection here.
DO $$
DECLARE
    t text;
    import_tables text[] := ARRAY[
        'import_sku_map','import_order_seen','import_order_cancel_seen','import_refund_seen'
    ];
BEGIN
    FOREACH t IN ARRAY import_tables LOOP
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
