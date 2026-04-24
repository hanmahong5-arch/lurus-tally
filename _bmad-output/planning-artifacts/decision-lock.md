# Lurus Tally — Decision Lock

> 版本: 2.0 | 日期: 2026-04-23 | 状态: LOCKED
> 本文件是跨三份规划文档（prd.md / architecture.md / epics.md）的硬约束速查表。
> 任何变更必须同时更新三份文档并在此追加变更记录。

---

## §1 核心原则

| # | 规则 | 出处 |
|---|------|------|
| R-1 | Profile 切换只在 UI 层（组件 props + CSS 变量）处理，业务逻辑和 API 层不接受 Profile 参数——一套 API 服务所有 Profile | PRD §13.1 风险缓解 |
| R-2 | 所有 LLM 调用必须走 Hub（api.lurus.cn），边缘端 AI 功能降级为本地报表 | Architecture §22 |
| R-3 | 财务金额字段和库存数量字段禁止 last-write-wins 自动合并，必须走 `sync_conflict` 表人工裁决 | PRD §7.2 + Architecture §6.4 |
| R-4 | 库存变更唯一路径：只通过 `InventoryCalculator.ApplyMovement` 触发，禁止直接 `UPDATE stock_snapshot` | Architecture §22 |
| R-5 | 所有金额字段 `NUMERIC(18,4)`，汇率字段 `NUMERIC(20,8)`，禁止 float64 | Architecture §22 |

---

## §2 V1 产品决策（LOCKED）

### DL-1: 双 Persona 锁定

| 决策 | 内容 |
|------|------|
| **锁定** | V1 支持两个 Persona：`cross_border`（跨境贸易企业）和 `retail`（五金/本地零售）|
| **hybrid 状态** | `hybrid` 枚举值在 V1 DB schema 建立（`tenant_profile.profile_type` CHECK 约束含此值），但 V1 不实现 hybrid 专属 UI 路径；hybrid 作为 V2.5 选项预留 |
| **切换规则** | 租户创建后 90 天内可免费切换一次；切换不删数据；90 天后走订阅变更流程 |

### DL-2: Profile 边界

| 决策 | 内容 |
|------|------|
| **Profile 仅在 UI 层** | Profile 感知渲染通过 `useProfile()` hook + `<ProfileGate>` 组件实现，不下沉到 Go API/business logic |
| **API 不分叉** | 同一 endpoint 按 Profile 返回字段超集；不做 `/cross-border/` vs `/retail/` URL 分叉（ADR-009）|
| **Profile 存储** | 独立表 `tally.tenant_profile`，字段 `profile_type VARCHAR(20)`，migration 000013（不在 `tenant` 表加列）|

### DL-3: 商品计量枚举（最终值）

`product.measurement_strategy` 枚举，migration 000015：

| 值 | 场景 |
|----|------|
| `individual` | 标准件（默认） |
| `weight` | 按重量散装（螺丝/散粮）|
| `length` | 按长度（钢管/电缆）|
| `volume` | 按体积（液体）|
| `batch` | 批次管理（食品/医药）|
| `serial` | 序列号管理（贵重品）|

**注**：epics.md 旧值 `unit_count / by_weight / bulk / lot_based` 已废弃，以上为唯一权威枚举。

### DL-4: 库存成本策略名称（最终值）

`tenant_profile.inventory_method` CHECK 约束值，migration 000013：

| DB 值 | Go 实现类 | 说明 |
|--------|-----------|------|
| `fifo` | `FIFOCalculator` | 先进先出（cross_border 默认）|
| `wac` | `WeightedAvgCalculator` | 移动加权平均（retail/hybrid 默认）|
| `by_weight` | `ByWeightCalculator` | 散装称重 |
| `batch` | `BatchCalculator` | 批次独立成本（FEFO）|
| `bulk_merged` | `BulkMergedCalculator` | 散装跨批次合并 |

**注**：PRD §附 列出的 `WAC / WeightedUnit / BatchLot / BulkMerge` 为人类可读别名；DB 约束和 Go 文件名以上表为准。

### DL-5: 离线字段 V1 建好 V2 激活

migration 000016 在 `bill_head / bill_item / payment_head / stock_initial` 四张表加以下 4 列：

