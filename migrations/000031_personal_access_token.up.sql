-- 000031_personal_access_token.up.sql
-- Personal Access Tokens for MCP / API key auth (ADR-0011 Phase 2).
--
-- Token format on the wire:
--   tally_pat_<prefix><secret>
--     prefix : 8 URL-safe random chars, stored in plaintext for fast row lookup
--     secret : 32 URL-safe random chars; only its sha256 is persisted
--
-- Middleware path:
--   1. Bearer starts with "tally_pat_" → strip
--   2. Extract first 8 chars → SELECT row WHERE prefix = $1 AND revoked_at IS NULL
--      AND (expires_at IS NULL OR expires_at > now())
--   3. constant_time_compare(sha256(prefix||secret), row.hash) → if match, attach
--      tenant_id + scopes to the request context.

SET search_path TO tally;

CREATE TABLE personal_access_token (
    id            UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID         NOT NULL,
    name          VARCHAR(64)  NOT NULL,
    prefix        VARCHAR(16)  NOT NULL,
    hash          CHAR(64)     NOT NULL,
    scopes        TEXT[]       NOT NULL DEFAULT ARRAY['read'],
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    expires_at    TIMESTAMPTZ,
    last_used_at  TIMESTAMPTZ,
    revoked_at    TIMESTAMPTZ,
    CONSTRAINT uq_pat_prefix UNIQUE (prefix)
);

-- List-by-tenant queries filter out revoked tokens; partial index keeps it tight.
CREATE INDEX idx_pat_tenant_active
    ON personal_access_token (tenant_id)
    WHERE revoked_at IS NULL;

-- RLS: same relax pattern as user_identity_mapping (see migration 000025) —
-- auth middleware must look up by prefix BEFORE app.tenant_id is known.
-- Reading by prefix alone exposes only what the caller already provided.
-- Once tenant_id is set, normal isolation resumes for /api/v1/auth/pats CRUD.
ALTER TABLE personal_access_token ENABLE ROW LEVEL SECURITY;
ALTER TABLE personal_access_token FORCE ROW LEVEL SECURITY;
CREATE POLICY pat_rls ON personal_access_token
    USING (
        current_setting('app.tenant_id', true) IS NULL
        OR current_setting('app.tenant_id', true) = ''
        OR tenant_id = current_setting('app.tenant_id', true)::UUID
    );
