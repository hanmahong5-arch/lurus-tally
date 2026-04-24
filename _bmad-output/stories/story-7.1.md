# Story 7.1: 销售出库基础闭环

**Epic**: 7 — 销售流程闭环
**Story ID**: 7.1
**Profile**: both
**Type**: feat
**Estimate**: 12h
**Status**: Draft

---

## Context

Epic 5 (Story 5.1) 建立了 `RecordMovementUseCase` 契约（`Direction="out"`, `ReferenceType="sale"`），Epic 6 将采用同一 `bill_head/bill_item` 表实现采购入库。本 Story 在此基础上完成销售出库的对称侧：业务员录入销售单 → 审核触发 `RecordMovementUseCase.Execute` 写出库 movement → 库存减少 → 收款记录写入 `payment_head`。

注意：`payment_head` 已在 `migrations/000008_init_finance.up.sql` 中存在（含 `related_bill_id` FK），本 Story **不新增 migration**，直接复用现有表；`payment_head.pay_type` 字段用于存储收款方式（cash/wechat/alipay/card/credit/transfer）。

---

## Acceptance Criteria

1. `POST /api/v1/sale-bills` 创建草稿 → `bill_head` 写入一行，`bill_type='出库'`，`sub_type='销售'`，`status=0`；响应含 `id` + `bill_no`。

2. `POST /api/v1/sale-bills/:id/approve` 审核 → 对每个 `bill_item` 调用 `RecordMovementUseCase.Execute({Direction:"out", ReferenceType:"sale", ReferenceID:bill_id})`；所有 item 在同一事务内完成后 `bill_head.status` 更新为 2；`stock_snapshot.on_hand_qty` 精确减少；WAC 下 `unit_cost` 不变。

3. **库存不足回滚**：approve 时任一 item 的 `qty_base > stock_snapshot.on_hand_qty` → 整单所有 movement 全部回滚（advisory lock 序列化保证），`bill_head.status` 保持 0，响应 HTTP 422，body 含 `{"error":"insufficient_stock","product_id":"...","available":"N","requested":"M"}`。

4. `POST /api/v1/sale-bills/:id/approve` 带 `paid_amount > 0` → 在同一事务内 upsert `payment_head` 一行，`related_bill_id=bill_id`，`amount=paid_amount`，`pay_type=payment_method`，`pay_date=now()`；`bill_head.paid_amount` 更新为 `paid_amount`。

5. **赊账场景**：`paid_amount=0`（或省略）→ bill 正常 approve，不写 `payment_head`，`bill_head.paid_amount=0`；`GET /api/v1/sale-bills/:id` 响应的 `receivable_amount = total_amount - paid_amount`。

6. **多次还款**：`POST /api/v1/payments` body `{bill_id, amount, payment_method}` → 追加一条 `payment_head`，`bill_head.paid_amount` 累加；`receivable_amount` 相应减少；`bill_head.status` 仍为 2（付款完结状态另设，超出 MVP 范围）。

7. **POS 快速结账**：`POST /api/v1/sale-bills/quick-checkout` body `{items:[{product_id,warehouse_id,qty,unit_id?,unit_price}], payment_method, paid_amount, customer_name?}` → 单事务内 create draft + approve + record payment → HTTP 201，响应含 `{bill_id, bill_no, total_amount, receivable_amount}`。全程 ≤ 500ms（无外部调用，只有本地 DB 事务）。

8. `GET /api/v1/sale-bills` 支持 `status`, `partner_id`, `date_from`, `date_to`, `page`, `page_size` 过滤，返回列表含 `receivable_amount`。

9. `GET /api/v1/sale-bills/:id` 返回 bill head + items（含 `product_name`, `unit_name`）+ payments 列表 + `receivable_amount`。

10. **Profile-aware 前端**：retail 租户进入 `/sales/new`，页面顶部显示「快速结账」大按钮（cash/wechat/alipay 三个快捷方式），完整表单折叠在「高级」区域；cross_border 租户页面默认展开完整表单，「快速结账」折叠。

11. 所有 Go unit test + handler test `go test ./...` PASS；前端 `bunx tsc --noEmit` + `bun run build` PASS。

---

