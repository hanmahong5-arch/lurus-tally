# Epics: Lurus Tally (2b-svc-psi) — v2 双 Persona + 行业 Profile + 边缘部署

源 PRD: `./prd.md` (版本: 1.0, 2026-04-23, 单 persona 草稿; 新 PRD 双 persona 版并行重写中)
源架构: `./architecture.md` (版本: 1.0, 2026-04-23; 新架构双 persona 版并行重写中)
旧 Epics 备份: `./_archive/epics-v1-single-persona-2026-04-23.md`
生成时间: 2026-04-23

---

## 已锁定决策（Decision Lock 路 B）

| 决策 | 内容 |
|------|------|
| DL-1 | 单产品 + 行业 Profile（cross_border / retail / hybrid）；profile 存独立表 `tenant_profile.profile_type`（不加在 tenant 表） |
| DL-2 | 双 Persona 并列：跨境企业 + 线下五金店 |
| DL-3 | 商品模型升级：measurement_strategy / alt_units / attributes JSONB |
| DL-4 | 库存策略 Strategy Pattern：FIFO / 加权平均 / 按重 / 按批次 |
| DL-5 | V1 建 origin/sync_status 字段（离线容器），V2 再实施边缘部署 |
| DL-6 | 高级 AI（补货 Agent / 动态定价 / NL 查询）延后到 V3 |

## Profile 说明

每个 Story 必须标注以下之一：
- `Profile: cross_border` — 仅适用跨境企业 persona
- `Profile: retail` — 仅适用线下零售（五金店）persona
- `Profile: both` — 双 persona 都需要

---

## Executive Summary

| 维度 | V1 | V2 | V3 | 合计 |
|------|-----|-----|-----|------|
| Epic 数 | 11 (E1-E11) | 5 (E12-E16) | 4 (E17-E20) | 20 |
| Story 数 | 78 | 24 | 16 | 118 |
| 预估工时（单人）| ~460h | ~170h | ~130h | ~760h |
| Sprint 单人（2周/sprint，70%效率=56h）| ~8 sprint | ~3 sprint | ~2.5 sprint | ~14 sprint |
| Sprint 双人并行 | ~5 sprint | ~2 sprint | ~1.5 sprint | ~8.5 sprint |

### 关键里程碑

| Milestone | 完成 Epic | 目标节点 |
|-----------|----------|---------|
| MVP α — 核心进销存跑通 (Lighthouse 客户) | E1-E7 | M3 |
| MVP β — 双 Profile + Onboarding + AI 助手 | E8-E11 | M6 |
| V2 — 边缘部署 & 离线同步 GA | E12-E16 | M9 |
| V3 — 高级 AI | E17-E20 | M12+ |

---

## Epic 依赖图

```
V1 基础链路（单 Profile = both）:
E1 → E2 → E3 → E4 → E5 → E6/E7 → E9 → E10
             ↘ (商品中台) ↗
E8 (Profile 机制) 在 E4 之后、E9 之前完成，供 E9/E10 消费

跨境专属 / 零售专属:
E4 → E9 (跨境能力，multi-currency / HS Code / 海运)
E4 → E10 (零售能力，POS / 称重 / 支付)

Onboarding:
E11 依赖 E8 (Profile 选择向导) + E9/E10 (专属能力可用)

V2 (边缘部署):
E1 → E12 (edge binary) → E13 (sync engine) → E14 (冲突 UI)
E13 → E15 (PWA 壳) → E16 (节点管理)

V3 (AI):
E10 + E11 → E17/E18/E19/E20 (AI 需要完整数据)
```

**风险优先排序逻辑**: Epic 2 (RLS 多租户) 在 Epic 3 之前——多租户安全是 SaaS 生死线，晚期发现 GORM+RLS 坑会波及全表。Epic 8 (Profile 机制) 提前到 E8 而非 E11——Profile 感知的 UI 渲染框架是跨境/零售双 persona 差异化的技术底座，晚建设需大规模返工。Epic 12 (edge binary) 在 V2 首个 Epic——离线部署是五金店 persona 的核心差异需求，也是技术风险最高的假设（SQLite 与 PostgreSQL 兼容、build tag 隔离），必须最早验证。

---

## V1 — 双 Profile MVP（Web SaaS only，不含离线）

---

## Epic 1: 项目骨架与 CI/CD 管线

**目标**: 开发者 `git clone` 后一条命令启动完整本地开发环境；GitHub Actions 流水线绿色；ArgoCD App 注册到 lurus-tally namespace。

**PRD Requirements**: §12 W1-W2 工程里程碑；决策锁 §5

**Risk**: Go module 路径与 GHCR 私有镜像认证；Next.js standalone 与 Bun 在 Docker 多阶段构建的兼容性需实测。

**Acceptance**:
- `make dev` 启动后端 :18200 + 前端 :3000，健康检查通过
- CI 全部 job 绿色（lint → typecheck → test → build → push）
- `migrate up` 后 tally schema 27 张表 + RLS policies 均已创建
- ArgoCD application `lurus-tally` 指向 stage overlay

**依赖**: 无（首 Epic）

**预估**: 7 Stories × 平均 5h = **35h**

### Stories

| # | Title | Profile | 类型 | 工时 | 关键文件 |
|---|-------|---------|------|------|---------|
| 1.1 | Go 服务可启动并通过健康检查 | both | infra | 6h | `cmd/server/main.go`, `lifecycle/`, `pkg/config/` |
| 1.2 | Next.js 前端可访问登录页占位 | both | infra | 4h | `web/app/(auth)/login/page.tsx` |
| 1.3 | 数据库迁移脚本完整执行（全 27 张表 + V2 预留字段） | both | infra | 6h | `migrations/` |
| 1.4 | GitHub Actions CI 流水线（lint/typecheck/test/build） | both | infra | 5h | `.github/workflows/ci.yml` |
| 1.5 | Docker 多阶段构建与 GHCR 镜像推送 | both | infra | 5h | `Dockerfile` |
| 1.6 | ArgoCD ApplicationSet 注册与 K8s 基础清单 | both | infra | 5h | `deploy/k8s/base/`, `deploy/argocd/` |
| 1.7 | 本地开发 Makefile + 环境变量模板 | both | infra | 4h | `Makefile`, `.env.example` |

> **注**: Stories 1.1-1.6 已 DONE（`migration head: 12, 27 tables, 15 tests PASS`）。Story 1.7 未完成。

#### Story 1.7 — 本地开发 Makefile + 环境变量模板
- **As a** 新加入开发者, **I want** 一个 `.env.example` 和 `make dev` 命令快速启动本地环境, **so that** 减少首次上手摩擦。
- **Acceptance Criteria**:
  - `.env.example` 包含所有必填环境变量（含 `TENANT_PROFILE` 选项说明）及说明注释
  - `make dev` 启动 PostgreSQL (Docker Compose) + Go 后端 + Next.js 前端，health 通过后打印 URL
  - `make test` 运行 `go test ./...` + `cd web && bun run test`
- **Tech Notes**: `Makefile` targets: `dev / test / build / migrate-up / migrate-down`
- **Out of Scope**: Zitadel 本地模拟（用测试 tenant 绕过）

---

## Epic 2: 多租户与认证基础

**目标**: 用户可用 Zitadel OIDC 登录；注册后自动创建 tenant 记录并同步 `tenant_profile`（cross_border/retail/hybrid）；PostgreSQL RLS 隔离生效；RBAC 四角色权限控制在 API 层生效。

**PRD Requirements**: US-1.1, US-1.2；PRD §7.3 安全合规；决策锁 §3 第1条

**Risk**: GORM + PostgreSQL RLS 连接池复用——`SET LOCAL app.tenant_id` 必须在每个事务内生效，需要有跨租户 E2E 测试，一旦晚期发现问题会波及所有业务表。

**Acceptance**:
- Zitadel OIDC 登录全链路可跑通
- `TestRLS_CrossTenantQueryReturnsEmpty` 集成测试通过
- 四角色权限矩阵：仓管访问财务端点返回 403
- `tenant_profile.profile_type` 字段存储并可从 API 读取

**依赖**: Epic 1

**预估**: 6 Stories × 平均 6h = **36h**

### Stories

