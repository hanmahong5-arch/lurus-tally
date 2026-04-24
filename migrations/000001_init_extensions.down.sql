-- 000001_init_extensions.down.sql
-- Drops only the extensions installed by 000001.up.sql.
-- The tally schema itself is preserved so that the migrate driver's
-- schema_migrations table (which lives in tally) remains queryable
-- and TRUNCATE-able after down-all completes.
-- Subsequent migrations (000002+) drop their own tables in their down files.

DROP EXTENSION IF EXISTS "vector";
DROP EXTENSION IF EXISTS "pg_trgm";
DROP EXTENSION IF EXISTS "pgcrypto";
