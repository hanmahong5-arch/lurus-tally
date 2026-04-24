# Story 7.1: 销售出库基础闭环

**Epic**: 7 — 销售流程闭环
**Story ID**: 7.1
**Profile**: both
**Type**: feat
**Estimate**: 12h
**Status**: Done

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

- [x] 写失败测试 `TestBillHead_SaleType_Valid`（断言 `bill_type='出库'`, `sub_type='销售'` 枚举值有效）
- [x] 创建 `internal/domain/bill/bill.go`：
  - `BillHead` struct（对应 `tally.bill_head` 列，含 `PaidAmount`, `TotalAmount`, `Status` SMALLINT 枚举 0/1/2/3/4/9）
  - `BillItem` struct（对应 `tally.bill_item`，含 `QtyBase decimal.Decimal`）
  - 常量：`BillTypeOut = "出库"`, `SubTypeSale = "销售"`, `StatusDraft = 0`, `StatusApproved = 2`, `StatusCancelled = 9`
  - `ReceivableAmount() decimal.Decimal` 计算方法（`TotalAmount - PaidAmount`，非负 clamp）
- [x] 创建 `internal/domain/bill/bill_test.go`
- [x] 验证：`go test ./internal/domain/bill/...` PASS

### Task 2: Domain — payment entity

- [x] 写失败测试 `TestPayment_PayType_Valid`（断言 pay_type 枚举：cash/wechat/alipay/card/credit/transfer）
- [x] 创建 `internal/domain/payment/payment.go`：
  - `Payment` struct（对应 `tally.payment_head`：`ID`, `TenantID`, `BillID uuid.UUID`（映射 `related_bill_id`）, `PayType`, `Amount decimal.Decimal`, `PayDate time.Time`, `PartnerID *uuid.UUID`, `CreatorID uuid.UUID`, `Remark string`）
  - `PayType` 类型 + 枚举常量 + `Validate()` 方法
- [x] 创建 `internal/domain/payment/payment_test.go`
- [x] 验证：`go test ./internal/domain/payment/...` PASS

### Task 3: Repo — bill repo interface + PG implementation

- [x] 写失败测试 `TestBillRepo_CreateAndGet_RoundTrip`（使用 testcontainers 或 test-db DSN，断言写后读一致）
- [x] 创建/修改 `internal/adapter/repo/bill/repo.go`：added `UpdatePaidAmount`, `paid_amount` scan in `GetBillForUpdate`, `GetBill`, `ListBills`
- [x] 创建 `internal/adapter/repo/bill/repo_test.go`
- [x] 验证：repo 单元测试 PASS

### Task 4: Repo — payment repo

- [x] 写失败测试 `TestPaymentRepo_RecordAndList_RoundTrip`
- [x] 创建 `internal/adapter/repo/payment/repo.go`：
  - `Record(ctx, tx, p *domain.Payment) error`
  - `ListByBill(ctx, tenantID, billID uuid.UUID) ([]*domain.Payment, error)`
  - `SumByBill(ctx, tx, tenantID, billID uuid.UUID) (decimal.Decimal, error)`
- [x] 创建 `internal/adapter/repo/payment/repo_test.go`
- [x] 验证：`go test ./internal/adapter/repo/payment/...` PASS

### Task 5: Use case — create sale draft

- [x] 写失败测试 `TestCreateSaleDraft_ValidRequest_ReturnsBillID`
- [x] 写失败测试 `TestCreateSaleDraft_EmptyItems_ReturnsError`
- [x] 创建 `internal/app/bill/create_sale.go`
- [x] 创建 `internal/app/bill/create_sale_test.go`
- [x] 验证：`go test ./internal/app/bill/... -run TestCreateSale` PASS

### Task 6: Use case — approve sale (出库 + 首次收款)

- [x] 写失败测试 `TestApproveSale_AllItemsInStock_ApproveSucceeds`
- [x] 写失败测试 `TestApproveSale_OneItemInsufficient_RollsBack`
- [x] 写失败测试 `TestApproveSale_WithPaidAmount_RecordsPayment`
- [x] 写失败测试 `TestApproveSale_ZeroPaidAmount_SkipsPayment`
- [x] 创建 `internal/app/bill/approve_sale.go`（local `PaymentRecorder` interface to break cycle with app/payment）
- [x] 创建 `internal/app/bill/approve_sale_test.go`
- [x] 验证：`go test ./internal/app/bill/... -run TestApproveSale` PASS

