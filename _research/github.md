# 智能进销存 (2b-svc-psi) — GitHub 开源基座选型报告

**模式**: technical  
**生成日期**: 2026-04-23  
**作者**: bmad-researcher (analyst skill)  
**适用产品**: Lurus 企业 AI 基础设施平台，新增 AI-native 进销存 (PSI) 产品线

---

## 1. Executive Summary

GitHub 上没有一个既商用友好 (MIT/Apache-2.0)、又以 Go/PostgreSQL 为核心、又功能完备的进销存开源项目——三个条件同时满足者为零。主流选项分为两类：功能完备但 License 为 GPL-3.0 的重型 ERP (ERPNext/Dolibarr/Tryton/Odoo)，以及 License 友好但领域偏电商而非进销存的现代 headless 平台 (MedusaJS/Saleor/Vendure)。Lurus 的最优路径是以 **GreaterWMS**（Apache-2.0，Python，PostgreSQL 支持，4.3k stars，活跃维护）为仓储核心借鉴其数据模型，同时以 **Corteza**（Apache-2.0，Go 后端，2.1k stars）理解低代码元模型思路，然后**从零自研** Go + PostgreSQL 的 PSI 核心服务，深度对接已有 Platform 账户/计费/通知体系，并在 AI 层集成 Hub (LLM gateway) 做智能补货和预警。

---

## 2. 方法论

### 2.1 查询关键词（全部在 GitHub + 通用搜索引擎执行）

| 类别 | 关键词 |
|------|--------|
| 英文 WMS/ERP | `inventory management`, `warehouse management system`, `WMS`, `open source ERP`, `stock management`, `point of sale` |
| 英文电商 | `headless commerce inventory`, `MedusaJS`, `Saleor`, `Vendure` |
| 中文 | `进销存`, `仓储`, `库存管理` |
| 老牌项目 | Odoo, ERPNext/Frappe, Tryton, Apache OFBiz, Dolibarr, IDempiere |
| 技术栈筛选 | `Go OR Golang inventory`, `Apache-2.0 OR MIT ERP inventory`, `multi-tenant SaaS inventory` |
| 新兴项目 | GitHub Topics: `inventory-management` (按 stars 排序), `warehouse-management-system`, `erp` |

### 2.2 筛选漏斗

- 初始候选 > 50 个项目
- 去除 stars < 500 且近 12 个月无 commit：淘汰约 30 个
- 去除纯学习/demo 项目（无 release，无 issue 讨论）：淘汰约 10 个
- 保留 13 个进入详细分析，矩阵展示 10 个核心候选

---

## 3. 候选对比矩阵

| 项目 | Stars | 最近 Commit | 语言 | License | 功能完备度 | Lurus 契合度 | 简评 |
|------|-------|------------|------|---------|-----------|-------------|------|
| **ERPNext / Frappe** | 33.1k | 活跃 (2026) | Python | GPL-3.0 | ★★★★★ | ★★☆ | 功能最全，但 GPL 强制开源修改，且 Python 栈与 Lurus 偏离 |
| **Odoo Community** | 50.3k | 活跃 (2026) | Python | LGPL-3.0 | ★★★★★ | ★★☆ | 全球最大生态，LGPL 商用有条件允许，但体量极重，运维复杂 |
| **Dolibarr** | 6.7k | 活跃 (2026) | PHP | GPL-3.0 | ★★★★☆ | ★☆☆ | PHP 栈，GPL，与 Lurus 技术栈完全不符 |
| **Tryton** | ~低 | 活跃 | Python | GPL-3.0 | ★★★★☆ | ★☆☆ | 模块化设计好，但 GitHub mirror，GPL，社区小 |
| **Apache OFBiz** | 1.0k | 活跃 | Java | Apache-2.0 | ★★★★☆ | ★★☆ | License 最友好，但 Java 技术债重，UI 陈旧，上手成本高 |
| **GreaterWMS** | 4.3k | 2025-06 | Python/JS | Apache-2.0 | ★★★☆☆ | ★★★★☆ | License 好，支持 PostgreSQL，供应链流程完整，可借鉴数据模型 |
| **MedusaJS** | 32.8k | 活跃 | TypeScript | MIT | ★★★☆☆ | ★★★☆☆ | 前端栈契合，License 好，但定位是电商而非 B2B 进销存，无采购/生产 |
| **Saleor** | 22.4k | 活跃 | Python | BSD-3 | ★★★☆☆ | ★★☆☆☆ | 多仓库多渠道强，但 Python 后端+GraphQL，PSI 深度不足 |
| **Vendure** | 6.9k | 活跃 | TypeScript | GPL-3.0 | ★★★☆☆ | ★★☆☆☆ | 前端栈契合，但核心 v3+ 改为 GPL，商用需付费商业授权 |
| **Corteza** | 2.1k | 活跃 (2026) | Go/Vue | Apache-2.0 | ★★☆☆☆ | ★★★☆☆ | Go 后端唯一选项，低代码平台而非 PSI 专用，可研究元模型 |

