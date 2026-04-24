-- 000007_init_bill.up.sql
-- Domain 6: bill_head + bill_item — universal bill model shared by purchase/sales/transfer/stocktake.
-- Derived from jshERP jsh_depot_head/jsh_depot_item (Apache-2.0)
-- Extended: UUID PK, JSONB attachments, UUID[] salesperson_ids, amendment_of_id (red-reversal tracking)
--
-- bill_type: '入库' / '出库' / '其它'
-- sub_type: 采购/销售退货/销售/调拨/盘点录入/盘点复盘/采购订单/销售订单/...
-- status: 0草稿 1已提交 2已审核 3部分完成 4完成 9取消

CREATE TABLE IF NOT EXISTS tally.bill_head (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL,
    bill_no             VARCHAR(50) NOT NULL,
    bill_no_draft       VARCHAR(50),
    bill_type           VARCHAR(30) NOT NULL,
    sub_type            VARCHAR(30) NOT NULL,
    status              SMALLINT NOT NULL DEFAULT 0,
    purchase_status     SMALLINT DEFAULT 0,
    partner_id          UUID REFERENCES tally.partner(id),
    operator_id         UUID,
    creator_id          UUID NOT NULL,
    account_id          UUID,
    bill_date           TIMESTAMPTZ NOT NULL,
    total_amount        NUMERIC(18,4) NOT NULL DEFAULT 0,
    paid_amount         NUMERIC(18,4) NOT NULL DEFAULT 0,
    discount_rate       NUMERIC(8,4),
    discount_amount     NUMERIC(18,4),
    other_amount        NUMERIC(18,4),
    deposit_amount      NUMERIC(18,4),
    pay_type            VARCHAR(30),
    remark              TEXT,
    attachments         JSONB DEFAULT '[]',
    salesperson_ids     UUID[],
    link_bill_id        UUID REFERENCES tally.bill_head(id),
    source              VARCHAR(10) DEFAULT 'web',
    amendment_of_id     UUID REFERENCES tally.bill_head(id),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at          TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_bill_head_tenant ON tally.bill_head(tenant_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_bill_head_no
    ON tally.bill_head(tenant_id, bill_no) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_bill_head_type
    ON tally.bill_head(tenant_id, bill_type, sub_type, bill_date);
CREATE INDEX IF NOT EXISTS idx_bill_head_partner ON tally.bill_head(tenant_id, partner_id);

-- Derived from jshERP jsh_depot_item (Apache-2.0)
-- Extended: serial_nos TEXT[] (replaces jshERP comma-separated string), lot_id FK, bin_id FK
CREATE TABLE IF NOT EXISTS tally.bill_item (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL,
    head_id             UUID NOT NULL REFERENCES tally.bill_head(id) ON DELETE CASCADE,
    product_id          UUID NOT NULL REFERENCES tally.product(id),
    product_sku_id      UUID REFERENCES tally.product_sku(id),
    warehouse_id        UUID REFERENCES tally.warehouse(id),
    target_warehouse_id UUID REFERENCES tally.warehouse(id),
    unit_name           VARCHAR(20),
    sku_attrs           VARCHAR(200),
    qty                 NUMERIC(18,4) NOT NULL,
    base_qty            NUMERIC(18,4),
    unit_price          NUMERIC(18,6),
    purchase_price      NUMERIC(18,6),
    tax_rate            NUMERIC(8,4),
    tax_amount          NUMERIC(18,4),
    line_amount         NUMERIC(18,4),
    lot_id              UUID REFERENCES tally.stock_lot(id),
    serial_nos          TEXT[],
    expiry_date         DATE,
    link_item_id        UUID REFERENCES tally.bill_item(id),
    bin_id              UUID REFERENCES tally.warehouse_bin(id),
    remark              VARCHAR(500),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at          TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_bill_item_head ON tally.bill_item(head_id);
CREATE INDEX IF NOT EXISTS idx_bill_item_product ON tally.bill_item(tenant_id, product_id);
