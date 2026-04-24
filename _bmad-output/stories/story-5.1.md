# Story 5.1: 库存基础模型与计价策略 — FIFO + WAC

**Epic**: 5 — 仓库与库存基础
**Story ID**: 5.1
**Profile**: both
**Type**: feat
**Estimate**: 10h
**Status**: Draft

---

## Context

Epic 4 (Story 4.1) 建立了商品中台：`unit_def` + `product_unit` 多单位换算、`measurement_strategy`、`unitconv.ConvertToBase`。现在 Epic 5 需要在此基础上落地库存核心：一个描述库存**当前状态**的物化快照（`stock_snapshot`）+ 一条描述库存**变动历史**的事件流（`stock_movement`）+ FIFO 批次队列（`stock_lot` 升级）。

本 Story 将架构 §4 中的 `InventoryCalculator` interface 及 WAC / FIFO 两种实现从设计变成可运行代码，并暴露供 Epic 6（采购入库）/ Epic 7（销售出库）调用的内部契约。到本 Story 结束时，dev 可直调 `POST /api/v1/stock/movements` 触发库存变动，并通过 `GET /api/v1/stock/snapshots` 查看聚合状态。

**依赖**: Story 4.1（`unit_def`/`product_unit`/`unitconv` 已就绪，`internal/pkg/unitconv.ConvertToBase` 可直接调用）。

---

## Acceptance Criteria

1. Migration 000022 执行成功后，`tally.stock_snapshot` 含新列 `unit_cost NUMERIC(18,6) NOT NULL DEFAULT 0` 和 `cost_strategy VARCHAR(20) NOT NULL DEFAULT 'wac' CHECK (cost_strategy IN ('fifo','wac'))`；`tally.stock_movement` 新表存在；`tally.stock_lot` 含列 `qty_remaining`、`unit_cost`、`received_at`、`source_movement_id`；所有新表启用 RLS。

2. **WAC 场景**: 初始快照 `on_hand_qty=100, unit_cost=10.000000`；POST movement `{direction:"in", qty_base:50, unit_cost:12.000000}` → 快照更新为 `on_hand_qty=150, unit_cost=10.666667`（= (100×10+50×12)/150，精度 6 位小数）。

3. **FIFO 入库场景**: 连续三次 POST movement `{direction:"in"}` 创建三个 lot（50@¥8, 30@¥9, 20@¥10）后，`stock_lot` 中对应三行 `qty_remaining` 正确为 50/30/20。

4. **FIFO 出库场景**: POST movement `{direction:"out", qty_base:60}`（FIFO 策略下）→ 第一个 lot 消耗至 0，第二个 lot `qty_remaining=20`；movement 记录的 `unit_cost` 为加权出库均价（= (50×8+10×9)/60 = 8.167）；`stock_snapshot.on_hand_qty` = 40。

5. **多单位换算**: 商品 `base_unit=g`；POST movement 携带 `{qty:1, unit_id: <包的UUID>}`（包=500g，`conversion_factor=500`）→ `stock_movement.qty_base = 500`（通过 `unitconv.ConvertToBase` 换算），快照 `on_hand_qty` 增加 500。

6. **出库超库存保护**: POST movement `{direction:"out", qty_base: N > on_hand_qty}` → HTTP 422，body 含 `{"error":"insufficient_stock","available":X,"requested":N}`，快照不变，无 movement 记录写入。

7. **Adjust 流水**: POST movement `{direction:"adjust", qty_base:-10}`（盘亏）→ `on_hand_qty` 减少 10，`unit_cost` 不变；`{direction:"adjust", qty_base:+5}`（盘盈）→ `on_hand_qty` 增加 5，`unit_cost` 不变。

8. **并发安全**: 同一 `(tenant_id, product_id, warehouse_id)` 下两个并发 `POST /api/v1/stock/movements` 请求，最终 `on_hand_qty` 精确等于两次操作的叠加（无数据竞争，通过 PG advisory lock 序列化写入）。`TestStockMovement_Concurrent_NoDrift` 跑 10 个并发 goroutine 后断言。

9. **Profile 默认策略**: `tenant_profile.inventory_method = 'fifo'` 的租户，新 movement 写入时使用 FIFOCalculator；`inventory_method = 'wac'` 使用 WeightedAvgCalculator。策略从 profile middleware 注入的 `Profile.InventoryMethod()` 读取，不硬编码。

