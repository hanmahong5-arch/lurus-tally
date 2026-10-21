-- 000060_pos_offline_client_ref.up.sql
-- Offline POS replay support: a till that was offline queues its checkouts and replays them later.
--
--   1. bill_head.client_ref   - client-generated idempotency reference of a checkout (<= 64 chars);
--                               (tenant_id, client_ref) is unique so a replayed request can never book a second sale.
--   2. bill_head.source       - already exists (VARCHAR(10) DEFAULT 'web', never written before); widened to
--                               VARCHAR(20) because 'pos_offline' is 11 chars. Values in use: web / pos / pos_offline
--                               (import / shopify reserved). No CHECK: the application validates the value.
--   3. bill_head.occurred_at  - the moment the sale really happened at the till (the replay may arrive hours later).
--                               NULL = same as created_at. bill_date is set from it by the application.
-- The unique index deliberately ignores deleted_at: a voided offline sale must not be re-bookable by a late replay.
SET search_path TO tally;

ALTER TABLE tally.bill_head
    ADD COLUMN IF NOT EXISTS client_ref  TEXT,
    ADD COLUMN IF NOT EXISTS occurred_at TIMESTAMPTZ;

ALTER TABLE tally.bill_head
    ALTER COLUMN source TYPE VARCHAR(20);

COMMENT ON COLUMN tally.bill_head.client_ref IS
    'Client-generated idempotency reference of a POS checkout (<= 64 chars); unique per tenant when set.';
COMMENT ON COLUMN tally.bill_head.occurred_at IS
    'When the sale happened at the till (offline replay may be later than created_at); NULL = created_at.';

CREATE UNIQUE INDEX IF NOT EXISTS bill_head_tenant_client_ref_uidx
    ON tally.bill_head (tenant_id, client_ref) WHERE client_ref IS NOT NULL;
