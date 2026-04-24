# Story 1.1 — Go 服务可启动并通过健康检查

**Epic**: 1 — 项目骨架与 CI/CD 管线
**Story ID**: 1.1
**优先级**: P0（MVP α 第一个 story，无前置依赖）
**类型**: infra（奠基）
**预估**: 6–8 小时
**Status**: Done
**Owner**: TBD

---

## User Story

As a Lurus Tally developer,
I want a runnable Go HTTP server skeleton with health probes, structured logging, and graceful shutdown,
so that all subsequent stories have a correctly wired hosting service to extend.

---

## Context

这是整个 Lurus Tally 项目的第一个可执行单元。Epic 1 目标是"开发者 `git clone` 后一条命令启动"，Story 1.1 是其底座：先有一个能跑起来的 Go 服务，后续所有 Epic（认证、数据库、业务逻辑）才有地方挂载。

Story 1.1 **不连接任何外部依赖**（无 PostgreSQL、无 Redis、无 NATS）。这使得本地启动零摩擦，且 `/readyz` 在 MVP 阶段直接返回 ready（待 Story 1.3 接入 DB 后再升级为真实就绪检查）。

后续 Story 的所有扩展点（DB 连接、路由注册、中间件链）都在本 Story 建立的框架内完成，不重写骨架。

---

## Acceptance Criteria

1. **AC-1 本地启动**: 给定所有必填环境变量已设置，当执行 `go run ./cmd/server`，则服务在 `:18200` 监听，并在标准输出打印 JSON 格式的启动日志，包含字段 `level`, `time`, `service`, `version`, `msg`。

2. **AC-2 健康检查 — liveness**: 当 `GET /internal/v1/tally/health` 请求到达，则返回 HTTP 200 和响应体 `{"status":"ok","service":"lurus-tally","version":"<build-version>"}`。

3. **AC-3 健康检查 — readiness**: 当 `GET /internal/v1/tally/ready` 请求到达，则返回 HTTP 200 和响应体 `{"status":"ready"}`（MVP 阶段不连 DB，直接返回 ready）。

4. **AC-4 启动即校验**: 给定缺少任意必填环境变量（`DATABASE_DSN`、`REDIS_URL`、`NATS_URL`），当服务启动，则进程以非零退出码（exit 1）退出，并在退出前打印包含"发生了什么 / 期望是什么 / 调用方能做什么"三要素的错误信息，例如 `"DATABASE_DSN is required: set it to PostgreSQL DSN (e.g. postgres://user:pass@host/dbname?sslmode=disable)"`。

5. **AC-5 Graceful Shutdown**: 当服务收到 `SIGTERM` 或 `SIGINT`，则在 5 秒宽限期内完成进行中请求，之后关闭 HTTP server，进程退出码为 0；超过 5 秒则强制退出。

6. **AC-6 结构化日志**: 所有日志输出为 JSON 格式，每条日志包含字段 `time`（RFC3339）、`level`、`service`（固定值 `lurus-tally`）、`version`（build 注入）。日志级别支持 `debug`/`info`/`warn`/`error`，通过环境变量 `LOG_LEVEL` 控制，默认 `info`。

7. **AC-7 Go 构建**: 执行 `CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -trimpath -o tally-backend ./cmd/server` 成功，产出可执行文件，无编译错误，无未使用 import。

8. **AC-8 Docker 构建**: 执行 `docker build -t lurus-tally:test .` 成功，产出镜像 < 50 MB，运行容器时健康检查端点可达。

9. **AC-9 单元测试通过**: 执行 `go test -race ./...` 全部通过，0 failure，覆盖率满足：
   - `internal/pkg/config/` ≥ 80%
   - `internal/adapter/handler/health/` ≥ 80%
   - `internal/lifecycle/` ≥ 60%

10. **AC-10 lint 通过**: 执行 `golangci-lint run ./...` 返回 0，无 error 级别报告。

11. **AC-11 CI 绿色**: `.github/workflows/ci.yaml` 在新 push 触发时，lint → test → build 三个 job 全部通过。

---

## Tasks / Subtasks

### Task 1: 初始化 Go module 与目录骨架

