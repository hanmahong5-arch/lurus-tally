# Lurus Tally — 能抄就抄，抄不到就分析后集成 (详细计划)

**生成时间**: 2026-04-23  
**模式**: technical  
**源码已实际克隆**: jshERP @ `/tmp/jshERP` · GreaterWMS @ `/tmp/GreaterWMS`  
**License 验证**: jshERP Apache-2.0 (https://github.com/jishenghua/jshERP/blob/master/LICENSE) · GreaterWMS Apache-2.0 (https://github.com/GreaterWMS/GreaterWMS/blob/main/LICENSE)

---

## 1. Executive Summary

jshERP 的核心抽象是 **`depot_head + depot_item` 通用单据模型**，用一对主-子表承载采购入库、销售出库、调拨、盘点录入、组装/拆卸等全部单据类型，靠 `type`（入库/出库/其它）+ `sub_type`（采购/销售退货/调拨…共 12 种）组合区分业务含义。该设计极度精简，Lurus Tally 应继承此抽象但用 PostgreSQL RLS 替代 `tenant_id` 列散布全表的做法。库存成本算法使用**移动加权平均（WAC）**，通过扫描全量 `depot_item` 历史流水重算，无 FIFO 实现，Lurus 需自主补充 FIFO 层。GreaterWMS 贡献了 jshERP 没有的 **货位(bin)/ASN/拣货单/差异分类** 设计，适合拼接进仓储模块。

---

## 2. jshERP 全表清单与转换

### 2.1 数据库总览（32 张表，已实际读取 SQL）

**来源**: `jshERP-boot/docs/jsh_erp.sql` (MySQL 8.0, 2026-04-04 版本)

| jshERP 原表名 | 推荐 PostgreSQL 表名 | 业务域 | 抄写建议 |
|---|---|---|---|
| jsh_tenant | tenant | 租户 | 结构抄，但 Lurus 已有 platform 管租户，此表只做本地缓存 |
| jsh_user | user_profile | 用户 | 抄字段，但密码/auth 完全剔除，靠 Zitadel |
| jsh_organization | org_department | 部门 | 无脑抄 |
| jsh_orga_user_rel | org_user_rel | 部门-用户关系 | 无脑抄 |
| jsh_role | role | 角色 | 无脑抄（RBAC 基础） |
| jsh_function | menu_function | 菜单/功能模块 | 抄结构，值全部替换为 Lurus 路由 |
| jsh_role_function | role_function | 角色-功能关系 | 无脑抄 |
| jsh_user_business | user_business | 用户-业务配置 (仓库/角色/经手人权限) | 结构抄，JSON 格式重构 |
| jsh_supplier | partner | 供应商/客户 (合二为一靠 type 字段) | 抄，但拆成 partner + partner_bank |
| jsh_depot | warehouse | 仓库 | 无脑抄 |
| jsh_material | product | 商品 | 抄 90%，加 embedding/ai_metadata |
| jsh_material_category | product_category | 商品分类（树形） | 无脑抄 |
| jsh_material_extend | product_sku | SKU/多单位/多价格 | 抄，字段重命名更清晰 |
| jsh_material_attribute | product_attribute | 属性组（颜色/尺码…） | 无脑抄 |
| jsh_material_property | product_field_alias | 字段别名配置 | 抄，价值低 |
| jsh_material_initial_stock | stock_initial | 期初库存 | 无脑抄 |
| jsh_material_current_stock | stock_snapshot | 库存快照（实时缓存） | 抄结构，改名更清晰 |
| jsh_unit | unit | 多单位换算 | 无脑抄 |
| jsh_depot_head | bill_head | **核心：单据主表** | 抄并扩展状态机 |
| jsh_depot_item | bill_item | **核心：单据明细** | 抄并加 lot_id |
| jsh_account | finance_account | 资金账户 | 无脑抄 |
| jsh_account_head | payment_head | 收款/付款单据 | 无脑抄 |
| jsh_account_item | payment_item | 收款/付款明细 | 无脑抄 |
| jsh_serial_number | stock_serial | 序列号台账 | 无脑抄 |
| jsh_in_out_item | finance_category | 收支项目分类 | 无脑抄 |
| jsh_person | staff | 经手人 | 无脑抄 |
| jsh_sequence | bill_sequence | 单据编号生成器 | 抄结构，PostgreSQL 用 sequences 替代 |
| jsh_log | audit_log | 操作日志 | 抄，扩展字段 |
| jsh_msg | notification | 系统消息 | 可弃，Lurus 已有 notification 服务 |
| jsh_sys_dict_type | dict_type | 数据字典类型 | 无脑抄 |
| jsh_sys_dict_data | dict_data | 数据字典值 | 无脑抄 |
| jsh_platform_config | platform_config | 平台级配置 | 抄，合并进 system_config |
| jsh_system_config | system_config | 租户级系统配置 | 抄，扩展 AI 配置项 |

---

### 2.2 核心表详细分析

#### jsh_depot_head → bill_head（单据主表，最重要）

**原始字段**（实际验证）:
```sql
-- jshERP 原始（MySQL）
id bigint AUTO_INCREMENT
type varchar(50)              -- '入库' / '出库' / '其它'
sub_type varchar(50)          -- 见下方常量表
default_number varchar(50)    -- 系统初始票号
number varchar(50)            -- 最终票号（允许改）
create_time datetime
oper_time datetime            -- 实际出入库时间
organ_id bigint               -- 供应商/客户 id
creator bigint
account_id bigint             -- 结算账户
change_amount decimal(24,6)   -- 实付/实收金额
back_amount decimal(24,6)     -- 找零
total_price decimal(24,6)     -- 合计
pay_type varchar(50)          -- 现付/预付款/记账/…
bill_type varchar(50)         -- 单据类型（备注用）
remark varchar(1000)
file_name varchar(1000)       -- 附件
sales_man varchar(50)         -- 销售员（逗号分隔多人，设计糟糕）
account_id_list varchar(50)   -- 多账户（逗号分隔，设计糟糕）
account_money_list varchar(200)
discount decimal(24,6)        -- 折扣率
discount_money decimal(24,6)
discount_last_money decimal(24,6) -- 折后金额
other_money decimal(24,6)     -- 运杂费
deposit decimal(24,6)         -- 订金
status varchar(1)             -- '0'未审核 '1'已审核 '2'完成 '3'部分 '9'审核中
purchase_status varchar(1)    -- '0'未采购 '2'完成 '3'部分
source varchar(1)             -- '0' PC '1' 手机
link_number varchar(50)       -- 关联单号（如采购订单号）
link_apply varchar(50)        -- 关联请购单号
tenant_id bigint
delete_flag varchar(1)        -- '0'正常 '1'删除
```

**type × sub_type 完整组合**（源自 `BusinessConstants.java` 实际代码）:

| type | sub_type | 业务含义 | 库存影响 |
|---|---|---|---|
| 入库 | 采购 | 采购入库 | +库存 |
| 入库 | 采购退货 | 采购退货入库（反向）| 实际上游层面出库退回 |
| 入库 | 销售退货 | 销售退货（客户退货） | +库存 |
| 入库 | 零售退货 | 零售退货 | +库存 |
| 入库 | 其它 | 其他杂项入库 | +库存 |
| 入库 | 盘点录入 | 盘点差异补入 | +库存 |
| 入库 | 组装单 | BOM 组装产出 | +库存（成品） |
| 出库 | 销售 | 销售出库 | -库存 |
| 出库 | 采购退货 | 采购退货出库（退还供应商）| -库存 |
| 出库 | 零售 | 零售出库 | -库存 |
| 出库 | 其它 | 其他杂项出库 | -库存 |
| 出库 | 拆卸单 | BOM 拆卸 | -库存（成品） |
| 其它 | 调拨 | 跨仓调拨 | 仓 A -，仓 B + |
| 其它 | 盘点复盘 | 盘点调平 | 按差异调整 |
| 其它 | 请购单 | 采购申请（无库存影响）| 无 |
| 其它 | 采购订单 | 采购合同（无库存影响）| 无 |
| 其它 | 销售订单 | 销售合同（无库存影响）| 无 |

**Lurus 转换建议**（PostgreSQL）:

```sql
CREATE TABLE bill_head (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL,                           -- RLS key
    bill_no     VARCHAR(50) NOT NULL,                    -- 最终单号
    bill_no_draft VARCHAR(50),                           -- 初始草稿号
    bill_type   VARCHAR(30) NOT NULL,                    -- 入库/出库/其它
    sub_type    VARCHAR(30) NOT NULL,                    -- 见枚举
    status      SMALLINT NOT NULL DEFAULT 0,             -- 0草稿 1已审 2完成 3部分 9审核中
    purchase_status SMALLINT DEFAULT 0,
    partner_id  UUID REFERENCES partner(id),             -- 供应商/客户
    operator_id UUID,                                    -- 经手人
    creator_id  UUID NOT NULL,
    account_id  UUID REFERENCES finance_account(id),
    bill_date   TIMESTAMPTZ NOT NULL,                    -- 实际业务时间
    total_amount     NUMERIC(18,4) NOT NULL DEFAULT 0,
    paid_amount      NUMERIC(18,4) NOT NULL DEFAULT 0,
    discount_rate    NUMERIC(8,4),
    discount_amount  NUMERIC(18,4),
    other_amount     NUMERIC(18,4),
    deposit_amount   NUMERIC(18,4),
    pay_type    VARCHAR(30),
    remark      TEXT,
    attachments JSONB DEFAULT '[]',                      -- 替代 file_name 字符串
    salesperson_ids UUID[],                              -- 替代逗号分隔字符串
    link_bill_id UUID REFERENCES bill_head(id),          -- 关联单据
    source      VARCHAR(10) DEFAULT 'web',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ                              -- 软删除
);

CREATE INDEX idx_bill_head_tenant ON bill_head(tenant_id);
CREATE INDEX idx_bill_head_bill_no ON bill_head(tenant_id, bill_no);
CREATE INDEX idx_bill_head_type ON bill_head(tenant_id, bill_type, sub_type);
CREATE INDEX idx_bill_head_partner ON bill_head(tenant_id, partner_id);
CREATE INDEX idx_bill_head_date ON bill_head(tenant_id, bill_date);

-- RLS
ALTER TABLE bill_head ENABLE ROW LEVEL SECURITY;
CREATE POLICY bill_head_tenant_isolation ON bill_head
    USING (tenant_id = current_setting('app.tenant_id')::UUID);
```

**Go struct**:

```go
// Derived from jshERP jsh_depot_head (Apache-2.0)
// Modified: UUID PK, PostgreSQL RLS, status as int, attachments as JSON
type BillHead struct {
    ID             uuid.UUID        `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
    TenantID       uuid.UUID        `gorm:"type:uuid;not null;index"`
    BillNo         string           `gorm:"size:50;not null"`
    BillNoDraft    string           `gorm:"size:50"`
    BillType       string           `gorm:"size:30;not null"`  // 入库/出库/其它
    SubType        string           `gorm:"size:30;not null"`  // 采购/销售/…
    Status         int16            `gorm:"not null;default:0"`
    PurchaseStatus int16            `gorm:"default:0"`
    PartnerID      *uuid.UUID       `gorm:"type:uuid"`
    OperatorID     *uuid.UUID       `gorm:"type:uuid"`
    CreatorID      uuid.UUID        `gorm:"type:uuid;not null"`
    AccountID      *uuid.UUID       `gorm:"type:uuid"`
    BillDate       time.Time        `gorm:"not null"`
    TotalAmount    decimal.Decimal  `gorm:"type:numeric(18,4);not null;default:0"`
    PaidAmount     decimal.Decimal  `gorm:"type:numeric(18,4);not null;default:0"`
    DiscountRate   *decimal.Decimal `gorm:"type:numeric(8,4)"`
    DiscountAmount *decimal.Decimal `gorm:"type:numeric(18,4)"`
    OtherAmount    *decimal.Decimal `gorm:"type:numeric(18,4)"`
    DepositAmount  *decimal.Decimal `gorm:"type:numeric(18,4)"`
    PayType        string           `gorm:"size:30"`
    Remark         string           `gorm:"type:text"`
    Attachments    datatypes.JSON   `gorm:"type:jsonb;default:'[]'"`
    SalespersonIDs pq.StringArray   `gorm:"type:uuid[]"`
    LinkBillID     *uuid.UUID       `gorm:"type:uuid"`
    Source         string           `gorm:"size:10;default:'web'"`
    CreatedAt      time.Time
    UpdatedAt      time.Time
    DeletedAt      gorm.DeletedAt   `gorm:"index"`
}