## Tasks / Subtasks

### Task 1: Domain — bill entity (sale variant)

- [ ] 写失败测试 `TestBillHead_SaleType_Valid`（断言 `bill_type='出库'`, `sub_type='销售'` 枚举值有效）
- [ ] 创建 `internal/domain/bill/bill.go`：
  - `BillHead` struct（对应 `tally.bill_head` 列，含 `PaidAmount`, `TotalAmount`, `Status` SMALLINT 枚举 0/1/2/3/4/9）
  - `BillItem` struct（对应 `tally.bill_item`，含 `QtyBase decimal.Decimal`）
  - 常量：`BillTypeOut = "出库"`, `SubTypeSale = "销售"`, `StatusDraft = 0`, `StatusApproved = 2`, `StatusCancelled = 9`
  - `ReceivableAmount() decimal.Decimal` 计算方法（`TotalAmount - PaidAmount`，非负 clamp）
- [ ] 创建 `internal/domain/bill/bill_test.go`
- [ ] 验证：`go test ./internal/domain/bill/...` PASS

### Task 2: Domain — payment entity

- [ ] 写失败测试 `TestPayment_PayType_Valid`（断言 pay_type 枚举：cash/wechat/alipay/card/credit/transfer）
- [ ] 创建 `internal/domain/payment/payment.go`：
  - `Payment` struct（对应 `tally.payment_head`：`ID`, `TenantID`, `BillID uuid.UUID`（映射 `related_bill_id`）, `PayType`, `Amount decimal.Decimal`, `PayDate time.Time`, `PartnerID *uuid.UUID`, `CreatorID uuid.UUID`, `Remark string`）
  - `PayType` 类型 + 枚举常量 + `Validate()` 方法
- [ ] 创建 `internal/domain/payment/payment_test.go`
- [ ] 验证：`go test ./internal/domain/payment/...` PASS

### Task 3: Repo — bill repo interface + PG implementation

- [ ] 写失败测试 `TestBillRepo_CreateAndGet_RoundTrip`（使用 testcontainers 或 test-db DSN，断言写后读一致）
- [ ] 创建 `internal/adapter/repo/bill/repo.go`，实现接口：
  - `CreateDraft(ctx, tx, head *domain.BillHead, items []*domain.BillItem) error`（在事务内写 bill_head + bill_items，`bill_no` 用 `'SL' + YYYYMMDD + 4位序号` 格式生成）
  - `GetByID(ctx, tenantID, billID uuid.UUID) (*domain.BillHead, []*domain.BillItem, error)`
  - `List(ctx, filter BillFilter) ([]*domain.BillHead, int64, error)`（BillFilter：status, partner_id, date_from/to, page, page_size）
  - `UpdateStatus(ctx, tx, tenantID, billID uuid.UUID, status int16, paidAmount decimal.Decimal) error`
  - `Cancel(ctx, tx, tenantID, billID uuid.UUID) error`（status → 9）
  - `WithTx(ctx, fn func(*sql.Tx) error) error`（复用 db 级事务，与 stock repo 模式一致）
- [ ] 创建 `internal/adapter/repo/bill/repo_test.go`
- [ ] 验证：repo 单元测试 PASS

### Task 4: Repo — payment repo

- [ ] 写失败测试 `TestPaymentRepo_RecordAndList_RoundTrip`
- [ ] 创建 `internal/adapter/repo/payment/repo.go`：
  - `Record(ctx, tx, p *domain.Payment) error`（INSERT payment_head；`pay_date` = `p.PayDate`，若零值用 `now()`）
  - `ListByBill(ctx, tenantID, billID uuid.UUID) ([]*domain.Payment, error)`
  - `SumByBill(ctx, tx, tenantID, billID uuid.UUID) (decimal.Decimal, error)`（`SELECT COALESCE(SUM(amount),0) WHERE related_bill_id=? AND deleted_at IS NULL`）
- [ ] 创建 `internal/adapter/repo/payment/repo_test.go`
- [ ] 验证：`go test ./internal/adapter/repo/payment/...` PASS

### Task 5: Use case — create sale draft