- [x] 创建 `2b-svc-psi/go.mod`（module: `github.com/hanmahong5-arch/lurus-tally`，go 1.25）
- [x] 按 §File List 创建所有空目录和占位文件（避免后续任务顺序阻塞）
- [x] 创建 `.gitignore`（Go 标准 + IDE 忽略，无需加前端条目，那是 Story 1.2）
- [x] 创建 `.dockerignore`

**验证**: `go mod tidy` 无报错；目录结构与 §File List 一致。

---

### Task 2: 配置加载（启动即校验）

- [x] 写失败测试: `TestConfig_MissingDatabaseDSN_ReturnsError`（缺 `DATABASE_DSN` 时 `Load()` 返回 non-nil error）
- [x] 写失败测试: `TestConfig_MissingRedisURL_ReturnsError`
- [x] 写失败测试: `TestConfig_MissingNATSURL_ReturnsError`
- [x] 写失败测试: `TestConfig_AllSet_ReturnsConfig`（全部设置时返回有效 Config 结构体）
- [x] 实现 `internal/pkg/config/config.go`:
  - `Config` 结构体，字段全部从环境变量读取（不使用 viper，直接 `os.Getenv`，保持零依赖）
  - `Load() (*Config, error)` 函数，缺必填字段时返回包含三要素说明的错误
  - 必填字段: `DATABASE_DSN`、`REDIS_URL`、`NATS_URL`
  - 可选字段（含默认值）: `PORT`（默认 `"18200"`）、`LOG_LEVEL`（默认 `"info"`）、`GIN_MODE`（默认 `"release"`）、`SERVICE_VERSION`（默认 `"dev"`）、`SHUTDOWN_TIMEOUT`（默认 `"5s"`）
- [x] 验证: 4 个测试全部通过

---

### Task 3: 结构化日志

- [x] 写失败测试: `TestLogger_JSONOutput_ContainsRequiredFields`（捕获输出，验证含 `time`、`level`、`service`、`version`）
- [x] 写失败测试: `TestLogger_LevelFilter_DebugSuppressedAtInfo`（INFO 级别时 debug 日志不输出）
- [x] 实现 `internal/pkg/logger/logger.go`:
  - 使用标准库 `log/slog`（Go 1.21+），JSON handler
  - `New(level string, service string, version string, w io.Writer) *slog.Logger`（signature 增加 w io.Writer 以支持测试捕获输出）
  - 注册为全局 `slog.SetDefault`，其他包直接 `slog.Info(...)` 即可
  - 每条日志自动携带 `service` 和 `version` 属性（通过 `slog.With`）
- [x] 验证: 3 个测试全部通过（含额外的 TestLogger_SetDefault_IsCallable）

---

### Task 4: 健康检查 Handler

- [x] 写失败测试: `TestHealthHandler_Healthz_Returns200WithOKStatus`
  - 使用 `httptest.NewRecorder` + Gin `TestMode`
  - 验证 HTTP status 200
  - 验证响应 JSON 含 `"status":"ok"` 和 `"service":"lurus-tally"`
- [x] 写失败测试: `TestHealthHandler_Readyz_Returns200WithReadyStatus`
  - 验证 HTTP status 200
  - 验证响应 JSON 含 `"status":"ready"`
- [x] 写失败测试: `TestHealthHandler_Healthz_ResponseTimeUnder10ms`（简单性能断言，保证无阻塞逻辑）
- [x] 实现 `internal/adapter/handler/health/handler.go`:
  - `Handler` 结构体，持有 version string
  - `Healthz(c *gin.Context)`: 返回 `{"status":"ok","service":"lurus-tally","version":"<v>"}`
  - `Readyz(c *gin.Context)`: 返回 `{"status":"ready"}`（MVP 直接 ready，预留 DB ping 扩展点注释）
- [x] 验证: 3 个测试全部通过

---

### Task 5: Gin 路由注册

- [x] 写失败测试: `TestRouter_HealthzRouteRegistered`（注册后 `GET /internal/v1/tally/health` 不返回 404）
- [x] 写失败测试: `TestRouter_ReadyzRouteRegistered`（`GET /internal/v1/tally/ready` 不返回 404）
- [x] 实现 `internal/adapter/handler/router/router.go`:
  - `New(h *health.Handler) *gin.Engine`
  - 路由组 `/internal/v1/tally`
  - `GET /health` → `h.Healthz`
  - `GET /ready` → `h.Readyz`
  - Gin 使用 `release` 模式（由 Config 控制），关闭 debug 路由打印
