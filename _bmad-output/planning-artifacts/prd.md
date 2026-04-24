# Lurus Tally — Product Requirements Document

> Version: 2.0 | 日期: 2026-04-23 | 状态: Draft
> Owner: Lurus Product Team | 服务目录: `2b-svc-psi`
> 前版本备份: `_archive/prd-v1-cross-border-only-2026-04-23.md`

---

## 1. Product Vision

### 1.1 一句话定位

**Lurus Tally 是一款 AI-native 进销存底座，单产品覆盖"村口五金店"到"跨境电商企业"两个极端场景 —— 通过行业 Profile 机制切换，而非两套产品。**

### 1.2 为什么是双场景

v1 PRD 聚焦跨境企业，忽视了国内数量更大的本地零售客群（五金/建材/百货）。两个场景有同一个核心痛点：**库存核算和进出货记录完全依赖人工或 Excel，管理混乱、数据失真**。

差异只在规模、节奏和操作习惯，而不在数据模型本质：

- **跨境企业**：标准 SKU、多币种、较长供应链、复杂审批流，IT 能力强，容忍学习曲线。
- **五金/本地零售**：长尾 SKU、散装计量、柜台秒杀式交易，完全不容忍复杂系统，断网还要能卖货。

行业 Profile 机制通过切换 UI 布局、字段可见性、默认值和工作流节点，用一套代码服务两个场景。

### 1.3 六个月成功指标

| 指标 | 目标值 | 备注 |
|------|-------|------|
| Lighthouse 客户数（跨境） | 3+ | M3 内部 dogfood |
| Lighthouse 客户数（零售） | 5+ | M3 内部 dogfood，五金/百货 |
| Stage 付费客户（总计） | 30+ | M6 MVP β；双场景并计 |
| AI 助手日活查询 | ≥ 每用户 2 次/天 | ⌘K + Drawer 触发总数 |
| 补货建议采纳率 | ≥ 40% | Kova Agent 建议被执行的比例 |
| 月客户留存率 | ≥ 85% | 付费续费留存 |
| 零售上手时间 | < 5 分钟 | 五金店店主完成第一笔出货 |
| 跨境首次开单时间 | < 10 分钟 | 新用户注册到完成第一张销售单 |

---

## 2. Strategic Context

### 2.1 与 Lurus 既有体系的关系

Tally 是 Lurus Platform 产品组（P0）第五个成员，深度复用已有基础设施：

| Lurus 能力 | Tally 使用方式 | 节省建设成本 |
|-----------|--------------|-------------|
| **2l-svc-platform** | 账户/钱包/订阅/权益，调 `/internal/v1/...` (bearer key) | 6-12 个月 |
| **2b-svc-api (Hub)** | 所有 LLM 调用走 Hub 网关，Hub 负责路由/计量/熔断 | 3-6 个月 |
| **2b-svc-kova** | Kova Agent 引擎执行补货分析、滞销预警等 Agent 任务 | 6-9 个月 |
| **2b-svc-memorus** | RAG 历史销量/客户偏好上下文，提升 AI 预测精度 | 3-6 个月 |
| **Zitadel (OIDC)** | 单点登录，复用 auth.lurus.cn | 2-3 个月 |
| **PostgreSQL lurus-pg-rw** | 共用集群，`tally` schema，Row-Level Security 多租户 | 1-2 个月 |
| **NATS** | 新增 `PSI_EVENTS` stream，与 IDENTITY_EVENTS/LLM_EVENTS 并列 | 1 个月 |

**产品组归属**: Platform (P0)。订阅收入、AI 调用量、客户数据纳入 Platform 产品线统一管理。

### 2.2 市场切入点

| 竞品 | 他们的护城河 | Tally 的切入 |
|------|------------|------------|
| **管家婆** | 本地服务网络、断网开单 | AI Agent 补货决策、顶级 UX、边缘节点离线支持 |
| **速达** | 制造业生产模块覆盖 | Web-first 更快上手、AI 异常检测 |
| **简道云 PSI** | 零代码自定义 | 垂直深度（批次/序列号/散装计量）、Kova Agent |
| **收钱吧/美团收银** | POS 闭环生态 | 进销存数据深度、企业级多仓库管理 |
| **Cin7** | 100+ 渠道集成 | 中文语境、国内合规、Lurus Hub 计费体系 |

Tally 的稀缺组合：**行业 Profile 双场景 + AI Agent + 边缘离线 + Linear 级 UX**，三者同时具备在国内是空白。

---

## 3. Personas

### 3.1 Persona A — 跨境企业（Profile: cross_border）

**代表用户：老板娘 李青 (决策者 + 主付费人)**

| 维度 | 详情 |
|------|------|
| 典型规模 | 10-200 人，抖店 + 批发双渠道 |
| SKU 数量级 | 千级，标准化（服装/电子/家具）|
| 商品标识 | 条码 + HS Code + 多语言描述 |
| 计量单位 | 件 / 箱 / 托，单位换算标准 |
| 单笔交易耗时 | 分钟到小时（拣货 + 审核 + 发货）|
| 流程节点 | 订单 → 拣货 → 出库 → 海运 → 清关 → 签收 |
| 财务模式 | 多币种、汇兑损益、应收账期 30-90 天 |
| 客户关系 | B2B 长期合同，客户档案完整 |
| 库存盘点 | 周期性 ABC 分类，每月一次 |
| IT 能力 | 中-高，有 ERP 使用经验 |
| 网络条件 | 稳定云端，无离线需求 |
| 客单价 | 千-万级 RMB / 单 |
| 付费容忍 | ¥500-5000/月 SaaS |
| 关键 AI 痛点 | 跨境补货预测、动态定价、多渠道库存分配 |

**典型 User Journey（跨境）**:

1. 早上 9 点打开 Dashboard → Kova Agent 推送"SKU-023 预计 18 天后断货，建议补货 500 件"
2. 查看 AI 分析依据（近 90 天销量趋势 + 当前库存）→ 采纳建议 → 跳转预填采购单
3. 采购单审核（2 级审批）→ 发给供应商
4. 货到仓库 → 扫码入库 → WAC 成本自动重算
5. 月底 → AI Drawer 查"本月各币种应收汇总" → 导出对账单给会计

