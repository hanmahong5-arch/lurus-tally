-- 000043_subtable_rls_force_case.down.sql
-- Reverse 000043: these child tables had NO row-level security before this
-- migration, so the clean reversal is to drop the policy it added and disable
-- RLS entirely. DISABLE ROW LEVEL SECURITY also clears the FORCE flag, returning
-- each table to its pre-043 state (isolation relying purely on each query's
-- WHERE tenant_id = $N).
--
-- dict_type / dict_data were never touched by the up migration and are not
-- touched here.

SET search_path TO tally;

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
        EXECUTE format('DROP POLICY IF EXISTS tenant_isolation ON tally.%I', t);
        EXECUTE format('ALTER TABLE tally.%I DISABLE ROW LEVEL SECURITY', t);
    END LOOP;
END $$;