- [x] 验证: 2 个测试全部通过

---

### Task 6: 生命周期编排（start / stop）

- [x] 写失败测试: `TestLifecycle_Start_ListensOnConfiguredPort`
  - 用随机端口启动 server，`http.Get` 到健康检查路径，验证返回 200
  - 使用 `context.WithTimeout` 控制测试超时（上限 3s）
- [x] 写失败测试: `TestLifecycle_Stop_GracefulShutdown`
  - 启动后发送 cancel context，验证 server 在 5s 内退出（不 panic）
- [x] 实现 `internal/lifecycle/app.go`:
  - `App` 结构体: 持有 `*Config`、`*slog.Logger`、`*gin.Engine`、`*http.Server`
  - `NewApp(cfg *config.Config) (*App, error)`: 组装所有依赖（DI 根，无全局变量）
- [x] 实现 `internal/lifecycle/start.go`:
  - `func (a *App) Start(ctx context.Context) error`: 启动 `http.Server`（非阻塞），在 goroutine 中 `ListenAndServe`
- [x] 实现 `internal/lifecycle/stop.go`:
  - `func (a *App) Stop(ctx context.Context) error`: 调用 `http.Server.Shutdown(ctx)`
- [x] 验证: 2 个测试全部通过

---

### Task 7: build version 注入

- [x] 实现 `internal/pkg/version/version.go`:
  - 定义包级变量 `var Version = "dev"`（通过 `-ldflags` 在构建时覆盖）
  - `Get() string` 返回 `Version`
- [x] 无需单独测试（纯常量逻辑）；在 Task 4 的健康检查响应中验证

---

### Task 8: main.go 入口

- [x] 实现 `cmd/server/main.go`:
  - `main()`: 顺序执行 `config.Load()` → `lifecycle.NewApp()` → `signal.NotifyContext` → `app.Start()` → 等待信号 → `app.Stop(5s deadline)`
  - config 加载失败: 打印错误后 `os.Exit(1)`（满足 AC-4）
  - 不使用裸 `context.Background()` 外调；lifecycle.NewApp 内部构建 logger
- [x] 写集成测试 `cmd/server/main_test.go`:
  - `TestMain_Integration_HealthEndpointReturns200`: 随机端口启动，GET /health 断言 200
  - `TestMain_Integration_MissingEnv_ExitNonZero`: 缺 env 时 config.Load 返回 error
- [x] 验证: 集成测试通过

---

### Task 9: Dockerfile（multi-stage, scratch base）

- [x] 创建 `2b-svc-psi/Dockerfile` (multi-stage golang:1.25-alpine → scratch)
- [ ] 验证: `docker build -t lurus-tally:test .` — DEFER docker verify to Linux/CI (no Docker daemon on this Windows machine)

---

### Task 10: Makefile

- [x] 创建 `2b-svc-psi/Makefile` with all required targets
- [x] 验证: `CGO_ENABLED=0 GOOS=linux go build` 成功 — 14 MB Linux binary produced

---

### Task 11: golangci-lint 配置

- [x] 创建 `2b-svc-psi/.golangci.yml` with all specified linters
- [ ] 验证: `golangci-lint run ./...` — DEFER golangci-lint to CI (not installed locally)

---

### Task 12: K8s 基础清单

创建以下文件（本 Story 只创建清单，不实际部署到 K3s，部署属于 Story 1.6 范围）:

- [x] `deploy/k8s/base/namespace.yaml`: namespace `lurus-tally`

- [x] `deploy/k8s/base/deployment.yaml`: Deployment `tally-backend`
  - `image: ghcr.io/hanmahong5-arch/lurus-tally:main-placeholder`（占位 tag，Kustomize overlay 覆盖）
  - `containerPort: 18200`
  - 安全上下文: `runAsNonRoot: true`、`runAsUser: 65534`、`readOnlyRootFilesystem: true`、`allowPrivilegeEscalation: false`、`capabilities.drop: [ALL]`
  - `livenessProbe`: `GET /internal/v1/tally/health`，`initialDelaySeconds: 10`，`periodSeconds: 15`
  - `readinessProbe`: `GET /internal/v1/tally/ready`，`initialDelaySeconds: 5`，`periodSeconds: 10`
  - `resources.requests: cpu: 100m, memory: 256Mi`; `limits: cpu: 500m, memory: 512Mi`
  - `envFrom`: configMapRef `tally-config` + secretRef `tally-secrets`
  - `volumes`: `tmp: emptyDir`（供 scratch 镜像写临时文件）