**辅助角色（跨境场景）**:
- **仓管老赵** — 每日 30-80 张入/出库单，条码枪 HID 模式，批次管理
- **业务员小王** — 外勤开单，实时查库存，移动端查询
- **财务张会计** — 月末对账，多币种汇兑核对，应收台账导出

---

### 3.2 Persona B — 五金/本地零售（Profile: retail）

**代表用户：五金店老板 周大明 (决策者 + 主操作人)**

| 维度 | 详情 |
|------|------|
| 典型规模 | 1-5 人，夫妻店或小团队 |
| SKU 数量级 | 万级长尾，规格散乱（M6×20 螺丝 vs M6×25 螺丝）|
| 商品标识 | 多数无条码，店主自命名（"6分管"、"加厚款"）|
| 计量单位 | 件 + 斤 + 米 + 包 + 散装混用 |
| 单笔交易耗时 | 5-30 秒（柜台直接交易）|
| 流程节点 | 选货 → 报价 → 收钱 → 出库（一步到位）|
| 财务模式 | 现金 / 赊账 / 微信扫码，当日结，可能不开票 |
| 客户关系 | 散客 + 熟客赊账，无正式合同 |
| 库存盘点 | 高频零碎，目测 + 边卖边记 |
| IT 能力 | 低，店主一人，5 分钟必须能上手 |
| 网络条件 | 农村/偏远店面可能断网，断网必须能继续收银 |
| 客单价 | 个位-百级 RMB / 单 |
| 付费容忍 | ¥99-300/年 或买断 |
| 关键 AI 痛点 | 智能记忆"上次这个客户买了什么"、规格混乱时模糊匹配商品、月报替店主整理 |

**典型 User Journey（五金店零售）**:

1. 客户进店 → 店主打开 PWA → 报价模式（极简界面，无菜单干扰）
2. 语音/输入"M6×20 螺丝 2 包"→ AI 模糊匹配 → 确认商品
3. 选数量（件/散装按克/米）→ 显示总价 → 收款（现金/微信）
4. 一键出库 → 打印小票（可选）
5. 熟客赊账 → 绑定客户档案 → 挂账记录
6. 断网时 → 本地 PWA 继续出货 → 网络恢复后自动同步

**辅助角色（零售场景）**:
- **老板娘/帮手** — 操作进货入库，更新售价
- **记账员（兼职）** — 月底查赊账流水，核对应收

---

### 3.3 混营 Profile（Profile: hybrid）

适用：同时做线下门店 + 跨境出口的中型企业（如建材商、工业品经销商）。
- 展示两个 Profile 的功能并集
- UI 密度介于两者之间
- 跨境字段（HS Code、汇率）默认收起但可用
- POS 模式和审批流同时存在

---

## 4. Profile 机制设计

### 4.1 什么是 Profile

Profile 是租户创建时选择的场景标签，值为 `cross_border` / `retail` / `hybrid`。一旦选定，Profile 影响以下层面：

| 层 | cross_border | retail | hybrid |
|----|-------------|--------|--------|
| **DB 默认值** | `measurement_strategy=individual`, `currency_code=USD/CNY` | `measurement_strategy=weight/volume`, `currency_code=CNY` | 同 cross_border |
| **商品表单** | 显示 HS Code、原产地、多语言名称字段 | 隐藏上述字段，突出散装单位和规格属性 | 全部可见，高级字段折叠 |
| **销售流程** | Stepper 多步（选客户→添明细→审核→出库）| 单屏 POS 模式（选货+付款+出库一步完成）| 两种模式均可切换 |
| **财务字段** | 多币种、汇率录入、账期天数 | 现金/赊账/微信，当日结 | 全部 |
| **报表默认** | 多币种汇总、清关状态、渠道分配 | 日营业额、赊账余额、散货消耗 | 全部报表 |
| **AI 提示词模板** | 补货预测（含 lead time、汇率）| 熟客记忆、规格模糊匹配 | 自动识别查询类型 |
| **离线模式** | 不支持（默认在线）| 必须支持（PWA + 本地 SQLite）| 可选 |

### 4.2 Profile 不影响的部分

- **数据库核心表结构** —— 完全共享，`product`、`stock_snapshot`、`bill_head` 等表在所有 Profile 下字段完全相同，只有可见性和默认值不同。
- **权限系统** —— RBAC 四角色（管理员/仓管/业务/只读）对所有 Profile 通用。
- **AI 基础设施** —— Hub、Kova、Memorus 所有 Profile 共享调用链路。

### 4.3 Profile 切换规则

- 租户创建后可在 90 天内免费切换一次 Profile。
- 切换不删除数据，仅改变展示层和 AI 模板。
- 90 天后切换视为产品升级，走订阅变更流程。

---

## 5. 商品模型设计

### 5.1 SKU vs Variant 决策

**M6×20 螺丝 vs M6×25 螺丝 = 1 SPU + 2 SKU（不是 2 个独立商品）**

理由：两者共享商品名称、分类、供应商，只有规格属性不同。用 `attributes JSONB` 区分：

```
product (SPU): "六角螺丝"
  └── product_sku (SKU-001): attributes = {"规格": "M6×20", "材质": "镀锌"}
  └── product_sku (SKU-002): attributes = {"规格": "M6×25", "材质": "镀锌"}
```

对于五金店，长尾 SKU 过多时允许"快速 SKU"模式：商品即 SKU，不定义 SPU 层，一个商品条目直接对应库存。

### 5.2 计量策略 (measurement_strategy)

| 策略 | 场景 | 示例 |
|------|------|------|
| `individual` | 标准件，一件一件计数 | 手机、箱包、服装 |
| `weight` | 按重量散装出售 | 螺丝散装（克/斤/千克）、散粮 |
| `length` | 按长度出售 | 钢管（米）、电缆（米）|
| `volume` | 按体积出售 | 液体（升/毫升）|
| `batch` | 按批次管理（食品/医药）| 批号 + 有效期 |
| `serial` | 序列号管理（贵重品）| 手机 IMEI、设备 SN |

同一个 SKU 只能有一种 `measurement_strategy`，但可以有多个销售单位（`alt_units[]`）：

```
base_unit: 克
alt_units: [
  { unit: "斤", ratio: 500 },    // 1 斤 = 500 克
  { unit: "千克", ratio: 1000 }  // 1 千克 = 1000 克
]
```

### 5.3 attributes JSONB 规范

`product_sku.attributes` 存储任意扩展属性：

