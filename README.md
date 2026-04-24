# Lurus Tally (2b-svc-psi)

> 智能进销存系统 (Smart Purchase-Sales-Inventory)

**状态**: Story 1.1 骨架完成 — Go 服务可启动并通过健康检查
**产品组**: Platform (P0) 候选
**目标客群**: 中小企业 B2B (制造/批发/零售/电商)

## Vision

不是传统进销存的 CRUD 系统，而是 AI-native 的库存智能体：
- **预测**: LLM + 时序预测自动补货 / 滞销预警
- **决策**: 智能议价、动态定价、多渠道库存最优分配
- **集成**: 与 Lurus Hub (LLM 网关) + Memorus (AI 记忆) + Platform (账户/计费) 无缝打通
- **多端**: Web + 微信小程序 + 移动端，覆盖中国 SMB 场景

## Status

| Phase | Status |
|-------|--------|
| 1. 命名与骨架 | DONE (2026-04-23) |
| 2. 开源基座调研 (3 Agent 并行) | DONE (2026-04-23) |
| 3. 选型决策 (锁定 Go 自研 + Web Only + 顶级 UX) | DONE (2026-04-23) |
| 4. UX 标杆调研 + 抄代码计划 (并行) | DONE (2026-04-23) |
| 5. PRD + Architecture (并行) | DONE (2026-04-23) |
| 6. Epics 拆分 | DONE (2026-04-23) |
| 7. Story 1.1 — Go 服务骨架 | DONE (2026-04-23) |

**锁定决策**: 见 `_research/decision-lock.md`。

## Quick Start

### Prerequisites

- Go 1.25+
- Docker (for `make docker-build`)
- golangci-lint (for `make lint`)

### Local Run

```bash
# 1. Clone the repo and enter the service directory
git clone https://github.com/hanmahong5-arch/lurus-tally.git
cd lurus-tally

# 2. Copy env template and fill in placeholder values
cp .env.example .env
# Edit .env — set DATABASE_DSN / REDIS_URL / NATS_URL at minimum

# 3. Start the server
make run

# 4. Verify health endpoints
curl http://localhost:18200/internal/v1/tally/health
# Expected: {"service":"lurus-tally","status":"ok","version":"dev"}

curl http://localhost:18200/internal/v1/tally/ready
# Expected: {"status":"ready"}
```

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `DATABASE_DSN` | Yes | — | PostgreSQL DSN (e.g. `postgres://user:pass@host/db?sslmode=disable`) |
| `REDIS_URL` | Yes | — | Redis URL (e.g. `redis://localhost:6379/5`) |
| `NATS_URL` | Yes | — | NATS URL (e.g. `nats://localhost:4222`) |
| `PORT` | No | `18200` | HTTP listen port |
| `LOG_LEVEL` | No | `info` | Log verbosity: `debug`, `info`, `warn`, `error` |
| `GIN_MODE` | No | `release` | Gin mode: `release` or `debug` |
| `SERVICE_VERSION` | No | `dev` | Build version label (injected by `-ldflags` in CI) |
| `SHUTDOWN_TIMEOUT` | No | `5s` | Graceful shutdown deadline |

## Make Targets

| Target | Description |
|--------|-------------|
| `make build` | Compile production binary `tally-backend` |
| `make test` | Run all unit and integration tests |
| `make lint` | Run golangci-lint |
| `make run` | Start server from source (requires `.env`) |
| `make docker-build` | Build Docker image `lurus-tally:local` |
| `make clean` | Remove build artifacts |
| `make coverage` | Generate and open HTML coverage report |

## Tests

```bash
make test
# or: go test -count=1 ./...
# CI uses: go test -race -count=1 ./...
```

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

详见 `NOTICE` 文件。本产品借鉴了:
- **jshERP** (Apache-2.0) — 核心进销存数据模型
- **GreaterWMS** (Apache-2.0) — WMS schema (六状态库存)
- **Apache OFBiz** (Apache-2.0) — 设计模式参考
- **shadcn/ui + Radix** (MIT) — 前端组件
- **Medusa.js v2** (MIT) — Headless inventory 架构参考

Third-party license notices are collected in `THIRD_PARTY_LICENSES/`.

License 红榜 (永不引入): GPL/AGPL 系列、JeecgBoot 附加禁制、Vendure v3+。

## BMAD

| Resource | Path |
|----------|------|
| PRD | `./_bmad-output/planning-artifacts/prd.md` |
| Epics | `./_bmad-output/planning-artifacts/epics.md` |
| Architecture | `./_bmad-output/planning-artifacts/architecture.md` |
