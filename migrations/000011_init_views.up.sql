-- 000011_init_views.up.sql
-- Materialized view for stock summary reports and regular view for AI reorder suggestions.
-- The materialized view is empty on creation; the tally-worker refreshes it periodically.
-- REFRESH MATERIALIZED VIEW CONCURRENTLY requires the unique index created below.

CREATE MATERIALIZED VIEW IF NOT EXISTS tally.report_stock_summary AS
SELECT
    ss.tenant_id,
    p.id          AS product_id,
    p.code        AS product_code,
    p.name        AS product_name,
    w.id          AS warehouse_id,
    w.name        AS warehouse_name,
    ss.on_hand_qty,
    ss.available_qty,
    ss.avg_cost_price,
    ss.on_hand_qty * ss.avg_cost_price AS stock_value,
    si.low_safe_qty,
    si.high_safe_qty,
    CASE WHEN ss.available_qty < COALESCE(si.low_safe_qty, 0)
         THEN true ELSE false END AS is_low_stock
FROM tally.stock_snapshot ss
JOIN tally.product p ON p.id = ss.product_id
JOIN tally.warehouse w ON w.id = ss.warehouse_id
LEFT JOIN tally.stock_initial si
    ON si.product_id = ss.product_id AND si.warehouse_id = ss.warehouse_id;

-- Unique index required for REFRESH MATERIALIZED VIEW CONCURRENTLY
CREATE UNIQUE INDEX IF NOT EXISTS idx_report_stock_summary
    ON tally.report_stock_summary(tenant_id, product_id, warehouse_id);

-- AI reorder suggestions view consumed by Kova Agent
CREATE OR REPLACE VIEW tally.ai_reorder_suggestions AS
SELECT
    ss.tenant_id,
    p.id          AS product_id,
    p.name        AS product_name,
    ss.available_qty,
    p.predicted_monthly_demand,
    p.predicted_stockout_at,
    si.low_safe_qty,
    GREATEST(0, COALESCE(si.low_safe_qty, 0) * 2 - ss.available_qty) AS suggested_order_qty,
    p.recommendation_notes
FROM tally.stock_snapshot ss
JOIN tally.product p ON p.id = ss.product_id
LEFT JOIN tally.stock_initial si
    ON si.product_id = ss.product_id AND si.warehouse_id = ss.warehouse_id
WHERE p.predicted_stockout_at < now() + interval '30 days'
   OR ss.available_qty < COALESCE(si.low_safe_qty, 0);
