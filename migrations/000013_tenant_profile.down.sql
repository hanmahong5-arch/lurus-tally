-- 000013_tenant_profile.down.sql

SET search_path TO tally;

DROP TABLE IF EXISTS user_identity_mapping;
DROP TABLE IF EXISTS tenant_profile;
