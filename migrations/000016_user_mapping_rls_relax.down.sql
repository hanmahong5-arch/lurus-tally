-- 000016_user_mapping_rls_relax.down.sql
-- Revert RLS on user_identity_mapping to strict tenant_id enforcement.

SET search_path TO tally;

DROP POLICY IF EXISTS uim_rls ON user_identity_mapping;

CREATE POLICY uim_rls ON user_identity_mapping
    USING (tenant_id = current_setting('app.tenant_id', true)::UUID);
