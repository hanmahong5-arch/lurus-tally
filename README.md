# Lurus Tally (2b-svc-psi)

> 智能进销存系统 (Smart Purchase-Sales-Inventory)

**状态**: Story 1.1 骨架完成 — Go 服务可启动并通过健康检查
**产品组**: Platform (P0) 候选 · **目标客群**: 中小企业 B2B (制造/批发/零售/电商)

## Vision

AI-native 库存智能体（非传统 CRUD 进销存）:
- **预测**: LLM + 时序预测自动补货 / 滞销预警
- **决策**: 智能议价、动态定价、多渠道库存最优分配
- **集成**: 与 Lurus Hub (LLM 网关) + Memorus (AI 记忆) + Platform (账户/计费) 打通
- **多端**: Web + 微信小程序 + 移动端，覆盖中国 SMB 场景

## Status

Phase 1-7 全部 DONE (2026-04-23): 命名与骨架 → 开源基座调研 (3 Agent 并行) → 选型决策 (锁定 Go 自研 + Web Only + 顶级 UX) → UX 标杆调研 + 抄代码计划 → PRD + Architecture → Epics 拆分 → Story 1.1 Go 服务骨架。锁定决策见 `_research/decision-lock.md`。

## Quick Start

**Prerequisites**: Go 1.25+ · Bun 1.2+ (for `make dev-web`) · Docker Desktop + Compose v2 (for `make dev` / `make test-integration`) · golangci-lint (for `make lint`)

```bash
git clone https://github.com/hanmahong5-arch/lurus-tally.git && cd lurus-tally
cp .env.example .env       # defaults work as-is for local dev — no edits required
make dev                   # start Postgres + Redis + NATS containers, then Go backend
curl http://localhost:18200/internal/v1/tally/health
# Expected: {"service":"lurus-tally","status":"ok","version":"dev"}
make dev-web               # (2nd terminal) Next.js frontend at http://localhost:3000
make dev-stop              # tear down Docker services when done
```

## Make Targets

| Target | Description |
|--------|-------------|
| `make dev` | Start Docker services (Postgres + Redis + NATS) then Go backend |
| `make dev-web` | Start Next.js dev server at http://localhost:3000 |
| `make dev-stop` | Stop and remove Docker Compose services |
| `make migrate-up` | Apply all pending migrations against `.env` DATABASE_DSN |
| `make migrate-down` | Roll back the most recent migration |
| `make seed` | Seed stub (no-op in MVP stage) |
| `make test` | Run unit tests (no Docker required) |
| `make test-integration` | Run testcontainers-go integration tests (requires Docker Desktop) |
| `make build` | Compile production binary `tally-backend` |
| `make lint` | Run golangci-lint |
| `make run` | Start server from source (requires services already up) |
| `make docker-build` | Build Docker image `lurus-tally:local` |
| `make clean` | Remove build artifacts |
| `make coverage` | Generate and open HTML coverage report |

Tests: `make test` (or `go test -count=1 ./...`); CI uses `go test -race -count=1 ./...`.

## Directory Structure (Story 1.1 scope)

```
2b-svc-psi/
├── cmd/server/
│   ├── main.go              # Entry point — config → DI → signal → shutdown
│   └── main_test.go         # Integration tests
├── internal/
│   ├── adapter/handler/
│   │   ├── health/          # Liveness + readiness handlers
│   │   └── router/          # Gin route registration
│   ├── lifecycle/           # App struct, Start/Stop
│   └── pkg/
│       ├── config/          # Env loading + startup validation
│       ├── logger/          # JSON structured logging (log/slog)
│       └── version/         # Build-time version variable
├── deploy/k8s/
│   ├── base/                # K8s manifests (Namespace, Deployment, Service, …)
│   └── overlays/stage|prod/ # Kustomize overlays
├── Dockerfile               # Multi-stage: golang:1.25-alpine → scratch
├── Makefile
├── .golangci.yml
├── .env.example
└── .github/workflows/ci.yaml
```

## Documents

| Document | Path | Status |
|----------|------|--------|
| 决策锁定 | `_research/decision-lock.md` | DONE |
| GitHub 国际开源调研 | `_research/github.md` | DONE |
| 市场+趋势 | `_research/google.md` | DONE |
| Gitee 国内开源调研 | `_research/gitee.md` | DONE |
| 综合选型决策 | `_research/synthesis.md` | DONE |
| UX 标杆 | `_research/ux-benchmarks.md` | DONE |
| 抄代码计划 | `_research/code-borrowing-plan.md` | DONE |
| **PRD** | `_bmad-output/planning-artifacts/prd.md` | DONE |
| **Architecture** | `_bmad-output/planning-artifacts/architecture.md` | DONE |
| **Epics** | `_bmad-output/planning-artifacts/epics.md` | DONE |
| Stories | `_bmad-output/stories/` | Story 1.1 DONE |

## Open-Source Lineage

详见 `NOTICE` 文件。借鉴: **jshERP** (Apache-2.0, 核心进销存数据模型) · **GreaterWMS** (Apache-2.0, WMS schema 六状态库存) · **Apache OFBiz** (Apache-2.0, 设计模式参考) · **shadcn/ui + Radix** (MIT, 前端组件) · **Medusa.js v2** (MIT, Headless inventory 架构参考)。Third-party license notices 收集于 `THIRD_PARTY_LICENSES/`。

License 红榜 (永不引入): GPL/AGPL 系列、JeecgBoot 附加禁制、Vendure v3+。

## BMAD

| Resource | Path |
|----------|------|
| PRD | `./_bmad-output/planning-artifacts/prd.md` |
| Epics | `./_bmad-output/planning-artifacts/epics.md` |
| Architecture | `./_bmad-output/planning-artifacts/architecture.md` |
