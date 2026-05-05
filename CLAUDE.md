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

**STAGE 部署阻塞**: NEWAPI_KEY secret 未注入 + Zitadel confidential client 待注册 + ArgoCD ApplicationSet 仅支持 R1 in-cluster destination（需扩 multi-cluster 才能落 R6）。

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
| llm-inference | api.lurus.cn (Hub) | Hub API Key |
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

## BMAD (已完成)

| Resource | Path |
|----------|------|
| Decision Lock | `./_bmad-output/planning-artifacts/decision-lock.md` |
| PRD | `./_bmad-output/planning-artifacts/prd.md` (~10k 字) |
| Architecture | `./_bmad-output/planning-artifacts/architecture.md` (~14k 字, 27 张表 DDL) |
| Epics | `./_bmad-output/planning-artifacts/epics.md` (20 epics / 100+ stories) |
| Stories (done) | `./_bmad-output/planning-artifacts/stories/{1.1-1.7,28.1-28.2}*.md` |
| 选型调研 | `./_research/` (4 份) |
