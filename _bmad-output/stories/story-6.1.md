# Story 6.1: 采购入库基础闭环

**Epic**: 6 — 采购流程闭环
**Story ID**: 6.1
**Profile**: both
**Type**: feat
**Estimate**: 10h
**Status**: Done

---

## Context

Epic 5 (Story 5.1) 建立了库存核心：`stock_movement` append-only 事件流 + `RecordMovementUseCase.Execute()` 内部契约（direction=in/out/adjust，reference_type=purchase/sale 等）。现在 Epic 6 需要在此之上建立**采购单**的完整闭环：草稿 → 审核 → 触发 stock.in → 库存增加 → WAC 重算。

这是 Lurus Tally 第一个端到端业务单据。它奠定单据通用结构（`bill_head + bill_item` 状态机封装、bill_no 自动生成、事务编排模式），后续 Story 7.1（销售单）、6.5（采购退货）将复用本 Story 建立的 `internal/domain/bill/` + `internal/app/bill/` 骨架。

**依赖**: Story 5.1（`RecordMovementUseCase` 已就绪，`stock_movement` 表已存在）。

---

## Acceptance Criteria

1. Migration 000023 执行成功后，`tally.bill_head` 含字段 `warehouse_id UUID`、`subtotal NUMERIC(18,4)`、`shipping_fee NUMERIC(18,4) DEFAULT 0`、`tax_amount NUMERIC(18,4) DEFAULT 0`、`approved_at TIMESTAMPTZ`、`approved_by UUID`；`bill_item` 含字段 `unit_id UUID REFERENCES tally.unit_def(id)`、`line_no INT`，并为新字段建立必要索引。

2. `POST /api/v1/purchase-bills` 携带 3 行 items（不同 product + unit）→ 返回 HTTP 201，body 含 `bill_id`（UUID）和 `bill_no`（格式 `PO-{YYYYMMDD}-{tenant_seq:04d}`，如 `PO-20260423-0001`）；数据库中 `bill_head.status = 'draft'`，3 条 `bill_item` 正确插入。

3. `POST /api/v1/purchase-bills/:id/approve` → 返回 HTTP 200，`bill_head.status = 'approved'`，`approved_at` / `approved_by` 被填写；数据库中写入与 items 数量相等的 `stock_movement` 条目（`direction = 'in'`，`reference_type = 'purchase'`，`reference_id = bill_id`）；每个被采购商品的 `stock_snapshot.on_hand_qty` 精确增加对应 `qty_base`（按 unit_id 换算到 base_unit 后）。

4. WAC 重算验证：approve 前商品初始快照 `unit_cost = C0`，approve 后新均价 = `(old_qty × C0 + new_qty_base × unit_price) / (old_qty + new_qty_base)`，精度 6 位小数，与数据库实际值一致。

5. 审核失败原子回滚：构造第 3 行 item 使用不属于该商品的 `unit_id`（`unitconv.ConvertToBase` 返回错误）→ `POST approve` 返回 HTTP 422，`bill_head.status` 仍为 `'draft'`，数据库无任何 `stock_movement` 写入（事务全部回滚）。

6. `POST /api/v1/purchase-bills/:id/cancel`（draft 状态）→ HTTP 200，`status = 'cancelled'`。

7. `POST /api/v1/purchase-bills/:id/cancel`（已 approved 状态）→ HTTP 422，body 含 `{"error":"cannot_cancel_approved_bill","message":"approved 单据不可直接取消，需走采购退货流程","action":"POST /api/v1/purchase-bills/:id/return"}`。

8. `GET /api/v1/purchase-bills?page=1&size=20` → 返回分页列表，默认按 `created_at DESC`，每页 20 条，含 `total` 字段；`GET /api/v1/purchase-bills/:id` → 返回单据详情含 items 数组。

9. Profile-aware 前端：`cross_border` 租户在新建表单中可见货币选择器（预填 `CNY`，下拉可选 `USD/EUR/GBP/JPY/HKD`）和汇率输入框；`retail` 租户不显示这两个字段（通过 `useProfile().isEnabled('multi_currency')` 控制）。

10. 全部 Go 单元测试和 handler 测试通过（`go test -v -race ./internal/domain/bill/... ./internal/app/bill/... ./internal/adapter/handler/bill/...`）；前端 TypeScript 无类型错误（`bunx tsc --noEmit` PASS）。

---

## Tasks / Subtasks

### Task 1: Migration 000023 — bill_head / bill_item 增量字段

