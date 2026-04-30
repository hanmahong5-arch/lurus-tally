# Lurus Tally (2b-svc-psi)

AI-native 智能进销存 SaaS (Web only)。面向中小企业（制造/批发/零售/电商）。Platform 产品组 (P0)。

**差异化**: ⌘K Command Palette · AI 助手 Drawer · Kova 补货 Agent · Hub 自然语言查询 · 暗黑模式默认 · Linear 级体验。

- Namespace / Port: `lurus-tally` / 18200
- Domain: `tally.lurus.cn` (prod) / `tally-stage.lurus.cn` (stage on R6)
- DB: PostgreSQL schema `tally` (RLS), Redis DB 5, NATS stream `PSI_EVENTS`
- Repo: (待创建) `hanmahong5-arch/lurus-tally`
- License: Apache-2.0（代码白名单：jshERP / GreaterWMS / OFBiz / MedusaJS / shadcn-ui；红榜 GPL/JeecgBoot/Vendure-v3+ 禁用）

## Current State

🚧 Epic 1 closed (Stories 1.1–1.7) + Billing integration shipped (Story 10.1, 待 R6 STAGE 部署). Migration head: 12 (27 表 + 1 MV + 11 RLS). Images: `ghcr.io/hanmahong5-arch/lurus-tally-{backend,web}`. Sprint 详情 → `./_bmad-output/planning-artifacts/sprint-status.yaml`.

**Billing**: `/api/v1/billing/{overview,subscribe}`. Env: `PLATFORM_INTERNAL_KEY` + `PLATFORM_BASE_URL`(默认 `http://platform-core.lurus-platform.svc:18104`); 空 → 501.

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

- Repo 未创建（待用户授权 `hanmahong5-arch/lurus-tally`）
- 金税四期 ISV v2 选型待定

## BMAD (已完成)

| Resource | Path |
|----------|------|
| Decision Lock | `./_bmad-output/planning-artifacts/decision-lock.md` |
| PRD | `./_bmad-output/planning-artifacts/prd.md` (~10k 字) |
| Architecture | `./_bmad-output/planning-artifacts/architecture.md` (~14k 字, 27 张表 DDL) |
| Epics | `./_bmad-output/planning-artifacts/epics.md` (11 epics / 68 stories) |
| Story 1.1 | `./_bmad-output/planning-artifacts/stories/1.1-*.md` (done) |
| 选型调研 | `./_research/` (4 份) |