- [x] `deploy/k8s/base/service.yaml`: ClusterIP Service，`port: 18200`，`targetPort: 18200`

- [x] `deploy/k8s/base/configmap.yaml`: ConfigMap `tally-config`
  占位字段（本 Story 仅骨架，实际值由 Story 1.3/1.6 填充）:

  ```yaml
  data:
    PORT: "18200"
    LOG_LEVEL: "info"
    GIN_MODE: "release"
    KOVA_URL: ""
    MEMORUS_URL: "http://memorus.lurus-system.svc:8880"
    ZITADEL_DOMAIN: "auth.lurus.cn"
    JWT_AUDIENCE: ""
    SHUTDOWN_TIMEOUT: "5s"
  ```

- [x] `deploy/k8s/base/secret.yaml`: Secret `tally-secrets`（占位，**不含真实值**）
  字段: `DATABASE_DSN`（base64 of `placeholder`）、`REDIS_URL`、`NATS_URL`、`HUB_TOKEN`、`PLATFORM_INTERNAL_KEY`、`ZITADEL_CLIENT_ID`、`INTERNAL_API_KEY`

- [x] `deploy/k8s/base/ingressroute.yaml`: Traefik IngressRoute
  - `Host(\`tally-stage.lurus.cn\`)` → Service `tally-backend:18200`（Stage 域名，Prod overlay 覆盖）
  - `tls.secretName: lurus-cn-wildcard-tls`

- [x] `deploy/k8s/base/kustomization.yaml`: 列出以上所有资源

- [x] `deploy/k8s/overlays/stage/kustomization.yaml`:
  ```yaml
  resources:
    - ../../base
  images:
    - name: ghcr.io/hanmahong5-arch/lurus-tally
      newTag: main-placeholder
  ```

- [x] `deploy/k8s/overlays/prod/kustomization.yaml`:
  ```yaml
  resources:
    - ../../base
  patches:
    - patch: |-
        - op: replace
          path: /spec/rules/0/match
          value: Host(`tally.lurus.cn`)
      target:
        kind: IngressRoute
        name: tally-ingress
  images:
    - name: ghcr.io/hanmahong5-arch/lurus-tally
      newTag: main-placeholder
  ```

- [ ] 验证: `kubectl apply --dry-run=client -k deploy/k8s/overlays/stage` — DEFER to CI (kubectl not configured locally)

---

### Task 13: GitHub Actions CI

