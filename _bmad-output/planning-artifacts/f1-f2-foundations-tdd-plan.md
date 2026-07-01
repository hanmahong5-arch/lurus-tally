# F1 + F2 地基 — 可执行 TDD 计划

> 创建：2026-06-20
> 状态：READY — owner 已拍板从 F1+F2 起步（见 `./roadmap-v2-2026-06-20-multi-client-autonomy.md`）
> 范围：仅两块地基 —— **F1 OpenAPI 规格 + codegen**、**F2 持久化异步 job 引擎**。A 系列自治、B 多端、C 设备不在本计划。
> 工作分支：`feat/roadmap-multi-client-autonomy-2026-06-20`（off main，独立 worktree，**不碰 swarm 树**）。
> 纪律：`需求→RED→GREEN→重构→提交`；覆盖目标 app ≥80% / repo ≥60%；`go test ./...` 全绿才算完成（typecheck≠build≠CI）。

---

## 顺序

F1 与 F2 互相独立，可并行。单人推进则 **F1 先**（风险最低、解锁 B/C 三端、不碰 DB migration），再 F2。

---

# F1 — OpenAPI 规格 + 类型化 client codegen

## 目标（可验证）
1. `api/openapi.yaml`（OpenAPI 3.1）覆盖 `/api/v1` 客户端面，与现有 `2l-svc-platform/api/openapi.yaml` 同位置约定。
2. **规格完整性 gate**：一个测试枚举真实 router 上每个 `/api/v1` 路由+方法，断言 spec 已收录 → 防 drift。
3. spec 通过结构校验；前端能从 spec codegen 类型并 `bun run build` 通过。

## 设计决策
- **手写 spec + 完整性 gate**，不走全量 handler 注解（注解要碰上百个 handler，与 swarm 冲突面大；spec gate 同样防 drift 且零业务改动）。
- 校验用 Go 库 `github.com/getkin/kin-openapi`（`openapi3.Loader` + `Validate`），**测试内完成，不引入外部 CLI**。
- 前端 codegen 用 `openapi-typescript`（bun devDep），产出 `web/lib/api/_generated/schema.d.ts` 作类型真源；现有手写 client 增量改为引用生成类型，不一次性重写。

## 文件改动
- 新 `api/openapi.yaml` —— 首批覆盖"第一张 PO"关键路径 + 鉴权：
  `GET /api/v1/me`、products CRUD、`POST /api/v1/suppliers`、`POST /api/v1/warehouses`、purchase-bills、sale-bills、payments、`GET /api/v1/stock/snapshots`、`GET /api/v1/stock/movements`、replenish suggestions/draft-batch、PAT CRUD。
- 新 `tests/contract/openapi_coverage_test.go` —— 复用 rls_e2e 的真 router 构造器（见 `tests/integration/rls_e2e_test.go` 如何 build router），调 `engine.Routes()` 枚举，过滤 `/api/v1`，断言每条在 spec 的 paths+method 中存在；spec 多余路径也报错（双向）。
- 新 `tests/contract/openapi_valid_test.go` —— `openapi3.Loader.LoadFromFile` + `doc.Validate(ctx)`。
- `web/package.json` —— 加 `openapi-typescript` devDep + `gen:api` script；`web/lib/api/_generated/`（gitignore 生成物或提交，二选一在实现时定）。
- `.github/workflows/*.yaml` —— 加 step：跑 contract test + 前端 `gen:api` 后 `bun run build`（codegen smoke）。

## RED → GREEN 里程碑
1. **RED**：写 `openapi_coverage_test.go`，`api/openapi.yaml` 仅含 1 条路径 → 测试列出所有未收录路由（红）。
2. **GREEN**：逐路径补 spec 至 coverage 测试绿；`openapi_valid_test.go` 绿。
3. **GREEN**：`cd web && bun run gen:api && bun run build` 通过；至少一个手写 client 模块切到生成类型仍编译。

## 验证命令
```bash
go test ./tests/contract/...                 # coverage + valid 双绿
cd web && bun run gen:api && bun run build    # codegen smoke + 前端构建
```

