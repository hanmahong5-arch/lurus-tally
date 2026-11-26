-- Intake inbox: a photo, file, voice note or receipt waits here until a person
-- confirms it. Confirming writes the existing invoice registry; stock and
-- receivables still move only through bill approval.

CREATE TABLE tally.intake_document (
  id             UUID PRIMARY KEY,
  tenant_id      UUID NOT NULL,
  source         VARCHAR(16) NOT NULL CHECK (source IN ('camera','file','voice','receipt')),
  direction      VARCHAR(3) NOT NULL DEFAULT 'in' CHECK (direction IN ('in','out')),
  status         VARCHAR(16) NOT NULL CHECK (status IN ('received','needs_review','accepted','rejected')),
  client_ref     VARCHAR(64),
  object_key     TEXT,
  content_type   VARCHAR(128),
  qr_payload     TEXT,
  seller_tax_no  VARCHAR(32),
  invoice_no     VARCHAR(64),
  issue_date     DATE,
  total_amount   NUMERIC(18,4),
  parsed         JSONB NOT NULL DEFAULT '{}',
  invoice_id     UUID,
  draft_bill_id  UUID,
  reject_reason  TEXT,
  created_by     UUID,
  lock_by        UUID,
  lock_until     TIMESTAMPTZ,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX intake_document_client_ref_uidx
  ON tally.intake_document (tenant_id, client_ref)
  WHERE client_ref IS NOT NULL AND client_ref <> '';

CREATE INDEX intake_document_status_idx
  ON tally.intake_document (tenant_id, status, created_at DESC);

CREATE TABLE tally.intake_fingerprint (
  tenant_id    UUID NOT NULL,
  kind         VARCHAR(16) NOT NULL CHECK (kind IN ('qr','image','text')),
  fingerprint  CHAR(64) NOT NULL,
  document_id  UUID NOT NULL REFERENCES tally.intake_document(id) ON DELETE CASCADE,
  PRIMARY KEY (tenant_id, kind, fingerprint)
);

ALTER TABLE tally.intake_document ENABLE ROW LEVEL SECURITY;
ALTER TABLE tally.intake_document FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tally.intake_document
  USING (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
              THEN false
              ELSE tenant_id = current_setting('app.tenant_id', true)::uuid END)
  WITH CHECK (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                   THEN false
                   ELSE tenant_id = current_setting('app.tenant_id', true)::uuid END);

ALTER TABLE tally.intake_fingerprint ENABLE ROW LEVEL SECURITY;
ALTER TABLE tally.intake_fingerprint FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tally.intake_fingerprint
  USING (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
              THEN false
              ELSE tenant_id = current_setting('app.tenant_id', true)::uuid END)
  WITH CHECK (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                   THEN false
                   ELSE tenant_id = current_setting('app.tenant_id', true)::uuid END);
