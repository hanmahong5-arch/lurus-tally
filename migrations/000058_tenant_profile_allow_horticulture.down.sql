-- 000058_tenant_profile_allow_horticulture.down.sql
-- Restores the 000013 constraint. Horticulture rows would violate it, so they are
-- folded into 'hybrid' (the closest generic profile) first.
SET search_path TO tally;

UPDATE tenant_profile SET profile_type = 'hybrid' WHERE profile_type = 'horticulture';
ALTER TABLE tenant_profile DROP CONSTRAINT IF EXISTS tenant_profile_profile_type_check;
ALTER TABLE tenant_profile ADD CONSTRAINT tenant_profile_profile_type_check
    CHECK (profile_type IN ('cross_border', 'retail', 'hybrid'));
