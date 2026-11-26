-- One official statement movement. Confirming it writes tally.payment_head;
-- it does not approve a bill or move stock.

CREATE TABLE tally.money_line (
  id            UUID PRIMARY KEY,
  tenant_id     UUID NOT NULL,
  channel       VARCHAR(16) NOT NULL CHECK (channel IN ('alipay', 'wechat', 'bank')),
  account_ref   VARCHAR(64) NOT NULL DEFAULT '',
  external_id   VARCHAR(128) NOT NULL,
  booked_at     TIMESTAMPTZ NOT NULL,
  amount        NUMERIC(18,2) NOT NULL CHECK (amount <> 0),
  counterparty  VARCHAR(256) NOT NULL DEFAULT '',
  memo          VARCHAR(512) NOT NULL DEFAULT '',
  status        VARCHAR(16) NOT NULL CHECK (status IN ('open', 'matched', 'ignored')),
  bill_id       UUID,
  payment_id    UUID,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, channel, account_ref, external_id)
);

CREATE INDEX money_line_open_idx
  ON tally.money_line (tenant_id, status, booked_at DESC, id DESC);

ALTER TABLE tally.money_line ENABLE ROW LEVEL SECURITY;
ALTER TABLE tally.money_line FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tally.money_line
  USING (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
              THEN false
              ELSE tenant_id = current_setting('app.tenant_id', true)::uuid END)
  WITH CHECK (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                   THEN false
                   ELSE tenant_id = current_setting('app.tenant_id', true)::uuid END);
