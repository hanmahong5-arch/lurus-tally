-- 000059_tenant_platform_org_and_member_roles.up.sql
-- Multi-member shops: membership truth lives in lurus-platform (identity.org_members),
-- Tally keeps the shop-level role and a local projection.
--
--   1. tenant.platform_org_id          - the platform organization (identity.organizations.id, BIGINT) behind a shop; NULL until provisioned/backfilled.
--   2. user_identity_mapping.role      - widened to owner|manager|clerk|cashier|finance. The legacy
--                                        values admin/viewer stay allowed so a rolling deploy (old pods still
--                                        inserting 'admin') never trips the CHECK; code normalises admin->manager,
--                                        viewer->clerk. Existing shop owners are backfilled to 'owner'.
--   3. user_identity_mapping.platform_account_id - platform account of the member, needed to map a platform
--                                        member event / account_id back to the local row.
--   4. dev_org / dev_account / dev_org_member / dev_org_invite - backing store of the dev-only local stand-in for the platform
--                                        organization API (TALLY_DEV_MODE only; empty and unused elsewhere).
-- zitadel_sub stays UNIQUE: phase 1 is one person, one shop.
SET search_path TO tally;

ALTER TABLE tally.tenant
    ADD COLUMN IF NOT EXISTS platform_org_id BIGINT;

COMMENT ON COLUMN tally.tenant.platform_org_id IS
    'lurus-platform organization id (identity.organizations.id, bigint) backing this shop (members + roles); NULL until provisioned. Source: ChooseProfile / team lazy backfill.';

CREATE UNIQUE INDEX IF NOT EXISTS tenant_platform_org_id_uidx
    ON tally.tenant (platform_org_id) WHERE platform_org_id IS NOT NULL;

ALTER TABLE tally.user_identity_mapping
    ADD COLUMN IF NOT EXISTS platform_account_id BIGINT;

CREATE INDEX IF NOT EXISTS uim_tenant_account_idx
    ON tally.user_identity_mapping (tenant_id, platform_account_id);

ALTER TABLE tally.user_identity_mapping DROP CONSTRAINT IF EXISTS user_identity_mapping_role_check;
ALTER TABLE tally.user_identity_mapping ADD CONSTRAINT user_identity_mapping_role_check
    CHECK (role IN ('owner', 'manager', 'clerk', 'cashier', 'finance', 'admin', 'viewer'));

UPDATE tally.user_identity_mapping SET role = 'owner' WHERE is_owner = true AND role <> 'owner';

-- Dev-only stand-in for platform organizations. No RLS: never read by request-scoped code.
CREATE TABLE IF NOT EXISTS tally.dev_account (
    account_id   BIGINT       PRIMARY KEY,
    idp_subject  VARCHAR(255) NOT NULL UNIQUE,
    email        VARCHAR(255) NOT NULL DEFAULT '',
    display_name VARCHAR(255) NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS tally.dev_org_member (
    org_id        BIGINT       NOT NULL,
    account_id    BIGINT       NOT NULL,
    role          VARCHAR(16)  NOT NULL CHECK (role IN ('owner', 'admin', 'member')),
    product_roles JSONB        NOT NULL DEFAULT '{}'::jsonb,
    joined_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, account_id)
);

CREATE TABLE IF NOT EXISTS tally.dev_org_invite (
    id                      UUID        PRIMARY KEY,
    org_id                  BIGINT      NOT NULL,
    token_hash              TEXT        NOT NULL UNIQUE,
    role                    VARCHAR(16) NOT NULL CHECK (role IN ('admin', 'member')),
    product_roles           JSONB       NOT NULL DEFAULT '{}'::jsonb,
    invited_by_account_id   BIGINT      NOT NULL,
    expires_at              TIMESTAMPTZ NOT NULL,
    accepted_by_account_id  BIGINT,
    accepted_at             TIMESTAMPTZ,
    revoked_at              TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS tally.dev_org (
    id           BIGSERIAL    PRIMARY KEY,
    product      VARCHAR(32)  NOT NULL,
    external_ref VARCHAR(128) NOT NULL,
    name         VARCHAR(255) NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (product, external_ref)
);