func (BillHead) TableName() string { return "bill_head" }
```

---

#### jsh_depot_item → bill_item（单据明细）

**关键设计**：`depot_id` 是出发仓库，`another_depot_id` 是调拨目标仓库（仅调拨时有值）。`sn_list` 存储序列号列表（逗号分隔，设计糟糕）。`batch_number` 是批号。

```sql
CREATE TABLE bill_item (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID NOT NULL,
    head_id           UUID NOT NULL REFERENCES bill_head(id) ON DELETE CASCADE,
    product_id        UUID NOT NULL REFERENCES product(id),
    product_sku_id    UUID REFERENCES product_sku(id),          -- 对应 material_extend_id
    warehouse_id      UUID REFERENCES warehouse(id),
    target_warehouse_id UUID REFERENCES warehouse(id),          -- 调拨目标仓
    unit_name         VARCHAR(20),                              -- 操作单位名称
    sku_attrs         VARCHAR(100),                             -- 多属性值（如 "红色,M"）
    qty               NUMERIC(18,4) NOT NULL,                   -- 操作数量
    base_qty          NUMERIC(18,4),                            -- 换算为基础单位数量
    unit_price        NUMERIC(18,6),                            -- 单价
    purchase_price    NUMERIC(18,6),                            -- 采购价（出库时记录成本）
    tax_rate          NUMERIC(8,4),
    tax_amount        NUMERIC(18,4),
    line_amount       NUMERIC(18,4),                            -- 行金额（含税）
    lot_no            VARCHAR(100),                             -- 批号
    serial_nos        TEXT[],                                   -- 序列号数组（替代逗号字符串）
    expiry_date       DATE,
    link_item_id      UUID REFERENCES bill_item(id),            -- 关联明细（如退货关联原出库）
    remark            VARCHAR(500),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at        TIMESTAMPTZ
);

CREATE INDEX idx_bill_item_head ON bill_item(head_id);
CREATE INDEX idx_bill_item_product ON bill_item(tenant_id, product_id);
CREATE INDEX idx_bill_item_warehouse ON bill_item(tenant_id, warehouse_id);
ALTER TABLE bill_item ENABLE ROW LEVEL SECURITY;
CREATE POLICY bill_item_tenant_isolation ON bill_item
    USING (tenant_id = current_setting('app.tenant_id')::UUID);