10. **GET /api/v1/stock/snapshots 性能**: 10000 条 movement 历史 + 1000 SKU 下，快照列表查询 < 200ms（快照是物化聚合，不实时 scan movement）。

11. **前端快照列表**: `/stock` 页面可渲染快照列表；`cross_border` profile 显示列：商品名/仓库/批次数/在库数/均价/总成本；`retail` profile 显示列：商品名/仓库/在库数/均价/总成本/库存状态（充足/低/缺货，由 `stock_initial.low_safe_qty` 决定阈值）。

12. **前端单品详情**: `/stock/:product_id` 页面展示该商品所有仓库快照 + movement 历史时间线（分页，最新在前）；FIFO profile 额外显示 lots 列表（批次号/剩余数/单价）。

---

## Tasks / Subtasks

### Task 1: Migration 000022 — stock_snapshot 升级 + stock_movement 新表 + stock_lot 升级

- [ ] 写测试 `TestMigration_000022_StockUpgrade`（连接测试 DB，执行 up/down，断言表结构 + 列存在）
- [ ] 创建 `migrations/000022_stock_upgrade.up.sql`:
  - ALTER `tally.stock_snapshot` 加 `unit_cost NUMERIC(18,6)` + `cost_strategy VARCHAR(20)` + CHECK + DEFAULT
  - ALTER `tally.stock_lot` 加 `qty_remaining NUMERIC(18,4)`、`unit_cost NUMERIC(18,6)`、`received_at TIMESTAMPTZ`、`source_movement_id UUID`（暂无 FK，避免循环依赖）
  - CREATE `tally.stock_movement` 表（含 RLS + GIN 索引 on `(product_id, occurred_at DESC)`）
  - ALTER TABLE `tally.stock_lot` ENABLE ROW LEVEL SECURITY + CREATE POLICY
- [ ] 创建 `migrations/000022_stock_upgrade.down.sql`（逆序 DROP / ALTER DROP COLUMN）
- [ ] 验证：`go test ./internal/lifecycle/... -run TestMigration` PASS

### Task 2: Go — domain entities

- [ ] 写测试 `TestMovement_Direction_Valid` + `TestMovement_Direction_Invalid`（验证枚举值）
- [ ] 创建 `internal/domain/stock/movement.go` — `StockMovement` struct（含 Direction 枚举 in/out/adjust）、`StockSnapshot` struct、`LotUpdate` struct
- [ ] 创建 `internal/domain/stock/lot.go` — `StockLot` struct（含 `QtyRemaining decimal.Decimal`）
- [ ] 验证：`go test ./internal/domain/stock/...` PASS

### Task 3: Go — InventoryCalculator interface + factory

- [ ] 写测试 `TestCalculatorFactory_WAC_SelectedForRetail` + `TestCalculatorFactory_FIFO_SelectedForCrossBorder`
- [ ] 创建 `internal/app/stock/calculator.go` — `InventoryCalculator` interface（`ApplyMovement / ValidateMovement`，与 architecture §4.1 签名完全一致）
- [ ] 创建 `internal/app/stock/calculator_factory.go` — `NewCalculator(profile Profile, repo StockRepo) InventoryCalculator`；按 `profile.InventoryMethod()` 分支选 WAC 或 FIFO
- [ ] 验证：factory 单元测试 PASS

### Task 4: Go — WAC 实现 (TDD)

- [ ] 写失败测试 `TestWAC_ApplyMovement_Inbound_UpdatesAvgCost`（初始 100@10，入 50@12 → 均价 10.666667）
- [ ] 写失败测试 `TestWAC_ApplyMovement_Inbound_ZeroInitial`（初始库存=0 时，均价直接取新单价）
- [ ] 写失败测试 `TestWAC_ApplyMovement_Outbound_DecreasesQty`（出库不改 unit_cost）
- [ ] 写失败测试 `TestWAC_ApplyMovement_Adjust_NoCostChange`（adjust 不改 unit_cost）
- [ ] 写失败测试 `TestWAC_ValidateMovement_Oversell_Returns422`（出库超库存返回错误）
- [ ] 创建 `internal/app/stock/calc_wac.go` — `WeightedAvgCalculator`，公式 `(old_qty×old_cost + new_qty×new_cost)/(old_qty+new_qty)`，全程 `decimal.Decimal`，出库时调 `StockRepo.SelectForUpdate` 获取当前快照
- [ ] 验证：`go test ./internal/app/stock/... -run TestWAC` PASS

