-- 000036_account_sessions_audit.down.sql
SET search_path TO tally;

DROP TABLE IF EXISTS tally.user_profile;
DROP TABLE IF EXISTS tally.audit_log;
DROP TABLE IF EXISTS tally.user_session;
