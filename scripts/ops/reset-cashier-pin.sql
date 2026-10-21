-- Support-only: clear a shop's cashier-mode PIN. Every device's cashier mode
-- is released on its next read (no PIN means no cashier mode).
-- Run against the tally database after confirming the owner's identity.
-- Usage: psql "$DSN" -v tenant_id=<tenant uuid> -f reset-cashier-pin.sql
SET search_path TO tally;
UPDATE tenant
SET settings = COALESCE(settings, '{}'::jsonb) - 'cashier_mode',
    updated_at = now()
WHERE id = :'tenant_id'::uuid;