| # | Title | Profile | 类型 | 工时 | 关键文件 |
|---|-------|---------|------|------|---------|
| 2.1 | 用户可用 Zitadel OIDC 完成登录与登出 | both | feat | 7h | `web/lib/auth.ts`, `adapter/middleware/auth.go` |
| 2.2 | 登录后自动创建/同步租户记录（含 tenant_profile 字段） | both | feat | 5h | `app/tenant/`, `adapter/platform/tenant.go` |
| 2.3 | API 请求全局注入租户上下文（RLS 激活） | both | feat | 6h | `adapter/middleware/tenant_rls.go` |
| 2.4 | 跨租户数据隔离 E2E 验证 | both | test | 4h | `tests/integration/rls_isolation_test.go` |
| 2.5 | RBAC 四角色权限矩阵实施 | both | feat | 6h | `adapter/middleware/auth.go`, `pkg/types/role.go` |
| 2.6 | 企业设置向导（三步引导，含 Profile 选择） | both | feat | 6h | `web/app/(dashboard)/settings/`, `components/onboarding/` |

#### Story 2.2 — 登录后自动创建/同步租户记录（含 tenant_profile）
- **As a** 新用户, **I want** 首次登录后系统自动创建企业空间并记录行业 Profile, **so that** 后续 UI 按行业定制。
- **Acceptance Criteria**:
  - 首次登录时 `tally.tenant` 中 upsert 对应记录，`profile` 字段默认 `hybrid`
  - Platform 同步回调 `/internal/v1/tally/tenant/sync` 能更新本地缓存
- **Tech Notes**: `tenant_profile.profile_type` 类型 `VARCHAR(20) CHECK IN ('cross_border','retail','hybrid')`；独立表 `tally.tenant_profile`（migration 000013）；不在 `tenant` 表加字段

#### Story 2.6 — 企业设置向导（三步引导 + Profile 选择）
- **As a** 新注册老板, **I want** 首次登录后看到三步引导向导并选择行业 Profile, **so that** 系统界面按我的业务类型定制。
- **Acceptance Criteria**:
  - 步骤 1: 公司名称/行业 → 行业选择包含"跨境贸易/外贸批发"和"线下零售/实体门店"选项 → 存 `tenant_profile.profile_type`
  - 步骤 2: 创建第一个仓库
  - 步骤 3: 演示数据（按 Profile 加载不同种子）或空白
  - 完成后跳转 Dashboard，按 Profile 展示对应 CTA 引导
- **Tech Notes**: `tenant.settings.onboarding_done: true` 二次登录不再弹出；Profile 选择触发后续 Epic 8 的 UI 适配逻辑

---

## Epic 3: 设计系统基石

**目标**: 开发者可从组件库组装任意业务页面；⌘K Command Palette、AI Drawer、暗黑模式、DataTable、Sheet 等核心组件均可独立 demo；Profile 感知的条件渲染 hook 就绪。

**PRD Requirements**: PRD §8 UX 原则 P1-P10；决策锁 §4

**Risk**: shadcn/ui 2025 OKLCH 色彩空间与 TailwindCSS v4 可能有配置冲突；Framer Motion 与 Next.js App Router 服务端组件边界需仔细标注 "use client"。

**Acceptance**:
- 所有核心组件有可运行 demo 页
- ⌘K 打开、AI Drawer 右侧滑出、暗黑/亮色切换无闪烁
- `useProfile()` hook 在任意组件可读取当前租户 Profile
- Profile 感知渲染：`<ProfileGate profile="cross_border">` 按 Profile 条件渲染

**依赖**: Epic 1

**预估**: 7 Stories × 平均 5h = **35h**

### Stories

| # | Title | Profile | 类型 | 工时 | 关键文件 |
|---|-------|---------|------|------|---------|
| 3.1 | 主题系统与暗黑模式（OKLCH 色彩空间） | both | feat | 4h | `styles/globals.css` |
| 3.2 | 可折叠侧边栏与顶栏主布局 | both | feat | 5h | `components/layout/sidebar.tsx` |
| 3.3 | DataTable 通用封装（TanStack Table + 骨架屏） | both | feat | 6h | `components/data-table/` |
| 3.4 | ⌘K Command Palette 框架 | both | feat | 5h | `components/command-palette/` |
| 3.5 | AI Drawer 框架（流式输出占位） | both | feat | 5h | `components/ai-drawer/` |
| 3.6 | Slide-over Sheet、空状态组件、Toast 系统 | both | feat | 4h | `components/slide-over/` |
| 3.7 | Profile 感知渲染 hook 与条件组件 | both | feat | 4h | `hooks/use-profile.ts`, `components/profile-gate.tsx` |

#### Story 3.7 — Profile 感知渲染 hook 与条件组件
- **As a** 开发者, **I want** 一个 `useProfile()` hook 和 `<ProfileGate>` 组件, **so that** 跨境专属/零售专属 UI 能按租户 Profile 自动显隐，无需在每个页面手写判断逻辑。
- **Acceptance Criteria**:
  - `useProfile()` 返回 `{ profile: 'cross_border' | 'retail' | 'hybrid' }`，从 session 读取，无 API 额外请求
  - `<ProfileGate profiles={['cross_border']}>` 仅在 cross_border 或 hybrid 租户下渲染子树
  - `<ProfileGate profiles={['retail']}>` 仅在 retail 或 hybrid 下渲染
  - Profile 变更后（设置页修改）无需刷新，Zustand store 实时更新
- **Tech Notes**: `stores/tenant-store.ts` 存 profile；Profile 从 JWT claim 或 `/api/v1/tenant` 接口读取并缓存

---

## Epic 4: 商品中台（升级版）

**目标**: 管理员可创建商品并定义 `measurement_strategy`（件/重量/散装/批次）、`alt_units`（多单位换算）、`attributes` JSONB（自由属性）；条码枪扫码定位 SKU < 300ms；安全库存阈值触发预警；V2 离线容器字段（`origin / sync_status`）在表结构中已预留。

**PRD Requirements**: US-2.1~2.6；PRD §6.1 商品模块 P0；决策锁 DL-3

**Risk**: `measurement_strategy` 枚举新增后影响采购/销售/库存模块的所有计量逻辑——必须在本 Epic 中锁定枚举值和计量接口合约，后续 Epic 直接调用，不允许各模块各自实现。

**Acceptance**:
- 商品 CRUD 完整（含 measurement_strategy / alt_units / attributes JSONB）
- Excel 导入 500 行 < 5s 集成测试通过
- 条码扫码 < 300ms（Redis 缓存路径）
- `origin` / `sync_status` 字段存在于 DDL（V2 预留，V1 不写入逻辑）
- 商品查询 API 返回 `measurement_strategy` 和 `alt_units`

**依赖**: Epic 2（租户上下文）, Epic 3（DataTable/Sheet）

**预估**: 9 Stories × 平均 5.5h = **50h**

### Stories

| # | Title | Profile | 类型 | 工时 | 关键文件 |
|---|-------|---------|------|------|---------|
| 4.1 | 商品列表与全文搜索 | both | feat | 5h | `app/product/query_product.go`, `web/app/products/page.tsx` |
| 4.2 | 新建/编辑商品（Sheet 表单 + 分类树） | both | feat | 5h | `app/product/create_product.go` |
| 4.3 | measurement_strategy 商品计量模型（件/重量/散装/批次） | both | feat | 7h | `domain/entity/product.go`, `pkg/types/measurement.go` |
| 4.4 | alt_units 多单位换算（箱/件/托/kg/散装） | both | feat | 6h | `domain/entity/unit.go`, `handler/v1/sku.go` |
| 4.5 | attributes JSONB 自由属性（五金规格/跨境 HS Code 预填） | both | feat | 5h | `domain/entity/product.go attributes JSONB` |
| 4.6 | 批次管理与序列号管理（商品级开关） | both | feat | 5h | `domain/entity/stock_lot.go`, `stock_serial.go` |
| 4.7 | 条码扫码定位 SKU（< 300ms，Redis 缓存） | both | feat | 4h | `adapter/repo/product_sku_repo.go` |
| 4.8 | 安全库存阈值与预警状态 | both | feat | 4h | `app/stock/alert_stock.go` |
| 4.9 | Excel 批量导入商品（含 measurement_strategy 列）+ 商品停售 | both | feat | 7h | `app/product/import_product.go` |

#### Story 4.3 — measurement_strategy 商品计量模型
- **As a** 管理员, **I want** 为每种商品选择计量策略（件数/重量/散装计量/批次管理）, **so that** 五金店散装螺丝和跨境标准箱都能准确记账。
- **Acceptance Criteria**:
  - 枚举: `individual`（件数，默认）/ `weight`（按重量，kg/g）/ `length`（按长度）/ `volume`（按体积）/ `batch`（强制批次）/ `serial`（序列号）
  - 选 `weight`/`length`/`volume` 时，开单数量字段为小数；选 `individual` 时为整数
  - 计量策略影响采购单、销售单、库存查询的数量字段类型（后续 Epic 消费）
