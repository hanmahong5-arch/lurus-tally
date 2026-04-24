# 智能进销存基座选型 — 综合决策

> 综合 GitHub / Google / Gitee 三份并行调研，输出可执行的选型决策与 90 天路线
> 日期: 2026-04-23 | 作者: Opus 主对话综合
> 输入: `github.md` (国际开源) + `google.md` (市场+趋势) + `gitee.md` (国内开源)

---

## 0. 决策一句话

**Fork `ruoyi-vue-pro` (MIT) 做 90 天 Java MVP 抢占窗口期；同步用 Go 自研 `2b-svc-psi`，数据模型借鉴 `jshERP` + `GreaterWMS`，AI 差异化层调用 Hub + Kova，6-9 个月后 Java MVP 退役切换到 Go 正式版。**

---

## 1. 三份调研的一致结论

| 议题 | GitHub Agent | Google Agent | Gitee Agent | 共识 |
|------|-------------|--------------|-------------|------|
| 直接 Fork 一个完整开源项目作为基座? | **不可能** —— License + Go + 完整功能三者真空 | —— | **不推荐** —— GPL/附加条款污染 | **明确不行** |
| 国内 SaaS 商业化的 License 红线? | GPL/AGPL 不可碰 | —— | 赤龙/盒木/JeecgBoot/Finer 全雷 | **MIT/Apache-2.0(无附加) 唯一安全** |
| 业务模型从哪里学? | GreaterWMS + ERPNext DocType + OFBiz | —— | jshERP (60+ 表) + ruoyi-vue-pro ERP 模块 | **GreaterWMS + jshERP 双蓝图** |
| 真正的差异化在哪? | AI 层是无法 fork 的护城河 | AI-native 12-18 月窗口期 + 金税四期合规 | 国内开源全无 AI 进销存案例 | **AI Agent + 合规 = 双护城河** |
| 技术栈建议? | Go + PostgreSQL + NATS 自研 | API-first + AI 层 SaaS 化 | Go 重写，Java 仅作 MVP | **自研 Go，复用 Lurus 全栈** |

---

## 2. 推荐方案：两段式 (双轨并行)

### 阶段 A — Java MVP 抢占窗口 (Day 0-90)

| 项 | 决策 |
|----|------|
| 仓库 | Fork `https://github.com/YunaiV/ruoyi-vue-pro` (MIT) → `2b-svc-psi-mvp` |
| 启用模块 | `yudao-module-erp` (采购/销售/库存 30 张表) + `yudao-module-pay` + 多租户 + Flowable |
| 集成点 | 内部 API 对接 `2l-svc-platform` (账户/钱包) —— **不用 yudao 自带账户** |
| 部署 | R6 Stage (43.226.38.244) —— 不上 R1 prod |
| 域名 | `tally-mvp.lurus.cn` |
| 目标 | 邀请 10-20 个早期客户验证需求；产出真实 PRD |
| AI 接入 | 仅做 Hub LLM 自然语言查询 demo (低成本验证客户付费意愿) |

**为何能选 ruoyi-vue-pro**: MIT 是国内项目里**唯一无附加条款**的 license；ERP 模块已生产可用；Vue3+Element Plus 演示效果好；微信小程序+支付内置；2025 年仍在迭代。

### 阶段 B — Go 自研正式版 (Day 60-270，与 A 重叠)

| 项 | 决策 |
|----|------|
| 仓库 | `2b-svc-psi/` (本目录) —— 全新 Go 服务 |
| 后端 | Go 1.25 + Gin + GORM + PostgreSQL + NATS (与 Hub/Lucrum 同栈) |
| 前端 | React 18 + Next.js + Bun + Semi UI (与 2b-svc-api/web 同栈，组件可复用) |
| 移动端 | Uniapp (微信小程序+APP，参考 yudao 设计但重写) |
| 数据模型 | **逐表对照 `jshERP` 60+ 张表**，翻译为 PostgreSQL DDL；**WMS 部分参考 GreaterWMS** |
| 多租户 | 复用 `2l-svc-platform` 租户体系，PostgreSQL Row-Level Security |
| AI 集成 | Hub (LLM 网关) + Kova (Agent 引擎) + Memorus (RAG 历史销量) |
| 部署 | R6 Stage 验证 → R1 Prod (达交付标准后) |
| 域名 | `tally.lurus.cn` |
| 命名空间 | `lurus-tally` (新) |
| 镜像 | `ghcr.io/hanmahong5-arch/lurus-tally:main-<sha7>` |

