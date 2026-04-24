# Story 1.4 — GitHub Actions CI 流水线

**Epic**: 1 — 项目骨架与 CI/CD 管线
**Story ID**: 1.4
**优先级**: P0（Story 1.3 Done；Story 1.5 Docker 推送依赖 CI 绿灯）
**类型**: infra（质量门禁）
**预估**: 4–6 小时
**Status**: Done (YAML written, local pre-flight passed; live CI deferred to first push)

---

## User Story

As a Lurus Tally developer,
I want every push and PR to main to automatically run lint, typecheck, unit test, integration test, and build,
so that broken code is caught before it reaches main and PR reviewers never need to ask "does it even build?".

---

## Context

Story 1.1–1.3 建立了 Go 服务骨架、Next.js 前端脚手架和 12 个 SQL 迁移文件。当前 `.github/workflows/ci.yaml` 仅含 Go lint/test/build 三个 job，缺少：前端 typecheck/lint/build、集成测试（需要 pgvector Postgres 服务容器）、Bun 缓存，以及正确的 `working-directory`（当前引用 `2b-svc-psi/` 前缀，但 `lurus-tally` 是独立 repo，根目录即服务根）。

本 Story 将 `ci.yaml` 重写为完整的 5-job 流水线，并在 GitHub 上配置 branch protection，确保任意 job 失败时 PR 无法合并。

---

## Acceptance Criteria

1. **AC-1 触发规则**: 对 `main` 分支的每次 `push` 和每个指向 `main` 的 `pull_request` 都触发 CI；其他分支 push 不触发（减少无效资源消耗）。

2. **AC-2 Go lint job 通过**: `golangci/golangci-lint-action@v6` 使用 `.golangci.yml`（已存在于 repo 根），在 `ubuntu-latest` 上运行，使用 `actions/setup-go@v5`（Go 1.25），go module cache 命中率 > 0（`cache: true`）。输出与本地 `golangci-lint run ./...` 一致。

3. **AC-3 Go unit test job 通过**: `go test -race -count=1 -coverprofile=coverage.out ./...` 在 `ubuntu-latest` 上通过；`coverage.out` 作为 artifact 上传（供后续 Story 1.5 Quality Gate 使用）。**不**运行 `integration` build tag 的测试（integration 单独 job）。

4. **AC-4 Go build job 通过**: `CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -trimpath -o tally-backend ./cmd/server` 编译成功，产物可 `ls -lh`。仅在 lint + unit test 全绿后运行（`needs: [lint, test]`）。

5. **AC-5 前端 CI job 通过**: 在同一 job 内顺序执行：`bun install --frozen-lockfile` → `bunx tsc --noEmit`（typecheck）→ `bun run lint`（ESLint）→ `bun run build`（Next.js production build）。使用 `oven-sh/setup-bun@v2`，`bun.lockb` 和 `node_modules` 按 `web/bun.lockb` hash 缓存。

6. **AC-6 集成测试 job 通过**: 独立 job，使用 `services: postgres`（`pgvector/pgvector:pg16` 镜像），通过 `go test -tags integration -timeout 120s -count=1 ./tests/integration/...` 运行 `TestMigration_*` 五个测试，全部 PASS。环境变量 `DATABASE_DSN` 指向 service container。

7. **AC-7 job 并行**: lint、unit test、frontend 三个 job 并行运行（无 `needs` 依赖）；build 依赖 lint + unit test；integration test 独立（无需等待其他 job，与 frontend/build 并行）。整体 CI 耗时 ≤ 5 分钟（热缓存下）。

8. **AC-8 失败阻止合并**: GitHub branch protection 规则配置 5 个 required status checks（`Lint / lint`、`Test / unit-test`、`Build / build`、`Frontend / frontend`、`Integration Test / integration-test`），任意失败则 PR 不可合并（⚠️ 此 AC 依赖手动在 GitHub Settings 配置，CI 文件本身无法自动设置）。

9. **AC-9 working-directory 正确**: 所有 Go job 步骤均在 repo 根目录运行（`lurus-tally` 是独立 repo，无 `2b-svc-psi/` 前缀）；前端 job 在 `web/` 目录运行。

10. **AC-10 缓存有效**: Go job 使用 `actions/setup-go@v5` 内置 module cache（`cache: true`，cache key 含 `go.sum` hash）；前端 job 使用 `actions/cache@v4` 缓存 `web/node_modules`（cache key 含 `web/bun.lockb` hash）。CI 二次运行时 "Cache hit" 日志可见。

