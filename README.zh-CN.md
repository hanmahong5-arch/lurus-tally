中文 | [English](./README.md)

# Lurus Tally

面向中小企业（制造、批发、零售、电商）的 AI-native 进销存（PSI）系统 —— 不依赖重型 ERP 即可完成多仓库存管控。

Tally 是一套 Go + Next.js 的 Web 应用，而非表单堆砌的 CRUD 系统：内置自然语言库存查询的 AI 助手、Agent 建议式的补货流程，以及面向键盘操作的命令面板。后端按业务模块采用严格的四层架构（domain / app 用例 / repository / handler），基于 PostgreSQL 行级安全（RLS）实现多租户隔离；CI 在每次推送时跑单元测试、基于 `testcontainers` 的集成测试，以及前端 Vitest + Playwright 测试套件。当前已部署至 Lurus 的 staging 环境，并曾两次打包为可自托管的 Docker Compose 交付物装给外部客户（见 `deploy/customer/`）；尚未转正进入公司生产集群。

## 核心能力

- 多租户商品/SKU/计量单位/多仓库存管理，基于 PostgreSQL RLS 实现租户隔离（`internal/domain/product/`、`internal/domain/stock/`、`internal/domain/warehouse/`）
- 采购单与销售单流程（草稿 → 审批 → 取消/恢复），驱动库存变动及应付/应收账款（`internal/app/bill/`、`internal/adapter/handler/bill/`）
- 面向进出口卖家的跨境币种与汇率跟踪（`internal/domain/currency/`、`internal/app/currency/`）
- AI 助手 Drawer（流式对话）+ `⌘K` 命令面板，后端接入 LLM 网关客户端（`internal/pkg/llmclient/`、`internal/adapter/handler/ai/`、`web/components/ai-assistant/`、`web/components/command-palette/`）
- 补货建议、毛利/ABC 分析/滞销预警报表，以及每周「周一卡片」摘要（`internal/app/replenish/`、`internal/app/reports/`、`internal/app/digest/`）
- 苗木零售垂直行业包：200 种苗木字典 + 项目管理，按租户通过行业开关（industry flag）门控（`internal/domain/horticulture/`、`internal/app/project/`、`web/components/horticulture/`）
- Platform 侧订阅计费、OIDC 登录（厂商中立，可对接任意标准兼容的身份提供方）、可选的 Memorus AI 记忆召回 —— 未配置时均优雅降级为禁用状态（`internal/adapter/platform/`、`web/auth.ts`、`internal/pkg/memorusclient/`）

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/diagrams/architecture-dark.svg">
  <img alt="分层架构图：Tally 后端 adapter/app/domain 三层依赖方向总体向内收敛到零外部依赖的 domain 核心，但存在一个已记录的例外——部分 app 层用例会直接 import adapter 包；pkg 是仅供 adapter 与 app 使用的共享基础设施；PostgreSQL 行级安全通过 adapter 层建立的租户绑定连接强制租户隔离。" src="docs/diagrams/architecture.svg">
</picture>

## 快速开始

需要 Go 1.25+、Bun 1.2+，以及 Docker Desktop / Compose v2（用于本地依赖服务）。

```bash
git clone https://github.com/hanmahong5-arch/lurus-tally.git && cd lurus-tally
cp .env.example .env             # 本地开发默认值可直接使用，无需修改

# 后端 —— 通过 Docker 启动 Postgres + Redis + NATS，再启动 Go 服务
make dev
curl http://localhost:18200/internal/v1/tally/health
# 预期输出: {"service":"lurus-tally","status":"ok","version":"dev"}

# 前端（另开一个终端）
cd web
bun install
bun run dev                      # http://localhost:3000
```

```bash
# 测试
go test -count=1 ./...                                 # 后端单元测试
go test -v -count=1 -tags=integration -race ./...      # 后端集成测试（需要 Docker）
cd web && bun run test                                 # 前端 Vitest

# 生产构建
CGO_ENABLED=0 GOOS=linux go build -trimpath -o tally-backend ./cmd/server
cd web && bun run build
```

完整 target 列表见 `Makefile`（`migrate-up`、`migrate-down`、`lint`、`docker-build`、`coverage` 等）。

## 架构

后端按业务模块（`account`、`product`、`stock`、`bill`、`billing`、`horticulture`、`project` 等）采用统一的四层包结构：

