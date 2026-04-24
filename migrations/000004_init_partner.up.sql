-- 000004_init_partner.up.sql
-- Domain 3: partner_* — suppliers, customers, and their bank accounts.
-- Derived from jshERP jsh_supplier (Apache-2.0)

CREATE TABLE IF NOT EXISTS tally.partner (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID NOT NULL,
    partner_type     VARCHAR(20) NOT NULL CHECK (partner_type IN ('supplier','customer','both','member')),
    name             VARCHAR(255) NOT NULL,
    code             VARCHAR(100),
    contact_name     VARCHAR(100),
    phone            VARCHAR(30),
    mobile           VARCHAR(30),
    email            VARCHAR(100),
    address          TEXT,
    tax_no           VARCHAR(100),
    default_tax_rate NUMERIC(8,4),
    credit_limit     NUMERIC(18,4),
    advance_balance  NUMERIC(18,4) NOT NULL DEFAULT 0,
    ar_balance       NUMERIC(18,4) NOT NULL DEFAULT 0,
    ap_balance       NUMERIC(18,4) NOT NULL DEFAULT 0,
    enabled          BOOLEAN NOT NULL DEFAULT true,
    remark           TEXT,
    ai_metadata      JSONB NOT NULL DEFAULT '{}',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_partner_tenant ON tally.partner(tenant_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_partner_code ON tally.partner(tenant_id, code)
    WHERE deleted_at IS NULL AND code IS NOT NULL;

CREATE TABLE IF NOT EXISTS tally.partner_bank (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL,
    partner_id   UUID NOT NULL REFERENCES tally.partner(id),
    bank_name    VARCHAR(100),
    account_no   VARCHAR(100),
    account_name VARCHAR(100),
    is_default   BOOLEAN DEFAULT false
);
