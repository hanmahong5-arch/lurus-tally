-- 000009_init_audit.up.sql
-- Domain 8: audit_log — operation audit trail.
-- Derived from jshERP jsh_log (Apache-2.0), extended with changes JSONB

CREATE TABLE IF NOT EXISTS tally.audit_log (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL,
    user_id     UUID,
    action      VARCHAR(50) NOT NULL,
    resource    VARCHAR(50) NOT NULL,
    resource_id UUID,
    changes     JSONB,
    client_ip   VARCHAR(100),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_audit_log_tenant ON tally.audit_log(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_log_resource ON tally.audit_log(resource, resource_id);
