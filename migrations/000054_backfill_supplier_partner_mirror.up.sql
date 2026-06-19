-- 000054_backfill_supplier_partner_mirror.up.sql
-- The supplier->partner mirror (a tally.partner row sharing the supplier's id,
-- partner_type='supplier') is co-created on supplier Create. Suppliers that
-- existed BEFORE that change have no partner row, so a bill referencing them
-- fails the bill_head.partner_id FK (23503 -> 409). Backfill a mirror for every
-- supplier lacking one, carrying the supplier's deleted_at so a soft-deleted
-- supplier gets a soft-deleted mirror (keeps the partial unique code index, which
-- is WHERE deleted_at IS NULL, collision-free).

INSERT INTO tally.partner
    (id, tenant_id, partner_type, name, code, created_at, updated_at, deleted_at)
SELECT s.id, s.tenant_id, 'supplier', s.name, s.code, s.created_at, s.updated_at, s.deleted_at
FROM tally.supplier s
WHERE NOT EXISTS (SELECT 1 FROM tally.partner p WHERE p.id = s.id);
