-- 000022_stock_upgrade.up.sql
-- Upgrades stock subsystem: adds cost tracking to stock_snapshot + stock_lot,
-- adds warehouse_id to stock_lot for FIFO multi-warehouse support,
-- and creates the append-only stock_movement event log table.
-- Part of Epic 5 — Warehouse & Inventory Foundation.

SET search_path TO tally;

-- 1. Upgrade stock_snapshot: add unit_cost + cost_strategy columns.
--    avg_cost_price is retained for backwards compatibility; new code reads/writes unit_cost.
ALTER TABLE stock_snapshot
    ADD COLUMN unit_cost      NUMERIC(18,6) NOT NULL DEFAULT 0,
    ADD COLUMN cost_strategy  VARCHAR(20)   NOT NULL DEFAULT 'wac'
        CHECK (cost_strategy IN ('fifo', 'wac'));

-- 2. Upgrade stock_lot: add FIFO-required columns.
--    warehouse_id scopes lots per warehouse (required for multi-warehouse FIFO correctness).
--    qty_remaining tracks unconsumed quantity for FIFO drain.
--    received_at drives FIFO ordering (oldest first).
--    source_movement_id links back to the movement that created this lot.
ALTER TABLE stock_lot
    ADD COLUMN warehouse_id       UUID          REFERENCES warehouse(id) ON DELETE RESTRICT,
    ADD COLUMN qty_remaining      NUMERIC(18,4) NOT NULL DEFAULT 0,
    ADD COLUMN unit_cost          NUMERIC(18,6) NOT NULL DEFAULT 0,
    ADD COLUMN received_at        TIMESTAMPTZ   NOT NULL DEFAULT now(),
    ADD COLUMN source_movement_id UUID;

-- Backfill: existing lots should have qty_remaining = qty.
UPDATE stock_lot SET qty_remaining = qty WHERE qty_remaining = 0 AND qty > 0;

-- FIFO drain index: pick oldest lots with remaining stock efficiently.
CREATE INDEX stock_lot_fifo_idx
    ON stock_lot (tenant_id, product_id, warehouse_id, received_at)
    WHERE qty_remaining > 0;

-- Enable RLS on stock_lot (was missing in 000006).
ALTER TABLE stock_lot ENABLE ROW LEVEL SECURITY;
CREATE POLICY stock_lot_tenant_isolation ON stock_lot
    USING (tenant_id = current_setting('app.tenant_id', true)::UUID);

-- 3. Create stock_movement: append-only event log. Never UPDATE/DELETE rows.
CREATE TABLE stock_movement (
    id              UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID          NOT NULL,
    product_id      UUID          NOT NULL REFERENCES product(id) ON DELETE RESTRICT,
    warehouse_id    UUID          NOT NULL    REFERENCES warehouse(id) ON DELETE RESTRICT,
    direction       VARCHAR(10)   NOT NULL CHECK (direction IN ('in', 'out', 'adjust')),
    qty_base        NUMERIC(18,6) NOT NULL,
    unit_cost       NUMERIC(18,6) NOT NULL DEFAULT 0,
    total_cost      NUMERIC(18,6) NOT NULL DEFAULT 0,
    reference_type  VARCHAR(20)   NOT NULL
        CHECK (reference_type IN ('purchase', 'sale', 'adjust', 'transfer', 'init')),
    reference_id    UUID,
    occurred_at     TIMESTAMPTZ   NOT NULL DEFAULT now(),
    created_by      UUID,
    note            TEXT,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now()
);

-- History index: per-product timeline (most recent first).
CREATE INDEX stock_movement_history_idx
    ON stock_movement (product_id, occurred_at DESC);

-- Tenant filter index: accelerates listing by tenant.
CREATE INDEX stock_movement_tenant_idx
    ON stock_movement (tenant_id);

-- Row-level security.
ALTER TABLE stock_movement ENABLE ROW LEVEL SECURITY;
CREATE POLICY stock_movement_tenant_isolation ON stock_movement
    USING (tenant_id = current_setting('app.tenant_id', true)::UUID);
