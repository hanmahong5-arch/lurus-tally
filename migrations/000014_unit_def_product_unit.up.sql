-- 000014_unit_def_product_unit.up.sql
-- Unit definition catalogue (system + tenant custom) and per-product unit conversion table.
-- Both tables get RLS; system units (is_system=true) are visible across all tenants via policy.
SET search_path TO tally;

CREATE TABLE unit_def (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID        NOT NULL,
    code        VARCHAR(20) NOT NULL,
    name        VARCHAR(50) NOT NULL,
    unit_type   VARCHAR(20) NOT NULL
                    CHECK (unit_type IN ('count','weight','length','volume','area','time')),
    is_system   BOOLEAN     NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, code)
);

CREATE INDEX unit_def_tenant_idx ON unit_def (tenant_id);

ALTER TABLE unit_def ENABLE ROW LEVEL SECURITY;
CREATE POLICY unit_def_rls ON unit_def
    USING (
        tenant_id = current_setting('app.tenant_id', true)::UUID
        OR is_system = true
    );

CREATE TABLE product_unit (
    id                   UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            UUID          NOT NULL,
    product_id           UUID          NOT NULL REFERENCES product(id) ON DELETE CASCADE,
    unit_id              UUID          NOT NULL REFERENCES unit_def(id) ON DELETE RESTRICT,
    conversion_factor    NUMERIC(20,6) NOT NULL CHECK (conversion_factor > 0),
    is_base              BOOLEAN       NOT NULL DEFAULT false,
    is_default_sale      BOOLEAN       NOT NULL DEFAULT false,
    is_default_purchase  BOOLEAN       NOT NULL DEFAULT false,
    created_at           TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    UNIQUE (product_id, unit_id)
);

CREATE INDEX product_unit_product_idx ON product_unit (product_id);
CREATE INDEX product_unit_tenant_idx  ON product_unit (tenant_id);

ALTER TABLE product_unit ENABLE ROW LEVEL SECURITY;
CREATE POLICY product_unit_rls ON product_unit
    USING (tenant_id = current_setting('app.tenant_id', true)::UUID);

-- Seed system units (tenant_id = zero UUID, is_system = true → visible to all tenants via RLS).
INSERT INTO unit_def (id, tenant_id, code, name, unit_type, is_system) VALUES
    (gen_random_uuid(), '00000000-0000-0000-0000-000000000000', 'pcs', '件',  'count',  true),
    (gen_random_uuid(), '00000000-0000-0000-0000-000000000000', 'box', '箱',  'count',  true),
    (gen_random_uuid(), '00000000-0000-0000-0000-000000000000', 'kg',  '千克', 'weight', true),
    (gen_random_uuid(), '00000000-0000-0000-0000-000000000000', 'g',   '克',  'weight', true),
    (gen_random_uuid(), '00000000-0000-0000-0000-000000000000', 'm',   '米',  'length', true),
    (gen_random_uuid(), '00000000-0000-0000-0000-000000000000', 'cm',  '厘米','length', true),
    (gen_random_uuid(), '00000000-0000-0000-0000-000000000000', 'l',   '升',  'volume', true);