| 场景 | attributes 示例 |
|------|----------------|
| 跨境合规 | `{"hs_code": "7318.15", "origin": "CN", "material": "不锈钢"}` |
| 五金规格 | `{"规格": "M6×20", "材质": "镀锌", "强度等级": "8.8"}` |
| 服装 | `{"color": "蓝色", "size": "XL", "season": "2026春夏"}` |
| 通用 | 任意 key-value，无 schema 限制 |

---

## 6. 核心场景与 User Stories

> 每个 Story 标注适用 Profile：`[CB]` = cross_border | `[R]` = retail | `[Both]` = 两者通用

### 模块 1: 账户与多租户 [Both]

- **US-1.1** [Both] As a 新用户, I want 用 Zitadel OIDC 注册并选择行业 Profile（跨境企业 / 本地零售 / 混营）, so that 系统自动配置适合我业务的默认布局和字段。
  - 验收: 注册后选 Profile → `tenant_profile.profile_type` 写入 → UI 布局立即切换；PostgreSQL RLS 策略激活；跨租户查询返回空集。
- **US-1.2** [Both] As a 企业管理员, I want 邀请团队成员并分配角色（管理员/仓管/业务/只读）, so that 权限精确控制。
  - 验收: 仓管角色无法访问财务台账；业务角色无法修改商品成本价；角色限制在所有 Profile 下一致。
- **US-1.3** [R] As a 零售店主, I want 在向导中选择"本地零售"后看到极简引导（只需填店名和第一个商品）, so that 5 分钟内完成初始化开始收银。
  - 验收: 零售向导 2 步完成（店名 + 快速添加商品）；商品可以无条码；完成后直接进入 POS 模式首页。
- **US-1.4** [CB] As a 跨境企业管理员, I want 在向导中完成仓库配置、多币种设置和团队邀请, so that 系统在第一周就能支撑真实业务运营。
  - 验收: 跨境向导 4 步（公司信息 + 仓库 + 币种 + 邀请成员）；完成后显示演示数据引导 CTA。

---

### 模块 2: 商品与 SKU [Both]

- **US-2.1** [Both] As a 管理员, I want 创建商品并定义 measurement_strategy 和 alt_units, so that 系统按正确方式计量库存和开单。
  - 验收: 创建时必选计量策略；weight/length/volume 策略下必填 base_unit；alt_units 最多 5 个换算单位。
- **US-2.2** [Both] As a 管理员, I want 通过 attributes JSONB 为商品 SKU 添加任意扩展属性, so that 五金规格或跨境合规字段都能自由录入。
  - 验收: attributes 支持任意 key-value；UI 提供常用属性模板（跨境：HS Code/原产地；五金：规格/材质）；保存后全文检索可命中 attributes 内容。
- **US-2.3** [CB] As a 管理员, I want 为 SKU 录入 HS Code 和原产地, so that 跨境清关和合规检查有据可查。
  - 验收: HS Code 字段 10 位数字校验；原产地下拉（ISO 国家代码）；缺少 HS Code 的 SKU 在出口销售单审核时显示警告。
- **US-2.4** [Both] As a 管理员, I want 批量导入商品（Excel 模板）, so that 从旧系统迁移时不必逐条录入。
  - 验收: 模板含 Profile 对应的默认字段；导入 500 行 < 5s；错误行标红显示，正确行入库。
- **US-2.5** [Both] As a 仓管, I want 用条码枪扫码定位 SKU, so that 开单和盘点时不必手动搜索。
  - 验收: HID 键盘模式（无 SDK），焦点自动捕获，扫码到定位 SKU < 300ms。
- **US-2.6** [Both] As a 管理员, I want 为商品设置安全库存阈值, so that 低库存时系统自动预警。
  - 验收: 库存量 < 安全库存 50% 显示黄色预警；< 20% 显示红色紧急预警；Dashboard 待办卡片聚合。
- **US-2.7** [CB] As a 管理员, I want 为批次管理商品录入批号和有效期, so that 追溯和先进先出合规。
  - 验收: 批次管理商品入库时批号/有效期必填；出库时系统建议最近到期批次优先。
- **US-2.8** [CB] As a 管理员, I want 为贵重商品开启序列号管理, so that 每件商品可追溯到具体买家。
  - 验收: 序列号在出库单中一一对应；查询序列号可定位到对应销售单和买家。
- **US-2.9** [R] As a 零售店主, I want 快速添加商品（只填名称和价格，规格可后补）, so that 刚进货时不被繁琐表单拖慢。
  - 验收: retail profile 下商品创建最少只需商品名 + 售价两个字段；其余字段可后续补录；快速模式跳过 SKU 矩阵。

---

### 模块 3: 仓库与库存 [Both]

- **US-3.1** [Both] As a 管理员, I want 创建多个仓库并定义各仓库的 SKU 库存量, so that 多仓库分开管理。
  - 验收: 同一 SKU 在不同仓库有独立库存；库存合计视图可选"全仓"或"单仓"。
- **US-3.2** [Both] As a 仓管, I want 查看库存实时六状态（在手/可用/预占/在途/损坏/冻结）, so that 知道哪些库存可销售。
  - 验收: 可用库存 = 在手 - 预占；销售单确认后自动更新预占；入库确认后更新在手。
- **US-3.3** [CB] As a 管理员, I want 预留多渠道库存视图（v1 UI 展示，v2 接 API）, so that 未来多平台接入有数据基础。
  - 验收: 库存表有 `channel_id` 字段（v1 默认 `default`）；UI 预留"渠道"筛选列（v1 禁用但可见）。
- **US-3.4** [R] As a 零售店主, I want 查看简化库存列表（商品名/数量/单位/预警状态）, so that 不被六状态复杂度困扰。
  - 验收: retail profile 下默认只显示"在手"和"可用"两列；六状态数据仍在数据库完整维护，仅 UI 简化；高级用户可切换完整视图。

---

### 模块 4: 采购流程

- **US-4.1** [CB] As a 采购员, I want 创建采购单（选供应商 → 添加 SKU 明细 → 确认提交）, so that 正式发起采购流程。
  - 验收: Stepper 三步；步骤 2 行内编辑数量/单价；草稿自动保存；提交后生成 `PO-YYYYMMDD-XXX` 编号。
