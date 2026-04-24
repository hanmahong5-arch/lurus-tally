-- 000010_init_config.up.sql
-- Domain 9: system configuration, data dictionary, and bill sequence generator.
-- Derived from jshERP jsh_system_config/jsh_sys_dict_type/jsh_sequence (Apache-2.0)

-- Derived from jshERP jsh_system_config (Apache-2.0)
CREATE TABLE IF NOT EXISTS tally.system_config (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL,
    key          VARCHAR(100) NOT NULL,
    value        TEXT,
    description  VARCHAR(500),
    UNIQUE (tenant_id, key)
);

-- Derived from jshERP jsh_sys_dict_type (Apache-2.0)
CREATE TABLE IF NOT EXISTS tally.dict_type (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID,
    type_code VARCHAR(100) NOT NULL UNIQUE,
    type_name VARCHAR(100) NOT NULL,
    remark    VARCHAR(500)
);

-- Derived from jshERP jsh_sys_dict_data (Apache-2.0)
CREATE TABLE IF NOT EXISTS tally.dict_data (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID,
    type_id    UUID NOT NULL REFERENCES tally.dict_type(id),
    label      VARCHAR(100) NOT NULL,
    value      VARCHAR(100) NOT NULL,
    sort       INT DEFAULT 0,
    enabled    BOOLEAN DEFAULT true
);

-- Derived from jshERP jsh_sequence (Apache-2.0)
-- Bill number generator: each tenant+prefix pair tracks its own sequence.
CREATE TABLE IF NOT EXISTS tally.bill_sequence (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL,
    prefix      VARCHAR(20) NOT NULL,
    current_val BIGINT NOT NULL DEFAULT 0,
    UNIQUE (tenant_id, prefix)
);
