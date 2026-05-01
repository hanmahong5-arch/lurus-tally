CREATE TABLE tally.project (
    id               UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID         NOT NULL,
    code             VARCHAR(50)  NOT NULL,
    name             VARCHAR(200) NOT NULL,
    customer_id      UUID         REFERENCES tally.partner(id) ON DELETE SET NULL,
    contract_amount  NUMERIC(18,2),
    start_date       DATE,
    end_date         DATE,
    status           VARCHAR(20)  NOT NULL DEFAULT 'active'
                         CHECK (status IN ('active','paused','completed','cancelled')),
    address          TEXT,
    manager          VARCHAR(100),
    remark           TEXT,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ,
    CONSTRAINT uq_project_tenant_code UNIQUE (tenant_id, code)
);

CREATE INDEX idx_project_tenant   ON tally.project(tenant_id);
CREATE INDEX idx_project_status   ON tally.project(tenant_id, status);
CREATE INDEX idx_project_customer ON tally.project(customer_id) WHERE customer_id IS NOT NULL;
CREATE INDEX idx_project_name_trgm ON tally.project USING GIN (name gin_trgm_ops);

-- RLS: strict tenant isolation (no shared seed for project).
ALTER TABLE tally.project ENABLE ROW LEVEL SECURITY;
CREATE POLICY project_rls ON tally.project
    USING (tenant_id = current_setting('app.tenant_id', true)::UUID);
