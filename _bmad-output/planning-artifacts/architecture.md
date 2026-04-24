# Lurus Tally — Architecture Document v2

> 版本: 2.0 | 日期: 2026-04-23 | 状态: **APPROVED**
> 本版本重写原因：双 Persona + 行业 Profile + 边缘离线部署。
> v1 备份：`_archive/architecture-v1-single-cloud-2026-04-23.md`
> 所有端口/命名空间/DB schema/Redis DB/NATS stream 严格匹配 lurus.yaml。

---

## 1. System Context

### 1.1 总览：云端 SaaS + 边缘节点 + 双 Persona 共享 Kernel

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                           外部用户（两类 Persona）                               │
│   Persona A: 跨境贸易商 (cross_border)        Persona B: 实体零售/批发 (retail) │
│   批发商 / 电商卖家 / 外贸公司                  五金店 / 食品经销商 / 便利店      │
└─────────┬──────────────────────────────────────────────┬───────────────────────┘
          │ HTTPS tally.lurus.cn                         │ 本地网络 / 离线
          │                                              │
┌─────────▼──────────────────────────────┐   ┌──────────▼────────────────────────┐
│          云端 SaaS                      │   │         边缘节点 (Edge)            │
│  namespace: lurus-tally (K3s)          │   │  单 Go binary (build tag: edge)   │
│  tally-backend :18200 (Go)             │   │  SQLite (WAL 模式)                 │
│  tally-web :3000 (Next.js 14)          │   │  PWA 前端 (离线 service worker)   │
│  tally-worker (goroutine group)        │   │  Tauri 可选壳                      │
│                                        │   │  支持 retail profile 仓库场景      │
│  Profile Kernel (共享)                 │◄──►│  Profile Kernel (共享，子集 schema)│
│  cross_border | retail | hybrid        │   │  NATS JetStream 上行同步           │
└─────────┬──────────────────────────────┘   └───────────────────────────────────┘
          │
          ▼
