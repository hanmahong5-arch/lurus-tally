-- 000027_rls_relax_safe_short_circuit.down.sql
-- Restore the OR-pattern relaxed policies (migrations 25 + 26).

SET search_path TO tally;

DROP POLICY IF EXISTS tenant_profile_rls ON tenant_profile;
CREATE POLICY tenant_profile_rls ON tenant_profile
    USING (
        current_setting('app.tenant_id', true) IS NULL
        OR current_setting('app.tenant_id', true) = ''
        OR tenant_id = current_setting('app.tenant_id', true)::UUID
    );

DROP POLICY IF EXISTS uim_rls ON user_identity_mapping;
CREATE POLICY uim_rls ON user_identity_mapping
    USING (
        current_setting('app.tenant_id', true) IS NULL
        OR current_setting('app.tenant_id', true) = ''
        OR tenant_id = current_setting('app.tenant_id', true)::UUID
    );