- **Tech Notes**: `pkg/types/measurement.go` 定义 `MeasurementStrategy` 类型及业务规则；`product.measurement_strategy VARCHAR(20) NOT NULL DEFAULT 'individual'`（migration 000015）；后续 Epic 中所有数量字段使用 `NUMERIC(18,4)` 以兼容小数
- **Profile**: `both`（五金店用 weight/volume，跨境用 individual/batch/serial，都需要）

#### Story 4.5 — attributes JSONB 自由属性
- **As a** 管理员, **I want** 为商品添加自由键值属性, **so that** 五金店可记录"材质/表面处理/规格型号"，跨境可记录"HS Code/原产地/申报价格"。
- **Acceptance Criteria**:
  - `product.attributes JSONB` 存任意键值对，最多 20 个字段
  - 后台可定义"属性模板"：零售模板（材质/颜色/规格）、跨境模板（HS Code/原产地/海关申报单位）
  - `attributes` 可在商品搜索中作为过滤条件（`attributes->>'hs_code' = '...'`）
- **Profile**: `both`（内容不同，但机制共享）

---

## Epic 5: 仓库与库存基础

**目标**: 管理员可创建多个仓库；库存六状态实时查询；WAC + 库存策略 Strategy Pattern（FIFO / 加权平均 / 按重 / 按批次）框架就绪；`stock_ledger` 可追溯；V2 `sync_status` 字段预留。

**PRD Requirements**: US-3.1~3.3；PRD §6.2 仓库 P0；PRD §6.3 库存 P0；决策锁 DL-4

**Risk**: 库存策略 Strategy Pattern——多策略共存时，WAC 计算代码路径必须在本 Epic 封装为 interface，否则采购/销售 Epic 会各自实现不一致的成本计算。并发更新 (`SELECT FOR UPDATE`) 也在本 Epic 明确。

**Acceptance**:
- 仓库 CRUD + 库存六状态查询 API 完整
- `CostStrategy` interface 已定义，WAC 实现通过，FIFO 骨架通过（V1 WAC 默认）
- `TestStockConcurrentUpdate_NoOversell` 并发测试通过
- `stock_snapshot.sync_status` 字段存在（V2 预留，V1 不写逻辑）

**依赖**: Epic 4（商品数据）

**预估**: 6 Stories × 平均 5h = **30h**

### Stories

| # | Title | Profile | 类型 | 工时 | 关键文件 |
|---|-------|---------|------|------|---------|
| 5.1 | 仓库创建与管理 | both | feat | 4h | `app/warehouse/`, `handler/v1/warehouse.go` |
| 5.2 | 库存六状态实时查询（多仓库视图） | both | feat | 5h | `app/stock/query_stock.go`, `domain/entity/stock_snapshot.go` |
| 5.3 | 库存策略 Strategy Pattern（CostStrategy interface + WAC 实现） | both | feat | 7h | `app/stock/cost_strategy.go`, `wac_strategy.go`, `fifo_strategy.go` |
| 5.4 | 库存流水追溯（stock_ledger） | both | feat | 5h | `domain/entity/stock_ledger.go`, `adapter/repo/stock_repo.go` |
| 5.5 | 库存快照物化视图刷新与 Worker | both | feat | 4h | `lifecycle/worker.go`, `migrations/000011` |
| 5.6 | 散装/按重计量库存（对接 measurement_strategy） | both | feat | 5h | `app/stock/` 小数库存逻辑 |

#### Story 5.3 — 库存策略 Strategy Pattern
- **As a** 系统, **I want** 库存成本计算通过 Strategy Pattern 实现, **so that** V1 用 WAC、V2 可无缝切换 FIFO 或按重量计量，不需要修改上层业务逻辑。
- **Acceptance Criteria**:
  - `CostStrategy` interface: `CalcInboundCost(sku, qty, unitPrice) → avgCost`
  - `WACStrategy` 实现通过 4 种场景单元测试
  - `FIFOStrategy` 骨架编译通过（V1 不注册为默认，V2 激活）
  - `ByWeightStrategy` 处理小数数量（`NUMERIC(18,4)`）
  - Tenant 可通过 `system_config.cost_strategy` 选择策略（默认 `wac`）
- **Tech Notes**: `app/stock/cost_strategy.go` 定义 interface；`lifecycle/app.go` 按 config 注入；FIFO 需要 `stock_lot` 表记录入库批次顺序

#### Story 5.6 — 散装/按重计量库存
- **As a** 仓管（五金店）, **I want** 系统支持以克/千克为单位记录螺丝等散装商品库存, **so that** 不必把散装商品换算成"件"来管理。
- **Acceptance Criteria**:
  - `measurement_strategy = 'weight'` 的 SKU，`stock_snapshot.on_hand_qty` 使用 `NUMERIC(18,4)` 存储
  - 开单时可输入 0.5 kg 等小数数量
  - 库存显示时附带单位（`kg` / `g` 由 `product.base_unit` 决定）
- **Profile**: `retail`（五金店主用）

---

## Epic 6: 采购流程闭环

**目标**: 采购员可创建采购单 → 入库 → WAC 重算 → 应付台账；支持部分入库、反审红冲；计量单位按 `measurement_strategy` 正确显示（五金店采购"50 kg 螺丝"而非"50 件"）。

**PRD Requirements**: US-4.1~4.4；PRD §6.4 采购 P0

**Risk**: 状态机散乱——`bill_head` 的采购状态转换规则若不统一封装，各 handler 各自实现会导致 bug 难追踪。`pkg/types/bill_status.go` 必须在 Story 6.1 中先定义。

**Acceptance**:
- 完整采购单状态机测试（含非法转换 400）
- 入库后 `stock_snapshot.on_hand_qty` 精确增加，WAC 重算
- 红冲后原单"已冲销"，反向入库单生成，审计日志记录
- 采购数量单位按商品 `measurement_strategy` 正确显示

**依赖**: Epic 5

**预估**: 7 Stories × 平均 5.5h = **38h**

### Stories

| # | Title | Profile | 类型 | 工时 | 关键文件 |
|---|-------|---------|------|------|---------|
| 6.1 | 创建采购单草稿（Stepper + 计量单位感知） | both | feat | 7h | `app/purchase/create_purchase.go`, `web/app/purchases/new/` |
| 6.2 | 采购单提交审核与状态机 | both | feat | 5h | `app/purchase/submit_purchase.go`, `pkg/types/bill_status.go` |
| 6.3 | 采购入库确认（全量/部分 + 成本策略触发） | both | feat | 6h | `app/purchase/receive_purchase.go` |
| 6.4 | 采购单应付款管理（多次付款录入） | both | feat | 5h | `app/finance/create_payment.go` |
| 6.5 | 采购单反审与红冲 | both | feat | 6h | `app/purchase/cancel_purchase.go` |
| 6.6 | 采购单列表与详情页 | both | feat | 4h | `web/app/purchases/page.tsx` |
| 6.7 | 采购退货（退供应商） | both | feat | 5h | `app/purchase/` 退货子流程 |

---

## Epic 7: 销售流程闭环

**目标**: 业务员可创建销售单 → 超库存预警 → 出库确认 → 库存扣减 → 应收台账；支持打印送货单；零售 Profile 下支持现金/赊账收款区分；跨境 Profile 下支持外币金额显示（V1 仅显示，E9 做正式多币种）。

**PRD Requirements**: US-5.1~5.4；PRD §6.5 销售 P0

**Risk**: 超卖并发控制——提交到出库确认之间可能并发超卖，出库确认时必须 `SELECT FOR UPDATE`，在 Story 7.3 明确。

**Acceptance**:
- 销售单完整状态机测试（含超库存场景）
- 出库确认后 `on_hand_qty` 精确减少
- 打印送货单含公司抬头、中文大写金额
- 零售 Profile 下有"收现金/赊账"快速选项

**依赖**: Epic 5（库存）, Epic 6（参考收付款结构）

**预估**: 8 Stories × 平均 5h = **40h**

### Stories

