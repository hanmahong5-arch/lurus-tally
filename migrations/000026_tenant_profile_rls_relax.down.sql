-- 000026_tenant_profile_rls_relax.down.sql
-- Restore the strict tenant_profile RLS policy from migration 13.

SET search_path TO tally;

DROP POLICY IF EXISTS tenant_profile_rls ON tenant_profile;

CREATE POLICY tenant_profile_rls ON tenant_profile
    USING (tenant_id = current_setting('app.tenant_id', true)::UUID);
