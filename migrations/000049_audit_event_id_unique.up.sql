-- 000049_audit_event_id_unique.up.sql
-- Make account_audit_log idempotent against at-least-once event redelivery.
--
-- The audit subscriber consumes PSI_EVENTS with a durable JetStream consumer
-- (MaxDeliver:5, Nak on transient failure). A redelivered message previously
-- produced a *duplicate* audit row because the INSERT used a fresh row id and
-- carried no dedup key. We add the envelope's event_id as that key.
--
-- event_id is NULLable on purpose: synchronous audit writes (PAT create/revoke,
-- ai_executor) have no NATS envelope and pass an empty id. Postgres does NOT
-- enforce uniqueness across NULLs, so those rows still insert freely; only
-- subscriber rows that carry a real event_id are de-duplicated. The repo's
-- INSERT uses ON CONFLICT (event_id) DO NOTHING, which is a no-op for NULLs.

SET search_path TO tally;

ALTER TABLE tally.account_audit_log
    ADD COLUMN IF NOT EXISTS event_id TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS ux_account_audit_log_event_id
    ON tally.account_audit_log (event_id);
