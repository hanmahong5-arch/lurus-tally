-- 000054_backfill_supplier_partner_mirror.down.sql
-- No-op: backfilled partner mirrors are indistinguishable from mirrors created
-- by supplier Create, and removing them would re-break bill FKs. Down is
-- intentionally empty (the data correction is not safely reversible).
SELECT 1;
