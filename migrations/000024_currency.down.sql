-- Down migration 000024: reverse currency tables and bill_head columns
SET search_path TO tally;

-- Remove partner column if it was added
DO $$ BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'tally' AND table_name = 'partner'
          AND column_name = 'default_currency'
    ) THEN
        ALTER TABLE partner DROP COLUMN IF EXISTS default_currency;
    END IF;
END $$;

-- Remove bill_head columns
ALTER TABLE bill_head
    DROP COLUMN IF EXISTS amount_local,
    DROP COLUMN IF EXISTS exchange_rate,
    DROP COLUMN IF EXISTS currency;

-- Drop exchange_rate table (policy + indexes dropped automatically)
DROP TABLE IF EXISTS exchange_rate;

-- Drop currency table
DROP TABLE IF EXISTS currency;
