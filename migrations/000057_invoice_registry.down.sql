-- 000057_invoice_registry.down.sql
DROP POLICY IF EXISTS tenant_isolation ON tally.invoice_bill;
DROP TABLE IF EXISTS tally.invoice_bill;
DROP POLICY IF EXISTS tenant_isolation ON tally.invoice;
DROP TABLE IF EXISTS tally.invoice;