| 字段 | 类型 | V1 行为 | V2 行为 |
|------|------|---------|---------|
| `origin` | `VARCHAR(10) CHECK IN ('cloud','edge')` DEFAULT `'cloud'` | 所有记录写 `cloud` | 边缘写 `edge` |
| `sync_status` | `VARCHAR(20) CHECK IN ('synced','pending','conflict')` DEFAULT `'synced'` | 所有记录写 `synced` | 边缘写 `pending` |
| `edge_node_id` | `UUID NULL` | 始终 NULL | 边缘写节点 ID |
| `edge_timestamp` | `TIMESTAMPTZ NULL` | 始终 NULL | 边缘写本地时间（UTC）|

**注**：`stock_snapshot` 不加此 4 列（服务端聚合，不来自边缘直写）。`edge_created_at` 已废弃，统一用 `edge_timestamp`。

### DL-6: POS 路由隔离

`retail` Profile 的 POS 收银台走独立路由 `/pos`（`web/app/(dashboard)/pos/page.tsx`），与主应用完全分离渲染，隐藏侧边栏和顶栏导航，互不干扰（Architecture §13.3）。

### DL-7: 边缘 SQLite 驱动

边缘 binary（`-tags edge`）使用 `modernc.org/sqlite`（纯 Go，`CGO_ENABLED=0`），禁止 `mattn/go-sqlite3`（需要目标平台 GCC）。理由：交叉编译（Windows/Linux/macOS amd64+arm64）零成本（ADR-010）。

---

## §3 命名标准（跨三份文档统一后的权威字段名）

| 字段/概念 | 权威名称 | 废弃名称 | 所在表 |
|-----------|---------|---------|--------|
| 租户行业配置字段 | `tenant_profile.profile_type` | `tenant.profile` | `tally.tenant_profile` |
| 边缘时间戳字段 | `edge_timestamp` | `edge_created_at` | `bill_head` 等（migration 000016）|
| 多币种原币金额 | `amount_local` | `total_amount_cny` | `bill_head`（migration 000019）|
| 多币种货币代码 | `currency` | `currency_code` | `bill_head`（migration 000019）|
| 散装称重策略值 | `weight` | `by_weight` | `product.measurement_strategy` |
| 标准件策略值 | `individual` | `unit_count` | `product.measurement_strategy` |
| 批次策略值 | `batch` | `lot_based` | `product.measurement_strategy` |
| 散装合并策略值 | `bulk_merged` | `bulk` | `tenant_profile.inventory_method` |

---

## §4 Migration 编号分配（000013–000021）

| Migration | 内容 | 状态 |
|-----------|------|------|
| 000013 | `tenant_profile` 表 + RLS | V1 |
| 000014 | `unit_def` + `product_unit` + RLS | V1 |
| 000015 | `product` 加 `measurement_strategy`/`default_unit_id`/`attributes` 3 列 + GIN 索引 | V1 |
| 000016 | `bill_head/bill_item/payment_head/stock_initial` 各加 4 列（`origin/sync_status/edge_node_id/edge_timestamp`）| V1 预留 |
| 000017 | `edge_node` 表 + RLS + `bill_head` FK | V2 |
| 000018 | `sync_conflict` 表 + RLS | V2 |
| 000019 | `currency` + `exchange_rate` 表 + `partner.default_currency` + `bill_head` 3 列 | V1（cross_border）|
| 000020 | GIN 索引（product.attributes / tenant_profile.custom_overrides / edge_node.settings）| V1 |
| 000021 | 补全 RLS policy | V1 |

**注**：`000013` 已被 `tenant_profile` 占用。Story 9.1 Tech Notes 中 `000013_cross_border_fields` 已更正为 `000019_add_currency`。

---

## §5 待确认项（Blockers for devs）

| # | 问题 | 影响 | 负责人 | 截止 |
|---|------|------|-------|------|
| OC-1 | 会员模块（`member` + `member_points_ledger` 表）未在 architecture.md 定义 DDL | Epic 10.5 无法开工 | PM + Architect | Epic 10 开始前 |
| OC-2 | `product.embedding` (`vector(1536)`) 字段：pgvector 在 lurus-pg-rw 是否已安装 | Epic 20.4 依赖 | 架构师 | V3 规划时 |

---

## 变更记录

| 日期 | 变更 | 触发原因 |
|------|------|---------|
| 2026-04-23 | 初版 decision-lock.md 创建；统一三份文档命名分歧（tenant_profile.profile_type / edge_timestamp / measurement_strategy 枚举 / inventory_method 枚举 / 多币种字段名 / migration 编号）| 三份新文档并行重写后协调 |