- **US-4.2** [Both] As a 仓管/店主, I want 确认采购入库（扫码 + 实收数量）, so that 库存自动增加并生成入库单。
  - 验收: 入库确认后，对应 SKU `on_hand` 增加；WAC 成本自动重算（按 measurement_strategy 选对应算法）。
- **US-4.3** [CB] As a 财务, I want 查看每张采购单的应付款状态（未付/部分付/已结清）并录入多次付款, so that 控制付款节奏。
  - 验收: 采购单详情显示应付金额/已付金额/剩余应付；支持录入付款记录（金额/日期/备注）。
- **US-4.4** [R] As a 零售店主, I want 快速进货（只填供应商和商品数量，不需要审批），so that 拿货时现场录入，不误事。
  - 验收: retail profile 下进货单无审批步骤；两步完成（选商品 + 填数量）；进货后库存立即更新。
- **US-4.5** [Both] As a 采购员/店主, I want 对已审核采购单进行红冲, so that 纠正错误而不破坏数据完整性。
  - 验收: 红冲后原单状态变"已冲销"；自动生成对应红字入库单反冲库存；操作记录写入审计日志。

---

### 模块 5: 销售流程

- **US-5.1** [CB] As a 业务员, I want 创建销售单（选客户 → 添加 SKU + 数量 + 价格 → 审核 → 出库）, so that 完成一笔 B2B 销售。
  - 验收: 销售单创建时校验 `available` 库存充足；超库存时阻止出库确认但允许保存草稿。
- **US-5.2** [R] As a 零售店主, I want 在 POS 模式下快速收银（选货 → 付款 → 出库一步完成）, so that 30 秒内完成一笔柜台交易。
  - 验收: POS 界面不含导航菜单和侧边栏；商品搜索支持模糊匹配（"6分管"匹配"六分管"）；付款确认后库存立即扣减；支持现金/微信/赊账三种付款方式一键选择。
- **US-5.3** [R] As a 零售店主, I want 对熟客赊账并随时查看该客户总欠账, so that 月底对账有清晰记录。
  - 验收: 赊账时绑定客户档案；客户页面显示总欠账金额/最近交易/明细列表；AI Drawer 支持"老张现在欠我多少钱"自然语言查询。
- **US-5.4** [Both] As a 业务员/店主, I want 在销售单中设置折扣, so that 灵活定价不同客户。
  - 验收: 行级折扣（折扣率或折扣金额）和单据级整体折扣均支持；折后金额实时汇总。
- **US-5.5** [CB] As a 财务, I want 查看每张销售单的应收款状态, so that 催款有据可查。
  - 验收: 应收台账按客户汇总；支持录入收款记录；超期应收自动标红（可配置超期天数）。
- **US-5.6** [Both] As a 财务/店主, I want 对销售单进行红冲（销售退货）, so that 处理客户退货时账面正确。
  - 验收: 红冲生成退货入库单，库存恢复；对应应收自动抵减；审计日志记录操作人。
- **US-5.7** [CB] As a 业务员, I want 在销售单中录入跨境物流信息（运单号/运输方式/预计到港时间）, so that 追踪海运状态。
  - 验收: 销售单有"物流"子面板；运单号录入后显示运输方式和状态；v1 只录入，v2 对接物流 API 自动查询。

---

### 模块 6: 调拨与盘点 [Both]

- **US-6.1** [Both] As a 仓管/店主, I want 发起跨仓库调拨单, so that A 仓库存可转移至 B 仓。
  - 验收: 调拨单确认后 A 仓 `on_hand` 减少，B 仓 `on_hand` 增加；调拨过程中显示"在途"状态。
- **US-6.2** [CB] As a 仓管, I want 发起整仓盘点（含循环盘点模式）, so that 不关仓也能定期核实库存。
  - 验收: 可选"整仓盘点"或"循环盘点（按类别）"；实盘录入差异自动计算；差异单审核后库存调平。
- **US-6.3** [R] As a 零售店主, I want 快速盘点（扫码 + 输入实际数量），盘完立即生效不需要审批, so that 不打断日常营业。
  - 验收: retail profile 下盘点单跳过审批步骤，确认后立即调平库存；操作日志仍记录差异和调平原因。

---

### 模块 7: 财务台账

- **US-7.1** [CB] As a 财务, I want 查看多币种应收台账（按客户/币种/时间分组）, so that 掌握各币种回款情况。
  - 验收: 台账按客户 + 币种分组；显示原币金额和按当日汇率折算的人民币等值；超期金额高亮。
- **US-7.2** [R] As a 零售店主, I want 查看简化赊账台账（客户姓名/欠款金额/最近交易）, so that 月底能快速对账。
  - 验收: retail profile 下财务页面默认展示"赊账总览"；按客户排序；支持标记"已还清"（一键操作）。
- **US-7.3** [Both] As a 财务/店主, I want 查看资金账户余额和收支流水, so that 对现金账户有基本了解。
  - 验收: 支持多个资金账户（银行账户/支付宝/微信/现金）；每笔收付款关联到对应单据。
- **US-7.4** [CB] As a 财务, I want 录入汇兑损益（外汇结汇时汇率差），so that 财务数据准确反映真实盈亏。
  - 验收: 支持录入结汇汇率和结汇金额；自动计算汇兑损益（=结汇金额 × (结汇汇率 - 记账汇率)）；损益纳入财务报表。

---

### 模块 8: 报表

- **US-8.1** [Both] As a 老板/店主, I want 查看仪表盘（当日/月销售额/毛利率/库存价值/欠款总额）, so that 每天 5 秒看懂业务状态。
  - 验收: cross_border 显示多币种汇总 + 月环比；retail 显示今日营业额 + 赊账总余额；Dashboard 首屏 LCP < 1.5s。
- **US-8.2** [CB] As a 老板, I want 查看库存周转率报表（按商品/类别）, so that 找出资金占压最严重的品类。
  - 验收: 支持自定义时间范围；周转率 = 出库成本 / 平均库存价值；支持 Excel 导出。
- **US-8.3** [CB] As a 老板, I want 查看 ABC 分析（按销量/销售额分三档）, so that 聚焦高价值 SKU。
  - 验收: A 类（前 20% SKU 贡献 80% 销售额）；B 类；C 类；支持按时间段重算。
- **US-8.4** [Both] As a 老板/店主, I want 查看滞销预警报表（超过 N 天未动库存的 SKU）, so that 主动清库减损。
  - 验收: 阈值可配置（默认 30 天）；列表显示当前库存量/库存金额/最后出库时间。
