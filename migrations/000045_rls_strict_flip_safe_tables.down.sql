-- 000045_rls_strict_flip_safe_tables.down.sql
-- Revert supplier + project to the non-breaking empty->true CASE form (000042).

SET search_path TO tally;

DO $$
DECLARE
    t text;
    flip_tables text[] := ARRAY['supplier','project'];
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
