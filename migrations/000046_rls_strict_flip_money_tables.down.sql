-- 000046_rls_strict_flip_money_tables.down.sql
-- Revert the core product/money/stock tables to the non-breaking empty->true
-- CASE form (000042). unit_def keeps its is_system ELSE branch.

SET search_path TO tally;

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
                            THEN true
                            ELSE tenant_id = current_setting('app.tenant_id', true)::uuid END)
                WITH CHECK (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                                 THEN true
                                 ELSE tenant_id = current_setting('app.tenant_id', true)::uuid END)
        $q$, t);
    END LOOP;
END $$;

DROP POLICY IF EXISTS tenant_isolation ON unit_def;
CREATE POLICY tenant_isolation ON unit_def
    USING (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                THEN true
                ELSE tenant_id = current_setting('app.tenant_id', true)::uuid OR is_system = true END)
    WITH CHECK (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                     THEN true
                     ELSE tenant_id = current_setting('app.tenant_id', true)::uuid OR is_system = true END);