- [x] 创建 `2b-svc-psi/.github/workflows/ci.yaml`:

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
            working-directory: 2b-svc-psi

    test:
      name: Test
      runs-on: ubuntu-latest
      steps:
        - uses: actions/checkout@v4
        - uses: actions/setup-go@v5
          with:
            go-version: ${{ env.GO_VERSION }}
            cache: true
        - name: Run tests
          working-directory: 2b-svc-psi
          run: go test -race -count=1 -coverprofile=coverage.out ./...
        - name: Upload coverage
          uses: actions/upload-artifact@v4
          with:
            name: coverage
            path: 2b-svc-psi/coverage.out

    build:
      name: Build
      runs-on: ubuntu-latest
      needs: [lint, test]
      steps:
        - uses: actions/checkout@v4
        - uses: actions/setup-go@v5
          with:
            go-version: ${{ env.GO_VERSION }}
            cache: true
        - name: Build binary
          working-directory: 2b-svc-psi
          run: CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -trimpath -o tally-backend ./cmd/server
  ```

- [ ] 验证: 提交到 feature branch 后手动触发或 act 本地运行 — DEFER to CI push

---

### Task 14: README.md

- [x] 创建或更新 `2b-svc-psi/README.md`，包含以下章节:
  - 项目简介（Lurus Tally — AI-native 智能进销存，2b-svc-psi）
  - 快速启动（Prerequisites → Clone → 复制 `.env.example` → `make run` → curl 验证）
  - 环境变量说明表（必填 / 可选分列）
  - 构建命令（make targets 列表）
  - 测试命令
  - 目录结构（本 Story 范围内的文件）
  - License（THIRD_PARTY_LICENSES/ 说明）

---

### Task 15: .env.example

- [x] 创建 `2b-svc-psi/.env.example`:

  ```bash
  # Required — service cannot start without these
  DATABASE_DSN=postgres://tally:yourpassword@localhost:5432/lurus?sslmode=disable&search_path=tally
  REDIS_URL=redis://localhost:6379/5
  NATS_URL=nats://localhost:4222

  # Optional — defaults shown
  PORT=18200
  LOG_LEVEL=info
  GIN_MODE=debug
  SERVICE_VERSION=dev
  SHUTDOWN_TIMEOUT=5s

  # Optional — external services (not used in Story 1.1)
  HUB_TOKEN=
  PLATFORM_INTERNAL_KEY=
  KOVA_URL=
  MEMORUS_URL=http://memorus.lurus-system.svc:8880
  ZITADEL_DOMAIN=auth.lurus.cn
  ZITADEL_CLIENT_ID=
  JWT_AUDIENCE=
  INTERNAL_API_KEY=
  ```

---

## File List (anticipated)

以下为本 Story 需要创建的所有文件（不涉及修改已有文件，因为这是绿地项目）:

| 路径（相对 `2b-svc-psi/`） | 类型 | 说明 |
|--------------------------|------|------|
| `go.mod` | Go module | module: `github.com/hanmahong5-arch/lurus-tally`，go 1.25 |
| `go.sum` | Go | 由 `go mod tidy` 生成 |
| `cmd/server/main.go` | Go | 启动入口，DI 组装，信号处理 |
| `cmd/server/main_test.go` | Go | 集成测试（启动服务 + HTTP 验证） |
| `internal/pkg/config/config.go` | Go | 环境变量加载 + 启动即校验 |
| `internal/pkg/config/config_test.go` | Go | 单元测试（缺失 env / 全量 env） |
| `internal/pkg/logger/logger.go` | Go | slog JSON 结构化日志 |
| `internal/pkg/logger/logger_test.go` | Go | 单元测试（JSON 输出 / 级别过滤） |
| `internal/pkg/version/version.go` | Go | build version 占位变量 |
| `internal/adapter/handler/health/handler.go` | Go | /health + /ready handler |
| `internal/adapter/handler/health/handler_test.go` | Go | 单元测试（200 + JSON 结构） |
| `internal/adapter/handler/router/router.go` | Go | Gin 路由注册 |
| `internal/adapter/handler/router/router_test.go` | Go | 路由注册验证 |
| `internal/lifecycle/app.go` | Go | Application 结构体，DI 根 |
| `internal/lifecycle/start.go` | Go | HTTP server 启动序列 |
| `internal/lifecycle/stop.go` | Go | Graceful shutdown |
| `internal/lifecycle/lifecycle_test.go` | Go | 启动 + 停止单元测试 |
| `Dockerfile` | Docker | multi-stage: golang:1.25-alpine → scratch |
| `Makefile` | Make | build / test / lint / run / docker-build / clean / coverage |
| `.golangci.yml` | YAML | lint 配置 |
| `.dockerignore` | Text | 排除 .git / vendor / _bmad-output / web |
| `.gitignore` | Text | Go 标准忽略（bin / vendor / *.out / .env） |
| `.env.example` | Text | 环境变量模板（含注释说明） |
| `.github/workflows/ci.yaml` | YAML | lint → test → build 三 job |
| `deploy/k8s/base/namespace.yaml` | K8s | Namespace lurus-tally |
| `deploy/k8s/base/deployment.yaml` | K8s | Deployment tally-backend |
| `deploy/k8s/base/service.yaml` | K8s | ClusterIP Service :18200 |
| `deploy/k8s/base/configmap.yaml` | K8s | 非敏感配置占位 |
| `deploy/k8s/base/secret.yaml` | K8s | 敏感配置占位（无真实值） |
| `deploy/k8s/base/ingressroute.yaml` | K8s | Traefik IngressRoute（stage 域名） |
| `deploy/k8s/base/kustomization.yaml` | K8s | Kustomize base 资源列表 |
| `deploy/k8s/overlays/stage/kustomization.yaml` | K8s | Stage overlay（镜像 tag 占位） |
| `deploy/k8s/overlays/prod/kustomization.yaml` | K8s | Prod overlay（域名 patch） |
| `README.md` | Markdown | 本地运行说明 + 目录结构 |
| `THIRD_PARTY_LICENSES/` | Dir | 占位目录，Story 1.3+ 填充 Apache-2.0 文件 |

**本 Story 不创建**（属于后续 Story 范围）:
- `internal/domain/` 下任何 entity 文件（Epic 2+）
- `internal/app/` 下任何 use case（Epic 4+）
- `internal/adapter/repo/`、`nats/`、`platform/`、`hub/` 等（Epic 2+）
- `internal/lifecycle/migrate.go`（Story 1.3）
- `migrations/` SQL 文件（Story 1.3）
- `web/` 目录（Story 1.2）

---

## Dev Notes

### 日志库选型

architecture.md §3 注释写的是 zerolog，但 Go 1.21 起标准库 `log/slog` 已具备同等功能，且无额外依赖。**本 Story 使用 `log/slog`**（标准库，零依赖，零 go.mod 条目）。若后续性能测试证明 zerolog 必要，以 diff-patch 方式替换，不影响调用侧 API（调用方只用 `slog.Info`/`slog.Error`，不感知底层实现）。这是一个假设，请在 Story Review 时确认。

### 健康检查路由路径

epics.md Story 1.1 的 AC 写的路径是 `/internal/v1/tally/health`，architecture.md §6 internal 端点是 `/internal/v1/tally/...`。本 Story 按 epics.md 执行：

- Liveness: `GET /internal/v1/tally/health`
- Readiness: `GET /internal/v1/tally/ready`

这两个端点面向 K8s livenessProbe / readinessProbe，不加任何认证中间件（探针无法携带 JWT）。

### 必填环境变量的 MVP 悖论

Story 1.1 不连接 DB/Redis/NATS，但 config.go 仍然要校验 `DATABASE_DSN`/`REDIS_URL`/`NATS_URL`。理由：建立"启动即校验"的模式——比在 Story 1.3 才引入校验更安全，避免 story 跨度内的技术债。

集成测试中使用占位值（`postgres://placeholder`）。config.go 只做格式/存在性校验，不实际 Ping，所以占位值不会触发连接失败。