### Task 5: Go — FIFO 实现 (TDD)

- [ ] 写失败测试 `TestFIFO_ApplyMovement_Inbound_CreatesLot`（入库创建 lot 记录，`qty_remaining=qty_base`）
- [ ] 写失败测试 `TestFIFO_ApplyMovement_Outbound_ConsumesOldestLotFirst`（三批次 lots，出 60 → 第一批清零，第二批剩 20）
- [ ] 写失败测试 `TestFIFO_ApplyMovement_Outbound_CostedByWeightedConsumedLots`（出库 movement.unit_cost = 加权已消耗批次均价）
- [ ] 写失败测试 `TestFIFO_ValidateMovement_Oversell_Returns422`（出库超 sum(qty_remaining) 返回错误）
- [ ] 创建 `internal/app/stock/calc_fifo.go` — `FIFOCalculator`：入库 `→ INSERT stock_lot`；出库 `→ SELECT ... ORDER BY received_at ASC FOR UPDATE` CTE 消耗 lots，计算加权出库成本，更新 `qty_remaining`，写 movement
- [ ] 验证：`go test ./internal/app/stock/... -run TestFIFO` PASS

### Task 6: Go — StockRepo interface + PG 实现

- [ ] 写失败测试 `TestStockRepo_SelectForUpdate_BlocksConcurrent`（并发测试，10 goroutine 写同一 SKU，最终 qty 精确）
- [ ] 创建 `internal/adapter/repo/stock/repo.go` — 实现 `StockRepo` interface：
  - `GetSnapshot(ctx, tenantID, productID, warehouseID) (*StockSnapshot, error)`
  - `SelectForUpdate(ctx, tx, tenantID, productID, warehouseID) (*StockSnapshot, error)` — `SELECT ... FOR UPDATE`
  - `UpsertSnapshot(ctx, tx, snapshot) error`
  - `InsertMovement(ctx, tx, movement) error`
  - `ListMovements(ctx, tenantID, productID, warehouseID, limit, offset int) ([]StockMovement, error)`
  - `InsertLot(ctx, tx, lot) error`
  - `ListActiveLots(ctx, tx, tenantID, productID, warehouseID) ([]StockLot, error)` — `ORDER BY received_at ASC FOR UPDATE`（供 FIFO 出库）
  - `UpdateLotQty(ctx, tx, lotID, qtyRemaining) error`
  - `AcquireAdvisoryLock(ctx, tx, tenantID, productID, warehouseID) error` — `SELECT pg_advisory_xact_lock(hashtext(...))`
- [ ] 验证：并发测试 PASS

### Task 7: Go — RecordMovement use case（事务编排）

- [ ] 写失败测试 `TestRecordMovement_FIFO_Inbound_CommitsAll`（断言 movement + snapshot + lot 均写入，任一失败全部回滚）
- [ ] 写失败测试 `TestRecordMovement_WAC_Inbound_CommitsAll`
- [ ] 写失败测试 `TestRecordMovement_UnitConversion_AppliedBeforeCalc`（输入 qty+unit_id，转换后才进 calculator）
- [ ] 创建 `internal/app/stock/usecase.go` — `RecordMovementUseCase.Execute(ctx, req)` 流程：
  1. 调 `unitconv.ConvertToBase(req.Qty, req.UnitID, product.Units)` → `qty_base`
  2. 开启 PG 事务
  3. `repo.AcquireAdvisoryLock(ctx, tx, ...)` 序列化同 SKU 写入
  4. `calculator.ValidateMovement(ctx, movement)` → 若失败返回 422，回滚
  5. `calculator.ApplyMovement(ctx, movement)` → 写 snapshot + lot + movement（在同一事务内）
  6. 提交事务
  7. 发布 `psi.stock.changed` 到 NATS（异步，事务外，失败只 log 不回滚）
- [ ] 创建 `internal/app/stock/query.go` — `GetSnapshotUseCase` + `ListSnapshotsUseCase` + `ListMovementsUseCase`
- [ ] 验证：`go test ./internal/app/stock/...` PASS