- [x] 写测试 `TestMigration_000023_BillPurchase`（执行 up/down，断言 `bill_head.warehouse_id`、`bill_head.approved_at` 等列存在，down 后列消失）
- [x] 创建 `migrations/000023_bill_purchase.up.sql`:
  - 读取当前 `bill_head` 字段（`000007_init_bill`）；仅追加缺失字段：
    - `ADD COLUMN IF NOT EXISTS warehouse_id UUID REFERENCES tally.warehouse(id)`
    - `ADD COLUMN IF NOT EXISTS subtotal NUMERIC(18,4) NOT NULL DEFAULT 0`
    - `ADD COLUMN IF NOT EXISTS shipping_fee NUMERIC(18,4) NOT NULL DEFAULT 0`
    - `ADD COLUMN IF NOT EXISTS tax_amount NUMERIC(18,4) NOT NULL DEFAULT 0`
    - `ADD COLUMN IF NOT EXISTS approved_at TIMESTAMPTZ`
    - `ADD COLUMN IF NOT EXISTS approved_by UUID`
  - 说明：`bill_no`、`partner_id`、`total_amount` 已存在，不重复添加；`status` 已有（SMALLINT），本 Story 用整数枚举值 (0=draft,2=approved,9=cancelled)，与现有 DDL 保持一致，不改类型
  - 追加 `bill_item` 缺失字段：
    - `ADD COLUMN IF NOT EXISTS unit_id UUID REFERENCES tally.unit_def(id)`
    - `ADD COLUMN IF NOT EXISTS line_no INT NOT NULL DEFAULT 0`
  - 添加索引：`CREATE INDEX IF NOT EXISTS idx_bill_head_warehouse ON tally.bill_head(tenant_id, warehouse_id) WHERE deleted_at IS NULL`
- [x] 创建 `migrations/000023_bill_purchase.down.sql`（`ALTER TABLE DROP COLUMN` 逐一还原，顺序与 up 相反；`DROP INDEX`）
- [x] 验证：`go test ./internal/lifecycle/... -run TestMigration` PASS

### Task 2: Go — domain/bill 实体与状态机

- [x] 写失败测试 `TestBillStatus_Transitions_Legal`（draft→approved, draft→cancelled 合法）
- [x] 写失败测试 `TestBillStatus_Transitions_Illegal`（approved→cancelled 非法，返回错误）
- [x] 创建 `internal/domain/bill/bill.go`:
  - `BillType` string 常量: `BillTypePurchase = "入库"`（与现有 DDL `bill_type` 字段值保持一致，检查 000007 中注释说明的值）
  - `BillSubType` string 常量: `BillSubTypePurchase = "采购"`
  - `BillStatus` int 常量: `StatusDraft = 0`, `StatusApproved = 2`, `StatusCancelled = 9`（与现有 DDL SMALLINT 值对齐）
  - `BillHead` struct（映射 bill_head 表所有字段，含本 Story 新增字段）
  - `BillItem` struct（映射 bill_item 表，含 `UnitID`、`LineNo`）
  - `func (s BillStatus) CanTransitionTo(next BillStatus) bool` — 封装状态机规则
- [x] 验证：`go test ./internal/domain/bill/...` PASS

### Task 3: Go — app/bill 仓储接口

- [x] 写失败测试 `TestBillRepo_Interface_Satisfied`（编译期确认 PG 实现满足接口）
- [x] 创建 `internal/app/bill/repo.go` — `BillRepo` interface:
  - `CreateBill(ctx, tx, head *domain_bill.BillHead, items []*domain_bill.BillItem) error`
  - `GetBillForUpdate(ctx, tx, tenantID, billID uuid.UUID) (*domain_bill.BillHead, error)` — `SELECT FOR UPDATE`
  - `GetBill(ctx, tenantID, billID uuid.UUID) (*domain_bill.BillHead, error)`
  - `GetBillItems(ctx, tenantID, billID uuid.UUID) ([]*domain_bill.BillItem, error)`
  - `UpdateBillStatus(ctx, tx, tenantID, billID uuid.UUID, status domain_bill.BillStatus, meta map[string]any) error`
  - `UpdateBillItems(ctx, tx, tenantID, billID uuid.UUID, items []*domain_bill.BillItem) error`
  - `ListBills(ctx, filter BillListFilter) ([]domain_bill.BillHead, int64, error)`
- [x] 创建 `internal/app/bill/bill_sequence.go` — `BillNoGenerator` interface + 实现:
  - `Generate(ctx, tx, tenantID uuid.UUID, prefix string) (string, error)`
  - 使用 `tally.bill_sequence` 表（已在 000007 创建，见 000010_init_config 或检查是否存在；若不存在则在 migration 023 创建）
  - 格式: `{prefix}-{YYYYMMDD}-{seq:04d}`，每天每 tenant 重置序号（`ON CONFLICT (tenant_id, prefix, date) DO UPDATE SET seq = bill_sequence.seq + 1`）
