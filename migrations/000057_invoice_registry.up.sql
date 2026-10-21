-- 000057_invoice_registry.up.sql
-- Invoice registry (phase 1): the tax invoice is issued offline at the tax bureau;
-- Tally only records the invoice number and which bills it covers. No tax-control
-- integration. direction: 'out' = sales invoice issued to a customer, 'in' = purchase
-- invoice received from a supplier. A red-letter invoice is a separate row with
-- negative amounts and red_of_id pointing at the original, which flips to 'red'.

CREATE TABLE tally.invoice (
  id               UUID PRIMARY KEY,
  tenant_id        UUID NOT NULL,
  direction        VARCHAR(3) NOT NULL CHECK (direction IN ('in','out')),
  invoice_no       VARCHAR(64) NOT NULL,
  invoice_code     VARCHAR(32),
  invoice_type     VARCHAR(32) NOT NULL,
  partner_id       UUID NOT NULL REFERENCES tally.partner(id),
  issue_date       DATE NOT NULL,
  amount_excl_tax  NUMERIC(18,4) NOT NULL,
  tax_amount       NUMERIC(18,4) NOT NULL,
  total_amount     NUMERIC(18,4) NOT NULL,
  status           VARCHAR(8) NOT NULL DEFAULT 'issued' CHECK (status IN ('issued','void','red')),
  red_of_id        UUID REFERENCES tally.invoice(id),
  remark           TEXT,
  created_by       UUID,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at       TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_invoice_no
  ON tally.invoice (tenant_id, direction, invoice_no) WHERE deleted_at IS NULL;
CREATE INDEX idx_invoice_date
  ON tally.invoice (tenant_id, issue_date DESC) WHERE deleted_at IS NULL;
CREATE INDEX idx_invoice_partner
  ON tally.invoice (tenant_id, partner_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_invoice_red_of
  ON tally.invoice (red_of_id) WHERE red_of_id IS NOT NULL;

CREATE TABLE tally.invoice_bill (
  invoice_id  UUID NOT NULL REFERENCES tally.invoice(id) ON DELETE CASCADE,
  bill_id     UUID NOT NULL REFERENCES tally.bill_head(id),
  tenant_id   UUID NOT NULL,
  amount      NUMERIC(18,4) NOT NULL CHECK (amount > 0),
  PRIMARY KEY (invoice_id, bill_id)
);

CREATE INDEX idx_invoice_bill_bill ON tally.invoice_bill (bill_id);

ALTER TABLE tally.invoice ENABLE ROW LEVEL SECURITY;
ALTER TABLE tally.invoice FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tally.invoice
  USING (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
              THEN false
              ELSE tenant_id = current_setting('app.tenant_id', true)::uuid END)
  WITH CHECK (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                   THEN false
                   ELSE tenant_id = current_setting('app.tenant_id', true)::uuid END);

ALTER TABLE tally.invoice_bill ENABLE ROW LEVEL SECURITY;
ALTER TABLE tally.invoice_bill FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tally.invoice_bill
  USING (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
              THEN false
              ELSE tenant_id = current_setting('app.tenant_id', true)::uuid END)
  WITH CHECK (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                   THEN false
                   ELSE tenant_id = current_setting('app.tenant_id', true)::uuid END);
