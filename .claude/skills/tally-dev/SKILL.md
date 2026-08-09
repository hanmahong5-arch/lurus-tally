---
name: tally-dev
description: Tally (2b-svc-tally) 全栈开发手册。Go 4-layer (Gin+GORM) + Next.js 14 (Bun)。覆盖 scratch embed、NUMERIC scan、industry profile gate、platform 接入、worktree 并行、NextAuth OIDC id_token、App Router 陷阱。自动触发：工作目录在 2b-svc-tally 子树,或讨论 Tally(苗木字典 / 项目核算 / horticulture pack)时。
---

# tally-dev — Tally 项目开发手册

> 项目：`2b-svc-tally` (lurus-tally) — AI-native 智能进销存 SaaS。Go (Gin + GORM) + Next.js 14 (Bun)。
> 本手册随源码交付；原仓库跨产品协调文件（`lurus.yaml`、`doc/coord/contracts.md` 等）不在此交付范围内，
> 项目内规划文档见 `_bmad-output/planning-artifacts/`（PRD / architecture / epics / stories / platform-integration-map / horticulture-extension）。

## 1. 项目快速定位

| 项 | 值 |
|----|-----|
| DB schema | `tally` (PostgreSQL RLS) |
| Redis DB | 5（部署方可按自身环境重新分配） |
| NATS stream | `PSI_EVENTS` |
| K8s manifest | `deploy/k8s/base/` + `deploy/k8s/overlays/`（overlay 按目标环境自行调整，仓内 `stage` overlay 为原厂内部命名，可参考结构后按自己的环境命名新增 overlay） |
| 私有化部署 | 见 `deploy/customer/INSTALL.md`（Docker Compose 自包含部署，无 K8s/ArgoCD 依赖） |

## 2. 后端 4-Layer 包结构（每加新模块照抄）

```
internal/
├── domain/<module>/         # 实体 + Validate + 领域常量（无依赖）
├── app/<module>/            # 用例 (CreateUseCase / GetUseCase / ListUseCase / UpdateUseCase / DeleteUseCase / RestoreUseCase) + Repository interface + ErrXxx
├── adapter/repo/<module>/   # 实现 Repository (raw SQL via database/sql, NOT ORM)
├── adapter/handler/<module>/  # Gin handler + DTO（snake_case JSON）
└── lifecycle/app.go         # DI 注入：repo → use cases → handler → router.New(...)
```

**包名同名时 import alias**:
```go
import (
    domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/horticulture"
    apphort "github.com/hanmahong5-arch/lurus-tally/internal/app/horticulture"
)
```

参考已实现：`internal/{domain,app,adapter}/{product,bill,horticulture,project}/`

## 3. router.New 加 handler = breaking change

每加一个 handler 模块，`internal/adapter/handler/router/router.go` 的 `New(...)` 多一个参数。S28.1 12→13，S28.2 13→14。**必须同步更新**：
- `router_test.go` 的 `newTestRouter()` 多传 1 个 nil
- `internal/lifecycle/app.go` 构造完整 DI 链后传给 `router.New`
- `notImplemented` fallback：handler 为 nil 时返回 501（让集成测试能验证路由注册）

## 6. Platform 接入（Tally consumes 7 capability）

真源：`_bmad-output/planning-artifacts/platform-integration-map.md`

| Capability | 状态 | 路径 |
|-----------|------|------|
| identity | ✅ | `internal/adapter/platform/`（原 `pkg/platformclient`，重构后兼容 alias 还在）|
| billing | ✅ | 同上 |
| llm-inference | ✅ | `internal/pkg/llmclient/` → newapi.lurus.cn |
| auth | ✅ | `web/auth.ts` Zitadel OIDC PKCE |
| memory | ✅ | `internal/pkg/memorusclient/` (Track C, MEMORUS_API_KEY 空→降级) |
| notification | 🟡 client ready | `internal/adapter/platform/notification.go` + `internal/adapter/nats/` (NATS-first, 业务事件待接入) |
| agent-execution | ❌ 待接 | kova-rest:3002, E17/E31 |

**降级模式**（platform 依赖都应当降级）:
```go
// memorus/notification 等非关键依赖：API key/URL 空 → New return nil + nil error
func New(cfg Config) (*Client, error) {
    if cfg.APIKey == "" { return nil, nil }  // calling code nil-check
    // ...
}
```

`internal/lifecycle/app.go` 启动 log 一行 `<capability>: enabled (mode=...)` 让运维一眼看出降级状态。

## 8. K8s 部署（自建集群）

`deploy/k8s/base/` + `deploy/k8s/overlays/` 提供 Kustomize 基线；`deploy/customer/` 提供不依赖 K8s 的 Docker Compose 私有化部署（见 `deploy/customer/INSTALL.md`，推荐无 K8s 运维能力时使用）。

