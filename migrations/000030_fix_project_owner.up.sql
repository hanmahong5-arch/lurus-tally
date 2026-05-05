-- Hotfix for STAGE drift: when 000029 was first applied, schema_migrations was
-- dirty and the migration was replayed manually with `psql -U postgres`, leaving
-- tally.project owned by `postgres` with no grants to the application role `tally`.
-- Symptom: SQLSTATE 42501 permission denied on SELECT/INSERT/UPDATE/DELETE.
--
-- Idempotent: ALTER OWNER and GRANT are safe to re-run.
-- Safe on fresh DBs where the migrator (running as `tally`) already owns the table.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='tally' AND table_name='project') THEN
        EXECUTE 'ALTER TABLE tally.project OWNER TO tally';
        EXECUTE 'GRANT ALL ON tally.project TO tally';
    END IF;
END $$;