- **US-8.5** [R] As a 零售店主, I want 查看日报（今日进货金额/出货金额/收款/赊账新增）, so that 每天关店前核对当日流水。
  - 验收: retail profile 下 Dashboard 包含"今日日报"卡片；数据截止当前时间实时计算；支持打印。
- **US-8.6** [CB] As a 财务, I want 查看散装商品消耗报表（按重量/长度/体积维度汇总），so that 掌握散货成本。
  - 验收: 对 `measurement_strategy` 为 weight/length/volume 的 SKU 单独汇总；出库单位和基础单位均展示。

---

### 模块 9: AI 助手 (Hub + Kova) [Both]

- **US-9.1** [Both] As a 任何用户, I want 用 ⌘K 唤起自然语言查询 + AI Drawer 获取答案, so that 不用记报表路径也能得到数据洞察。
  - 验收: ⌘K 打开 Command Palette；AI Drawer 流式返回表格+分析；Hub API P95 响应 < 3s（含流式首字节）。
- **US-9.2** [CB] As a 老板, I want Kova 补货 Agent 推送补货建议（带预测依据和预计断货天数）, so that 不用自己分析销量就能决策。
  - 验收: Kova Agent 每日 09:00 运行；建议以"待办卡片"出现在 Dashboard；用户可采纳（跳转预填采购单）/ 忽略 / 暂缓；v1 仅建议，不自动提交。
- **US-9.3** [Both] As a 老板/店主, I want 收到滞销预警推送（Dashboard 卡片）, so that 及时处置积压。
  - 验收: Kova Agent 每日运行；滞销 SKU 推送 Dashboard 卡片；AI 给出处置建议（降价/调拨/退供）。
- **US-9.4** [R] As a 零售店主, I want AI 识别"老张上次买了什么"并在开单时提示, so that 服务熟客更高效。
  - 验收: AI Drawer 支持"老张上次来买了什么"自然语言查询；基于 Memorus 客户偏好上下文；返回最近 3 次购买记录。
- **US-9.5** [R] As a 零售店主, I want AI 模糊匹配规格混乱的商品名称（"6分管"→"六分管"，"M6×20"→"6号20mm螺丝"）, so that 找货时不被叫法差异卡住。
  - 验收: POS 搜索框接入 AI 模糊匹配；非精确匹配时给出候选列表（Top 3）并标注匹配依据；用户确认后进入开单流程。

---

### 模块 10: 离线 / 边缘节点 [R]（V2 实现，V1 预留接口）

> 本模块 V1 不实现，但数据模型必须在 V1 建立，不允许后期破坏性变更。

- **US-10.1** [R] As a 零售店主, I want 在断网时继续出货收银并记录交易, so that 网络不稳定不影响营业。
  - 验收: PWA 离线模式下 POS 功能完整可用；交易写入本地 IndexedDB；恢复网络后 60s 内同步到云端；同步成功率 ≥ 99%（除冲突外）。
- **US-10.2** [R] As a 零售店主, I want 冲突时看到两条记录并选择保留哪条, so that 不会因系统自动决策丢数据。
  - 验收: 云端与本地同一时间段有冲突时，推送通知给店主；展示两条记录对比；店主选择"保留云端"或"保留本地"；选择后立即同步。
- **US-10.3** [R] As a 零售店主（边缘节点）, I want 在本地 NUC/小主机上部署 Tally Backend, so that 完全断网也能运行全部功能。
  - 验收: Backend 提供 edge 部署包；PWA 自动检测并连接本地 Backend；本地 Backend 定时（可配置间隔）向云端同步。

**离线 schema 约束（V1 必须建立）**：每张单据加字段：
- `origin`: `cloud` | `edge` — 记录单据创建来源
- `sync_status`: `synced` | `pending` | `conflict` — 同步状态
- `edge_timestamp`: 本地创建时间戳（边缘节点时间，用于冲突解决）

---

## 7. 离线优先（Edge Mode）设计

### 7.1 数据流

```
[边缘 PWA] --> [本地 IndexedDB] --> [后台 Sync Queue]
                                          |
                        (网络恢复)         |
                                          v
                              [云端 API: POST /api/v1/sync/batch]
                                          |
                                          v
                              [冲突检测: edge_timestamp vs cloud version]
                                          |
                          ┌───────────────┴───────────────┐
                      无冲突                          有冲突
                    自动合并                      推送给店主选择
```

### 7.2 冲突解决策略

| 冲突类型 | 策略 |
|---------|------|
| 同一单据两端均修改 | 以 `edge_timestamp` 较新者优先（last-write-wins by edge timestamp），同时保留两版本供店主审阅 |
| 库存数量冲突（边缘出货 vs 云端同时调整）| 强制人工选择，不自动合并（金额一致性不允许自动覆盖）|
| 客户赊账冲突 | 同库存处理，人工选择 |

### 7.3 可降级功能列表

| 功能 | 离线可用 | 必须在线 |
|------|---------|---------|
| POS 出货收银 | ✓ | — |
| 查看本地库存（edge 同步后快照）| ✓ | — |
| 赊账记录 | ✓ | — |
| 查看历史订单（本地缓存）| ✓ | — |
| 多币种汇率更新 | — | ✓（汇率数据源）|
| AI 自然语言查询 | — | ✓（Hub LLM）|
| Kova Agent 补货建议 | — | ✓（Agent 执行）|
| 云端报表 | — | ✓ |
| 发票开具 | — | ✓ |

---

## 8. 多端体验

| 端 | 实现 | Profile 适用 | 状态 |
|----|------|------------|------|
| **Web 主端** | Next.js 14 App Router | cross_border 主要端 | V1 Day 1 |
| **Web 平板模式** | 响应式同 Web | 两者均适用 | V1 Day 1 |
| **PWA 离线端** | Next.js + Service Worker + IndexedDB | retail 核心端 | V2 |
| **移动查询端** | Web 响应式（手机浏览器）| 业务员查库存/开单 | V1（只读/简单开单）|
| **边缘节点 Backend** | Docker 独立部署包 | retail edge 模式 | V2 |

---

## 9. Non-Functional Requirements

### 9.1 性能预算

