CREATE TABLE tally.event_outbox (
  id            UUID PRIMARY KEY,
  tenant_id     UUID NOT NULL,
  subject       VARCHAR(256) NOT NULL,
  payload       JSONB NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  published_at  TIMESTAMPTZ,
  attempts      INTEGER NOT NULL DEFAULT 0,
  last_error    TEXT
);

CREATE INDEX ix_event_outbox_unpublished
  ON tally.event_outbox (created_at)
  WHERE published_at IS NULL;

ALTER TABLE tally.event_outbox ENABLE ROW LEVEL SECURITY;

CREATE POLICY event_outbox_isolation ON tally.event_outbox
  USING (
    current_setting('app.tenant_id', true) = 'service'
    OR tenant_id::text = current_setting('app.tenant_id', true)
  );