- [ ] 写失败测试 `TestCreateSaleDraft_ValidRequest_ReturnsBillID`（mock billRepo，断言 CreateDraft 被调用一次）
- [ ] 写失败测试 `TestCreateSaleDraft_EmptyItems_ReturnsError`（空 items 返回 validation error）
- [ ] 创建 `internal/app/bill/create_sale.go`：
  - `CreateSaleUseCase.Execute(ctx, req CreateSaleRequest) (*domain.BillHead, error)`
  - `CreateSaleRequest`：`{TenantID, PartnerID *uuid.UUID, CreatorID uuid.UUID, BillDate time.Time, Items []SaleItem, PayType string, Remark string}`
  - `SaleItem`：`{ProductID, WarehouseID uuid.UUID, Qty decimal.Decimal, UnitID *uuid.UUID, ConvFactor string, UnitPrice decimal.Decimal}`
  - 校验：items 不能为空；每个 item qty > 0；unit_price >= 0
  - 计算 `bill_item.line_amount = qty * unit_price`；`bill_head.total_amount = sum(line_amounts)`
  - 调 `billRepo.CreateDraft(ctx, tx, head, items)`
- [ ] 创建 `internal/app/bill/create_sale_test.go`
- [ ] 验证：`go test ./internal/app/bill/... -run TestCreateSale` PASS

### Task 6: Use case — approve sale (出库 + 首次收款)

- [ ] 写失败测试 `TestApproveSale_AllItemsInStock_ApproveSucceeds`（mock stockUC + billRepo + paymentRepo，断言 RecordMovement 被调用 len(items) 次，status 更新为 2）
- [ ] 写失败测试 `TestApproveSale_OneItemInsufficient_RollsBack`（第二个 item 触发 InsufficientStockError，断言 status 保持 0，paymentRepo.Record 未调用）
- [ ] 写失败测试 `TestApproveSale_WithPaidAmount_RecordsPayment`（paid_amount > 0，断言 paymentRepo.Record 被调用一次）
- [ ] 写失败测试 `TestApproveSale_ZeroPaidAmount_SkipsPayment`（paid_amount = 0，断言 paymentRepo.Record 未调用）
- [ ] 创建 `internal/app/bill/approve_sale.go`：
  - `ApproveSaleUseCase.Execute(ctx, req ApproveSaleRequest) error`
  - `ApproveSaleRequest`：`{TenantID, BillID, CreatorID uuid.UUID, PaidAmount decimal.Decimal, PayType string}`
  - 事务流程（单 `WithTx` 包裹全部步骤）：
    1. `billRepo.GetByID` 加载 head + items；校验 status == 0（draft）
    2. 逐 item 调 `stockUC.Execute({Direction:"out", ReferenceType:"sale", ReferenceID:&billID, ...})`；遇 `*stock.InsufficientStockError` 立即返回，事务回滚
    3. `billRepo.UpdateStatus(ctx, tx, tenantID, billID, StatusApproved, paidAmount)`
    4. 若 `paidAmount.GreaterThan(decimal.Zero)` → `paymentRepo.Record(ctx, tx, payment)`
  - 注意：`stockUC.Execute` 内部开启自己的事务（含 advisory lock）；`ApproveSaleUseCase` 应使用 **外层事务包裹 bill status + payment 写入**，stock movement 则通过 `WithTx` 嵌套（pgx 支持 savepoint 嵌套或使用 `BEGIN; ... COMMIT` 连接级事务）。
    - **实际设计**：`RecordMovementUseCase` 接受可选外层 `*sql.Tx`；若为 nil 则自开事务。`ApproveSaleUseCase` 开启外层事务，将 tx 传给每次 `Execute` 调用，最终 commit/rollback 全部操作。需在 `RecordMovementUseCase` 增加 `ExecuteInTx(ctx, tx, req)` 变体。
- [ ] 创建 `internal/app/bill/approve_sale_test.go`
- [ ] 验证：`go test ./internal/app/bill/... -run TestApproveSale` PASS

### Task 7: Use case — POS quick checkout