```

**Go struct**:

```go
// Derived from jshERP jsh_depot_item (Apache-2.0)
type BillItem struct {
    ID                 uuid.UUID       `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
    TenantID           uuid.UUID       `gorm:"type:uuid;not null"`
    HeadID             uuid.UUID       `gorm:"type:uuid;not null;index"`
    ProductID          uuid.UUID       `gorm:"type:uuid;not null;index"`
    ProductSKUID       *uuid.UUID      `gorm:"type:uuid"`
    WarehouseID        *uuid.UUID      `gorm:"type:uuid;index"`
    TargetWarehouseID  *uuid.UUID      `gorm:"type:uuid"`
    UnitName           string          `gorm:"size:20"`
    SKUAttrs           string          `gorm:"size:100"`
    Qty                decimal.Decimal `gorm:"type:numeric(18,4);not null"`
    BaseQty            *decimal.Decimal `gorm:"type:numeric(18,4)"`
    UnitPrice          *decimal.Decimal `gorm:"type:numeric(18,6)"`
    PurchasePrice      *decimal.Decimal `gorm:"type:numeric(18,6)"`
    TaxRate            *decimal.Decimal `gorm:"type:numeric(8,4)"`
    TaxAmount          *decimal.Decimal `gorm:"type:numeric(18,4)"`
    LineAmount         *decimal.Decimal `gorm:"type:numeric(18,4)"`
    LotNo              string          `gorm:"size:100"`
    SerialNos          pq.StringArray  `gorm:"type:text[]"`
    ExpiryDate         *time.Time
    LinkItemID         *uuid.UUID      `gorm:"type:uuid"`
    Remark             string          `gorm:"size:500"`
    CreatedAt          time.Time
    DeletedAt          gorm.DeletedAt  `gorm:"index"`
}
```

---

#### jsh_material → product（商品/物料主表）

**原始字段**（实际验证）: `name`, `mfrs`(制造商), `model`(型号), `standard`(规格), `brand`, `mnemonic`(助记码), `color`, `unit`, `remark`, `img_name`, `unit_id`, `expiry_num`(保质期天数), `weight`, `enabled`, `other_field1/2/3`(自定义), `enable_serial_number`, `enable_batch_number`, `position`(货架位), `attribute`(多属性JSON), `category_id`, `tenant_id`

**Lurus 转换**（增加 AI 字段）:

```go
// Derived from jshERP jsh_material (Apache-2.0)
// Added: embedding, ai_metadata, predicted_* fields for AI capabilities
type Product struct {
    ID                 uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
    TenantID           uuid.UUID      `gorm:"type:uuid;not null;index"`
    CategoryID         *uuid.UUID     `gorm:"type:uuid"`
    Code               string         `gorm:"size:100;uniqueIndex:idx_product_code_tenant"`
    Name               string         `gorm:"size:200;not null"`
    Manufacturer       string         `gorm:"size:100"`
    Model              string         `gorm:"size:100"`
    Spec               string         `gorm:"size:200"`
    Brand              string         `gorm:"size:100"`
    Mnemonic           string         `gorm:"size:100"`  // 助记码/拼音首字母
    Color              string         `gorm:"size:50"`
    UnitID             *uuid.UUID     `gorm:"type:uuid"`
    ExpiryDays         *int
    WeightKg           *decimal.Decimal `gorm:"type:numeric(18,4)"`
    Enabled            bool           `gorm:"default:true"`
    EnableSerialNo     bool           `gorm:"default:false"`
    EnableLotNo        bool           `gorm:"default:false"`
    ShelfPosition      string         `gorm:"size:100"`
    AttributeGroupID   *uuid.UUID     `gorm:"type:uuid"`
    ImgURLs            pq.StringArray `gorm:"type:text[]"`
    CustomField1       string         `gorm:"size:500"`
    CustomField2       string         `gorm:"size:500"`
    CustomField3       string         `gorm:"size:500"`
    Remark             string         `gorm:"type:text"`
    // AI 专属字段
    Embedding          pgvector.Vector `gorm:"type:vector(1536)"`    // pgvector
    AIMetadata         datatypes.JSON  `gorm:"type:jsonb;default:'{}'"`
    PredictedDemand    *decimal.Decimal `gorm:"type:numeric(18,4)"` // AI 预测月需求量
    PredictedStockout  *time.Time                                   // AI 预测缺货时间
    CreatedAt          time.Time
    UpdatedAt          time.Time
    DeletedAt          gorm.DeletedAt  `gorm:"index"`
}
```

---

#### jsh_material_extend → product_sku（SKU/多单位/多价格）

**核心设计**: 每个商品对应 1-N 个 SKU。`default_flag='1'` 的是基础 SKU。多单位通过多条 SKU 记录实现（如"个"和"箱"各一条，通过 `unit` 表换算比例）。多属性（颜色+尺码）通过 `sku` 字段存储逗号分隔的属性值。

```go
// Derived from jshERP jsh_material_extend (Apache-2.0)
type ProductSKU struct {
    ID              uuid.UUID       `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
    TenantID        uuid.UUID       `gorm:"type:uuid;not null;index"`
    ProductID       uuid.UUID       `gorm:"type:uuid;not null;index"`
    BarCode         string          `gorm:"size:100;index"`
    UnitName        string          `gorm:"size:50"`
    SKUAttrs        string          `gorm:"size:200"`    // 属性组合，如 "红色,M"
    PurchasePrice   decimal.Decimal `gorm:"type:numeric(18,6);not null;default:0"`
    RetailPrice     decimal.Decimal `gorm:"type:numeric(18,6);not null;default:0"`
    WholesalePrice  decimal.Decimal `gorm:"type:numeric(18,6);not null;default:0"`
    MinPrice        *decimal.Decimal `gorm:"type:numeric(18,6)"`
    IsDefault       bool            `gorm:"default:false"`
    CreatedAt       time.Time
    UpdatedAt       time.Time
    DeletedAt       gorm.DeletedAt  `gorm:"index"`
}
```

---

#### jsh_supplier → partner（供应商/客户统一）

**重要发现**: jshERP 用同一张 `jsh_supplier` 表存供应商和客户，靠 `type` 字段区分（`'供应商'`/`'客户'`/`'会员'`）。表中同时有 `advance_in`（预收款）和 `begin_need_pay`（期初应付）等财务字段，说明应收应付是嵌入式而非独立台账。Lurus 应拆分为 partner（主档）+ partner_ar_ap（应收应付台账）。

```go
// Derived from jshERP jsh_supplier (Apache-2.0)
// Split: partner (master) + partner_ar_ap (ledger, separate table)
type Partner struct {
    ID             uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
    TenantID       uuid.UUID  `gorm:"type:uuid;not null;index"`
    Type           string     `gorm:"size:20;not null"`  // supplier/customer/member
    Name           string     `gorm:"size:255;not null"`
    ContactName    string     `gorm:"size:100"`
    Phone          string     `gorm:"size:30"`
    Mobile         string     `gorm:"size:30"`
    Email          string     `gorm:"size:100"`
    Address        string     `gorm:"size:200"`
    TaxNo          string     `gorm:"size:100"`       // 纳税人识别号
    DefaultTaxRate *decimal.Decimal `gorm:"type:numeric(8,4)"`
    CreditLimit    *decimal.Decimal `gorm:"type:numeric(18,4)"`
    Enabled        bool       `gorm:"default:true"`
    Remark         string     `gorm:"type:text"`
    // AI 字段
    AIMetadata     datatypes.JSON `gorm:"type:jsonb;default:'{}'"`
    CreatedAt      time.Time
    UpdatedAt      time.Time
    DeletedAt      gorm.DeletedAt `gorm:"index"`
}

