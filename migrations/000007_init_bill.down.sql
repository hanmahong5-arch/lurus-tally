-- 000007_init_bill.down.sql
-- Drop in reverse FK dependency order.
DROP TABLE IF EXISTS tally.bill_item;
DROP TABLE IF EXISTS tally.bill_head;
