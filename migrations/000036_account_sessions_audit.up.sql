-- 000036_account_sessions_audit.up.sql
-- Three companion tables for the account-center Tier 3 page:
--   * user_session  — one row per authenticated browser session, used by the
--                     "安全" tab to list active devices and revoke them
--   * audit_log     — write-only flow of business-significant events
--                     (PAT created / revoked, bill approved, etc.) for the
--                     "活动日志" tab
--   * user_profile  — per-user editable profile (display_name override,
--                     phone, avatar bytes). One row per (tenant_id, user_id)
--
-- All three follow the established tenant_isolation RLS pattern
-- (see migration 000033 for the same shape on supplier/warehouse). Defence
-- in depth: handlers also filter by tenant_id explicitly.

SET search_path TO tally;

-- ── user_session ────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS tally.user_session (
    id           UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID         NOT NULL,
    user_id      TEXT         NOT NULL,           -- Zitadel sub
    user_agent   TEXT,
    ip_addr      INET,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    last_active  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    revoked_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS ix_user_session_tenant_user
    ON tally.user_session (tenant_id, user_id)
    WHERE revoked_at IS NULL;

ALTER TABLE tally.user_session ENABLE ROW LEVEL SECURITY;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_policies
        WHERE schemaname = 'tally' AND tablename = 'user_session' AND policyname = 'tenant_isolation'
    ) THEN
        CREATE POLICY tenant_isolation ON tally.user_session
            USING (tenant_id = current_setting('app.tenant_id')::uuid);
    END IF;
END
$$;

-- ── audit_log ───────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS tally.audit_log (
    id           UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID         NOT NULL,
    actor_id     TEXT         NOT NULL,           -- Zitadel sub of the actor
    action       TEXT         NOT NULL,           -- e.g. "pat.created"
    target_kind  TEXT,                            -- e.g. "pat", "bill"
    target_id    TEXT,
    payload      JSONB        NOT NULL DEFAULT '{}'::jsonb,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS ix_audit_log_tenant_created
    ON tally.audit_log (tenant_id, created_at DESC);

ALTER TABLE tally.audit_log ENABLE ROW LEVEL SECURITY;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_policies
        WHERE schemaname = 'tally' AND tablename = 'audit_log' AND policyname = 'tenant_isolation'
    ) THEN
        CREATE POLICY tenant_isolation ON tally.audit_log
            USING (tenant_id = current_setting('app.tenant_id')::uuid);
    END IF;
END
$$;

-- ── user_profile ────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS tally.user_profile (
    tenant_id            UUID         NOT NULL,
    user_id              TEXT         NOT NULL,           -- Zitadel sub
    display_name         VARCHAR(128),
    phone                VARCHAR(32),
    avatar_content_type  VARCHAR(32),
    avatar_bytes         BYTEA,                           -- max ~200KB enforced at handler
    updated_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, user_id)
);

ALTER TABLE tally.user_profile ENABLE ROW LEVEL SECURITY;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_policies
        WHERE schemaname = 'tally' AND tablename = 'user_profile' AND policyname = 'tenant_isolation'
    ) THEN
        CREATE POLICY tenant_isolation ON tally.user_profile
            USING (tenant_id = current_setting('app.tenant_id')::uuid);
    END IF;
END
$$;