- [ ] 写失败测试 `TestQuickCheckout_ValidRequest_ReturnsBillID`（断言 create + approve + payment 均调用，返回 bill_id）
- [ ] 写失败测试 `TestQuickCheckout_InsufficientStock_Returns422`（approve 内触发 insufficient stock，整体回滚）
- [ ] 创建 `internal/app/bill/quick_checkout.go`：
  - `QuickCheckoutUseCase.Execute(ctx, req QuickCheckoutRequest) (*QuickCheckoutResult, error)`
  - `QuickCheckoutRequest`：`{TenantID, CreatorID uuid.UUID, CustomerName string, Items []SaleItem, PaymentMethod string, PaidAmount decimal.Decimal}`
  - 单事务内：CreateDraft → ApproveWithinTx → RecordPayment（复用 `ApproveSaleUseCase.ExecuteInTx`）
  - `QuickCheckoutResult`：`{BillID, BillNo string, TotalAmount, ReceivableAmount decimal.Decimal}`
- [ ] 创建 `internal/app/bill/quick_checkout_test.go`
- [ ] 验证：`go test ./internal/app/bill/... -run TestQuickCheckout` PASS

### Task 8: Use case — record payment (赊账后还款)

- [ ] 写失败测试 `TestRecordPayment_ApprovedBill_UpdatesPaidAmount`（断言 paymentRepo.Record + billRepo.UpdateStatus paid_amount 累加）
- [ ] 写失败测试 `TestRecordPayment_DraftBill_ReturnsError`（bill 未 approve 时拒绝收款）
- [ ] 创建 `internal/app/payment/record.go`：
  - `RecordPaymentUseCase.Execute(ctx, req RecordPaymentRequest) error`
  - `RecordPaymentRequest`：`{TenantID, BillID, CreatorID uuid.UUID, Amount decimal.Decimal, PayType string, Remark string}`
  - 校验：bill status == 2（approved）；amount > 0
  - 事务：`paymentRepo.Record` + `paymentRepo.SumByBill` 计算新累计值 + `billRepo.UpdateStatus(..., newPaidAmount)`
- [ ] 写失败测试 `TestListPayments_ByBillID_ReturnsAll`
- [ ] 创建 `internal/app/payment/list.go`：`ListPaymentsUseCase.Execute(ctx, tenantID, billID) ([]*domain.Payment, error)`
- [ ] 创建 `internal/app/payment/record_test.go` + `list_test.go`
- [ ] 验证：`go test ./internal/app/payment/...` PASS

### Task 9: RecordMovementUseCase — ExecuteInTx 变体

- [ ] 写失败测试 `TestRecordMovement_ExecuteInTx_UsesProvidedTx`（断言传入的 tx 被 repo 方法使用，无新 BEGIN）
- [ ] 在 `internal/app/stock/usecase.go` 增加 `ExecuteInTx(ctx, tx *sql.Tx, req RecordMovementRequest) (*domain.Snapshot, error)`（步骤与 `Execute` 相同，但跳过 `WithTx` 包裹，直接用传入的 tx）
- [ ] 更新 `internal/app/stock/usecase_test.go`
- [ ] 验证：`go test ./internal/app/stock/... -run TestRecordMovement_ExecuteInTx` PASS

### Task 10: HTTP handler — sale bills

- [ ] 写失败测试 `TestSaleHandler_CreateDraft_Returns201`
- [ ] 写失败测试 `TestSaleHandler_Approve_Returns200`
- [ ] 写失败测试 `TestSaleHandler_Approve_InsufficientStock_Returns422`
- [ ] 写失败测试 `TestSaleHandler_QuickCheckout_Returns201`
- [ ] 写失败测试 `TestSaleHandler_List_Returns200`
- [ ] 写失败测试 `TestSaleHandler_GetByID_Returns200WithPayments`
- [ ] 创建 `internal/adapter/handler/bill/sale_handler.go`：
  - `SaleHandler` struct 持有 `createUC`, `approveUC`, `quickCheckoutUC`, `listPaymentsUC`
  - `POST /api/v1/sale-bills` → `createUC.Execute`
  - `PUT /api/v1/sale-bills/:id` → （本 Story：返回 501，Task 留空，完整编辑在 Story 7.2）
  - `POST /api/v1/sale-bills/:id/approve` body `{paid_amount?, payment_method?}` → `approveUC.Execute`；`*stock.InsufficientStockError` → 422
  - `POST /api/v1/sale-bills/:id/cancel` → billRepo.Cancel（直接 repo 调用，无专用 use case）
  - `GET /api/v1/sale-bills` → `listUC.Execute`（注：list use case 在 Task 3 repo 中一并实现）
  - `GET /api/v1/sale-bills/:id` → billRepo.GetByID + listPaymentsUC，组装响应含 `receivable_amount`
  - `POST /api/v1/sale-bills/quick-checkout` → `quickCheckoutUC.Execute`
  - `X-Tenant-ID` header 读取（与 product handler 模式一致，待 Story 2.3 中间件接入后自动替换）
