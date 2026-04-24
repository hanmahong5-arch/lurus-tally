-- 000005_init_product.up.sql
-- Domain 4: product_* — categories, products (with AI embedding), SKUs, attributes, units.
-- Derived from jshERP jsh_material* (Apache-2.0)
-- Added: embedding vector(1536), ai_metadata JSONB, predicted_* fields (Lurus proprietary)

CREATE TABLE IF NOT EXISTS tally.product_category (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL,
    parent_id    UUID REFERENCES tally.product_category(id),
    name         VARCHAR(100) NOT NULL,
    code         VARCHAR(50),
    sort         INT NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at   TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_product_cat_tenant ON tally.product_category(tenant_id);

CREATE TABLE IF NOT EXISTS tally.product (
    id                        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                 UUID NOT NULL,
    category_id               UUID REFERENCES tally.product_category(id),
    code                      VARCHAR(100) NOT NULL,
    name                      VARCHAR(200) NOT NULL,
    manufacturer              VARCHAR(100),
    model                     VARCHAR(100),
    spec                      VARCHAR(200),
    brand                     VARCHAR(100),
    mnemonic                  VARCHAR(100),
    color                     VARCHAR(50),
    unit_id                   UUID,
    expiry_days               INT,
    weight_kg                 NUMERIC(18,4),
    enabled                   BOOLEAN NOT NULL DEFAULT true,
    enable_serial_no          BOOLEAN NOT NULL DEFAULT false,
    enable_lot_no             BOOLEAN NOT NULL DEFAULT false,
    shelf_position            VARCHAR(100),
    img_urls                  TEXT[],
    custom_field1             VARCHAR(500),
    custom_field2             VARCHAR(500),
    custom_field3             VARCHAR(500),
    remark                    TEXT,
    -- AI-specific fields (Lurus proprietary)
    embedding                 vector(1536),
    ai_metadata               JSONB NOT NULL DEFAULT '{}',
    predicted_monthly_demand  NUMERIC(18,4),
    predicted_stockout_at     TIMESTAMPTZ,
    recommendation_notes      TEXT,
    created_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at                TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_product_tenant ON tally.product(tenant_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_product_code ON tally.product(tenant_id, code) WHERE deleted_at IS NULL;
-- ivfflat index for cosine similarity search on product embeddings; requires pgvector extension.
CREATE INDEX IF NOT EXISTS idx_product_embedding ON tally.product
    USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);

CREATE TABLE IF NOT EXISTS tally.product_sku (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID NOT NULL,
    product_id       UUID NOT NULL REFERENCES tally.product(id),
    bar_code         VARCHAR(100),
    unit_name        VARCHAR(50),
    sku_attrs        VARCHAR(200),
    purchase_price   NUMERIC(18,6) NOT NULL DEFAULT 0,
    retail_price     NUMERIC(18,6) NOT NULL DEFAULT 0,
    wholesale_price  NUMERIC(18,6) NOT NULL DEFAULT 0,
    min_price        NUMERIC(18,6),
    is_default       BOOLEAN NOT NULL DEFAULT false,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_sku_product ON tally.product_sku(product_id);
CREATE INDEX IF NOT EXISTS idx_sku_barcode ON tally.product_sku(tenant_id, bar_code) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS tally.product_attribute (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID NOT NULL,
    attribute_name   VARCHAR(100) NOT NULL,
    attribute_values TEXT[],
    sort             INT DEFAULT 0
);

CREATE TABLE IF NOT EXISTS tally.unit (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL,
    name        VARCHAR(100) NOT NULL,
    base_unit   VARCHAR(50),
    sub_units   JSONB DEFAULT '[]',
    enabled     BOOLEAN DEFAULT true
);