| # | Title | Profile | 类型 | 工时 | 关键文件 |
|---|-------|---------|------|------|---------|
| 7.1 | 创建销售单（客户选择 + SKU 明细 + 折扣 + 计量单位） | both | feat | 7h | `app/sales/create_sales.go`, `web/app/sales/new/` |
| 7.2 | 库存实时校验与超库存行内警告 | both | feat | 4h | `app/sales/submit_sales.go` |
| 7.3 | 出库确认（全量/部分 + 并发锁） | both | feat | 6h | `app/sales/ship_sales.go` |
| 7.4 | 销售单应收款管理与超期标红 | both | feat | 5h | `app/finance/` |
| 7.5 | 销售退货（红冲）与库存恢复 | both | feat | 5h | `app/sales/cancel_sales.go` |
| 7.6 | 销售单打印（送货单/对账单 + 中文大写） | both | feat | 5h | `web/styles/print.css`, `components/print/` |
| 7.7 | 零售快速收款（现金/赊账/班次结算入口） | retail | feat | 5h | `web/app/sales/[id]/collect.tsx` |
| 7.8 | 销售单列表与详情页 | both | feat | 3h | `web/app/sales/page.tsx` |

#### Story 7.7 — 零售快速收款（现金/赊账/班次结算入口）
- **As a** 五金店收银员, **I want** 销售单确认时快速选择"现金收清"或"挂账赊欠", **so that** 一笔单子 30 秒内可以完成。
- **Acceptance Criteria**:
  - 销售单详情页右上角两个大按钮："收现金"（直接结清）/ "挂账"（标记赊账，自动进应收台账）
  - 班次结算入口：当日收款汇总弹窗（现金 + 微信/支付宝汇总，供店主核对）
  - "收现金"后自动更新 `finance_account.current_balance`（现金账户）
- **Profile**: `retail`
- **Out of Scope**: 微信/支付宝收单（Epic 10 实现）；打印小票（Epic 10 实现）

---

## Epic 8: Profile 机制实现

**目标**: 后端 `tenant_profile` middleware 就绪；前端侧边栏导航按 Profile 动态显示/隐藏菜单项；Profile 专属字段集（采购/销售单的跨境/零售差异字段）按 Profile 正确渲染；管理员可在设置页修改 Profile。

**PRD Requirements**: 决策锁 DL-1；无直接 PRD User Story，但是所有 Profile 相关 Story 的基础设施

**Risk**: 如果 Profile 机制做成"前端硬判断"（`if profile === 'retail'`），会散落在数十个组件里，维护灾难。必须在本 Epic 建立统一的 Profile 感知渲染框架和后端 field-set 配置，后续 E9/E10 直接调用。

**Acceptance**:
- 后端 `/api/v1/profile/field-set` 返回当前 Profile 的可用字段列表
- 侧边栏：跨境菜单（多币种/HS Code/海运）在 retail 下隐藏；零售菜单（POS/称重）在 cross_border 下隐藏
- 采购单/销售单表单：字段按 Profile 配置动态显隐（无需前端硬编码 if/else）
- 设置页可切换 Profile，切换后 UI 即时更新

**依赖**: Epic 3（ProfileGate 组件）, Epic 6（采购单字段）, Epic 7（销售单字段）

**预估**: 5 Stories × 平均 6h = **30h**

### Stories

| # | Title | Profile | 类型 | 工时 | 关键文件 |
|---|-------|---------|------|------|---------|
| 8.1 | 后端 Profile Field-Set 配置与 middleware | both | feat | 6h | `app/profile/field_set.go`, `adapter/middleware/profile.go` |
| 8.2 | 侧边栏导航按 Profile 动态渲染 | both | feat | 5h | `components/layout/sidebar.tsx` Profile 条件 |
| 8.3 | 采购/销售单表单 Profile 感知字段集 | both | feat | 7h | `components/bill/bill-form.tsx` + field-set 驱动 |
| 8.4 | 商品 Profile 字段差异（HS Code vs 五金规格属性） | both | feat | 5h | `components/product/product-form.tsx` |
| 8.5 | 设置页 Profile 切换与 UI 即时刷新 | both | feat | 5h | `web/app/(dashboard)/settings/profile/page.tsx` |

#### Story 8.1 — 后端 Profile Field-Set 配置与 middleware
- **As a** 系统, **I want** 后端 API 按租户 Profile 返回可用字段集, **so that** 前端无需硬编码 if/else 判断各字段是否显示。
- **Acceptance Criteria**:
  - `GET /api/v1/profile/field-set` 返回 `{ bill_fields: [...], product_fields: [...] }` 按 Profile 过滤
  - `cross_border` 字段集含: `currency_code / exchange_rate / hs_code / country_of_origin / shipping_status`
  - `retail` 字段集含: `weight_qty / bulk_unit / pos_payment_method / shift_id`
  - Gin middleware `ProfileContext` 将 `tenant_profile.profile_type` 注入 `ctx`（通过 `ProfileResolver`），handler 据此过滤响应字段
- **Tech Notes**: `app/profile/field_set.go` 定义 `FieldSet` struct；field-set 配置存代码（非 DB），避免配置漂移

---

## Epic 9: 跨境专属能力

**目标**: 跨境企业 persona 可在 Tally 中处理多币种报价/入库、记录 HS Code、追踪海运状态；V1 实现多币种显示和人工汇率录入（不接外部汇率 API）。

**PRD Requirements**: 决策锁 DL-2；PRD §4.2 多币种（V2）提前到 V1 双 persona 版本

**Risk**: 多币种的金额存储策略——必须在本 Epic 开始前锁定：所有金额以"原始货币 + 汇率 + CNY 等值"三字段存储，避免后期换算失真。存储方案在 Story 9.1 中明确。

**Acceptance**:
- 跨境 Profile 租户可在采购/销售单中选择外币，填入人工汇率，系统自动换算 CNY 等值
- HS Code 可在商品属性中存储并在单据中显示
- 海运状态字段在单据中可手动更新
- `currency_code` / `exchange_rate` / `amount_cny` 三字段在 `bill_head` DDL 中存在
- 零售 Profile 租户看不到以上字段（ProfileGate 隔离）

**依赖**: Epic 8（Profile 机制）

**预估**: 5 Stories × 平均 6h = **30h**

### Stories

| # | Title | Profile | 类型 | 工时 | 关键文件 |
|---|-------|---------|------|------|---------|
| 9.1 | 多币种存储模型（bill_head 三字段 + 汇率录入） | cross_border | feat | 7h | `migrations/` bill_head 扩展, `domain/entity/bill_head.go` |
| 9.2 | 采购/销售单外币输入与 CNY 换算显示 | cross_border | feat | 6h | `web/app/purchases/new/`, `web/app/sales/new/` |
| 9.3 | HS Code 商品属性与单据打印 | cross_border | feat | 5h | `app/product/`, `components/print/` |
| 9.4 | 海运状态追踪（手动录入，单据附注） | cross_border | feat | 5h | `bill_head.shipping_status`, `web/app/purchases/[id]/` |
| 9.5 | 跨境财务报表（外币应收/应付，按币种汇总） | cross_border | feat | 6h | `app/finance/query_payment.go`, `web/app/finance/` |

#### Story 9.1 — 多币种存储模型
- **As a** 跨境企业财务, **I want** 系统以原始外币 + 汇率 + CNY 等值三个字段存储金额, **so that** 汇率变动后历史单据金额不失真，且可随时重新换算报表。
- **Acceptance Criteria**:
  - `bill_head` 新增（migration 000019）: `currency VARCHAR(10) DEFAULT 'CNY'`，`exchange_rate NUMERIC(20,8) DEFAULT 1`，`amount_local NUMERIC(18,4)`（原始币金额；CNY 等值始终存 `total_amount`）
  - `bill_item` 新增: `unit_price_orig NUMERIC(18,4)`（原始币单价）
  - 创建单据时，用户输入原始外币金额 + 当日汇率，系统自动计算 CNY 等值并存储
  - 历史单据的 CNY 等值不随汇率变化重算（快照语义）
- **Profile**: `cross_border`
- **Tech Notes**: 迁移文件 `000019_add_currency.up.sql`（000013 已被 tenant_profile 占用）；多币种字段为 `currency`（非 `currency_code`）、`amount_local`（非 `total_amount_cny`）；所有财务汇总报表以 `total_amount`（CNY）为基准，`amount_local` 存原币金额

#### Story 9.5 — 跨境财务报表（外币应收/应付）
- **As a** 跨境企业财务, **I want** 应收/应付台账按币种分组汇总, **so that** 可以分别看 USD / EUR / CNY 的欠款情况。
- **Acceptance Criteria**:
  - 台账支持"按币种"筛选 Tab（CNY / USD / EUR / 其他）
  - 每个币种显示：原始金额 / 汇率 / CNY 等值
  - 合计行仅汇总 CNY 等值（跨币种无法直接相加）
- **Profile**: `cross_border`

---

## Epic 10: 零售专属能力

