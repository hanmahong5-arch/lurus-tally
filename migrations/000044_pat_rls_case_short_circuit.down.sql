-- 000044_pat_rls_case_short_circuit.down.sql
-- Revert pat_rls to the original (crash-prone) OR form from 000031.

SET search_path TO tally;

DROP POLICY IF EXISTS pat_rls ON personal_access_token;
CREATE POLICY pat_rls ON personal_access_token
    USING (
        current_setting('app.tenant_id', true) IS NULL
        OR current_setting('app.tenant_id', true) = ''
        OR tenant_id = current_setting('app.tenant_id', true)::UUID
    );
