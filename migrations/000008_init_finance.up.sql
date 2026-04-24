-- 000008_init_finance.up.sql
-- Domain 7: finance_* — fund accounts, payment head/items, finance categories.
-- Derived from jshERP jsh_account* + jsh_in_out_item (Apache-2.0)

-- Derived from jshERP jsh_account (Apache-2.0)
CREATE TABLE IF NOT EXISTS tally.finance_account (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID NOT NULL,
    name             VARCHAR(100) NOT NULL,
    code             VARCHAR(50),
    initial_balance  NUMERIC(18,4) NOT NULL DEFAULT 0,
    current_balance  NUMERIC(18,4) NOT NULL DEFAULT 0,
    is_default       BOOLEAN DEFAULT false,
    enabled          BOOLEAN DEFAULT true,
    sort             INT DEFAULT 0,
    remark           VARCHAR(200),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);

-- Derived from jshERP jsh_account_head (Apache-2.0)
CREATE TABLE IF NOT EXISTS tally.payment_head (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID NOT NULL,
    pay_type         VARCHAR(30) NOT NULL,
    partner_id       UUID REFERENCES tally.partner(id),
    operator_id      UUID,
    creator_id       UUID NOT NULL,
    bill_no          VARCHAR(50),
    pay_date         TIMESTAMPTZ NOT NULL,
    amount           NUMERIC(18,4) NOT NULL,
    discount_amount  NUMERIC(18,4) DEFAULT 0,
    total_amount     NUMERIC(18,4) NOT NULL,
    account_id       UUID REFERENCES tally.finance_account(id),
    related_bill_id  UUID REFERENCES tally.bill_head(id),
    remark           TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_payment_head_tenant ON tally.payment_head(tenant_id) WHERE deleted_at IS NULL;

-- Derived from jshERP jsh_account_item (Apache-2.0)
CREATE TABLE IF NOT EXISTS tally.payment_item (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL,
    head_id             UUID NOT NULL REFERENCES tally.payment_head(id),
    finance_category_id UUID,
    amount              NUMERIC(18,4) NOT NULL,
    remark              VARCHAR(500)
);

-- Derived from jshERP jsh_in_out_item (Apache-2.0)
CREATE TABLE IF NOT EXISTS tally.finance_category (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    name      VARCHAR(100) NOT NULL,
    cat_type  VARCHAR(20) NOT NULL CHECK (cat_type IN ('income','expense')),
    enabled   BOOLEAN DEFAULT true,
    sort      INT DEFAULT 0
);
