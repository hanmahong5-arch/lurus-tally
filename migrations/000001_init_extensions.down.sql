-- 000001_init_extensions.down.sql
-- Drops the entire tally schema and all objects within it (CASCADE).
-- Use only in down-all scenarios; this is irreversible in production.

DROP SCHEMA IF EXISTS tally CASCADE;
