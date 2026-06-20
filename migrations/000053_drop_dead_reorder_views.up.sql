-- 000053_drop_dead_reorder_views.up.sql
-- Remove the dead legacy reorder/low-stock definition that coexisted with the
-- learned reorder-point (ROP) engine, collapsing to a single execution path.
--
-- WHY: `ai_reorder_suggestions` (a view using the obsolete
-- `available_qty < low_safe_qty * 2` heuristic) and `report_stock_summary`
-- (a materialized view whose `is_low_stock` used the same obsolete rule) had
-- ZERO readers in the codebase — no Go query selects them and nothing ever runs
-- REFRESH MATERIALIZED VIEW, so the matview was created empty and never updated.
-- The live low-stock alert, replenishment suggestions, and weekly digest all
-- derive from the learned ROP in app/replenish instead. The columns
-- product.predicted_monthly_demand / predicted_stockout_at / recommendation_notes
-- existed only to feed the dead view and were never written by any code. Keeping
-- them is a second, divergent reorder definition — the legacy/new split the
-- architecture forbids.

-- Drop in dependency order: views first (they reference the product columns),
-- then the now-unreferenced columns.
DROP VIEW IF EXISTS tally.ai_reorder_suggestions;
DROP MATERIALIZED VIEW IF EXISTS tally.report_stock_summary; -- idx_report_stock_summary drops with it

ALTER TABLE tally.product DROP COLUMN IF EXISTS predicted_monthly_demand;
ALTER TABLE tally.product DROP COLUMN IF EXISTS predicted_stockout_at;
ALTER TABLE tally.product DROP COLUMN IF EXISTS recommendation_notes;
