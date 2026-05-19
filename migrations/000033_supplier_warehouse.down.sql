-- 000033_supplier_warehouse.down.sql

-- Reverse warehouse additions.
DROP INDEX IF EXISTS tally.ux_warehouse_tenant_name;
ALTER TABLE tally.warehouse
    DROP COLUMN IF EXISTS manager,
    DROP COLUMN IF EXISTS code,
    DROP COLUMN IF EXISTS updated_at;

-- Drop supplier entirely.
DROP TABLE IF EXISTS tally.supplier;