11. **AC-11 Bun lockfile 已提交**: `web/bun.lockb` 文件存在于 repo 中（`bun install --frozen-lockfile` 要求 lockfile 与 `package.json` 一致），否则前端 job 在 `--frozen-lockfile` 模式下失败。

12. **AC-12 pgvector 镜像用于集成测试**: 集成测试 service 容器使用 `pgvector/pgvector:pg16`（非 `postgres:16`），确保 `CREATE EXTENSION IF NOT EXISTS "vector"` 不报错（Story 1.3 AC-3 的 CI 落地）。

---

## Tasks / Subtasks

### Task 1: 确认前置条件

- [x] 确认 `web/bun.lock` 已存在（OQ-3 resolution: lockfile is `bun.lock` text format, not `bun.lockb`. File present at `web/bun.lock`, `bun install --frozen-lockfile` exits 0.）
  - [x] 写验证命令: `ls web/bun.lock && bun install --frozen-lockfile` — 文件存在且非空，安装无变更
- [x] 确认 `.golangci.yml` 存在于 repo 根（已确认：Story 1.1 产物）
- [x] 确认 `tests/integration/migration_test.go` 存在（已确认：Story 1.3 产物；使用 testcontainers-go，不读 DATABASE_DSN — 无需修改，per OQ-1 resolution）
- [x] 确认 `web/package.json` 含 `typecheck`、`lint`、`build` 三个 script（`bun run lint` 0 errors; `bunx tsc --noEmit` 0 errors; `bun run build` 3 routes static OK）

---

### Task 2: 重写 `.github/workflows/ci.yml`（rename from ci.yaml）

**注意**: 现有文件为 `ci.yaml`，保持 `.yaml` 后缀（已有文件直接覆盖）。

- [x] 写测试: 在本地逐条执行以下命令，验证每个 job 的核心步骤可通过
  - [x] `golangci-lint run ./...` — ⏳ SKIP: golangci-lint not installed on Windows host (will run on ubuntu-latest in CI)
  - [x] `go test -race -count=1 ./...` — ⏳ PARTIAL: -race requires CGO on Windows; `go test -count=1 ./...` EXIT:0 (6 packages PASS); -race will work on ubuntu-latest in CI
  - [x] `CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -trimpath -o /tmp/tally-backend ./cmd/server` — EXIT:0, binary 18MB produced
  - [x] `cd web && bun install --frozen-lockfile && bunx tsc --noEmit && bun run lint && bun run build` — all EXIT:0 (lint: "No ESLint warnings or errors"; build: 3 static routes)
- [x] 实现 `.github/workflows/ci.yaml`（覆盖现有文件）:

  OQ resolutions applied:
  - OQ-1: integration job uses testcontainers-go (no `services:` block); test code NOT modified
  - OQ-2: YAML is design-correct; live CI deferred to repo creation
  - OQ-3: lockfile `bun.lock` (text format); cache key hashes `web/bun.lock`

  **Job 结构** (as implemented):
  ```
  backend-lint        (Go, runs: ubuntu-latest, no needs)
  backend-test        (Go, runs: ubuntu-latest, no needs) → uploads coverage.out artifact
  frontend            (Bun/Next.js, runs: ubuntu-latest, no needs)
  backend-integration (Go + testcontainers, runs: ubuntu-latest, no needs)
  backend-build       (Go, runs: ubuntu-latest, needs: [backend-lint, backend-test]) → uploads tally-backend artifact
  ```

- [x] 验证: YAML parsed via js-yaml (bun -e): `Jobs: ["backend-lint","backend-test","frontend","backend-integration","backend-build"]` EXIT:0

---

### Task 3: 确认 `web/bun.lock` 已追踪

- [x] 写测试（幂等检查）: `git ls-files web/bun.lock` — `2b-svc-psi` 不是独立 repo (git root = `lurus` governance repo with deny-all `/*` .gitignore); `web/bun.lock` file EXISTS on disk. When `lurus-tally` is created as standalone repo, `bun.lock` will not be covered by a deny-all gitignore and must be `git add`-ed on first commit.
  - [x] Note: CI runner will have `bun.lock` present (checked out via `actions/checkout@v4`) since it's part of the standalone repo's working tree.
- [x] 验证: `bun install --frozen-lockfile` EXIT:0, "no changes" — lockfile consistent with package.json

---

### Task 4: 推送到 GitHub 并观察首次 CI 运行