```
internal/
├── domain/<module>/     # 实体、校验、领域常量 —— 无外部依赖
├── app/<module>/        # 用例 (Create/Get/List/Update/Delete/Restore) + Repository 接口定义
├── adapter/
│   ├── repo/<module>/   # Repository 实现 —— database/sql + pgx/v5 手写 SQL，无 ORM
│   ├── handler/<module>/ # Gin HTTP handler + 请求/响应 DTO
│   ├── middleware/       # 鉴权、租户连接绑定、幂等、请求体大小限制、请求超时、metrics
│   ├── platform/         # Lurus Platform 客户端（identity / billing / notification）
│   └── nats/             # NATS JetStream 发布端（PSI_EVENTS stream）
├── pkg/                 # config、logger、llmclient、memorusclient、version 等
└── lifecycle/app.go     # 依赖注入：repo -> 用例 -> handler -> router.New(...)

cmd/
├── server/              # 主 API 服务入口
├── tally-mcp/           # MCP (Model Context Protocol) 服务二进制
└── kill-switch-monitor/ # 与 API 镜像随行发布的独立监控二进制

web/
├── app/(dashboard)/     # 需登录的应用路由（Next.js App Router route group）
├── app/(auth)/          # 登录/鉴权路由
├── components/          # ai-assistant、command-palette、horticulture、project、pos、draft、undo 等
├── lib/                 # api client、billing、draft（IndexedDB）、undo 撤销栈
└── auth.ts              # NextAuth.js OIDC (PKCE) 配置

migrations/              # golang-migrate SQL migration，通过 embed.go 内嵌进二进制
deploy/
├── k8s/                 # Kustomize base + stage/prod overlay
└── customer/            # 可自托管的 Docker Compose 安装包（INSTALL.md、acceptance.sh）
```

数据库访问全部走 `database/sql` + `jackc/pgx/v5` 手写 SQL（无 ORM）；租户隔离由 PostgreSQL 行级安全（RLS）在数据库层强制执行，`internal/adapter/middleware/tenant_db.go` 会将每个请求绑定到携带 `app.tenant_id` 的数据库连接上。

上图所示的向内依赖方向是目标形态而非绝对约束：少数 `app/<module>` 用例会直接 import `adapter/middleware`（例如上报 metrics）或某个具体的 `adapter/repo/<module>` 包，而不是走注入的 `Repository` 接口——这是一个已记录在案的分层例外，而非隐藏问题。

## 配置

完整参考见 `.env.example`。关键环境变量：

| 变量 | 是否必填 | 默认值 | 说明 |
|---|---|---|---|
| `DATABASE_DSN` | 是 | — | PostgreSQL DSN；应包含 `search_path=tally` |
| `REDIS_URL` | 是 | — | Redis 连接地址（DB 5 为 Tally 保留） |
| `NATS_URL` | 是 | — | NATS 地址；`PSI_EVENTS` JetStream stream 会在启动时自动创建 |
| `PORT` | 否 | `18200` | HTTP 监听端口 |
| `LOG_LEVEL` | 否 | `info` | `debug` \| `info` \| `warn` \| `error` |
| `GIN_MODE` | 否 | `release` | `debug` \| `release` |
| `SHUTDOWN_TIMEOUT` | 否 | `5s` | 优雅关闭超时时间 |
| `MIGRATE_ON_BOOT` | 否 | `true` | 启动时执行内嵌的 migration |
| `PLATFORM_INTERNAL_KEY` | Platform 功能需要 | 空 | 调用 platform-core 的 Bearer key |
| `PLATFORM_BASE_URL` | Platform 功能需要 | `http://platform-core.lurus-platform.svc:18104` | platform-core 内网地址；未设置时计费相关调用会报错 |
| `NEWAPI_API_KEY` | 否 | 空 | LLM 网关（newapi）API key；留空时 AI 功能会干净地禁用 |
| `NEWAPI_BASE_URL` | 否 | `https://newapi.lurus.cn/v1` | LLM 网关地址 |
| `KOVA_URL` | 否 | 空 | Kova Agent 执行端点；留空时 Agent 功能禁用 |
| `MEMORUS_BASE_URL` | 否 | `http://memorus-r.lurus-system.svc:8880/api/v1` | AI 记忆引擎地址（须含 `/api/v1`） |
| `MEMORUS_API_KEY` | 否 | 空 | 留空则禁用记忆召回；AI 功能本身仍可用 |
| `OIDC_ISSUER` | 鉴权需要 | 空 | OIDC issuer（可为裸域名或完整 URL）；留空且未同时设置 `TALLY_DEV_MODE=true` 时服务**会拒绝启动**，因为留空会让整个 `/api/v1` 处于未鉴权状态 |
| `OIDC_CLIENT_ID` | 鉴权需要 | 空 | Confidential OIDC client ID |
| `OIDC_AUDIENCE` | 鉴权需要 | 空 | 预期的 `aud` claim |
| `OIDC_JWKS_PATH` | 否 | `/oauth/v2/keys` | 拼接在 issuer 后的 JWKS 路径 |
| `SEED_NURSERY_DICT` | 否 | `false` | 启动时加载 200 种苗木种子数据（幂等） |
| `TALLY_NOTIFY_WECOM_WEBHOOK` / `TALLY_NOTIFY_DINGTALK_WEBHOOK` / `TALLY_NOTIFY_FEISHU_WEBHOOK` | 否 | 空 | 群机器人通知 webhook（当前尚未接入 DI 依赖图，详见 `.env.example` 注释） |

