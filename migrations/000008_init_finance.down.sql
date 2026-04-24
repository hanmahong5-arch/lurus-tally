-- 000008_init_finance.down.sql
-- Drop in reverse FK dependency order.
DROP TABLE IF EXISTS tally.payment_item;
DROP TABLE IF EXISTS tally.payment_head;
DROP TABLE IF EXISTS tally.finance_account;
DROP TABLE IF EXISTS tally.finance_category;