| 指标 | 目标值 | 说明 |
|------|-------|------|
| LCP（首屏最大内容绘制）| < 1.5s | ux-benchmarks §3 P17 |
| 路由切换（侧边栏导航）| < 100ms 感知 | 乐观预加载 |
| 表格滚动（< 2000 行）| 60 fps | TanStack Table |
| 表格滚动（> 2000 行）| 60 fps + TanStack Virtual | Defer 到需要时 |
| AI 查询首字节响应 | < 1.5s P95 | Hub 流式输出 |
| POS 出货确认（retail 模式）| < 500ms P99 | 零售柜台不等待 |
| 采购/销售单提交 | < 1s P99 | 乐观更新后落库 |
| Excel 导入 500 行 | < 5s | |
| 条码扫码到 SKU 定位 | < 300ms | HID 键盘模式 |
| 离线同步延迟（恢复网络后）| ≤ 60s | Sync Queue 触发 |

### 9.2 离线可用性

| 指标 | 目标值 |
|------|-------|
| 断网状态核心交易可用率 | ≥ 99% |
| 离线数据持久化可靠率 | ≥ 99.9%（本地 IndexedDB 写入成功率）|
| 同步成功率（恢复网络后）| ≥ 99%（无冲突场景）|
| 冲突率 | < 1%（正常单店操作场景）|

### 9.3 上手速度（可测量 NFR）

| 指标 | 目标值 | 测量方式 |
|------|-------|---------|
| 五金店首次上手（retail profile）| ≤ 5 分钟 | 用户测试：注册 → 选 Profile → 完成第一笔出货 |
| 跨境企业首次开单 | ≤ 10 分钟 | 埋点：注册 → 第一张销售单提交 |
| 多币种汇率滞后 | ≤ 24 小时 | 汇率更新任务 SLA |

### 9.4 可用性 SLA

| 阶段 | SLA | 说明 |
|------|-----|------|
| MVP α（M1-M3）| 99% | 内部 dogfood |
| MVP β（M4-M6）| 99.5% | 付费客户开始使用 |
| GA v1（M7+）| 99.9% | 生产标准，每月停机不超过 43 分钟 |

### 9.5 安全与合规

- **多租户隔离**: PostgreSQL RLS，`app.tenant_id` 每请求通过 GORM 中间件注入；RLS 策略验证写入测试覆盖。
- **认证授权**: Zitadel OIDC，JWT 验证；RBAC 四角色；每个 API 路由强制鉴权。
- **操作审计日志**: 所有写操作写入 `audit_log` 表（who/what/when/result）；普通用户不可删除。
- **金额精度**: 所有金额字段使用 `NUMERIC(18,4)` 避免浮点误差；计量策略为 weight/volume 时小数位数可配置（精度到 0.001）。
- **数据本地化**: 所有业务数据存储在 `lurus-pg-rw`（国内服务器）；AI 调用通过 Hub 代理。
- **离线数据安全**: IndexedDB 数据在浏览器本地加密存储（SubtleCrypto AES-GCM），边缘节点 Backend 启用 TLS。
- **传输加密**: 全程 HTTPS/TLS 1.3；Traefik Ingress，证书自动续签。
- **输入校验**: 所有外部输入在 Handler 层 Zod（前端）+ Gin binding（后端）校验。

### 9.6 可扩展性基线

| 维度 | V1 上限 | V2 目标 |
|------|--------|--------|
| SKU 数 | 10,000（cross_border）/ 50,000（retail 长尾）| 200,000+ |
| 日均单据数 | 1,000（CB）/ 5,000（retail）| 50,000+ |
| 并发用户 | 20 | 100+ |
| 仓库数 | 10 | 100+ |

---

## 10. UX 原则（落地版）

| # | 原则 | 来源 | Tally 落地 |
|---|------|------|-----------|
| P1 | **Slide-over Sheet 替代全页跳转** | Linear/Stripe | 商品详情、SKU 详情、单据详情均用右侧 Sheet；背景列表仍可见 |
| P2 | **⌘K 是全局操作入口** | Raycast/Linear | 所有高频操作可从 ⌘K 触达：创建单据、查找 SKU、AI 查询、查看预警 |
| P3 | **POS 模式无干扰** | 收钱吧/美团收银 | retail profile 下 POS 界面隐藏全部导航；只露出搜索框、商品列表、付款按钮 |
| P4 | **数字右对齐 + tabular-nums + 千分位** | Stripe | 所有库存量/金额列统一规范；散装单位（克/米）保留 3 位小数 |
| P5 | **行内操作按钮 hover 时才显示** | Stripe/Shopify | 表格行 hover 浮现"编辑/确认/打印"；常态 UI 保持干净 |
| P6 | **乐观更新 + Undo Toast** | Linear/Notion | 单据状态变更立即 UI 反映；3s Undo 窗口；失败时 Toast 变红回滚 |
| P7 | **空状态主动引导** | Vercel/Supabase | retail 新用户：[快速添加第一个商品]；CB 新用户：[批量导入] + [手动添加] |
| P8 | **骨架屏优先** | Linear/Vercel | 列表首次加载用 Skeleton；长时操作用 Spinner + 进度条 |
| P9 | **侧边栏可折叠** | Vercel/Linear | 折叠 48px icon 模式；展开 220px；Framer Motion 过渡 |
| P10 | **Profile 感知的界面密度** | 新增 | cross_border 默认 Regular 密度；retail 默认 Compact + 大字体（适合老花眼店主）|

**暗黑模式**: 默认暗黑模式（Linear 风格）；shadcn/ui 2025 OKLCH 色彩空间；TailwindCSS v4。

---

## 11. AI 差异化策略

### 11.1 Hub 自然语言查询（V1 Day 1）

**技术路径**: Hub API → DeepSeek/Claude → SQL 查询结果格式化 → Markdown 输出 → Drawer 流式渲染。

**cross_border 专属查询类型**:
- "本月各货币应收汇总" / "台灯 A 款清关状态"
- "哪些 SKU 的 HS Code 未填写" / "上月补货采纳率多少"
- "美元应收和人民币应收分别是多少"

**retail 专属查询类型**:
- "老张现在欠我多少钱" / "这周卖了多少螺丝"
- "M6×20 螺丝还剩多少斤" / "今天哪个商品卖得最好"
- "上次老李来买了什么" / "帮我整理今天的流水"

**共享查询类型**:
- 库存状态、低库存预警、滞销商品、日/月报表生成

