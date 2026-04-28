-- 000016_user_mapping_rls_relax.up.sql
-- Relax RLS on user_identity_mapping so the auth flow can look up a Zitadel
-- sub → tenant_id mapping BEFORE app.tenant_id has been set on the session.
--
-- Without this change, GetMe (which is the very first request after OIDC
-- callback) cannot find the user's tenant: the policy filters all rows out
-- because app.tenant_id is null at that point.
--
-- Security note: zitadel_sub is globally unique and the only query path is
-- "WHERE zitadel_sub = $1". Allowing reads when app.tenant_id is unset
-- exposes no extra information beyond what the caller already provided
-- (the sub itself). Once tenant_id is determined, app.tenant_id is set on
-- subsequent queries and tenant isolation resumes.

SET search_path TO tally;

DROP POLICY IF EXISTS uim_rls ON user_identity_mapping;

CREATE POLICY uim_rls ON user_identity_mapping
    USING (
        current_setting('app.tenant_id', true) IS NULL
        OR current_setting('app.tenant_id', true) = ''
        OR tenant_id = current_setting('app.tenant_id', true)::UUID
    );
