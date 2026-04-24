-- 000013_tenant_profile.up.sql
-- Creates tenant_profile (one profile per tenant) and user_identity_mapping
-- (maps Zitadel OIDC sub to tally user/tenant). Both tables require RLS.

SET search_path TO tally;

-- tenant_profile: stores the chosen profile type for each tenant.
-- UNIQUE(tenant_id) enforced by PRIMARY KEY; one row per tenant.
CREATE TABLE tenant_profile (
    id                UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID         NOT NULL UNIQUE REFERENCES tenant(id) ON DELETE CASCADE,
    profile_type      VARCHAR(20)  NOT NULL CHECK (profile_type IN ('cross_border', 'retail', 'hybrid')),
    inventory_method  VARCHAR(20)  NOT NULL DEFAULT 'wac'
                                   CHECK (inventory_method IN ('fifo', 'wac', 'by_weight', 'batch', 'bulk_merged')),
    custom_overrides  JSONB        NOT NULL DEFAULT '{}'::jsonb,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX idx_tenant_profile_tenant ON tenant_profile (tenant_id);

ALTER TABLE tenant_profile ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_profile FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_profile_rls ON tenant_profile
    USING (tenant_id = current_setting('app.tenant_id', true)::UUID);

-- user_identity_mapping: maps Zitadel OIDC 'sub' claim to an internal user/tenant.
CREATE TABLE user_identity_mapping (
    id            UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID         NOT NULL REFERENCES tenant(id) ON DELETE CASCADE,
    zitadel_sub   VARCHAR(255) NOT NULL UNIQUE,
    email         VARCHAR(255) NOT NULL,
    display_name  VARCHAR(255),
    role          VARCHAR(20)  NOT NULL DEFAULT 'admin'
                               CHECK (role IN ('admin', 'manager', 'clerk', 'viewer')),
    is_owner      BOOLEAN      NOT NULL DEFAULT false,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX uim_tenant_idx ON user_identity_mapping (tenant_id);
CREATE INDEX uim_email_idx  ON user_identity_mapping (email);

ALTER TABLE user_identity_mapping ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_identity_mapping FORCE ROW LEVEL SECURITY;
CREATE POLICY uim_rls ON user_identity_mapping
    USING (tenant_id = current_setting('app.tenant_id', true)::UUID);