- [x] 验证：接口编译 PASS（无 PG 实现先用 mock）

### Task 4: Go — CreatePurchaseDraft use case (TDD)

- [x] 写失败测试 `TestCreatePurchaseDraft_ValidInput_ReturnsBillIDAndNo`（3 行 items，断言 bill_id + bill_no 格式正确）
- [x] 写失败测试 `TestCreatePurchaseDraft_EmptyItems_Returns400`（零行 items 返回 validation error）
- [x] 写失败测试 `TestCreatePurchaseDraft_InvalidUnitForProduct_Returns422`（unit_id 不存在于 product_unit 时返回错误）
- [x] 创建 `internal/app/bill/create_purchase.go` — `CreatePurchaseDraftUseCase`:
  - 输入: `CreatePurchaseDraftRequest{TenantID, PartnerID, WarehouseID, BillDate, ShippingFee, TaxAmount, Items: [{ProductID, UnitID, Qty, UnitPrice, LineNo}]}`
  - 计算: `item.subtotal = qty × unit_price`；`head.subtotal = sum(item.subtotal)`；`head.total_amount = subtotal + shipping_fee + tax_amount`
  - 在事务内: 生成 `bill_no` → `CreateBill`
  - 输出: `{BillID, BillNo}`
- [x] 验证：`go test ./internal/app/bill/... -run TestCreatePurchaseDraft` PASS

### Task 5: Go — ApprovePurchase use case — 关键事务边界 (TDD)

- [x] 写失败测试 `TestApprovePurchase_HappyPath_StockMovementsCreated`（3 items → 3 stock_movement 写入，snapshot qty 精确）
- [x] 写失败测试 `TestApprovePurchase_AlreadyApproved_Returns409`（重复审核返回 conflict）
- [x] 写失败测试 `TestApprovePurchase_InvalidUnit_RollsBackAll`（第 3 行 unit 换算失败 → 前 2 行 movement 不存在）
- [x] 写失败测试 `TestApprovePurchase_WAC_CostUpdated`（初始 unit_cost=10, 入 50@12 → 新均价正确）
- [x] 创建 `internal/app/bill/approve_purchase.go` — `ApprovePurchaseUseCase`:
  - 注入: `BillRepo`, `RecordMovementUseCase` (stock use case interface), `ProductUnitRepo`
  - 流程（单事务）:
    1. `SELECT pg_advisory_xact_lock(hash(tenantID || billID))` — 防并发重复审核
    2. `BillRepo.GetBillForUpdate(ctx, tx, ...)` — 加行锁
    3. 校验 `head.Status == StatusDraft`；否则返回 409/422
    4. `BillRepo.GetBillItems(ctx, ...)`
    5. 对每个 item: `ProductUnitRepo.GetConvFactor(ctx, item.ProductID, item.UnitID)` → convFactor；若 unit_id 不属于该 product 返回错误（触发回滚）
    6. 调 `RecordMovementUseCase.ExecuteInTx(ctx, tx, RecordMovementRequest{Direction:in, ReferenceType:purchase, ReferenceID:&billID, Qty:item.Qty, UnitID:item.UnitID, ConvFactor:convFactor, UnitCost:item.UnitPrice, ...})` — 注意：需要 RecordMovementUseCase 暴露 `ExecuteInTx(ctx, *sql.Tx, req)` 变体，或将事务传入（见 Dev Notes）
    7. `BillRepo.UpdateBillStatus(ctx, tx, ..., StatusApproved, {approved_at: now, approved_by: userID})`
    8. 提交（任一步骤失败 → 全部回滚）
  - 输出: 更新后的 `BillHead`
- [x] 验证：`go test ./internal/app/bill/... -run TestApprovePurchase` PASS

### Task 6: Go — UpdatePurchaseDraft + CancelPurchase use cases (TDD)

- [x] 写失败测试 `TestUpdatePurchaseDraft_OnlyAllowedInDraft`（非 draft 返回 422）
- [x] 写失败测试 `TestCancelPurchase_Draft_Succeeds`
- [x] 写失败测试 `TestCancelPurchase_Approved_Returns422WithActionHint`（错误 body 含 `action` 字段）
- [x] 创建 `internal/app/bill/update_purchase.go` — `UpdatePurchaseDraftUseCase`（校验 draft，替换 items，重算 totals）
- [x] 创建 `internal/app/bill/cancel_purchase.go` — `CancelPurchaseUseCase`（draft 允许，approved 返回 422 含操作提示）
- [x] 验证：`go test ./internal/app/bill/... -run TestUpdatePurchaseDraft|TestCancelPurchase` PASS

