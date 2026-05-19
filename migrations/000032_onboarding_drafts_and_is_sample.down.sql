-- 000032_onboarding_drafts_and_is_sample.down.sql
-- Reverse of 000032: drop onboarding_drafts + is_sample column.
--
-- WARNING: down migration loses any in-flight wizard drafts (acceptable —
-- drafts are by definition non-canonical) AND loses the is_sample flag
-- (any "clear sample data" feature shipped after this migration will break
-- if rolled back, but the data itself stays intact).

SET search_path TO tally;

DROP INDEX IF EXISTS idx_partner_sample;
DROP INDEX IF EXISTS idx_warehouse_sample;
ALTER TABLE partner   DROP COLUMN IF EXISTS is_sample;
ALTER TABLE warehouse DROP COLUMN IF EXISTS is_sample;

DROP INDEX IF EXISTS idx_onboarding_drafts_updated;
DROP TABLE IF EXISTS onboarding_drafts;
