-- 000006_init_stock.up.sql
-- Domain 5: warehouse_* / stock_* — warehouses, bins, inventory snapshots, lots, serials, initial stock.
-- Derived from jshERP jsh_depot/jsh_material_current_stock + GreaterWMS binset (Apache-2.0)

CREATE TABLE IF NOT EXISTS tally.warehouse (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL,
    name        VARCHAR(100) NOT NULL,
    address     VARCHAR(200),
    manager_id  UUID,
    enabled     BOOLEAN DEFAULT true,
    is_default  BOOLEAN DEFAULT false,
    sort        INT DEFAULT 0,
    remark      VARCHAR(200),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);

-- Derived from GreaterWMS binset/models.py (Apache-2.0)
-- bin_size values: S/M/L/XL; bin_property values: 常温/冷藏/危品
CREATE TABLE IF NOT EXISTS tally.warehouse_bin (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL,
    warehouse_id UUID NOT NULL REFERENCES tally.warehouse(id),
    bin_code     VARCHAR(100) NOT NULL,
    bin_zone     VARCHAR(50),
    bin_size     VARCHAR(50),
    bin_property VARCHAR(50),
    is_empty     BOOLEAN DEFAULT true,
    bar_code     VARCHAR(100),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at   TIMESTAMPTZ
);

-- Derived from jshERP jsh_material_initial_stock (Apache-2.0)
CREATE TABLE IF NOT EXISTS tally.stock_initial (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL,
    product_id      UUID NOT NULL REFERENCES tally.product(id),
    warehouse_id    UUID NOT NULL REFERENCES tally.warehouse(id),
    qty             NUMERIC(18,4) NOT NULL DEFAULT 0,
    low_safe_qty    NUMERIC(18,4),
    high_safe_qty   NUMERIC(18,4),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_stock_initial_unique
    ON tally.stock_initial(tenant_id, product_id, warehouse_id);

-- Derived from jshERP jsh_material_current_stock (Apache-2.0)
-- Extended with GreaterWMS multi-status stock concept: on_hand/available/reserved/in_transit/damage/hold
CREATE TABLE IF NOT EXISTS tally.stock_snapshot (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL,
    product_id      UUID NOT NULL REFERENCES tally.product(id),
    warehouse_id    UUID NOT NULL REFERENCES tally.warehouse(id),
    on_hand_qty     NUMERIC(18,4) NOT NULL DEFAULT 0,
    available_qty   NUMERIC(18,4) NOT NULL DEFAULT 0,
    reserved_qty    NUMERIC(18,4) NOT NULL DEFAULT 0,
    in_transit_qty  NUMERIC(18,4) NOT NULL DEFAULT 0,
    damage_qty      NUMERIC(18,4) NOT NULL DEFAULT 0,
    hold_qty        NUMERIC(18,4) NOT NULL DEFAULT 0,
    avg_cost_price  NUMERIC(18,6) NOT NULL DEFAULT 0,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_stock_snapshot_unique
    ON tally.stock_snapshot(tenant_id, product_id, warehouse_id);

-- OFBiz Lot design: independent lot tracking per product.
CREATE TABLE IF NOT EXISTS tally.stock_lot (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID NOT NULL,
    product_id       UUID NOT NULL REFERENCES tally.product(id),
    lot_no           VARCHAR(100) NOT NULL,
    manufacture_date DATE,
    expiry_date      DATE,
    qty              NUMERIC(18,4) NOT NULL DEFAULT 0,
    cost_price       NUMERIC(18,6),
    remark           TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_lot_no ON tally.stock_lot(tenant_id, product_id, lot_no);

-- Derived from jshERP jsh_serial_number (Apache-2.0)
CREATE TABLE IF NOT EXISTS tally.stock_serial (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL,
    product_id   UUID NOT NULL REFERENCES tally.product(id),
    warehouse_id UUID,
    serial_no    VARCHAR(100) NOT NULL,
    is_sold      BOOLEAN NOT NULL DEFAULT false,
    cost_price   NUMERIC(18,6),
    in_bill_no   VARCHAR(50),
    out_bill_no  VARCHAR(50),
    creator_id   UUID,
    remark       TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at   TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_serial_no
    ON tally.stock_serial(tenant_id, serial_no) WHERE deleted_at IS NULL;
