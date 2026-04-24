-- 000023_bill_purchase.up.sql
-- Story 6.1: purchase receipt baseline — extends bill_head/bill_item with purchase-specific columns.
-- Uses ADD COLUMN IF NOT EXISTS (PG 9.6+) so re-running the migration is safe.

SET search_path TO tally;

-- bill_head: purchase-specific columns
ALTER TABLE bill_head
    ADD COLUMN IF NOT EXISTS warehouse_id   UUID REFERENCES tally.warehouse(id),
    ADD COLUMN IF NOT EXISTS subtotal       NUMERIC(18,4) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS shipping_fee   NUMERIC(18,4) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS tax_amount     NUMERIC(18,4) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS approved_at    TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS approved_by    UUID;

-- bill_item: structured unit FK + line ordering
ALTER TABLE bill_item
    ADD COLUMN IF NOT EXISTS unit_id  UUID,
    ADD COLUMN IF NOT EXISTS line_no  INT NOT NULL DEFAULT 0;

-- FK on bill_item.unit_id (idempotent via DO block)
DO $$ BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE table_schema = 'tally'
          AND table_name   = 'bill_item'
          AND constraint_name = 'bill_item_unit_id_fkey'
    ) THEN
        ALTER TABLE bill_item
            ADD CONSTRAINT bill_item_unit_id_fkey
            FOREIGN KEY (unit_id) REFERENCES tally.unit_def(id) ON DELETE RESTRICT;
    END IF;
END $$;

-- Index: warehouse lookup on bill_head
CREATE INDEX IF NOT EXISTS idx_bill_head_warehouse
    ON tally.bill_head(tenant_id, warehouse_id)
    WHERE deleted_at IS NULL;

-- bill_sequence table for auto-numbered bill_no (may already exist from 000010, CREATE IF NOT EXISTS is safe)
-- 000010 schema: (id UUID PK, tenant_id UUID, prefix VARCHAR(20), current_val BIGINT, UNIQUE(tenant_id, prefix))
-- We keep 000010's schema and do not alter it here.