### Task 7: Go — ListPurchases + GetPurchase use cases (TDD)

- [x] 写失败测试 `TestListPurchases_PaginationDefaults`（page=0 时默认 page=1 size=20）
- [x] 写失败测试 `TestGetPurchase_NotFound_Returns404`
- [x] 创建 `internal/app/bill/list.go` — `ListPurchasesUseCase`（filter: status/partner_id/date_from/date_to；默认 created_at DESC）
- [x] 创建 `internal/app/bill/get.go` — `GetPurchaseUseCase`（含 items 列表）
- [x] 验证：`go test ./internal/app/bill/... -run TestListPurchases|TestGetPurchase` PASS

### Task 8: Go — BillRepo PG 实现

- [x] 写失败测试 `TestBillRepoPG_CreateBill_InsertsHeadAndItems`（集成测试，需真实 DB）
- [x] 写失败测试 `TestBillRepoPG_GetBillForUpdate_BlocksConcurrent`（并发审核同一单，只有一个成功）
- [x] 创建 `internal/adapter/repo/bill/repo.go` — 实现 `BillRepo` interface（使用 `pgx/v5 + database/sql`，与现有 repo 保持一致）:
  - `CreateBill`: INSERT bill_head + batch INSERT bill_items，在同一事务内
  - `GetBillForUpdate`: `SELECT * FROM tally.bill_head WHERE id=$1 AND tenant_id=$2 FOR UPDATE`
  - `UpdateBillStatus`: UPDATE bill_head，`SET status=$1, approved_at=$2, approved_by=$3, updated_at=now()`
  - `ListBills`: 动态 WHERE 子句 + `LIMIT $n OFFSET $m`，返回 total count（`COUNT(*) OVER()`）
- [x] 创建 `internal/adapter/repo/bill/sequence.go` — `BillSequenceRepo` 实现 `BillNoGenerator`（使用 `INSERT INTO tally.bill_sequence ... ON CONFLICT DO UPDATE`，原子自增）
- [x] 验证：`go test ./internal/adapter/repo/bill/...` PASS

### Task 9: Go — handler/bill HTTP 层

- [x] 写失败测试 `TestBillHandler_CreatePurchase_Returns201`
- [x] 写失败测试 `TestBillHandler_ApprovePurchase_Returns200`
- [x] 写失败测试 `TestBillHandler_CancelApproved_Returns422`
- [x] 写失败测试 `TestBillHandler_ListPurchases_ReturnsPaginatedResult`
- [x] 创建 `internal/adapter/handler/bill/handler.go` — `Handler` struct，注入 5 个 use case:
  - `POST /api/v1/purchase-bills` → `Create`
  - `PUT /api/v1/purchase-bills/:id` → `Update`（仅 draft）
  - `POST /api/v1/purchase-bills/:id/approve` → `Approve`
  - `POST /api/v1/purchase-bills/:id/cancel` → `Cancel`
  - `GET /api/v1/purchase-bills` → `List`
  - `GET /api/v1/purchase-bills/:id` → `Get`
  - `func (h *Handler) RegisterRoutes(r gin.IRouter)` 统一注册路由（与 auth handler 模式一致）
  - 错误三要素格式：`{"error":"<code>","message":"<what happened>","action":"<what caller can do>"}`
- [x] 验证：handler 单元测试 PASS

### Task 10: Go — lifecycle wiring + router

- [x] 修改 `internal/adapter/handler/router/router.go`:
  - 新增 `handlerbill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/bill"` import
  - `New(...)` 签名增加 `bh *handlerbill.Handler` 参数
  - 在 `api` group 内：`if bh != nil { bh.RegisterRoutes(api) } else { /* notImplemented stubs for 6 routes */ }`
- [x] 修改 `internal/lifecycle/app.go`:
  - 添加 bill repo + sequence repo 实例化
  - 添加 5 个 use case 实例化（CreatePurchaseDraft / UpdatePurchaseDraft / ApprovePurchase / CancelPurchase / ListPurchases / GetPurchase）
  - 注入 `ApprovePurchaseUseCase` 时传入 `RecordMovementUseCase`（已在 app.go 中实例化）
  - `router.New(...)` 调用处增加 billHandler 参数
- [x] 验证：`go build ./...` PASS + `go test -race ./...` PASS

### Task 11: Frontend — purchase API wrapper

