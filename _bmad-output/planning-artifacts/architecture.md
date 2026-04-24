# Lurus Tally — Architecture Document

> 版本: 1.0 | 日期: 2026-04-23 | 状态: **APPROVED**
> 数据来源: decision-lock.md (LOCKED) + code-borrowing-plan.md §6 + ux-benchmarks.md + lurus.yaml
> 所有端口/命名空间/DB schema/Redis DB/NATS stream 均严格匹配 lurus.yaml 已注册值。

---

## 1. System Context (C4 Level 1)

### 1.1 上下文图

```
                        ┌─────────────────────────────────────────────┐
                        │             外部用户                         │
                        │  中小企业老板 / 财务 / 仓管 / 业务员          │
                        └───────────────────┬─────────────────────────┘
                                            │ HTTPS (tally.lurus.cn)
                        ┌───────────────────▼─────────────────────────┐
                        │              Lurus Tally                     │
                        │   2b-svc-psi  |  namespace: lurus-tally      │
                        │   后端 Go :18200 + 前端 Next.js :3000         │
                        └──┬────┬─────┬────┬────┬────┬────────────────┘
                           │    │     │    │    │    │
            ┌──────────────┘    │     │    │    │    └──────────────────┐
            │            ┌──────┘     │    │    └──────────────┐        │
            │            │            │    │                   │        │
            ▼            ▼            ▼    ▼                   ▼        ▼
   ┌──────────────┐ ┌─────────┐ ┌───────┐ ┌──────────┐ ┌─────────┐ ┌──────────┐
   │ 2l-svc-      │ │ 2b-svc- │ │ 2b-   │ │ 2b-svc-  │ │ NATS    │ │ Zitadel  │
   │ platform     │ │ api     │ │ svc-  │ │ memorus  │ │ PSI_    │ │ (auth.   │
   │ (账户/计费)  │ │ (Hub    │ │ kova  │ │ (RAG)    │ │ EVENTS) │ │ lurus.cn)│
   │ :18104       │ │ LLM)    │ │ (Agent│ │ :8880    │ │         │ │          │
   │              │ │ :8850   │ │ )     │ │          │ │         │ │          │
   └──────────────┘ └─────────┘ └───────┘ └──────────┘ └─────────┘ └──────────┘
            │
            ▼
   ┌──────────────────────────────────────────────────────────────────┐
   │           共享基础设施                                            │
   │  lurus-pg-rw (schema: tally)  Redis DB 5  NATS PSI_EVENTS        │
   │  MinIO (user-uploads bucket)  Jaeger/Loki/Prometheus              │
   └──────────────────────────────────────────────────────────────────┘
```

### 1.2 外部依赖关系

| 系统 | 调用方向 | 协议 | 用途 |
|------|----------|------|------|
| Zitadel (auth.lurus.cn) | Tally ← | OIDC | 用户认证、JWT 签发、角色声明 |
| 2l-svc-platform (:18104) | Tally → | HTTP REST (bearer key) | 租户账户验证、订阅状态、配额检查、计费事件 |
| 2b-svc-api / Hub (:8850) | Tally → | HTTP REST (OpenAI 兼容) | LLM 自然语言查询、函数调用、流式输出 |
| 2b-svc-kova | Tally → | HTTP REST | 补货 Agent 注册与触发、Agent 状态查询 |
| 2b-svc-memorus (:8880) | Tally → | HTTP REST | 写入销售/库存事件；RAG 历史语义检索 |
| 2l-svc-platform/notification | Tally → | HTTP POST (internal bearer) | 库存预警推送、单据状态通知 |
| PostgreSQL (lurus-pg-rw) | Tally → | TCP/5432 | 所有业务数据持久化 (schema: tally) |
| Redis DB 5 | Tally → | TCP/6379 | 会话缓存、限流计数器、乐观锁版本 |
| NATS PSI_EVENTS | Tally ↔ | TCP/4222 | 库存变更事件发布；Worker 消费 |
| MinIO | Tally → | HTTP S3 API | 附件/商品图片存储 (user-uploads bucket) |

### 1.3 数据流向概述

1. **用户认证流**: 浏览器 → Zitadel OIDC → tally-web Next.js BFF → `/api/auth/callback` → 签发 session cookie → 后续请求携带 JWT 到 tally-backend
2. **业务操作流**: 前端 → Next.js BFF (`/api/v1/*`) → Go Backend → PostgreSQL/Redis → 返回响应
3. **库存事件流**: Go Backend 审核单据 → 更新 `stock_snapshot` → 发布 `psi.stock.changed` 到 NATS PSI_EVENTS → tally-worker 消费 → 触发预警/AI分析
4. **AI 查询流**: 前端 AI Drawer → Go Backend `/api/v1/ai/chat` → Hub LLM (SSE) → Memorus RAG 增强 → 流式响应到前端
5. **Agent 触发流**: tally-worker 定时/事件触发 → Kova Agent API → Agent 执行 → 结果写回 PostgreSQL → 通知前端

---

