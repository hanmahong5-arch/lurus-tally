-- 000048_unit_def_writecheck_no_system.down.sql
-- Restore the 000046 unit_def policy whose WITH CHECK also accepted is_system rows
-- (re-opening the cross-tenant write hole). FORCE is left intact.

SET search_path TO tally;

DROP POLICY IF EXISTS tenant_isolation ON unit_def;
CREATE POLICY tenant_isolation ON unit_def
    USING (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                THEN false
                ELSE tenant_id = current_setting('app.tenant_id', true)::uuid OR is_system = true END)
    WITH CHECK (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                     THEN false
                     ELSE tenant_id = current_setting('app.tenant_id', true)::uuid OR is_system = true END);