**V1 边界**: AI 不直接执行任何写操作。可建议并跳转预填表单，需用户确认提交。

### 11.2 Kova 补货 Agent（V1 建议级）

**cross_border 模式**:
1. 读取历史 90 天销量 + 当前库存 + 安全库存 + lead time 天数
2. 计算预计断货时间（基于日均销量 + 趋势）
3. 考虑 lead time 生成补货时间点建议
4. 建议写入 `agent_recommendations`，推送 Dashboard 待办卡片

**retail 模式**:
1. 读取近 30 天出货记录（周期更短，零售节奏快）
2. 检测库存低于安全水位的商品
3. 推送"XX 螺丝快卖完了，要不要补货"简化提示
4. 不涉及 lead time 分析（零售补货通常当天可拿到货）

### 11.3 Memorus 客户记忆（retail 场景核心）

**retail profile 下激活 Memorus 记录**:
- 每次出货时，向 Memorus 写入客户购买记录（客户 ID + 商品列表 + 数量 + 日期）
- AI Drawer 查询"老张"时，从 Memorus 检索最近 10 次购买记录
- POS 搜索框输入客户名后，自动显示"TA 上次买的"提示

**cross_border profile 下**:
- 同样记录，但侧重 B2B 客户偏好（采购周期、常购品类、价格敏感区间）

### 11.4 计费模式

- **基础订阅**: 走 `2l-svc-platform` 订阅体系（月付/年付）
- **定价差异**: retail profile 提供年付低价套餐（¥99-300/年）；cross_border 提供月付套餐（¥500-5000/月）
- **AI 调用按量计费**: 每次 Hub LLM 调用、Kova Agent 执行均计量；超出套餐额度扣钱包
- **边缘节点授权**: V2 时 edge 部署需要额外授权（包含在高级套餐或单独购买）

---

## 12. MVP 范围 vs V2 vs V3

### MVP V1（共享 Kernel + 双 Profile Web SaaS）

**包含**:
- cross_border + retail Profile 基础流程（进货/出货/库存/财务台账）
- Profile 机制：DB 默认值 + UI 布局切换 + AI 提示词模板
- 商品模型：`measurement_strategy` + `alt_units` + `attributes JSONB` + `origin/sync_status` 字段
- POS 模式（retail，Web 端，无离线）
- Hub 自然语言查询（双 Profile 各自查询模板）
- Kova 补货 Agent V1（建议级，双 Profile 分别触发）
- 多币种字段预留（cross_border 场景录入，V1 不做汇率换算）
- Web SaaS 主站（cloud 部署）

**不包含（Defer V2）**:
- PWA 离线模式
- 边缘节点 Backend 部署
- 离线同步与冲突解决
- 多币种汇率自动更新和汇兑损益计算
- 金税四期 ISV 集成（留接口）
- 抖店/拼多多 OAuth 库存同步（留 `channel_id` 模型）

### V2（离线 + 边缘节点 + 合规扩展）

- PWA 离线模式（IndexedDB + Service Worker）
- 边缘节点 Backend Docker 部署包
- 离线同步与冲突解决机制（包含人工选择 UI）
- 多币种汇率自动更新（接入汇率 API）
- 汇兑损益计算和报表
- 金税四期 ISV 对接（航信/百望云/诺诺选型后接入）
- 多渠道库存 API 同步（抖店/拼多多 OAuth）

### V3（高级 AI Agent）

- Kova 补货 Agent 自动执行（无需人工审核的可撤销自动草稿）
- Kova 询价 Agent（多供应商比价 → 自动推荐最优）
- 动态定价建议 Agent（基于库存水位 + 销售速度）
- 自然语言开单（语音/文字 → 直接创建单据草稿）
- hybrid profile 进一步差异化（跨境+零售混营企业的专属工作流）

---

## 13. 风险与依赖

### 13.1 双 Persona UI 复杂度风险

| 风险 | 影响 | 缓解 |
|------|------|------|
| Profile 切换导致代码分支爆炸（每个组件都要判断 Profile）| 高（维护成本翻倍）| 约束规则：Profile 切换只在 UI 层（shadcn 组件的 props 和 CSS 变量）处理；业务逻辑和 API 层不接受 Profile 参数 —— 一套 API 服务所有 Profile |
| retail 极简要求与 cross_border 功能完整性矛盾 | 中 | retail POS 界面独立路由（`/pos`），与主应用完全分离渲染；互不干扰 |
| 5 分钟上手 NFR 实际不可达 | 高 | M2 阶段招募 5 位真实五金店老板做用户测试，测试结果驱动迭代；上手时间 > 8 分钟时阻塞 V1 GA |

### 13.2 离线冲突金额一致性风险

| 风险 | 影响 | 缓解 |
|------|------|------|
| 边缘节点和云端同时修改同一库存记录，自动合并导致库存数量错误 | 极高（财务数据错误）| 库存数量和赊账金额字段**禁止 last-write-wins 自动合并**，必须人工选择 |
| 边缘节点时钟漂移，`edge_timestamp` 时间戳不可信 | 中 | Sync 时附带 NTP 校验时间戳；超过 ±5 分钟偏差的同步请求标记为"时钟警告"，由用户确认 |

### 13.3 商品模型开源基座兼容性风险

| 风险 | 影响 | 缓解 |
|------|------|------|
| jshERP 数据模型未设计 measurement_strategy 和 alt_units | 中 | 重新设计 product_sku 表，不直接抄 jshERP SKU 字段；jshERP 只借鉴单据流程模型 |
| attributes JSONB 查询性能（万级 SKU，高频搜索）| 中 | attributes 建 GIN 索引；AI 模糊匹配走 embedding 检索（V2），V1 先用 pg_trgm |

### 13.4 跨境 Profile 多币种时间表风险

| 风险 | 影响 | 缓解 |
|------|------|------|
| V1 跨境场景无汇率自动更新（只能手工录汇率）| 中（影响 CB 客户接受度）| V1 明确告知 CB 客户汇率需手工维护；V2 Q1 接入汇率 API；NFR 明确"V1 汇率滞后 ≤ 24h"需人工操作达成 |
| V2 汇率 API 费用未预估 | 低 | 选型开放汇率 API（fixer.io 或 ExchangeRate-API），月费 < $20 |

---

## 14. Roadmap

