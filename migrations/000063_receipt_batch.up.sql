-- One approved purchase line is one receipt batch. Sales draw it down so a
-- later reader can see which receipt the goods left from. Cost stays on the
-- stock snapshot (moving average); these rows are not a second cost ledger.

CREATE TABLE tally.receipt_batch (
  id              UUID PRIMARY KEY,
  tenant_id       UUID NOT NULL,
  warehouse_id    UUID NOT NULL,
  product_id      UUID NOT NULL,
  source_item_id  UUID NOT NULL,
  qty_base        NUMERIC(18,4) NOT NULL CHECK (qty_base > 0),
  qty_remaining   NUMERIC(18,4) NOT NULL CHECK (qty_remaining >= 0 AND qty_remaining <= qty_base),
  conv_factor     NUMERIC(18,6) NOT NULL CHECK (conv_factor > 0),
  received_at     TIMESTAMPTZ NOT NULL,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, source_item_id)
);

CREATE INDEX receipt_batch_product_idx
  ON tally.receipt_batch (tenant_id, warehouse_id, product_id, received_at, id);

CREATE TABLE tally.batch_draw (
  id            UUID PRIMARY KEY,
  tenant_id     UUID NOT NULL,
  sale_item_id  UUID NOT NULL,
  batch_id      UUID REFERENCES tally.receipt_batch(id),
  qty_base      NUMERIC(18,4) NOT NULL CHECK (qty_base > 0),
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX batch_draw_sale_item_idx
  ON tally.batch_draw (tenant_id, sale_item_id, created_at DESC, id DESC);

ALTER TABLE tally.receipt_batch ENABLE ROW LEVEL SECURITY;
ALTER TABLE tally.receipt_batch FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tally.receipt_batch
  USING (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
              THEN false
              ELSE tenant_id = current_setting('app.tenant_id', true)::uuid END)
  WITH CHECK (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                   THEN false
                   ELSE tenant_id = current_setting('app.tenant_id', true)::uuid END);

ALTER TABLE tally.batch_draw ENABLE ROW LEVEL SECURITY;
ALTER TABLE tally.batch_draw FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tally.batch_draw
  USING (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
              THEN false
              ELSE tenant_id = current_setting('app.tenant_id', true)::uuid END)
  WITH CHECK (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                   THEN false
                   ELSE tenant_id = current_setting('app.tenant_id', true)::uuid END);
