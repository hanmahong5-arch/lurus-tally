-- 000052_pat_drop_scopes.down.sql
-- Reverse 000052: re-add the scopes column with its original definition so a
-- rollback restores the table shape from migration 000031. Existing rows get
-- the default ARRAY['read'] (the only value the application ever wrote).

SET search_path TO tally;

ALTER TABLE tally.personal_access_token
    ADD COLUMN IF NOT EXISTS scopes TEXT[] NOT NULL DEFAULT ARRAY['read'];