## 风险 / 注意
- 低。零业务逻辑改动。
- coverage 测试须**复用 rls_e2e 已有的 router builder**，勿另起一套（参考 memory：rls_e2e 驱动真 router；router 构造有唯一入口）。
- spec 首批只覆盖客户端关键面，`/internal/v1` 与 webhooks 可标 `x-internal` 暂不纳入 gate（在测试过滤里排除并 `log` 说明，勿静默漏）。

---

# F2 — 持久化异步 job 引擎

## 目标（可验证）
长任务可 **续跑 / 取消 / 看进度 / 重试 / 多副本安全**，复用现有 outbox 模式与 RLS service-bypass。为 A1/A2 自治与设备长流程提供底座。

## 设计：复用 outbox 模式，扩状态机
现成模板：`internal/adapter/nats/outbox_worker.go`（30s ticker + `FOR UPDATE SKIP LOCKED` + attempts/last_error）、`internal/adapter/usagereport/retry_worker.go`（独立 tx 入队 + 稳定幂等键）、mig 000035/000053（partial index + RLS FORCE + service bypass）。

## F2.1 — migration `tally.async_jobs`
> **先在 `doc/coord/migration-ledger.md` 预留下一个空闲 ID（当前 main=000053，feat=000054，swarm churn 中 → 取 ledger 给的号）再写 SQL。** 本文用占位 `0000NN`。

```sql
-- 0000NN_async_jobs.up.sql
CREATE TABLE tally.async_jobs (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id       UUID NOT NULL,
  job_type        TEXT NOT NULL,
  state           TEXT NOT NULL DEFAULT 'pending',  -- pending|running|succeeded|failed|cancelled
  input           JSONB NOT NULL DEFAULT '{}',
  output          JSONB,
  progress        SMALLINT NOT NULL DEFAULT 0,       -- 0..100
  attempts        INT NOT NULL DEFAULT 0,
  max_attempts    INT NOT NULL DEFAULT 5,
  last_error      TEXT,
  scheduled_for   TIMESTAMPTZ,                       -- NULL=立即；否则 <=now() 才领取
  started_at      TIMESTAMPTZ,
  finished_at     TIMESTAMPTZ,
  cancel_requested BOOLEAN NOT NULL DEFAULT false,
  timeout_seconds INT NOT NULL DEFAULT 1800,
  parent_job_id   UUID REFERENCES tally.async_jobs(id),
  trace_id        UUID,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT async_jobs_state_chk CHECK (state IN ('pending','running','succeeded','failed','cancelled')),
  CONSTRAINT async_jobs_progress_chk CHECK (progress BETWEEN 0 AND 100)
);
-- 领取索引（pending 且到点）
CREATE INDEX async_jobs_claim_idx ON tally.async_jobs (scheduled_for NULLS FIRST, created_at)
  WHERE state = 'pending';
-- reaper 索引（卡死的 running）
CREATE INDEX async_jobs_running_idx ON tally.async_jobs (started_at) WHERE state = 'running';
-- RLS：FORCE + service bypass，对齐 mig 000035/000053 不变式
ALTER TABLE tally.async_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE tally.async_jobs FORCE ROW LEVEL SECURITY;
-- policy 用与 outbox 相同的 tenant GUC + 'service' 短路 CASE（照搬 000042/000053 写法）
```
down 迁移 drop table（与现有 up/down 配对纪律一致）。

## F2.2 — domain `internal/domain/job/job.go`（纯逻辑，表驱动测试）
- `Job` 实体 + `State` 枚举 + 合法转移函数：`pending→running→{succeeded|failed}`、`running→pending`（重试，attempts<max）、`pending/running→cancelled`、终态不可变。
- 纯函数 `NextOnError(j) State`（attempts+1<max → pending 重试；否则 failed）、`CanClaim(j, now)`、`IsStale(j, now)`。
- **RED**：`job_test.go` 表驱动覆盖每条合法/非法转移、重试耗尽→failed、终态写入被拒、`IsStale` 边界。