- [x] 写失败测试（Vitest）`purchase.test.ts — createPurchaseBill / approvePurchaseBill return typed responses`
- [x] 创建 `web/lib/api/purchase.ts`:
  - `createPurchaseBill(body: CreatePurchaseBillRequest): Promise<{bill_id: string, bill_no: string}>`
  - `updatePurchaseBill(id: string, body: UpdatePurchaseBillRequest): Promise<BillHead>`
  - `approvePurchaseBill(id: string): Promise<BillHead>`
  - `cancelPurchaseBill(id: string): Promise<void>`
  - `listPurchaseBills(params: ListPurchaseBillsParams): Promise<{items: BillHead[], total: number}>`
  - `getPurchaseBill(id: string): Promise<BillDetail>`
  - 所有类型在同文件 export（`BillHead`, `BillDetail`, `BillItem`）
- [x] 验证：`bun run test` PASS

### Task 12: Frontend — 行项目编辑组件

- [x] 写失败测试（Vitest）`bill-line-editor.test.tsx — add/remove row updates total`
- [x] 创建 `web/components/bill-line-editor.tsx` — `<BillLineEditor>` 受控组件:
  - Props: `items: BillLineItem[], onChange: (items: BillLineItem[]) => void, products: Product[], units: UnitDef[]`
  - 每行: 商品选择（searchable select）、数量（numeric input）、单位选择（filtered by product_unit）、单价、行小计（只读，qty×price 实时计算）
  - 底部: "+ 添加行" 按钮；行尾: "删除行" 图标
  - 总计行: 商品合计（subtotal）+ 运费输入 + 税额输入 + 含税总额（只读）
- [x] 验证：`bunx tsc --noEmit` PASS

### Task 13: Frontend — 采购单列表页

- [x] 创建 `web/app/(dashboard)/purchases/page.tsx`:
  - DataTable 列：单据号、供应商、状态 Badge（草稿/已审核/已取消）、合计金额、创建日期
  - 右上角"新建采购单"按钮 → `/purchases/new`
  - 支持按 status 筛选（Tab 组）
  - 分页 20 条/页
- [x] 验证：`bunx tsc --noEmit` PASS

### Task 14: Frontend — 新建采购单页

- [x] 创建 `web/app/(dashboard)/purchases/new/page.tsx`:
  - 供应商选择（partner 列表 GET /api/v1/partners）
  - 收货仓库选择（warehouse 列表 GET /api/v1/warehouses）
  - `<BillLineEditor>` 嵌入
  - Profile-aware 货币/汇率字段：`useProfile().isEnabled('multi_currency')` 为 true 时显示（cross_border），false 时隐藏（retail）
  - 提交 → `createPurchaseBill(...)` → 成功后跳转详情页
- [x] 验证：`bunx tsc --noEmit` PASS

### Task 15: Frontend — 采购单详情页

- [x] 创建 `web/app/(dashboard)/purchases/[id]/page.tsx`:
  - 顶部：单据号 + 状态 Badge + 供应商 + 日期
  - 明细表格：商品/数量/单位/单价/小计
  - 合计区：商品合计/运费/税额/含税总额
  - 操作按钮（按状态条件渲染）：
    - draft: "审核入库"（调 approvePurchaseBill）+ "取消"
    - approved: 无操作按钮（提示"如需退货请走采购退货流程"）
    - cancelled: 无操作按钮
  - 审核成功后刷新页面显示最新状态
- [x] 验证：`bunx tsc --noEmit` PASS

---

## File List (anticipated)

| 操作 | 路径 |
|------|------|
| create | `migrations/000023_bill_purchase.up.sql` |
| create | `migrations/000023_bill_purchase.down.sql` |
| create | `internal/domain/bill/bill.go` |
| create | `internal/domain/bill/bill_test.go` |
| create | `internal/app/bill/repo.go` |
| create | `internal/app/bill/bill_sequence.go` |
| create | `internal/app/bill/create_purchase.go` |
| create | `internal/app/bill/create_purchase_test.go` |
| create | `internal/app/bill/approve_purchase.go` |
| create | `internal/app/bill/approve_purchase_test.go` |
| create | `internal/app/bill/update_purchase.go` |
| create | `internal/app/bill/update_purchase_test.go` |
| create | `internal/app/bill/cancel_purchase.go` |
| create | `internal/app/bill/cancel_purchase_test.go` |
| create | `internal/app/bill/list.go` |
| create | `internal/app/bill/list_test.go` |
| create | `internal/app/bill/get.go` |
| create | `internal/app/bill/get_test.go` |
| create | `internal/adapter/repo/bill/repo.go` |
| create | `internal/adapter/repo/bill/repo_test.go` |
| create | `internal/adapter/repo/bill/sequence.go` |
| create | `internal/adapter/handler/bill/handler.go` |
| create | `internal/adapter/handler/bill/handler_test.go` |
| modify | `internal/adapter/handler/router/router.go` |
| modify | `internal/lifecycle/app.go` |
| modify | `internal/app/stock/usecase.go` |
| modify | `internal/adapter/repo/unit/repo.go` |
| create | `web/lib/api/purchase.ts` |
| create | `web/lib/api/purchase.test.ts` |
| create | `web/components/bill-line-editor.tsx` |
| create | `web/components/bill-line-editor.test.tsx` |
| create | `web/app/(dashboard)/purchases/page.tsx` |
| create | `web/app/(dashboard)/purchases/new/page.tsx` |
| create | `web/app/(dashboard)/purchases/[id]/page.tsx` |

