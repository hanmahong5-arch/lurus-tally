-- 000038_product_lead_time.down.sql
ALTER TABLE tally.product
    DROP COLUMN IF EXISTS lead_time_days;