## F2.3 — repo `internal/adapter/repo/asyncjob/store.go`
接口：`Enqueue`（独立 tx，照 usage_report_outbox）、`Claim(n)`（`SKIP LOCKED`，pending→running 原子）、`MarkSucceeded/MarkFailed/Reschedule`、`UpdateProgress`、`RequestCancel`、`ReapStale(olderThan)`（running→pending）、`GetByID`、`ListByTenant`。
- **RED**：真 PG 集成测试（testcontainers，参考 memory：tally 集成 docker 可跑、`-race` 不可用）。覆盖：claim 多 worker 不重复领取（并发 goroutine + SKIP LOCKED）；retry 回 pending；reaper 捞 stale running；幂等。
- **RLS FORCE 证明**：须建 NOSUPERUSER owner 角色 + `ALTER TABLE ... OWNER`（memory：testcontainers 默认 superuser 绕过 RLS）；断言跨租户不可见、service 角色可见。

## F2.4 — app `internal/app/job/runner.go`
- `JobHandler` 接口：`Type() string` + `Execute(ctx, JobContext) (output, error)`；`JobContext` 暴露 `Input`、`SetProgress(pct)`、`Cancelled() bool`（协作式取消，轮询 cancel_requested）。
- `Runner`：注册表 type→handler；`RunOnce(ctx)` 领一批→逐个执行→落 succeeded/failed/重试；超时用 `context.WithTimeout(timeout_seconds)`。
- **RED**：注册 fake handler，enqueue→`RunOnce`→断言 succeeded + output；failing handler 重试到 max→failed；handler 内观察到 `Cancelled()` 后 RequestCancel→cancelled；progress 落库。
- 失败错误三要素包装（`fmt.Errorf("...: %w")`），不裸抛。

## F2.5 — worker 接线 `internal/adapter/job/worker.go` + lifecycle
- `JobWorker`：ticker（默认 10s，常量/配置）调 `Runner.RunOnce`；`ReaperWorker`：周期 `ReapStale`。照搬 `outbox_worker.go` 结构 + 指标（pending count / oldest age）。
- `internal/lifecycle/app.go`：在 outbox/usage worker 旁 `go jobWorker.Run(ctx)` + reaper；`stop.go` 注册 context 取消，优雅 drain。
- `internal/pkg/config/config.go`：加 `JOB_WORKER_INTERVAL`、`JOB_REAPER_INTERVAL`（可选，有默认；对齐现有 optional() 风格）。
- **验证**：boot e2e —— enqueue 一个 job → 启服务 → 断言被处理（参考 platform 的 boot-e2e 模式或现有 lifecycle 测试）。

## F2.6 — RLS e2e 防回归
- 扩 `tests/integration/rls_e2e_test.go`：加 async_jobs 跨租户隔离用例（A 租户建 job，B 租户列不到；service 角色全见）。
- **注意**（memory）：rls_e2e 驱动真 router；本 phase **不新增 always-applied 拒绝型中间件**，故不会炸 doReq；但若实现中顺手加了，须同步改 doReq（每 POST 带唯一 Idempotency-Key）。

## 验证命令（F2 全绿才算完成）
```bash
go build ./...
go test ./internal/domain/job/... ./internal/app/job/...        # 纯逻辑 + runner
go test ./internal/adapter/repo/asyncjob/...                     # 真 PG 集成（docker，无 -race）
go test ./tests/integration/ -run RLS                            # RLS e2e 含 async_jobs
# migration up/down 配对：迁到 head 再 down 一格再 up，无错
```

## 风险 / 注意
- 中。新表 + 状态机 + 双 worker。
- **migration 号必须经 ledger**，勿提前硬编（swarm churn）。
- **测 RLS FORCE 必须 NOSUPERUSER owner**，否则 superuser 绕过 → 假绿（memory 已记两次踩坑）。
- reaper 的 stale 判定用 `now()-started_at > timeout_seconds`，避免长任务被误判重跑——timeout 要给足或 handler 定期 `SetProgress` 续约（可选：进度更新顺带 bump started_at 当心跳，实现时定）。

---

## 完成定义（Definition of Done）
- F1：contract 双测绿 + 前端 codegen smoke 绿 + spec 进 CI。
- F2：domain/app 单测绿 + repo 集成绿（真 PG + NOSUPERUSER RLS）+ boot e2e 绿 + RLS e2e 含 async_jobs 绿 + migration 配对绿。
- 两者：`go build ./...` + `golangci-lint run ./...` 绿；不替 owner 决定 commit（写完待 owner 确认再 commit/push）。
