-- 000056_stock_movement_reference_type_return.down.sql
-- stock_movement 是追加型事件日志，不能删除历史行。回退前已存在 return 行时，
-- 把它们改标为 'sale'（销售退货的方向相反，仅作类别降级，数量与方向不变），再收紧约束。
-- 这是有损回退：回退后无法再区分这些行原为退货。
-- 事件日志的 UPDATE 触发器（若有）需放行本语句；000022 未建此类触发器。
-- stock_movement 开了 FORCE RLS，迁移以非超级用户 owner 执行且 GUC 为空时 UPDATE 看不到任何行，
-- 残留 return 行会让后面的 ADD CONSTRAINT 全表校验失败；故 UPDATE 前临时关闭 FORCE，完成后恢复（owner 有权切换）。
ALTER TABLE tally.stock_movement NO FORCE ROW LEVEL SECURITY;
UPDATE tally.stock_movement SET reference_type = 'sale' WHERE reference_type = 'return';
ALTER TABLE tally.stock_movement FORCE ROW LEVEL SECURITY;
ALTER TABLE tally.stock_movement
    DROP CONSTRAINT IF EXISTS stock_movement_reference_type_check;
ALTER TABLE tally.stock_movement
    ADD CONSTRAINT stock_movement_reference_type_check
    CHECK (reference_type IN ('purchase', 'sale', 'adjust', 'transfer', 'init'));
