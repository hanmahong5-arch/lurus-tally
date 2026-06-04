-- 000047_rls_strict_flip_child_tables.down.sql
-- Revert the Phase-3 final flip: restore the non-breaking empty->true arm on the
-- 12 tenant tables flipped by the up migration. FORCE is left intact (it was set
-- by 000042 / 000043, not by 000047). This returns each table to the canonical
-- plain tenant_id CASE form 000042 / 000043 established.

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
                            THEN true
                            ELSE tenant_id = current_setting('app.tenant_id', true)::uuid END)
                WITH CHECK (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                                 THEN true
                                 ELSE tenant_id = current_setting('app.tenant_id', true)::uuid END)
        $q$, t);
    END LOOP;
END $$;
