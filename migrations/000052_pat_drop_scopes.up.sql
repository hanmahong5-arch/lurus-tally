-- 000052_pat_drop_scopes.up.sql
-- Hardening Wave 2 — drop the vestigial `scopes` column from personal_access_token.
--
-- Scopes were never enforced: the create API accepts only {name, expires_at}
-- and hardcoded scopes=['read'], the PAT resolver returned scopes that the auth
-- middleware discarded, and no HasScope/authorization check ever read them. The
-- only varied writer was bootstrap-stage.sh injecting ['read','write'] via raw
-- SQL. Removing the column (and its Go field/plumbing) collapses this to a
-- single execution path. No FK or index depends on scopes — idx_pat_tenant_active
-- is on tenant_id, so the DROP is non-blocking and metadata-only.

SET search_path TO tally;

ALTER TABLE tally.personal_access_token
    DROP COLUMN IF EXISTS scopes;