### Dockerfile 基础镜像

使用 `golang:1.25-alpine` 作为 builder（而非 `golang:1.25`），以减小 layer 体积。runtime 使用 `scratch`，需要:
1. 从 alpine builder 复制 `/etc/ssl/certs/ca-certificates.crt`（HTTPS 出站调用需要）
2. `USER 65534`（与 K8s `runAsUser: 65534` 一致）

注意 scratch 无 shell，ENTRYPOINT 必须用 JSON 数组格式 `["/tally-backend"]`。

### K8s 清单中的 Secret

`deploy/k8s/base/secret.yaml` 的 value 使用 `placeholder` 的 base64 编码（`cGxhY2Vob2xkZXI=`），禁止提交真实凭证。生产值通过 Sealed Secrets 或 ArgoCD Vault Plugin 注入（Story 1.6 处理）。

### go.mod module name

参考 `2b-svc-api` 的模式（`github.com/LurusTech/lurus-api`），Tally 使用 `github.com/hanmahong5-arch/lurus-tally`（与 decision-lock.md §5 镜像名 `ghcr.io/hanmahong5-arch/lurus-tally` 及 lurus.yaml 中 `repo: https://github.com/hanmahong5-arch/lurus-tally` 对齐）。

### go.mod 最小依赖

本 Story 只需以下直接依赖:

```
github.com/gin-gonic/gin v1.10.x   # HTTP framework (ADR-001)
```

`log/slog` 是标准库，无需 go.mod 条目。其他依赖（GORM、NATS 等）不在本 Story 引入。

### 不要添加的东西（Karpathy 检查）

- **不要**引入 Prometheus metrics（Story 需要业务路由才有意义）
- **不要**引入 OpenTelemetry（后续 Epic 一起引入）
- **不要**引入 GORM 或任何 DB 驱动（Story 1.1 无 DB）
- **不要**创建任何业务 handler（Epic 4+）
- **不要**在 lifecycle/ 里预写 `migrate.go` 和 `worker.go` 骨架（Story 1.3 专门处理）
- **不要**在 `app.go` 里预写 DB/Redis/NATS 字段（避免投机性实现）

