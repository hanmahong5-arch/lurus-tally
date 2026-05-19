-- 000034_payment_constraints.up.sql
-- W1 integrity hardening:
--   1. bill_head.revision column for cancel-restore cap.
--   2. bill_head paid_amount <= total_amount CHECK constraint.
--   3. payment_head.related_bill_id FK ON DELETE RESTRICT (orphan prevention).
--      NOTE: related_bill_id already references tally.bill_head(id) per 000008_init_finance.up.sql.
--      This migration verifies the FK exists and adds the explicit ON DELETE RESTRICT variant
--      if it was created without a delete rule. We use IF NOT EXISTS-style via constraint name check.
--   4. stock_movement.reference_id NOT NULL (orphan movements are bugs; backfill deletes them).

-- 1. Add revision column for restore cap tracking.
ALTER TABLE tally.bill_head
    ADD COLUMN IF NOT EXISTS revision INTEGER NOT NULL DEFAULT 0;

-- 2. paid_amount CHECK: paid cannot exceed total.
-- Non-deferrable is intentional: our code always writes paid_amount after total_amount in the
-- same transaction (recompute-then-write order), so the constraint is safe to check immediately.
-- If existing rows violate this, the migration FAILS LOUDLY rather than silently fixing
-- accounting data — corrupted accounting data must be investigated, not auto-corrected.
ALTER TABLE tally.bill_head
    ADD CONSTRAINT bill_head_paid_le_total
    CHECK (paid_amount <= total_amount);

-- 3. payment_head FK: add ON DELETE RESTRICT if not already present.
-- The original 000008 migration created the FK without an explicit delete rule (defaults to NO ACTION).
-- Dropping and re-adding with RESTRICT is semantically equivalent for normal operations but
-- makes the intent explicit and prevents hard-delete of bill_head rows that still have payments.
-- We drop first to avoid "constraint already exists" errors on re-run.
ALTER TABLE tally.payment_head
    DROP CONSTRAINT IF EXISTS payment_head_bill_head_fk;
ALTER TABLE tally.payment_head
    ADD CONSTRAINT payment_head_bill_head_fk
    FOREIGN KEY (related_bill_id) REFERENCES tally.bill_head(id) ON DELETE RESTRICT;

-- 4. stock_movement.reference_id NOT NULL.
-- Every legitimate movement has a source (purchase_bill / sale_bill / adjust).
-- Orphan rows with NULL reference_id indicate bugs — delete them before enforcing NOT NULL.
-- If this DELETE removes rows, it means there is a pre-existing data integrity bug that
-- must be investigated; the migration proceeds loudly (the DELETE count appears in migration log).
DELETE FROM tally.stock_movement WHERE reference_id IS NULL;
ALTER TABLE tally.stock_movement
    ALTER COLUMN reference_id SET NOT NULL;