## API 概览

所有业务路由挂载在 `/api/v1` 下（见 `internal/adapter/handler/router/router.go`）；`/internal/v1/tally/health` 与 `/internal/v1/tally/ready` 是免鉴权的存活/就绪探针，`/internal/v1/metrics` 在 bearer-token 鉴权后暴露 Prometheus 格式的 LLM 可观测性指标。

| 分组 | 示例 |
|---|---|
| 鉴权与租户资料 | `GET /api/v1/me`、`POST /api/v1/tenant/profile`、`/api/v1/auth/pats` 下的 PAT CRUD |
| 商品与单位 | `/api/v1/products`、`/api/v1/units` |
| 库存 | `GET /api/v1/stock/snapshots`、`/api/v1/stock/movements`、`/api/v1/stock/alerts/low-stock`（只读；变更走单据审批） |
| 采购单/销售单 | `/api/v1/purchase-bills`、`/api/v1/sale-bills`、`/api/v1/sale-bills/quick-checkout` |
| 收付款与计费 | `/api/v1/payments`、`/api/v1/billing/overview`、`/api/v1/billing/subscribe` |
| AI 助手 | `POST /api/v1/ai/chat`（SSE）、`/api/v1/ai/plans`（提案/确认/取消/回退） |
| 苗木与项目 | `/api/v1/nursery-dict`、`/api/v1/projects` |
| 供应商与仓库 | `/api/v1/suppliers`、`/api/v1/warehouses` |
| 报表与搜索 | `/api/v1/reports/{gross-margin,abc,dead-stock,sales-top}`、`/api/v1/search`、`/api/v1/weekly-summary` |
| 导出与导入 | `/api/v1/exports/{bills,stock,payments}.csv`、`/api/v1/imports/orders` |
| 账户中心 | `/api/v1/account/{sessions,audit-log,profile,avatar}` |
| 引导流程 | `/api/v1/onboarding/{seed-demo,clear-demo}` |

所有写操作路由都要求携带 `Idempotency-Key` 请求头（由 `internal/adapter/middleware/idempotency_require.go` 强制），与可选的 Redis 去重层是否启用无关。

## 开发约定

摘自本仓开发约定文档：

- 后端：`go run ./cmd/server`（`:18200`）；构建命令 `CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -trimpath`。
- 前端：`bun install` / `bun run dev` / `bun run build` / `bun run lint` —— 一律用 Bun，禁 npm/yarn/npx/node。
- License：Apache-2.0（见 `LICENSE`）；第三方许可证声明见 `NOTICE` 与 `THIRD_PARTY_LICENSES/`。
- 新增后端模块沿用上述四层包结构；新增一个 handler 会改变 `router.New(...)` 的函数签名，属于 breaking change，需要在同一个 commit 内同步更新测试与 DI 装配代码。

## 关联项目

Tally 是 Lurus 平台 monorepo 中的服务之一，运行时消费以下若干共享能力：

- **platform-core**（`2l-svc-platform`）—— 身份、订阅计费、通知，通过 `internal/adapter/platform/` 调用
- **LLM 网关** —— 平台的 OpenAI 兼容推理网关，由 `internal/pkg/llmclient/` 调用以驱动 AI 助手（`NEWAPI_BASE_URL` / `NEWAPI_API_KEY`）
- **Memorus** —— AI 记忆/召回服务，通过 `internal/pkg/memorusclient/` 接入，为可选依赖
- **Kova** —— 补货 Agent 所依赖的 Agent 执行运行时（端点通过 `KOVA_URL` 配置；尚未接入具体 handler）

## 开源血统

数据模型与 schema 设计借鉴 **jshERP**、**GreaterWMS**（均 Apache-2.0）与 **Apache OFBiz**（设计模式参考）；前端组件基于 **shadcn/ui + Radix**（MIT），并参考 **Medusa.js v2**（MIT）。完整归属见 `NOTICE`；第三方依赖许可证分别收集于 `THIRD_PARTY_LICENSES/`（Go module）与 `web/package.json`（npm 包）。
