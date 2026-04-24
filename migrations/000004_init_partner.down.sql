-- 000004_init_partner.down.sql
-- Drop in reverse FK dependency order.
DROP TABLE IF EXISTS tally.partner_bank;
DROP TABLE IF EXISTS tally.partner;
