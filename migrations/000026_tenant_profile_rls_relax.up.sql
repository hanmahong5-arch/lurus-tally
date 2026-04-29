-- 000026_tenant_profile_rls_relax.up.sql
-- Relax RLS on tenant_profile for the same reason as migration 25 did for
-- user_identity_mapping: the auth chain (sub → mapping → tenant_id → profile)
-- needs to read tenant_profile BEFORE app.tenant_id is set on the session.
--
-- Without this change, every /api/v1/me call (and the post-bootstrap fetch in
-- ChooseProfileUseCase) fails with `invalid input syntax for type uuid: ""`
-- because the policy casts an empty current_setting('app.tenant_id', true)
-- to UUID.
--
-- Security note: callers always supply tenant_id in the WHERE clause
-- (`WHERE tenant_id = $1`); the only query path is by primary key. Allowing
-- reads when app.tenant_id is unset exposes no rows the caller didn't already
-- ask for. Once tenant_id is determined and propagated through subsequent
-- queries (with app.tenant_id set), tenant isolation resumes for all other
-- tenant-scoped tables.

SET search_path TO tally;

DROP POLICY IF EXISTS tenant_profile_rls ON tenant_profile;

CREATE POLICY tenant_profile_rls ON tenant_profile
    USING (
        current_setting('app.tenant_id', true) IS NULL
        OR current_setting('app.tenant_id', true) = ''
        OR tenant_id = current_setting('app.tenant_id', true)::UUID
    );
