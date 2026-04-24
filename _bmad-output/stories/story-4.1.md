# Story 4.1: 商品中台升级 — measurement_strategy + unit_def + product_unit + attributes JSONB

**Epic**: 4 — 商品中台
**Story ID**: 4.1
**Profile**: both
**Type**: feat
**Estimate**: 8h
**Status**: Done

---

## Context

Epic 1 建立了 `product` 表骨架（继承自 jshERP），缺乏 Lurus Tally 的差异化特性：
- 计量维度（individual/weight/length/volume/batch/serial）
- 多单位换算（unit_def + product_unit）
- JSONB 灵活属性（profile-aware 字段：HS Code、英文名、原产地 for cross_border；散装、赊账 for retail）

本 Story 补全这三块，为库存 Epic 5（批次管理、按重量入库）和单据 Epic 7（多单位开票）铺好数据基础。

---

## Acceptance Criteria

| # | Criteria | Verifiable |
|---|----------|-----------|
| AC-1 | Migration 000014 创建 `unit_def` + `product_unit` 表，含 RLS + 系统预灌 7 条单位 | ⏳ (DB needed) |
| AC-2 | Migration 000015 给 `product` 加 `measurement_strategy`、`default_unit_id`、`attributes` 3 列 + GIN/BTREE 索引 | ⏳ (DB needed) |
| AC-3 | `MeasurementStrategy` 枚举编译时安全，所有 6 个值可用 | ✅ |
| AC-4 | `unitconv.ConvertToBase` 精度 6 位，输入零分母时返回 error | ✅ |
| AC-5 | `go test ./internal/...` PASS（unitconv + domain entity 单元测试）| ✅ |
| AC-6 | `go build ./...` PASS | ✅ |
| AC-7 | Product CRUD REST 端点注册（`/api/v1/products` GET/POST, `/:id` GET/PUT/DELETE）| ✅ |
| AC-8 | Unit REST 端点注册（`/api/v1/units` GET/POST, `/:id` DELETE）| ✅ |
| AC-9 | `bunx tsc --noEmit` PASS（前端无类型错误）| ✅ |
| AC-10 | `bun run build` PASS（生产构建无报错）| ✅ |

---

## Tasks / Subtasks

### Task 1: Migration 000014 — unit_def + product_unit

- [x] 创建 `migrations/000014_unit_def_product_unit.up.sql`
- [x] 创建 `migrations/000014_unit_def_product_unit.down.sql`

### Task 2: Migration 000015 — product 列升级

- [x] 创建 `migrations/000015_product_upgrade.up.sql`
- [x] 创建 `migrations/000015_product_upgrade.down.sql`

### Task 3: Go — domain entities

- [x] 写测试 `TestMeasurementStrategy_IsValid_*`
- [x] 创建 `internal/domain/product/measurement.go`
- [x] 创建 `internal/domain/product/product.go`
- [x] 创建 `internal/domain/unit/unit.go`

### Task 4: Go — unitconv pkg (TDD)

- [x] 写测试 `internal/pkg/unitconv/unitconv_test.go` (RED)
- [x] 实现 `internal/pkg/unitconv/unitconv.go` (GREEN)
- [x] 验证: `go test ./internal/pkg/unitconv/...` PASS

### Task 5: Go — app use cases

- [x] 创建 `internal/app/product/` (create/list/get/update/delete)
- [x] 创建 `internal/app/unit/` (create/list/delete)

### Task 6: Go — adapter repos

- [x] 创建 `internal/adapter/repo/product/repo.go`
- [x] 创建 `internal/adapter/repo/unit/repo.go`

### Task 7: Go — handlers + middleware stub

- [x] 创建 `internal/adapter/middleware/profile.go`
- [x] 创建 `internal/adapter/handler/product/handler.go`
- [x] 创建 `internal/adapter/handler/unit/handler.go`
- [x] 更新 `internal/adapter/handler/router/router.go`

### Task 8: Go — lifecycle wiring

- [x] 更新 `internal/lifecycle/app.go`

### Task 9: Frontend — API wrappers