若在自有 K8s 集群部署，典型手动升级流程（无 ArgoCD 时）：

```bash
# 1. 构建并推送镜像到你自己的镜像仓库（见根 Dockerfile / web/Dockerfile）
# 2. set image（注意 container name 不一定等于 deployment name，先用
#    `kubectl get deploy <name> -o jsonpath='{.spec.template.spec.containers[*].name}'` 确认）
kubectl -n <your-namespace> set image deployment/tally-backend tally-backend=<your-registry>/tally-backend:<tag>
kubectl -n <your-namespace> set image deployment/tally-web tally-web=<your-registry>/tally-web:<tag>

# 3. 等 rollout
kubectl -n <your-namespace> rollout status deployment/tally-backend --timeout=180s

# 4. migration boot 失败时（schema_migrations dirty）见 references/db-migration-gotchas.md §4.3

# 5. verify
curl -sS -o /dev/null -w 'HTTP %{http_code}\n' https://<your-domain>/internal/v1/tally/health
```

**踩坑**:
- `kubectl delete --field-selector=status.phase!=Running` 不匹配 CrashLoopBackOff（它仍是 Running phase）。直接 `delete pod <name> --force`。
- container name 不一定 = deployment name：先 `get deploy -o jsonpath` 确认再 set image。
- migration 若涉及 `pg_trgm` 索引，见 references/db-migration-gotchas.md §4.2 的 schema 限定坑。

## 9. Quality Gate（PR 合并前必跑）

```bash
# 后端
gofmt -w ./internal
gofmt -l ./internal  # must empty
go vet ./...
CGO_ENABLED=0 GOOS=linux go build ./...
# golangci-lint run ./... 本地 Windows 跑不了，CI 会跑

# 前端
cd web
bun run lint
bunx tsc --noEmit
bun run build
bun run test  # vitest（playwright spec 会失败，是 pre-existing pattern）
```

**CI 历史失败模式**（按高频排）:
1. gofmt drift（merge 后没 normalize）→ `gofmt -w ./internal`
2. unused import / unused type（重构后忘删）→ `go vet ./... && golangci-lint run`
3. tsc ES3 spread iterator → `Array.from(...)`
4. tsc strict optional types（IDBValidKey 等）→ 删掉 `(key: string)` explicit type，让 TS 推断
5. `bun run lint` 1 个 pre-existing Palette.tsx warning（不影响）

## 10. 已交付 Story / Epic 索引

| ID | 内容 | 状态 |
|----|------|------|
| 1.1-1.7 | E1 多租户 + 商品 + 库存 + 单据基础 | done |
| 9.1 | 跨境货币 + 汇率 | done |
| 10.1 | Platform billing 接入 | done |
| 11.1 | AI Drawer + ⌘K Palette | done |
| 21.1 | 草稿 (IndexedDB) + Cmd+Z 撤销栈 | done |
| 28.1 | 苗木字典 + 200 种 seed | done |
| 28.2 | 项目 CRUD + 卡片网格列表页 | done |
| Track A | platform adapter 重构 + NATS notification | done |
| Track C | memorus client + AI Drawer 历史召回 | done |
| Track F | sidebar industry gate + horticulture profile | done |

后续：E28.3+（项目-单据关联）→ E29（发票）→ E30（多账套）→ E31（主动 AI / kova workflow）→ E32（移动端 PWA）。

## 11. 关联文档

- 真源 `lurus.yaml` § lurus-tally
- PRD: `2b-svc-tally/_bmad-output/planning-artifacts/prd.md` (~10k 字)
- Architecture: `_bmad-output/planning-artifacts/architecture.md` (~14k 字, 27 表 DDL)
- Epics: `_bmad-output/planning-artifacts/epics.md`
- Horticulture pack: `_bmad-output/planning-artifacts/horticulture-extension.md`
- Platform 接入: `_bmad-output/planning-artifacts/platform-integration-map.md`
- 跨服务契约: `doc/coord/contracts.md`
- 部署节点详情：`/.claude/skills/deploy/SKILL.md`

## References(按需 Read,不自动进上下文)

| 文件 | 内容 | 何时读 |
|------|------|--------|
| `references/db-migration-gotchas.md` | §4: scratch //go:embed / pg_trgm schema 限定 / dirty force fix / NUMERIC *string / sliceToArray / RLS nil UUID | 写 migration、加 repo、部署 CrashLoop |
| `references/frontend-gotchas.md` | §5: (dashboard) 路由组 + middleware matcher / NextAuth id_token / tsc ES3 / 卡片网格+Sheet 模板 / sidebar industry gate | 改 web/ 任何页面或 auth |
| `references/swarm-worktree.md` | §7: worktree 隔离 / territory 划分 / sequential merge / lifecycle/app.go 冲突解法 | 开多 agent 并行开发 |