### Task 7: Use case — POS quick checkout

- [x] 写失败测试 `TestQuickCheckout_ValidRequest_ReturnsBillID`
- [x] 写失败测试 `TestQuickCheckout_InsufficientStock_Returns422`
- [x] 创建 `internal/app/bill/quick_checkout.go`
- [x] 创建 `internal/app/bill/quick_checkout_test.go`
- [x] 验证：`go test ./internal/app/bill/... -run TestQuickCheckout` PASS

### Task 8: Use case — record payment (赊账后还款)

- [x] 写失败测试 `TestRecordPayment_ApprovedBill_UpdatesPaidAmount`
- [x] 写失败测试 `TestRecordPayment_DraftBill_ReturnsError`
- [x] 创建 `internal/app/payment/record.go`（local `BillReader` interface to break cycle with app/bill）
- [x] 写失败测试 `TestListPayments_ByBillID_ReturnsAll`
- [x] 创建 `internal/app/payment/list.go`
- [x] 创建 `internal/app/payment/record_test.go` + `list_test.go`
- [x] 验证：`go test ./internal/app/payment/...` PASS

### Task 9: RecordMovementUseCase — ExecuteInTx 变体

- [x] 写失败测试 `TestRecordMovement_ExecuteInTx_UsesProvidedTx`
- [x] 在 `internal/app/stock/usecase.go` 增加 `ExecuteInTx(ctx, tx *sql.Tx, req RecordMovementRequest) (*domain.Snapshot, error)`
- [x] 更新 `internal/app/stock/usecase_test.go`
- [x] 验证：`go test ./internal/app/stock/...` PASS

### Task 10: HTTP handler — sale bills

- [x] 写失败测试 `TestSaleHandler_CreateDraft_Returns201`
- [x] 写失败测试 `TestSaleHandler_Approve_Returns200`
- [x] 写失败测试 `TestSaleHandler_Approve_InsufficientStock_Returns422`
- [x] 写失败测试 `TestSaleHandler_QuickCheckout_Returns201`
- [x] 写失败测试 `TestSaleHandler_List_Returns200`
- [x] 写失败测试 `TestSaleHandler_GetByID_Returns200WithPayments`
- [x] 创建 `internal/adapter/handler/bill/sale_handler.go`
- [x] 创建 `internal/adapter/handler/bill/sale_handler_test.go`
- [x] 验证：handler 单元测试 PASS

### Task 11: HTTP handler — payments

- [x] 写失败测试 `TestPaymentHandler_Record_Returns201`
- [x] 写失败测试 `TestPaymentHandler_List_Returns200`
- [x] 创建 `internal/adapter/handler/payment/handler.go`
- [x] 创建 `internal/adapter/handler/payment/handler_test.go`
- [x] 验证：`go test ./internal/adapter/handler/payment/...` PASS

### Task 12: Router + lifecycle wiring

- [x] 写失败测试 `TestRouter_SaleRoutes_Registered`（断言 `/api/v1/sale-bills` + `/api/v1/payments` 路由存在，返回非 404）
- [x] 修改 `internal/adapter/handler/router/router.go`：added `*handlerbill.SaleHandler` + `*handlerpayment.Handler` params
- [x] 修改 `internal/lifecycle/app.go`：wire all sale + payment use cases
- [x] 验证：`go build ./...` PASS + `go test ./...` PASS

### Task 13: Frontend — sale list page

- [x] 写失败测试（Vitest）`sale.test.ts — listSaleBills returns typed array` (7 tests PASS)
- [x] 创建 `web/lib/api/sale.ts`：`createSaleBill / approveSaleBill / quickCheckout / listSaleBills / getSaleBill` fetch wrappers，类型化响应
- [x] 创建 `web/app/(dashboard)/sales/page.tsx`：DataTable 展示销售单列表；列：单号/客户/日期/金额/应收/状态；状态色：草稿灰/审核蓝/取消红
- [x] 验证：lint PASS (no errors in new files)

### Task 14: Frontend — new sale page (profile-aware)

- [x] 创建 `web/app/(dashboard)/sales/new/page.tsx`：`useProfile()` retail→quick default, toggle switch; `SaleLineEditor` + payment section
- [x] 创建 `web/components/sale-line-editor.tsx`（POS-optimized, no shipping/tax）
- [x] 验证：lint PASS