**目标**: 五金店 persona 可用 POS 界面快速收银；支持 ESC/POS 小票打印；接入微信/支付宝收单（展示二维码）；会员卡积分；称重秤 USB/串口集成（读取重量自动填入数量）。

**PRD Requirements**: 决策锁 DL-2；PRD §13 "永远不做 POS" — 注意：PRD 原文 POS 在 Out of Scope，但双 persona 方向已锁定零售能力。此处按用户指令执行，覆盖原 PRD §13 的 POS 禁令。

**Risk**: 称重秤集成——浏览器 Web Serial API 兼容性有限（Chrome 89+），Safari 不支持。需在 Story 10.4 早期验证目标设备（Windows 收银机/iPad）的浏览器环境，必要时降级为手动输入 + 快捷键触发。

**Acceptance**:
- 零售 Profile 租户有"收银台"页面，可快速完成一笔收银 < 30 秒
- ESC/POS 小票打印在连接打印机时可工作
- 微信/支付宝二维码展示可用（不要求自动确认，V2 接 Open API 做自动确认）
- 会员卡积分可录入和查询
- 跨境 Profile 租户看不到以上入口

**依赖**: Epic 8（Profile 机制），Epic 7（销售单基础）

**预估**: 6 Stories × 平均 7h = **42h**

### Stories

| # | Title | Profile | 类型 | 工时 | 关键文件 |
|---|-------|---------|------|------|---------|
| 10.1 | POS 收银台界面（商品快捷选择 + 数量 + 合计） | retail | feat | 8h | `web/app/(dashboard)/pos/page.tsx`, `components/pos/` |
| 10.2 | ESC/POS 小票打印（USB 热敏打印机） | retail | feat | 7h | `components/pos/receipt-printer.ts`（Web Serial API） |
| 10.3 | 微信/支付宝收款二维码展示（手动确认） | retail | feat | 6h | `web/app/(dashboard)/pos/payment/page.tsx` |
| 10.4 | 称重秤 USB 集成（Web Serial API 读取重量） | retail | feat | 7h | `hooks/use-scale.ts`（Web Serial API） |
| 10.5 | 会员卡系统（积分录入/兑换/查询） | retail | feat | 7h | `app/member/`, `domain/entity/member.go` |
| 10.6 | 班次结算（当日收款汇总 + 交班报表） | retail | feat | 7h | `app/shift/`, `web/app/(dashboard)/shift/page.tsx` |

#### Story 10.1 — POS 收银台界面
- **As a** 五金店收银员, **I want** 一个全屏收银台界面，快速点选商品/扫码/输入数量后一键收款, **so that** 每笔收银不超过 30 秒。
- **Acceptance Criteria**:
  - 左侧: 商品快捷列表（按分类分组，每个 tile 显示商品图/名/价）；右侧: 当前购物车 + 合计
  - 扫码枪扫码自动加入购物车（同 Epic 4 barcode 逻辑）
  - 称重商品自动从秤读取重量（Story 10.4 完成后对接）
  - 结算弹窗：现金/微信/支付宝三个大按钮；现金需输入收款金额自动计算找零
  - 收银完成自动触发 Story 7 销售单创建（后台异步）
- **Profile**: `retail`
- **Tech Notes**: `/pos/page.tsx` 独立于主布局（全屏，隐藏侧边栏）；Zustand cart store 管理购物车状态

#### Story 10.4 — 称重秤 USB 集成
- **As a** 五金店仓管, **I want** 把螺丝放上秤后，系统自动读取重量并填入开单数量, **so that** 不需要手动输入重量减少错误。
- **Acceptance Criteria**:
  - `useScale()` hook 通过 Web Serial API 监听串口数据，解析重量值（支持常见称重协议：OHAUS/Toledo 简单格式）
  - 在商品 `measurement_strategy = 'weight'` 的行，数量列旁边显示"从秤读取"按钮
  - 秤不可用时（浏览器不支持/串口未授权）降级为手动输入，不报错
  - 测试：用模拟串口数据验证重量解析正确
- **Profile**: `retail`
- **Out of Scope**: 自动识别称重秤型号（V1 仅支持手动配置波特率/协议）

#### Story 10.5 — 会员卡系统
- **As a** 五金店老板, **I want** 记录老顾客的积分并在下次消费时兑换, **so that** 提升回头客黏性。
- **Acceptance Criteria**:
  - 会员注册：手机号 + 姓名 + 初始积分 = 0
  - 每笔销售单关联会员（按手机号搜索）；积分规则：每消费 ¥1 = 1 积分（`system_config` 可配置比例）
  - 积分兑换：兑换时自动抵扣对应金额（100 积分 = ¥1，可配置）
  - 会员查询：手机号搜索后显示历史消费 + 积分余额
- **Profile**: `retail`
- **Tech Notes**: `domain/entity/member.go`，`member_points_ledger`（积分流水）

---

## Epic 11: Onboarding 向导（双 persona）

**目标**: 新用户注册后 5 分钟内完成首次开单；跨境企业和五金店各有专属 onboarding 路径；演示数据按 Profile 预置真实场景（跨境：汇率单据 / 零售：散装商品 + 会员）。

**PRD Requirements**: PRD §5 Journey 1；PRD §1.3（平均首次开单时间 < 10 分钟）；决策锁 DL-2

**Risk**: 演示数据的维护成本——两套种子数据（cross_border / retail）如果 hardcode 在 SQL，后续商品模型变更会导致种子失效。需要用程序化种子生成，而非静态 SQL。

**Acceptance**:
- 跨境路径：注册 → 选 cross_border → 演示数据含外币采购单 → 5 分钟内完成第一张跨境销售单
- 零售路径：注册 → 选 retail → 演示数据含散装商品/会员 → 5 分钟内完成第一笔 POS 收银
- Onboarding 完成率埋点（注册到首次开单的漏斗）

**依赖**: Epic 8（Profile 机制）, Epic 9（跨境能力），Epic 10（零售能力）

**预估**: 5 Stories × 平均 5h = **25h**

### Stories

| # | Title | Profile | 类型 | 工时 | 关键文件 |
|---|-------|---------|------|------|---------|
| 11.1 | Onboarding 向导框架（Profile 分叉路径） | both | feat | 5h | `web/app/(onboarding)/`, `components/onboarding/` |
| 11.2 | 跨境 Onboarding 路径（汇率设置 + 第一张采购单引导） | cross_border | feat | 5h | `web/app/(onboarding)/cross-border/` |
| 11.3 | 零售 Onboarding 路径（POS 设置 + 第一笔收银引导） | retail | feat | 5h | `web/app/(onboarding)/retail/` |
| 11.4 | Profile 专属演示数据（程序化种子生成） | both | feat | 6h | `internal/seed/`, `cmd/seed/main.go` |
| 11.5 | Onboarding 完成率埋点与首次开单引导 CTA | both | feat | 4h | `components/onboarding/progress-tracker.tsx` |

---

---

## V2 — 边缘部署 & 离线优先

---

## Epic 12: 边缘端 Go Binary（五金店本地部署）

**目标**: Tally 可以编译为独立 Go binary，使用 SQLite 作为本地存储，单 tenant 模式运行，不依赖云端 PostgreSQL；部署在五金店 Windows 收银机或 Linux mini PC 上，断网时 POS 功能完整可用。

**PRD Requirements**: 决策锁 DL-5 / DL-6；V2 边缘部署

**Risk**: SQLite 与 PostgreSQL 的 SQL 方言差异——GORM 可切换 dialect，但 PostgreSQL 特有语法（RLS、JSONB `->>`、`NUMERIC`精度、`uuid_generate_v4()`）需要在 edge build tag 下有兼容实现。这是 V2 最高技术风险，需要在 Story 12.1 中做 PoC。

**Acceptance**:
- `go build -tags edge ./cmd/server` 编译成功，生成 ~30MB SQLite 版 binary
- 该 binary 在无网络环境下启动，POS 收银/采购入库/销售出库可用
- V1 预留的 `origin` / `sync_status` 字段在 SQLite 版本中已使用（`origin='edge'`，`sync_status='pending'`）
- E2E 测试：离线 POS 收银 10 笔，之后网络恢复，数据待同步（Story 13 接管）

**依赖**: E1（基础架构）, E4（商品模型），E7（销售流程）

**预估**: 6 Stories × 平均 6h = **36h**

### Stories

