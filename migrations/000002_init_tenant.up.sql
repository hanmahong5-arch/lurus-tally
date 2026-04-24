-- 000002_init_tenant.up.sql
-- Domain 1: tenant local cache.
-- The id column syncs from 2l-svc-platform; no DEFAULT gen_random_uuid() here.

CREATE TABLE IF NOT EXISTS tally.tenant (
    id            UUID PRIMARY KEY,
    name          VARCHAR(200) NOT NULL,
    status        SMALLINT NOT NULL DEFAULT 1,
    plan_type     VARCHAR(30),
    expire_at     TIMESTAMPTZ,
    settings      JSONB NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
