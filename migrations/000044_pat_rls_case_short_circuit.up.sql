-- 000044_pat_rls_case_short_circuit.up.sql
-- Fix the last crash-prone OR-form RLS policy: personal_access_token.
--
-- 000031 created pat_rls with the OR pattern:
--   current_setting('app.tenant_id', true) IS NULL
--     OR current_setting('app.tenant_id', true) = ''
--     OR tenant_id = current_setting('app.tenant_id', true)::UUID
-- Postgres does NOT guarantee short-circuit evaluation of OR, so when
-- app.tenant_id is the empty string '' (a state a pooled connection can carry),
-- the planner may evaluate the third disjunct's cast ''::uuid and raise
--   ERROR: invalid input syntax for type uuid: "" (SQLSTATE 22P02)
-- The PAT prefix lookup runs in the auth middleware BEFORE the tenant is known
-- (on a shared pool connection), so this intermittently rejects a VALID token
-- with 401 "invalid or expired token" — a real, hard-to-reproduce auth outage.
-- Found by the non-superuser app-boot e2e (tests/integration/rls_e2e_test.go).
--
-- 000027 (tenant_profile / user_identity_mapping) and 000041 (import_*) already
-- converted this class to the guaranteed-short-circuit CASE form; pat_rls was
-- the last OR holdout (000031 predates the lesson). Convert it, preserving the
-- relaxed empty->true branch the pre-tenant prefix lookup depends on, and add an
-- explicit WITH CHECK so PAT writes are tenant-scoped once app.tenant_id is set.
-- FORCE ROW LEVEL SECURITY (set by 000031) is left in place.

SET search_path TO tally;

DROP POLICY IF EXISTS pat_rls ON personal_access_token;
CREATE POLICY pat_rls ON personal_access_token
    USING (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                THEN true
                ELSE tenant_id = current_setting('app.tenant_id', true)::uuid END)
    WITH CHECK (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                     THEN true
                     ELSE tenant_id = current_setting('app.tenant_id', true)::uuid END);