| # | Title | Profile | 类型 | 工时 | 关键文件 |
|---|-------|---------|------|------|---------|
| 12.1 | Build tag edge + SQLite dialect PoC（风险验证） | retail | spike | 8h | `internal/adapter/repo/dialect/`, `cmd/server/main_edge.go` |
| 12.2 | SQLite 迁移脚本（edge 版，去除 PG 专属语法） | retail | feat | 7h | `migrations/edge/` |
| 12.3 | 单 tenant 模式（去除 RLS，本地文件存 tenant config） | retail | feat | 5h | `internal/adapter/middleware/tenant_local.go` |
| 12.4 | Edge binary 离线 POS 收银（无网络完整流程） | retail | feat | 6h | `cmd/server/main_edge.go` + POS 路由 |
| 12.5 | Edge binary 打包（Windows exe + Linux binary + 自启动） | retail | infra | 6h | `Makefile edge-build`, `deploy/edge/` |
| 12.6 | Edge binary E2E 测试（离线操作场景） | retail | test | 4h | `tests/edge/` |

---

## Epic 13: 同步引擎

**目标**: 边缘节点的本地数据在网络恢复后自动同步到云端 PostgreSQL；支持增量上行（NATS JetStream）和全量下行（HTTP Pull）；冲突检测使用向量时钟，冲突记录写入 `sync_conflict` 表待人工裁决。

**PRD Requirements**: 决策锁 DL-5；V2 同步引擎

**Risk**: 向量时钟实现复杂度——对于 V2 阶段的场景（单店铺 + 单云端，冲突极少），可以简化为"云端优先 + 上行 CAS（Compare-And-Swap）"策略，降低实现复杂度。在 Story 13.1 中决策。

**Acceptance**:
- 边缘节点数据上行：`sync_status='pending'` 的记录批量发布到 NATS JetStream
- 云端消费并 upsert 到 PostgreSQL，成功后通知边缘节点更新 `sync_status='synced'`
- 下行：云端配置变更（商品库/客户库）通过 HTTP Pull 同步到边缘 SQLite
- 冲突（同一记录在云端和边缘均被修改）写入 `sync_conflict` 表，等待 Epic 14 裁决

**依赖**: Epic 12（Edge binary）

**预估**: 5 Stories × 平均 7h = **35h**

### Stories

| # | Title | Profile | 类型 | 工时 | 关键文件 |
|---|-------|---------|------|------|---------|
| 13.1 | 同步策略决策与向量时钟/CAS 设计（spike） | retail | spike | 6h | `doc/decisions/sync-strategy.md` |
| 13.2 | 上行同步引擎（边缘 → NATS → 云端 PG） | retail | feat | 8h | `internal/sync/uplink.go`, `adapter/nats/sync_producer.go` |
| 13.3 | 下行同步引擎（云端配置 HTTP Pull → 边缘 SQLite） | retail | feat | 7h | `internal/sync/downlink.go` |
| 13.4 | 冲突检测与 sync_conflict 表写入 | retail | feat | 7h | `internal/sync/conflict_detector.go`, `domain/entity/sync_conflict.go` |
| 13.5 | 同步状态监控（边缘节点同步日志 + 云端消费延迟告警） | retail | feat | 7h | `internal/sync/monitor.go`, Prometheus 指标 |

---

## Epic 14: 冲突裁决 UI

**目标**: 系统管理员可在云端 Admin 界面看到所有未裁决冲突，逐条查看"云端版本 vs 边缘版本"的 diff，选择采用哪一方或手动合并，裁决结果自动同步回边缘节点。

**PRD Requirements**: 决策锁 DL-5；V2 冲突裁决

**Risk**: 冲突 diff 展示——对于非技术人员（店主），需要将字段级 diff 翻译成业务语言（"云端库存 50 件 vs 门店库存 48 件"，而非 JSON diff）。

**Acceptance**:
- 冲突列表页：显示记录类型、冲突时间、两端摘要
- 冲突详情：业务语言 diff（按字段中文名展示，不是 JSON）
- 裁决操作：采用云端 / 采用边缘 / 手动合并（仅简单字段）
- 裁决结果触发同步引擎将结果推送回边缘节点

**依赖**: Epic 13（同步引擎）

**预估**: 4 Stories × 平均 5h = **20h**

### Stories

| # | Title | Profile | 类型 | 工时 | 关键文件 |
|---|-------|---------|------|------|---------|
| 14.1 | 冲突列表页（sync_conflict 查询 + 状态筛选） | retail | feat | 5h | `web/app/(dashboard)/admin/conflicts/page.tsx` |
| 14.2 | 冲突详情业务语言 diff 展示 | retail | feat | 6h | `components/conflict/conflict-diff.tsx` |
| 14.3 | 裁决操作（采用云端/边缘/手动合并） | retail | feat | 5h | `app/conflict/resolve_conflict.go` |
| 14.4 | 裁决结果回写边缘节点（触发下行同步） | retail | feat | 4h | `internal/sync/downlink.go` 扩展 |

---

## Epic 15: PWA 离线壳

**目标**: Tally Web 应用可以作为 PWA 安装到 Windows/Mac/iOS/Android；Service Worker 缓存关键资源和 API 响应；网络中断时 UI 展示"离线模式"提示，POS 基础操作继续可用（通过 IndexedDB 兜底）。

**PRD Requirements**: 决策锁 DL-5；V2 PWA 离线

**Risk**: IndexedDB 与 SQLite 的双重存储——Edge binary 已有 SQLite，PWA 模式的 IndexedDB 是额外兜底层，两者数据不互通。V2 的 PWA 离线模式主要服务于"临时断网"场景（<1 小时），不替代 Edge binary 的"长期离线"。需在 Story 15.1 中明确边界。

**Acceptance**:
- Tally 可被浏览器"安装为应用"（PWA manifest 完整）
- 断网时：商品列表、最近单据列表从 Service Worker 缓存返回（只读）
- 断网时：POS 收银新建销售单暂存 IndexedDB，恢复网络后自动上传
- 离线状态：顶栏显示"离线模式 - 数据将在恢复连接后同步"

**依赖**: Epic 13（同步引擎，提供上传接口）

**预估**: 5 Stories × 平均 5h = **25h**

### Stories

| # | Title | Profile | 类型 | 工时 | 关键文件 |
|---|-------|---------|------|------|---------|
| 15.1 | PWA 范围定义与 Service Worker 框架 | retail | spike | 5h | `web/public/sw.js`, `web/app/manifest.json` |
| 15.2 | 关键资源缓存策略（商品库/最近单据） | retail | feat | 5h | `sw.js` 缓存策略 |
| 15.3 | 离线 POS 草稿暂存（IndexedDB + 自动上传） | retail | feat | 6h | `hooks/use-offline-queue.ts`, `lib/idb.ts` |
| 15.4 | 离线状态 UI（顶栏指示 + 降级提示） | retail | feat | 4h | `components/layout/topbar.tsx` 网络状态 |
| 15.5 | PWA 安装引导（首次访问 install prompt） | retail | feat | 5h | `components/pwa/install-prompt.tsx` |

---

## Epic 16: 边缘节点管理

**目标**: 系统管理员可在云端 Dashboard 查看所有已注册的边缘节点（店铺）、心跳状态、版本号；边缘节点可自动检测新版本并提示更新；节点注册使用一次性激活码。

**PRD Requirements**: 决策锁 DL-5；V2 边缘节点管理

**Risk**: 自动 self-update 在 Windows 环境下需要替换运行中的 binary，Windows 文件锁问题需要额外处理（先写新文件、重命名、重启）。

**Acceptance**:
- 边缘节点首次启动时向云端注册（激活码 + 设备信息）
- 云端节点列表显示：节点名称/店铺/版本/最后心跳时间/同步状态
- 边缘节点每 5 分钟上报心跳；超过 30 分钟无心跳 → 告警
- 新版本发布后，边缘节点检测到版本差异 → 显示"有新版本可更新"提示

**依赖**: Epic 12（Edge binary），Epic 13（同步引擎）

**预估**: 4 Stories × 平均 5h = **20h**

### Stories

| # | Title | Profile | 类型 | 工时 | 关键文件 |
|---|-------|---------|------|------|---------|
| 16.1 | 边缘节点注册与激活码机制 | retail | feat | 5h | `app/edge_node/register.go`, `domain/entity/edge_node.go` |
| 16.2 | 节点心跳上报与云端列表页 | retail | feat | 5h | `app/edge_node/heartbeat.go`, `web/app/(dashboard)/admin/nodes/` |
| 16.3 | 版本检测与 self-update 提示 | retail | feat | 6h | `internal/updater/`, `cmd/server/main_edge.go` |
| 16.4 | 节点告警（心跳超时 + 同步积压） | retail | feat | 4h | `lifecycle/worker.go` 告警 task + 通知 |

