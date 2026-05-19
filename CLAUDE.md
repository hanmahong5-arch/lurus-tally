# Lurus Tally (2b-svc-psi)

AI-native 智能进销存 SaaS (Web only)。面向中小企业（制造/批发/零售/电商）。Platform 产品组 (P0)。

**差异化**: ⌘K Command Palette · AI 助手 Drawer · Kova 补货 Agent · Hub 自然语言查询 · 暗黑模式默认 · Linear 级体验。

- Namespace / Port: `lurus-tally` / 18200
- Domain: `tally.lurus.cn` (prod) / `tally-stage.lurus.cn` (stage on R6)
- DB: PostgreSQL schema `tally` (RLS), Redis DB 5, NATS stream `PSI_EVENTS`
- Repo: `hanmahong5-arch/lurus-tally`
- License: Apache-2.0（代码白名单：jshERP / GreaterWMS / OFBiz / MedusaJS / shadcn-ui；红榜 GPL/JeecgBoot/Vendure-v3+ 禁用）

## Current State

🚧 Epic 1 closed + Billing integration (Story 10.1) + 进销存核心 vertical slice 闭环 (Epic 4/5/6/7 后端齐 + Stock REST + 库存 UI). Migration head: 30. Images: `ghcr.io/hanmahong5-arch/lurus-tally-{backend,web}`. Sprint 详情 → `./_bmad-output/planning-artifacts/sprint-status.yaml`.

**STAGE 运行状态** (2026-05-18 实测): `tally-stage.lurus.cn` 已在 R6 跑 24d, `tally-backend` + `tally-web` Running, `/internal/v1/tally/ready` 返 200。ArgoCD 不接管(ADR-0006), 部署走人工 `ssh + kubectl apply -k`。Secret 9 keys 已注入, 但 `MEMORUS_API_KEY` / `NEXTAUTH_URL` / `ZITADEL_ISSUER` / `ZITADEL_CLIENT_SECRET` 缺 — 需配真实端到端登录测试确认是否阻塞。

**Billing**: `/api/v1/billing/{overview,subscribe}`. Env: `PLATFORM_INTERNAL_KEY` + `PLATFORM_BASE_URL`(默认 `http://platform-core.lurus-platform.svc:18104`); 空 → 501.

**Platform 集成**: identity / billing / memory / notification / agent 已接（5/7）；llm-inference 走 Hub 直连，auth 走 Zitadel OIDC。

**Stock REST**: GET `/api/v1/stock/{snapshots,snapshots/:product/:warehouse,movements}`（read-only；mutation 经 bill approval）。

**NATS PSI_EVENTS**: 强类型 6 个 typed publisher（stock/bill/alert）+ 通用 Publish 向后兼容。Schema 见 `doc/coord/contracts.md`.

## Tech Stack (locked)

| Layer | Choice |
|-------|--------|
| Backend | Go 1.25 + Gin + GORM |
| DB | PostgreSQL (RLS 多租户隔离) |
| Cache | Redis DB 5 |
| Events | NATS JetStream stream `PSI_EVENTS` |
| Frontend | Next.js 14 + shadcn/ui + Tailwind + Framer Motion + Bun |

## Directory (one level)

- `cmd/` — entry
- `internal/{domain,app,adapter,lifecycle,pkg}` — 见根 CLAUDE.md Go convention
- `web/` — Next.js (Bun)
- `deploy/` — K8s manifests
- `_research/` — 选型调研
- `_bmad-output/` — planning artifacts

## Commands

```bash
# Backend dev
go run ./cmd/server                         # port 18200
go test -v ./...

# Build
CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -trimpath -o tally ./cmd/server

# Frontend
cd web && bun install && bun run dev
cd web && bun run build && bun run lint
```

## Cross-service Dependencies (capabilities consumed)

| Capability | Provider | Auth |
|-----------|----------|------|
| identity | platform-core.lurus-platform.svc:18104 | `INTERNAL_API_KEY` |
| billing | platform-core (wallet/subscription) | same |
| llm-inference | newapi.lurus.cn | NewAPI Key |
| memory | memorus.lurus-system.svc:8880 | `MEMORUS_API_KEY` |
| agent-execution | kova-rest:3002 | Kova API Key |
| notification | notification.lurus-platform.svc:18900 | `INTERNAL_API_KEY` |
| auth | auth.lurus.cn (Zitadel OIDC) | PKCE |

