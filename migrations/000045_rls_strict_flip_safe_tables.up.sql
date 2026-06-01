-- 000045_rls_strict_flip_safe_tables.up.sql
-- Phase 3 (first, conservative increment): flip the short-circuit CASE arm from
-- THEN true to THEN false for the tenant tables that are provably accessed ONLY
-- by pinned request paths. Under THEN false an unpinned query returns 0 rows /
-- rejects writes, so a future forgotten pin fails LOUD instead of silently
-- leaning on a hand-written WHERE -- defence against regressions, on top of the
-- isolation Phase 1+2 already enforce for every pinned path.
--
-- SCOPE = {supplier, project} ONLY. These are written/read exclusively by their
-- pinned CRUD repos (+ pinned search), with NO public-webhook, background,
-- bootstrap, or pre-tenant access. End-to-end validated by the non-superuser
-- app-boot e2e (TestRLS_E2E_EntityCRUDIsolation) — both still serve their
-- endpoints under the strict policy — and by TestRLS_StrictFlip (unpinned -> 0).
--
-- DELIBERATELY NOT flipped yet (audited unpinned access):
--   * warehouse, exchange_rate, stock_*, bill_*, payment_* — the PUBLIC shopify
--     webhook -> import path reads/writes them UNPINNED (warehouse checker, FX
--     rater, sale creation, NextBillNo). Flipping needs the webhook import
--     pinned first, and that path currently has no integration-test coverage.
--   * import_*/shopify_shop_map (webhook), event_outbox/account_audit_log
--     (background), nursery_dict (startup seed), tenant_profile/
--     user_identity_mapping/personal_access_token (pre-tenant auth), user_session
--     (session-record runs before the pin) — must keep empty->true / relaxed.
--   * product, unit_def, partner, and the 000043 children — left empty->true
--     pending confirmation they have no unpinned reader (e.g. import sale path).

SET search_path TO tally;

DO $$
DECLARE
    t text;
    flip_tables text[] := ARRAY['supplier','project'];
BEGIN
    FOREACH t IN ARRAY flip_tables LOOP
        EXECUTE format('DROP POLICY IF EXISTS tenant_isolation ON tally.%I', t);
        EXECUTE format($q$
            CREATE POLICY tenant_isolation ON tally.%I
                USING (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                            THEN false
                            ELSE tenant_id = current_setting('app.tenant_id', true)::uuid END)
                WITH CHECK (CASE WHEN COALESCE(current_setting('app.tenant_id', true), '') = ''
                                 THEN false
                                 ELSE tenant_id = current_setting('app.tenant_id', true)::uuid END)
        $q$, t);
        -- FORCE was already set by 000042; DROP/CREATE POLICY leaves it intact.
    END LOOP;
END $$;