**目录结构** (沿用 Lurus Go 服务约定):
```
2b-svc-psi/
├── cmd/server/main.go
├── internal/
│   ├── domain/entity/       # product, sku, warehouse, po, so, stock, batch, sn, invoice…
│   ├── app/                  # purchase/, sales/, stock/, finance/, ai_agent/
│   ├── adapter/
│   │   ├── handler/          # router/v1, router/internal, router/agent
│   │   ├── repo/             # GORM repos
│   │   ├── nats/             # 跨平台库存事件订阅
│   │   ├── platform/         # 调 2l-svc-platform (gRPC + REST)
│   │   ├── hub/              # 调 2b-svc-api (LLM 网关)
│   │   ├── kova/             # 调 2b-svc-kova (Agent 决策)
│   │   ├── tax/              # 金税四期 ISV (航信/百望云/诺诺)
│   │   └── ecom/             # 抖店/拼多多/淘宝/京东 多渠道
│   ├── lifecycle/
│   └── pkg/
├── web/                      # React + Bun
├── miniapp/                  # Uniapp 微信小程序
├── migrations/               # PostgreSQL SQL
├── deploy/k8s/               # base + overlay/{stage,prod}
└── _bmad-output/planning-artifacts/
```

---

## 3. 差异化护城河 (Lurus 独有)

来自 Google 调研：国内 PSI 市场 AI-native 真空 12-18 个月。Lurus 必须做以下功能，否则与传统进销存无差异：

| 护城河 | 实现路径 | 依赖 Lurus 已有能力 |
|--------|---------|-------------------|
| **Kova 补货 Agent** | 历史销量 + lead time + 季节性 → Agent 自主生成补货单 (人工审核) | Kova (已有) |
| **自然语言查询** | "本周库存周转最慢 20 SKU 并出处置方案" → Hub LLM | Hub (已有) |
| **多渠道库存智能分配** | 各平台转化率 → 自动建议抖店/拼多多/京东库存比例 | 自建 + Hub |
| **金税四期 AI 巡检** | 数电票 XML 归档 + 四流一致校验 + 税负率异常推送 | 自建 + ISV (新建) |
| **滞销/爆款预测** | RAG 历史 + LLM → SKU 生命周期预测，2-4 周前预警 | Memorus + Hub |
| **Agent 询价** (中期) | 多供应商比价请求 → 自动汇总最优方案 | Kova (已有) |
| **平台账户/计费** | 订阅 + 钱包 + Agent 调用按量计费 | Platform (已有) |

**核心战略**: 基础 PSI CRUD 可考虑开源以拉开发者生态 (类似 Saleor/Medusa)；AI 层和 Agent 决策保持闭源 SaaS，按量计费走 Hub 计量体系。

---

## 4. 必须避开的坑 (License 红榜)

| 项目 | 风险 | 处置 |
|------|------|------|
| 赤龙ERP | GPL-2.0 | **禁止任何代码引入** |
| 盒木ERP (HimoolERP) | GPL-3.0 | **禁止任何代码引入** |
| JeecgBoot | Apache + "禁止开发竞品"附加条款 | **禁止 Fork** —— Lurus 做 ERP SaaS 直接踩雷 |
| Finer 进销存 | Apache + "禁止包装类似产品" | **仅作需求参考** |
| WMS-RuoYi | 修改版 Apache + 多租户运营需授权 | **禁止 Fork** —— 多租户是核心 |
| Vendure v3+ | GPL-3.0 + €8000/年商业授权 | **禁止使用** —— v2.x 已 EOL |
| ERPNext/Frappe | GPL-3.0 + 品牌商标限制 | **仅作功能参考** —— 不分发任何派生代码 |
| Odoo Community | LGPL-3.0 + Odoo 官方 SaaS 竞争 | **仅作功能参考** |
| 点可云 ERP | License 未公开 | **视同不可商用** |

