-- 000023_bill_purchase.down.sql
-- Reverses Story 6.1 schema additions in reverse order.

SET search_path TO tally;

DROP INDEX IF EXISTS tally.idx_bill_head_warehouse;

ALTER TABLE bill_item
    DROP CONSTRAINT IF EXISTS bill_item_unit_id_fkey,
    DROP COLUMN IF EXISTS unit_id,
    DROP COLUMN IF EXISTS line_no;

ALTER TABLE bill_head
    DROP COLUMN IF EXISTS approved_by,
    DROP COLUMN IF EXISTS approved_at,
    DROP COLUMN IF EXISTS tax_amount,
    DROP COLUMN IF EXISTS shipping_fee,
    DROP COLUMN IF EXISTS subtotal,
    DROP COLUMN IF EXISTS warehouse_id;