- [ ] 创建 `internal/adapter/handler/bill/sale_handler_test.go`
- [ ] 验证：handler 单元测试 PASS

### Task 11: HTTP handler — payments

- [ ] 写失败测试 `TestPaymentHandler_Record_Returns201`
- [ ] 写失败测试 `TestPaymentHandler_List_Returns200`
- [ ] 创建 `internal/adapter/handler/payment/handler.go`：
  - `POST /api/v1/payments` body `{bill_id, amount, payment_method, remark?}` → `recordPaymentUC.Execute`
  - `GET /api/v1/payments?bill_id=...` → `listPaymentsUC.Execute`
- [ ] 创建 `internal/adapter/handler/payment/handler_test.go`
- [ ] 验证：`go test ./internal/adapter/handler/payment/...` PASS

### Task 12: Router + lifecycle wiring

- [ ] 写失败测试 `TestRouter_SaleRoutes_Registered`（断言 `/api/v1/sale-bills` + `/api/v1/payments` 路由存在，返回非 404）
- [ ] 修改 `internal/adapter/handler/router/router.go`：
  - 添加 `*handlerbill.SaleHandler` + `*handlerpayment.Handler` 参数
  - 注册路由组 `/api/v1/sale-bills` + `/api/v1/payments`
  - 沿用 `saleHandler(sh, fn)` + `paymentHandler(ph, fn)` nil-safe 模式
- [ ] 修改 `internal/lifecycle/app.go`：
  - wire `BillRepo` → `CreateSaleUseCase` → `ApproveSaleUseCase` → `QuickCheckoutUseCase` → `SaleHandler`
  - wire `PaymentRepo` → `RecordPaymentUseCase` → `ListPaymentsUseCase` → `PaymentHandler`
  - 将 `stockUC.ExecuteInTx` 注入 `ApproveSaleUseCase`
- [ ] 验证：`go build ./...` PASS + `go test ./...` PASS

### Task 13: Frontend — sale list page

- [ ] 写失败测试（Vitest）`sale.test.ts — listSaleBills returns typed array`
- [ ] 创建 `web/lib/api/sale.ts`：`createSaleBill / approveSaleBill / quickCheckout / listSaleBills / getSaleBill` fetch wrappers，类型化响应
- [ ] 创建 `web/app/(dashboard)/sales/page.tsx`：DataTable 展示销售单列表；列：单号/客户/日期/金额/应收/状态；状态色：草稿灰/审核蓝/取消红
- [ ] 验证：`bunx tsc --noEmit` PASS

### Task 14: Frontend — new sale page (profile-aware)

- [ ] 创建 `web/app/(dashboard)/sales/new/page.tsx`：
  - `useProfile()` 读取当前 profile
  - retail：顶部「快速结账」区域（商品行 + 数量 + cash/wechat/alipay 三快捷按钮）默认展开；完整表单（客户/备注/日期）在「高级」折叠区
  - cross_border：完整表单默认展开；「快速结账」折叠
  - 复用（或创建）`web/components/sale-line-editor.tsx`（行项：商品选择 + 单位 + 数量 + 单价 + 金额；与 bill-line-editor 同结构，如已存在则复用）
- [ ] 验证：`bunx tsc --noEmit` PASS

### Task 15: Frontend — sale detail page