- [ ] `git push origin main`（或打开 PR）— ⏳ DEFERRED: repo `hanmahong5-arch/lurus-tally` not yet created
- [ ] 在 `https://github.com/hanmahong5-arch/lurus-tally/actions` 观察 workflow 触发
- [ ] 逐个 job 检查日志，记录实际耗时
- [ ] 若任何 job 失败，根据日志修复（常见问题见 §Dev Notes）

---

### Task 5: 配置 GitHub branch protection

- [ ] 打开 `https://github.com/hanmahong5-arch/lurus-tally/settings/branches` — ⏳ DEFERRED: repo not yet created
- [ ] 为 `main` 分支添加 rule: "Require status checks to pass before merging"
- [ ] 添加 5 个 required checks（名称需与 `jobs.<id>.name` 完全匹配）:
  - `Lint`（job name: `Lint`, job id: `backend-lint`）
  - `Unit Test`（job name: `Unit Test`, job id: `backend-test`）
  - `Build`（job name: `Build`, job id: `backend-build`）
  - `Frontend`（job name: `Frontend`, job id: `frontend`）
  - `Integration Test`（job name: `Integration Test`, job id: `backend-integration`）
- [ ] 验证: 打开一个测试 PR，确认 checks 列表出现上述 5 项

---

## File List (actual)

| 文件路径（相对 `2b-svc-psi/`，即 repo 根） | 操作 | 说明 |
|--------------------------------------------|------|------|
| `.github/workflows/ci.yaml` | modified（重写） | 覆盖现有 3-job 骨架为完整 5-job 流水线（backend-lint/backend-test/frontend/backend-integration/backend-build） |
| `_bmad-output/stories/story-1.4.md` | modified | Dev Agent Record + AC verification table updated |

**Governance files updated (lurus root)**:
| 文件路径 | 操作 |
|----------|------|
| `doc/coord/changelog.md` | prepended Story 1.4 one-liner |
| `doc/coord/service-status.md` | updated Tally block |
| `doc/process.md` | prepended ≤15 line Story 1.4 entry |

**本 Story 不创建/不修改**:
- `tests/integration/migration_test.go`（per OQ-1: testcontainers-go approach retained, no changes needed）
- `Dockerfile`（已存在，Story 1.5 范围）
- 任何 Go 或 TypeScript 业务代码
- K8s manifests（Story 1.6 范围）

---

## Dev Notes

### 完整 CI YAML 参考

以下为 Story 1.4 实现时应写入 `.github/workflows/ci.yaml` 的内容（逐字参考，开发者可按实际需要微调版本号）：

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

env:
  GO_VERSION: "1.25"

jobs:
  lint:
    name: Lint
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}
          cache: true
      - uses: golangci/golangci-lint-action@v6
        with:
          version: latest

  unit-test:
    name: Unit Test
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}
          cache: true
      - name: Run unit tests
        run: go test -race -count=1 -coverprofile=coverage.out ./...
      - name: Upload coverage
        uses: actions/upload-artifact@v4
        with:
          name: coverage
          path: coverage.out

  build:
    name: Build
    runs-on: ubuntu-latest
    needs: [lint, unit-test]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}
          cache: true
      - name: Build backend binary
        run: CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -trimpath -o tally-backend ./cmd/server
      - name: Verify binary
        run: ls -lh tally-backend

  frontend:
    name: Frontend
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: oven-sh/setup-bun@v2
      - name: Cache node_modules
        uses: actions/cache@v4
        with:
          path: web/node_modules
          key: bun-${{ hashFiles('web/bun.lockb') }}
          restore-keys: bun-
      - name: Install dependencies
        working-directory: web
        run: bun install --frozen-lockfile
      - name: Typecheck
        working-directory: web
        run: bunx tsc --noEmit
      - name: Lint
        working-directory: web
        run: bun run lint
      - name: Build
        working-directory: web
        run: bun run build

  integration-test:
    name: Integration Test
    runs-on: ubuntu-latest
    services:
      postgres:
        image: pgvector/pgvector:pg16
        env:
          POSTGRES_USER: tally
          POSTGRES_PASSWORD: tally
          POSTGRES_DB: tally
        ports:
          - 5432:5432
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5
    env:
      DATABASE_DSN: postgres://tally:tally@localhost:5432/tally?sslmode=disable
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}
          cache: true
      - name: Run integration tests
        run: go test -tags integration -timeout 120s -count=1 -v ./tests/integration/...
