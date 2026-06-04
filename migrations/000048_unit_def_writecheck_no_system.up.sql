-- 000048_unit_def_writecheck_no_system.up.sql
-- Close a cross-tenant WRITE hole in the unit_def RLS policy.
--
-- unit_def is shared: system units (is_system=true, tenant_id=000...0, seeded by
-- migration 000014) are READABLE by every tenant via the USING `OR is_system=true`
-- arm. But 000046's WITH CHECK carried the SAME `OR is_system=true` arm, so a
-- tenant connection (app.tenant_id set) could INSERT/UPDATE a row with
-- is_system=true and have it pass the check -- that row then becomes visible to
-- ALL tenants through the USING arm. That is cross-tenant data injection: one
-- tenant can publish a globally-visible unit.
--
-- The app never does this (unit create.go hardcodes is_system=false; there is no
-- unit-update path; delete blocks is_system rows), so this is defence-in-depth --
-- exactly what the RLS backstop exists to provide: the DB must not rely on the
-- app staying correct.
--
-- Fix: keep the READ arm unchanged (tenants still SEE system units) but tighten
-- WITH CHECK so a tenant may only write rows scoped to ITS OWN tenant_id AND
-- is_system=false. System units keep being seeded at migration time (before FORCE
-- and this policy exist), so seeding is unaffected.

SET search_path TO tally;

DROP POLICY IF EXISTS tenant_isolation ON unit_def;
CREATE POLICY tenant_isolation ON unit_def
    USING (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                THEN false
                ELSE tenant_id = current_setting('app.tenant_id', true)::uuid OR is_system = true END)
    WITH CHECK (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                     THEN false
                     ELSE tenant_id = current_setting('app.tenant_id', true)::uuid AND is_system = false END);