- [ ] 创建 `web/lib/api/payment.ts`：`recordPayment / listPayments` fetch wrappers
- [ ] 创建 `web/components/payment-form.tsx`：收款表单（金额 + 方式选择 + 备注，提交调 `POST /api/v1/payments`）
- [ ] 创建 `web/app/(dashboard)/sales/[id]/page.tsx`：
  - 显示 bill head（客户/单号/日期/总额/应收余额/状态）
  - DataTable 明细行（商品/数量/单价/金额）
  - 收款记录列表（时间/方式/金额）
  - 底部「收款」按钮（status==2 且 receivable_amount>0 时显示）→ 弹出 `PaymentForm`
  - 「审核」按钮（status==0 时显示）→ 调 approve endpoint
- [ ] 验证：`bunx tsc --noEmit` PASS + `bun run build` PASS

---

## File List (anticipated)

| 操作 | 路径 |
|------|------|
| create | `internal/domain/bill/bill.go` |
| create | `internal/domain/bill/bill_test.go` |
| create | `internal/domain/payment/payment.go` |
| create | `internal/domain/payment/payment_test.go` |
| create | `internal/adapter/repo/bill/repo.go` |
| create | `internal/adapter/repo/bill/repo_test.go` |
| create | `internal/adapter/repo/payment/repo.go` |
| create | `internal/adapter/repo/payment/repo_test.go` |
| create | `internal/app/bill/create_sale.go` |
| create | `internal/app/bill/create_sale_test.go` |
| create | `internal/app/bill/approve_sale.go` |
| create | `internal/app/bill/approve_sale_test.go` |
| create | `internal/app/bill/quick_checkout.go` |
| create | `internal/app/bill/quick_checkout_test.go` |
| create | `internal/app/payment/record.go` |
| create | `internal/app/payment/record_test.go` |
| create | `internal/app/payment/list.go` |
| create | `internal/app/payment/list_test.go` |
| modify | `internal/app/stock/usecase.go` (add ExecuteInTx) |
| modify | `internal/app/stock/usecase_test.go` |
| create | `internal/adapter/handler/bill/sale_handler.go` |
| create | `internal/adapter/handler/bill/sale_handler_test.go` |
| create | `internal/adapter/handler/payment/handler.go` |
| create | `internal/adapter/handler/payment/handler_test.go` |
| modify | `internal/adapter/handler/router/router.go` |
| modify | `internal/lifecycle/app.go` |
| create | `web/lib/api/sale.ts` |
| create | `web/lib/api/sale.test.ts` |
| create | `web/lib/api/payment.ts` |
| create | `web/components/sale-line-editor.tsx` |
| create | `web/components/payment-form.tsx` |
| create | `web/app/(dashboard)/sales/page.tsx` |
| create | `web/app/(dashboard)/sales/new/page.tsx` |
| create | `web/app/(dashboard)/sales/[id]/page.tsx` |

不需要新 migration：`payment_head` 已存在于 `migrations/000008_init_finance.up.sql`，`bill_head/bill_item` 已存在于 `migrations/000007_init_bill.up.sql`。

---

## Dev Notes

### 事务边界设计（关键）

`ApproveSaleUseCase` 需要在单一 PG 事务内完成：(a) 每个 item 的 stock movement，(b) bill status 更新，(c) payment 写入。但 `RecordMovementUseCase.Execute` 当前自开事务（`repo.WithTx`）。

解决方案：在 Task 9 中新增 `ExecuteInTx(ctx, tx *sql.Tx, req)` 变体——使用调用方提供的外层 `tx`，跳过 `WithTx`，但仍执行 advisory lock（`pg_advisory_xact_lock` 在该事务内生效）。`ApproveSaleUseCase` 用 `billRepo.WithTx` 开启外层事务，传 tx 给每次 `ExecuteInTx`，最后写 bill status + payment 后 commit。

这保证：任一 item 出库失败 → 外层事务 rollback → bill status 和 payment 记录都不落地。

### payment_head 字段映射

`tally.payment_head` 现有字段中：
- `pay_type` → 存收款方式（cash/wechat/alipay/card/credit/transfer）
- `related_bill_id` → FK 到 `bill_head.id`（已有）
- `amount` + `total_amount` → 写相同值（本 Story 无折扣，`discount_amount=0`）
- `partner_id` → 从 bill_head.partner_id 读取（可为 nil，quick-checkout 场景客户名存 `remark`）
- `pay_date` → approve/checkout 时的 `now()`

