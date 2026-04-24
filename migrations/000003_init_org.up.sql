-- 000003_init_org.up.sql
-- Domain 2: org_* — organizational structure (departments + user relations).
-- Derived from jshERP jsh_organization + jsh_orga_user_rel (Apache-2.0)

CREATE TABLE IF NOT EXISTS tally.org_department (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tally.tenant(id),
    parent_id   UUID REFERENCES tally.org_department(id),
    name        VARCHAR(100) NOT NULL,
    sort        INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_org_dept_tenant ON tally.org_department(tenant_id);

CREATE TABLE IF NOT EXISTS tally.org_user_rel (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL,
    dept_id      UUID REFERENCES tally.org_department(id),
    user_id      UUID NOT NULL,
    sort         INT DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
