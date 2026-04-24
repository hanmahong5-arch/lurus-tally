-- Migration 000024: currency table, exchange_rate table, bill_head currency columns
SET search_path TO tally;

-- Currency reference table (global, no tenant_id, no RLS)
CREATE TABLE currency (
    code    VARCHAR(10) PRIMARY KEY,
    name    VARCHAR(100) NOT NULL,
    symbol  VARCHAR(10),
    enabled BOOLEAN NOT NULL DEFAULT true
);

INSERT INTO currency (code, name, symbol, enabled) VALUES
    ('CNY', '人民币',  '¥',    true),
    ('USD', '美元',    '$',    true),
    ('EUR', '欧元',    '€',    true),
    ('GBP', '英镑',    '£',    true),
    ('JPY', '日元',    '¥',    true),
    ('HKD', '港币',    'HK$',  true);

-- Exchange rate table (per-tenant, RLS-protected)
CREATE TABLE exchange_rate (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID        NOT NULL,
    from_currency VARCHAR(10) NOT NULL REFERENCES currency(code),
    to_currency   VARCHAR(10) NOT NULL REFERENCES currency(code),
    rate          NUMERIC(20,8) NOT NULL CHECK (rate > 0),
    source        VARCHAR(50) NOT NULL DEFAULT 'manual',
    effective_at  TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Unique index: one rate per tenant/pair/day (used for ON CONFLICT upsert)
CREATE UNIQUE INDEX idx_exchange_rate_pair_date
    ON exchange_rate(tenant_id, from_currency, to_currency, effective_at);

-- Lookup index for GetRateOn (DESC to hit LIMIT 1 efficiently)
CREATE INDEX idx_exchange_rate_lookup
    ON exchange_rate(tenant_id, from_currency, to_currency, effective_at DESC);

ALTER TABLE exchange_rate ENABLE ROW LEVEL SECURITY;
ALTER TABLE exchange_rate FORCE ROW LEVEL SECURITY;

CREATE POLICY exchange_rate_rls ON exchange_rate
    USING (tenant_id = current_setting('app.tenant_id', true)::UUID);

-- Add currency columns to bill_head (nullable, backward-compatible)
ALTER TABLE bill_head
    ADD COLUMN IF NOT EXISTS currency      VARCHAR(10)   DEFAULT 'CNY' REFERENCES currency(code),
    ADD COLUMN IF NOT EXISTS exchange_rate NUMERIC(20,8) DEFAULT 1,
    ADD COLUMN IF NOT EXISTS amount_local  NUMERIC(18,4);

-- Add default_currency to partner if the table exists
DO $$ BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = 'tally' AND table_name = 'partner'
    ) THEN
        ALTER TABLE partner
            ADD COLUMN IF NOT EXISTS default_currency VARCHAR(10) DEFAULT 'CNY'
            REFERENCES currency(code);
    END IF;
END $$;
