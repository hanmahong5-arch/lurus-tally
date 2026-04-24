-- 000022_stock_upgrade.down.sql
-- Reverses 000022_stock_upgrade.up.sql in strict reverse-dependency order.

SET search_path TO tally;

-- 3. Drop stock_movement (must precede removal of warehouse FK column from stock_lot).
DROP POLICY IF EXISTS stock_movement_tenant_isolation ON stock_movement;
DROP TABLE IF EXISTS stock_movement;

-- 2. Reverse stock_lot upgrades.
DROP INDEX IF EXISTS stock_lot_fifo_idx;
DROP POLICY IF EXISTS stock_lot_tenant_isolation ON stock_lot;
ALTER TABLE stock_lot DISABLE ROW LEVEL SECURITY;
ALTER TABLE stock_lot
    DROP COLUMN IF EXISTS source_movement_id,
    DROP COLUMN IF EXISTS received_at,
    DROP COLUMN IF EXISTS unit_cost,
    DROP COLUMN IF EXISTS qty_remaining,
    DROP COLUMN IF EXISTS warehouse_id;

-- 1. Reverse stock_snapshot upgrades.
ALTER TABLE stock_snapshot
    DROP COLUMN IF EXISTS cost_strategy,
    DROP COLUMN IF EXISTS unit_cost;
