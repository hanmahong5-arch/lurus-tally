-- 000032_onboarding_drafts_and_is_sample.up.sql
-- Onboarding v2 phase α.0: foundation for the 5-step wizard + sample-data sandbox.
--
-- Adds:
--   1. tally.onboarding_drafts  — per-user step cache so wizard is resumable
--                                 across browser close / multi-tab / device switch
--   2. is_sample BOOL on warehouse + partner — flags rows that Bootstrap auto-seeded
--                                              so the dashboard can offer "clear
--                                              sample data" once user starts real work
--
-- Rationale (Onboarding v2 design, 3-agent research synthesized 2026-05-19):
--   - Server-side draft is hard requirement (Stripe Connect pattern + multi-tab safe)
--   - is_sample lets us run reverse onboarding (pre-populated dashboard) without
--     muddying real data — same trick as Superhuman synthetic inbox / Katana free-tier
--     SKU limit. User can keep playing with seed rows OR one-click discard them.
--
-- NOT in this migration (deferred to later phases per phased rollout):
--   - is_sample on bill_head / product → added when α.2 ships sample bills
--   - RLS policy on onboarding_drafts → see below; relaxed by design

SET search_path TO tally;

-- 1. onboarding_drafts — single row per (user_sub) holding the latest step + payload.
--    UPSERT pattern; deleted on step-5 commit.
--    user_sub is the Zitadel sub claim (string, not UUID) because at draft time the
--    user may not yet have a tenant_id row.
CREATE TABLE IF NOT EXISTS onboarding_drafts (
    user_sub    VARCHAR(255) PRIMARY KEY,
    step        SMALLINT     NOT NULL CHECK (step BETWEEN 1 AND 5),
    payload     JSONB        NOT NULL DEFAULT '{}'::jsonb,
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- RLS note: onboarding_drafts is INTENTIONALLY NOT row-level-secured.
-- Reason: at draft time the user has no tenant_id yet (it's created on step-5
-- commit), so app.tenant_id can't be SET. The authentication layer enforces
-- the only access control needed: user_sub from JWT must match the row's
-- user_sub. Same pattern as migration 000025 (user_mapping RLS relax) /
-- 000026 (tenant_profile RLS relax) for the same pre-tenant chicken-and-egg.

CREATE INDEX IF NOT EXISTS idx_onboarding_drafts_updated
    ON onboarding_drafts(updated_at);

-- 2. is_sample flag on warehouse + partner.
--    Default false so all existing rows (including the demo seeds inserted before
--    this migration ran) stay "real". Bootstrap will start setting true on new
--    seeds after the matching bootstrap.go change ships.
--    A separate backfill migration (027 follow-up) will retroactively mark seed
--    rows from earlier tenants once we can identify them by name/created_at
--    correlation — that's a P0 follow-up captured in the audit.
ALTER TABLE warehouse ADD COLUMN IF NOT EXISTS is_sample BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE partner   ADD COLUMN IF NOT EXISTS is_sample BOOLEAN NOT NULL DEFAULT false;

-- Partial index speeds up the "clear sample data" DELETE which is one-shot per tenant.
CREATE INDEX IF NOT EXISTS idx_warehouse_sample
    ON warehouse(tenant_id) WHERE is_sample = true;
CREATE INDEX IF NOT EXISTS idx_partner_sample
    ON partner(tenant_id)   WHERE is_sample = true;