| Phase | 时间 | 核心目标 | 关键 Milestone |
|-------|------|---------|--------------|
| **MVP α（内部 dogfood）** | M1-M3 | 双 Profile 核心 CRUD + AI 查询 + 3 CB + 5 Retail Lighthouse | M3: 两个 Profile 各完成一个完整运营周期 |
| **MVP β（付费测试）** | M4-M6 | Stage 对外 + 30 付费客户（双场景各 15）+ Kova 补货上线 | M6: 月留存 ≥ 80%；retail 上手 ≤ 5 分钟用户测试验证 |
| **GA V1** | M7-M9 | tally.lurus.cn R1 正式上线 + 100 客户 | M9: 100 付费客户，ARPU ≥ ¥200（零售）/ ¥500（跨境）|
| **V2** | M10-M12 | PWA 离线 + 边缘节点 + 金税四期 + 多渠道 | M12: retail 离线可用率 ≥ 99% |

### M1-M3 工程里程碑（双 Profile 优先）

- W1-W2: 项目骨架 + Profile 机制 DB 层（`tenant_profile.profile_type` 字段 + `measurement_strategy` 枚举 + `alt_units` + `attributes` + `origin/sync_status` 字段）
- W3-W4: 商品/SKU/仓库基础 CRUD（含 retail 快速商品模式）
- W5-W6: 采购单 + 入库（CB Stepper + retail 快速进货）+ WAC 成本
- W7-W8: 销售单 + 出库（CB 流程）+ POS 模式（retail）+ 赊账
- W9-W10: 库存盘点 + 调拨 + 双 Profile 报表基础
- W11-W12: Hub AI 查询（双 Profile 查询模板）+ Kova Agent PoC + Lighthouse 客户上线

---

## 15. Out of Scope（永远不做）

| 永远不做 | 替代建议 |
|---------|---------|
| **生产 BOM / MES（物料清单/制造执行）** | 引导至 Katana MRP |
| **HR 模块（工资/考勤）** | 引导至钉钉/飞书 HR |
| **CRM 模块（线索/商机/合同）** | Tally 有客户台账（应收/赊账），但不做销售过程管理 |
| **区块链存证 / NFT** | — |
| **进销存 + 财务一体化总账（科目/凭证）** | 引导至金蝶/用友；Tally 只做应收应付台账 |
| **独立站/电商系统** | Tally 管库存，不管店铺运营 |
| **独立移动 APP** | 响应式 Web + PWA 替代；不做原生 iOS/Android |
| **零售会员积分体系** | V3 可评估；V1/V2 不做 |

---

## 16. Open Questions

| # | 问题 | 决策人 | 预计解决时机 |
|---|------|-------|------------|
| OQ-1 | pgvector 扩展在 lurus-pg-rw 是否已安装（retail AI 模糊匹配 V2 依赖）| 架构师 | Architecture 文档阶段 |
| OQ-2 | Kova Agent 调用 P95 延迟在 POS 实时场景是否 < 500ms（retail 频率更高）| 架构师 + Dev | M2 PoC 基准测试 |
| OQ-3 | retail profile 的 POS 模式：独立路由（`/pos`）还是主 Layout 的特殊状态？影响代码隔离程度 | 前端架构师 | Epic 3 开始前 |
| OQ-4 | 边缘节点 Backend 部署格式：Docker Compose 还是单二进制 + SQLite？零售店主 IT 能力决定 | PM + Dev | M4（V2 规划启动时）|
| OQ-5 | 多币种汇率 API 选型（fixer.io / ExchangeRate-API）：费用和精度对比 | PM + 采购 | M4 |
| OQ-6 | hybrid profile 的 UI 密度配置：跟随用户手动选择，还是系统根据当前操作自动切换？| PM | M3 Lighthouse 客户访谈后 |
| OQ-7 | retail Lighthouse 客户行业：五金/百货/小超市？行业差异影响 attributes 默认模板优先级 | PM | M1 周内确定 |
| OQ-8 | AI 模糊匹配商品（"6分管"→"六分管"）：V1 用 pg_trgm 够用还是必须上 embedding？延迟要求 < 200ms | 架构师 | M2 PoC 测试 |
| OQ-9 | 离线冲突人工选择 UI（V2）：是推送到主端 Web 还是在 PWA 内弹出？影响 UX 设计 | PM + 设计 | V2 规划阶段 |
| OQ-10 | 计费分层：retail ¥99/年 和 cross_border ¥500/月 是否走同一订阅 Plan，或需要 2 个 Plan ID？ | PM | Lighthouse 客户付费意愿访谈后 |

---

## 附: 核心数据模型速查（V2 扩展）

> 详细 DDL 和 Go struct 见 architecture.md。

**新增/变更字段**（相对 V1 架构文档，需由 Architecture 负责 agent 同步）:

```
tenant
  + profile: ENUM('cross_border', 'retail', 'hybrid') NOT NULL DEFAULT 'cross_border'

product_sku
  + measurement_strategy: ENUM('individual','weight','length','volume','batch','serial')
  + base_unit: TEXT              -- 基础单位 (克/米/升/件)
  + alt_units: JSONB             -- [{unit, ratio}, ...]
  + attributes: JSONB            -- HS Code/规格/材质等扩展属性

bill_head (所有单据通用主表)
  + origin: ENUM('cloud','edge') DEFAULT 'cloud'
  + sync_status: ENUM('synced','pending','conflict') DEFAULT 'synced'
  + edge_timestamp: TIMESTAMPTZ -- 边缘节点本地时间戳（UTC，用于冲突解决，对应 architecture migration 000016）
```

**库存算法可插拔注册点** (`internal/app/stock/cost_engine.go`，由 Architecture agent 定义接口):
- `FIFO` — 先进先出（cross_border 批次管理场景）
- `WAC` — 移动加权平均（默认）
- `WeightedUnit` — 按重量/长度/体积的加权平均（retail 散装场景）
- `BatchLot` — 按批次独立成本（食品/医药）
- `BulkMerge` — 散装并入（同规格散装商品合批计算）

---

*文档版本: 2.0 | 前版本: `_archive/prd-v1-cross-border-only-2026-04-23.md`*
*下一步: Architecture 负责 agent 同步新增字段 + Edge Mode 数据流 → epics.md 更新 Epic 1（Profile DB 迁移）+ 新增 Epic 12（Edge Mode V2）*