## 2. Container Diagram (C4 Level 2)

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│  namespace: lurus-tally                                                          │
│                                                                                  │
│  ┌─────────────────────────┐      ┌─────────────────────────────────────────┐   │
│  │     tally-web            │      │          tally-backend                   │   │
│  │  Next.js 14 (App Router) │      │    Go 1.25 + Gin + GORM                 │   │
│  │  TypeScript + Bun        │      │    Port: 18200                           │   │
│  │  Port: 3000              │      │                                          │   │
│  │                          │      │  ┌──────────┐  ┌──────────┐             │   │
│  │  • shadcn/ui             │──────▶  │ handler/ │  │  app/    │             │   │
│  │  • TanStack Table/Query  │  REST│  │  (Gin)   │  │ use-case │             │   │
│  │  • Zustand               │      │  └────┬─────┘  └────┬─────┘             │   │
│  │  • Framer Motion         │      │       │              │                   │   │
│  │  • Recharts + Tremor     │      │  ┌────▼─────────────▼────────────────┐  │   │
│  │  • cmdk + sonner         │      │  │        adapter/                   │  │   │
│  │  • next-themes           │      │  │  repo/ │ nats/ │ hub/ │ kova/    │  │   │
│  │                          │      │  │ platform/ │ memorus/ │ tax/      │  │   │
│  │  BFF 层: /api/* proxy    │      │  └───────────────────────────────────┘  │   │
│  └─────────────────────────┘      └─────────────────────────────────────────┘   │
│                                                                                  │
│  ┌─────────────────────────────────────────────────────────────────────────┐    │
│  │                          tally-worker                                    │    │
│  │  Go 1.25 独立 Goroutine 组 (同 binary，独立 Deployment 可选)              │    │
│  │  • NATS PSI_EVENTS 消费者（库存预警、单据状态变更）                       │    │
│  │  • 定时任务（低库存扫描 每小时、AI预测刷新 每日）                         │    │
│  │  • Kova Agent 触发器（补货 Agent / 滞销 Agent）                          │    │
│  │  • Memorus 写入（销售事件 / 库存快照）                                   │    │
│  └─────────────────────────────────────────────────────────────────────────┘    │
│                                                                                  │
└─────────────────────────────────────────────────────────────────────────────────┘

外部依赖 (cluster-internal):
  lurus-pg-rw.database.svc:5432  (schema: tally)
  redis.messaging.svc:6379       (DB 5)
  nats.messaging.svc:4222        (PSI_EVENTS stream)
  api.lurus.cn / lurus-system    (Hub LLM :8850)
  platform-core.lurus-platform.svc:18104  (Platform)
  kova-rest                      (Kova Agent)
  memorus.lurus-system.svc:8880  (Memorus)
```

---

## 3. Backend Project Structure (Go)

```
2b-svc-psi/
├── cmd/
│   └── server/
│       └── main.go                    # 启动入口，DI 组装
├── internal/
│   ├── domain/
│   │   └── entity/
│   │       ├── tenant.go              # 租户本地缓存模型
│   │       ├── product.go             # 商品主档 (含 embedding/ai_metadata)
│   │       ├── product_sku.go         # SKU/多单位/多价格
│   │       ├── product_category.go    # 商品分类树
│   │       ├── product_attribute.go   # 属性组（颜色/尺码）
│   │       ├── unit.go                # 多单位换算
│   │       ├── partner.go             # 供应商/客户统一主档
│   │       ├── partner_bank.go        # 银行账户
│   │       ├── warehouse.go           # 仓库主档
│   │       ├── warehouse_bin.go       # 货位（库位）
│   │       ├── bill_head.go           # 单据主表（通用：采购/销售/调拨/盘点）
│   │       ├── bill_item.go           # 单据明细
│   │       ├── stock_snapshot.go      # 库存快照（实时）
│   │       ├── stock_initial.go       # 期初库存/安全库存
│   │       ├── stock_lot.go           # 批次台账
│   │       ├── stock_serial.go        # 序列号台账
│   │       ├── finance_account.go     # 资金账户
│   │       ├── payment_head.go        # 收付款主表
│   │       ├── payment_item.go        # 收付款明细
│   │       ├── finance_category.go    # 收支项目分类
│   │       ├── org_department.go      # 部门
│   │       ├── org_user_rel.go        # 部门-用户关系
│   │       ├── audit_log.go           # 操作审计日志
│   │       ├── system_config.go       # 租户级系统配置
│   │       ├── dict_type.go           # 数据字典类型
│   │       ├── dict_data.go           # 数据字典值
│   │       └── bill_sequence.go       # 单据编号生成器
│   ├── app/
│   │   ├── product/
│   │   │   ├── create_product.go      # 创建商品
│   │   │   ├── update_product.go      # 更新商品
│   │   │   ├── delete_product.go      # 删除商品（软删）
│   │   │   ├── query_product.go       # 查询/搜索（含向量搜索）
│   │   │   └── import_product.go      # 批量导入（Excel/CSV）
│   │   ├── stock/
│   │   │   ├── query_stock.go         # 库存查询（多维度）
│   │   │   ├── adjust_stock.go        # 手动调整（非单据）
│   │   │   ├── alert_stock.go         # 预警检查 + 推送
│   │   │   └── reorder_suggestion.go  # 补货建议查询
│   │   ├── purchase/
│   │   │   ├── create_purchase.go     # 创建采购单/采购订单
│   │   │   ├── submit_purchase.go     # 提交审核
│   │   │   ├── approve_purchase.go    # 审核（库存 +）
│   │   │   ├── reject_purchase.go     # 驳回
│   │   │   ├── cancel_purchase.go     # 取消（红冲）
│   │   │   ├── receive_purchase.go    # 确认入库（部分收货支持）
│   │   │   └── query_purchase.go      # 查询/列表
│   │   ├── sales/
│   │   │   ├── create_sales.go        # 创建销售单/销售订单
│   │   │   ├── submit_sales.go        # 提交审核
│   │   │   ├── approve_sales.go       # 审核（库存预扣）
│   │   │   ├── ship_sales.go          # 确认发货（库存 -）
│   │   │   ├── cancel_sales.go        # 取消（红冲）
│   │   │   └── query_sales.go         # 查询/列表
│   │   ├── transfer/
│   │   │   ├── create_transfer.go     # 创建调拨单
│   │   │   ├── approve_transfer.go    # 审核（跨仓库存变动）
│   │   │   └── query_transfer.go      # 查询/列表
│   │   ├── stocktake/
│   │   │   ├── create_stocktake.go    # 创建盘点任务
│   │   │   ├── record_stocktake.go    # 录入实盘数量
│   │   │   ├── finalize_stocktake.go  # 盘点完成（差异写入）
│   │   │   └── query_stocktake.go     # 查询/列表
│   │   ├── finance/
│   │   │   ├── create_payment.go      # 创建收付款单
│   │   │   ├── query_payment.go       # 查询台账
│   │   │   └── reconcile.go          # 应收应付对账
│   │   ├── report/
│   │   │   ├── stock_report.go        # 库存报表（周转/ABC/滞销）
│   │   │   ├── purchase_report.go     # 采购报表
│   │   │   ├── sales_report.go        # 销售报表
│   │   │   └── finance_report.go      # 财务报表（应收应付）
│   │   └── ai_agent/
│   │       ├── chat.go                # Hub LLM 自然语言查询（含 RAG）
│   │       ├── function_registry.go   # 工具调用函数注册表
│   │       ├── reorder_agent.go       # Kova 补货 Agent 交互
│   │       ├── deadstock_agent.go     # 滞销预警 Agent 交互
│   │       └── prompts/               # Prompt 模板目录
│   │           ├── chat_system.txt    # 系统角色 prompt
│   │           ├── stock_query.txt    # 库存查询 prompt
│   │           └── reorder.txt        # 补货分析 prompt
│   ├── adapter/
│   │   ├── handler/
│   │   │   ├── router.go              # Gin 路由注册总表
│   │   │   ├── v1/
│   │   │   │   ├── product.go         # /api/v1/products
│   │   │   │   ├── sku.go             # /api/v1/skus
│   │   │   │   ├── warehouse.go       # /api/v1/warehouses
│   │   │   │   ├── stock.go           # /api/v1/stocks
│   │   │   │   ├── purchase.go        # /api/v1/purchases
│   │   │   │   ├── sales.go           # /api/v1/sales
│   │   │   │   ├── transfer.go        # /api/v1/transfers
│   │   │   │   ├── stocktake.go       # /api/v1/stocktakes
│   │   │   │   ├── partner.go         # /api/v1/partners
│   │   │   │   ├── finance.go         # /api/v1/finance
│   │   │   │   ├── report.go          # /api/v1/reports
│   │   │   │   └── ai.go              # /api/v1/ai (chat + suggestions)
│   │   │   ├── internal/
│   │   │   │   └── stock_query.go     # /internal/v1/tally/...
│   │   │   ├── agent/
│   │   │   │   └── tools.go           # /agent/v1/tools (Kova 工具调用)
│   │   │   └── ws/
│   │   │       └── hub.go             # WebSocket Hub (实时推送)
│   │   ├── middleware/
│   │   │   ├── auth.go                # JWT 验证 (Zitadel OIDC)
│   │   │   ├── tenant_rls.go          # SET app.tenant_id (PostgreSQL RLS)
│   │   │   ├── ratelimit.go           # Redis 滑动窗口限流
│   │   │   └── audit.go              # 写操作自动记录 audit_log
│   │   ├── repo/
│   │   │   ├── product_repo.go        # GORM ProductRepo
│   │   │   ├── product_sku_repo.go
│   │   │   ├── warehouse_repo.go
│   │   │   ├── stock_repo.go          # stock_snapshot + stock_lot CRUD
│   │   │   ├── bill_repo.go           # bill_head + bill_item CRUD
│   │   │   ├── partner_repo.go
│   │   │   ├── payment_repo.go
│   │   │   ├── audit_repo.go
│   │   │   └── config_repo.go
│   │   ├── nats/
│   │   │   ├── publisher.go           # PSI_EVENTS 发布
│   │   │   └── subscriber.go          # PSI_EVENTS 消费（worker）
│   │   ├── platform/
│   │   │   ├── client.go              # 2l-svc-platform HTTP client
│   │   │   ├── tenant.go              # 租户信息同步
│   │   │   └── billing.go             # 配额检查 + 计费事件
│   │   ├── hub/
│   │   │   ├── client.go              # Hub LLM HTTP client (OpenAI 兼容)
│   │   │   └── streaming.go           # SSE 流式转发
│   │   ├── kova/
│   │   │   ├── client.go              # Kova REST client
│   │   │   └── agent.go               # Agent 注册/触发/状态
│   │   ├── memorus/
│   │   │   ├── client.go              # Memorus HTTP client
│   │   │   ├── write.go               # 事件写入记忆
│   │   │   └── search.go              # RAG 语义检索
│   │   ├── notification/
│   │   │   └── client.go              # platform/notification POST /internal/v1/notify
│   │   └── tax/
│   │       └── stub.go                # 金税四期 ISV stub（v2 实现）
│   ├── lifecycle/
│   │   ├── app.go                     # Application struct (DI root)
│   │   ├── start.go                   # 启动序列（DB连接→NATS→HTTP→Worker）
│   │   ├── stop.go                    # 优雅停机（drain → close）
│   │   ├── migrate.go                 # 启动时运行 golang-migrate
│   │   └── worker.go                  # 后台 Worker goroutine 管理
│   └── pkg/
│       ├── config/
│       │   └── config.go              # 环境变量解析（启动即校验）
│       ├── logger/
│       │   └── logger.go              # 结构化 JSON 日志（zerolog）
│       ├── common/
│       │   ├── response.go            # 统一响应格式
│       │   ├── errors.go              # 错误码枚举
│       │   ├── pagination.go          # cursor-based 分页
│       │   └── validator.go           # 入参校验工具
│       ├── dto/
│       │   ├── product_dto.go         # 商品 Request/Response DTO
│       │   ├── bill_dto.go            # 单据 DTO
│       │   ├── stock_dto.go           # 库存 DTO
│       │   └── report_dto.go          # 报表 DTO
│       ├── types/
│       │   ├── bill_status.go         # 单据状态枚举
│       │   ├── bill_type.go           # 单据类型/子类型常量
│       │   └── partner_type.go        # 合作伙伴类型枚举
│       ├── metrics/
│       │   └── metrics.go             # Prometheus 指标注册
│       └── tracing/
│           └── tracing.go             # OpenTelemetry 初始化
├── web/                               # Next.js 14 前端（详见 §4）
├── migrations/                        # golang-migrate SQL 文件
│   ├── 000001_init_extensions.up.sql
│   ├── 000002_init_tenant.up.sql
│   ├── 000003_init_org.up.sql
│   ├── 000004_init_partner.up.sql
│   ├── 000005_init_product.up.sql
│   ├── 000006_init_stock.up.sql
│   ├── 000007_init_bill.up.sql
│   ├── 000008_init_finance.up.sql
│   ├── 000009_init_audit.up.sql
│   ├── 000010_init_config.up.sql
│   ├── 000011_init_views.up.sql
│   └── 000012_init_rls.up.sql        # 所有 RLS policy
├── deploy/
│   └── k8s/
│       ├── base/                      # Kustomize base
│       └── overlays/
│           ├── stage/
│           └── prod/
├── tests/
│   ├── integration/
│   └── e2e/
├── THIRD_PARTY_LICENSES/
│   ├── jshERP-LICENSE                 # Apache-2.0 (数据模型来源)
│   └── GreaterWMS-LICENSE             # Apache-2.0 (WMS 模型来源)
└── CLAUDE.md
```

**文件数量估计**: 约 80+ Go 文件（domain 27 个 entity、app 35+ use case、adapter 25+ 文件、pkg 15+ 文件）。

---

## 4. Frontend Project Structure (Next.js)

```
2b-svc-psi/web/
├── app/
│   ├── (auth)/
│   │   ├── login/
│   │   │   └── page.tsx               # 登录入口（跳转 Zitadel）
│   │   └── callback/
│   │       └── page.tsx               # OIDC callback 处理
│   ├── (dashboard)/
│   │   ├── layout.tsx                 # 主布局：Sidebar + Topbar + AI Drawer
│   │   ├── page.tsx                   # 仪表盘首页
│   │   ├── products/
│   │   │   ├── page.tsx               # 商品列表（TanStack Table，虚拟滚动）
│   │   │   ├── new/
│   │   │   │   └── page.tsx           # 新建商品表单
│   │   │   └── [id]/
│   │   │       └── page.tsx           # 商品详情（Slide-over 入口）
│   │   ├── stock/
│   │   │   ├── page.tsx               # 库存总览（按仓库/商品维度）
│   │   │   └── [warehouseId]/
│   │   │       └── page.tsx           # 单仓库库存详情
│   │   ├── purchases/
│   │   │   ├── page.tsx               # 采购单列表
│   │   │   ├── new/
│   │   │   │   └── page.tsx           # 创建采购单（Stepper）
│   │   │   └── [id]/
│   │   │       └── page.tsx           # 采购单详情
│   │   ├── sales/
│   │   │   ├── page.tsx               # 销售单列表
│   │   │   ├── new/
│   │   │   │   └── page.tsx           # 创建销售单（Stepper）
│   │   │   └── [id]/
│   │   │       └── page.tsx           # 销售单详情
│   │   ├── transfers/
│   │   │   ├── page.tsx               # 调拨单列表
│   │   │   └── new/
│   │   │       └── page.tsx           # 创建调拨单
│   │   ├── stocktakes/
│   │   │   ├── page.tsx               # 盘点任务列表
│   │   │   └── [id]/
│   │   │       └── page.tsx           # 盘点操作页（进度条 + 行内录入）
│   │   ├── partners/
│   │   │   ├── page.tsx               # 供应商/客户列表
│   │   │   └── [id]/
│   │   │       └── page.tsx           # 合作伙伴详情（应收应付台账）
│   │   ├── finance/
│   │   │   ├── page.tsx               # 财务总览
│   │   │   ├── accounts/
│   │   │   │   └── page.tsx           # 资金账户列表
│   │   │   └── payments/
│   │   │       └── page.tsx           # 收付款记录
│   │   ├── reports/
│   │   │   ├── page.tsx               # 报表总览（KPI 卡 + 图表）
│   │   │   ├── stock/
│   │   │   │   └── page.tsx           # 库存报表（周转/ABC/滞销）
│   │   │   ├── purchases/
│   │   │   │   └── page.tsx           # 采购分析报表
│   │   │   ├── sales/
│   │   │   │   └── page.tsx           # 销售分析报表
│   │   │   └── finance/
│   │   │       └── page.tsx           # 财务报表（应收应付账龄）
│   │   └── settings/
│   │       ├── page.tsx               # 系统配置
│   │       └── warehouses/
│   │           └── page.tsx           # 仓库管理
│   ├── api/
│   │   ├── auth/
│   │   │   ├── [...nextauth]/
│   │   │   │   └── route.ts           # NextAuth OIDC handler
│   │   │   └── callback/
│   │   │       └── route.ts
│   │   └── proxy/
│   │       └── [...path]/
│   │           └── route.ts           # BFF 代理转发到 Go Backend :18200
│   └── layout.tsx                     # Root layout（ThemeProvider + QueryProvider）
├── components/
│   ├── ui/                            # shadcn/ui 组件（Button/Input/Sheet/Dialog/...）
│   │   ├── button.tsx
│   │   ├── sheet.tsx                  # Slide-over Sheet (侧滑详情)
│   │   ├── dialog.tsx
│   │   ├── badge.tsx                  # 状态 Badge（success/warning/danger）
│   │   ├── skeleton.tsx               # 骨架屏
│   │   ├── tooltip.tsx                # 含快捷键提示
│   │   ├── dropdown-menu.tsx
│   │   ├── command.tsx                # cmdk 封装
│   │   └── ...（其余 shadcn 组件）
│   ├── ai-drawer/
│   │   ├── ai-drawer.tsx              # 右侧固定 AI Drawer（非遮罩）
│   │   ├── chat-message.tsx           # 消息气泡（含内联操作按钮）
│   │   └── use-ai-chat.ts             # 流式输出 hook (useChat)
│   ├── command-palette/
│   │   ├── command-palette.tsx        # ⌘K 全局命令面板
│   │   └── commands.ts                # 命令定义表（导航/操作/搜索分组）
│   ├── data-table/
│   │   ├── data-table.tsx             # TanStack Table 通用封装（含虚拟滚动）
│   │   ├── data-table-toolbar.tsx     # 搜索框 + 过滤器 chip
│   │   ├── data-table-pagination.tsx  # 分页控件
│   │   └── column-helpers.ts          # 列定义工具函数
│   ├── form-builder/
│   │   ├── form-field.tsx             # React Hook Form + Zod 通用字段
│   │   ├── stepper.tsx                # 多步表单 Stepper 组件
│   │   └── product-search.tsx         # 商品搜索输入（combobox）
│   ├── bill/
│   │   ├── bill-detail-sheet.tsx      # 单据详情 Slide-over
│   │   ├── bill-item-table.tsx        # 单据明细行内可编辑表格
│   │   └── bill-status-badge.tsx      # 单据状态颜色 Badge
│   ├── charts/                        # 所有图表组件（强制 "use client"）
│   │   ├── kpi-card.tsx               # KPI 卡片 (Tremor + CountUp)
│   │   ├── stock-trend-chart.tsx      # 库存趋势 (Recharts AreaChart)
│   │   ├── sales-bar-chart.tsx        # 销售 BarChart
│   │   └── abc-pie-chart.tsx          # ABC 分析 PieChart
│   ├── layout/
│   │   ├── sidebar.tsx                # 可折叠侧边栏（Zustand 持久化）
│   │   ├── topbar.tsx                 # 顶栏（搜索 + 通知 + AI 按钮）
│   │   └── empty-state.tsx            # 空状态组件（主动引导 CTA）
│   └── slide-over/
│       └── slide-over.tsx             # 通用 Slide-over 封装（Framer Motion）
├── lib/
│   ├── api-client.ts                  # fetch 封装（自动携带 JWT）
│   ├── query-keys.ts                  # TanStack Query key factory
│   ├── utils.ts                       # cn() + formatCNY() + dayjs helpers
│   └── auth.ts                        # NextAuth 配置（Zitadel provider）
├── hooks/
│   ├── use-debounce.ts                # 搜索 debounce
│   ├── use-media-query.ts             # Sheet ↔ Dialog 响应式切换
│   ├── use-table-density.ts           # 表格密度（localStorage 持久化）
│   └── use-command-palette.ts         # ⌘K 全局监听
├── stores/
│   ├── sidebar-store.ts               # Zustand: 侧边栏折叠状态
│   ├── ai-drawer-store.ts             # Zustand: AI Drawer 开关
│   └── table-store.ts                 # Zustand: 表格密度/列显示
└── styles/
    ├── globals.css                    # OKLCH 主题变量（亮色 + 暗色）
    └── print.css                      # 打印样式（隐藏导航，显示公司抬头）
```

**关键配置**: `package.json` 使用 Bun，`next.config.ts` 启用 `output: "standalone"`，字体使用 `next/font` 加载 Noto Sans SC（见 ux-benchmarks.md §7）。

---

## 5. PostgreSQL Schema (核心章节)

**数据库连接**: `lurus-pg-rw.database.svc:5432`，schema: `tally`（已在 lurus.yaml 注册）。
**License 声明**: 所有带注释 "Derived from jshERP/GreaterWMS (Apache-2.0)" 的表结构源自 code-borrowing-plan.md §2 的分析，并在 `THIRD_PARTY_LICENSES/` 保存原始 LICENSE 文件。

### 5.1 前置扩展

```sql
CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "vector";   -- pgvector，需运维确认已部署（见 §18 Open Questions）
```

### 5.2 完整 DDL（27 张核心表）

```sql
-- =====================================================
-- 域 1: tenant — 租户本地缓存
-- =====================================================
CREATE TABLE tally.tenant (
    id            UUID PRIMARY KEY,           -- 与 2l-svc-platform 同 ID
    name          VARCHAR(200) NOT NULL,
    status        SMALLINT NOT NULL DEFAULT 1,-- 1启用 0禁用
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
CREATE TABLE tally.org_department (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tally.tenant(id),
    parent_id   UUID REFERENCES tally.org_department(id),
    name        VARCHAR(100) NOT NULL,
    sort        INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);
CREATE INDEX idx_org_dept_tenant ON tally.org_department(tenant_id);

CREATE TABLE tally.org_user_rel (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL,
    dept_id      UUID REFERENCES tally.org_department(id),
    user_id      UUID NOT NULL,
    sort         INT DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- =====================================================
-- 域 3: partner_* — 供应商/客户
-- =====================================================
-- Derived from jshERP jsh_supplier (Apache-2.0)
CREATE TABLE tally.partner (
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
    advance_balance NUMERIC(18,4) NOT NULL DEFAULT 0,
    ar_balance      NUMERIC(18,4) NOT NULL DEFAULT 0,
    ap_balance      NUMERIC(18,4) NOT NULL DEFAULT 0,
    enabled         BOOLEAN NOT NULL DEFAULT true,
    remark          TEXT,
    ai_metadata     JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ
);
CREATE INDEX idx_partner_tenant ON tally.partner(tenant_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX idx_partner_code ON tally.partner(tenant_id, code)
    WHERE deleted_at IS NULL AND code IS NOT NULL;
ALTER TABLE tally.partner ENABLE ROW LEVEL SECURITY;
CREATE POLICY partner_rls ON tally.partner
    USING (tenant_id = current_setting('app.tenant_id')::UUID);

CREATE TABLE tally.partner_bank (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL,
    partner_id   UUID NOT NULL REFERENCES tally.partner(id),
    bank_name    VARCHAR(100),
    account_no   VARCHAR(100),
    account_name VARCHAR(100),
    is_default   BOOLEAN DEFAULT false
);

-- =====================================================
-- 域 4: product_* — 商品/SKU/分类
-- =====================================================
-- Derived from jshERP jsh_material_category (Apache-2.0)
CREATE TABLE tally.product_category (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL,
    parent_id    UUID REFERENCES tally.product_category(id),
    name         VARCHAR(100) NOT NULL,
    code         VARCHAR(50),
    sort         INT NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at   TIMESTAMPTZ
);
CREATE INDEX idx_product_cat_tenant ON tally.product_category(tenant_id);

-- Derived from jshERP jsh_material (Apache-2.0)
-- Added: embedding vector(1536), ai_metadata JSONB, predicted_* (Lurus 独有)
CREATE TABLE tally.product (
    id                        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                 UUID NOT NULL,
    category_id               UUID REFERENCES tally.product_category(id),
    code                      VARCHAR(100) NOT NULL,
    name                      VARCHAR(200) NOT NULL,
    manufacturer              VARCHAR(100),
    model                     VARCHAR(100),
    spec                      VARCHAR(200),
    brand                     VARCHAR(100),
    mnemonic                  VARCHAR(100),
    color                     VARCHAR(50),
    unit_id                   UUID,
    expiry_days               INT,
    weight_kg                 NUMERIC(18,4),
    enabled                   BOOLEAN NOT NULL DEFAULT true,
    enable_serial_no          BOOLEAN NOT NULL DEFAULT false,
    enable_lot_no             BOOLEAN NOT NULL DEFAULT false,
    shelf_position            VARCHAR(100),
    img_urls                  TEXT[],
    custom_field1             VARCHAR(500),
    custom_field2             VARCHAR(500),
    custom_field3             VARCHAR(500),
    remark                    TEXT,
    -- AI 专属字段 (Lurus 独有)
    embedding                 vector(1536),
    ai_metadata               JSONB NOT NULL DEFAULT '{}',
    predicted_monthly_demand  NUMERIC(18,4),
    predicted_stockout_at     TIMESTAMPTZ,
    recommendation_notes      TEXT,
    created_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at                TIMESTAMPTZ
);
CREATE INDEX idx_product_tenant ON tally.product(tenant_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX idx_product_code ON tally.product(tenant_id, code) WHERE deleted_at IS NULL;
CREATE INDEX idx_product_embedding ON tally.product
    USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);
ALTER TABLE tally.product ENABLE ROW LEVEL SECURITY;
CREATE POLICY product_rls ON tally.product
    USING (tenant_id = current_setting('app.tenant_id')::UUID);

-- Derived from jshERP jsh_material_extend (Apache-2.0)
CREATE TABLE tally.product_sku (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID NOT NULL,
    product_id       UUID NOT NULL REFERENCES tally.product(id),
    bar_code         VARCHAR(100),
    unit_name        VARCHAR(50),
    sku_attrs        VARCHAR(200),
    purchase_price   NUMERIC(18,6) NOT NULL DEFAULT 0,
    retail_price     NUMERIC(18,6) NOT NULL DEFAULT 0,
    wholesale_price  NUMERIC(18,6) NOT NULL DEFAULT 0,
    min_price        NUMERIC(18,6),
    is_default       BOOLEAN NOT NULL DEFAULT false,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);
CREATE INDEX idx_sku_product ON tally.product_sku(product_id);
CREATE INDEX idx_sku_barcode ON tally.product_sku(tenant_id, bar_code) WHERE deleted_at IS NULL;

-- Derived from jshERP jsh_material_attribute (Apache-2.0)
CREATE TABLE tally.product_attribute (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID NOT NULL,
    attribute_name   VARCHAR(100) NOT NULL,
    attribute_values TEXT[],
    sort             INT DEFAULT 0
);

-- Derived from jshERP jsh_unit (Apache-2.0)
CREATE TABLE tally.unit (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL,
    name        VARCHAR(100) NOT NULL,
    base_unit   VARCHAR(50),
    sub_units   JSONB DEFAULT '[]',
    enabled     BOOLEAN DEFAULT true
);

-- =====================================================
-- 域 5: warehouse_* / stock_* — 仓库与库存
-- =====================================================
-- Derived from jshERP jsh_depot (Apache-2.0)
CREATE TABLE tally.warehouse (
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
ALTER TABLE tally.warehouse ENABLE ROW LEVEL SECURITY;
CREATE POLICY warehouse_rls ON tally.warehouse
    USING (tenant_id = current_setting('app.tenant_id')::UUID);

-- Derived from GreaterWMS binset/models.py (Apache-2.0)
CREATE TABLE tally.warehouse_bin (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL,
    warehouse_id UUID NOT NULL REFERENCES tally.warehouse(id),
    bin_code     VARCHAR(100) NOT NULL,
    bin_zone     VARCHAR(50),
    bin_size     VARCHAR(50),     -- S/M/L/XL
    bin_property VARCHAR(50),     -- 常温/冷藏/危品
    is_empty     BOOLEAN DEFAULT true,
    bar_code     VARCHAR(100),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at   TIMESTAMPTZ
);

-- Derived from jshERP jsh_material_initial_stock (Apache-2.0)
CREATE TABLE tally.stock_initial (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL,
    product_id      UUID NOT NULL REFERENCES tally.product(id),
    warehouse_id    UUID NOT NULL REFERENCES tally.warehouse(id),
    qty             NUMERIC(18,4) NOT NULL DEFAULT 0,
    low_safe_qty    NUMERIC(18,4),
    high_safe_qty   NUMERIC(18,4),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_stock_initial_unique
    ON tally.stock_initial(tenant_id, product_id, warehouse_id);

-- Derived from jshERP jsh_material_current_stock (Apache-2.0)
-- Extended with GreaterWMS multi-status stock concept (Apache-2.0)
CREATE TABLE tally.stock_snapshot (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL,
    product_id      UUID NOT NULL REFERENCES tally.product(id),
    warehouse_id    UUID NOT NULL REFERENCES tally.warehouse(id),
    on_hand_qty     NUMERIC(18,4) NOT NULL DEFAULT 0,
    available_qty   NUMERIC(18,4) NOT NULL DEFAULT 0,
    reserved_qty    NUMERIC(18,4) NOT NULL DEFAULT 0,
    in_transit_qty  NUMERIC(18,4) NOT NULL DEFAULT 0,
    damage_qty      NUMERIC(18,4) NOT NULL DEFAULT 0,
    hold_qty        NUMERIC(18,4) NOT NULL DEFAULT 0,
    avg_cost_price  NUMERIC(18,6) NOT NULL DEFAULT 0,   -- 移动加权平均 (WAC)
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_stock_snapshot_unique
    ON tally.stock_snapshot(tenant_id, product_id, warehouse_id);
ALTER TABLE tally.stock_snapshot ENABLE ROW LEVEL SECURITY;
CREATE POLICY stock_snapshot_rls ON tally.stock_snapshot
    USING (tenant_id = current_setting('app.tenant_id')::UUID);

-- NEW — OFBiz Lot 设计借鉴（批次独立追踪）
CREATE TABLE tally.stock_lot (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID NOT NULL,
    product_id       UUID NOT NULL REFERENCES tally.product(id),
    lot_no           VARCHAR(100) NOT NULL,
    manufacture_date DATE,
    expiry_date      DATE,
    qty              NUMERIC(18,4) NOT NULL DEFAULT 0,
    cost_price       NUMERIC(18,6),
    remark           TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_lot_no ON tally.stock_lot(tenant_id, product_id, lot_no);

-- Derived from jshERP jsh_serial_number (Apache-2.0)
CREATE TABLE tally.stock_serial (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL,
    product_id   UUID NOT NULL REFERENCES tally.product(id),
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
CREATE UNIQUE INDEX idx_serial_no
    ON tally.stock_serial(tenant_id, serial_no) WHERE deleted_at IS NULL;

-- =====================================================
-- 域 6: bill_head + bill_item — 核心通用单据
-- =====================================================
-- Derived from jshERP jsh_depot_head (Apache-2.0)
-- Extended: UUID PK, JSONB attachments, UUID[] salesperson_ids, amendment_of_id
-- bill_type: '入库'/'出库'/'其它'
-- sub_type: 采购/销售退货/销售/调拨/盘点录入/盘点复盘/采购订单/销售订单/...
CREATE TABLE tally.bill_head (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL,
    bill_no             VARCHAR(50) NOT NULL,
    bill_no_draft       VARCHAR(50),
    bill_type           VARCHAR(30) NOT NULL,
    sub_type            VARCHAR(30) NOT NULL,
    status              SMALLINT NOT NULL DEFAULT 0,
    -- 0草稿 1已提交 2已审核 3部分完成 4完成 9取消
    purchase_status     SMALLINT DEFAULT 0,
    partner_id          UUID REFERENCES tally.partner(id),
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
    link_bill_id        UUID REFERENCES tally.bill_head(id),
    source              VARCHAR(10) DEFAULT 'web',
    amendment_of_id     UUID REFERENCES tally.bill_head(id),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at          TIMESTAMPTZ
);
CREATE INDEX idx_bill_head_tenant ON tally.bill_head(tenant_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX idx_bill_head_no
    ON tally.bill_head(tenant_id, bill_no) WHERE deleted_at IS NULL;
CREATE INDEX idx_bill_head_type
    ON tally.bill_head(tenant_id, bill_type, sub_type, bill_date);
CREATE INDEX idx_bill_head_partner ON tally.bill_head(tenant_id, partner_id);
ALTER TABLE tally.bill_head ENABLE ROW LEVEL SECURITY;
CREATE POLICY bill_head_rls ON tally.bill_head
    USING (tenant_id = current_setting('app.tenant_id')::UUID);

-- Derived from jshERP jsh_depot_item (Apache-2.0)
-- Extended: serial_nos TEXT[], lot_id FK, bin_id FK
CREATE TABLE tally.bill_item (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL,
    head_id             UUID NOT NULL REFERENCES tally.bill_head(id) ON DELETE CASCADE,
    product_id          UUID NOT NULL REFERENCES tally.product(id),
    product_sku_id      UUID REFERENCES tally.product_sku(id),
    warehouse_id        UUID REFERENCES tally.warehouse(id),
    target_warehouse_id UUID REFERENCES tally.warehouse(id),
    unit_name           VARCHAR(20),
    sku_attrs           VARCHAR(200),
    qty                 NUMERIC(18,4) NOT NULL,
    base_qty            NUMERIC(18,4),
    unit_price          NUMERIC(18,6),
    purchase_price      NUMERIC(18,6),
    tax_rate            NUMERIC(8,4),
    tax_amount          NUMERIC(18,4),
    line_amount         NUMERIC(18,4),
    lot_id              UUID REFERENCES tally.stock_lot(id),
    serial_nos          TEXT[],
    expiry_date         DATE,
    link_item_id        UUID REFERENCES tally.bill_item(id),
    bin_id              UUID REFERENCES tally.warehouse_bin(id),
    remark              VARCHAR(500),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at          TIMESTAMPTZ
);
CREATE INDEX idx_bill_item_head ON tally.bill_item(head_id);
CREATE INDEX idx_bill_item_product ON tally.bill_item(tenant_id, product_id);
ALTER TABLE tally.bill_item ENABLE ROW LEVEL SECURITY;
CREATE POLICY bill_item_rls ON tally.bill_item
    USING (tenant_id = current_setting('app.tenant_id')::UUID);

-- =====================================================
-- 域 7: finance_* — 资金账户/收付款
-- =====================================================
-- Derived from jshERP jsh_account (Apache-2.0)
CREATE TABLE tally.finance_account (
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
CREATE TABLE tally.payment_head (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID NOT NULL,
    pay_type         VARCHAR(30) NOT NULL,
    partner_id       UUID REFERENCES tally.partner(id),
    operator_id      UUID,
    creator_id       UUID NOT NULL,
    bill_no          VARCHAR(50),
    pay_date         TIMESTAMPTZ NOT NULL,
    amount           NUMERIC(18,4) NOT NULL,
    discount_amount  NUMERIC(18,4) DEFAULT 0,
    total_amount     NUMERIC(18,4) NOT NULL,
    account_id       UUID REFERENCES tally.finance_account(id),
    related_bill_id  UUID REFERENCES tally.bill_head(id),
    remark           TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);
CREATE INDEX idx_payment_head_tenant ON tally.payment_head(tenant_id) WHERE deleted_at IS NULL;
ALTER TABLE tally.payment_head ENABLE ROW LEVEL SECURITY;
CREATE POLICY payment_head_rls ON tally.payment_head
    USING (tenant_id = current_setting('app.tenant_id')::UUID);

-- Derived from jshERP jsh_account_item (Apache-2.0)
CREATE TABLE tally.payment_item (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL,
    head_id             UUID NOT NULL REFERENCES tally.payment_head(id),
    finance_category_id UUID,
    amount              NUMERIC(18,4) NOT NULL,
    remark              VARCHAR(500)
);

-- Derived from jshERP jsh_in_out_item (Apache-2.0)
CREATE TABLE tally.finance_category (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    name      VARCHAR(100) NOT NULL,
    cat_type  VARCHAR(20) NOT NULL CHECK (cat_type IN ('income','expense')),
    enabled   BOOLEAN DEFAULT true,
    sort      INT DEFAULT 0
);

-- =====================================================
-- 域 8: audit_log — 操作审计
-- =====================================================
-- Derived from jshERP jsh_log (Apache-2.0), extended with changes JSONB
CREATE TABLE tally.audit_log (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL,
    user_id     UUID,
    action      VARCHAR(50) NOT NULL,
    resource    VARCHAR(50) NOT NULL,
    resource_id UUID,
    changes     JSONB,
    client_ip   VARCHAR(100),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_log_tenant ON tally.audit_log(tenant_id, created_at DESC);
CREATE INDEX idx_audit_log_resource ON tally.audit_log(resource, resource_id);

-- =====================================================
-- 域 9: 系统配置与字典
-- =====================================================
-- Derived from jshERP jsh_system_config (Apache-2.0)
CREATE TABLE tally.system_config (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL,
    key          VARCHAR(100) NOT NULL,
    value        TEXT,
    description  VARCHAR(500),
    UNIQUE (tenant_id, key)
);

-- Derived from jshERP jsh_sys_dict_type + jsh_sys_dict_data (Apache-2.0)
CREATE TABLE tally.dict_type (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID,
    type_code VARCHAR(100) NOT NULL UNIQUE,
    type_name VARCHAR(100) NOT NULL,
    remark    VARCHAR(500)
);

CREATE TABLE tally.dict_data (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID,
    type_id    UUID NOT NULL REFERENCES tally.dict_type(id),
    label      VARCHAR(100) NOT NULL,
    value      VARCHAR(100) NOT NULL,
    sort       INT DEFAULT 0,
    enabled    BOOLEAN DEFAULT true
);

-- Derived from jshERP jsh_sequence (Apache-2.0)
-- 单据编号生成器
CREATE TABLE tally.bill_sequence (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL,
    prefix      VARCHAR(20) NOT NULL,
    current_val BIGINT NOT NULL DEFAULT 0,
    UNIQUE (tenant_id, prefix)
);
```

### 5.3 物化视图

```sql
-- 库存汇总报表（取代实时计算）
CREATE MATERIALIZED VIEW tally.report_stock_summary AS
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
    CASE WHEN ss.available_qty < COALESCE(si.low_safe_qty, 0)
         THEN true ELSE false END AS is_low_stock
FROM tally.stock_snapshot ss
JOIN tally.product p ON p.id = ss.product_id
JOIN tally.warehouse w ON w.id = ss.warehouse_id
LEFT JOIN tally.stock_initial si
    ON si.product_id = ss.product_id AND si.warehouse_id = ss.warehouse_id;

CREATE UNIQUE INDEX idx_report_stock_summary
    ON tally.report_stock_summary(tenant_id, product_id, warehouse_id);

-- AI 补货建议视图（供 Kova Agent 消费）
CREATE VIEW tally.ai_reorder_suggestions AS
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
FROM tally.stock_snapshot ss
JOIN tally.product p ON p.id = ss.product_id
LEFT JOIN tally.stock_initial si
    ON si.product_id = ss.product_id AND si.warehouse_id = ss.warehouse_id
WHERE p.predicted_stockout_at < now() + interval '30 days'
   OR ss.available_qty < COALESCE(si.low_safe_qty, 0);
```

### 5.4 触发器

```sql
-- 自动更新 updated_at
CREATE OR REPLACE FUNCTION tally.touch_updated_at()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN NEW.updated_at = now(); RETURN NEW; END;
$$;

-- 为所有主表挂载 trigger
CREATE TRIGGER trg_product_updated_at
    BEFORE UPDATE ON tally.product
    FOR EACH ROW EXECUTE FUNCTION tally.touch_updated_at();

-- 库存快照联动（审核单据后由 Go 应用层调用，非 trigger，保持业务逻辑在应用层）
```

### 5.5 迁移文件约定

文件命名: `{6位序号}_{snake_case_desc}.{up|down}.sql`，工具: `golang-migrate`。

```
migrations/
├── 000001_init_extensions.up.sql    # pgcrypto, vector
├── 000002_init_tenant.up.sql
├── 000003_init_org.up.sql
├── 000004_init_partner.up.sql
├── 000005_init_product.up.sql
├── 000006_init_stock.up.sql
├── 000007_init_bill.up.sql
├── 000008_init_finance.up.sql
├── 000009_init_audit.up.sql
├── 000010_init_config.up.sql
├── 000011_init_views.up.sql
└── 000012_init_rls.up.sql
```

---

## 6. API Design

### 6.1 REST API（面向前端 + 第三方）

**约定**:
- 基础路径: `/api/v1/`
- 认证: `Authorization: Bearer <zitadel-jwt>`
- 分页: cursor-based（`cursor` + `limit`），列表接口统一支持 `?search=&sort=&order=`
- 错误格式:

```json
{
  "code": "STOCK_INSUFFICIENT",
  "message": "商品 TD-A001 库存不足，当前 5 件，需要 10 件",
  "detail": { "sku_id": "uuid", "available": 5, "required": 10 }
}
```

**核心端点表**:

| 资源 | 端点 | 方法 | 说明 |
|------|------|------|------|
| 商品 | `/api/v1/products` | GET/POST | 列表/创建 |
| | `/api/v1/products/:id` | GET/PATCH/DELETE | 详情/更新/删除 |
| | `/api/v1/products/import` | POST | 批量导入（Excel/CSV） |
| | `/api/v1/products/:id/skus` | GET/POST | SKU 列表/创建 |
| SKU | `/api/v1/skus/:id` | GET/PATCH/DELETE | SKU 详情/更新/删除 |
| 仓库 | `/api/v1/warehouses` | GET/POST | 列表/创建 |
| | `/api/v1/warehouses/:id` | GET/PATCH/DELETE | 详情/更新/删除 |
| | `/api/v1/warehouses/:id/bins` | GET/POST | 货位列表/创建 |
| 库存 | `/api/v1/stocks` | GET | 库存查询（多维度） |
| | `/api/v1/stocks/alerts` | GET | 低库存预警列表 |
| | `/api/v1/stocks/snapshot` | GET | 库存快照（报表用） |
| 采购 | `/api/v1/purchases` | GET/POST | 列表/创建草稿 |
| | `/api/v1/purchases/:id` | GET/PATCH | 详情/更新草稿 |
| | `/api/v1/purchases/:id/submit` | POST | 提交审核 |
| | `/api/v1/purchases/:id/approve` | POST | 审核通过（库存 +） |
| | `/api/v1/purchases/:id/reject` | POST | 驳回 |
| | `/api/v1/purchases/:id/cancel` | POST | 取消（红冲） |
| | `/api/v1/purchases/:id/receive` | POST | 确认入库（支持部分） |
| 销售 | `/api/v1/sales` | GET/POST | 列表/创建草稿 |
| | `/api/v1/sales/:id` | GET/PATCH | 详情/更新草稿 |
| | `/api/v1/sales/:id/submit` | POST | 提交审核 |
| | `/api/v1/sales/:id/approve` | POST | 审核（锁定库存） |
| | `/api/v1/sales/:id/ship` | POST | 确认发货（库存 -） |
| | `/api/v1/sales/:id/cancel` | POST | 取消（红冲） |
| 调拨 | `/api/v1/transfers` | GET/POST | 列表/创建 |
| | `/api/v1/transfers/:id/approve` | POST | 审核（跨仓库存变动） |
| 盘点 | `/api/v1/stocktakes` | GET/POST | 列表/创建任务 |
| | `/api/v1/stocktakes/:id/records` | POST | 录入实盘数量 |
| | `/api/v1/stocktakes/:id/finalize` | POST | 完成盘点 |
| 合作伙伴 | `/api/v1/partners` | GET/POST | 供应商/客户列表/创建 |
| | `/api/v1/partners/:id` | GET/PATCH/DELETE | 详情/更新/删除 |
| | `/api/v1/partners/:id/ar_ap` | GET | 应收应付台账 |
| 财务 | `/api/v1/finance/accounts` | GET/POST | 资金账户 |
| | `/api/v1/finance/payments` | GET/POST | 收付款单据 |
| 报表 | `/api/v1/reports/stock` | GET | 库存报表（周转/ABC/滞销） |
| | `/api/v1/reports/purchase` | GET | 采购分析 |
| | `/api/v1/reports/sales` | GET | 销售分析 |
| | `/api/v1/reports/finance` | GET | 财务报表 |
| AI | `/api/v1/ai/chat` | POST | LLM 自然语言查询（SSE 流式） |
| | `/api/v1/ai/suggestions` | GET | AI 补货建议列表 |
| | `/api/v1/ai/embeddings/refresh` | POST | 刷新商品 embedding |

### 6.2 Internal API（面向 Lurus 其他服务）

认证: `Authorization: Bearer <INTERNAL_API_KEY>`，路由前缀: `/internal/v1/tally/`。

| 端点 | 方法 | 用途 | 消费方 |
|------|------|------|--------|
| `/internal/v1/tally/sku/:id/stock` | GET | 查询 SKU 库存（用于 Lucrum/其他服务）| 待定 |
| `/internal/v1/tally/tenant/sync` | POST | 租户状态同步（platform 回调）| 2l-svc-platform |
| `/internal/v1/tally/health` | GET | 健康探针 | K8s liveness |
| `/internal/v1/tally/metrics` | GET | Prometheus 指标 | Prometheus |

### 6.3 Agent API（面向 Kova Agent + Hub 函数调用）

当 Hub LLM 发起 function calling 时，tally-backend 作为工具提供方。

```json
// 工具注册列表（Go 函数对应的 JSON schema）
[
  {
    "name": "query_stock",
    "description": "查询商品或 SKU 的当前库存数量",
    "parameters": {
      "product_code": "string?",
      "sku_id": "string?",
      "warehouse_id": "string?"
    }
  },
  {
    "name": "query_low_stock",
    "description": "查询低于安全库存的商品列表",
    "parameters": { "threshold_ratio": "number (0-1, default 1.0)" }
  },
  {
    "name": "suggest_reorder",
    "description": "为指定 SKU 生成补货建议（数量+供应商+预计成本）",
    "parameters": { "sku_id": "string" }
  },
  {
    "name": "forecast_stockout",
    "description": "预测指定 SKU 的缺货时间",
    "parameters": { "sku_id": "string", "lookback_days": "int (default 30)" }
  },
  {
    "name": "query_sales_trend",
    "description": "查询商品/SKU 的销售趋势数据",
    "parameters": { "sku_id": "string", "days": "int" }
  },
  {
    "name": "create_purchase_draft",
    "description": "创建采购单草稿（需用户在 UI 确认）",
    "parameters": {
      "partner_id": "string",
      "items": [{ "sku_id": "string", "qty": "number", "unit_price": "number" }]
    }
  }
]
```

### 6.4 WebSocket

端点: `/ws`，认证: query param `?token=<jwt>`

| 事件 | 方向 | payload | 用途 |
|------|------|---------|------|
| `stock.changed` | server→client | `{ sku_id, qty_delta, available_qty }` | 实时库存变更推送 |
| `ai.stream` | server→client | `{ message_id, delta, done }` | AI 流式输出 |
| `alert.low_stock` | server→client | `{ sku_id, name, available_qty }` | 低库存预警实时通知 |
| `bill.status_changed` | server→client | `{ bill_id, bill_no, old_status, new_status }` | 单据状态变更通知 |
| `ping` | client→server | — | 保活 |

---

## 7. AI Integration Architecture

### 7.1 Hub LLM 集成

**调用路径**: tally-backend → Hub (`api.lurus.cn`, OpenAI 兼容协议)

```go
// internal/adapter/hub/client.go
type HubClient struct {
    baseURL    string  // api.lurus.cn/v1
    apiKey     string  // Hub token (from env HUB_TOKEN)
    httpClient *http.Client  // 带 timeout 的 client
}

// 模型选择策略（不硬编码，从 system_config 读取）
// 简单查询（库存余量、单价查询）: claude-haiku-4 (低成本)
// 复杂分析（ABC分析、滞销分析）: claude-sonnet-4-5
// 长上下文报告生成: claude-opus 系列
```

**流式输出链路**:
```
前端 AI Drawer → POST /api/v1/ai/chat → Go handler
→ Memorus search (RAG上下文) → Hub /v1/chat/completions (SSE)
→ Go handler 转发 SSE → WebSocket → 前端 useChat hook 渲染
```

**失败降级策略**:
- Hub 不可用（503/timeout）→ 返回 `{"error": "AI 助手暂时不可用，请稍后重试"}`，不 panic
- Memorus 不可用 → 跳过 RAG 增强，直接调用 Hub（降级但不中断）

**Prompt 模板存储**: `internal/app/ai_agent/prompts/` 纯文本文件，编译时嵌入（`//go:embed`）。

### 7.2 Kova Agent 集成

**补货 Agent 工作流**:
```
触发条件:
  1. 定时: tally-worker 每日 00:30 扫描 ai_reorder_suggestions 视图
  2. 事件: PSI_EVENTS psi.alert.low_stock 触发即时分析

执行流程:
  tally-worker → POST kova-rest/v1/agents/reorder/run
  → Kova 执行 Agent (调用 tally /agent/v1/tools 获取数据)
  → Agent 输出补货建议 JSON
  → tally-worker 写入 product.recommendation_notes + predicted_stockout_at
  → 发布 psi.agent.suggestion.created 事件
  → WebSocket 推送到在线用户
```

**滞销预警 Agent**: 每周日 02:00 运行，分析 30/60/90 天滞销 SKU，更新 `ai_metadata`。

### 7.3 Memorus RAG 集成

**写入时机**（tally-worker 异步处理，不阻塞主链路）:
- 采购单审核通过 → 写入 `{event: "purchase_received", sku_id, qty, partner_name, price}`
- 销售单发货 → 写入 `{event: "sale_shipped", sku_id, qty, customer_name, price}`
- 库存盘点完成 → 写入差异记录

**读取时机**（AI chat 前）:
```go
// internal/adapter/memorus/search.go
func (c *MemClient) SearchRelevantHistory(ctx context.Context, query string) ([]Memory, error) {
    // POST /v1/memory/search { query, top_k: 5 }
    // 超时 2s，失败则返回空列表（降级）
}
```

---

## 8. NATS Event Catalog

**Stream**: `PSI_EVENTS`（已在 lurus.yaml 注册）
**Subject 前缀**: `psi.`
**消费者**: tally-worker（lurus-tally 内部），Kova Agent，2l-svc-platform/notification

### 8.1 事件定义

```go
// internal/pkg/types/events.go

type PSIEvent struct {
    EventID   string    `json:"event_id"`  // UUID
    EventType string    `json:"event_type"` // 见下表
    TenantID  string    `json:"tenant_id"`
    Timestamp time.Time `json:"timestamp"`
    Payload   any       `json:"payload"`
}

// psi.product.created / psi.product.updated / psi.product.deleted
type ProductEventPayload struct {
    ProductID string `json:"product_id"`
    Name      string `json:"name"`
    Code      string `json:"code"`
}

// psi.stock.changed
type StockChangedPayload struct {
    SKUID       string          `json:"sku_id"`
    WarehouseID string          `json:"warehouse_id"`
    QtyDelta    decimal.Decimal `json:"qty_delta"`
    Reason      string          `json:"reason"`  // 采购/销售/调拨/盘点
    BillNo      string          `json:"bill_no"`
    AfterQty    decimal.Decimal `json:"after_qty"`
}

// psi.purchase.submitted / psi.purchase.approved / psi.purchase.received / psi.purchase.cancelled
type PurchaseEventPayload struct {
    BillID    string `json:"bill_id"`
    BillNo    string `json:"bill_no"`
    PartnerID string `json:"partner_id"`
    Amount    string `json:"amount"`
}

// psi.sales.submitted / psi.sales.approved / psi.sales.shipped / psi.sales.cancelled
type SalesEventPayload struct {
    BillID     string `json:"bill_id"`
    BillNo     string `json:"bill_no"`
    CustomerID string `json:"customer_id"`
    Amount     string `json:"amount"`
}

// psi.stocktake.completed
type StocktakeCompletedPayload struct {
    TaskID      string `json:"task_id"`
    WarehouseID string `json:"warehouse_id"`
    DiffCount   int    `json:"diff_count"`
}

// psi.alert.low_stock
type LowStockAlertPayload struct {
    ProductID   string          `json:"product_id"`
    ProductName string          `json:"product_name"`
    SKUID       string          `json:"sku_id"`
    WarehouseID string          `json:"warehouse_id"`
    Available   decimal.Decimal `json:"available_qty"`
    SafeQty     decimal.Decimal `json:"safe_qty"`
}

// psi.alert.dead_stock
type DeadStockAlertPayload struct {
    ProductID   string `json:"product_id"`
    SKUID       string `json:"sku_id"`
    StagnantDays int   `json:"stagnant_days"`
}

// psi.agent.suggestion.created
type AgentSuggestionPayload struct {
    ProductID        string `json:"product_id"`
    SuggestionType   string `json:"suggestion_type"` // reorder/dead_stock
    SuggestionText   string `json:"suggestion_text"`
    SuggestedQty     string `json:"suggested_qty,omitempty"`
}
```

### 8.2 事件消费者表

| 事件 | 消费者 | 处理逻辑 |
|------|--------|----------|
| `psi.stock.changed` | tally-worker | 检查 low_safe_qty → 触发 psi.alert.low_stock |
| `psi.stock.changed` | Memorus (未来) | 写入历史记忆 |
| `psi.purchase.received` | tally-worker | 触发 Memorus 写入 |
| `psi.sales.shipped` | tally-worker | 触发 Memorus 写入 |
| `psi.alert.low_stock` | tally-worker | 调 notification 推送 + 触发 Kova 补货 Agent |
| `psi.agent.suggestion.created` | tally-worker | WebSocket 推送到在线用户 |

---

## 9. Multi-Tenant Architecture

### 9.1 租户接入流程

```
用户访问 tally.lurus.cn
→ Zitadel OIDC 认证（auth.lurus.cn）
→ JWT claim 携带 tenant_id（organization claim）
→ tally-backend middleware/auth.go 解析 JWT
→ middleware/tenant_rls.go 执行 SET LOCAL app.tenant_id = '<uuid>'
→ 所有 GORM 查询自动经 RLS 过滤（无需手动 WHERE tenant_id = ?）
```

**平台账户验证**（启动时 + 登录时）:
```go
// 调 2l-svc-platform /internal/v1/tenants/:id 验证租户状态和订阅级别
// 失败时拒绝登录，返回 "账户已过期，请续费"
```

### 9.2 RLS 实现细节

每张业务表都执行:
```sql
ALTER TABLE tally.<table> ENABLE ROW LEVEL SECURITY;
CREATE POLICY <table>_rls ON tally.<table>
    USING (tenant_id = current_setting('app.tenant_id')::UUID);
```

Go 中间件在每次请求处理前:
```go
// adapter/middleware/tenant_rls.go
func TenantRLS(db *gorm.DB) gin.HandlerFunc {
    return func(c *gin.Context) {
        tenantID := c.GetString("tenant_id") // 从 JWT 解析
        if tenantID == "" {
            c.AbortWithStatus(401)
            return
        }
        // 在本次事务中设置 tenant_id
        db.Exec("SET LOCAL app.tenant_id = ?", tenantID)
        c.Next()
    }
}
```

### 9.3 资源配额（基于 platform 订阅）

| 订阅级别 | SKU 上限 | 单据/日上限 | AI 查询/月上限 |
|----------|----------|-------------|----------------|
| Free | 100 | 20 | 50 |
| Pro | 10,000 | 1,000 | 5,000 |
| Enterprise | 不限 | 不限 | 按量计费 |

配额检查在 `adapter/platform/billing.go` 中实现，每次创建单据前调用。

---

## 10. Security

### 10.1 认证流程

```
浏览器 → Zitadel OIDC (auth.lurus.cn) → ID Token (JWT)
→ Next.js BFF (/api/auth) → session cookie
→ BFF 代理请求: Authorization: Bearer <JWT> → tally-backend
→ middleware/auth.go: JWKS 验证签名 + 提取 tenant_id/user_id/roles
```

### 10.2 权限模型（RBAC）

| 角色 | 商品管理 | 采购 | 销售 | 库存调整 | 财务 | 报表 | 系统设置 |
|------|----------|------|------|----------|------|------|----------|
| 超级管理员 | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| 财务 | 只读 | 只读 | 只读 | ✗ | ✓ | ✓ | ✗ |
| 仓库管理员 | ✓ | 审核/入库 | 审核/出库 | ✓ | ✗ | 库存 | ✗ |
| 业务员 | 只读 | 创建/提交 | 创建/提交 | ✗ | ✗ | 只读 | ✗ |
| 只读查看者 | 只读 | 只读 | 只读 | ✗ | 只读 | ✓ | ✗ |

角色存储在 Zitadel 的 Organization Role，通过 JWT claim 传入，tally 不维护独立用户系统。

### 10.3 审计日志

所有写操作（CREATE/UPDATE/DELETE/APPROVE/CANCEL）由 `middleware/audit.go` 自动记录到 `audit_log`，包含:
- `changes`: before/after diff（JSONB）
- `client_ip`: 客户端 IP（透传 X-Forwarded-For）
- `user_id`: 操作人
- `action`: create/update/delete/approve/cancel/reverse

### 10.4 防御性编码要点（引用全局 CLAUDE.md §3）

- 所有外部输入在 handler 层 Zod/Gin-binding 校验，内部信任已校验数据
- 金额字段全部使用 `NUMERIC(18,4)`，禁止 float
- SQL 注入防护: 全程 GORM parameterized query，禁止 `Raw` 拼接字符串
- 启动即校验所有必需环境变量（`lifecycle/app.go`），缺失则 fast-fail

---

## 11. Observability

### 11.1 业务指标（Prometheus）

```go
// pkg/metrics/metrics.go
var (
    BillCreatedTotal    = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "tally_bill_created_total",
        Help: "Number of bills created",
    }, []string{"tenant_id", "sub_type"})

    AIQueryTotal = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "tally_ai_query_total",
        Help: "Number of AI chat queries",
    }, []string{"tenant_id", "model"})

    ActiveTenantsGauge = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "tally_active_tenants",
        Help: "Number of active tenants",
    })

    StockAlertTotal = promauto.NewCounter(prometheus.CounterOpts{
        Name: "tally_stock_alert_total",
        Help: "Number of low-stock alerts triggered",
    })
)
```

### 11.2 技术指标

- HTTP P50/P95/P99 延迟（gin-prometheus 中间件）
- 错误率（4xx/5xx 分类）
- NATS 消息处理延迟
- Hub LLM 调用延迟（从 adapter/hub 记录）

### 11.3 日志与链路

- **日志**: zerolog JSON 格式 → Loki（每条关键操作含 `tenant_id` / `bill_no` / `user_id`）
- **链路**: OpenTelemetry → Jaeger（trace_id 在 HTTP response header `X-Trace-Id` 透传）
- **告警**: Grafana Alertmanager（低库存触发次数异常、API 错误率 > 1%）

---

## 12. Performance Budget

引用 ux-benchmarks.md §7 前端预算，后端补充:

| 指标 | 目标 | 测量方式 |
|------|------|----------|
| 常规 API P95 | < 200ms | Prometheus histogram |
| 复杂报表 API P99 | < 500ms | Prometheus histogram |
| 表格首屏（1000行）| < 1s | Lighthouse LCP |
| JS Bundle（首屏）| < 150kb gzip | Bundle Analyzer |
| LCP | < 1.5s | Lighthouse |
| FID / INP | < 100ms | Web Vitals |
| 大表格 60 FPS | 虚拟滚动（>1000行）| Chrome DevTools |
| 单租户单据 | 1000+/日不卡 | 压测（k6）|
| 并发租户（MVP）| 100+ | 压测（k6）|
| AI 查询 P95 | < 3s（含 Hub 延迟）| 自定义 histogram |

**虚拟滚动**: 超过 1000 行启用 `@tanstack/react-virtual`（见 ux-benchmarks.md §7）。

**WAC 成本计算**: 增量更新（审核单据时只更新该商品快照），不做 jshERP 式全量重算，避免性能瓶颈（见 code-borrowing-plan.md §7.2）。

---

## 13. Deployment Architecture

### 13.1 K8s 资源清单

**Namespace**: `lurus-tally`（已在 lurus.yaml 注册）

```yaml
# deploy/k8s/base/backend-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tally-backend
  namespace: lurus-tally
spec:
  replicas: 1  # MVP
  selector:
    matchLabels:
      app: tally-backend
  template:
    spec:
      containers:
        - name: tally-backend
          image: ghcr.io/hanmahong5-arch/lurus-tally:main-<sha7>
          ports:
            - containerPort: 18200
          resources:
            requests:
              cpu: 100m
              memory: 256Mi
            limits:
              cpu: 500m
              memory: 512Mi
          env:
            - name: DATABASE_DSN
              valueFrom:
                secretKeyRef:
                  name: tally-secrets
                  key: DATABASE_DSN
            # 其他 env 见 §13.3

# deploy/k8s/base/web-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tally-web
  namespace: lurus-tally
spec:
  replicas: 1  # MVP
  template:
    spec:
      containers:
        - name: tally-web
          image: ghcr.io/hanmahong5-arch/lurus-tally:main-<sha7>  # 多阶段构建同一镜像
          ports:
            - containerPort: 3000
```

### 13.2 IngressRoute（Traefik）

```yaml
# deploy/k8s/base/ingress.yaml
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: tally-ingress
  namespace: lurus-tally
spec:
  entryPoints:
    - websecure
  routes:
    - match: Host(`tally.lurus.cn`)
      kind: Rule
      services:
        - name: tally-web
          port: 3000
    - match: Host(`tally.lurus.cn`) && PathPrefix(`/api`)
      kind: Rule
      services:
        - name: tally-backend
          port: 18200
  tls:
    secretName: lurus-cn-wildcard-tls
```

### 13.3 环境变量与 Secret

| 变量名 | 来源 | 说明 |
|--------|------|------|
| `DATABASE_DSN` | Secret | PostgreSQL DSN（schema: tally） |
| `REDIS_URL` | Secret | `redis://redis.messaging.svc:6379/5`（DB 5） |
| `NATS_URL` | Secret | `nats://nats.messaging.svc:4222` |
| `HUB_TOKEN` | Secret | Hub API Key |
| `PLATFORM_INTERNAL_KEY` | Secret | 2l-svc-platform Internal API Key |
| `KOVA_URL` | ConfigMap | Kova REST endpoint |
| `MEMORUS_URL` | ConfigMap | `http://memorus.lurus-system.svc:8880` |
| `ZITADEL_DOMAIN` | ConfigMap | `auth.lurus.cn` |
| `ZITADEL_CLIENT_ID` | Secret | OIDC Client ID |
| `INTERNAL_API_KEY` | Secret | tally 对外暴露的 Internal API Key |
| `JWT_AUDIENCE` | ConfigMap | OIDC audience 验证 |

### 13.4 HPA

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: tally-backend-hpa
  namespace: lurus-tally
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: tally-backend
  minReplicas: 1
  maxReplicas: 5
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
```

### 13.5 ArgoCD 注册

在根仓库 `deploy/argocd/appset-services.yaml` 追加 `lurus-tally` 条目，`targetRevision: main`，`prune: false`，`selfHeal: true`。

---

## 14. CI/CD

### 14.1 GitHub Actions 流水线

```
push/PR to main:
  1. lint (golangci-lint + bun run lint)
  2. typecheck (go vet + tsc --noEmit)
  3. test (go test -race ./... + bun test)
  4. build
     - 后端: CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -trimpath ./cmd/server
     - 前端: bun run build
  5. docker build (多阶段构建: 后端 scratch + 前端 standalone)
  6. trivy scan (Critical/High 漏洞阻断)
  7. push to GHCR (Public): ghcr.io/hanmahong5-arch/lurus-tally:main-<sha7>
  8. ArgoCD auto-sync（监听镜像 tag 变更）
```

**注意**: GHCR 仓库必须设为 Public（K3s 各 worker 节点无需单独配置 imagePullSecret）。

---

## 15. Migration & Rollout

### 15.1 首次部署流程

1. 运维确认 `pgvector` 扩展已在 `lurus-pg-rw` 安装（见 §18 Open Questions）
2. 创建 PostgreSQL schema: `CREATE SCHEMA tally;`
3. 运行 golang-migrate: `migrate -path migrations -database $DATABASE_DSN up`
4. 注册 Zitadel 资源: 创建 `lurus-tally` OIDC 客户端（confidential）
5. 部署 ArgoCD Application → stage (R6: 43.226.38.244)

### 15.2 数据导入支持

MVP 支持以下批量导入（Excel/CSV）:
- 商品/SKU 导入（`/api/v1/products/import`）
- 合作伙伴（供应商/客户）导入
- 期初库存导入（`stock_initial`）
- 格式: Excel (.xlsx) 模板下载 → 填写 → 上传解析

### 15.3 毕业标准（Stage → Prod）

- Stage 稳定运行 30+ 天，0 数据事故
- 5+ 早期客户完成核心流程（采购/销售/盘点）
- 所有接口 P95 < 200ms（通过 Prometheus 确认）
- 无 Critical/High 安全漏洞（Trivy 通过）

---

## 16. Cost Estimation

| 项目 | 估算 | 假设 |
|------|------|------|
| AI 调用（Hub，Pro 租户）| ~¥50/租户/月 | 每日 50 次查询，平均 1000 token/次 |
| K8s 资源（MVP 3 Pod）| ~¥200/月（R6 算力）| 100m CPU + 256Mi 各 × 3 |
| DB 存储（单租户 1 年）| ~2-5 GB | 10k SKU + 1000 单据/日 × 365 |
| MinIO 存储（商品图）| ~1 GB/租户/年 | 100 MB/月上传 |

---

## 17. Architectural Decision Records (ADR)

### ADR-001: 为什么选 Go（而非 Java/Node/Python）

**背景**: Lurus 全栈已是 Go，Hub/Platform/Lucrum 同栈。
**决策**: Go 1.25 + Gin + GORM。
**理由**: 全栈一致降低认知负担；CGO_ENABLED=0 scratch 镜像极简；性能远超 Java/Node 单线程；不引入 JVM 冷启动。
**替代方案放弃原因**: Java（JVM 启动慢，镜像大，Lurus 无 Java 经验）；Node（GIL-less 但单线程，不适合 CPU 密集报表计算）。

### ADR-002: 为什么用 PostgreSQL RLS 多租户（而非 Schema-per-tenant）

**背景**: 多租户隔离是核心安全需求，需要与 `lurus-pg-rw` 共用集群。
**决策**: 单 schema `tally` + PostgreSQL Row-Level Security。
**理由**: Schema-per-tenant 需要动态 DDL，不适合 golang-migrate；100+ 租户时 schema 数量爆炸；RLS 在数据库层强制隔离，应用层无法绕过；与 2l-svc-platform 同模式，经过生产验证。
**风险**: `current_setting` 未设置时 RLS 会阻止所有查询，需要确保每次请求都正确设置。

### ADR-003: 为什么选 Next.js 14 App Router（而非 Pages Router / SPA）

**背景**: 需要 SSR 提升首屏性能，同时需要复杂交互组件。
**决策**: Next.js 14 App Router，Server Component 优先，只在需要交互的叶子节点使用 Client Component。
**理由**: LCP < 1.5s 目标要求 SSR；App Router 支持流式渲染（Suspense）；BFF 层统一处理认证和 API 代理；`output: standalone` 简化容器化。

### ADR-004: 为什么选 shadcn/ui（而非 Ant Design / Semi UI）

**背景**: 需要顶级 SaaS 级别体验，对标 Linear/Vercel/Stripe Dashboard。
**决策**: shadcn/ui + Radix Primitives + Tailwind CSS + Framer Motion。
**理由**: Ant Design 视觉风格陈旧（B端企业感），与 Linear 级体验定位不符；shadcn/ui 是 Linear/Vercel 官方使用方案；代码所有权在本项目，无版本锁定风险；Tailwind v4 OKLCH 色彩空间支持精确暗黑模式（见 ux-benchmarks.md §8）。

### ADR-005: 为什么选 NATS PSI_EVENTS（而非 Kafka/RabbitMQ）

**背景**: 需要库存变更事件总线，与 Lurus 现有基础设施集成。
**决策**: NATS JetStream，stream `PSI_EVENTS`（已在 lurus.yaml 注册）。
**理由**: Lurus 全栈已有 NATS（LLM_EVENTS/LUCRUM_EVENTS/IDENTITY_EVENTS），无需引入新中间件；JetStream 提供持久化和 at-least-once 语义，满足库存事件可靠性要求；Kafka 运维复杂度远高于 NATS，对当前团队规模不合适。

### ADR-006: 为什么选 Kova Agent（而非自建 Agent 引擎）

**背景**: 需要补货 Agent、滞销预警 Agent 等自主 AI 决策能力。
**决策**: 复用 2b-svc-kova（已有），通过 kova-rest HTTP 调用。
**理由**: Kova 已实现 WAL 持久化、Agent 状态管理、工具调用注册，从零实现需要 3+ 个月；Lurus 战略是 Platform 产品组共用 AI 基础设施；Kova 已通过 1791 测试验证（见 memory RECENT MILESTONES）。

### ADR-007: 为什么 v1 只做 WAC 不做 FIFO

**背景**: 库存成本核算有 WAC（移动加权平均）和 FIFO（先进先出）两种主流方法。
**决策**: v1 只实现 WAC。
**理由**: jshERP 仅有 WAC，已被数十万中国 SMB 接受，说明 WAC 满足 MVP 需求；FIFO 需要 `stock_lot` 完整实现 + 复杂出库批次追踪逻辑；Karpathy 原则②：简单优先，只做解决问题所需最少代码。FIFO 预留数据模型（`stock_lot` 表 + `bill_item.lot_id`），v2 可实现。

### ADR-008: 为什么 v1 不做小程序/移动端

**背景**: 进销存有移动端（扫码入库、移动盘点）的真实需求。
**决策**: v1 仅 Web 端（`decision-lock.md §1` 锁定）。
**理由**: Web 端顶级体验是差异化核心，分散精力做小程序会导致两端都平庸；Web 端可在移动浏览器使用（响应式设计覆盖平板/手机查询需求）；条码枪通过 USB HID 在 PC 浏览器直接输入，覆盖仓库扫码场景；小程序需要微信审核和 AppID 注册，拖慢 MVP 速度。

---

## 18. Open Questions / TBD

| 问题 | 影响 | 负责人 | 截止 |
|------|------|--------|------|
| pgvector 是否已在 lurus-pg-rw 部署 | 商品 embedding 字段能否启用 | 运维 | W1（首次部署前） |
| Temporal worker 是否需要独立 Deployment | 高峰期 worker 是否影响 API 延迟 | Arch | v1 流量评估后 |
| 多租户并发上限（100+）的实际压测结果 | HPA 水位配置 | Dev | Stage 稳定后 |
| Zitadel `lurus-tally` OIDC 客户端注册 | 阻塞首次部署 | 运维 | W1 |
| Hub Token 是否需要独立申请 | AI 功能依赖 | PM | W1 |
| 金税四期 ISV（v2）选型（航信/百望云/诺诺）| 合规差异化 | PM | v2 规划阶段 |

---

## 附录：关键设计规则速查

| 规则 | 说明 |
|------|------|
| 所有金额字段 | `NUMERIC(18,4)`，禁止 float |
| tenant_id 传递 | JWT → middleware → `SET LOCAL app.tenant_id` → RLS 自动过滤 |
| 库存变更 | 只通过审核/反审核单据触发，禁止直接更新 stock_snapshot |
| 反审核规则 | status=4（完成）的单据禁止反审核，只能走红冲（新建 amendment 单） |
| 单据编号 | 格式 `{prefix}-{YYYYMMDD}-{sequence}`，通过 bill_sequence 生成 |
| 所有 LLM 调用 | 必须走 Hub（api.lurus.cn），禁止直连 OpenAI/DeepSeek |
| 所有 Agent 决策 | 必须走 Kova，禁止自建 Agent 逻辑 |
| 所有 RAG 历史 | 必须走 Memorus，禁止自建向量库 |
| License 合规 | jshERP/GreaterWMS 衍生代码必须保留 THIRD_PARTY_LICENSES/ 目录 |
