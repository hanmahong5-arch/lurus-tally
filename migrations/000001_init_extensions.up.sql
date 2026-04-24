-- 000001_init_extensions.up.sql
-- Creates the tally schema and installs required PostgreSQL extensions.
-- This migration MUST run first; all subsequent migrations depend on these extensions.

CREATE SCHEMA IF NOT EXISTS tally;

-- pgcrypto: provides gen_random_uuid() used by all tables with UUID PKs
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- vector: pgvector extension for product.embedding similarity search
-- Requires pgvector >= 0.5.0 installed in the PostgreSQL cluster.
CREATE EXTENSION IF NOT EXISTS "vector";

-- pg_trgm: trigram indexes for fuzzy text search on product name / partner name
CREATE EXTENSION IF NOT EXISTS "pg_trgm";
