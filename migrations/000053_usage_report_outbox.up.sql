-- 000053_usage_report_outbox.up.sql
-- Durable retry queue for LLM usage events that could not be reported to the
-- platform metering ingest (unprovisioned tenant / resolver error / platform
-- unreachable). Before this, such events were silently dropped, losing billable
-- usage. The reporter now enqueues here and a dedicated worker re-resolves the
-- account (so a tenant provisioned AFTER the drop is back-reported) and re-POSTs.
--
-- Mirrors tally.event_outbox (000035): RLS with a 'service' bypass for the
-- background worker, partial index on the unsent set, attempts/last_error for
-- bounded retry. Differs by carrying TYPED usage fields (the worker rebuilds the
-- platform request) and a sent_at success marker instead of subject/payload.

CREATE TABLE tally.usage_report_outbox (
  id                 UUID PRIMARY KEY,          -- stable; seeds the platform idempotency key
  tenant_id          UUID NOT NULL,             -- re-resolved to an account at drain time
  model              TEXT NOT NULL,
  prompt_tokens      INTEGER NOT NULL,
  completion_tokens  INTEGER NOT NULL,
  occurred_at        TIMESTAMPTZ NOT NULL,      -- original event time
  reason             TEXT NOT NULL,             -- no_account | resolve_error | post_error
  created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
  sent_at            TIMESTAMPTZ,               -- NULL until successfully (re)delivered
  attempts           INTEGER NOT NULL DEFAULT 0,
  last_error         TEXT
);

CREATE INDEX ix_usage_report_outbox_unsent
  ON tally.usage_report_outbox (created_at)
  WHERE sent_at IS NULL;

ALTER TABLE tally.usage_report_outbox ENABLE ROW LEVEL SECURITY;
-- FORCE so the policy also binds the table OWNER — the role the app connects as.
-- ENABLE alone does NOT apply to the owner (the silent-bypass class migration 042
-- was written to fix); every other tenant table is FORCEd, so this one must be too.
ALTER TABLE tally.usage_report_outbox FORCE ROW LEVEL SECURITY;

-- USING + WITH CHECK (the form migration 042 standardised) so the 'service' pin /
-- tenant isolation governs reads AND writes; without WITH CHECK an INSERT under
-- FORCE would lean on Postgres defaulting it to USING, which is implicit and fragile.
CREATE POLICY usage_report_outbox_isolation ON tally.usage_report_outbox
  USING (
    current_setting('app.tenant_id', true) = 'service'
    OR tenant_id::text = current_setting('app.tenant_id', true)
  )
  WITH CHECK (
    current_setting('app.tenant_id', true) = 'service'
    OR tenant_id::text = current_setting('app.tenant_id', true)
  );