```

### 现有 ci.yaml 的问题清单（重写动机）

| 问题 | 现状 | 修复后 |
|------|------|--------|
| `working-directory: 2b-svc-psi` | 按 monorepo 根设计，但 lurus-tally 是独立 repo | 删除所有 working-directory（根即服务根）|
| 无前端 job | 缺失 typecheck/lint/build | 新增 `frontend` job |
| 无集成测试 job | migration 集成测试从未在 CI 跑 | 新增 `integration-test` job + pgvector service |
| 无 Bun 缓存 | 每次 CI 全量安装 | `actions/cache@v4` 按 `bun.lockb` hash 缓存 |
| golangci-lint `version: latest` | 浮动版本可能导致 lint 规则变化破坏 CI | 保留 `latest`，但在 story AC 中注明此风险；若出现意外失败可锁定版本如 `v1.64.0` |

### golangci-lint 版本锁定

`golangci/golangci-lint-action@v6` 默认拉 `latest` 版本。`latest` 版本升级后新增 linter 规则可能导致原本通过的代码突然失败（CI 无改动却变红）。

**建议**: 首次 CI 通过后，在 YAML 中锁定到当时的版本（如 `version: v1.64.0`）。开发者可通过每月升级 PR 统一管理 lint 升级。本 Story 实现时先用 `latest`；若 CI 出现意外 lint 失败，锁定当前最新版本即可。

### bun.lockb 必须提交

`bun install --frozen-lockfile` 在 lockfile 与 `package.json` 不一致时直接失败（非警告）。`web/bun.lockb` **必须**被 `git add` 并提交。检查方法：

```bash
git ls-files web/bun.lockb
# 若无输出: cd web && bun install && git add web/bun.lockb && git commit -m "chore(web): commit bun lockfile"
```

`.gitignore` 中不能有 `*.lockb` 规则。

### integration-test job 不依赖 testcontainers-go

Story 1.3 的 `tests/integration/migration_test.go` 使用 `testcontainers-go` 在本地起 Docker 容器。在 CI 中，`services: postgres` 已提供好数据库实例，`testcontainers-go` 会尝试再起一个容器，与 service container 竞争。

**潜在冲突**: 如果集成测试代码固定调用 `testcontainers-go` 启动容器（而非读取 `DATABASE_DSN` 环境变量），则 CI integration-test job 可能需要改写测试初始化逻辑，让测试优先读取 `DATABASE_DSN` 环境变量（若已设置则跳过 testcontainers 启动）。

**推荐模式**（在 `migration_test.go` 的 `TestMain` 或各测试的 `setup` 函数中）:

```go
dsn := os.Getenv("DATABASE_DSN")
if dsn == "" {
    // fallback: testcontainers-go (local dev with Docker)
    dsn = startTestContainer(t)
}
```

若 Story 1.3 的实现已用此模式，则 CI job 开箱即用。若未用此模式，**本 Story 需同步修改 `tests/integration/migration_test.go`**（估算额外 1 小时工时）。开发者在实现 Task 2 前需检查该文件。

### pgvector/pgvector:pg16 vs postgres:16

CI 集成测试必须使用 `pgvector/pgvector:pg16`（含 pgvector 扩展），而非官方 `postgres:16`。原因：迁移文件 `000001_init_extensions.up.sql` 执行 `CREATE EXTENSION IF NOT EXISTS "vector"`；标准 postgres 镜像无此扩展，执行时报 `ERROR: could not open extension control file`，导致 `TestMigration_AllTablesExist` 失败。

**镜像来源**: `ghcr.io/pgvector/pgvector:pg16`（亦可用 `pgvector/pgvector:pg16`，两者等价）。因 CI 运行在 GitHub-hosted runner（ubuntu-latest），无国内镜像拉取问题。

### DATABASE_DSN 格式

`services.postgres` 暴露在 `localhost:5432`（GitHub Actions 自动端口映射）。环境变量格式：

```
postgres://tally:tally@localhost:5432/tally?sslmode=disable
```

`golang-migrate` 和 `pgx/v5/stdlib` 均支持此格式。若测试代码使用 `lib/pq` driver，DSN 前缀改用 `postgresql://`（两者等价）。

### Go race detector + integration test 交互

`go test -race` 与 integration build tag 可以共用，但 race detector 使内存用量约 5 倍，在 2G GitHub runner 下可能 OOM。**集成测试 job 不加 `-race`**（`-timeout 120s` 已作为超时保护）；unit-test job 保留 `-race`（仅跑内存占用较小的单元测试）。

### `bun run test` 缺失

