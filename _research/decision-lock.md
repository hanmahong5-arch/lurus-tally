# 决策锁定 (Decision Lock) — Lurus Tally

> 日期: 2026-04-23 | 状态: **LOCKED** | 决策人: 用户
> 本文件锁定不可变决策。后续所有 PRD/Architecture/Story 必须遵守。

---

## 1. 路线决策 (取代 synthesis.md §2 的两段式)

| 项 | 决策 | 备注 |
|----|------|------|
| 路线 | **直接 Go 自研** | **取消 Java MVP 路线** |
| 端 | **仅 Web 端** | 不做小程序、不做 APP、不做 PDA、不做 Uniapp |
| 体验定位 | **客户体验顶级** | 对标 Linear / Vercel / Stripe Dashboard / Notion 级别 |
| 实施策略 | **能抄就抄，抄不到就重新设计后集成** | License 安全的项目优先抄 (jshERP / GreaterWMS / OFBiz) |
| 团队作业 | **多 BMAD Agent 并行** | 按最优流程：UX 调研 → PRD → Arch → Epics → Story → Dev |

## 2. 锁定的技术栈

| 层 | 选型 | 不容妥协的理由 |
|----|------|--------------|
| 后端语言 | **Go 1.25** | Lurus 全栈一致 |
| Web 框架 | **Gin** | Lurus Hub/Lucrum 同栈 |
| ORM | **GORM** | Lurus 标准 |
| 数据库 | **PostgreSQL** + Row-Level Security | 多租户 + 与 lurus-pg-rw 共用 |
| 缓存 | **Redis** (DB 5, 待分配) | Lurus 标准 |
| 消息 | **NATS** stream `PSI_EVENTS` | Lurus 标准 |
| 前端语言 | **TypeScript** | 强类型 |
| 前端框架 | **Next.js 14 (App Router)** | SSR + 客户端组件混合，最佳体验 |
| UI 库 | **shadcn/ui + Radix Primitives + Tailwind CSS** | Linear/Vercel 同款方案 |
| 动效 | **Framer Motion** | 顶级动效标准 |
| 图表 | **Recharts** + **Tremor** | Stripe Dashboard 风格 |
| 表格 | **TanStack Table v8** | 性能最强 |
| 表单 | **React Hook Form + Zod** | 类型安全 |
| 状态 | **Zustand + TanStack Query** | 轻量+强大 |
| 包管理 | **Bun** | Lurus 强制 |
| AI 集成 | Hub LLM 网关 + Kova Agent + Memorus | 复用 Lurus 既有能力 |

## 3. 锁定的产品边界 (MVP 范围)

**Must-Have (MVP 必须有)**:
1. 多租户账户接入 (调 2l-svc-platform)
2. 商品 / SKU / 多仓库 / 库存 (含批次 + 序列号)
3. 采购单 → 入库单 → 应付
4. 销售单 → 出库单 → 应收
5. 库存调拨 / 盘点
6. 财务对账 (应收应付台账)
7. 多渠道库存视图 (即使先不接电商 API，也要预留模型)
8. **Hub LLM 自然语言查询** (差异化必备)
9. **Kova 补货 Agent** (差异化必备 — 哪怕第一版只是建议)
10. 报表 (库存周转 / ABC 分析 / 滞销预警)

**Defer 到 v2 (MVP 不做)**:
- 金税四期 ISV 集成 (留接口，v2 选型对接)
- 抖店/拼多多/淘宝 OAuth 同步 (留接口)
- 生产 BOM / MES
- POS 收银
- HR / CRM
- 出海多币种

## 4. 锁定的客户体验原则

借鉴 Linear/Vercel/Stripe 等顶级 SaaS:

1. **键盘优先** — 所有高频操作可走 ⌘K Command Palette
2. **<100ms 响应** — 乐观更新 + 骨架屏 + 预加载
3. **零模态弹窗** — 用 Slide-over Sheet 代替 Modal
4. **AI 无处不在** — 每个表格/页面都有 AI 助手按钮
5. **空状态有引导** — 不让用户看到空白页
6. **错误可恢复** — 所有操作可撤销 (Undo)
7. **暗黑模式默认** — Linear 风格
8. **极简设计** — 留白 + 等宽数字 + 优雅动效
9. **实时协作** — 多人在线状态 + 实时数据同步 (NATS WebSocket)
10. **响应式** — Web 端但要在平板/桌面上都最佳

## 5. 锁定的部署策略

| 环境 | 域名 | 服务器 |
|------|------|--------|
| Stage | `tally-stage.lurus.cn` | R6 (43.226.38.244) |
| Prod | `tally.lurus.cn` | R1 (100.98.57.55) — 达交付标准后 |

| 资源 | 分配 |
|------|------|
| Namespace | `lurus-tally` |
| DB Schema | `tally` (在 lurus-pg-rw) |
| Redis DB | 5 |
| NATS Stream | `PSI_EVENTS` |
| Image | `ghcr.io/hanmahong5-arch/lurus-tally:main-<sha7>` |
| Port | 后端 18200, 前端走 Next.js 内嵌 (3000) |

## 6. 团队 Agent 编排 (BMAD 流程)

| 波次 | Agent | 任务 | 输出 | 前置 |
|------|-------|------|------|------|
| **W1.1** | bmad-researcher (sonnet) | UX 标杆调研 (Linear/Vercel/Stripe/Notion/Shopify Admin) | `_research/ux-benchmarks.md` | 无 |
| **W1.2** | bmad-researcher (sonnet) | 抄代码可行性分析 (jshERP + GreaterWMS + ERPNext schema 全表分析) | `_research/code-borrowing-plan.md` | 无 |
| **W2.1** | general-purpose (sonnet) | 编写完整 PRD | `_bmad-output/planning-artifacts/prd.md` | W1.1 + W1.2 |
| **W2.2** | general-purpose (sonnet) | 编写完整 Architecture | `_bmad-output/planning-artifacts/architecture.md` | W1.1 + W1.2 |
| **W3** | bmad-epics | 拆 Epics + Story 大纲 | `_bmad-output/planning-artifacts/epics.md` | W2.1 + W2.2 |
| **W4** | bmad-sm | 写第一个 Story (项目骨架) | `_bmad-output/stories/story-1.1.md` | W3 |
| **W5** | bmad-dev | 实现 Story #1 (TDD) | 真实代码 + 测试 | W4 |
| **W6** | bmad-sm + bmad-dev (循环) | Story #2, #3, ... | 持续 | W5 |

平台级动作 (与 W3-W4 并行):
- 更新根 `lurus.yaml` 加入 `2b-svc-psi` 服务定义
- 更新根 `CLAUDE.md` Platform Map
- 占位 `doc/coord/contracts.md` + `service-status.md`
- 创建独立 git repo `hanmahong5-arch/lurus-tally` (待用户授权)

## 7. 不可变约束

- License 红榜永不解禁: GPL/AGPL/JeecgBoot 附加禁制系列项目，**任何代码都不准引入**
- 安全白名单: jshERP (Apache-2.0 纯净) + GreaterWMS (Apache-2.0) + OFBiz (Apache-2.0) + MedusaJS (MIT) + shadcn/ui (MIT)
- 抄代码必须保留原 LICENSE 文件 + 在 Lurus README 致谢
- 所有 LLM 调用走 Hub (不直连 OpenAI/DeepSeek)
- 所有 Agent 决策走 Kova
- 所有 RAG 走 Memorus
