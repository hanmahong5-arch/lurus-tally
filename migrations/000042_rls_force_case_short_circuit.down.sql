-- 000042_rls_force_case_short_circuit.down.sql
-- Reverse 000042: restore each table's pre-042 policy (name + definition) and
-- drop the FORCE flag this migration added. Reversing logically reintroduces the
-- prior crash-prone / owner-bypassed forms -- that is what "undo this migration"
-- means; the bugs were fixed BY 000042.
--
-- exchange_rate keeps FORCE (it was FORCE before 000042, from 000024).
-- tenant_profile / user_identity_mapping / personal_access_token are not touched
-- by the up migration, so they are not touched here.

SET search_path TO tally;

-- Strict tables that originally carried a single 2-arg current_setting policy.
DO $$
DECLARE
    rec record;
BEGIN
    FOR rec IN
        SELECT * FROM (VALUES
            ('partner','partner_rls'),
            ('product','product_rls'),
            ('warehouse','warehouse_rls'),
            ('stock_snapshot','stock_snapshot_rls'),
            ('bill_head','bill_head_rls'),
            ('bill_item','bill_item_rls'),
            ('payment_head','payment_head_rls'),
            ('audit_log','audit_log_rls'),
            ('system_config','system_config_rls'),
            ('bill_sequence','bill_sequence_rls'),
            ('org_department','org_department_rls'),
            ('stock_lot','stock_lot_tenant_isolation'),
            ('stock_movement','stock_movement_tenant_isolation'),
            ('project','project_rls'),
            ('product_unit','product_unit_rls')
        ) AS v(tbl, pol)
    LOOP
        EXECUTE format('DROP POLICY IF EXISTS tenant_isolation ON tally.%I', rec.tbl);
        EXECUTE format(
            'CREATE POLICY %I ON tally.%I USING (tenant_id = current_setting(''app.tenant_id'', true)::uuid)',
            rec.pol, rec.tbl);
        EXECUTE format('ALTER TABLE tally.%I NO FORCE ROW LEVEL SECURITY', rec.tbl);
    END LOOP;
END $$;

-- exchange_rate: restore the strict policy but KEEP FORCE (pre-042 state).
DROP POLICY IF EXISTS tenant_isolation ON exchange_rate;
CREATE POLICY exchange_rate_rls ON exchange_rate
    USING (tenant_id = current_setting('app.tenant_id', true)::uuid);

-- supplier + account tables: restore the original single-arg current_setting form.
DROP POLICY IF EXISTS tenant_isolation ON supplier;
CREATE POLICY tenant_isolation ON supplier
    USING (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE supplier NO FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON user_session;
CREATE POLICY tenant_isolation ON user_session
    USING (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE user_session NO FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON account_audit_log;
CREATE POLICY tenant_isolation ON account_audit_log
    USING (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE account_audit_log NO FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON user_profile;
CREATE POLICY tenant_isolation ON user_profile
    USING (tenant_id = current_setting('app.tenant_id')::uuid);
ALTER TABLE user_profile NO FORCE ROW LEVEL SECURITY;

-- unit_def: restore is_system cross-tenant visibility (2-arg form).
DROP POLICY IF EXISTS tenant_isolation ON unit_def;
CREATE POLICY unit_def_rls ON unit_def
    USING (tenant_id = current_setting('app.tenant_id', true)::uuid OR is_system = true);
ALTER TABLE unit_def NO FORCE ROW LEVEL SECURITY;

-- nursery_dict: restore shared seed visibility.
DROP POLICY IF EXISTS tenant_isolation ON nursery_dict;
CREATE POLICY nursery_dict_rls ON nursery_dict
    USING (tenant_id = current_setting('app.tenant_id', true)::uuid
           OR tenant_id = '00000000-0000-0000-0000-000000000000'::uuid);
ALTER TABLE nursery_dict NO FORCE ROW LEVEL SECURITY;

-- event_outbox: restore the original service-or-tenant policy.
DROP POLICY IF EXISTS tenant_isolation ON event_outbox;
CREATE POLICY event_outbox_isolation ON event_outbox
    USING (current_setting('app.tenant_id', true) = 'service'
           OR tenant_id::text = current_setting('app.tenant_id', true));
ALTER TABLE event_outbox NO FORCE ROW LEVEL SECURITY;

-- import dedup tables: restore the 000041 CASE form, drop FORCE.
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
        $q$, t);
        EXECUTE format('ALTER TABLE tally.%I NO FORCE ROW LEVEL SECURITY', t);
    END LOOP;
END $$;
