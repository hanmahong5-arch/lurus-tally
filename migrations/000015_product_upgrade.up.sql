-- 000015_product_upgrade.up.sql
-- Adds measurement_strategy, default_unit_id, and attributes JSONB to the product table.
-- GIN index on attributes enables efficient @> / ? / ?& / ?| JSONB operators.
SET search_path TO tally;

ALTER TABLE product
    ADD COLUMN measurement_strategy VARCHAR(20) NOT NULL DEFAULT 'individual'
        CHECK (measurement_strategy IN ('individual','weight','length','volume','batch','serial')),
    ADD COLUMN default_unit_id UUID NULL REFERENCES unit_def(id) ON DELETE SET NULL,
    ADD COLUMN attributes JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE INDEX product_attributes_gin   ON product USING gin(attributes);
CREATE INDEX product_default_unit_idx ON product (default_unit_id)
    WHERE default_unit_id IS NOT NULL;