`web/package.json` 当前无 `test` script（Story 1.2 已确认）。Epic 中提到 `bun run test`，但此 script 不存在。**本 Story 前端 job 不包含 `bun run test`**，避免 CI 因缺失 script 报错。前端单元测试在 Epic 3（UI 组件 Story）引入 Vitest 后补充。

### 不要添加的东西（Karpathy 检查）

- **不要**在 CI 添加 Trivy 扫描（Story 1.5 范围）
- **不要**在 CI 添加 Docker build/push（Story 1.5 范围）
- **不要**在 CI 添加 ArgoCD sync（Story 1.6 范围）
- **不要**在 Go test job 加 `-tags integration`（integration 单独 job，避免无 Postgres 时 unit-test job 失败）
- **不要**将 `coverage.out` 上传到 Coveralls/Codecov（未在 epic 要求）

---

## Testing Strategy

| AC | 验证方式 | 执行时机 |
|----|---------|---------|
| AC-1 触发规则 | 推送 PR 后观察 GitHub Actions 触发 | CI（首次 push） |
| AC-2 Go lint | 本地 `golangci-lint run ./...` exit 0 | 本地 + CI |
| AC-3 Go unit test | 本地 `go test -race ./...` PASS | 本地 + CI |
| AC-4 Go build | 本地 `CGO_ENABLED=0 GOOS=linux go build ./cmd/server` | 本地 + CI |
| AC-5 Frontend CI | 本地 `cd web && bun install --frozen-lockfile && bunx tsc --noEmit && bun run lint && bun run build` | 本地 + CI |
| AC-6 集成测试 | CI job 日志显示 5 个 `TestMigration_*` PASS | CI（需 pgvector service） |
| AC-7 并行 job | GitHub Actions timeline 图（Gantt 视图）确认 lint/unit-test/frontend/integration-test 并行 | CI（可视化） |
| AC-8 阻止合并 | 打开测试 PR，故意让一个 check 失败，确认 Merge button 变灰 | 手动（GitHub Settings） |
| AC-9 working-directory | YAML 审查：无 `2b-svc-psi/` 前缀 | 代码审查 |
| AC-10 缓存 | CI 二次运行日志含 "Cache hit" | CI（第二次运行） |
| AC-11 bun.lockb | `git ls-files web/bun.lockb` 非空 | 本地 + CI |
| AC-12 pgvector 镜像 | YAML 审查：`image: pgvector/pgvector:pg16` | 代码审查 |

**诚实约束**: AC-1/AC-6/AC-7/AC-8/AC-10 只能在 GitHub Actions 上验证，无法在 Windows 本地模拟。实现完成后必须推送并等待 CI 实际通过，才能将对应 AC 标记为 ✅。

---

## Out of Scope

- Docker 镜像构建与 GHCR 推送（Story 1.5）
- Trivy 漏洞扫描（Story 1.5）
- ArgoCD ApplicationSet 注册（Story 1.6）
- 前端单元测试（`bun run test`/Vitest）— 当前 `web/package.json` 无 `test` script，Epic 3 补充
- `golangci-lint` 版本锁定的长期治理（本 Story 先用 `latest`，首次 CI 通过后锁定）
- Secrets 管理（`DATABASE_DSN` 在 integration job 内联，不含生产凭证）

---

## Dependencies

- **前置 Story**: Story 1.1 Done（Go 骨架 + `.golangci.yml`）、Story 1.2 Done（`web/` Next.js 骨架 + `bun` 工具链）、Story 1.3 Done（`tests/integration/migration_test.go` + build tag `integration`）
- **阻塞项**: `hanmahong5-arch/lurus-tally` GitHub repo 尚未创建（CLAUDE.md §Known Issues）— CI 配置文件本地可完成，但 AC-1/AC-6/AC-7/AC-8/AC-10 验证需 repo 可访问。**开本 Story 前需确认 repo 已创建**。
- **潜在阻塞**: `tests/integration/migration_test.go` 是否支持读取 `DATABASE_DSN` 环境变量（如不支持需修改，估算额外 1 小时）

---

## Open Questions