type PartnerBank struct {
    ID         uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
    TenantID   uuid.UUID `gorm:"type:uuid;not null;index"`
    PartnerID  uuid.UUID `gorm:"type:uuid;not null;index"`
    BankName   string    `gorm:"size:100"`
    AccountNo  string    `gorm:"size:100"`
    IsDefault  bool      `gorm:"default:false"`
}
```

---

#### jsh_material_current_stock → stock_snapshot（库存快照）

jshERP 维护两层：**快照层**（`jsh_material_current_stock`：商品+仓库维度的实时数量）和**流水层**（`depot_item`：所有历史进出明细）。成本价 `current_unit_price` 也存在快照层。Lurus 继承此双层设计。

```go
// Derived from jshERP jsh_material_current_stock (Apache-2.0)
type StockSnapshot struct {
    ID             uuid.UUID       `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
    TenantID       uuid.UUID       `gorm:"type:uuid;not null;uniqueIndex:idx_snapshot_unique"`
    ProductID      uuid.UUID       `gorm:"type:uuid;not null;uniqueIndex:idx_snapshot_unique"`
    WarehouseID    uuid.UUID       `gorm:"type:uuid;not null;uniqueIndex:idx_snapshot_unique"`
    OnHandQty      decimal.Decimal `gorm:"type:numeric(18,4);not null;default:0"`
    AvgCostPrice   decimal.Decimal `gorm:"type:numeric(18,6);not null;default:0"` // WAC 成本价
    UpdatedAt      time.Time       `gorm:"not null"`
}
```

---

#### jsh_serial_number → stock_serial（序列号台账）

```go
// Derived from jshERP jsh_serial_number (Apache-2.0)
type StockSerial struct {
    ID          uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
    TenantID    uuid.UUID  `gorm:"type:uuid;not null;index"`
    ProductID   uuid.UUID  `gorm:"type:uuid;not null;index"`
    WarehouseID *uuid.UUID `gorm:"type:uuid;index"`
    SerialNo    string     `gorm:"size:100;not null;uniqueIndex:idx_serial_tenant"`
    IsSold      bool       `gorm:"default:false"`
    CostPrice   *decimal.Decimal `gorm:"type:numeric(18,6)"`
    InBillNo    string     `gorm:"size:50;index"`
    OutBillNo   string     `gorm:"size:50;index"`
    CreatorID   uuid.UUID  `gorm:"type:uuid"`
    Remark      string     `gorm:"type:text"`
    CreatedAt   time.Time
    UpdatedAt   time.Time
    DeletedAt   gorm.DeletedAt `gorm:"index"`
}
```

---

#### jsh_unit → unit（多单位换算）

jshERP 支持 1 基础单位 + 3 副单位，比例存在 `ratio/ratio_two/ratio_three`。实际应用中超过 3 个副单位的情况极罕见，结构可直接抄。

---

## 3. GreaterWMS 增量贡献

**来源**: `GreaterWMS/` (Django models.py，实际克隆验证)

jshERP 没有但 GreaterWMS 有的设计，按价值排序：

### 3.1 货位(Bin)管理

```python
# GreaterWMS binset/models.py - 货位主档
bin_name, bin_size, bin_property, empty_label, bar_code

# GreaterWMS stock/models.py - 货位库存
StockBinModel: bin_name, goods_code, goods_qty, pick_qty, picked_qty, bin_size, bin_property
```

**Lurus 引入价值**: jshERP 只有仓库级别，没有货位/库位。GreaterWMS 的 `StockBin` 模型提供了 bin 维度的库存明细，适合有货架管理需求的仓库。

**推荐引入**: `warehouse_bin`（货位主档）+ `stock_bin`（货位库存）两张表。

### 3.2 ASN（预报到货单）

```python
# GreaterWMS asn/models.py
AsnListModel: asn_code, asn_status, total_weight, total_volume, total_cost, supplier, bar_code, transportation_fee(JSON)
AsnDetailModel: asn_code, goods_code, goods_qty, goods_actual_qty, sorted_qty,
                goods_shortage_qty, goods_more_qty, goods_damage_qty
```

**jshERP vs GreaterWMS**: jshERP 的采购入库是事后记录，没有预报到货(ASN)。GreaterWMS 的 `goods_shortage_qty/goods_more_qty/goods_damage_qty` 字段处理收货差异（短缺/溢收/损坏）是进销存必须的。

**推荐引入**: `stock_asn_head + stock_asn_item`，差异字段必须引入。

### 3.3 出库拣货单

```python
# GreaterWMS dn/models.py
PickingListModel: dn_code, bin_name, goods_code, picking_status, pick_qty, picked_qty, t_code
```

jshERP 无拣货单概念，仓库出库直接按销售单发货。有仓库员工拣货场景时 GreaterWMS 的拣货单设计必须引入。

### 3.4 库存状态细分

```python
# GreaterWMS stock/models.py - StockListModel
onhand_stock, can_order_stock, ordered_stock, inspect_stock, hold_stock, damage_stock,
asn_stock, dn_stock, pre_load_stock, pre_sort_stock, sorted_stock, pick_stock, picked_stock, back_order_stock
```

比 jshERP 的单一 `current_number` 细粒度得多。Lurus 应引入至少 6 个状态: `available/reserved/in_transit/in_inspect/damaged/on_hold`。

### 3.5 盘点模型

```python
# GreaterWMS cyclecount/models.py
QTYRecorder: mode_code, bin_name, goods_code, goods_qty   # 流水记录
CyclecountModeDayModel: cyclecount_status, goods_qty, physical_inventory, difference  # 结果
```

GreaterWMS 同时有**日循环盘点**和**手工盘点**两种模式，比 jshERP 的盘点录入+盘点复盘更清晰。

---

## 4. OFBiz 设计模式借鉴

**来源**: OFBiz entitymodel.xml (https://github.com/apache/ofbiz-framework) — 无需抄代码，借鉴设计思想

### 4.1 OFBiz vs jshERP 对比

| 概念 | jshERP 设计 | OFBiz 设计 | Lurus 采用 |
|---|---|---|---|
| 商品主档 | jsh_material（单表）| Product + ProductType + ProductFeature（分离） | jshERP 简化版 + OFBiz feature 思路 |
| 库存单元 | current_stock（按商品+仓库）| InventoryItem（按批次/序列号独立行）| 双层：snapshot + item |
| 订单 | depot_head（通用单据）| OrderHeader/OrderItem（严格区分采购/销售）| jshERP 统一单据，但状态机用 OFBiz |
| 设施/仓库 | jsh_depot（简单）| Facility + FacilityLocation（货位）| 引入 GreaterWMS bin 层 |
| 批次 | batch_number 字段 | Lot（独立表，追踪批次所有流转）| 独立 stock_lot 表 |

### 4.2 OFBiz 值得借鉴的 10 个模式

1. **Facility 分级**: Facility（工厂/仓库）→ FacilityLocation（区域）→ FacilityLocationGeoPoint（货位坐标）。Lurus 可简化为 warehouse → zone → bin 三级。
2. **Lot 独立追踪**: OFBiz 的 `Lot` 表（lot_id, quantity, expiry_date, creation_date）配合 `InventoryItemDetail` 记录每批次每笔进出，比 jshERP 的 `batch_number` 字符串字段完整得多。
3. **OrderItem 状态独立**: 每行明细有自己的状态（部分发货、部分收货），而非只有 header 级状态。jshERP 的 `purchase_status` 只在 head 级，无法精确跟踪行级履行进度。
4. **ProductPrice 独立**: OFBiz 把价格体系抽成独立的 `ProductPrice` 表（含 price_type/currency/min_qty/from_date），比 jshERP 的 extend 表更灵活。对多货币/多价格等级场景必须用 OFBiz 思路。
5. **OrderType 扩展性**: OFBiz 用 `OrderType`（枚举表）而非硬编码字符串区分采购/销售/内部调拨/退货，更易扩展。
6. **Payment 与 Invoice 分离**: OFBiz 严格区分 Invoice（应收应付凭证）和 Payment（实际付款），并通过 `PaymentApplication` 关联两者（支持部分付款、一次付款抵多张发票）。jshERP 把这些混在 account_head/account_item 里。
7. **GlAccount（总账科目）对接**: OFBiz 每类交易都映射到科目，支持财务凭证自动生成。Lurus 如需对接财务系统（如用友/金蝶）需预留 `gl_account_code` 字段。
8. **PartyRole 模式**: OFBiz 的 Party（统一 actor）+ PartyRole（角色）设计让同一主体既是供应商又是客户。jshERP 的 supplier 表 type 字段也在做同样的事，但不如 OFBiz 规范。
9. **WorkEffort（工单）**: BOM 组装/拆卸用工单管理，jshERP 的组装单/拆卸单过于简单，无 BOM 展开计算。Lurus 如做生产模块需引入。
10. **Return（退货）独立**:  OFBiz 用 `ReturnHeader/ReturnItem` 独立管理退货，而不是用入库单+负数数量混入正向流程。Lurus 应考虑是否要独立退货单还是复用 jshERP 的退货入库子类型。

---

## 5. ERPNext/Odoo 思想借鉴（不抄代码）

### 5.1 单据状态机（ERPNext 标准）

ERPNext 的标准状态流转（适用于所有采购/销售单据）:

```
草稿(Draft) → 已提交(Submitted) → 已审核(Approved) → [部分履行] → 已完成(Completed)
                    ↓                      ↓
               已取消(Cancelled)    反审核→草稿（红冲创建 Amendment）
```

**关键规则**:
- 已提交单据**不可修改**，修改必须先反审核（产生 amendment 记录）
- 已完成单据**不可反审核**，只能做对冲单（红冲）
- jshERP 的 status 只有 0/1/2/3/9，对应到此状态机: 0=草稿, 9=审核中, 1=已审核, 2/3=完成/部分完成
- Lurus 建议增加 `cancelled` 状态和 `amendment_of_id` 字段（指向被修订的原始单据）

### 5.2 多公司/多租户隔离

- **Odoo 方案**: 每个 company 是逻辑隔离，所有表都有 `company_id`，在 ORM 层自动加过滤
- **ERPNext 方案**: 类似，用 `company` 字段作为 DocType 的顶层隔离维度
- **Lurus 方案**: PostgreSQL RLS（`tenant_id = current_setting('app.tenant_id')::UUID`）。比上述两者都更彻底——数据库层隔离，应用层无需每次手动过滤。

### 5.3 字段元数据驱动的打印模板

- **ERPNext** 用 Jinja2 模板 + DocType 元数据动态生成打印单据（如发货单/采购单/收据）
- **Lurus 实现**: 不需要完整实现，只需在 `system_config` 中存 Go template 字符串，运行时渲染 PDF。核心字段: `template_type`, `template_content`(JSONB), `paper_size`

---

## 6. Lurus Tally 推荐 DDL（PostgreSQL，27 张核心表）

```sql
-- =====================================================
-- 前置：启用扩展
-- =====================================================
CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "vector";     -- pgvector（AI embedding）

-- =====================================================
-- 域 1: tenant_* — 租户本地缓存
-- =====================================================
CREATE TABLE tenant (
    id            UUID PRIMARY KEY,           -- 与 platform 同 ID
    name          VARCHAR(200) NOT NULL,
    status        SMALLINT NOT NULL DEFAULT 1, -- 1启用 0禁用
    plan_type     VARCHAR(30),                -- free/pro/enterprise
    expire_at     TIMESTAMPTZ,
    settings      JSONB NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- =====================================================
-- 域 2: org_* — 组织架构
-- =====================================================
-- Derived from jshERP jsh_organization + jsh_orga_user_rel (Apache-2.0)
CREATE TABLE org_department (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenant(id),
    parent_id   UUID REFERENCES org_department(id),
    name        VARCHAR(100) NOT NULL,
    sort        INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);
CREATE INDEX idx_org_dept_tenant ON org_department(tenant_id);

CREATE TABLE org_user_rel (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL,
    dept_id      UUID REFERENCES org_department(id),
    user_id      UUID NOT NULL,
    sort         INT DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- =====================================================
-- 域 3: partner_* — 供应商/客户
-- =====================================================
-- Derived from jshERP jsh_supplier (Apache-2.0)
CREATE TABLE partner (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL,
    partner_type    VARCHAR(20) NOT NULL CHECK (partner_type IN ('supplier','customer','both','member')),
    name            VARCHAR(255) NOT NULL,
    code            VARCHAR(100),
    contact_name    VARCHAR(100),
    phone           VARCHAR(30),
    mobile          VARCHAR(30),
    email           VARCHAR(100),
    address         TEXT,
    tax_no          VARCHAR(100),
    default_tax_rate NUMERIC(8,4),
    credit_limit    NUMERIC(18,4),
    advance_balance NUMERIC(18,4) NOT NULL DEFAULT 0, -- 预收/预付款余额
    ar_balance      NUMERIC(18,4) NOT NULL DEFAULT 0, -- 应收余额
    ap_balance      NUMERIC(18,4) NOT NULL DEFAULT 0, -- 应付余额
    enabled         BOOLEAN NOT NULL DEFAULT true,
    remark          TEXT,
    ai_metadata     JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ
);
CREATE INDEX idx_partner_tenant ON partner(tenant_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX idx_partner_code ON partner(tenant_id, code) WHERE deleted_at IS NULL AND code IS NOT NULL;
ALTER TABLE partner ENABLE ROW LEVEL SECURITY;
CREATE POLICY partner_rls ON partner USING (tenant_id = current_setting('app.tenant_id')::UUID);

CREATE TABLE partner_bank (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL,
    partner_id  UUID NOT NULL REFERENCES partner(id),
    bank_name   VARCHAR(100),
    account_no  VARCHAR(100),
    account_name VARCHAR(100),
    is_default  BOOLEAN DEFAULT false
);

-- =====================================================
-- 域 4: product_* — 商品/SKU/分类
-- =====================================================
-- Derived from jshERP jsh_material_category (Apache-2.0)
CREATE TABLE product_category (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL,
    parent_id    UUID REFERENCES product_category(id),
    name         VARCHAR(100) NOT NULL,
    code         VARCHAR(50),
    sort         INT NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at   TIMESTAMPTZ
);
CREATE INDEX idx_product_cat_tenant ON product_category(tenant_id);

-- Derived from jshERP jsh_material (Apache-2.0)
-- Added: embedding, ai_metadata, predicted_* (Lurus AI extensions)
CREATE TABLE product (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL,
    category_id         UUID REFERENCES product_category(id),
    code                VARCHAR(100) NOT NULL,
    name                VARCHAR(200) NOT NULL,
    manufacturer        VARCHAR(100),
    model               VARCHAR(100),
    spec                VARCHAR(200),
    brand               VARCHAR(100),
    mnemonic            VARCHAR(100),
    color               VARCHAR(50),
    unit_id             UUID,
    expiry_days         INT,
    weight_kg           NUMERIC(18,4),
    enabled             BOOLEAN NOT NULL DEFAULT true,
    enable_serial_no    BOOLEAN NOT NULL DEFAULT false,
    enable_lot_no       BOOLEAN NOT NULL DEFAULT false,
    shelf_position      VARCHAR(100),
    img_urls            TEXT[],
    custom_field1       VARCHAR(500),
    custom_field2       VARCHAR(500),
    custom_field3       VARCHAR(500),
    remark              TEXT,
    -- AI 专属（Lurus 独有）
    embedding           vector(1536),
    ai_metadata         JSONB NOT NULL DEFAULT '{}',
    predicted_monthly_demand  NUMERIC(18,4),
    predicted_stockout_at     TIMESTAMPTZ,
    recommendation_notes      TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at          TIMESTAMPTZ
);
CREATE INDEX idx_product_tenant ON product(tenant_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX idx_product_code ON product(tenant_id, code) WHERE deleted_at IS NULL;
CREATE INDEX idx_product_embedding ON product USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);
ALTER TABLE product ENABLE ROW LEVEL SECURITY;
CREATE POLICY product_rls ON product USING (tenant_id = current_setting('app.tenant_id')::UUID);

-- Derived from jshERP jsh_material_extend (Apache-2.0)
CREATE TABLE product_sku (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID NOT NULL,
    product_id       UUID NOT NULL REFERENCES product(id),
    bar_code         VARCHAR(100),
    unit_name        VARCHAR(50),
    sku_attrs        VARCHAR(200),    -- "红色,M"
    purchase_price   NUMERIC(18,6) NOT NULL DEFAULT 0,
    retail_price     NUMERIC(18,6) NOT NULL DEFAULT 0,
    wholesale_price  NUMERIC(18,6) NOT NULL DEFAULT 0,
    min_price        NUMERIC(18,6),
    is_default       BOOLEAN NOT NULL DEFAULT false,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);
CREATE INDEX idx_sku_product ON product_sku(product_id);
CREATE INDEX idx_sku_barcode ON product_sku(tenant_id, bar_code) WHERE deleted_at IS NULL;

-- Derived from jshERP jsh_material_attribute (Apache-2.0)
CREATE TABLE product_attribute (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID NOT NULL,
    attribute_name   VARCHAR(100) NOT NULL,
    attribute_values TEXT[],          -- ['红色','橙色','黄色']
    sort             INT DEFAULT 0
);

-- Derived from jshERP jsh_unit (Apache-2.0)
CREATE TABLE unit (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL,
    name            VARCHAR(100) NOT NULL,
    base_unit       VARCHAR(50),
    sub_units       JSONB DEFAULT '[]',  -- [{name:'箱', ratio:12}, ...]
    enabled         BOOLEAN DEFAULT true
);

-- =====================================================
-- 域 5: stock_* — 库存
-- =====================================================
-- Derived from jshERP jsh_material_initial_stock (Apache-2.0)
CREATE TABLE stock_initial (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL,
    product_id      UUID NOT NULL REFERENCES product(id),
    warehouse_id    UUID NOT NULL,
    qty             NUMERIC(18,4) NOT NULL DEFAULT 0,
    low_safe_qty    NUMERIC(18,4),
    high_safe_qty   NUMERIC(18,4),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_stock_initial_unique ON stock_initial(tenant_id, product_id, warehouse_id);

-- Derived from jshERP jsh_material_current_stock (Apache-2.0)
-- Extended with GreaterWMS multi-status stock concept
CREATE TABLE stock_snapshot (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL,
    product_id      UUID NOT NULL REFERENCES product(id),
    warehouse_id    UUID NOT NULL,
    on_hand_qty     NUMERIC(18,4) NOT NULL DEFAULT 0,   -- 实际在库
    available_qty   NUMERIC(18,4) NOT NULL DEFAULT 0,   -- 可用（未锁定）
    reserved_qty    NUMERIC(18,4) NOT NULL DEFAULT 0,   -- 已锁定（待出库）
    in_transit_qty  NUMERIC(18,4) NOT NULL DEFAULT 0,   -- 在途（ASN 未收）
    damage_qty      NUMERIC(18,4) NOT NULL DEFAULT 0,   -- 损坏
    hold_qty        NUMERIC(18,4) NOT NULL DEFAULT 0,   -- 冻结
    avg_cost_price  NUMERIC(18,6) NOT NULL DEFAULT 0,   -- 移动加权平均成本
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_stock_snapshot_unique ON stock_snapshot(tenant_id, product_id, warehouse_id);
ALTER TABLE stock_snapshot ENABLE ROW LEVEL SECURITY;
CREATE POLICY stock_snapshot_rls ON stock_snapshot USING (tenant_id = current_setting('app.tenant_id')::UUID);

-- Derived from jshERP jsh_serial_number (Apache-2.0)
CREATE TABLE stock_serial (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL,
    product_id   UUID NOT NULL REFERENCES product(id),
    warehouse_id UUID,
    serial_no    VARCHAR(100) NOT NULL,
    is_sold      BOOLEAN NOT NULL DEFAULT false,
    cost_price   NUMERIC(18,6),
    in_bill_no   VARCHAR(50),
    out_bill_no  VARCHAR(50),
    creator_id   UUID,
    remark       TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at   TIMESTAMPTZ
);
CREATE UNIQUE INDEX idx_serial_no ON stock_serial(tenant_id, serial_no) WHERE deleted_at IS NULL;

-- NEW（jshERP 无，OFBiz Lot 借鉴）
CREATE TABLE stock_lot (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL,
    product_id   UUID NOT NULL REFERENCES product(id),
    lot_no       VARCHAR(100) NOT NULL,
    manufacture_date DATE,
    expiry_date  DATE,
    qty          NUMERIC(18,4) NOT NULL DEFAULT 0,
    cost_price   NUMERIC(18,6),
    remark       TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_lot_no ON stock_lot(tenant_id, product_id, lot_no);

-- NEW（GreaterWMS binset 借鉴）
CREATE TABLE warehouse_bin (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL,
    warehouse_id UUID NOT NULL,
    bin_code     VARCHAR(100) NOT NULL,
    bin_zone     VARCHAR(50),
    bin_size     VARCHAR(50),    -- S/M/L/XL
    bin_property VARCHAR(50),    -- 常温/冷藏/危品
    is_empty     BOOLEAN DEFAULT true,
    bar_code     VARCHAR(100),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at   TIMESTAMPTZ
);

-- =====================================================
-- 域 6: 单据核心（bill_head + bill_item）
-- =====================================================
-- Derived from jshERP jsh_depot_head (Apache-2.0)
-- Extended: UUID PK, JSONB attachments, array salesperson_ids, link_bill_id FK
CREATE TABLE bill_head (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL,
    bill_no             VARCHAR(50) NOT NULL,
    bill_no_draft       VARCHAR(50),
    bill_type           VARCHAR(30) NOT NULL,    -- 入库/出库/其它
    sub_type            VARCHAR(30) NOT NULL,    -- 采购/销售/调拨/…
    status              SMALLINT NOT NULL DEFAULT 0, -- 0草稿 1已审 2完成 3部分 4取消 9审核中
    purchase_status     SMALLINT DEFAULT 0,
    partner_id          UUID REFERENCES partner(id),
    operator_id         UUID,
    creator_id          UUID NOT NULL,
    account_id          UUID,
    bill_date           TIMESTAMPTZ NOT NULL,
    total_amount        NUMERIC(18,4) NOT NULL DEFAULT 0,
    paid_amount         NUMERIC(18,4) NOT NULL DEFAULT 0,
    discount_rate       NUMERIC(8,4),
    discount_amount     NUMERIC(18,4),
    other_amount        NUMERIC(18,4),
    deposit_amount      NUMERIC(18,4),
    pay_type            VARCHAR(30),
    remark              TEXT,
    attachments         JSONB DEFAULT '[]',
    salesperson_ids     UUID[],
    link_bill_id        UUID REFERENCES bill_head(id), -- 关联原始单据（退货/调拨关联）
    source              VARCHAR(10) DEFAULT 'web',
    amendment_of_id     UUID REFERENCES bill_head(id), -- 修订单关联原单
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at          TIMESTAMPTZ
);
CREATE INDEX idx_bill_head_tenant ON bill_head(tenant_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX idx_bill_head_no ON bill_head(tenant_id, bill_no) WHERE deleted_at IS NULL;
CREATE INDEX idx_bill_head_type ON bill_head(tenant_id, bill_type, sub_type, bill_date);
CREATE INDEX idx_bill_head_partner ON bill_head(tenant_id, partner_id);
ALTER TABLE bill_head ENABLE ROW LEVEL SECURITY;
CREATE POLICY bill_head_rls ON bill_head USING (tenant_id = current_setting('app.tenant_id')::UUID);

-- Derived from jshERP jsh_depot_item (Apache-2.0)
-- Extended: serial_nos as TEXT[], lot_id FK
CREATE TABLE bill_item (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL,
    head_id             UUID NOT NULL REFERENCES bill_head(id) ON DELETE CASCADE,
    product_id          UUID NOT NULL REFERENCES product(id),
    product_sku_id      UUID REFERENCES product_sku(id),
    warehouse_id        UUID,
    target_warehouse_id UUID,
    unit_name           VARCHAR(20),
    sku_attrs           VARCHAR(200),
    qty                 NUMERIC(18,4) NOT NULL,
    base_qty            NUMERIC(18,4),
    unit_price          NUMERIC(18,6),
    purchase_price      NUMERIC(18,6),
    tax_rate            NUMERIC(8,4),
    tax_amount          NUMERIC(18,4),
    line_amount         NUMERIC(18,4),
    lot_id              UUID REFERENCES stock_lot(id),
    serial_nos          TEXT[],
    expiry_date         DATE,
    link_item_id        UUID REFERENCES bill_item(id),
    bin_id              UUID REFERENCES warehouse_bin(id), -- GreaterWMS 货位
    remark              VARCHAR(500),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at          TIMESTAMPTZ
);
CREATE INDEX idx_bill_item_head ON bill_item(head_id);
CREATE INDEX idx_bill_item_product ON bill_item(tenant_id, product_id);
ALTER TABLE bill_item ENABLE ROW LEVEL SECURITY;
CREATE POLICY bill_item_rls ON bill_item USING (tenant_id = current_setting('app.tenant_id')::UUID);

-- =====================================================
-- 域 7: purchase_* / sales_* 单独订单表（OFBiz 思路）
-- 注意：实物单据走 bill_head/bill_item，以下是合同/订单层
-- =====================================================
-- 订单表（采购订单/销售订单）与 bill_head 通过 link_bill_id 关联
-- 此处共用 bill_head，sub_type='采购订单'/'销售订单' 标识
-- 无需单独表，避免 jshERP 的双路径问题

-- =====================================================
-- 域 8: finance_* — 资金账户/收付款
-- =====================================================
-- Derived from jshERP jsh_account (Apache-2.0)
CREATE TABLE finance_account (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID NOT NULL,
    name             VARCHAR(100) NOT NULL,
    code             VARCHAR(50),
    initial_balance  NUMERIC(18,4) NOT NULL DEFAULT 0,
    current_balance  NUMERIC(18,4) NOT NULL DEFAULT 0,
    is_default       BOOLEAN DEFAULT false,
    enabled          BOOLEAN DEFAULT true,
    sort             INT DEFAULT 0,
    remark           VARCHAR(200),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);

-- Derived from jshERP jsh_account_head (Apache-2.0)
CREATE TABLE payment_head (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID NOT NULL,
    pay_type         VARCHAR(30) NOT NULL, -- 收款/付款/转账/支出/收入
    partner_id       UUID REFERENCES partner(id),
    operator_id      UUID,
    creator_id       UUID NOT NULL,
    bill_no          VARCHAR(50),
    pay_date         TIMESTAMPTZ NOT NULL,
    amount           NUMERIC(18,4) NOT NULL,
    discount_amount  NUMERIC(18,4) DEFAULT 0,
    total_amount     NUMERIC(18,4) NOT NULL,
    account_id       UUID REFERENCES finance_account(id),
    related_bill_id  UUID REFERENCES bill_head(id),
    remark           TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);
CREATE INDEX idx_payment_head_tenant ON payment_head(tenant_id) WHERE deleted_at IS NULL;
ALTER TABLE payment_head ENABLE ROW LEVEL SECURITY;
CREATE POLICY payment_head_rls ON payment_head USING (tenant_id = current_setting('app.tenant_id')::UUID);

-- Derived from jshERP jsh_account_item (Apache-2.0)
CREATE TABLE payment_item (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID NOT NULL,
    head_id          UUID NOT NULL REFERENCES payment_head(id),
    finance_category_id UUID,
    amount           NUMERIC(18,4) NOT NULL,
    remark           VARCHAR(500)
);

-- Derived from jshERP jsh_in_out_item (Apache-2.0)
CREATE TABLE finance_category (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    name      VARCHAR(100) NOT NULL,
    cat_type  VARCHAR(20) NOT NULL CHECK (cat_type IN ('income','expense')),
    enabled   BOOLEAN DEFAULT true,
    sort      INT DEFAULT 0
);

-- =====================================================
-- 域 9: audit_* — 操作审计
-- =====================================================
-- Derived from jshERP jsh_log (Apache-2.0)
CREATE TABLE audit_log (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL,
    user_id     UUID,
    action      VARCHAR(50) NOT NULL,   -- create/update/delete/approve/reverse
    resource    VARCHAR(50) NOT NULL,   -- bill_head/product/…
    resource_id UUID,
    changes     JSONB,                  -- before/after diff
    client_ip   VARCHAR(100),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_log_tenant ON audit_log(tenant_id, created_at DESC);
CREATE INDEX idx_audit_log_resource ON audit_log(resource, resource_id);

-- =====================================================
-- 域 10: 系统配置
-- =====================================================
-- Derived from jshERP jsh_system_config (Apache-2.0), extended with AI config
CREATE TABLE system_config (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL,
    key          VARCHAR(100) NOT NULL,
    value        TEXT,
    description  VARCHAR(500),
    UNIQUE (tenant_id, key)
);
-- 关键配置项: move_avg_price_enabled, force_approval_enabled,
--             allow_negative_stock, print_template_*, ai_auto_predict_enabled

-- Derived from jshERP jsh_sys_dict_type + jsh_sys_dict_data (Apache-2.0)
CREATE TABLE dict_type (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID,                    -- NULL 表示系统级
    type_code VARCHAR(100) NOT NULL UNIQUE,
    type_name VARCHAR(100) NOT NULL,
    remark    VARCHAR(500)
);

CREATE TABLE dict_data (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID,
    type_id    UUID NOT NULL REFERENCES dict_type(id),
    label      VARCHAR(100) NOT NULL,
    value      VARCHAR(100) NOT NULL,
    sort       INT DEFAULT 0,
    enabled    BOOLEAN DEFAULT true
);

-- =====================================================
-- 域 11: report_* — 物化视图（性能优化）
-- =====================================================
-- 库存盈亏汇总视图（取代 jshERP 实时计算）
CREATE MATERIALIZED VIEW report_stock_summary AS
SELECT
    ss.tenant_id,
    p.id          AS product_id,
    p.code        AS product_code,
    p.name        AS product_name,
    w.id          AS warehouse_id,
    w.name        AS warehouse_name,
    ss.on_hand_qty,
    ss.available_qty,
    ss.avg_cost_price,
    ss.on_hand_qty * ss.avg_cost_price AS stock_value,
    si.low_safe_qty,
    si.high_safe_qty,
    CASE WHEN ss.available_qty < COALESCE(si.low_safe_qty, 0) THEN true ELSE false END AS is_low_stock
FROM stock_snapshot ss
JOIN product p ON p.id = ss.product_id
JOIN warehouse w ON w.id = ss.warehouse_id
LEFT JOIN stock_initial si ON si.product_id = ss.product_id AND si.warehouse_id = ss.warehouse_id;

CREATE UNIQUE INDEX idx_report_stock_summary ON report_stock_summary(tenant_id, product_id, warehouse_id);

-- 仓库需单独建表
CREATE TABLE warehouse (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL,
    name        VARCHAR(100) NOT NULL,
    address     VARCHAR(200),
    manager_id  UUID,
    enabled     BOOLEAN DEFAULT true,
    is_default  BOOLEAN DEFAULT false,
    sort        INT DEFAULT 0,
    remark      VARCHAR(200),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);
```

---

## 7. 关键业务规则

### 7.1 库存计算机制（实际代码验证）

jshERP 采用**双层机制**（源: `DepotItemService.java`，实际阅读）:

**第一层——快照层** (`jsh_material_current_stock`):
- 每次审核/反审核单据后，调用 `updateCurrentStockFun(mId, dId, currentUnitPrice)` 更新快照
- 快照存储：(`product_id`, `warehouse_id`) → (`current_number`, `current_unit_price`)
- 实时查询直接读快照，O(1) 速度

**第二层——流水层** (`jsh_depot_item`):
- 所有进出记录不删不改（delete_flag 软删除）
- 计算成本价时扫描全量历史重算（WAC）

**库存增减规则**（源: `BusinessConstants.java` 常量 + `DepotItemService` 逻辑）:
- 入库类（采购/销售退货/零售退货/其它入库/盘点录入）: `on_hand_qty += base_qty`
- 出库类（销售/采购退货/零售/其它出库）: `on_hand_qty -= base_qty`
- 调拨: 源仓 `-= qty`, 目标仓 `+= qty`（通过 `another_depot_id` 实现）

### 7.2 移动加权平均成本算法（WAC）

源自 `DepotItemService.updateCurrentUnitPrice()` 实际代码（Apache-2.0，不抄代码，只抄算法思想）：

```
算法逻辑（以时间正序遍历所有 depot_item 流水）:

初始: currentQty=0, currentUnitPrice=0, currentTotalCost=0

对每条流水 item（按 oper_time 正序）:
  if 入库（非销售退货/零售退货）:
    currentTotalCost += item.all_price
    currentQty      += item.basic_number
    if currentQty > 0 and currentTotalCost > 0:
      currentUnitPrice = currentTotalCost / currentQty   ← 重新计算 WAC

  if 入库（销售退货/零售退货）:
    currentTotalCost += item.basic_number * currentUnitPrice   ← 用当前 WAC（不改成本）
    currentQty       += item.basic_number

  if 出库（非采购退货）:
    currentTotalCost += item.basic_number * currentUnitPrice   ← 用当前 WAC 出库
    currentQty       += item.basic_number   ← 注意：basic_number 已是负数

  if 出库（采购退货）:
    currentTotalCost += item.all_price   ← 用原始采购价退回
    currentQty       += item.basic_number
    重新计算 WAC

  if 组装/拆卸/盘点复盘:
    currentTotalCost += item.basic_number * currentUnitPrice
    currentQty       += item.basic_number

溢出保护: if currentUnitPrice > 1亿 or < -1亿: reset to 0
```

**Lurus 注意**: 全量扫描在数据量大时性能差，应改为**增量更新**（每次入库单审核后只更新该商品的快照，不扫描全量历史）。这是 jshERP 的主要性能瓶颈。

### 7.3 FIFO 出库

**jshERP 无 FIFO 实现**（经实际代码验证，只有 WAC）。Lurus 需自主实现：

```
FIFO 出库算法思路（Lurus 自主设计）:
1. 出库时查询该商品该仓库所有未完全消耗的入库批次（按 bill_date 正序）
2. 从最早批次开始扣减，记录每批扣减的 lot_id + qty + cost_price
3. 加权平均出库成本 = sum(lot_qty * lot_cost) / total_out_qty
4. 实现依赖 stock_lot 表 + bill_item.lot_id 字段
```

### 7.4 库存预警

jshERP 在 `jsh_material_initial_stock` 存 `low_safe_stock` 和 `high_safe_stock`，但**没有主动预警推送**，只在查询时计算。Lurus 应：
- 审核入库/出库单后，通过 NATS `LLM_EVENTS` 或 `IDENTITY_EVENTS` 发布库存变更事件
- 独立 worker 订阅事件，检查 `available_qty < low_safe_qty` 触发预警通知

### 7.5 单据审核/反审核（红冲）

**实际代码行为**（源: `DepotHeadService.batchSetStatus()`）:

```
审核（status 0→1）:
  1. 校验负库存（配置开启时）：出库数量 > 当前库存则拒绝
  2. 更新 depot_head.status = '1'
  3. 遍历所有 depot_item，调用 updateCurrentStock() 更新快照
  4. 关联单据更新状态（如采购订单→已完成）

反审核（status 1→0）:
  1. 不可反审已完成(status=2)的单据
  2. 将 depot_head.status 回退为 '0'
  3. 遍历所有 depot_item，反向调用 updateCurrentStock()（数量取反）
  4. 关联单据状态回退
```

**Lurus 扩展**: 对已完成单据需走"红冲"流程（新建对应的退货/反向调拨单），而非直接反审核。`amendment_of_id` 字段记录红冲单与原单的关联。

### 7.6 盘点差异处理

jshERP 两步走：
1. `盘点录入`（sub_type='盘点录入'，type='入库'）：记录实盘数量
2. `盘点复盘`（sub_type='盘点复盘'，type='其它'）：审核后按差异调整库存

差异 = 实盘数量 - 系统库存。若为正，生成入库流水；若为负，生成出库流水（通过正/负数量处理）。

---

## 8. 抄/不抄 决策矩阵

| 模块 | 抄 jshERP | 抄 GreaterWMS | OFBiz 借鉴 | 自主设计 | 优先级 |
|---|---|---|---|---|---|
| 商品主档 | 抄 85%（字段）| — | 价格体系思路 | UUID/多租户/embedding | P0 |
| 商品 SKU/多单位 | 抄 90% | — | — | 命名更清晰 | P0 |
| 商品分类 | 抄 100% | — | — | — | P0 |
| 供应商/客户 | 抄 70%（字段） | 抄 level 字段 | PartyRole 思路 | 拆 bank 表 | P0 |
| 仓库主档 | 抄 100% | 抄 city/contact | — | — | P0 |
| 货位(Bin) | 无 | 抄 90% | Facility 三级思路 | — | P1 |
| 库存快照 | 抄 80% | 抄多状态字段 | — | `available_qty`/`reserved_qty` | P0 |
| 序列号台账 | 抄 100% | — | — | — | P1 |
| 批次(Lot) | 无（只有字段） | — | Lot 独立表 | 自主实现 | P1 |
| 通用单据主表 | 抄 80% | — | 状态机完善 | UUID/JSONB/数组字段 | P0 |
| 通用单据明细 | 抄 85% | 抄差异qty字段 | row-level 状态 | lot_id FK/serial_nos[] | P0 |
| ASN 预报到货 | 无 | 抄 90% | — | — | P1 |
| 拣货单 | 无 | 抄 80% | — | — | P2 |
| 资金账户 | 抄 100% | — | — | — | P1 |
| 收付款单据 | 抄 85% | — | Invoice 分离思路 | — | P1 |
| 应收应付台账 | 嵌入 supplier | — | Invoice 思路 | 独立 ar_ap 表 | P1 |
| WAC 成本算法 | 思想借鉴，自实现 | — | — | 增量更新（非全量扫） | P0 |
| FIFO 出库 | 无 | 无 | — | 自主实现（依赖 lot） | P2 |
| 单据状态机 | 抄状态值 | — | 状态流转完善 | 加 cancelled/amendment | P0 |
| 库存预警 | 字段抄，逻辑重写 | safety_stock 字段 | — | NATS 事件驱动 | P1 |
| 盘点 | 抄两步流程思路 | 抄循环盘点概念 | — | — | P1 |
| 操作日志 | 抄 85% | — | — | changes JSONB diff | P0 |
| 字典/配置 | 抄 100% | — | — | — | P0 |
| AI embedding | 无 | 无 | 无 | 完全自主（pgvector） | P1 |
| 打印模板 | 配置字段借鉴 | — | Jinja2 思路 | Go template + JSONB | P2 |

---

## 9. AI 增强字段设计

### 9.1 商品智能

```sql
-- product 表 AI 字段
embedding           vector(1536)    -- OpenAI/Kova 生成的商品描述向量
                                    -- 用途: 语义搜索、相似商品推荐
ai_metadata         JSONB           -- {
                                    --   "tags": ["电子","配件"],
                                    --   "auto_category": "...",
                                    --   "last_embedded_at": "..."
                                    -- }
predicted_monthly_demand  NUMERIC   -- Kova Agent 预测的月均需求
predicted_stockout_at     TIMESTAMPTZ -- 预测缺货时间点
recommendation_notes      TEXT      -- Kova Agent 的补货建议文本
```

**使用场景**:
- 搜索商品时用向量相似度（`embedding <=> query_vector < 0.3`）替代 LIKE
- Kova Agent 定期扫描 `stock_snapshot.available_qty < product.predicted_monthly_demand/4` → 生成补货建议

### 9.2 供应商/客户智能

```sql
-- partner 表 AI 字段
ai_metadata  JSONB  -- {
                    --   "risk_score": 0.3,      // 违约风险
                    --   "preferred_payment_terms": "net30",
                    --   "historical_avg_lead_days": 7.5
                    -- }
```

### 9.3 单据智能

```sql
-- bill_head 表 AI 字段（不加 embedding，单据一次性）
-- 通过 ai_metadata 存 Kova Agent 的分析结果
-- 示例: {"anomaly_detected": true, "reason": "单价异常高 +120%"}
```

### 9.4 库存预测物化视图

```sql
-- 供 Kova Agent 消费的预测数据视图
CREATE VIEW ai_reorder_suggestions AS
SELECT
    ss.tenant_id,
    p.id          AS product_id,
    p.name        AS product_name,
    ss.available_qty,
    p.predicted_monthly_demand,
    p.predicted_stockout_at,
    si.low_safe_qty,
    GREATEST(0, COALESCE(si.low_safe_qty, 0) * 2 - ss.available_qty) AS suggested_order_qty,
    p.recommendation_notes
FROM stock_snapshot ss
JOIN product p ON p.id = ss.product_id
LEFT JOIN stock_initial si ON si.product_id = ss.product_id AND si.warehouse_id = ss.warehouse_id
WHERE p.predicted_stockout_at < now() + interval '30 days'
   OR ss.available_qty < COALESCE(si.low_safe_qty, 0);
```

---

## 10. 法律操作清单

### 10.1 必须保留的 LICENSE 文件

在 `2b-svc-psi/` 根目录创建:

```
2b-svc-psi/
├── LICENSE                   ← Lurus Tally 自身的 License（商业或 Apache-2.0）
└── THIRD_PARTY_LICENSES/
    ├── jshERP-LICENSE        ← Apache-2.0 原文（从 jshERP 仓库复制）
    └── GreaterWMS-LICENSE    ← Apache-2.0 原文（从 GreaterWMS 仓库复制）
```

**jshERP LICENSE 原文地址**: https://github.com/jishenghua/jshERP/blob/master/LICENSE  
**GreaterWMS LICENSE 原文地址**: https://github.com/GreaterWMS/GreaterWMS/blob/main/LICENSE

### 10.2 NOTICES 文件

创建 `2b-svc-psi/NOTICES.md`:

```markdown
# Third-Party Software Notices

This product includes software derived from the following open-source projects:

## jshERP (管伊佳 ERP)
- Source: https://github.com/jishenghua/jshERP
- License: Apache License 2.0
- Copyright: Copyright (c) jishenghua and contributors
- Usage: Database schema design for product, warehouse, billing, and finance modules.
  Fields have been renamed, restructured for PostgreSQL, and extended with
  multi-tenancy (RLS), UUID primary keys, and AI-specific fields.

## GreaterWMS
- Source: https://github.com/GreaterWMS/GreaterWMS
- License: Apache License 2.0
- Copyright: Copyright (c) GreaterWMS contributors
- Usage: Data model concepts for ASN (advance shipping notice), bin/location
  management, stock status subdivision (available/reserved/damage/hold),
  and cycle count workflows.
```

### 10.3 Go 源文件头注释模板

对于直接从 jshERP 表结构派生的 Go struct 文件：

```go
// Copyright (c) 2026 Lurus Platform. All rights reserved.
//
// This file contains data models derived from jshERP
// (https://github.com/jishenghua/jshERP), licensed under Apache 2.0.
// Original structure has been modified for PostgreSQL, multi-tenancy via RLS,
// UUID primary keys, and AI-extended fields.
// See THIRD_PARTY_LICENSES/jshERP-LICENSE for full license text.
```

### 10.4 Apache-2.0 合规要求核查清单

- 保留原始版权声明（NOTICES 文件中已包含）
- 保留 LICENSE 文件原文（THIRD_PARTY_LICENSES/ 目录）
- 不得使用 jshERP/GreaterWMS 名称为 Lurus Tally 背书（不在产品名/商标中使用）
- 修改部分（重命名字段/添加 AI 字段/PostgreSQL 适配）的版权归属 Lurus
- 若 Lurus Tally 以商业闭源方式分发：Apache-2.0 允许，无需开源衍生代码

---

## 11. 风险与待确认项

### 已知风险

| 风险 | 描述 | 缓解措施 |
|---|---|---|
| WAC 全量扫描性能 | jshERP 的成本价重算要扫描该商品全量历史 depot_item | 改为增量更新（仅在入库时重算，用 snapshot 加速）|
| `sn_list` 字符串设计 | jshERP 序列号用逗号分隔字符串存在 depot_item 中，无法 FK 约束 | Lurus 改为 `serial_nos TEXT[]` + stock_serial 独立表 |
| 逗号分隔字段 | `sales_man`, `account_id_list` 等字段用逗号分隔多值 | Lurus 改为 PostgreSQL 数组字段 |
| 多货币未支持 | jshERP 无多货币设计 | 预留 `currency_code` 字段，默认 CNY |
| BOM 生产未覆盖 | jshERP 组装/拆卸单无 BOM 展开，原材料消耗需手工填 | Phase 2 引入 BOM 表，暂不纳入 P0 |

### 待确认项（Unknowns）

1. **pgvector 版本**: 生产集群 PostgreSQL 是否已安装 pgvector 扩展（需确认 K3s 中 postgres:16 镜像是否含 pgvector）。
2. **FIFO 是否 P0 需求**: 若客户有食品/医药行业，FIFO 是合规必须；若只做一般贸易，WAC 够用。需产品确认。
3. **多货币需求**: 是否需要在 P0 支持外币结算（如 USD/EUR），影响 `unit_price` 字段设计。
4. **GreaterWMS 货位模块**: bin 管理适合有仓库扫码器的客户，若目标客户是小微商贸（无专职仓管），货位模块可能是 overengineering。
5. **财务对接**: 是否需要自动生成财务凭证（对接用友/金蝶）？若需要，OFBiz 的 GlAccount 设计必须引入，影响 `payment_head` 表结构。
6. **`2b-svc-psi` 服务是否已存在**: 当前 `lurus.yaml` 中无此服务条目，需在 `lurus.yaml` `capabilities:` 中注册。

---

## 12. 引用

| 资源 | URL | License |
|---|---|---|
| jshERP 源码 | https://github.com/jishenghua/jshERP | Apache-2.0 |
| jshERP SQL Schema | https://github.com/jishenghua/jshERP/blob/master/jshERP-boot/docs/jsh_erp.sql | Apache-2.0 |
| jshERP BusinessConstants | https://github.com/jishenghua/jshERP (本地克隆读取) | Apache-2.0 |
| jshERP DepotItemService | https://github.com/jishenghua/jshERP (本地克隆读取) | Apache-2.0 |
| GreaterWMS 源码 | https://github.com/GreaterWMS/GreaterWMS | Apache-2.0 |
| GreaterWMS models | https://github.com/GreaterWMS/GreaterWMS (本地克隆读取) | Apache-2.0 |
| Apache OFBiz | https://github.com/apache/ofbiz-framework | Apache-2.0 |
| MedusaJS v2 | https://github.com/medusajs/medusa | MIT |
| InvenTree | https://github.com/inventree/InvenTree | MIT |
| pgvector | https://github.com/pgvector/pgvector | MIT |