## Known Issues

- 金税四期 ISV v2 选型待定
- ArgoCD ApplicationSet template 硬编码 in-cluster destination（R1 PROD 唯一）；R6 STAGE 部署需先注册 multi-cluster

## V1 Scope Decision (2026-05-05)

**砍枝**: Epic 12-16（边缘端 Go binary / 同步引擎 / 冲突裁决 UI / PWA 离线壳 / 边缘节点管理）整体延后到 **V2.5**。理由：V1 客户是 CN 中小企业网络稳定，离线非刚需；DL-5 离线字段已预留，未来激活成本低。

**V1 聚焦**: Epic 1-11 核心进销存闭环 + Profile 机制 + 双 persona（cross_border / retail）；Epic 17 (AI 补货) / 19 (Hub 自然语言查询) / 20 (Memorus 智能记忆) 作为差异化护城河。

## V1.5 Roadmap (2026-05-18, R1 = 2026 H2)

详细路线图 → `./_bmad-output/planning-artifacts/roadmap-v1.5.md`（12 sprint × 2 周, 1050h, 4 阶段 16 feature）。

### ICP (第一个客户画像)
**跨境电商 3-8 人精品工作室**(深圳/广州/义乌/杭州)。年营收 ¥300-1500 万, 80-400 活跃 SKU(精品非铺货), Amazon US/EU + Shopify 主战场。当前用 Excel + 钉飞表格 + Amazon 后台手工拉报表; 试过店小秘/马帮"太重", 金蝶精斗云对不上账。**不要的**: 5000+ SKU 铺货大卖 / 纯 1688 内贸 / 年营收 5000 万+。

### 价格锚
- 年付 ¥299/月 (¥3588/年) / 月付 ¥399/月
- **前 10 客户免费 90 天**(FBA 周期 60-90 天, 14 天看不到 aha)

### 北极星 + Anti-metric
- **North Star**: Weekly Active Decisions (WAD) = 每周用户基于 Tally 建议实际执行的 PO 数量。90 天目标累计 ≥ 80 WAD。
- **Anti-metric**: 功能数量增长 ≤ 3 个/月。某周新增 5 功能但 WAD 没涨 = 自嗨。

### 3 Kill Switch 信号 (任一红 2 周连续 = pivot 会议)
1. 前 10 客户 onboarding 完成率 < 40%
2. 第 45 天 AI 建议 PO 实际下单率 < 20%(±20% 金额内)
3. 90 天试用付费转化率 < 30%

### 3 条产品红线(违反需在代码里写明"为什么破例")
1. **AI 不是卖点, 自动化才是** — 卖"老板每周省 8 小时", 不卖"用了大模型"
2. **⌘K 是肌肉记忆, 不是搜索框** — 200ms 内出 command + entity + AI 三栏; DAU 渗透必须高过侧边导航
3. **每一次 AI 写库存都可预览/可撤销/可审计** — Preview Before Execute + Audit Trail + 30s Cmd+Z

## BMAD (已完成)

| Resource | Path |
|----------|------|
| Decision Lock | `./_bmad-output/planning-artifacts/decision-lock.md` |
| PRD | `./_bmad-output/planning-artifacts/prd.md` (~10k 字) |
| Architecture | `./_bmad-output/planning-artifacts/architecture.md` (~14k 字, 27 张表 DDL) |
| Epics | `./_bmad-output/planning-artifacts/epics.md` (20 epics / 100+ stories + V2.5 横向带 E21-E27) |
| V1.5 Roadmap | `./_bmad-output/planning-artifacts/roadmap-v1.5.md` (12 sprint / 16 feature / ICP+GTM+假设) |
| V2.5 UX Supplement | `./_bmad-output/planning-artifacts/roadmap-ux-supplement.md` |
| Stories (done) | `./_bmad-output/planning-artifacts/stories/{1.1-1.7,28.1-28.2}*.md` |
| 选型调研 | `./_research/` (4 份) |

---
_BMAD artifacts last review: 2026-05-18 — governance: `lurus/doc/audit/2026-05-18-bmad-output-stale.md`._
