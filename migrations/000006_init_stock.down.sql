-- 000006_init_stock.down.sql
-- Drop in reverse FK dependency order.
DROP TABLE IF EXISTS tally.stock_serial;
DROP TABLE IF EXISTS tally.stock_lot;
DROP TABLE IF EXISTS tally.stock_snapshot;
DROP TABLE IF EXISTS tally.stock_initial;
DROP TABLE IF EXISTS tally.warehouse_bin;
DROP TABLE IF EXISTS tally.warehouse;