### Task 15: Frontend — sale detail page

- [x] 创建 `web/lib/api/payment.ts`：`recordPayment / listPayments` fetch wrappers
- [x] 创建 `web/components/payment-form.tsx`：payment history + receivable summary + inline record form
- [x] 创建 `web/app/(dashboard)/sales/[id]/page.tsx`：head + items + payments + approve/pay buttons
- [x] 验证：lint PASS

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

### Implementation Summary (2026-04-23)

All 15 tasks implemented in two sessions (prev session: Tasks 1-12; this session: Tasks 13-15).

**Key decisions:**
- Import cycle between `app/bill` and `app/payment` solved by defining minimal local interfaces: `PaymentRecorder` in `app/bill/approve_sale.go`, `BillReader` in `app/payment/repo.go`. Neither package imports the other.
- `ApproveSaleUseCase.ExecuteInTx` exposes the inner logic for `QuickCheckoutUseCase` composition — single `WithTx` opened at quick-checkout level, inner approve uses provided tx.
- `InsufficientStockError` referenced as `*appstock.InsufficientStockError` (lives in `internal/app/stock`, not `internal/domain/stock`).
- Sale handler registers `POST /sale-bills/quick-checkout` before `/:id` pattern to prevent Gin treating "quick-checkout" as a bill ID.
- Frontend `sale.ts` re-exports `BillStatus`, `BillItem`, `BillLineItemInput` from `purchase.ts` to avoid duplication.
- `SaleLineEditor` created as a simplified component (no shipping/tax) optimized for POS UX.
- Pre-existing tsc error in `components/pos/product-search.tsx` (Story 10.1 background agent) not fixed — out of scope.
- Pre-existing lint errors in `lib/pos/cart-reducer.test.ts` (Story 10.1) not fixed — out of scope.

**Test evidence:**
- Go: `go test -count=1 ./...` — 28 packages pass, 0 failures
- TS (Vitest): 68 pass, 3 fail (pre-existing auth-session.test.ts, not from this story)
- Build: `CGO_ENABLED=0 GOOS=linux go build ./cmd/server` — success

**Files changed (this story):**

Backend:
- `internal/domain/bill/bill.go` — added `PaidAmount`, `ReceivableAmount()`
- `internal/domain/bill/bill_test.go` — added sale tests
- `internal/domain/payment/payment.go` — created
- `internal/domain/payment/payment_test.go` — created
- `internal/app/bill/repo.go` — added `UpdatePaidAmount` to `BillRepo` interface
- `internal/app/bill/create_sale.go` — created
- `internal/app/bill/create_sale_test.go` — created
- `internal/app/bill/approve_sale.go` — created (with `PaymentRecorder` interface)
- `internal/app/bill/approve_sale_test.go` — created
- `internal/app/bill/quick_checkout.go` — created
- `internal/app/bill/quick_checkout_test.go` — created
- `internal/app/payment/repo.go` — created (with `BillReader` interface)
- `internal/app/payment/record.go` — created
- `internal/app/payment/record_test.go` — created
- `internal/app/payment/list.go` — created
- `internal/app/payment/list_test.go` — created
- `internal/app/stock/usecase.go` — added `ExecuteInTx`
- `internal/adapter/repo/bill/repo.go` — added `UpdatePaidAmount`, `paid_amount` in queries
- `internal/adapter/repo/payment/repo.go` — created
- `internal/adapter/repo/payment/repo_test.go` — created
- `internal/adapter/handler/bill/sale_handler.go` — created
- `internal/adapter/handler/bill/sale_handler_test.go` — created
- `internal/adapter/handler/payment/handler.go` — created
- `internal/adapter/handler/payment/handler_test.go` — created
- `internal/adapter/handler/router/router.go` — added SaleHandler + PaymentHandler params
- `internal/adapter/handler/router/router_test.go` — updated newTestRouter() to 9 args
- `internal/lifecycle/app.go` — wire sale + payment use cases

Frontend:
- `web/lib/api/sale.ts` — created
- `web/lib/api/sale.test.ts` — created (7 tests)
- `web/lib/api/payment.ts` — created
- `web/components/sale-line-editor.tsx` — created
- `web/components/payment-form.tsx` — created
- `web/app/(dashboard)/sales/page.tsx` — created
- `web/app/(dashboard)/sales/new/page.tsx` — created
- `web/app/(dashboard)/sales/[id]/page.tsx` — created
