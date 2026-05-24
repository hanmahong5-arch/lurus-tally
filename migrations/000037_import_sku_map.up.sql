-- 000037_import_sku_map.up.sql
-- Platform-order import support: SKU mapping (auto-learn per platform) and
-- order dedup (idempotent re-import) for Amazon / Shopify CSV ingestion.

-- ── import_sku_map ───────────────────────────────────────────────────────────
-- Maps a platform SKU identifier back to a Tally product_id.
-- Populated automatically during the first import of an order line;
-- updated whenever the operator provides a manual correction in the UI.

CREATE TABLE IF NOT EXISTS tally.import_sku_map (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID        NOT NULL,
    platform         VARCHAR(32) NOT NULL,                 -- 'amazon' | 'shopify'
    platform_sku     VARCHAR(256) NOT NULL,
    product_id       UUID        NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_import_sku_map_tenant_platform_sku
    ON tally.import_sku_map (tenant_id, platform, platform_sku);

ALTER TABLE tally.import_sku_map ENABLE ROW LEVEL SECURITY;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_policies
        WHERE schemaname = 'tally' AND tablename = 'import_sku_map' AND policyname = 'tenant_isolation'
    ) THEN
        CREATE POLICY tenant_isolation ON tally.import_sku_map
            USING (tenant_id = current_setting('app.tenant_id')::uuid);
    END IF;
END
$$;

-- ── import_order_seen ────────────────────────────────────────────────────────
-- Tracks which platform order numbers have already been imported so that
-- re-uploading the same CSV is safe (idempotent dedup).

CREATE TABLE IF NOT EXISTS tally.import_order_seen (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID        NOT NULL,
    platform          VARCHAR(32) NOT NULL,
    platform_order_no VARCHAR(256) NOT NULL,
    bill_id           UUID        NOT NULL,               -- tally.bill_head.id
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_import_order_seen_tenant_platform_order
    ON tally.import_order_seen (tenant_id, platform, platform_order_no);

ALTER TABLE tally.import_order_seen ENABLE ROW LEVEL SECURITY;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_policies
        WHERE schemaname = 'tally' AND tablename = 'import_order_seen' AND policyname = 'tenant_isolation'
    ) THEN
        CREATE POLICY tenant_isolation ON tally.import_order_seen
            USING (tenant_id = current_setting('app.tenant_id')::uuid);
    END IF;
END
$$;
