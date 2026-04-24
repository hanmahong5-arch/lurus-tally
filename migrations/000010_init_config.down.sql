-- 000010_init_config.down.sql
-- Drop in reverse FK dependency order.
DROP TABLE IF EXISTS tally.bill_sequence;
DROP TABLE IF EXISTS tally.dict_data;
DROP TABLE IF EXISTS tally.dict_type;
DROP TABLE IF EXISTS tally.system_config;