> stars 数据均来自 2026-04 实测 WebFetch/WebSearch，标 "~" 的为搜索结果估算。

---

## 4. 每个候选详细分析

### 4.1 ERPNext / Frappe

**GitHub**: [github.com/frappe/erpnext](https://github.com/frappe/erpnext) — 33.1k stars, GPL-3.0, Python 81%

**简介**: Frappe Technologies 出品的全功能开源 ERP，基于自研 Frappe Framework（Python + JavaScript）。覆盖采购、销售、库存、制造、财务、HR 全模块。

**架构**: MVC 架构，元数据驱动，DocType 系统允许无代码扩展。前端 desk.js + Frappe UI，PostgreSQL/MariaDB 均支持。

**优势**: 功能最完整；社区最活跃（33k stars，Forum 活跃）；支持多仓库、序列号追踪、盘点、多公司；已有成熟中国本地化模块；自带低代码报表和 workflow 引擎。

**劣势**: GPL-3.0——分发修改版必须开源，虽 SaaS 部署有"GPL SaaS 漏洞"（不触发分发条款），但作为商业 SaaS 平台在法律解读上存在灰色地带，且**Frappe 明确要求非商业才能免费使用品牌名** [(erpnext.com/license-trademark)](https://erpnext.com/license-trademark)。Python 栈与 Lurus Go 主栈不符，集成成本高。体量重，K8s 部署需要较大资源。

**适用场景**: 企业自建 ERP，或有 Python 团队的服务商。

**Lurus 适配性**: 低。GPL 风险 + 技术栈不符，不适合作为 SaaS 商业化的代码基座，可作为**领域知识参考**（学习 DocType 设计和业务流程定义）。

---

### 4.2 Odoo Community

**GitHub**: [github.com/odoo/odoo](https://github.com/odoo/odoo) — 50.3k stars, LGPL-3.0, Python 51%

**简介**: 全球最大开源 ERP 生态，700 万+ 用户，Community 版 LGPL-3.0，Enterprise 版专有。原名 TinyERP/OpenERP。

**架构**: Python/ORM + PostgreSQL（唯一支持的数据库），XML/QWeb 模板前端，OWL (Odoo Web Library) JS 框架。模块化，数万个社区模块。

**优势**: 生态无可匹敌；PostgreSQL 原生；多渠道、多仓库、批次追踪、RFID/条码、MRP 均有成熟模块；中国本地化 (l10n_cn) 完善。

**劣势**: LGPL-3.0 商用条件复杂——Community 核心可商用，但自定义模块必须和核心一起分发时保持 LGPL，同时 Enterprise 功能闭源，SaaS 竞争时面临 Odoo 官方 SaaS 正面竞争。**数据库锁定 PostgreSQL**（这是优点但也是约束）。Python 栈。运维重：Odoo 服务内存消耗大，K8s 单副本需 2GB+ RAM。

**适用场景**: 企业全功能 ERP 部署，有 Odoo 合作伙伴支持的实施场景。

**Lurus 适配性**: 低。虽然 PostgreSQL 匹配，但 Python 栈、License 模糊性、与 Odoo 官方云的竞争关系，均不适合作为 Lurus SaaS 产品的底层代码基座。可作为**竞品分析对象和功能参考**。

---

### 4.3 Dolibarr

**GitHub**: [github.com/Dolibarr/dolibarr](https://github.com/Dolibarr/dolibarr) — 6.7k stars, GPL-3.0, PHP

**简介**: 2002 年起的老牌 PHP ERP/CRM，面向中小企业，模块化设计，启用所需模块即可。

**架构**: PHP 单体，MySQL/MariaDB/PostgreSQL 均支持，Bootstrap/jQuery 前端。

**优势**: 安装简单；支持多仓库、采购、销售、POS、财务对账；有 DoliStore 插件市场。

**劣势**: GPL-3.0；PHP 技术栈（Lurus 完全不用 PHP）；UI 陈旧；AI 集成能力弱；多租户支持非原生设计，需改造。

**Lurus 适配性**: 极低。PHP + GPL，与 Lurus 技术选型完全相反，仅供了解功能范围参考。

---

### 4.4 Tryton

**GitHub**: [github.com/tryton/tryton](https://github.com/tryton/tryton)（mirror），GPL-3.0-or-later, Python

**简介**: Odoo 4.2 的 fork（2008 年），模块化设计，三层架构（client/server/DB），主库在 hg.tryton.org。

**架构**: Python Server (trytond) + PostgreSQL，支持 XML-RPC/JSON-RPC，模块粒度极细（221 个独立仓库）。

**优势**: 模块设计非常干净；有 stock、purchase、sale、account 等完整模块；GNU Health 等知名项目基于它。

**劣势**: GPL；GitHub 使用率低（各模块 stars 个位数）；社区规模小于 ERPNext/Odoo；文档不友好；中文社区几乎为零。

**Lurus 适配性**: 极低。GPL + Python + 社区冷清，不适合。

---

### 4.5 Apache OFBiz

**GitHub**: [github.com/apache/ofbiz-framework](https://github.com/apache/ofbiz-framework) — 1.0k stars, Apache-2.0, Java 67%

**简介**: Apache 基金会旗下企业级 ERP/CRM/电商平台，1999 年发布，Oracle/HP 等大型企业曾在用。支持 ERP、CRM、OMS、WMS、供应链全套。

**架构**: Java EE 架构，Groovy/Freemarker 模板，支持 PostgreSQL/MySQL/Oracle。Gradle 构建。有官方 Docker 镜像。K8s + Helm 方案存在（社区贡献，非官方 Helm chart）。

**优势**: **Apache-2.0，是所有完整 ERP 中 License 最友好的**；功能覆盖最广（OMS + WMS + CRM + 财务）；PostgreSQL 支持；有 Docker 镜像。

**劣势**: Java 技术债重（Java EE 风格，非 Spring Boot 现代架构）；UI 极其陈旧（JSP 渲染）；1k stars 表明社区活跃度低；学习曲线极陡；中文支持差。

**Lurus 适配性**: 中低。License 完美，但 Java 栈和陈旧架构意味着 fork 改造成本极高，不建议直接基于它开发；可参考其**数据模型设计**（OFBiz 的 inventory/order 模型是行业标准参考之一）。

---

### 4.6 GreaterWMS

**GitHub**: [github.com/GreaterWMS/GreaterWMS](https://github.com/GreaterWMS/GreaterWMS) — 4.3k stars, Apache-2.0, Python 58% + JS

**简介**: 面向供应链物流的开源 WMS，国内项目，支持 ASN、出库单、盘点、货位管理。2021 年发布，目前在开发基于 Rust+Python 的新框架 Bomiot (v3.0)。

**架构**: Python (Django REST Framework) 后端，Vue.js 前端。生产环境推荐 PostgreSQL 13+（开发可用 SQLite）。支持 Docker 部署，有 API 文档。

**优势**: **Apache-2.0**；PostgreSQL 原生；供应链流程完整（采购入库/ASN/出库/盘点/货位）；中文友好；4.3k stars 活跃；最近 commit 2025-06；有移动端扫码支持。

**劣势**: Python 后端（与 Lurus Go 主栈不符）；销售/采购管理相对简单，财务对账能力弱；多租户支持未明确文档化；不是 AI-native 设计；v3.0 正在重构，存在稳定性风险。

**Lurus 适配性**: 中高。**Apache-2.0 + PostgreSQL** 是最契合 Lurus 约束的组合。推荐作为**数据模型和业务流程的核心参考**，不建议直接 fork 运行（Python 栈），而是在自研 Go 服务时参照其 API 设计和数据库 schema。

---

### 4.7 MedusaJS

**GitHub**: [github.com/medusajs/medusa](https://github.com/medusajs/medusa) — 32.8k stars, MIT, TypeScript 86%

**简介**: Node.js/TypeScript 模块化电商引擎，2021 年发布，MIT License。覆盖商品、订单、支付、发货、促销，但定位是 B2C/B2B 电商，而非 B2B 进销存。

**架构**: TypeScript Monorepo (Turbo)，Medusa v2 采用模块化架构（每个领域独立模块），PostgreSQL，Node.js 运行时。

**优势**: MIT License；TypeScript 与 Lurus 前端栈完全一致；32.8k stars 社区最活跃；API-first；多渠道、多地区支持；v2 模块化设计好，可只引入 inventory 模块。

**劣势**: **定位是电商而非进销存**——没有原生采购订单 (PO)、供应商管理、库存调拨、盘点、多仓调度等 B2B PSI 核心功能；Node.js 后端（Lurus 不用）；AI 预测功能需外挂；财务对账能力为零。

**Lurus 适配性**: 中。前端方案（Next.js storefront 模板）可复用，MIT License 友好，但核心 PSI 业务逻辑无法复用。如果 Lurus PSI 有面向 C 端 / 电商卖家的终端界面，可借鉴 Medusa 前端模板和 product/order 模型。

---

### 4.8 Saleor

**GitHub**: [github.com/saleor/saleor](https://github.com/saleor/saleor) — 22.4k stars, BSD-3-Clause, Python/Django + GraphQL

**简介**: Python/Django 后端，GraphQL-only API，面向中大型电商的 headless commerce 平台。原生支持多仓库、多渠道、多货币。

**架构**: Python/Django REST + GraphQL (Ariadne)，PostgreSQL，Celery 异步任务，Redis。前端独立（React dashboard）。

**优势**: BSD-3-Clause（商用最自由）；多仓库 (multi-warehouse) 是原生功能；22.4k stars；GraphQL API 灵活；支持 webhook (160+) 和 App 生态。

**劣势**: Python 后端；定位电商而非 B2B PSI（无采购 PO、无盘点流程、无财务对账）；GraphQL 学习成本；Saleor Cloud 商业版与自部署形成竞争。

**Lurus 适配性**: 低中。多仓库模型设计值得参考，但 Python 后端和电商定位决定无法作为 PSI 基座。

---

### 4.9 Vendure

**GitHub**: [github.com/vendure-ecommerce/vendure](https://github.com/vendure-ecommerce/vendure) — 6.9k stars, GPL-3.0 (v3+), TypeScript/NestJS

**简介**: TypeScript + NestJS 的 GraphQL 电商框架，企业级设计，插件系统强大，React Admin UI。

**架构**: TypeScript Monorepo，NestJS + TypeORM，支持 PostgreSQL/MySQL/SQLite，GraphQL API。

**优势**: TypeScript 全栈；PostgreSQL 支持；插件系统成熟；v3+ 有 React Admin 现代 UI。

**劣势**: **v3+ 从 MIT 改为 GPL-3.0**，商业部署需购买 VCL 商业授权（8,000 EUR/年）；定位电商；无 B2B PSI 采购/供应商功能；GraphQL-only。

**Lurus 适配性**: 低。License 变更是决定性障碍，商业授权年费不可接受作为 SaaS 基座。v2.x 的 MIT 代码已 EOL (2024-12-31)，不可依赖。

---

### 4.10 Corteza

**GitHub**: [github.com/cortezaproject/corteza](https://github.com/cortezaproject/corteza) — 2.1k stars, Apache-2.0, Go 54% + Vue.js

**简介**: Planet Crust 出品的低代码平台，Salesforce 开源替代品。后端 Go，前端 Vue.js，支持自定义数据模型、流程引擎、RBAC、REST API。已有案例基于 Corteza 构建自定义库存应用。

**架构**: Go (Gin-like) 后端，PostgreSQL，NATS（消息总线，与 Lurus 一致！），Redis，Docker 部署，Vue.js 前端。

**优势**: **Apache-2.0 + Go 后端**——是符合 Lurus 技术偏好的唯一选项；NATS 作为消息总线与 Lurus 基础设施天然兼容；低代码模型允许快速定制；AI 接口 (Aire) 存在；RBAC 成熟。

**劣势**: 2.1k stars（社区较小）；定位是低代码平台而非 PSI 专用；无现成采购/销售/库存模块，需从低代码层构建，开发成本高；PSI 专业深度（批次/序列号/盘点/财务对账）需大量自建；文档相对薄弱。

**Lurus 适配性**: 中。Go + Apache-2.0 + NATS 组合与 Lurus 基础设施高度一致，值得深入研究其**底层架构模式**（模块注册、事件总线、RBAC 设计）。但直接在 Corteza 上构建 PSI 产品层不如直接自研。

---

## 5. 推荐基座

### 主选：自研 Go PSI 服务，借鉴 GreaterWMS 数据模型

**推荐方案**: 新建 `2b-svc-psi`，技术栈 Go (Gin) + PostgreSQL + NATS，**不直接 fork 任何开源项目**，而是：

1. **数据模型参考 GreaterWMS**：其 Apache-2.0 数据库 schema（产品/仓库/货位/ASN/出库单/盘点单）经过生产验证，可作为 Lurus PSI 的 schema 设计蓝图。[GreaterWMS API 文档](https://github.com/GreaterWMS/GreaterWMS/tree/master/docs)
2. **业务流程参考 OFBiz + ERPNext**：OFBiz 的 `product/inventory/order` 实体模型是行业标准，ERPNext 的 DocType 定义是理解采购/销售流程的最佳学习材料。
3. **与 Lurus 既有基础设施深度集成**：账户/计费对接 `2l-svc-platform`，AI 补货预测调用 `2b-svc-api` (Hub LLM gateway)，通知推送走 NATS → `notification` 服务。
4. **AI 层**：在标准 PSI 业务流之上，通过调用 Hub 的 LLM API 实现补货预测 (RAG + 历史销量)、滞销预警、动态定价建议——这是与普通开源进销存的核心差异化。

**理由**:
- 任何 GPL 项目都有 SaaS 法律灰色地带，与 Lurus 商业化目标有隐性冲突；Apache-2.0/MIT 的完整 PSI 项目不存在（确认为空缺）。
- Lurus 已有 Go + PostgreSQL + NATS 的完整基础设施，自研边际成本低，且可深度定制多租户、AI 接口、计费对接。
- GreaterWMS 的 Apache-2.0 数据模型可合法参考，无需担心 License 风险。
- 自研 AI 层是真正的护城河，fork 任何开源项目都无法获得这一层。

**风险**: 自研意味着前期工期较长（估计 3-6 个月达到 MVP）；需要在产品规划时谨慎定义 PSI 核心功能边界，避免范围蔓延。

---

### 备选：Medusa v2 (MIT) 作为前端/电商侧补充

**推荐用途**: 若 Lurus PSI 有面向电商卖家（Shopify/淘宝/拼多多店铺）的多渠道库存同步场景，可引入 Medusa v2 的 `@medusajs/product` 和 `@medusajs/inventory` 模块（MIT，独立可用），作为电商侧的商品/库存 API 层，与自研 Go PSI 服务通过 NATS 事件同步。

**理由**:
- Medusa v2 模块化设计允许单独使用 inventory 模块，不必引入整个框架。
- MIT License 无任何商用限制。
- TypeScript 与 Lurus 前端栈（React/Next.js）完美匹配，前端组件可复用。
- 32.8k stars 说明社区稳健，长期维护可靠。

**局限**: 仅适合补充电商侧，不能替代 Go PSI 服务的 B2B 采购/仓库/财务核心。

---

## 6. 风险与陷阱

### 6.1 License 坑

| 风险 | 详情 | 严重度 |
|------|------|--------|
| ERPNext/Frappe GPL-3.0 | SaaS 部署不触发分发条款，但 Frappe **明确要求**商业产品使用品牌名需获得授权（non-commercial only）。若 Lurus 产品名或 UI 中出现 ERPNext/Frappe 字样将违规。 | 高 |
| Odoo LGPL-3.0 | Community 核心 LGPL 允许商用，但与 Enterprise 模块混用时条款复杂，且 Odoo 官方会对 SaaS 竞争者采取品牌保护行动（历史案例存在）。 | 中 |
| Vendure GPL-3.0 (v3+) | v3+ 已不再是 MIT，使用 v3+ 代码商业部署需购买 VCL 授权（€8,000/年/项目）。v2 已 EOL，不可依赖。 | 高（若误用 v3+） |
| GPL "SaaS 漏洞" 的误解 | GPL-3.0 不等于 AGPL-3.0，GPL SaaS 部署**不强制开放源码**，但这是法律解读，不同司法管辖区存在差异。中国法院对此无明确判例。保险起见，Lurus 应避免在核心产品中使用 GPL 代码。 | 中 |

### 6.2 维护风险

- **GreaterWMS v3.0 重构**：项目正在用 Rust+Python Bomiot 框架重写，v2.x 可能进入维护模式。若参考 v2 schema，需关注重大变更。
- **Apache OFBiz 社区萎缩**：1.0k stars 且多为老项目，活跃贡献者减少，长期参考价值下降。
- **MedusaJS v2 迁移破坏性变更**：v1 到 v2 有大规模 breaking change，引入前需确认版本策略。

### 6.3 技术债陷阱

- **选 Python/PHP 项目 fork**：ERPNext/Odoo/Dolibarr 均为 Python/PHP，与 Lurus Go 主栈不兼容，fork 改造成本约 6-12 个月工作量，且后续同步上游 patch 极困难。强烈不建议。
- **过度依赖开源框架的 UI**：大多数开源 ERP 的 UI 为传统 CRUD 管理后台，与 Lurus "AI-native" 产品定位不符，无论选哪个框架，前端都需要完全重写。
- **多租户改造成本**：ERPNext/Dolibarr/GreaterWMS 均非原生多租户设计，将其改造为 SaaS 多租户需要深入修改数据层（schema 隔离或 row-level security），成本高于自研。

### 6.4 中国市场特殊约束

- 进销存系统若涉及财务凭证，需对接中国增值税发票系统（金税系统），目前所有国际开源项目均无此集成，需自建。
- 数据本地化（如《数据安全法》要求）：SaaS 数据不能出境，K8s 部署在国内节点满足此条件，但选型时需确认第三方组件无外发数据的后门。

---

## 7. 引用

| 项目 | GitHub URL | License | Stars (2026-04) |
|------|-----------|---------|----------------|
| ERPNext | https://github.com/frappe/erpnext | GPL-3.0 | 33.1k |
| Odoo | https://github.com/odoo/odoo | LGPL-3.0 | 50.3k |
| Dolibarr | https://github.com/Dolibarr/dolibarr | GPL-3.0 | 6.7k |
| Tryton | https://github.com/tryton/tryton | GPL-3.0 | ~低 |
| Apache OFBiz | https://github.com/apache/ofbiz-framework | Apache-2.0 | 1.0k |
| GreaterWMS | https://github.com/GreaterWMS/GreaterWMS | Apache-2.0 | 4.3k |
| MedusaJS | https://github.com/medusajs/medusa | MIT | 32.8k |
| Saleor | https://github.com/saleor/saleor | BSD-3-Clause | 22.4k |
| Vendure | https://github.com/vendure-ecommerce/vendure | GPL-3.0 (v3+) | 6.9k |
| Corteza | https://github.com/cortezaproject/corteza | Apache-2.0 | 2.1k |
| ModernWMS | https://github.com/fjykTec/ModernWMS | Apache-2.0 | 1.5k |
| InvenTree | https://github.com/inventree/InvenTree | MIT | ~5.6k |
| 管店云 PSI | https://github.com/guandianyun/psi | GPL-3.0 | 68 |

**附录参考**:
- Odoo License 官方说明: https://www.odoo.com/documentation/19.0/legal/licenses.html
- Odoo LGPL SaaS 讨论: https://www.odoo.com/forum/help-1/lgpl-v3-odoo-community-license-to-be-used-in-a-commercial-softwaresaas-155092
- ERPNext License & Trademark: https://erpnext.com/license-trademark
- Vendure License: https://vendure.io/licensing
- Saleor 多仓库: https://saleor.io/blog/saleor-multiwarehouse-inventory
- PkgPulse Medusa vs Saleor vs Vendure 2026: https://www.pkgpulse.com/blog/medusa-vs-saleor-vs-vendure-headless-ecommerce-2026
- Corteza Go backend: https://cortezaproject.org
- GreaterWMS Docs: https://github.com/GreaterWMS/GreaterWMS
