-- 000051_tenant_platform_account_id.up.sql
-- Unified-billing Wave 2 — let Tally attribute LLM usage to a platform account.
--
-- lurus-platform owns wallets/subscriptions keyed by an int64 account_id
-- (resolved from a Zitadel sub). Tally is tenant-keyed (UUID) and the LLM hot
-- path (PAT-authenticated MCP / automation) carries no sub at all, so it cannot
-- resolve an account per request. We instead pin ONE account per tenant — the
-- bootstrap owner's — captured at onboarding time when ChooseProfile already
-- calls platform UpsertAccount and currently discards the returned id.
--
-- The usage reporter then maps tenant_id -> platform_account_id with a cached
-- local read (no hot-path round-trip). NULL means "not yet provisioned"
-- (platform was down at onboarding, or pre-Wave-2 tenants) — the reporter skips
-- such events in shadow mode rather than guessing.
--
-- No RLS: tally.tenant is the tenant *registry* (keyed by id, no tenant_id
-- column) and is intentionally outside the 11 tenant-scoped RLS tables, so the
-- reporter's background goroutine can read it without a request-scoped pin.

SET search_path TO tally;

ALTER TABLE tally.tenant
    ADD COLUMN IF NOT EXISTS platform_account_id BIGINT;

COMMENT ON COLUMN tally.tenant.platform_account_id IS
    'lurus-platform account id (int64) of the bootstrap owner; NULL until provisioned. Source: ChooseProfile UpsertAccount. Used by the LLM usage reporter (unified-billing Wave 2).';