**安全白名单** (确认无附加条款):
- `ruoyi-vue-pro` (MIT) → MVP fork
- `jshERP` (Apache-2.0 纯净) → 数据模型蓝图
- `GreaterWMS` (Apache-2.0) → WMS 数据模型
- `MedusaJS v2` (MIT) → 前端 inventory 模块可单独引入
- `Apache OFBiz` (Apache-2.0) → 数据模型参考 (UI 已陈旧)

---

## 5. 90 天行动计划

| Week | 任务 | 交付物 | Owner |
|------|------|--------|-------|
| W1 | PRD + Architecture (基于本 synthesis) | `_bmad-output/planning-artifacts/prd.md` + `architecture.md` | PM/Architect |
| W1 | Fork ruoyi-vue-pro → 本地启动 + 跑通 ERP 模块 demo | demo 录屏 | Dev |
| W2 | jshERP 60+ 表逐表分析 → PostgreSQL DDL 草稿 | `migrations/0001_init.sql` | Dev |
| W2-3 | MVP: ruoyi-vue-pro 接入 platform 账户体系 | 部署到 R6 stage | Dev |
| W3 | 金税四期 ISV 选型 (航信/百望云/诺诺) | 选型报告 + 报价 | Architect |
| W4 | MVP 上线 `tally-mvp.lurus.cn` + 邀请 5 位早期客户 | 客户访谈记录 | PM |
| W5-8 | Go 自研启动: domain + repo + handler + 多租户 | core/ 可跑通 | Dev |
| W6 | Hub LLM 自然语言查询 PoC (MVP 上嫁接) | demo + 客户反馈 | Dev |
| W8 | Kova 补货 Agent PoC | demo + 准确率数据 | Dev |
| W10 | 抖店/拼多多 OAuth + 库存同步 PoC | 库存事件流通 | Dev |
| W12 | 综合复盘 → 决策是否进入正式版冲刺 | Sprint Review | All |

---

## 6. 关键技术决策表

| 决策点 | 选项 | 决定 | 理由 |
|--------|------|------|------|
| 后端语言 | Go / Java / Python / Rust | **Go** | 与 Lurus 全栈一致，性能强，云原生友好 |
| 数据库 | PostgreSQL / MySQL | **PostgreSQL** | 与 Lurus 已有 lurus-pg-rw 共用，Row-Level Security 支持多租户 |
| 消息总线 | NATS / RabbitMQ / Kafka | **NATS** | 与 Lurus 全栈一致，已有 stream LLM_EVENTS/LUCRUM_EVENTS/IDENTITY_EVENTS |
| 多租户隔离 | 独立 DB / Schema / RLS | **RLS** | 同 lurus-platform 模式，账户体系直接复用 |
| API 风格 | REST / GraphQL / gRPC | **REST + 内部 gRPC** | 同 Hub/Platform 模式 |
| 前端 | React / Vue / Svelte | **React + Next.js + Bun + Semi UI** | 与 2b-svc-api/web 同栈 |
| 移动端 | Uniapp / RN / Flutter | **Uniapp** | 微信小程序 + APP 一份代码，国内 SMB 刚需 |
| 部署 | K3s / Docker Compose | **K3s** | 与 Lurus 全栈一致，ArgoCD GitOps |
| 落地服务器 | R1 prod / R6 stage | **R6 → R1** | 按 server-landing-policy 流程 |
| AI Agent | Kova / 自建 | **Kova** | 已有引擎，直接复用 Agent 编排能力 |
| LLM 调用 | Hub / 直连 OpenAI | **Hub** | 计费/路由/熔断已就绪 |
| RAG | Memorus / 自建向量库 | **Memorus** | 已有 AI 记忆引擎 |
| 金税接口 | 航信 / 百望云 / 诺诺 | **待选型** | W3 输出选型报告 |

