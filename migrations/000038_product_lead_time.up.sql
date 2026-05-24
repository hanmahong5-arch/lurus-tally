-- 000038_product_lead_time.up.sql
-- Adds per-product lead time in days to tally.product.
-- Per-supplier lead time refinement (supplier_product_lead_time table) is deferred to a future migration.

ALTER TABLE tally.product
    ADD COLUMN IF NOT EXISTS lead_time_days INT NOT NULL DEFAULT 7;
