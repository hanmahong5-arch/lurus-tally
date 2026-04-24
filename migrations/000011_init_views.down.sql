-- 000011_init_views.down.sql
-- Drop in reverse creation order.
DROP VIEW IF EXISTS tally.ai_reorder_suggestions;
DROP MATERIALIZED VIEW IF EXISTS tally.report_stock_summary;