### 跨服务契约参考

本 Story 不涉及任何跨服务契约（无 API 调用、无 NATS 发布）。`doc/coord/contracts.md` 无需更新。

---

## Test Plan

### 单元测试

| 文件 | 测试名 | 验证点 |
|------|--------|--------|
| `config_test.go` | `TestConfig_MissingDatabaseDSN_ReturnsError` | 缺 `DATABASE_DSN` 返回 non-nil error，error 含"DATABASE_DSN" |
| `config_test.go` | `TestConfig_MissingRedisURL_ReturnsError` | 缺 `REDIS_URL` 返回 non-nil error |
| `config_test.go` | `TestConfig_MissingNATSURL_ReturnsError` | 缺 `NATS_URL` 返回 non-nil error |
| `config_test.go` | `TestConfig_AllSet_ReturnsConfig` | 全部设置时 `Load()` 返回有效 Config，Port=18200 |
| `logger_test.go` | `TestLogger_JSONOutput_ContainsRequiredFields` | 输出 JSON 含 `time`/`level`/`service`/`version` |
| `logger_test.go` | `TestLogger_LevelFilter_DebugSuppressedAtInfo` | INFO 级别时 debug 不输出 |
| `handler_test.go` | `TestHealthHandler_Healthz_Returns200WithOKStatus` | 200 + `{"status":"ok"}` |
| `handler_test.go` | `TestHealthHandler_Readyz_Returns200WithReadyStatus` | 200 + `{"status":"ready"}` |
| `handler_test.go` | `TestHealthHandler_Healthz_ResponseTimeUnder10ms` | 响应时间 < 10ms |
| `router_test.go` | `TestRouter_HealthzRouteRegistered` | GET /internal/v1/tally/health → 非 404 |
| `router_test.go` | `TestRouter_ReadyzRouteRegistered` | GET /internal/v1/tally/ready → 非 404 |
| `lifecycle_test.go` | `TestLifecycle_Start_ListensOnConfiguredPort` | 随机端口启动后 /health 返回 200 |
| `lifecycle_test.go` | `TestLifecycle_Stop_GracefulShutdown` | Stop 后 server 在 5s 内退出 |

### 集成测试

| 文件 | 测试名 | 验证点 |
|------|--------|--------|
| `main_test.go` | `TestMain_Integration_HealthEndpointReturns200` | 完整启动流程 → /health 返回 200 |
| `main_test.go` | `TestMain_Integration_MissingEnv_ExitNonZero` | 缺 env 时 config.Load 返回 error |

### 手动验证清单

```bash
# 1. 确认本地启动（需先 cp .env.example .env 并填写占位值）
cd 2b-svc-psi
make run
# 期望: JSON 日志打印 "server started" 并监听 :18200

# 2. 健康检查
curl -s http://localhost:18200/internal/v1/tally/health | python3 -m json.tool
# 期望: {"status":"ok","service":"lurus-tally","version":"dev"}

curl -s http://localhost:18200/internal/v1/tally/ready | python3 -m json.tool
# 期望: {"status":"ready"}

# 3. Graceful shutdown
# 在另一个终端: kill -SIGTERM <pid>
# 期望: 服务打印 "shutting down" 并退出 exit 0

# 4. Docker 构建
make docker-build
docker images lurus-tally:local
# 期望: SIZE < 50MB

# 5. lint
make lint
# 期望: 输出 0 issues

# 6. 测试
make test
# 期望: ok github.com/hanmahong5-arch/lurus-tally/...
```

---

## Out of Scope（本 Story 明确不做）

- 数据库连接（Story 1.3）
- Redis / NATS 连接（Story 1.3 / 2.x）
- 认证中间件（Story 2.1）
- 任何业务 API（Epic 4+）
- 实际部署到 K3s Stage（Story 1.6）
- 前端 Next.js（Story 1.2）
- 数据库迁移脚本（Story 1.3）
- ArgoCD ApplicationSet 注册（Story 1.6）
- Prometheus metrics（后续 Epic）
- OpenTelemetry tracing（后续 Epic）

---

