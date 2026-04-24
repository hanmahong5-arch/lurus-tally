-- 000005_init_product.down.sql
-- Drop in reverse FK dependency order.
DROP TABLE IF EXISTS tally.product_sku;
DROP TABLE IF EXISTS tally.product_attribute;
DROP TABLE IF EXISTS tally.product;
DROP TABLE IF EXISTS tally.product_category;
DROP TABLE IF EXISTS tally.unit;