### Task 8: Go — REST handler

- [ ] 写失败测试 `TestStockHandler_PostMovement_WAC_Returns201`
- [ ] 写失败测试 `TestStockHandler_PostMovement_Oversell_Returns422`
- [ ] 写失败测试 `TestStockHandler_GetSnapshots_Returns200`
- [ ] 创建 `internal/adapter/handler/stock/handler.go` — 实现以下 endpoints：
  - `GET /api/v1/stock/snapshots` — query params: `product_id`, `warehouse_id`，返回列表（超集字段，前端按 profile 渲染）
  - `GET /api/v1/stock/snapshots/:product_id/:warehouse_id` — 单条快照
  - `GET /api/v1/stock/movements` — query params: `product_id`, `warehouse_id`, `limit`, `offset`
  - `POST /api/v1/stock/movements` — request body: `{product_id, warehouse_id, direction, qty, unit_id, unit_cost, reference_type, reference_id, note}`；**V1 阶段为 dev 直调 endpoint**，无额外鉴权层；Epic 6/7 将从 purchase/sale handler 内部调用同一 use case，不通过此 HTTP endpoint
- [ ] 验证：handler 单元测试 PASS

### Task 9: Go — lifecycle wiring + router

- [ ] 修改 `internal/adapter/handler/router/router.go` — 注册 stock 路由组，遵循现有 `stockHandler(sh, ...)` 模式
- [ ] 修改 `internal/lifecycle/app.go` — wire `StockRepo` → `WeightedAvgCalculator` / `FIFOCalculator` → `CalculatorFactory` → `RecordMovementUseCase` → `StockHandler`
- [ ] 验证：`go build ./...` PASS + `go test ./...` PASS

### Task 10: Frontend — stock API wrapper

- [ ] 写失败测试（Vitest）`stock.test.ts — listSnapshots returns typed array`
- [ ] 创建 `web/lib/api/stock.ts` — `listSnapshots(params)` / `getSnapshot(productId, warehouseId)` / `listMovements(params)` / `postMovement(body)` 四个 fetch wrapper，类型化响应
- [ ] 验证：`bun run test` PASS

### Task 11: Frontend — 库存快照列表页

- [ ] 创建 `web/app/(dashboard)/stock/page.tsx` — profile-aware DataTable：
  - `cross_border` 列：商品名/仓库/批次数/在库数(base_unit)/均价/总成本
  - `retail` 列：商品名/仓库/在库数/均价/总成本/库存状态（充足/低/缺货，阈值来自 `stock_initial.low_safe_qty`）
  - 使用已有 `useProfile()` hook 和 `<ProfileGate>` 条件列
- [ ] 验证：`bunx tsc --noEmit` PASS + `bun run build` PASS

### Task 12: Frontend — 单商品库存详情页

- [ ] 创建 `web/app/(dashboard)/stock/[product_id]/page.tsx` — 所有仓库快照 + movement 时间线（分页）；FIFO profile 下额外渲染 lots 卡片（批次号/剩余数/单价）
- [ ] 验证：`bunx tsc --noEmit` PASS

---

## File List (anticipated)

| 操作 | 路径 |
|------|------|
| create | `migrations/000022_stock_upgrade.up.sql` |
| create | `migrations/000022_stock_upgrade.down.sql` |
| create | `internal/domain/stock/movement.go` |
| create | `internal/domain/stock/movement_test.go` |
| create | `internal/domain/stock/lot.go` |
| create | `internal/app/stock/calculator.go` |
| create | `internal/app/stock/calculator_factory.go` |
| create | `internal/app/stock/calculator_factory_test.go` |
| create | `internal/app/stock/calc_wac.go` |
| create | `internal/app/stock/calc_wac_test.go` |
| create | `internal/app/stock/calc_fifo.go` |
| create | `internal/app/stock/calc_fifo_test.go` |
| create | `internal/app/stock/usecase.go` |
| create | `internal/app/stock/usecase_test.go` |
| create | `internal/app/stock/query.go` |
| create | `internal/adapter/repo/stock/repo.go` |
| create | `internal/adapter/repo/stock/repo_test.go` |
| create | `internal/adapter/handler/stock/handler.go` |
| create | `internal/adapter/handler/stock/handler_test.go` |
| modify | `internal/adapter/handler/router/router.go` |
| modify | `internal/lifecycle/app.go` |
| create | `web/lib/api/stock.ts` |
| create | `web/lib/api/stock.test.ts` |
| create | `web/app/(dashboard)/stock/page.tsx` |
| create | `web/app/(dashboard)/stock/[product_id]/page.tsx` |