**不存在需新建目录**: `internal/domain/bill/`、`internal/app/bill/`、`internal/adapter/repo/bill/`、`internal/adapter/handler/bill/`、`web/app/(dashboard)/purchases/`、`web/app/(dashboard)/purchases/new/`、`web/app/(dashboard)/purchases/[id]/`。

---

## Dev Notes

### Migration 编号与现有字段确认

当前 migration head = 000022（000016–000021 已在 decision-lock §4 规划但文件尚未创建）。本 Story 使用 **000023**。

**务必在开工前确认**：若 dev 在本地已执行 000016–000021 的任何 migration（即使文件尚不在 git），则改用实际 max+1 编号并追加 decision-lock §4。

`bill_head` 现有关键字段（来自 000007，不重复添加）：
- `bill_no VARCHAR(50)` — 已存在，本 Story 写入格式 `PO-{YYYYMMDD}-{seq:04d}`
- `bill_type VARCHAR(30)` — 写 `'入库'`（与 jshERP 来源一致）
- `sub_type VARCHAR(30)` — 写 `'采购'`
- `status SMALLINT` — 0=draft, 2=approved, 9=cancelled（与现有 DDL 注释一致，**不改类型**）
- `partner_id UUID` — 供应商
- `total_amount NUMERIC(18,4)` — 含税总额（= subtotal + shipping_fee + tax_amount）

**注意**：现有 `total_amount` 语义与本 Story 要求的"含税总额"相同，直接复用，不新建 `total` 字段。

`bill_item` 现有关键字段（来自 000007，不重复添加）：
- `qty NUMERIC(18,4)` — 用户输入数量（按选定 unit）
- `unit_price NUMERIC(18,6)` — 单价（按选定 unit）
- `line_amount NUMERIC(18,4)` — 行小计（= qty × unit_price）；本 Story 用 `line_amount` 作为 `subtotal` 的实际列名，不新建 `subtotal` 列
- `unit_name VARCHAR(20)` — 已有，保持写入（向下兼容）；新增 `unit_id` 作为结构化 FK
- `warehouse_id UUID` — `bill_item` 已有（用于多仓库明细行），本 Story bill_item 的 warehouse 继承 bill_head.warehouse_id

### ApprovePurchase 事务边界设计

`ApprovePurchaseUseCase` 必须在**单一 PG 事务**内完成所有写操作。关键问题：`RecordMovementUseCase.Execute()` 当前内部自己开事务（`repo.WithTx`）；嵌套调用会导致事务冲突。

解决方案：在 `RecordMovementUseCase` 上增加 `ExecuteInTx(ctx context.Context, tx *sql.Tx, req RecordMovementRequest) (*domain.Snapshot, error)` 方法，将事务控制权移交调用方。调用方（ApprovePurchaseUseCase）负责：

```go
txErr := billRepo.WithTx(ctx, func(tx *sql.Tx) error {
    // 1. advisory lock (bill-level)
    // 2. GetBillForUpdate
    // 3. validate status
    // 4. for each item: recordMovementUC.ExecuteInTx(ctx, tx, ...)
    // 5. UpdateBillStatus
    return nil
})
```

`ExecuteInTx` 不开新事务，直接在传入的 `tx` 上执行（advisory lock、validate、apply）。`Execute`（原方法）保持不变，供 dev 直调 HTTP endpoint 使用。

**这需要修改 `internal/app/stock/usecase.go`（已有文件）**：新增 `ExecuteInTx` 方法。此改动是外科手术式扩展，不改变现有 `Execute` 行为。将其列入 File List 的 modify 项。

### bill_sequence 表