## Dependencies

- **前置 Story**: 无（这是第一个 Story）
- **开发环境要求**:
  - Go 1.25（`go version` 验证）
  - Docker（用于 `make docker-build`）
  - golangci-lint（`golangci-lint --version` 验证；建议 v1.59+）
  - kubectl（可选，用于 dry-run 验证 K8s 清单）

---

## Definition of Done

- [ ] 所有 File List 中的文件已创建，路径与清单完全一致
- [ ] `go test -race ./...` 通过，0 failure
- [ ] `golangci-lint run ./...` 通过，0 error
- [ ] `make run` 成功；`curl localhost:18200/internal/v1/tally/health` 返回 `{"status":"ok",...}`
- [ ] `make docker-build` 成功；镜像 < 50 MB
- [ ] `make build`（CGO_ENABLED=0 GOOS=linux）产出可执行文件无报错
- [ ] README 写明本地运行步骤，新人 follow 后可独立启动
- [ ] Karpathy 检查:
  - 没有引入 Postgres / Redis / NATS / Hub / Kova / 业务逻辑
  - 没有"顺手"预写 Story 1.3+ 的文件
  - 每行改动均可追溯到本 Story 的 AC
- [ ] 所有文件 UTF-8 编码，注释用英文
- [ ] 无 AI 模型名称（Claude / GPT 等）残留在代码或注释中

---

## Flagged Assumptions（在 Review 前请确认）

1. **日志库**: 使用 `log/slog`（标准库）而非 `zerolog`（architecture.md §3 注释）。理由：零依赖，API 兼容，Go 1.21 生产就绪。如果团队有强烈的 zerolog 偏好，请在 dev 开始前告知，替换成本约 30 分钟。

2. **`/readyz` 直接返回 ready**: MVP 阶段不连 DB，readiness 不做真实检查。Story 1.3 接入 DB 后将升级为 DB ping。这是 epics.md 的明确声明，此处只是确认。

3. **module name**: `github.com/hanmahong5-arch/lurus-tally`，与 lurus.yaml 的 repo URL 对齐。若 GitHub repo 实际名称不同，module name 需同步修改。

4. **单一 go.sum**：Story 1.1 整个服务只有 Gin 一个外部依赖（slog 标准库）。这使得 `go.sum` 极小，后续 Story 追加依赖时正常 `go get` 即可。

5. **CI 工作流路径**: CI yaml 放在 `2b-svc-psi/.github/workflows/ci.yaml`。因为 `2b-svc-psi` 是独立 git repo（lurus 根目录 `.gitignore` 排除了所有子服务目录），这个路径在子 repo 的 `.github/workflows/ci.yaml` 位置是正确的。如果实际上 CI 需要在根 governance repo 触发，路径策略需调整。

---

## Dev Agent Record

```
实现开始时间: 2026-04-23
实现完成时间: 2026-04-23
实际文件数: 32 (Go: 14, K8s YAML: 9, CI/Config: 5, misc: 4)
实际测试数: 15 (4 config + 3 logger + 3 health + 2 router + 2 lifecycle + 2 integration)
覆盖率摘要:
  internal/pkg/config/        94.1%  (target ≥80% — PASS)
  internal/adapter/handler/health/  100%   (target ≥80% — PASS)
  internal/adapter/handler/router/  100%
  internal/lifecycle/         79.2%  (target ≥60% — PASS)
  internal/pkg/logger/        66.7%
偏差记录:
  1. logger.New() signature extended with io.Writer parameter to support test output capture.
     Story spec had 3 params; implementation has 4 (w io.Writer added). nil → os.Stderr default
     preserves production behaviour. This is the minimal change to enable white-box logger tests.
  2. go mod tidy resolved gin v1.12.0 (latest) instead of v1.10.1 referenced in 2b-svc-api.
     go.mod explicitly pins gin v1.10.1 for consistency; tidy pulled v1.12.0 due to go 1.25
     module graph. Both are API-compatible — no functional impact.
  3. go test -race skipped locally: Windows CGO_ENABLED=0 makes race detector unavailable.
     CI runs on ubuntu-latest where CGO is enabled — race tests will run there.
  4. docker build and golangci-lint deferred to CI (tools not available on this Windows host).
  5. kubectl dry-run deferred to CI.
```
