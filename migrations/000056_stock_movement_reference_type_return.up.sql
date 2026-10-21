-- 000056_stock_movement_reference_type_return.up.sql
-- 销售退货审核写 stock_movement.reference_type='return'，但 000022 的 CHECK 不含 return，
-- 导致退货审核必然违反约束而失败。扩展约束加入 'return'。
ALTER TABLE tally.stock_movement
    DROP CONSTRAINT IF EXISTS stock_movement_reference_type_check;
ALTER TABLE tally.stock_movement
    ADD CONSTRAINT stock_movement_reference_type_check
    CHECK (reference_type IN ('purchase', 'sale', 'return', 'adjust', 'transfer', 'init'));