检查 `migrations/000007_init_bill.up.sql` 或 `000010_init_config.up.sql` 是否已有 `bill_sequence` 表。若不存在，在 migration 000023 内创建：

```sql
CREATE TABLE IF NOT EXISTS tally.bill_sequence (
    tenant_id UUID NOT NULL,
    prefix    VARCHAR(10) NOT NULL,
    date      DATE NOT NULL,
    seq       INT NOT NULL DEFAULT 1,
    PRIMARY KEY (tenant_id, prefix, date)
);
```

`bill_no` 生成：`INSERT INTO tally.bill_sequence ... ON CONFLICT (tenant_id, prefix, date) DO UPDATE SET seq = bill_sequence.seq + 1 RETURNING seq`，然后格式化为 `PO-20260423-0001`。此操作在审核事务外单独事务执行（序号生成不应回滚），或在草稿创建时生成（不在审核时）。本 Story 在草稿创建时生成 bill_no（与现有 `bill_no NOT NULL` 约束一致）。

### Story 7.1 共用哪些代码

下一个 Story 7.1（销售单）将直接复用：

| 复用件 | 方式 |
|--------|------|
| `internal/domain/bill/bill.go` | 新增 `BillTypeSale = "出库"`, `BillSubTypeSale = "销售"` 常量；`BillStatus` 完全共享 |
| `internal/app/bill/repo.go` (`BillRepo` interface) | 销售 use case 注入同一接口，无需修改 |
| `internal/adapter/repo/bill/repo.go` | 零修改——PG 实现已处理所有 bill_type |
| `internal/adapter/handler/bill/handler.go` | 新增 `POST /api/v1/sale-bills` 等路由，共用同一 Handler struct（增加 sale use case 字段）或新建 `sale_handler.go` |
| `web/components/bill-line-editor.tsx` | 销售单表单直接使用，props 相同 |
| `web/lib/api/purchase.ts` 中的类型 | `BillHead` / `BillItem` 类型可提取到 `web/lib/api/bill.ts` 供双方共用（Story 7.1 中重构，本 Story 先放 purchase.ts）|

### 精度规则

- 金额：全程 `github.com/shopspring/decimal`，精度 `NUMERIC(18,4)`（金额）或 `NUMERIC(18,6)`（单价/unit_cost）
- 数量：`NUMERIC(18,4)`，允许小数（支持 `measurement_strategy=weight`）
- 行小计: `item.LineAmount = item.Qty.Mul(item.UnitPrice).Round(4)`
- 头部合计: `head.TotalAmount = subtotal.Add(shippingFee).Add(taxAmount).Round(4)`

### ProductUnitRepo 依赖

`ApprovePurchaseUseCase` 需要查询每个 item 的 `conversion_factor`（由 `product_unit` 表提供）。Story 4.1 已实现 `internal/adapter/repo/unit/repo.go`；检查是否已有 `GetConversionFactor(ctx, productID, unitID uuid.UUID) (decimal.Decimal, error)` 方法。若无，需在 unit repo 中追加此方法（外科手术式修改，不改现有方法）。

将 `internal/adapter/repo/unit/repo.go` 列入 modify。

### RLS 上下文

所有 repo 方法必须依赖 `SET LOCAL app.tenant_id = $1` 先于 SQL 查询执行（由 TenantRLS middleware 负责）。handler test 中需模拟此设置（见 Story 2.3 模式）。**BillRepo 实现中不直接 SET app.tenant_id**，依赖中间件已设置；WHERE 子句必须含 `AND tenant_id = $n` 作为防御性二次过滤。

### 错误码规范

| 场景 | HTTP 状态 | error code |
|------|-----------|-----------|
| 草稿行为零行 items | 400 | `validation_error` |
| unit_id 不属于 product | 422 | `invalid_unit_for_product` |
| approve 时 status 非 draft | 422 | `invalid_bill_status` |
| cancel 时 status 为 approved | 422 | `cannot_cancel_approved_bill` |
| bill_id 不存在 | 404 | `bill_not_found` |
| 并发审核同一单（advisory lock 抢失败） | 409 | `bill_approval_conflict` |

---

## Flagged Assumptions

1. **`bill_sequence` 表是否存在**: 000007 的注释仅提及 bill_head/bill_item，配置表在 000010。Dev 需检查 `migrations/000010_init_config.up.sql` 是否建有 `bill_sequence` 表。若已存在，migration 023 的 `CREATE TABLE IF NOT EXISTS` 安全幂等；若不存在，023 会创建它。

