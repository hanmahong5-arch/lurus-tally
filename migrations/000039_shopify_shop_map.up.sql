-- Migration 000039: shopify_shop_map
-- Maps a Shopify shop domain to a Tally tenant so the public webhook endpoint
-- can resolve tenant_id without an authenticated caller.
--
-- Cross-tenant lookup rationale: the webhook path is public (no auth context).
-- Queries on this table are deliberately performed without RLS tenant isolation
-- because the lookup input is the shop_domain (the unknown), not the tenant_id.
-- The application must use a connection that bypasses RLS (BYPASSRLS role or
-- SET LOCAL ROLE postgres; RESET ROLE pattern) when querying this table.

CREATE TABLE IF NOT EXISTS tally.shopify_shop_map (
    id           UUID        NOT NULL DEFAULT gen_random_uuid(),
    tenant_id    UUID        NOT NULL,
    shop_domain  TEXT        NOT NULL,
    warehouse_id UUID        NOT NULL,
    creator_id   UUID        NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT shopify_shop_map_pk PRIMARY KEY (id),
    -- One shop domain maps to exactly one tenant.
    CONSTRAINT shopify_shop_map_shop_domain_unique UNIQUE (shop_domain)
);

CREATE INDEX IF NOT EXISTS shopify_shop_map_tenant_idx
    ON tally.shopify_shop_map (tenant_id);