---

## 7. 风险与未知项

| 风险 | 影响 | 缓解 |
|------|------|------|
| 金税四期 ISV 接入费用未明 | 影响 MVP 合规功能时间表 | W3 收集 3 家报价对比 |
| Kova Agent 调用延迟在扫码开单场景是否可接受 | 影响 AI 体验定位 | W6 PoC 阶段做基准测试，目标 P95 < 500ms |
| 抖店/拼多多/淘宝多平台 API 授权和稳定性 | 多渠道护城河兑现 | W10 PoC，必要时引入旺店通等聚合中间件 |
| GreaterWMS v3 (Bomiot) 重构后 v2 schema 是否仍可参考 | 数据模型蓝图风险 | 直接参考 v2 已稳定 schema，不依赖 v3 |
| ruoyi-vue-pro MVP 退役时机判断 | 切换成本 | 设硬约束：Go 版功能达 80% MVP 时立即切换 |
| 自研 Go 工期估算 (3-6 个月达 MVP) | 进度风险 | 严控范围，避免范围蔓延；优先 must-have 功能清单 |
| AI 预测精度对小客户 (SKU<1000) 是否有意义 | 差异化兑现 | W6-8 PoC 阶段用真实客户数据验证 |
| Lurus 已有 17 个服务在维护，新增 PSI 资源压力 | 团队负载 | 阶段 A 由 1-2 人负责 MVP；阶段 B 视客户验证结果再决定投入 |

---

## 8. 与 Lurus 既有体系的集成图

```
┌─────────────────────────────────────────────────────────┐
│              tally.lurus.cn (Web + 小程序)              │
└──────────────────┬──────────────────────────────────────┘
                   │ REST
┌──────────────────▼──────────────────────────────────────┐
│           2b-svc-psi  (Go, lurus-tally ns)              │
│  domain/  app/  adapter/                                │
└──┬──────┬──────┬──────┬──────┬──────────────┬──────────┘
   │      │      │      │      │              │
   ▼      ▼      ▼      ▼      ▼              ▼
┌─────┐┌─────┐┌─────┐┌─────┐┌────────┐  ┌──────────┐
│ Hub ││Kova ││Memo ││ Plat││  NATS  │  │ PostgreSQL│
│ LLM ││Agent││ RAG ││ form││ stream │  │  (RLS)    │
└─────┘└─────┘└─────┘└─────┘└────────┘  └──────────┘
   │                    │       │
   │                    │       ▼
   │                    │  PSI_EVENTS (新增 stream)
   │                    │       │
   │                    ▼       ▼
   │              账户/钱包   多平台同步
   │              订阅/计费   抖店/拼多多/淘宝
   ▼
DeepSeek/Claude/GPT (按量计费回到 Platform)
                        │
                        ▼
                  税务 ISV (金税四期)
```

---

## 9. 下一步动作 (本次会话结束后)

1. **用户决策点** (需确认):
   - 是否同意"两段式 (Java MVP + Go 自研)"路径？
   - 阶段 A MVP 是否同意部署到 R6？
   - 是否接受品牌名 **Lurus Tally** + 目录 `2b-svc-psi`？
   - 投入度: 1-2 人启动 MVP，还是直接 3-5 人冲 Go 自研？

2. **若同意，按顺序执行**:
   - `/pm prd 2b-svc-psi` —— 生成 PRD
   - `/architect` —— 输出 architecture.md
   - `/pm epics` —— 输出 epics.md
   - 更新根 `lurus.yaml` 加入 `2b-svc-psi` 服务定义
   - 更新根 `CLAUDE.md` Platform Map 加入新行
   - 更新 `doc/coord/contracts.md` + `service-status.md` 占位

---

## 附: 三份调研报告完整内容

- [GitHub 国际开源调研](./github.md) (3000+ 字)
- [市场格局与趋势](./google.md) (5000+ 字)
- [Gitee 国内开源调研](./gitee.md) (3500+ 字)
