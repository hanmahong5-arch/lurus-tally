-- 000058_tenant_profile_allow_horticulture.up.sql
-- 000013 only allowed cross_border/retail/hybrid, but the domain and the setup
-- page already offer horticulture (nursery / landscape); choosing it hit the
-- CHECK and returned 500. Widen the constraint.
SET search_path TO tally;

ALTER TABLE tenant_profile DROP CONSTRAINT IF EXISTS tenant_profile_profile_type_check;
ALTER TABLE tenant_profile ADD CONSTRAINT tenant_profile_profile_type_check
    CHECK (profile_type IN ('cross_border', 'retail', 'hybrid', 'horticulture'));