---

## V3 — 高级 AI

---

## Epic 17: AI 补货 Agent（Kova 集成）

**目标**: Kova Agent 每日自动分析历史销量 + 安全库存，生成补货建议并以"待办卡片"形式推送到 Dashboard；用户可一键采纳（跳转预填采购单）或忽略（7 天不重推）。

**PRD Requirements**: US-9.2；PRD §9.2 Kova 补货 Agent；PRD §6.9 AI 助手 P0

**Risk**: 小数据量客户（SKU < 200，历史 < 3 个月）预测准确率不足——V3 必须先建 baseline（M3 收集 Lighthouse 客户数据），再启动 Agent 开发；不能在没有数据的情况下上线。

**Acceptance**:
- Kova Agent 每日 09:00 运行；基于 90 天历史销量计算预计断货时间
- 建议卡片出现在 Dashboard 待办区（带预测依据：日均销量/当前库存/预计断货天数）
- 用户采纳 → 跳转预填采购单（SKU/数量已填）；忽略 → 7 天不重推；暂缓 → 3 天后重推
- V3 仅建议，不自动提交采购单

**依赖**: V1 E10（报表数据）, V1 E11（AI Drawer 基础）

**预估**: 5 Stories × 平均 7h = **35h**

### Stories

| # | Title | Profile | 类型 | 工时 | 关键文件 |
|---|-------|---------|------|------|---------|
| 17.1 | 历史销量 Baseline 数据服务 + 预测模型接口 | both | feat | 8h | `app/ai_agent/sales_baseline.go` |
| 17.2 | Kova 补货 Agent 注册与每日触发 | both | feat | 7h | `app/ai_agent/reorder_agent.go`, `adapter/kova/` |
| 17.3 | 补货建议持久化与 Dashboard 待办卡片 | both | feat | 6h | `domain/entity/agent_recommendations.go`, `web/app/dashboard/` |
| 17.4 | 采纳/忽略/暂缓决策系统 | both | feat | 6h | `handler/v1/ai.go`, `components/recommendation-card.tsx` |
| 17.5 | 补货 Agent 精度监控（采纳率 + 实际结果追踪） | both | feat | 8h | `app/ai_agent/accuracy_tracker.go` |

---

## Epic 18: 动态定价（跨境专属）

**目标**: 跨境 persona 可在 Dashboard 看到 AI 推荐的销售价格调整建议，基于汇率变动 + 历史利润率 + 竞品参考价（手动录入）；建议以"调价建议卡片"形式呈现，用户确认后一键批量调价。

**PRD Requirements**: 决策锁 DL-6（高级 AI 延后到 V3）；PRD §2.2（简道云 AI 动态定价是竞品护城河，Tally 需超越）

**Risk**: 竞品价格数据来源——V3 只支持手动录入参考价，不爬取竞品网站（法律风险）。AI 建议完全基于内部数据（汇率 + 历史利润率）。

**Acceptance**:
- 系统自动检测 `exchange_rate` 变动 > 2%，触发"调价建议"
- 建议含：当前售价、建议新售价、调整理由（汇率变动 X% + 当前利润率 Y%）
- 用户可批量选择 SKU 执行调价

**依赖**: V1 E9（多币种）, V3 E17（AI 框架）

**预估**: 4 Stories × 平均 7h = **28h**

### Stories

| # | Title | Profile | 类型 | 工时 | 关键文件 |
|---|-------|---------|------|------|---------|
| 18.1 | 汇率变动检测与调价触发机制 | cross_border | feat | 6h | `lifecycle/worker.go` 汇率监控 task |
| 18.2 | AI 调价建议生成（Hub 调用 + 利润率分析） | cross_border | feat | 8h | `app/ai_agent/pricing_agent.go` |
| 18.3 | 调价建议 UI + 批量执行 | cross_border | feat | 7h | `web/app/(dashboard)/pricing/page.tsx` |
| 18.4 | 调价历史追踪与效果分析 | cross_border | feat | 7h | `app/report/pricing_report.go` |

---

## Epic 19: 自然语言查询 Hub（NL Query）

**目标**: 用户可通过 ⌘K → AI Drawer 用中文自然语言查询库存/销售/财务数据；Hub API Function Calling 驱动 SQL 查询；首字节响应 < 1.5s；回答中嵌入操作快捷按钮。

**PRD Requirements**: US-9.1；PRD §6.9 AI 助手 P0；PRD §9.1 Hub 自然语言查询

**Risk**: Hub API 在中国大陆 P95 延迟可能 > 3s——流式输出是必须（首字节 < 1.5s 可接受），长查询需要异步处理 + WebSocket 通知。Story 19.1 完成后立即压测。

**Acceptance**:
- 6 类查询（库存/销售/应收应付/预警/补货估算/报表生成）在 AI Drawer 可用
- Hub Function Calling 工具集：`query_stock / query_sales / query_ar_ap / query_alerts`
- SSE 流式输出，首字节 < 1.5s P95
- AI 不执行写操作，仅返回数据 + 建议 + 跳转预填链接

**依赖**: V1 E3（AI Drawer 框架）, V1 E10（报表数据）

**预估**: 4 Stories × 平均 7h = **28h**

### Stories

| # | Title | Profile | 类型 | 工时 | 关键文件 |
|---|-------|---------|------|------|---------|
| 19.1 | Hub AI 自然语言查询后端（SSE + Function Calling） | both | feat | 8h | `app/ai_agent/chat.go`, `function_registry.go` |
| 19.2 | AI Drawer 对接真实 Hub API（流式渲染 + 操作按钮） | both | feat | 7h | `components/ai-drawer/`, `use-ai-chat.ts` |
| 19.3 | ⌘K AI 查询入口与热门查询建议 | both | feat | 6h | `components/command-palette/commands.ts` |
| 19.4 | Kova 滞销预警 Agent（每日运行 + Dashboard 卡片） | both | feat | 7h | `app/ai_agent/deadstock_agent.go` |

---

## Epic 20: 智能记忆（Memorus 集成）

**目标**: 店主可以问"那个张大哥上次买啥"，系统通过 Memorus RAG 找到历史订单上下文并在 AI Drawer 中自然语言回答；销售/库存事件实时写入 Memorus，提升 AI 预测精度。

**PRD Requirements**: PRD §9.3 Memorus RAG 上下文；PRD §6.9 Memorus 上下文 P1

**Risk**: Memorus API 稳定性——`2b-svc-memorus` 目前是 Python 服务，延迟 P95 未知。AI Drawer 查询需要设超时（3s），超时降级为无 RAG 上下文的普通查询。

**Acceptance**:
- 销售单确认出库后，异步写入 Memorus（SKU + 客户 + 时间）
- AI Drawer 查询时自动附带 Memorus RAG 上下文（< 1s 超时降级）
- 自然语言查询"张大哥上次买啥"能正确返回最近一笔订单
- `product.embedding`（`vector(1536)`）字段在 V3 开始写入，用于商品相似度推荐

**依赖**: V3 E19（NL Query Hub）, V1 E4（商品 embedding 字段预留）

**预估**: 4 Stories × 平均 8h = **32h**

### Stories

| # | Title | Profile | 类型 | 工时 | 关键文件 |
|---|-------|---------|------|------|---------|
| 20.1 | 销售/库存事件写入 Memorus（异步，NATS 触发） | both | feat | 8h | `adapter/memorus/writer.go`, `lifecycle/worker.go` |
| 20.2 | AI 查询 Memorus RAG 上下文增强（< 1s 超时降级） | both | feat | 8h | `app/ai_agent/chat.go` RAG 增强 |
| 20.3 | 客户购买历史自然语言查询（"张大哥上次买啥"） | both | feat | 8h | `app/ai_agent/function_registry.go` 新增工具 |
| 20.4 | 商品 embedding 写入与相似商品推荐 | both | feat | 8h | `app/product/embedding.go`, `adapter/hub/embedding.go` |

---

## PRD 需求覆盖矩阵（v2）