┌────────────────────────────────────────────────────────────────────────────────┐
│                         Lurus 共享基础设施                                      │
│  2l-svc-platform :18104  2b-svc-api/Hub  2b-svc-kova  2b-svc-memorus :8880   │
│  PostgreSQL (schema:tally)  Redis DB5  NATS PSI_EVENTS  MinIO  Zitadel        │
└────────────────────────────────────────────────────────────────────────────────┘
```

### 1.2 外部依赖

| 系统 | 调用方向 | 协议 | 用途 |
|------|----------|------|------|
| Zitadel (auth.lurus.cn) | Tally ← | OIDC/PKCE | 用户认证、JWT、角色声明 |
| 2l-svc-platform (:18104) | Tally → | HTTP REST (bearer key) | 租户账户验证、订阅、配额、计费 |
| 2b-svc-api/Hub (:8850) | Tally → | HTTP (OpenAI 兼容) | LLM 查询、函数调用、流式 |
| 2b-svc-kova | Tally → | HTTP REST | 补货/滞销 Agent 注册与触发 |
| 2b-svc-memorus (:8880) | Tally → | HTTP REST | RAG 历史写入与检索 |
| 2l-svc-platform/notification (:18900) | Tally → | HTTP POST (bearer) | 库存预警、单据通知推送 |
| PostgreSQL lurus-pg-rw | Tally → | TCP/5432 | 业务数据持久化 (schema: tally) |
| Redis DB 5 | Tally → | TCP/6379 | 会话、限流、乐观锁版本 |
| NATS PSI_EVENTS | Tally ↔ | TCP/4222 | 库存变更事件发布与消费 |
| NATS PSI_EVENTS (edge subjects) | Edge ↔ Cloud | TCP/4222 | 边缘同步上行队列 |
| MinIO (user-uploads) | Tally → | HTTP S3 | 附件、商品图片 |
| ExchangeRate-API / 央行汇率 | Tally ← | HTTPS | 多币种汇率（跨境 profile） |

### 1.3 关键数据流

1. **认证流**: 浏览器 → Zitadel OIDC → Next.js BFF `/api/auth/callback` → session cookie → 后续请求携带 JWT → tally-backend
2. **Profile 注入流**: JWT 解析 tenant_id → 读取 `tenant_profile` 表 → ProfileResolver 注入 ctx → 所有下游 use case/handler 按 profile 行为
3. **业务操作流**: 前端 → Next.js BFF `/api/v1/*` → Go Backend → PostgreSQL/Redis → 返回响应
4. **库存事件流**: Go Backend 审核单据 → 更新 `stock_snapshot` → 发布 `psi.stock.changed` 到 NATS → tally-worker 消费 → 预警/AI分析
5. **边缘同步上行流**: 边缘 binary → 写本地 SQLite → 发布 `tally.edge.sync.bill` 到 NATS JetStream → cloud tally-worker 消费 → 写云端 PostgreSQL → 冲突写 `sync_conflict` 表
6. **边缘同步下行流**: tally-backend 配置变更 → 写 `edge_sync_queue` → 边缘 binary 定时 HTTP polling 拉取（ETag 比对）

---

## 2. Profile 机制

### 2.1 设计原则

核心 Kernel（所有业务逻辑）共享一套代码路径。Profile 注入差异行为，不分叉代码路径。Profile 影响的范围：

- UI 渲染：哪些字段/模块可见
- 字段默认值：成本算法、税率字段、单位
- 业务规则：库存计算策略（FIFO / WAC）、必填字段
- 功能开关：多币种、HS Code、称重秤、收银 POS

### 2.2 数据库侧

```sql
-- migration 000013
CREATE TABLE tally.tenant_profile (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID NOT NULL REFERENCES tally.tenant(id) ON DELETE CASCADE,
    profile_type     VARCHAR(20) NOT NULL CHECK (profile_type IN ('cross_border','retail','hybrid')),
    inventory_method VARCHAR(20) NOT NULL DEFAULT 'wac'
                     CHECK (inventory_method IN ('fifo','wac','by_weight','batch','bulk_merged')),
    custom_overrides JSONB NOT NULL DEFAULT '{}',
    -- custom_overrides 示例:
    -- {"default_tax_rate": 0.13, "currency": "USD", "enable_pos": true,
    --  "enable_scale": false, "enable_hs_code": true}
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id)
);
CREATE INDEX idx_tenant_profile_tenant ON tally.tenant_profile(tenant_id);
```

### 2.3 Go 侧 ProfileResolver

```go
// internal/app/profile/resolver.go

type Profile interface {
    Type() ProfileType
    InventoryMethod() InventoryMethod
    IsEnabled(feature string) bool          // "multi_currency","hs_code","pos","scale"
    DefaultTaxRate() decimal.Decimal
    DefaultCurrency() string                // "CNY" / "USD" / ...
    RequiredBillFields() []string           // cross_border: ["hs_code","origin_country"]
    UIFeatures() UIFeatureSet               // 前端按此渲染
}

type CrossBorderProfile struct { overrides JSONB }
type RetailProfile      struct { overrides JSONB }
type HybridProfile      struct { overrides JSONB }

// ProfileResolver 从 ctx 中读取 tenant_id，查询 tenant_profile（有缓存）
type ProfileResolver struct {
    repo   ProfileRepository
    cache  *sync.Map  // tenant_id -> Profile（TTL 5min）
}

func (r *ProfileResolver) Resolve(ctx context.Context) (Profile, error)
```

### 2.4 中间件注入

```go
// internal/adapter/middleware/profile.go
func ProfileMiddleware(resolver *ProfileResolver) gin.HandlerFunc {
    return func(c *gin.Context) {
        p, err := resolver.Resolve(c.Request.Context())
        if err != nil {
            c.AbortWithStatusJSON(500, ...)
            return
        }
        c.Set("profile", p)
        c.Next()
    }
}
// 所有 authenticated 路由挂载顺序：auth → tenant_rls → profile → handler
```

### 2.5 前端 Profile 感知

Next.js BFF 在用户登录后从后端 `/api/v1/me/profile` 拉取 `UIFeatureSet`，存入 Zustand `profile-store.ts`。组件通过 `useProfile()` hook 判断是否渲染特定功能。不使用 URL 分叉（同一端点返回 profile 感知字段），避免 endpoint 爆炸。

---

## 3. 核心数据模型 (32 张表 + 1 MV + 2 View)

**数据库连接**: `lurus-pg-rw.database.svc:5432`，schema: `tally`。
**金额精度不变约束**: 所有货币金额字段使用 `NUMERIC(18,4)`，数量字段 `NUMERIC(18,4)`，汇率 `NUMERIC(20,8)`，禁止 float。
**License 声明**: jshERP/GreaterWMS 衍生表保留注释 + `THIRD_PARTY_LICENSES/`。

### 3.1 域 1: tenant + profile

```sql
-- 保持 v1 tenant 表不变（migration 000002）

-- migration 000013: profile 表
CREATE TABLE tally.tenant_profile (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID NOT NULL REFERENCES tally.tenant(id) ON DELETE CASCADE,
    profile_type     VARCHAR(20) NOT NULL
                     CHECK (profile_type IN ('cross_border','retail','hybrid')),
    inventory_method VARCHAR(20) NOT NULL DEFAULT 'wac'
                     CHECK (inventory_method IN ('fifo','wac','by_weight','batch','bulk_merged')),
    custom_overrides JSONB NOT NULL DEFAULT '{}',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id)
);
CREATE INDEX idx_tenant_profile_tenant ON tally.tenant_profile(tenant_id);
ALTER TABLE tally.tenant_profile ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_profile_rls ON tally.tenant_profile
    USING (tenant_id = current_setting('app.tenant_id')::UUID);
```

### 3.2 域 2: org（不变，migration 000003）

### 3.3 域 3: partner（不变，migration 000004）

跨境 profile 追加字段在 partner 的 `attributes JSONB`（tax_no 已有；可扩展 swift_code、bank_country）。

### 3.4 域 4: 商品模型升级

```sql
-- migration 000014: unit + product_unit（替换旧 unit 表的 JSONB sub_units 方案）
-- 旧 tally.unit 表保留（其 sub_units JSONB 废弃，不迁移，由 product_unit 接管）

CREATE TABLE tally.unit_def (
    -- 明确命名 unit_def 避免与旧 unit 表冲突
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL,
    code        VARCHAR(50) NOT NULL,   -- "kg", "jin", "box", "pcs"
    name        VARCHAR(100) NOT NULL,  -- 显示名称
    unit_type   VARCHAR(20) NOT NULL    -- 'weight'|'length'|'volume'|'count'|'custom'
                CHECK (unit_type IN ('weight','length','volume','count','custom')),
    enabled     BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_unit_def_code ON tally.unit_def(tenant_id, code);
ALTER TABLE tally.unit_def ENABLE ROW LEVEL SECURITY;
CREATE POLICY unit_def_rls ON tally.unit_def
    USING (tenant_id = current_setting('app.tenant_id')::UUID);

CREATE TABLE tally.product_unit (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID NOT NULL,
    product_id        UUID NOT NULL REFERENCES tally.product(id) ON DELETE CASCADE,
    unit_id           UUID NOT NULL REFERENCES tally.unit_def(id),
    is_base           BOOLEAN NOT NULL DEFAULT false,  -- 有且只有一行 is_base=true
    conversion_factor NUMERIC(20,8) NOT NULL DEFAULT 1,
    -- conversion_factor: 该单位 = factor × base_unit
    -- base_unit 行 factor=1，包(1包=5斤) factor=5，箱(1箱=100斤) factor=100
    barcode           VARCHAR(100),
    purchase_price    NUMERIC(18,4),   -- 该单位的采购价（可选）
    sale_price        NUMERIC(18,4),   -- 该单位的售价（可选）
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_product_unit_base
    ON tally.product_unit(product_id) WHERE is_base = true;
CREATE INDEX idx_product_unit_product ON tally.product_unit(product_id);

-- migration 000015: 升级 product 表
ALTER TABLE tally.product
    ADD COLUMN measurement_strategy VARCHAR(20) NOT NULL DEFAULT 'individual'
        CHECK (measurement_strategy IN
               ('individual','weight','length','volume','batch','serial')),
    ADD COLUMN default_unit_id UUID REFERENCES tally.unit_def(id),
    ADD COLUMN attributes JSONB NOT NULL DEFAULT '{}';
    -- attributes 示例（cross_border）:
    -- {"hs_code": "6104.43", "origin_country": "CN",
    --  "name_en": "Women Knit Dress", "name_zh": "女款针织连衣裙",
    --  "customs_value_usd": "12.50", "brand_en": "NoLabel"}
    -- attributes 示例（retail/称重）:
    -- {"tare_weight_kg": 0.05, "price_per_kg": 28.00}

CREATE INDEX idx_product_attributes_gin ON tally.product USING GIN (attributes);
-- GIN 索引覆盖任意 JSONB key 查询，例如 attributes @> '{"hs_code":"6104.43"}'
```

**库存表统一用 base_unit 存储数量**。显示和录入时，前端按用户选择的单位换算（除以 conversion_factor）。换算在 Go 应用层完成，不在数据库触发器。

### 3.5 域 5: warehouse + stock（增加离线字段）

```sql
-- warehouse 表不变（migration 000006）

-- migration 000016: 所有单据相关表加离线字段
-- 影响: bill_head, bill_item, payment_head, stock_initial, stock_snapshot
-- 以 bill_head 为代表展示模式，其余同理

ALTER TABLE tally.bill_head
    ADD COLUMN origin       VARCHAR(10) NOT NULL DEFAULT 'cloud'
        CHECK (origin IN ('cloud','edge')),
    ADD COLUMN sync_status  VARCHAR(20) NOT NULL DEFAULT 'synced'
        CHECK (sync_status IN ('synced','pending','conflict')),
    ADD COLUMN edge_node_id UUID NULL,    -- FK to edge_node.id（延迟约束，edge_node 在 000017）
    ADD COLUMN edge_timestamp TIMESTAMPTZ NULL;
    -- edge_timestamp: 边缘节点本地时间（UTC），用于冲突解决

CREATE INDEX idx_bill_head_sync ON tally.bill_head(tenant_id, sync_status)
    WHERE sync_status != 'synced';

-- 同样为以下表加相同 4 列（migration 000016 统一）:
-- bill_item: ADD COLUMN origin, sync_status, edge_node_id, edge_timestamp
-- payment_head: ADD COLUMN origin, sync_status, edge_node_id, edge_timestamp
-- stock_initial: ADD COLUMN origin, sync_status (edge_node_id, edge_timestamp 可选)
-- 注: stock_snapshot 不加 origin/sync（快照由服务端聚合计算，不来自边缘直写）

-- migration 000017: edge_node 表
CREATE TABLE tally.edge_node (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL,
    name            VARCHAR(100) NOT NULL,   -- "仓库A门店终端"
    location        VARCHAR(200),            -- 物理地址描述
    api_key_hash    VARCHAR(128) NOT NULL,   -- SHA-256(api_key)，用于边缘认证
    last_seen_at    TIMESTAMPTZ,             -- 最近心跳时间
    last_synced_at  TIMESTAMPTZ,             -- 最近成功同步时间
    schema_version  INT NOT NULL DEFAULT 1, -- 边缘 schema 版本
    status          VARCHAR(20) NOT NULL DEFAULT 'active'
                    CHECK (status IN ('active','inactive','suspended')),
    settings        JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_edge_node_tenant ON tally.edge_node(tenant_id);
ALTER TABLE tally.edge_node ENABLE ROW LEVEL SECURITY;
CREATE POLICY edge_node_rls ON tally.edge_node
    USING (tenant_id = current_setting('app.tenant_id')::UUID);

-- 补全 bill_head.edge_node_id 外键（延迟到 000017 表存在后）
ALTER TABLE tally.bill_head
    ADD CONSTRAINT fk_bill_head_edge_node
    FOREIGN KEY (edge_node_id) REFERENCES tally.edge_node(id) DEFERRABLE;

-- migration 000018: sync_conflict 表
CREATE TABLE tally.sync_conflict (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL,
    edge_node_id    UUID NOT NULL REFERENCES tally.edge_node(id),
    resource_type   VARCHAR(50) NOT NULL,   -- 'bill_head'|'payment_head'|...
    resource_id     UUID NOT NULL,          -- 边缘端的记录 ID
    edge_payload    JSONB NOT NULL,         -- 边缘端完整数据快照
    cloud_payload   JSONB NOT NULL,         -- 冲突时云端已有数据快照
    conflict_reason VARCHAR(200),           -- 冲突说明（e.g. "cloud version updated_at newer"）
    resolved        BOOLEAN NOT NULL DEFAULT false,
    resolved_by     UUID,                   -- 解决人 user_id
    resolved_at     TIMESTAMPTZ,
    resolution      VARCHAR(20)             -- 'use_edge'|'use_cloud'|'manual'
                    CHECK (resolution IN ('use_edge','use_cloud','manual')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_sync_conflict_tenant ON tally.sync_conflict(tenant_id, resolved);
CREATE INDEX idx_sync_conflict_resource ON tally.sync_conflict(resource_type, resource_id);
ALTER TABLE tally.sync_conflict ENABLE ROW LEVEL SECURITY;
CREATE POLICY sync_conflict_rls ON tally.sync_conflict
    USING (tenant_id = current_setting('app.tenant_id')::UUID);
```

### 3.6 域 6: bill_head + bill_item（v1 保留，加离线字段见上）

`bill_head.source` 字段已有（v1 默认 `'web'`），边缘来源写 `'edge'`，`origin` 字段补充来源类型，二者语义不同。

### 3.7 域 7: finance（增加多币种支持）

```sql
-- migration 000019: currency + exchange_rate（跨境 profile 专用）
CREATE TABLE tally.currency (
    code        VARCHAR(10) PRIMARY KEY,    -- 'CNY'|'USD'|'EUR'|'GBP'|'JPY'|...
    name        VARCHAR(100) NOT NULL,
    symbol      VARCHAR(10),               -- '¥'|'$'|'€'
    enabled     BOOLEAN NOT NULL DEFAULT true
);

INSERT INTO tally.currency (code, name, symbol) VALUES
    ('CNY','人民币','¥'),
    ('USD','美元','$'),
    ('EUR','欧元','€'),
    ('GBP','英镑','£'),
    ('JPY','日元','¥'),
    ('HKD','港币','HK$');

CREATE TABLE tally.exchange_rate (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    from_currency VARCHAR(10) NOT NULL REFERENCES tally.currency(code),
    to_currency   VARCHAR(10) NOT NULL REFERENCES tally.currency(code),
    rate          NUMERIC(20,8) NOT NULL,   -- 1 from_currency = rate to_currency
    source        VARCHAR(50) NOT NULL DEFAULT 'manual',
    -- source: 'manual'|'exchangerate_api'|'pboc'（央行）
    effective_at  TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_exchange_rate_pair ON tally.exchange_rate(from_currency, to_currency, effective_at DESC);

-- 多币种扩展 partner（追加字段，migration 000019 一并）
ALTER TABLE tally.partner
    ADD COLUMN IF NOT EXISTS default_currency VARCHAR(10) DEFAULT 'CNY'
        REFERENCES tally.currency(code);

-- 多币种扩展 bill_head（追加字段，migration 000019 一并）
ALTER TABLE tally.bill_head
    ADD COLUMN IF NOT EXISTS currency       VARCHAR(10) DEFAULT 'CNY'
        REFERENCES tally.currency(code),
    ADD COLUMN IF NOT EXISTS exchange_rate  NUMERIC(20,8) DEFAULT 1,
    -- exchange_rate: 单据货币 → CNY 的汇率（CNY 单据 rate=1）
    ADD COLUMN IF NOT EXISTS amount_local   NUMERIC(18,4);
    -- amount_local: 以单据货币计的金额（total_amount 始终为 CNY）
```

### 3.8 JSONB GIN 索引

```sql
-- migration 000020: GIN 索引统一补建
CREATE INDEX idx_product_attributes_gin ON tally.product USING GIN (attributes);
CREATE INDEX idx_tenant_profile_overrides_gin ON tally.tenant_profile USING GIN (custom_overrides);
CREATE INDEX idx_edge_node_settings_gin ON tally.edge_node USING GIN (settings);
```

### 3.9 新表 RLS（migration 000021）

```sql
-- 补全所有 migration 000013-019 新增表的 RLS（集中在 000021 管理）
-- tenant_profile: 已在 000013 中含 RLS（见 §3.1）
-- unit_def, product_unit: 加 RLS
ALTER TABLE tally.unit_def ENABLE ROW LEVEL SECURITY;
-- （CREATE POLICY 已在 000014）

ALTER TABLE tally.product_unit ENABLE ROW LEVEL SECURITY;
CREATE POLICY product_unit_rls ON tally.product_unit
    USING (tenant_id = current_setting('app.tenant_id')::UUID);

-- edge_node, sync_conflict: 已在 000017/000018 中含 RLS
```

---

## 4. 库存计算 Strategy Pattern

### 4.1 设计

所有库存变动（采购入库、销售出库、调拨、盘点差异）统一通过 `InventoryCalculator` 接口处理，Profile 在初始化时决定使用哪个实现。

```go
// internal/app/stock/calculator.go

// StockMovement 表示一次库存变动请求（所有数量均以 base_unit 计）
type StockMovement struct {
    TenantID    uuid.UUID
    ProductID   uuid.UUID
    WarehouseID uuid.UUID
    Qty         decimal.Decimal  // 正=入库，负=出库
    UnitID      uuid.UUID        // 本次操作用的单位（换算到 base 后才进 calculator）
    LotID       *uuid.UUID       // 批次 ID（batch/fifo 需要）
    BillNo      string
    Reason      string           // 'purchase'|'sale'|'transfer'|'stocktake'|'adjust'
}

// StockSnapshot 计算后的库存快照增量
type StockSnapshot struct {
    OnHandDelta   decimal.Decimal
    AvgCostPrice  decimal.Decimal  // WAC 算法输出；FIFO 输出 nil
    LotUpdates    []LotUpdate      // FIFO/batch 需要更新的批次
}

type InventoryCalculator interface {
    // ApplyMovement 在事务内原子更新 stock_snapshot，返回更新后快照
    ApplyMovement(ctx context.Context, m StockMovement) (StockSnapshot, error)
    // ValidateMovement 出库前检查库存充足（不修改库存）
    ValidateMovement(ctx context.Context, m StockMovement) error
}
```

### 4.2 各策略实现

```go
// internal/app/stock/calc_wac.go
// WeightedAvgCalculator: 移动加权平均 (WAC)
// avg_cost = (current_value + new_qty * new_price) / (on_hand + new_qty)
// Profile: retail (default), hybrid

// internal/app/stock/calc_fifo.go
// FIFOCalculator: 先进先出，依赖 stock_lot 批次队列
// 出库时按 lot.created_at ASC 消费
// Profile: cross_border (default, 因跨境有明确批次追溯需求)

// internal/app/stock/calc_weight.go
// ByWeightCalculator: 散装称重商品
// 数量精度到 0.001 kg；零头合并（bulk_merge）逻辑
// measurement_strategy = 'weight' 时激活

// internal/app/stock/calc_batch.go
// BatchCalculator: 批次独立追踪（FEFO: first-expired-first-out）
// 出库时按 lot.expiry_date ASC 消费（食品/医药）
// measurement_strategy = 'batch' 时激活

// internal/app/stock/calc_bulk.go
// BulkMergedCalculator: 散装商品跨批次合并
// 用于五金散件（螺丝/线缆）：多批次入库合并为一个 pool
```

### 4.3 Profile 与默认 Calculator 映射

| Profile | 默认 Calculator | 可切换为 |
|---------|----------------|---------|
| `cross_border` | `FIFOCalculator` | `WeightedAvgCalculator`（custom_overrides） |
| `retail` | `WeightedAvgCalculator` | `BatchCalculator`（食品/医药子场景） |
| `hybrid` | `WeightedAvgCalculator` | 任意 |

Calculator 选择逻辑在 `internal/app/stock/calculator_factory.go`，根据 `Profile.InventoryMethod()` 和商品 `measurement_strategy` 两个维度决定。

### 4.4 浮点精度规则

**全程使用 `github.com/shopspring/decimal`**，禁止 `float64`。PostgreSQL 侧 `NUMERIC(18,4)` 与 Go `decimal.Decimal` 通过 GORM 自定义类型映射。汇率字段使用 `NUMERIC(20,8)` / `decimal.Decimal`（8 位小数）。

---

## 5. 多单位换算引擎

### 5.1 数据模型

用例：商品"牛腱子"，base_unit=斤(jin)，sale_units=[包(1包=5斤), 箱(1箱=20包=100斤)]

```
product_unit 表:
  (product_id=X, unit_id=jin, is_base=true,  conversion_factor=1)
  (product_id=X, unit_id=bao, is_base=false, conversion_factor=5)
  (product_id=X, unit_id=xiang, is_base=false, conversion_factor=100)
```

### 5.2 换算规则

所有库存数量在数据库以 base_unit 存储。

```go
// internal/pkg/unitconv/converter.go

// ToBase: 将用户输入数量（按 unitID）换算为 base_unit 数量
func ToBase(qty decimal.Decimal, unitID uuid.UUID, units []ProductUnit) (decimal.Decimal, error)

// FromBase: 将 base_unit 数量换算为显示单位
func FromBase(baseQty decimal.Decimal, unitID uuid.UUID, units []ProductUnit) (decimal.Decimal, error)

// 换算精度：decimal.NewFromString，round half-up 到 4 位小数
// 换算失败（单位未注册）→ 返回 error，不 panic
```

### 5.3 录入与显示流程

1. 前端：用户选择操作单位（包/箱/斤），输入数量
2. BFF 代理传递 `{unit_id, qty}` 到 Go backend
3. Go handler 调用 `unitconv.ToBase()` 换算为 base_unit 数量
4. 所有业务逻辑和库存计算使用 base_unit 数量
5. 响应返回时：后端带回 `{on_hand_qty, unit: "jin"}`，前端按用户偏好显示单位再换算

---

## 6. 离线优先架构（Edge）

### 6.1 边缘 Binary

边缘端用同一个 Go 代码库编译，通过 build tag 切换行为：

```bash
# 云端编译
CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -trimpath -o tally-cloud ./cmd/server

# 边缘编译（启用 edge build tag）
CGO_ENABLED=0 GOOS=linux go build -tags edge -ldflags="-s -w" -trimpath -o tally-edge ./cmd/server
# 或 Windows/macOS 桌面
GOOS=windows go build -tags edge -o tally-edge.exe ./cmd/server
```

`edge` build tag 的差异：

| 能力 | 云端 binary | 边缘 binary |
|------|------------|-------------|
| 数据库驱动 | pgx (PostgreSQL) | `modernc.org/sqlite`（纯 Go，无 CGO） |
| 多租户 RLS | 完整 RLS | 无（单 tenant，固定 tenant_id） |
| NATS 发布 | 立即发布 | 本地 SQLite queue，断网时累积 |
| NATS 订阅 | tally-worker | edge-sync-worker（仅接收下行配置变更） |
| AI 功能 | 完整（Hub/Kova/Memorus） | 降级：只提供本地报表，无 AI |
| 称重秤 | 不支持 | 支持（`go.bug.st/serial` 串口读取） |
| ESC/POS 打印 | 不支持 | 支持（USB/网络小票打印） |

### 6.2 边缘 Schema

边缘 SQLite schema 是云端 PostgreSQL schema 的子集：

- 移除：所有 RLS policy、pgvector 扩展、exchange_rate（边缘不做汇率拉取）、org_department（简化组织）
- 保留：product、product_unit、unit_def、partner、warehouse、stock_snapshot、stock_lot、bill_head、bill_item、payment_head、audit_log、system_config、tenant_profile（只有 1 行）、edge_node（只有 1 行，自己的注册信息）
- 离线字段（origin/sync_status/edge_node_id/edge_timestamp）完整保留
- 边缘 schema 通过单独的 migration 集管理（`migrations/edge/` 目录，不与云端混用）

### 6.3 同步协议

#### 上行（边缘→云）

```
边缘操作（审核单据/收付款）
  → 写本地 SQLite（sync_status='pending'）
  → 发布到本地 SQLite 临时队列（edge_sync_queue 表）

edge-sync-worker（每 30s polling）:
  → 读取 pending 记录
  → 若网络可达：POST https://tally.lurus.cn/internal/v1/edge/sync
    body: { edge_node_id, records: [{resource_type, resource_id, payload, edge_timestamp}] }
    auth: edge API Key（Bearer）
  → 云端 tally-backend edge-sync handler:
    1. 检查 cloud 侧是否存在该 resource_id（edge 生成的 UUID）
    2. 若不存在 → 直接写入（origin='edge', sync_status='synced'）
    3. 若已存在且 cloud.updated_at > edge_timestamp → 写入 sync_conflict 表
    4. 若已存在且 cloud.updated_at <= edge_timestamp → 覆盖（last-write-wins）
    5. 金额/库存相关（bill_head.total_amount / stock_snapshot）→ 强制走 sync_conflict（不自动覆盖）
  → 云端返回 {synced: [...], conflicts: [...]}
  → 边缘更新本地记录 sync_status='synced'|'conflict'

断网时：记录停留在 pending；网络恢复后 edge-sync-worker 重试（指数退避，最长 5min）
```

#### 下行（云→边缘）

```
触发条件：云端管理员修改 tenant_profile、system_config、product（主档变更）

云端：写 edge_sync_downstream 表（edge_node_id, resource_type, payload, version）
边缘 edge-sync-worker（每 60s）：
  GET https://tally.lurus.cn/internal/v1/edge/downstream?since=<last_synced_at>&edge_node_id=X
  Response-Header: ETag（版本号，边缘存储，下次带 If-None-Match）
  → 若 304 Not Modified → 跳过
  → 若 200 → 解析 payload 写入本地 SQLite（强制覆盖，下行无冲突）
```

#### PWA 最后防线

边缘 backend 不可达时（边缘机器故障），PWA service worker + IndexedDB 支持：
- 查看（离线读）：本地 IndexedDB 缓存最近 7 天数据
- 写操作：暂存到 IndexedDB sync queue，edge backend 恢复后上传
- 不支持：库存计算、单据审核（需要 edge backend 参与）

### 6.4 冲突解决规则

| 场景 | 处理方式 |
|------|---------|
| 商品主档/配置变更 | last-write-wins by edge_timestamp |
| 单据（bill_head/item）金额字段 | 强制 sync_conflict → 人工裁决 |
| 库存数量（stock_snapshot） | 强制 sync_conflict → 人工裁决 |
| 收付款记录 | 强制 sync_conflict → 人工裁决 |
| 审计日志 | 追加，不冲突 |

人工裁决界面：云端 `/app/(dashboard)/sync-conflicts/page.tsx`，显示 edge/cloud 数据对比，支持"采用边缘版本"/"采用云端版本"/"手动编辑"三选一。

---

## 7. 跨境专属服务集成（cross_border profile）

### 7.1 多币种引擎

汇率定时任务（`tally-worker/exchange_rate_job.go`）：

```go
// 每日 09:00 UTC 拉取汇率
// 优先：央行（PBoC）API；降级：ExchangeRate-API（https://api.exchangerate-api.com）
// 失败：保留最近一次有效汇率，告警（Grafana alert）
// 写入 exchange_rate 表，source='pboc'|'exchangerate_api'
```

所有金额字段存 CNY（`total_amount`），`amount_local` 存原币金额，`exchange_rate` 存汇率记录历史。

### 7.2 HS Code 管理

HS Code 存储在 `product.attributes JSONB`（`{"hs_code": "6104.43", "origin_country": "CN"}`）。

前端：商品表单在 cross_border profile 下显示 HS Code 搜索框（combobox），内置 6 位 HS Code 树（约 5000 条，编译时嵌入 `//go:embed`）。查询走 `attributes @> '{"hs_code":...}'`，命中 GIN 索引。

### 7.3 报关单生成

```go
// internal/adapter/handler/v1/customs.go (cross_border profile only)
// POST /api/v1/customs-declaration/generate
// 输入: bill_id（出库销售单）
// 输出: PDF/XLSX（Go html/template → wkhtmltopdf 或 excelize）
// 字段: HS Code、品名（中英）、原产地、数量、单价(USD)、总价、重量
```

### 7.4 外汇 API 对账

```go
// internal/app/finance/fx_reconcile.go
// 月末对账：将所有外币单据按当日汇率折算 CNY，与 total_amount 对比差异
// 差异写入 finance_category 的"汇兑损益"科目下的 payment_item
```

---

## 8. 零售专属服务集成（retail profile）

### 8.1 POS 收银

```go
// internal/adapter/pos/pos_handler.go (retail profile only, build tag: pos)
// 快速收银流程：商品条码扫描 → 库存扣减 → 收款（现金/微信/支付宝）→ 出小票
// 调 sales use case，sale type='pos_sale'，状态直接跳到"已完成"（跳过审核流程）
// POS 模式下 bill_head.sub_type = 'pos_sale'
```

### 8.2 ESC/POS 小票打印

```go
// internal/adapter/printer/escpos.go
// 使用 github.com/mike42/escpos （或自实现 ESC/POS 命令集）
// 支持：USB（/dev/usb/lp0）/ 网络打印（TCP:9100）/ 串口
// 边缘 binary 编译时包含；云端 binary 不包含（build tag: pos）
```

### 8.3 称重秤集成（边缘 binary 专属）

```go
// internal/adapter/scale/serial_scale.go (edge + build tag: scale)
// 依赖: go.bug.st/serial
// 协议: 大多数电子秤输出 ASCII 格式如 "ST,+002.350kg\r\n"
// 支持波特率: 1200/2400/4800/9600 bps（配置化）
// 称重结果推到前端 WebSocket /ws，前端 UI 自动填入数量字段
```

### 8.4 移动支付面对面收款

```go
// internal/adapter/payment/wechat_qr.go, alipay_qr.go
// SaaS 端代理调用（云端 binary），边缘端通过云端转发
// 微信：统一下单 API (native pay)，生成 QR 码
// 支付宝：预授权码收款或面对面 QR
// 回调写 payment_head，sync_status='synced'（云端发起，无离线问题）
```

---

## 9. 多租户 RLS 设计

### 9.1 不变约束

Profile 不影响 RLS 隔离粒度（仍按 `tenant_id` 隔离）。所有新增表均启用 RLS（见 migration 000021）。边缘端无 RLS（单 tenant，固定）。边缘上传时，云端 edge-sync handler 使用 `SET LOCAL app.tenant_id` 写入，与正常请求路径一致。

### 9.2 RLS 模板（标准）

```sql
ALTER TABLE tally.<table> ENABLE ROW LEVEL SECURITY;
CREATE POLICY <table>_rls ON tally.<table>
    USING (tenant_id = current_setting('app.tenant_id')::UUID);
```

### 9.3 RLS 危险点

`current_setting('app.tenant_id')` 未设置时，PostgreSQL 抛出异常，GORM 返回错误（不会静默返回全表）。确保每次请求都经过 `TenantRLS` middleware。edge-sync handler 是唯一需要手动管理 `SET LOCAL` 的非标准路径。

---

## 10. API 契约

### 10.1 Profile 感知策略

同一端点按 Profile 返回字段差异（不分叉 URL）。`/api/v1/products/:id` 在 cross_border profile 下额外返回 `attributes.hs_code`、`attributes.origin_country`；在 retail profile 下额外返回 `attributes.tare_weight_kg`。前端通过 `UIFeatureSet` 决定渲染哪些字段，不依赖字段是否存在（避免 undefined 错误）。

### 10.2 核心 REST 端点（在 v1 基础上的增量）

| 资源 | 端点 | 方法 | 新增说明 |
|------|------|------|---------|
| Profile | `/api/v1/me/profile` | GET | 返回当前 tenant profile + UIFeatureSet |
| | `/api/v1/admin/profile` | PUT | 管理员设置 profile_type + custom_overrides |
| 单位定义 | `/api/v1/unit-defs` | GET/POST | 租户级单位定义 CRUD |
| | `/api/v1/products/:id/units` | GET/POST/DELETE | 商品多单位换算配置 |
| 汇率 | `/api/v1/exchange-rates` | GET | 当日汇率（cross_border only） |
| 边缘节点 | `/api/v1/edge-nodes` | GET/POST | 注册/列出边缘节点（管理员） |
| | `/api/v1/edge-nodes/:id` | GET/PATCH/DELETE | 边缘节点管理 |
| | `/api/v1/edge-nodes/:id/revoke` | POST | 吊销 edge API Key |
| 同步冲突 | `/api/v1/sync-conflicts` | GET | 列出未解决冲突 |
| | `/api/v1/sync-conflicts/:id/resolve` | POST | 裁决冲突 |
| 报关单 | `/api/v1/customs-declaration/generate` | POST | 生成报关文件（cross_border only） |
| 称重 | `/ws/scale` | WS | 称重秤实时读数（edge only，通过 edge WS） |

**边缘专用 Internal 端点**:

| 端点 | 方法 | 用途 |
|------|------|------|
| `/internal/v1/edge/sync` | POST | 边缘上传同步记录 |
| `/internal/v1/edge/downstream` | GET | 边缘拉取下行配置变更 |
| `/internal/v1/edge/heartbeat` | POST | 边缘心跳（更新 last_seen_at） |

认证：`Authorization: Bearer <edge_api_key>`（明文 API Key，与 INTERNAL_API_KEY 不同，存在 edge_node.api_key_hash 验证）。

### 10.3 v1 端点全保留

v1 所有端点（§6 of v1 architecture）全保留，无破坏性变更。新字段通过 JSONB `attributes` 扩展，不改现有 Response 结构。

---

## 11. NATS Event 扩展

**Stream**: `PSI_EVENTS`（保留 v1 所有 subjects）

**新增 subjects（边缘同步）**:

| Subject | 方向 | Payload | 消费者 |
|---------|------|---------|--------|
| `tally.edge.sync.bill` | edge→cloud | `EdgeSyncRecord` | cloud tally-worker edge-sync handler |
| `tally.edge.sync.payment` | edge→cloud | `EdgeSyncRecord` | cloud tally-worker edge-sync handler |
| `tally.edge.downstream.config` | cloud→edge | `DownstreamConfigPayload` | edge-sync-worker |
| `tally.edge.heartbeat` | edge→cloud | `HeartbeatPayload` | cloud（更新 edge_node.last_seen_at） |
| `psi.exchange_rate.updated` | cloud internal | `ExchangeRatePayload` | tally-worker（通知在线用户） |

```go
// internal/pkg/types/events.go (新增)

type EdgeSyncRecord struct {
    EdgeNodeID     string    `json:"edge_node_id"`
    ResourceType   string    `json:"resource_type"`
    ResourceID     string    `json:"resource_id"`
    Payload        any       `json:"payload"`
    EdgeTimestamp  time.Time `json:"edge_timestamp"`
    SchemaVersion  int       `json:"schema_version"`
}

type HeartbeatPayload struct {
    EdgeNodeID    string    `json:"edge_node_id"`
    TenantID      string    `json:"tenant_id"`
    SchemaVersion int       `json:"schema_version"`
    Timestamp     time.Time `json:"timestamp"`
}
```

---

## 12. Backend 目录结构（增量更新）

在 v1 目录基础上新增/修改：

```
internal/
├── app/
│   ├── profile/
│   │   ├── resolver.go              # ProfileResolver + cache
│   │   ├── cross_border.go          # CrossBorderProfile{}
│   │   ├── retail.go                # RetailProfile{}
│   │   └── hybrid.go                # HybridProfile{}
│   ├── stock/
│   │   ├── calculator.go            # InventoryCalculator interface + StockMovement
│   │   ├── calc_wac.go              # WeightedAvgCalculator
│   │   ├── calc_fifo.go             # FIFOCalculator
│   │   ├── calc_weight.go           # ByWeightCalculator
│   │   ├── calc_batch.go            # BatchCalculator
│   │   ├── calc_bulk.go             # BulkMergedCalculator
│   │   └── calculator_factory.go    # 按 Profile + measurement_strategy 选策略
│   ├── edge/
│   │   ├── sync_handler.go          # 云端接收边缘上传的同步记录
│   │   ├── conflict_resolver.go     # 冲突检测 + 写 sync_conflict
│   │   └── downstream.go            # 生成边缘下行配置快照
│   └── finance/
│       ├── fx_rate_job.go           # 汇率定时拉取
│       └── fx_reconcile.go          # 月末汇兑损益对账
├── adapter/
│   ├── middleware/
│   │   └── profile.go               # ProfileMiddleware（新增）
│   ├── handler/v1/
│   │   ├── profile.go               # /api/v1/me/profile, /api/v1/admin/profile
│   │   ├── unit_def.go              # /api/v1/unit-defs, /api/v1/products/:id/units
│   │   ├── edge_node.go             # /api/v1/edge-nodes
│   │   ├── sync_conflict.go         # /api/v1/sync-conflicts
│   │   ├── exchange_rate.go         # /api/v1/exchange-rates
│   │   └── customs.go               # /api/v1/customs-declaration/generate
│   ├── handler/internal/
│   │   └── edge_sync.go             # /internal/v1/edge/sync|downstream|heartbeat
│   ├── pos/
│   │   └── pos_handler.go           # POS 收银（retail profile，build tag: pos）
│   ├── printer/
│   │   └── escpos.go                # ESC/POS 打印（build tag: pos）
│   ├── scale/
│   │   └── serial_scale.go          # 称重秤串口（build tag: scale，edge only）
│   └── payment/
│       ├── wechat_qr.go             # 微信面对面收款
│       └── alipay_qr.go             # 支付宝面对面收款
├── domain/entity/
│   ├── tenant_profile.go            # TenantProfile entity
│   ├── unit_def.go                  # UnitDef entity
│   ├── product_unit.go              # ProductUnit entity
│   ├── edge_node.go                 # EdgeNode entity
│   └── sync_conflict.go             # SyncConflict entity
└── pkg/
    └── unitconv/
        └── converter.go             # 单位换算工具函数
```

**cmd/ 入口分化**:

```
cmd/
├── server/
│   └── main.go          # 云端 binary（默认）
└── edge/
    └── main.go          # 边缘 binary（build tag: edge）
    -- 共享 95% 代码，差异通过 build tag 隔离
```

---

## 13. 前端 Profile 感知

### 13.1 Store

```typescript
// web/stores/profile-store.ts
interface ProfileStore {
  profileType: 'cross_border' | 'retail' | 'hybrid' | null
  features: UIFeatureSet
  isEnabled: (feature: string) => boolean
  // feature 枚举: 'multi_currency'|'hs_code'|'pos'|'scale'|'customs_doc'
  //               |'exchange_rate'|'serial_scale_ws'|'sync_conflicts'
}
```

### 13.2 条件渲染规则

- `useProfile().isEnabled('hs_code')` → 商品表单显示 HS Code 字段
- `useProfile().isEnabled('pos')` → 侧边栏显示"收银台"菜单项
- `useProfile().isEnabled('multi_currency')` → 单据表单显示货币选择器
- `useProfile().isEnabled('sync_conflicts')` → 顶栏显示冲突通知 Badge
- `useProfile().profileType === 'cross_border'` → 合作伙伴表单显示"默认收款货币"字段

### 13.3 新增页面

```
web/app/(dashboard)/
├── sync-conflicts/
│   ├── page.tsx              # 同步冲突列表（edge 租户可见）
│   └── [id]/
│       └── page.tsx          # 冲突裁决详情（edge/cloud 数据对比）
├── edge-nodes/
│   └── page.tsx              # 边缘节点管理（管理员）
└── pos/
    └── page.tsx              # 收银台（retail profile）
```

---

## 14. 部署架构

### 14.1 云端（不变 + 新增边缘 API）

- SaaS：R6 STAGE（43.226.38.244） / R1 PROD（100.98.57.55）
- K8s namespace: `lurus-tally`
- 镜像：`ghcr.io/hanmahong5-arch/lurus-tally-backend:main-<sha7>`（云端 backend）
- 新增镜像：`ghcr.io/hanmahong5-arch/lurus-tally-edge:main-<sha7>`（边缘 binary，Linux/Windows/macOS 多架构）

### 14.2 边缘节点部署

```
边缘节点安装流程:
1. 管理员在云端 /app/edge-nodes 注册节点，获得 edge_api_key
2. 下载 tally-edge binary（或 Tauri 安装包）
3. 配置环境变量:
   CLOUD_URL=https://tally.lurus.cn
   EDGE_API_KEY=<key>
   EDGE_NODE_ID=<uuid>
   SQLITE_PATH=/data/tally-edge.db
   NATS_URL=nats://nats.messaging.svc:4222  # 若网络可达
4. 运行: ./tally-edge（前台）或 systemctl enable tally-edge（Linux 服务）
5. 边缘 Web: http://localhost:18300（内置 Next.js standalone）
```

边缘 binary 不在 K8s 管理，由用户自行部署到本地机器（Linux PC、Windows 工作站、树莓派）。

自动更新：边缘 binary 定时（每日）检查 `https://tally.lurus.cn/internal/v1/edge/latest-version`，若版本不匹配，下载新 binary 并热重启（SIGTERM + exec 替换）。

### 14.3 新增 K8s 资源

```yaml
# deploy/k8s/base/backend-deployment.yaml 新增环境变量
- name: EXCHANGE_RATE_API_KEY       # ExchangeRate-API key
  valueFrom:
    secretKeyRef:
      name: tally-secrets
      key: EXCHANGE_RATE_API_KEY
- name: EDGE_SYNC_ENABLED
  value: "true"
- name: EDGE_API_KEY_SECRET         # 用于验证边缘 Bearer token 的 HMAC secret
  valueFrom:
    secretKeyRef:
      name: tally-secrets
      key: EDGE_API_KEY_SECRET
```

---

## 15. Migration 增量计划（000013–000021）

现状 head=12（27 张表 + 1 MV + RLS）。

| Migration | 内容 | 影响 | Down SQL 要点 |
|-----------|------|------|--------------|
| `000013_add_tenant_profile` | `tenant_profile` 表 + RLS | 新表 | DROP TABLE |
| `000014_add_unit_def_product_unit` | `unit_def` + `product_unit` + RLS | 新表 | DROP TABLE（先 product_unit，再 unit_def） |
| `000015_upgrade_product` | `product` 加 3 列 + GIN 索引 | ALTER TABLE | DROP INDEX; ALTER TABLE DROP COLUMN（3 次） |
| `000016_add_offline_cols` | `bill_head/bill_item/payment_head` 各加 4 列（origin/sync_status/edge_node_id/edge_timestamp） + 索引 | ALTER TABLE × 3 | ALTER TABLE DROP COLUMN × 12；DROP INDEX × 3 |
| `000017_add_edge_node` | `edge_node` 表 + RLS + `bill_head` FK | 新表 + ALTER | DROP CONSTRAINT；DROP TABLE |
| `000018_add_sync_conflict` | `sync_conflict` 表 + RLS | 新表 | DROP TABLE |
| `000019_add_currency` | `currency` 表 + `exchange_rate` 表 + `partner.default_currency` + `bill_head` 3 列 | 新表 + ALTER | DROP TABLE × 2；ALTER TABLE DROP COLUMN |
| `000020_add_gin_indexes` | 3 个 GIN 索引（product.attributes，tenant_profile.custom_overrides，edge_node.settings） | 纯索引 | DROP INDEX × 3 |
| `000021_complete_rls` | 补全 product_unit、unit_def 等漏出 RLS policy | 纯 RLS | DROP POLICY |

每个 migration 下行 SQL (`.down.sql`) 必须能完整还原，不允许 "no-op down"。

---

## 16. NFR 实现路径

| NFR | 目标 | 实现机制 |
|-----|------|---------|
| 离线可用率 | 99%（边缘功能） | 边缘 SQLite WAL + service worker IndexedDB 双重冗余 |
| 同步延迟 | 60s | edge-sync-worker 每 30s polling；NATS JetStream 推送另一路 |
| 首次开单 | < 5 分钟（五金店） | Profile='retail' 默认值大量预填；商品快速添加（扫码直接创建）；向导引导 |
| API P95 | < 200ms（常规）| 同 v1；profile 解析有内存缓存（TTL 5min），不每次查 DB |
| 金额精度 | 无精度丢失 | 全程 decimal.Decimal + NUMERIC(18,4)；汇率 NUMERIC(20,8) |
| 冲突裁决 | 财务类 100% 人工 | sync_conflict 表强制写入；界面裁决 |

---

## 17. ADR（v2 新增）

### ADR-009: Profile 注入 vs URL 分叉

**背景**: 双 Persona 带来 UI/逻辑差异，需要决定如何在 API 层表达。
**决策**: 同一 endpoint，Profile 感知返回字段 + 条件校验。不做 `/cross-border/` vs `/retail/` URL 分叉。
**理由**: endpoint 分叉会导致客户端路由逻辑翻倍，前端维护成本指数增长；Profile 驻留在 ctx 中，应用层可无缝使用；前端 `useProfile()` hook 统一管理渲染差异。
**权衡**: 同一 endpoint Response schema 含两个 Profile 的字段超集；前端需要 null-safe 处理。

### ADR-010: 边缘 SQLite 驱动选 modernc.org/sqlite（纯 Go）而非 mattn/go-sqlite3（CGO）

**背景**: 边缘 binary 需要交叉编译（Windows、Linux amd64/arm64），CGO 阻碍交叉编译。
**决策**: `modernc.org/sqlite`（CGO_ENABLED=0，纯 Go 实现）。
**理由**: 交叉编译零成本；与云端 PostgreSQL 接口对齐（通过 database/sql）；mattn/go-sqlite3 需要目标平台 GCC 工具链，CI 复杂度高。
**风险**: modernc.org/sqlite 性能略低于 mattn；边缘场景单用户访问，吞吐不是瓶颈。

### ADR-011: 边缘同步协议选 NATS JetStream + HTTP Polling 双轨

**背景**: 边缘网络不稳定（门店网络），需要可靠同步。
**决策**: 上行走 NATS JetStream（持久队列），断网时降级为本地 SQLite queue + HTTP Polling 重试；下行走 HTTP Polling（不用 NATS 推送，减少边缘长连接依赖）。
**理由**: NATS JetStream 提供 at-least-once 语义；边缘断网时 NATS 不可达，SQLite queue 兜底；下行配置变更频率低（每日一次量级），Polling 足够。

### ADR-012: 冲突解决策略选人工裁决（财务相关）+ last-write-wins（非财务）

**背景**: 离线冲突不可避免（边缘和云端同时修改同一单据）。
**决策**: 金额/库存字段强制 sync_conflict 表人工裁决；商品主档/配置走 last-write-wins by edge_timestamp。
**理由**: 财务数据错误代价极高（账目不平）；商品主档错误代价低（可重改）；last-write-wins by edge_timestamp 足够简单，五金店老板能理解。

### ADR-013: 库存计算 Strategy Pattern

**背景**: v1 只有 WAC；v2 需要 FIFO/称重/批次，且不同 Profile 需要不同默认。
**决策**: Strategy Pattern（interface + 多实现），Profile 决定默认，measurement_strategy 做二次选择。
**理由**: 不同算法行为差异大，if/else 嵌套会失控；Strategy Pattern 使各算法独立测试；factory 函数集中路由逻辑，handler 层无感知。

---

## 18. 安全

v1 §10 全保留（RBAC、审计日志、RLS、JWT 验证）。

**新增**:
- 边缘 API Key：128 位随机串，SHA-256 哈希存库，明文只在注册时返回一次（类 GitHub Personal Token 模式）
- 边缘通信必须 HTTPS（tally.lurus.cn 有通配符 TLS）
- 边缘 API Key 吊销端点：`POST /api/v1/edge-nodes/:id/revoke`，立即失效（不依赖 token 过期）

---

## 19. 性能

v1 §12 全保留（API P95 < 200ms、LCP < 1.5s 等）。

**新增影响点**:
- GIN 索引（product.attributes）：索引写放大约 1.3x；查询 `attributes @> '{"hs_code":"..."}'` 扫描量大幅降低
- Profile 缓存（`sync.Map` TTL 5min）：每次请求节省 1 次 DB 往返（~5ms）
- 边缘同步 handler（`/internal/v1/edge/sync`）：批量写（一次最多 100 条记录），用 DB 事务包裹

---

## 20. 可观测性（v1 基础上补充）

```go
// pkg/metrics/metrics.go 新增
var (
    EdgeSyncTotal = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "tally_edge_sync_total",
    }, []string{"tenant_id", "edge_node_id", "result"})  // result: synced|conflict|error

    SyncConflictOpenGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
        Name: "tally_sync_conflict_open",
    }, []string{"tenant_id"})

    ExchangeRateFetchTotal = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "tally_exchange_rate_fetch_total",
    }, []string{"source", "result"})  // source: pboc|exchangerate_api; result: ok|error

    ProfileType = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "tally_request_by_profile",
    }, []string{"profile_type", "endpoint"})
)
```

---

## 21. 风险与缓解

| 风险 | 等级 | 缓解措施 |
|------|------|---------|
| 双 Persona UI 复杂度导致回归 | 高 | Profile 参数化 e2e 测试（每个 profile 独立 Playwright 测试套） |
| 离线冲突积压（用户不处理 sync_conflict）| 高 | Dashboard 红色 Badge 强提示；超 10 条冲突则阻止新的边缘单据审核 |
| modernc.org/sqlite 与 PostgreSQL 行为差异 | 中 | 单元测试用 SQLite，集成测试必须用 PostgreSQL；DDL 差异由 build tag 隔离 |
| 边缘 binary 自动更新失败导致版本碎片 | 中 | 强制版本检查（schema_version 不匹配则拒绝同步，提示升级） |
| 汇率 API 不可达（财务数据依赖）| 中 | 保留最近一次有效汇率 + Grafana 告警；单据允许手动输入汇率 |
| GIN 索引写放大（高频商品更新）| 低 | 监控 `pg_stat_user_indexes.idx_blks_hit`；可降级为 btree 索引特定字段 |
| ESC/POS 打印机驱动兼容（Windows 设备路径差异）| 低 | 网络打印（TCP:9100）作为首选；USB 作为降级；文档化已验证型号列表 |
| 边缘 API Key 泄露（门店人员离职）| 低 | 提供吊销端点；API Key 不存明文；吊销后旧 Key 立即失效 |

---

## 22. 关键不变约束（设计规则速查）

| 规则 | 说明 |
|------|------|
| 所有金额字段 | `NUMERIC(18,4)`，禁止 float |
| 汇率字段 | `NUMERIC(20,8)`，Go 侧 `decimal.Decimal` |
| 库存数量存储单位 | 统一 base_unit，换算在应用层 |
| 库存变更唯一路径 | 只通过 `InventoryCalculator.ApplyMovement` 触发，禁止直接 UPDATE stock_snapshot |
| Profile 不分叉 URL | 同一 endpoint，Profile 通过 ctx 注入，不通过 URL prefix |
| 财务冲突强制人工 | 金额/库存字段离线冲突必须走 sync_conflict 表，禁止 auto-merge |
| 边缘 binary 无 CGO | build tag `edge` 强制 `modernc.org/sqlite`（纯 Go），CI 验证 CGO_ENABLED=0 可通过 |
| 所有 LLM 调用 | 必须走 Hub（api.lurus.cn），边缘端 AI 功能降级为本地报表 |
| tenant_id 传递 | JWT → middleware → `SET LOCAL app.tenant_id` → RLS 自动过滤（云端）；边缘单 tenant，固定 |
| 反审核规则 | status=4（完成）禁止反审核，只能走红冲 |
| 单据编号格式 | `{prefix}-{YYYYMMDD}-{sequence}` |
| License 合规 | jshERP/GreaterWMS 衍生代码保留注释 + THIRD_PARTY_LICENSES/ |
