-- 000028_nursery_dict.up.sql
-- Creates the nursery_dict table for horticulture species dictionary (Story 28.1).
-- pg_trgm was enabled in migration 000001; no need to re-enable here.

CREATE TYPE tally.nursery_type AS ENUM
    ('tree','shrub','herb','vine','bamboo','aquatic','bulb','fruit');

CREATE TABLE tally.nursery_dict (
    id               UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID         NOT NULL,
    name             VARCHAR(100) NOT NULL,
    latin_name       VARCHAR(200),
    family           VARCHAR(100),
    genus            VARCHAR(100),
    type             tally.nursery_type NOT NULL DEFAULT 'tree',
    is_evergreen     BOOLEAN      NOT NULL DEFAULT false,
    climate_zones    TEXT[]       NOT NULL DEFAULT '{}',
    -- best_season: 2-element array [start_month, end_month], months 1-12; '{}' means unset
    best_season      INT[]        NOT NULL DEFAULT '{}',
    -- spec_template: JSONB dict of typical spec keys, e.g. {"胸径_cm": null, "冠幅_cm": null}
    spec_template    JSONB        NOT NULL DEFAULT '{}',
    default_unit_id  UUID         REFERENCES tally.unit_def(id) ON DELETE SET NULL,
    photo_url        TEXT,
    remark           TEXT,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ,
    CONSTRAINT uq_nursery_dict_tenant_name UNIQUE (tenant_id, name)
);

CREATE INDEX idx_nursery_dict_tenant   ON tally.nursery_dict(tenant_id);
CREATE INDEX idx_nursery_dict_type     ON tally.nursery_dict(tenant_id, type);
CREATE INDEX idx_nursery_dict_name_trgm ON tally.nursery_dict
    USING GIN (name gin_trgm_ops);
CREATE INDEX idx_nursery_dict_spec_gin ON tally.nursery_dict
    USING GIN (spec_template);

-- RLS: own rows + shared seed rows (tenant_id = nil UUID = public seed)
ALTER TABLE tally.nursery_dict ENABLE ROW LEVEL SECURITY;
CREATE POLICY nursery_dict_rls ON tally.nursery_dict
    USING (
        tenant_id = current_setting('app.tenant_id', true)::UUID
        OR tenant_id = '00000000-0000-0000-0000-000000000000'::UUID
    );