- [x] 创建 `web/lib/api/products.ts`
- [x] 创建 `web/lib/api/units.ts`
- [x] 创建 stub `web/lib/profile.tsx`

### Task 10: Frontend — components + pages

- [x] 创建 `web/components/unit-selector.tsx`
- [x] 创建 `web/components/product-form.tsx`
- [x] 创建 `web/app/(dashboard)/products/page.tsx`
- [x] 创建 `web/app/(dashboard)/products/new/page.tsx`
- [x] 创建 `web/app/(dashboard)/products/[id]/page.tsx`

---

## File List

### 新增

| 文件 | 操作 |
|------|------|
| `migrations/000014_unit_def_product_unit.up.sql` | create |
| `migrations/000014_unit_def_product_unit.down.sql` | create |
| `migrations/000015_product_upgrade.up.sql` | create |
| `migrations/000015_product_upgrade.down.sql` | create |
| `internal/domain/product/measurement.go` | create |
| `internal/domain/product/measurement_test.go` | create |
| `internal/domain/product/product.go` | create |
| `internal/domain/unit/unit.go` | create |
| `internal/pkg/unitconv/unitconv.go` | create |
| `internal/pkg/unitconv/unitconv_test.go` | create |
| `internal/app/product/create.go` | create |
| `internal/app/product/list.go` | create |
| `internal/app/product/get.go` | create |
| `internal/app/product/update.go` | create |
| `internal/app/product/delete.go` | create |
| `internal/app/unit/create.go` | create |
| `internal/app/unit/list.go` | create |
| `internal/app/unit/delete.go` | create |
| `internal/adapter/repo/product/repo.go` | create |
| `internal/adapter/repo/unit/repo.go` | create |
| `internal/adapter/middleware/profile.go` | create |
| `internal/adapter/handler/product/handler.go` | create |
| `internal/adapter/handler/unit/handler.go` | create |
| `web/lib/api/products.ts` | create |
| `web/lib/api/units.ts` | create |
| `web/lib/profile.tsx` | create |
| `web/components/unit-selector.tsx` | create |
| `web/components/product-form.tsx` | create |
| `web/app/(dashboard)/products/page.tsx` | create |
| `web/app/(dashboard)/products/new/page.tsx` | create |
| `web/app/(dashboard)/products/[id]/page.tsx` | create |

### 修改

| 文件 | 操作 |
|------|------|
| `internal/adapter/handler/router/router.go` | modify — add product + unit routes |
| `internal/lifecycle/app.go` | modify — wire product + unit handlers |

---

## Dev Agent Record

### Implementation Notes

- No existing Go domain/app layer — all files are new.
- `product` table RLS already enabled in migration 000012; unit_def and product_unit get their own RLS in 000014.
- Profile middleware is a **stub**: reads `profile_type` from ctx (injected by future AuthMiddleware in Story 2.1), queries `tenant_profile` table, injects into Gin ctx. If no tenant_id in ctx, skips.
- Frontend `useProfile()` is a stub returning `{ profileType: 'cross_border' }` with TODO comment pointing to Story 2.1.
- Repos use `pgx/v5` (already in go.mod) via `database/sql`; no GORM added (not in go.mod).
- unitconv uses `math/big` decimal arithmetic to avoid float precision issues — NUMERIC(20,6) → int64 scaled math.

### Deviations

- GORM not in go.mod — using `jackc/pgx/v5` + `database/sql` per existing patterns.
- migration 000013 (tenant_profile) is Story 2.1 territory — not touched here. unit_def RLS uses `is_system=true` to bypass tenant filter for system units.

### Story 2.1 Wire-up TODOs

When Story 2.1 AuthMiddleware is implemented:
1. `internal/adapter/middleware/profile.go`: replace stub's `c.Get("tenant_id")` with real tenant_id from JWT claims set by AuthMiddleware.
2. `web/lib/profile.tsx`: replace stub `useProfile()` with real context reading from NextAuth session (`session.user.profileType`).
3. `internal/adapter/handler/router/router.go`: add `middleware.AuthMiddleware` before `middleware.ProfileMiddleware` in the API route group.
