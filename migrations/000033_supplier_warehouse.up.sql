-- 000033_supplier_warehouse.up.sql
-- Supplier: new standalone table for supplier contacts.
-- Warehouse: tally.warehouse already exists (000006); we add updated_at, code,
--   manager VARCHAR, and a unique (tenant_id, name) index + RLS policy.

-- ── Supplier ────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS tally.supplier (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID        NOT NULL,
    code       VARCHAR(64),
    name       VARCHAR(128) NOT NULL,
    contact    VARCHAR(128),
    phone      VARCHAR(64),
    email      VARCHAR(128),
    address    VARCHAR(500),
    remark     VARCHAR(500),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_supplier_tenant_name
    ON tally.supplier (tenant_id, name)
    WHERE deleted_at IS NULL;

ALTER TABLE tally.supplier ENABLE ROW LEVEL SECURITY;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_policies
        WHERE schemaname = 'tally' AND tablename = 'supplier' AND policyname = 'tenant_isolation'
    ) THEN
        CREATE POLICY tenant_isolation ON tally.supplier
            USING (tenant_id = current_setting('app.tenant_id')::uuid);
    END IF;
END
$$;

-- ── Warehouse additions ──────────────────────────────────────────────────────
-- tally.warehouse exists from 000006 with: id, tenant_id, name, address,
-- manager_id (UUID), enabled, is_default, sort, remark, created_at, deleted_at.
-- We add: updated_at, code, manager (VARCHAR) for CRUD display; unique index; RLS.

ALTER TABLE tally.warehouse
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN IF NOT EXISTS code       VARCHAR(64),
    ADD COLUMN IF NOT EXISTS manager    VARCHAR(128);

CREATE UNIQUE INDEX IF NOT EXISTS ux_warehouse_tenant_name
    ON tally.warehouse (tenant_id, name)
    WHERE deleted_at IS NULL;

ALTER TABLE tally.warehouse ENABLE ROW LEVEL SECURITY;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_policies
        WHERE schemaname = 'tally' AND tablename = 'warehouse' AND policyname = 'tenant_isolation'
    ) THEN
        CREATE POLICY tenant_isolation ON tally.warehouse
            USING (tenant_id = current_setting('app.tenant_id')::uuid);
    END IF;
END
$$;
