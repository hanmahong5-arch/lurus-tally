-- 000050_replenish_suggestion_log.up.sql
-- Suggestion result ledger for the replenishment loop ("show your track record").
--
-- One row per (tenant, product, suggestion day). The GET /replenish/suggestions
-- read path upserts today's snapshot (only rows with suggested_qty > 0); the
-- draft-batch write path stamps adopted_at/adopted_bill_id when the user turns
-- a suggestion into a PO draft. The scorecard reads adoption rate and
-- "suggested, ignored, and now stocked out" misses from this table.
--
-- Mutability contract: a row that has been adopted is immutable — the daily
-- upsert refreshes quantities only WHERE adopted_at IS NULL, so the ledger
-- keeps the numbers the user actually acted on.

SET search_path TO tally;

CREATE TABLE replenish_suggestion_log (
    id               UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID         NOT NULL,
    product_id       UUID         NOT NULL REFERENCES tally.product(id),
    suggested_on     DATE         NOT NULL,
    suggested_qty    NUMERIC(18,4) NOT NULL,
    available_qty    NUMERIC(18,4) NOT NULL DEFAULT 0,
    avg_daily_sales  NUMERIC(18,4) NOT NULL DEFAULT 0,
    lead_time_days   NUMERIC(8,2) NOT NULL DEFAULT 0,
    lead_time_source VARCHAR(16)  NOT NULL DEFAULT 'default'
        CHECK (lead_time_source IN ('learned', 'configured', 'default')),
    days_of_supply   NUMERIC(18,4) NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    adopted_at       TIMESTAMPTZ,
    adopted_bill_id  UUID         REFERENCES tally.bill_head(id),
    CONSTRAINT uq_suggestion_log_day UNIQUE (tenant_id, product_id, suggested_on)
);

-- Scorecard scans are windowed by day per tenant.
CREATE INDEX idx_suggestion_log_tenant_day
    ON replenish_suggestion_log (tenant_id, suggested_on DESC);

-- RLS: strict canonical CASE (same shape as 000045-000047). Both writers run
-- inside pinned /api/v1 requests (middleware.TenantDB), so empty GUC -> false
-- (fail closed) is correct; no webhook/background path touches this table.
ALTER TABLE replenish_suggestion_log ENABLE ROW LEVEL SECURITY;
ALTER TABLE replenish_suggestion_log FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON replenish_suggestion_log
    USING (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                THEN false
                ELSE tenant_id = current_setting('app.tenant_id', true)::uuid END)
    WITH CHECK (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                     THEN false
                     ELSE tenant_id = current_setting('app.tenant_id', true)::uuid END);