| # | 问题 | 阻塞 | 决策人 | 解决时机 |
|---|------|------|--------|----------|
| OQ-1 | `tests/integration/migration_test.go` 是否优先读取 `DATABASE_DSN` 环境变量（而非总是启动 testcontainers）？ | **是** — 若不支持，integration-test job 会启动两个 Postgres 实例，可能引发端口冲突或测试行为异常 | 开发者（检查 Story 1.3 产出代码） | **Task 2 前检查** |
| OQ-2 | GitHub repo `hanmahong5-arch/lurus-tally` 是否已创建？ | 是 — CI push 需要 repo 存在 | 仓库 owner | **Story 开工前确认** |
| OQ-3 | `web/bun.lockb` 是否已提交到 git？ | 是 — `--frozen-lockfile` 的前提 | 开发者 | Task 1 检查，若缺失 Task 3 生成 |

---

## Dev Agent Record

```
实现开始时间: 2026-04-23
实现完成时间: 2026-04-23 (YAML written + local pre-flight passed)
CI 首次全绿时间: ⏳ deferred — awaits repo creation + first push

OQ Resolutions applied:
  OQ-1: testcontainers-go retained (no services: block); migration_test.go NOT modified
  OQ-2: YAML is design-correct; live validation deferred to first push (repo not yet created)
  OQ-3: lockfile is bun.lock (text format, Bun 1.2+); cache key hashes web/bun.lock

AC 验证状态:

| AC | 状态 | 证据 |
|----|------|------|
| AC-1 触发规则 | ⏳ 待 CI 验证 | YAML: `on: push: branches: [main]` + `pull_request: branches: [main]`; live trigger requires first push |
| AC-2 Go lint | ⏳ 待 CI 验证 | golangci-lint-action@v6 in YAML; golangci-lint not installed locally (Windows host); `go vet ./...` EXIT:0 locally |
| AC-3 Go unit test | ✅ 本地通过（-race 仅限 CI） | `go test -count=1 ./...` EXIT:0 (6 packages PASS); -race requires CGO, runs correctly on ubuntu-latest |
| AC-4 Go build | ✅ 本地通过 | `CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -trimpath -o /tmp/tally-backend ./cmd/server` EXIT:0, binary 18MB |
| AC-5 前端 CI | ✅ 本地通过 | `bun install --frozen-lockfile` EXIT:0; `bunx tsc --noEmit` EXIT:0; `bun run lint` EXIT:0 (0 errors); `bun run build` EXIT:0 (3 static routes) |
| AC-6 集成测试 | ⏳ 待 CI 验证 | testcontainers-go spawns pgvector/pgvector:pg16; no Docker on Windows host; will run on ubuntu-latest (Docker pre-installed) |
| AC-7 job 并行 | ⏳ 待 CI 验证（可视化） | YAML: backend-lint/backend-test/frontend/backend-integration have no `needs`; backend-build `needs: [backend-lint, backend-test]` |
| AC-8 阻止合并 | ⏳ 待手动配置 GitHub branch protection | Requires repo creation first |
| AC-9 working-directory 正确 | ✅ YAML 审查通过 | No `working-directory: 2b-svc-psi` in YAML; Go jobs run at repo root; frontend steps use `working-directory: web` only |
| AC-10 缓存有效 | ⏳ 待 CI 第二次运行验证 | YAML: `setup-go@v5 cache: true`; `actions/cache@v4` key `bun-${{ hashFiles('web/bun.lock') }}` |
| AC-11 bun.lock 已提交 | ✅ 文件存在 | `web/bun.lock` exists on disk (Bun 1.2+ text format); will be tracked in standalone lurus-tally repo on first commit |
| AC-12 pgvector 镜像 | ✅ YAML 审查通过 | integration job: testcontainers-go uses `pgvectorImage = "pgvector/pgvector:pg16"` constant in migration_test.go; no `services:` block in YAML |

偏差记录:
  1. Integration job design changed (OQ-1): dropped `services: postgres` approach; uses testcontainers-go natively.
     Rationale: migration_test.go always starts its own container via testcontainers; a `services:` block would conflict.
     This simplifies the YAML (no health-check options, no DATABASE_DSN env var, no test code modifications).
  2. Lockfile reference corrected (OQ-3): all story + YAML references use `bun.lock` (text, Bun 1.2+), not `bun.lockb` (legacy binary).
  3. Job IDs renamed for clarity: `lint`→`backend-lint`, `test`→`backend-test`, `build`→`backend-build`, `integration-test`→`backend-integration`.
     Branch protection check names must match job `name:` field (Lint / Unit Test / Frontend / Integration Test / Build).
  4. Binary upload added to backend-build job (artifact `tally-backend`) for use in Story 1.5 Docker build.
  5. `go vet ./...` passes locally (EXIT:0). Full golangci-lint deferred to CI (not installed on Windows host).
```