| PRD 需求 | Epic | Story | 版本 | Profile |
|---------|------|-------|------|---------|
| US-1.1 OIDC 注册创建企业 | E2 | 2.1, 2.2 | V1 | both |
| US-1.2 邀请成员分配角色 | E2 | 2.5 | V1 | both |
| US-2.1 多 SKU 商品创建 | E4 | 4.2, 4.3, 4.4 | V1 | both |
| US-2.2 Excel 批量导入 | E4 | 4.9 | V1 | both |
| US-2.3 条码枪扫码定位 | E4 | 4.7 | V1 | both |
| US-2.4 安全库存阈值预警 | E4 | 4.8 | V1 | both |
| US-2.5 批次管理（FIFO 出货） | E4 | 4.6 | V1 | both |
| US-2.6 序列号管理 | E4 | 4.6 | V1 | both |
| US-3.1 多仓库独立库存 | E5 | 5.1, 5.2 | V1 | both |
| US-3.2 库存六状态 | E5 | 5.2 | V1 | both |
| US-3.3 多渠道库存视图预留 | E5 | 5.2 | V1 | both |
| US-4.1 采购单 Stepper 创建 | E6 | 6.1 | V1 | both |
| US-4.2 入库确认 + WAC 重算 | E6 | 6.3 | V1 | both |
| US-4.3 应付款状态查询 | E6 | 6.4 | V1 | both |
| US-4.4 反审/红冲 | E6 | 6.5 | V1 | both |
| US-5.1 销售单创建 + 出库确认 | E7 | 7.1, 7.3 | V1 | both |
| US-5.2 行级/单据级折扣 | E7 | 7.1 | V1 | both |
| US-5.3 应收款 + 超期标红 | E7 | 7.4 | V1 | both |
| US-5.4 销售退货红冲 | E7 | 7.5 | V1 | both |
| US-6.1 跨仓调拨单 | E6 | 6 (调拨并入 E6) | V1 | both |
| US-6.2 整仓盘点 | E6 | 6 (盘点并入 E6) | V1 | both |
| US-6.3 循环盘点 | E6 | 6 (盘点并入 E6) | V1 | both |
| US-7.1 应收账款台账 | E7 | 7.4 | V1 | both |
| US-7.2 应付账款台账 | E6 | 6.4 | V1 | both |
| US-7.3 资金账户余额 | E7 | 7.7 (财务) | V1 | both |
| US-8.1 Dashboard KPI 卡 | E10 (调整为 E11 前置) | — | V1 | both |
| US-8.2 库存周转率报表 | E10 | — | V1 | both |
| US-8.3 ABC 分析 | E10 | — | V1 | both |
| US-8.4 滞销预警报表 | E10 | — | V1 | both |
| US-9.1 ⌘K + AI Drawer | E19 | 19.1-19.3 | V3 | both |
| US-9.2 Kova 补货 Agent | E17 | 17.1-17.4 | V3 | both |
| US-9.3 滞销预警推送 | E19 | 19.4 | V3 | both |
| DL-1 Profile 机制 | E8 | 8.1-8.5 | V1 | both |
| DL-2 跨境双 persona | E9 | 9.1-9.5 | V1 | cross_border |
| DL-2 零售双 persona | E10 | 10.1-10.6 | V1 | retail |
| DL-3 商品模型升级 | E4 | 4.3, 4.4, 4.5 | V1 | both |
| DL-4 库存策略 Strategy Pattern | E5 | 5.3 | V1 | both |
| DL-5 离线容器字段 V1 预留 | E1/E4/E5 | 1.3, 4.3, 5.2 | V1 | retail |
| DL-5 边缘部署实施 | E12-E16 | — | V2 | retail |
| DL-6 高级 AI | E17-E20 | — | V3 | both |
| PRD §13 POS（原 Out of Scope） | E10 | 10.1-10.6 | V1（覆盖原禁令，双 persona 方向已锁定）| retail |

**覆盖说明**:
- 原 PRD 单 persona 需求（US-1.1 ~ US-9.3）全部覆盖
- 新增 Decision Lock 需求（DL-1 ~ DL-6）全部覆盖
- PRD §13 POS 禁令已被双 persona 方向覆盖，E10 正式实现零售收银能力
- 调拨（US-6.1）和盘点（US-6.2, 6.3）从独立 Epic 8 合并进 E6（采购闭环扩展），Story 编号在新 Epic 结构中待 SM 细化

---

## 工时预估汇总

| 阶段 | Epic | Story 数 | 总工时 | Sprint（单人，56h/sprint）|
|------|------|---------|-------|--------------------------|
| V1 — 双 Profile MVP | E1-E11 | 78 | ~461h | ~8.2 sprint |
| V2 — 边缘部署 | E12-E16 | 24 | ~136h | ~2.4 sprint |
| V3 — 高级 AI | E17-E20 | 16 | ~123h | ~2.2 sprint |
| **合计** | E1-E20 | **118** | **~720h** | **~12.8 sprint** |

双人并行系数约 0.6（并行效率折扣）：
- V1 双人: ~5 sprint（10 周）
- V2 双人: ~1.5 sprint（3 周）
- V3 双人: ~1.5 sprint（3 周）

---

## V2.5 横向增强带（E21-E27）

> 详见 `./roadmap-ux-supplement.md`。在 V2 完成后注入，与 V3 可并行。
> 焦点：可恢复性 / 性能 / 主动 AI（push）/ 协作 / 信任 / 输入生态 / 业务深度。

| Epic | 主题 | 工时 | 触发条件 |
|------|------|------|---------|
| E21 | 可恢复性（草稿+Cmd+Z+trail+"我做到哪了"） | ~60h | **首要 — 立即** |
| E22 | 性能基线（虚拟滚动+预取+流式） | ~40h | 单租户 SKU > 5k |
| E23 | 主动 AI 推送（断货预警+周报+OCR） | ~80h | 立即（复用 AI Drawer）|
| E24 | 协作权限（角色+审批+@评论） | ~70h | 5+ 人客户 |
| E25 | 输入/输出生态（扫码枪+打印机+微信钉钉+API） | ~90h | 实店签约 |
| E26 | 信任可观测（审计+健康度+变更日志） | ~50h | 5+ 人客户 |
| E27 | 深度业务（多仓/批次/BOM/电商同步） | 60-100h × N | 付费驱动 |

---

## V3-Horticulture（园林/苗木垂直行业，E28-E32）

> 详见 `./horticulture-extension.md`。第一个真实垂直行业落地。

| Epic | 主题 | 工时 | 优先级 |
|------|------|------|------|
| E28 | 项目核算+多维汇总+苗木字典+价格分级 | ~180h | 🔴 立即 (MVP H1) |
| E29 | 进销项发票+询价记录+价格历史 | ~70h | 🔴 立即 (MVP H2) |
| E30 | 多账套（同法人）+ 跨账套汇总 | ~60h | 🟡 GA H |
| E31 | 主动智能（季节/缺口/质保/损耗/现金流）| ~50h | 🟡 GA H |
| E32 | 移动端优先 PWA（现场录单/扫码盘点） | ~80h | 🟢 Field H |
- **双人总计: ~8 sprint（16 周）**

---

## 与 PRD/Architecture 重写的协调点

以下是本 Epics 文档依赖新 PRD/Architecture 重写完成后需要对齐的内容（并行重写中，当前为假设）：

| 协调项 | 本文档当前假设 | 需要新文档确认 |
|--------|--------------|----------------|
| `tenant_profile` 字段 DDL | 独立表 `tally.tenant_profile`，字段 `profile_type VARCHAR(20)`，migration 000013 | **已确认** — architecture §3.1 |
| `measurement_strategy` 枚举值 | `individual / weight / length / volume / batch / serial` | **已确认** — architecture migration 000015 |
| 多币种存储字段 | `bill_head` 增加 `currency / exchange_rate / amount_local`，migration 000019 | **已确认** — architecture §3.7（注：字段名为 `currency`+`amount_local`，非 `currency_code`+`total_amount_cny`；Story 9.1 Tech Notes 需按此更新） |
| `origin / sync_status / edge_node_id / edge_timestamp` V2 预留字段 | 存在于 `bill_head / bill_item / payment_head / stock_initial`（migration 000016） | **已确认** — architecture §3.5；`stock_snapshot` 不加（服务端聚合，不来自边缘直写） |
| Edge binary build tag | `-tags edge` 切换 `modernc.org/sqlite`（纯 Go，无 CGO） | **已确认** — architecture §6.1 + ADR-010 |
| POS 页面路由 | `web/app/(dashboard)/pos/page.tsx`（独立布局，隐藏侧边栏） | **已确认** — architecture §13.3 + PRD §13.1 风险缓解 |
| 会员表命名 | `member` + `member_points_ledger` | **待确认** — architecture v2 未覆盖会员模块（V1 Epic 10.5 属于双 persona 扩展，架构文档未列表） |
| NATS stream 扩展 | `PSI_EVENTS` 增加 5 个 `tally.edge.*` / `psi.exchange_rate.updated` 主题 | **已确认** — architecture §11 |
