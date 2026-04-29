-- 000027_rls_relax_safe_short_circuit.up.sql
-- Replace the OR-pattern relaxed RLS policies on tenant_profile and
-- user_identity_mapping with CASE-pattern equivalents.
--
-- Why: Postgres does NOT guarantee short-circuit evaluation of `OR`. The
-- planner is free to evaluate every disjunct, including
-- `current_setting('app.tenant_id', true)::UUID`. When the GUC is the empty
-- string `''` (which is what `set_config(..., '', true)` leaves behind after
-- a transaction in some pool states, and what `RESET` does NOT produce but
-- a stale pooled connection might), the cast raises:
--
--   ERROR: invalid input syntax for type uuid: "" (SQLSTATE 22P02)
--
-- `CASE WHEN ... THEN ... ELSE ...` IS guaranteed short-circuit per the SQL
-- standard. So we gate the cast behind a safety check.

SET search_path TO tally;

DROP POLICY IF EXISTS tenant_profile_rls ON tenant_profile;
CREATE POLICY tenant_profile_rls ON tenant_profile
    USING (
        CASE
            WHEN COALESCE(current_setting('app.tenant_id', true), '') = '' THEN true
            ELSE tenant_id = current_setting('app.tenant_id', true)::UUID
        END
    );

DROP POLICY IF EXISTS uim_rls ON user_identity_mapping;
CREATE POLICY uim_rls ON user_identity_mapping
    USING (
        CASE
            WHEN COALESCE(current_setting('app.tenant_id', true), '') = '' THEN true
            ELSE tenant_id = current_setting('app.tenant_id', true)::UUID
        END
    );
