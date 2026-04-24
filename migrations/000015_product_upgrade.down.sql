-- 000015_product_upgrade.down.sql
SET search_path TO tally;

DROP INDEX IF EXISTS product_default_unit_idx;
DROP INDEX IF EXISTS product_attributes_gin;

ALTER TABLE product
    DROP COLUMN IF EXISTS attributes,
    DROP COLUMN IF EXISTS default_unit_id,
    DROP COLUMN IF EXISTS measurement_strategy;