`payment_head` 无 `status` 字段（现有 schema），赊账场景通过 `bill_head.paid_amount < bill_head.total_amount` 判断应收余额，不在 payment_head 层面加状态。

### bill_no 生成规则

`SL` + `YYYYMMDD` + 4 位序号（当日自增，从 DB `SELECT COUNT(*)+1 WHERE bill_date = today AND sub_type='销售'`）。并发写入时序号可能重复，使用 `INSERT ... ON CONFLICT DO NOTHING` + 重试，或使用 sequence（简单实现）。V1 可用 `gen_random_uuid()` 的前 8 位代替序号（无需 sequence）。

### 出库 unit_cost 传入规则

`ApplyMovement` 中 out 方向的 `unit_cost` 由 WAC 计算器从 `stock_snapshot.unit_cost`（当前均价）读取，**不从 bill_item.unit_price 读取**（销售价≠成本价）。`RecordMovementRequest.UnitCost` 在 `ApproveSaleUseCase` 中传 `decimal.Zero`，让 calculator 自行从 snapshot 读当前均价。WAC `ApplyMovement` 出库时已实现 `unit_cost = snapshot.UnitCost`（不变）。

### profile-aware 前端约定

`useProfile()` hook 来自 Epic 3 Story 3.7（已在路线图中，本 Story 假设其存在且可读 `profile.type`）。若 hook 尚未实现，用 `const profile = { type: 'retail' }` 占位，并在 Task 14 中 TODO 注释标记。

### `bill_head.bill_type` 与 `sub_type` 的值

沿用 `000007_init_bill.up.sql` 注释中定义的惯例：
- `bill_type = '出库'`（sales 出库）
- `sub_type = '销售'`

采购侧（Story 6.1 规划）将使用 `bill_type = '入库'`, `sub_type = '采购'`。两者共用 `bill_head` 表，通过这两个字段区分。

### 并发超卖保护

Advisory lock 由 `RecordMovementUseCase.ExecuteInTx` 内部调用（`repo.AcquireAdvisoryLock`）。外层 `ApproveSaleUseCase` 的事务中，对每个 item 串行（非并发）调用 `ExecuteInTx`，天然无并发超卖。两个并发的 `POST /approve` 请求在数据库层被 advisory lock 序列化。

### contracts.md 引用

本 Story 触及 `RecordMovementUseCase`（Epic 5 对外契约）。`ExecuteInTx` 是 API 扩展（非破坏性），无需更新 `doc/coord/contracts.md`，但建议追加一行说明。

---

## Flagged Assumptions

1. **story-6.1 未存在**：`_bmad-output/stories/story-6.1.md` 不存在于当前文件系统。本 Story 假设 Epic 6 的采购单将使用相同的 `bill_head/bill_item` 表（`bill_type='入库'`, `sub_type='采购'`），并且 `BillRepo` 将被 Epic 6 共享。本 Story 先建立 `internal/adapter/repo/bill/repo.go`，Epic 6 扩展此 repo（不重复实现）。

2. **Epic 3 Story 3.7 的 `useProfile()` hook**：假设已存在或可以占位。若 `/hooks/use-profile.ts` 不存在，前端 Task 14 用 mock 实现并加 TODO 注释。

3. **`stock_snapshot` 的 `available_qty`**：当前 `domain.Snapshot` 含 `AvailableQty`，本 Story 假设其与 `OnHandQty` 相等（无预留逻辑），`InsufficientStockError` 以 `OnHandQty` 为准。

4. **migration head 确认**：当前已知 migration 文件为 000001–000015 + 000022（无 000016–000021）。本 Story 不新增 migration，无需确认编号。

5. **bill_head 的 `purchase_status` 列**：现有 DDL 含此列（名称偏向采购），sales 场景该列保持 0，忽略。未来 Epic 6 可重命名或扩展用途。

6. **`payment_head.remark` 字段长度**：DDL 中为 `TEXT`（无长度限制），quick-checkout 的 `customer_name` 存入此字段，长度足够。

---

## Dev Agent Record

(populated by bmad-dev during implementation)