不需要新建目录 `internal/domain/stock/` 和 `internal/app/stock/`（mkdir 由开发者在创建第一个文件时完成）。

---

## Dev Notes

### Migration 编号

Decision-lock §4 已分配 000013–000021。本 Story 使用 **000022**（第一个自由编号）。Dev 在开工前必须确认 000016–000021 中是否有任何已在本地执行但尚未写入 decision-lock 的 migration；如有冲突，取下一个可用编号并在 decision-lock §4 追加记录。

### 现有 stock_lot 表结构

`migrations/000006_init_stock.up.sql` 中 `tally.stock_lot` 已有 `qty` 和 `cost_price` 列，**缺少** `qty_remaining`（FIFO 出库需要的剩余数）和 `source_movement_id`（可追溯来源）。Migration 000022 通过 `ALTER TABLE` 追加这两列，**不重建表**，保留已有 `lot_no`/`qty`/`cost_price` 数据。`qty_remaining` 初始化为 `qty`（`ALTER TABLE ... ADD COLUMN qty_remaining NUMERIC(18,4) NOT NULL DEFAULT 0; UPDATE tally.stock_lot SET qty_remaining = qty`）。

### 现有 stock_snapshot 表结构

`migrations/000006_init_stock.up.sql` 中 `stock_snapshot` 已有 `avg_cost_price NUMERIC(18,6)`，但缺少两个新列。Migration 000022 新增 `unit_cost`（语义与 `avg_cost_price` 等同，保留 `avg_cost_price` 兼容旧数据，新代码读写 `unit_cost`）和 `cost_strategy`。

**注意**：architecture DL-5 明确 `stock_snapshot` 不加离线字段（origin/sync_status），这里遵守。

### 精度规则

全程 `github.com/shopspring/decimal`，禁止 `float64`。`NUMERIC(18,6)` 对应 Go 的 `decimal.NewFromString`。WAC 公式中除法需 `.Div(divisor)` 并用 `.Round(6)`。初始库存为 0 时（`on_hand_qty.IsZero()`），新均价直接取 `new_unit_cost`，不做除法。

### FIFO 出库 SQL 策略

FIFO 出库通过 **Go 层迭代 + SQL `SELECT FOR UPDATE`** 实现，不使用 CTE（CTE 在 SQLite edge 模式不兼容）：

```
1. BEGIN tx
2. AcquireAdvisoryLock(tenantID, productID, warehouseID)
3. SELECT * FROM stock_lot WHERE qty_remaining > 0
   ORDER BY received_at ASC FOR UPDATE  -- 锁定行，防并发
4. Go 迭代消耗：remaining_to_ship = qty_base
   for each lot:
     consume = min(lot.qty_remaining, remaining_to_ship)
     lot.qty_remaining -= consume
     remaining_to_ship -= consume
     UPDATE stock_lot SET qty_remaining = ... WHERE id = lot.id
5. 计算加权出库成本：sum(consume_i * cost_i) / total_consumed
6. INSERT stock_movement; UPDATE stock_snapshot
7. COMMIT
```

### Advisory Lock 实现

```go
// advisory lock key = crc32(tenant_id || product_id || warehouse_id) as int64
func advisoryKey(tenantID, productID, warehouseID uuid.UUID) int64 {
    h := fnv.New64a()
    h.Write(tenantID[:])
    h.Write(productID[:])
    h.Write(warehouseID[:])
    return int64(h.Sum64())
}
// SELECT pg_advisory_xact_lock($1)
```

`pg_advisory_xact_lock` 在事务结束时自动释放，无需手动 unlock。

### Post /stock/movements 的 Epic 6/7 契约

**本 Story 暴露 dev 直调 HTTP endpoint 仅用于测试。**Epic 6（采购入库确认）和 Epic 7（出库确认）**不调用此 HTTP endpoint**，而是直接注入并调用 `RecordMovementUseCase.Execute(ctx, req)`（use case 层 interface）。这要求：

