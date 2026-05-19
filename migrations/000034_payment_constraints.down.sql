-- 000034_payment_constraints.down.sql
-- Reverses 000034_payment_constraints.up.sql.

-- 4. Restore reference_id to nullable (data deleted in up cannot be recovered).
ALTER TABLE tally.stock_movement
    ALTER COLUMN reference_id DROP NOT NULL;

-- 3. Restore original payment_head FK (NO ACTION delete rule, no named constraint).
ALTER TABLE tally.payment_head
    DROP CONSTRAINT IF EXISTS payment_head_bill_head_fk;

-- 2. Drop paid_amount CHECK constraint.
ALTER TABLE tally.bill_head
    DROP CONSTRAINT IF EXISTS bill_head_paid_le_total;

-- 1. Drop revision column.
ALTER TABLE tally.bill_head
    DROP COLUMN IF EXISTS revision;