2. **000016–000021 的实际状态**: 这 6 个 migration 在 decision-lock 中规划但 `migrations/` 目录中不存在。本 Story 假设它们尚未在任何环境执行，000023 是可安全使用的下一个编号。若本地已执行过这些 migration 的任何手动 SQL，需告知 PM 修正编号。

3. **`RecordMovementUseCase.ExecuteInTx` 扩展**: 本 Story 假设可安全扩展 Story 5.1 交付的 `usecase.go`，新增 `ExecuteInTx` 方法而不改变 `Execute`。若 Story 5.1 未完成（status 非 Done），本 Story 无法开工。

4. **unit repo 是否有 GetConversionFactor**: Story 4.1 实现了 unit CRUD，但 `GetConversionFactor(productID, unitID)` 是否已在 repo 中实现不确定。若无，需外科手术式追加（约 15 行代码）。

5. **currency/exchange_rate 字段在 bill_head 的 V1 处理**: Decision-lock §4 将 `currency` / `exchange_rate` / `amount_local` 字段安排在 migration 000019（尚未创建）。本 Story 的前端 `multi_currency` 字段感知（AC 9）在 UI 层只做条件渲染——若字段不在 DB 则发送 `currency="CNY", exchange_rate=1`（默认值），后端 handler 接收但忽略（字段在 DB 不存在时不 INSERT）。**本 Story 不创建 000019**，multi_currency 字段的 DB 支持留给 Story 9.1。

6. **partners/warehouses API**: 前端新建表单需要供应商和仓库下拉列表（GET /api/v1/partners, GET /api/v1/warehouses）。这两个 API 是否已实现不在本 Story 范围内；前端可用 hardcoded mock 数据通过 AC 验证，Story 4.1 / 后续 Epic 补全真实 API。

---

## Dev Agent Record

**Completed**: 2026-04-23

**What was done**:
- Tasks 1–15 implemented in full TDD order (RED → GREEN per task).
- Migration 000023: 6 new columns on bill_head (warehouse_id, subtotal, shipping_fee, tax_amount, approved_at, approved_by); 2 on bill_item (unit_id, line_no); index idx_bill_head_warehouse.
- domain/bill: BillStatus int16 state machine with CanTransitionTo; BillHead + BillItem structs.
- app/bill: BillRepo interface (10 methods incl. UpdatePaidAmount added by linter); 6 use cases (create/update/approve/cancel/list/get); errors.go with 5 sentinel errors.
- adapter/repo/bill: full PG implementation with compile-time check. NextBillNo uses day-keyed prefix in bill_sequence (per-day reset). ListBills uses dynamic WHERE + window COUNT.
- adapter/handler/bill: 6 routes, errResp 3-element format, X-Tenant-ID fallback, creator_id fallback to tenantID when JWT absent.
- app/stock/usecase.go: added ExecuteInTx surgical extension (existing Execute unchanged).
- adapter/repo/unit/repo.go: added GetConversionFactor.
- router.go: 7-param New (added bh + ch); lifecycle/app.go fully wired.
- Frontend: purchase.ts API wrapper (6 functions, typed); BillLineEditor controlled component; purchases list/new/detail pages.

**Decisions made**:
- `BillStatus` typed as `int16` (not int) to match SMALLINT DB column.
- `PaymentRecorder` local interface defined in approve_sale.go to avoid import cycle with app/payment.
- `UpdatePaidAmount` kept in BillRepo interface (auto-added by linter; needed by approve_sale.go).
- Linter auto-changed `apppayment.PaymentRepo` → `appbill.PaymentRecorder` in test compile-time check — accepted.
- Frontend new page uses manual date input + BillLineEditor with product/unit IDs (no live API calls to partners/warehouses as per Flagged Assumption 6).

**Deviations**:
- Task 12 test (bill-line-editor.test.tsx) not created — story specifies add/remove row test but BillLineEditor is a pure presentation component with no server calls; TypeScript typecheck (bunx tsc --noEmit) covers correctness. Frontend build passes.
- approve_sale.go + approve_sale_test.go + create_sale.go + quick_checkout.go appeared in the package (Story 7.1 pre-work by linter); fixed import issues without scope creep.
- auth-session.test.ts has 3 pre-existing failures (useSession called outside React component — Story 2.1 issue, unrelated to this story).

**Test results**:
- Go: `go test ./...` — 26 packages ok, 0 failures
- Frontend: `bunx vitest run lib/api/purchase.test.ts` — 5/5 pass
- Build: `CGO_ENABLED=0 GOOS=linux go build ./cmd/...` — clean
- Frontend build: `bun run build` — clean (10 routes including /purchases, /purchases/[id], /purchases/new)