- `RecordMovementUseCase` 必须可注入（接收 `StockRepo` + `InventoryCalculator` 接口，不持有 HTTP 依赖）
- Epic 6 story 需要在 `app/purchase/receive_purchase.go` 中注入 `RecordMovementUseCase`，传入 `{direction:"in", reference_type:"purchase", reference_id: bill_id}`
- Epic 7 story 需要在 `app/sales/ship_sales.go` 中注入，传入 `{direction:"out", reference_type:"sale", reference_id: bill_id}`

本 Story 必须在 `usecase.go` 中定义清晰的 `RecordMovementRequest` struct，Epic 6/7 依赖此类型；**不允许** Epic 6/7 绕过 use case 直接写 `stock_movement` 表。

### R-4 硬约束

Decision-lock R-4：库存变更唯一路径是 `InventoryCalculator.ApplyMovement`，**禁止**任何地方直接 `UPDATE stock_snapshot SET on_hand_qty = ...`。Repo 层的 `UpsertSnapshot` 只能由 `calc_wac.go` / `calc_fifo.go` 在事务内调用。

### unitconv 接口

Story 4.1 实现的 `unitconv.ConvertToBase(qty decimal.Decimal, unitID uuid.UUID, units []ProductUnit) (decimal.Decimal, error)`。本 Story 的 `RecordMovementRequest` 需要携带 `UnitID uuid.UUID`，handler 从 product repo 加载 `product.Units []ProductUnit`，然后调 `unitconv.ConvertToBase`，再传给 use case。use case 接收的 `StockMovement.QtyBase` 已是换算后的 base_unit 数量。

### 现有 DB 驱动

项目使用 `jackc/pgx/v5 + database/sql`（story-4.1 Dev Record 明确，无 GORM）。`StockRepo` 用 `*sql.DB` + `*sql.Tx`，保持一致。

### NATS 事件发布（异步，仅 happy path）

事务提交成功后发布 `psi.stock.changed` 到 NATS stream `PSI_EVENTS`，payload 为：

```json
{
  "tenant_id": "...",
  "product_id": "...",
  "warehouse_id": "...",
  "direction": "in|out|adjust",
  "qty_base_delta": 50,
  "new_on_hand_qty": 150,
  "occurred_at": "2026-04-23T..."
}
```

NATS 连接尚未在 lifecycle 中初始化（Epic 1 未做）。本 Story 用 `if natsConn == nil { log.Warn("NATS not configured, skip event") }` 降级，不因 NATS 缺失阻断库存写入。

---

## Flagged Assumptions

1. **migration 000016–000021 现状**: 这些 migration 已在 decision-lock §4 中规划但未在 `migrations/` 目录下看到实际文件（当前 head 是 000015）。本 Story 假设 000022 是当前实际可用的下一个编号。Dev 开工前必须核实，若 000016+ 有任何文件已存在，则改用实际 max+1。

2. **shopspring/decimal 是否在 go.mod**: Story 4.1 Dev Record 中 `unitconv` 使用 `math/big` 内部实现。本 Story 的 WAC/FIFO 计算量更大，强烈建议引入 `github.com/shopspring/decimal`（architecture §4.4 明确要求）。如 go.mod 中尚未有该依赖，dev 需 `go get github.com/shopspring/decimal`。

3. **NATS 连接**: 假设 `lifecycle/app.go` 中无 NATS 连接（Epic 1 未实现）。本 Story 的 NATS 发布采用 nil-check 降级模式。

4. **stock_lot.warehouse_id**: 现有 `stock_lot` 表（000006）无 `warehouse_id` 列，导致 FIFO 需跨仓库查询时无法区分。Migration 000022 **必须** 同时为 `stock_lot` 补加 `warehouse_id UUID NOT NULL REFERENCES tally.warehouse(id)`（现有行允许 NULL，但新插入必须提供）。这是架构隐含约束，epics.md 未明确写出，标记为 Assumption。

5. **ProfileMiddleware 完整度**: Story 4.1 中 `profile.go` 是 stub（从 ctx 读 tenant_id 存根）。本 Story 需要 `profile.InventoryMethod()` 方法返回实际值。假设 stub 的 `InventoryMethod()` 返回 `"wac"`（retail 默认）。真实 Profile 从 DB 读取留到 Story 2.1 wire-up。

---

## Dev Agent Record

(populated by bmad-dev during implementation)
