-- Migration 000040: import_order_cancel_seen + import_refund_seen
-- Provides idempotent dedup for webhook-delivered order cancellations and
-- per-refund dedup for Shopify refunds/create events.
--
-- Design notes:
--   import_order_cancel_seen: one row per (tenant, platform, platform_order_no).
--     References the original sale bill (original_bill_id) and the reversal bill
--     created during cancellation (reversal_bill_id) for audit.
--
--   import_refund_seen: one row per (tenant, platform_refund_id).
--     Shopify assigns a numeric refund-level id unique across the store, so this
--     is the natural dedup key.  platform_order_no + bill_id are stored for joins.

-- ── import_order_cancel_seen ─────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS tally.import_order_cancel_seen (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID        NOT NULL,
    platform            VARCHAR(32) NOT NULL,                 -- 'shopify'
    platform_order_no   VARCHAR(256) NOT NULL,
    original_bill_id    UUID        NOT NULL,                 -- the sale bill that was cancelled
    reversal_bill_id    UUID        NOT NULL,                 -- the red-reversal bill created
    cancelled_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_import_order_cancel_seen_key
    ON tally.import_order_cancel_seen (tenant_id, platform, platform_order_no);

ALTER TABLE tally.import_order_cancel_seen ENABLE ROW LEVEL SECURITY;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_policies
        WHERE schemaname = 'tally'
          AND tablename  = 'import_order_cancel_seen'
          AND policyname = 'tenant_isolation'
    ) THEN
        CREATE POLICY tenant_isolation ON tally.import_order_cancel_seen
            USING (tenant_id = current_setting('app.tenant_id')::uuid);
    END IF;
END
$$;

-- ── import_refund_seen ───────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS tally.import_refund_seen (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID        NOT NULL,
    platform            VARCHAR(32) NOT NULL,                 -- 'shopify'
    platform_order_no   VARCHAR(256) NOT NULL,
    platform_refund_id  VARCHAR(256) NOT NULL,               -- Shopify refund.id (numeric string)
    bill_id             UUID        NOT NULL,                 -- the return-stock bill created
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_import_refund_seen_key
    ON tally.import_refund_seen (tenant_id, platform, platform_refund_id);

ALTER TABLE tally.import_refund_seen ENABLE ROW LEVEL SECURITY;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_policies
        WHERE schemaname = 'tally'
          AND tablename  = 'import_refund_seen'
          AND policyname = 'tenant_isolation'
    ) THEN
        CREATE POLICY tenant_isolation ON tally.import_refund_seen
            USING (tenant_id = current_setting('app.tenant_id')::uuid);
    END IF;
END
$$;
