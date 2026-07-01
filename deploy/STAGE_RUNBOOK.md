# Tally STAGE 直部署 Runbook (R6 / 100.122.83.20)

ArgoCD ApplicationSet 不接管 Tally STAGE，部署走人工 `ssh + kubectl apply -k`。决策见 governance repo `lurus/doc/decisions/0006-tally-stage-direct-deploy.md`（跨 repo，sibling clone）。

## 1. 前提清单（一次性）

✅ 必须先完成：

- **R6 SSH 通**：`ssh root@100.122.83.20 "kubectl get nodes"` 能正常返回
- **Zitadel 客户端注册**（浏览器手工）：
  1. 登 https://auth.lurus.cn
  2. Projects → 选 Lurus → Applications → New
  3. 类型 `Web`，Authentication `PKCE` 或 `Client Secret Basic`
  4. Redirect URI: `https://tally-stage.lurus.cn/api/auth/callback/zitadel`
  5. Post-logout URI: `https://tally-stage.lurus.cn`
  6. 拿到 `client_id` + `client_secret`
- **凭证收集**（建议从 `重要信息.md` 取）：
  - `DATABASE_DSN` — Tally PG schema connection string
  - `REDIS_URL` — `redis://...:6379/5`
  - `NATS_URL` — `nats://nats.lurus-platform.svc:4222`
  - `PLATFORM_INTERNAL_KEY` — Lurus Platform 内部 API bearer
  - `NEWAPI_API_KEY` — Hub LLM bearer (newapi.lurus.cn)
  - `MEMORUS_API_KEY` — memorus X-API-Key（可空，会降级 disabled）
  - `NEXTAUTH_SECRET` — `openssl rand -base64 32` 现场生成
  - `ZITADEL_CLIENT_ID` / `ZITADEL_CLIENT_SECRET` — 上一步拿到

## 2. Secret 注入（一次或凭证轮换时）

> **2026-05-18 漂移 note**: 实际运行中的 `tally-secrets` 与本节模板有差异。当前 9 keys: `AUTH_SECRET` (= NextAuth v5 重命名的 `NEXTAUTH_SECRET`) / `DATABASE_DSN` / `HUB_TOKEN` (deprecated placeholder) / `INTERNAL_API_KEY` (deprecated placeholder) / `NATS_URL` / `NEWAPI_API_KEY` / `PLATFORM_INTERNAL_KEY` / `REDIS_URL` / `ZITADEL_CLIENT_ID`。缺 `MEMORUS_API_KEY` (会降级 disabled) / `NEXTAUTH_URL` / `ZITADEL_ISSUER` / `ZITADEL_CLIENT_SECRET` — pod ready 在跑, 但端到端登录链路是否通畅未实测。下节模板保留作初始注入参考, 实际轮换前请先 `kubectl get secret tally-secrets -o jsonpath='{.data}' | jq 'keys'` 比对。
>
> **2026-06-04 update (RLS Wave-3 deploy, main-da39944)**: backend 现**硬要求** `ZITADEL_AUDIENCE`（`ZITADEL_DOMAIN` 非空时 `config.go` fast-fail，见 `ZITADEL_AUDIENCE is required when ZITADEL_DOMAIN is set`）。已注入 `ZITADEL_AUDIENCE` = `ZITADEL_CLIENT_ID` 值（语义：Tally 的 Zitadel client id = 预期 JWT `aud`）→ secret 现 10 keys。**若 aud 实际应为 project id 而非 client id，JWT 登录会 401（PAT 不受影响）——STAGE 真实 Zitadel 登录仍待实测。** 迁移 head 已从 v31 跃至 **v48**（FORCE+pin RLS 全量 + Phase-3 strict flip 已在 STAGE 生效，role `tally` 非超级用户故 fail-closed 真绑定）。
>
> **2026-06-13 fix (platform key drift)**: `PLATFORM_INTERNAL_KEY` 与 platform-core 的 `INTERNAL_API_KEY` **漂移了**（tally 持 45-char 旧值, platform 当前 64-char）→ 所有 platform 内部调用 401 → tally 映射为 `502 platform_auth_failed`（UAT J4 实测；billing/overview、notification、unified-billing usage 上报全受阻）。已 `kubectl -n lurus-tally patch secret tally-secrets` 把 `PLATFORM_INTERNAL_KEY` 对齐到 platform `platform-core-secrets.INTERNAL_API_KEY`（platform 仍认 legacy key → AllScopes 含 `usage:report`），重启 tally-backend。验证：billing/overview 由 502 → 404 Account not found（鉴权穿过）；集群内直打 platform `/internal/v1/usage/events`（product_id=lurus-tally）→ `{"ok":true}` 200 + 幂等重放。**根因是共享内部 key 轮换时 tally 未同步**；下次 platform 轮换 `INTERNAL_API_KEY` 必须同步 patch 此处。tally-web 不带此 key、无 scraper 抓 tally `/metrics`，故对齐无 inbound-gate 连锁。
>
> **2026-06-13 deploy (request timeout restored, main-da76ee4)**: backend 从 `main-2e99cf6` 升至 `main-da76ee4`（PR #8）。① 重新引入 per-request 超时——改为 race-free 的 **context-deadline** 中间件（`middleware.RequestTimeout(30s, isStreamingRoute)`，无 goroutine/无缓冲 writer，旧版 `gin.Context` 跨 goroutine 竞争已根除；SSE `/ai/chat` 与 `*.csv` 排除）② 顺带修 hardening 分支预存的 AI orchestrator 测试 race（`fakeExecutor.calls` → `atomic.Int64`，PR #7 靠调度侥幸过 CI）。验证：CI `-race` 全绿 → rollout success，新 pod 1/1 Running，migration head 仍 **51** dirty=false，公网 `/internal/v1/tally/ready` 200、`/api/v1/me`(无 token) 401。回滚见 §6（`rollout undo` 回上一版 main-2e99cf6）。
>
> **2026-06-13 deploy (startup hardening, main-5f127f5)**: backend 从 `main-da76ee4` 升至 `main-5f127f5`（PR #9）。① **auth-boundary fast-fail**：`config.go` 现在当 `ZITADEL_DOMAIN` 为空**且** `TALLY_DEV_MODE != true` 时启动直接报错（空 `ZITADEL_DOMAIN` 会让整个 `/api/v1` 无鉴权裸奔）。**对本 STAGE 无影响**——overlay 的 `tally-config` 已设 `ZITADEL_DOMAIN=test-auth.lurus.cn`，走 auth-enabled 路径;仅"裸 env 本地 `go run`"现需显式 `TALLY_DEV_MODE=true`。② pagination `offset` 加上限 100000(防深分页 seq-scan DoS)③ AI `AsyncWriteMemory` 加 5s 超时(memorus 当前 disabled,潜伏修复)。验证:CI `-race` 全绿 → rollout success,新 pod 1/1 Running 无 crashloop(证明 gate 不误伤已设 ZITADEL_DOMAIN 的部署配置),migration head 仍 **51**,公网 `/ready` 200、`/api/v1/me`(无 token) 401。回滚 §6 回 main-da76ee4。
>
> **2026-06-13 deploy (hardening wave 2, main-46b0a4d)**: backend 从 `main-5f127f5` 升至 `main-46b0a4d`（PR #10 + #11；PR #12 test-backfill 运行期 inert，一次 overlay bump 合并三者后部署）。① **Idempotency-Key 强制**：新增 always-applied `RequireIdempotencyKey` 中间件，对 5 条高危写路由(`POST /payments`、`/purchase-bills/:id/approve`、`/sale-bills/:id/approve`、`/sale-bills/quick-checkout`、`/ai/plans/:plan_id/confirm`)缺 header → 400 `missing_idempotency_key`(原 opt-in dedup 在无 Redis 时是 no-op，故枚举独立中间件保证一致)。② **PAT scopes 移除**：create 硬编码 `['read']`、resolver 丢弃、无 `HasScope` 强制 → 死代码删除；**migration 000052** `DROP COLUMN scopes`(boot 时 RunMigrations 跑 → head **51→52** dirty=false)。验证:CI `-race` 全绿(含 PR #10 修 rls_e2e doReq 缺 key)→ rollout success,新 pod 1/1 Running 无 crashloop;启动 log `migration completed version 52 dirty=false`;公网 `/ready` 200、`/api/v1/me`(无 token) 401;UAT PAT `POST /payments` 无 key → **400 missing_idempotency_key**(鉴权后、handler 前拦截，无 payment 副作用);bogus bill approve **带 key** → 404 `bill_not_found`(穿过 gate),同 key 重放 → `idempotent-replay: true`(Redis dedup live);`POST /auth/pats` → 201 响应**无 `scopes` 字段**且写入成功(证明移除未 gate 写)。回滚 §6 回 main-5f127f5(注意:回滚后 head 仍 52,scopes 列已 drop;如需恢复列须手动跑 000052.down)。
>
> **2026-06-16 deploy (seed-demo RefInit fix, main-5007577)**: backend 从 `main-46b0a4d` 升至 `main-5007577`(PR #13)。**修 P0** —— onboarding seed-demo 对所有新租户 500:`stockAdapter` 记开账库存设 `ReferenceType=init` 但不带 `ReferenceID`,migration 000034 把 `stock_movement.reference_id` 设为 NOT NULL → 每次 stock-init INSERT 触发 SQLSTATE 23502 → 500(此前误判"horticulture persona 特定",实为全 persona——retail/cross_border demo 产品已存在→no-op 不碰该路径,故假象)。修复 `internal/app/stock/usecase.go`:`RecordMovement` 在 `RefInit && ReferenceID==nil` 时自引用 `&m.ID`(开账无源单据,movement 即自身来源);仅命中当前必 500 的路径,不改任何现有可用行为。无 migration,head 仍 **52**。回归测试 `tests/integration/stock_refinit_reference_test.go` 真 PG RED→GREEN。验证:CI `-race`+integration 全绿 → rollout success,pod `main-5007577` 1/1 Running 0 重启;**STAGE 端到端确证**——在干净的 UAT **secondary** 租户 `POST /onboarding/seed-demo` → 200 `products_created:3` + 3 条初始库存快照(on_hand 5/45/60)(修复前此路径必 23502→500)。⚠️ J1 在 primary 租户复跑因 DEMO-RT 已存在而 no-op(`products_created:0`,未触发该路径),不构成验证——以 secondary 干净探针为准。**同 PR**:palette e2e selector test-bug 修(`{exact:true}`);`/confirm` 幂等-先于-plan_id 校验注为 intended(won't-fix)。🆕 **本次审计顺带发现新 bug(未修)**:`POST /onboarding/clear-demo` 500(SQLSTATE 23503)—— `stock_movement.product_id` 是 `ON DELETE RESTRICT`(migration 000022),DEMO 产品有库存流水时删不掉(`onboarding/repo.go` 注释误称 CASCADE);待单独修。回滚 §6 回 main-46b0a4d。
>
> **2026-06-16 deploy (clear-demo FK fix, main-09c3a0c)**: backend 从 `main-5007577` 升至 `main-09c3a0c`(PR #14)。修上一条标记的 clear-demo 500:`DeleteDemoProducts` 改为在租户 pinned 事务内(`dbscope.BeginTx`)先删 DEMO 产品的 `stock_movement`/`lot`/`snapshot` 再删产品(原 FK `ON DELETE RESTRICT` → 23503;repo 注释误称 CASCADE 已纠正)。无 migration,head 仍 **52**。回归测试 `tests/integration/onboarding_clear_demo_test.go` 真 PG RED(23503 `stock_snapshot_product_id_fkey`)→GREEN。验证:CI `-race`+integration 全绿 → rollout success,pod `main-09c3a0c` 1/1 ready 0 重启;**STAGE 端到端确证**——UAT secondary 租户(此前 seed-demo 验证留了 3 个带库存 DEMO 产品=触发态)`POST /onboarding/clear-demo` → **204**(修前 500),之后 `?q=DEMO-` 产品 0、snapshots 0(产品 + 库存行全清,顺带清掉残留)。回滚 §6 回 main-5007577。

> **2026-06-16 deploy (3 UAT product bugs + webhook routing, main-ba458ba)**: backend 从 `main-09c3a0c` 升至 `main-ba458ba`(PR #15 + #16)。三个后端修复 + 一个路由修复,无 migration,head 仍 **52**。① **payments 永 422 (P0)**:payment repo `SumByBill` 用 `SELECT COALESCE(SUM(amount),0) … FOR UPDATE`,PG 禁聚合行锁(SQLSTATE 0A000)→ 录款永远失败。超付串行化其实已由 `RecordPaymentUseCase` 的 `GetBillForUpdate` 锁 bill_head 行保证,聚合上的 FOR UPDATE 既非法又冗余,已删。② **FK 23503 裸 500**:`httperr` 新增 PG 约束分类器(23503→409 `invalid_reference`、23505→409 `duplicate`,静态安全消息不泄漏驱动细节;23502/23514 仍 500 不掩盖 server bug),接入 `WriteInternal`+`AsError`。③ **PAT 下 creator 缺失**:replenish draft-batch(原 500)/ imports(原 422)在无 Zitadel sub 时把 `creator_id` 兜底为 tenant id(与 payment handler 一致;creator_id 无 FK,机器/集成 actor 哨兵)。验证:CI `-race`+integration 全绿 → rollout success,pod `main-ba458ba` 1/1 ready 0 重启;**STAGE 端到端确证(UAT primary PAT)**——(#2)建采购单(qty2×50=100)→ approve 200 → `POST /payments` 30 → **201**、再 20 → **201**、`GET /payments` 列 2 笔(修前必 422);(#5)`POST /purchase-bills` 带不存在 partner_id → **409 `invalid_reference`**(修前 500);(#6)PAT `POST /replenish/draft-batch` → **200** 建草稿 `PO-…`(修前 500),草稿 `creator_id`=tenant id。回滚 §6 回 main-09c3a0c。

> **2026-06-16 STAGE 边缘漂移 (webhook 路由, 非 GitOps)**: PR #16 给 `IngressRoute` 加了 `PathPrefix(/webhooks)` → 后端,**那是 PROD(Traefik)真源**。R6 STAGE 用 node-level nginx(Traefik CRD 缺失,IngressRoute inert),故 STAGE 边缘**手工**改 `/etc/nginx/sites-enabled/lurus-stage`:backend location 正则 `^/(api/v1|internal)/` → `^/(api/v1|internal|webhooks)/`(`nginx -t` + `systemctl reload`)。修前 `POST /webhooks/shopify/orders` 被 Next.js auth middleware **307 → /login**(Go HMAC 校验不可达);修后 **401 `invalid_signature`**(直达后端 HMAC),`/api`+`/`+`/internal` 路由无回归。⚠️ 这是 R6 上的配置漂移,不在 git;重装 nginx 须重做。备份在 R6 `/root/lurus-stage.bak-20260616`。

> **2026-06-20 change (self-service registration visibility, branch `feat/roadmap-multi-client-autonomy-2026-06-20`)**: 让匿名访客能看 `/pricing` 自助注册入口。三处改动，**待部署**——线上当前 `curl -I https://tally-stage.lurus.cn/pricing` 仍 **307 → /login**（旧 web 镜像 main-e02788a 强制鉴权），须按下面 注册→patch→rollout 生效后才会 200。
> 1. **注册（一次性，浏览器）**：按 §1 在 Zitadel（STAGE issuer `test-auth.lurus.cn`）注册 Web 客户端，拿 `client_id`。
> 2. **patch（GitOps，已入 overlay）**：`overlays/stage/kustomization.yaml` 新增 `ZITADEL_AUDIENCE` configmap patch，占位 `<OIDC_CLIENT_ID>` 标 **MUST_SET**。`apply` 前把占位替换为第 1 步的 `client_id`（= secret `ZITADEL_CLIENT_ID`；⚠️ envFrom 中 secret 覆盖 configmap，两处须一致）。此前 audience 仅靠手工注入 secret（drift，见 §2 2026-06-04），现 overlay 自描述。⚠️ main 上 backend 读 `ZITADEL_AUDIENCE`（feat 分支改名 `OIDC_AUDIENCE` 未并入 main）。
> 3. **web `/pricing` 公开**：`web/middleware.ts` matcher 加 `pricing` 豁免 → `/((?!login|pricing|api|_next|favicon.ico).*)`，匿名直达 `/pricing` 不再 307→login。**须重新构建 `tally-web` 镜像**并 bump `kustomization.yaml` 的 `images.newTag`（`ghcr.io/.../lurus-tally-web`），否则线上仍跑旧镜像。注：`/pricing` 公开**只**依赖 web 镜像，与后端 IdP 无关——**不受 Casdoor STAGE 是否就绪影响**（豁免路径 middleware 根本不跑、不碰鉴权）。
> ⚠️ **IdP 现实更新（2026-06-20，ADR-0018）**：平台已 Zitadel→**Casdoor**。本节第 1/2 步的"Zitadel 注册 / `test-auth.lurus.cn` issuer"已陈旧——STAGE 后端鉴权应指 **Casdoor STAGE issuer**（待 Casdoor pod 起，ADR-0018 Phase 0），audience = Tally 的 **Casdoor** client id。但这条仅影响**登录/鉴权**，**不影响 `/pricing` 200**（见上）。
> 4. **rollout**：替换 audience 占位 + bump web tag 后，按 §3 `kubectl kustomize deploy/k8s/overlays/stage | ssh root@100.122.83.20 "kubectl apply -f -"`，§4 `rollout status`。验收：`curl -I https://tally-stage.lurus.cn/pricing` → **HTTP 200**（非 307→login）；`/internal/v1/tally/ready` 200；`/api/v1/me`（无 token）401。回滚见 §6。

替换尖括号内的实际值后整段执行：

```bash
ssh root@100.122.83.20 "kubectl create namespace lurus-tally --dry-run=client -o yaml | kubectl apply -f -"

ssh root@100.122.83.20 "kubectl -n lurus-tally create secret generic tally-secrets \
  --from-literal=DATABASE_DSN='<DATABASE_DSN>' \
  --from-literal=REDIS_URL='<REDIS_URL>' \
  --from-literal=NATS_URL='<NATS_URL>' \
  --from-literal=PLATFORM_INTERNAL_KEY='<PLATFORM_INTERNAL_KEY>' \
  --from-literal=NEWAPI_API_KEY='<NEWAPI_API_KEY>' \
  --from-literal=MEMORUS_API_KEY='<MEMORUS_API_KEY>' \
  --from-literal=NEXTAUTH_SECRET='<NEXTAUTH_SECRET>' \
  --from-literal=NEXTAUTH_URL='https://tally-stage.lurus.cn' \
  --from-literal=ZITADEL_CLIENT_ID='<ZITADEL_CLIENT_ID>' \
  --from-literal=ZITADEL_AUDIENCE='<ZITADEL_CLIENT_ID>' \
  --from-literal=ZITADEL_CLIENT_SECRET='<ZITADEL_CLIENT_SECRET>' \
  --from-literal=ZITADEL_ISSUER='https://auth.lurus.cn' \
  --from-literal=HUB_TOKEN='deprecated' \
  --from-literal=INTERNAL_API_KEY='deprecated' \
  --dry-run=client -o yaml | kubectl apply -f -"
```

> `HUB_TOKEN` / `INTERNAL_API_KEY` 是 base/secret.yaml 列出但 config.go 不读的旧 key，仅为 envFrom 兼容性占位（值可任意）。

## 3. 部署 / 升级

```bash
# 本地（PowerShell / Git Bash）从仓库根 cd 到 2b-svc-psi
cd C:/Users/Anita/Desktop/lurus/2b-svc-psi

# 渲染 stage overlay 并通过 ssh 远程 apply
kubectl kustomize deploy/k8s/overlays/stage | ssh root@100.122.83.20 "kubectl apply -f -"
```

> 如果本地无 `kubectl`：在 R6 上 clone repo 后跑 `kubectl apply -k 2b-svc-psi/deploy/k8s/overlays/stage`。

**镜像 tag 升级**：编辑 `deploy/k8s/overlays/stage/kustomization.yaml` 的 `images.newTag`，git commit，重跑上面 apply 命令。

## 4. 验证

```bash
ssh root@100.122.83.20 "kubectl -n lurus-tally rollout status deploy/tally-backend --timeout=180s"
ssh root@100.122.83.20 "kubectl -n lurus-tally rollout status deploy/tally-web --timeout=180s"
ssh root@100.122.83.20 "kubectl -n lurus-tally get pods,svc,ingressroute"

# 健康检查 — /ready 真实 ping DB(必需)/Redis(可选)。
# 200 = ready；503 = not_ready（响应体含具体哪个 dep 挂了）。
# rollout status 阶段若长时间卡 0/1 ready，先看这个。
curl -fsS https://tally-stage.lurus.cn/internal/v1/tally/ready | jq .
curl -fsS https://tally-stage.lurus.cn/  # 前端 200 即可

# 烟测（需 X-Tenant-ID，dev 模式下）
curl -fsS -H "X-Tenant-ID: <tenant-uuid>" https://tally-stage.lurus.cn/api/v1/stock/snapshots
```

## 5. 排障

```bash
# Pod 启动失败：看 init / config 报错
ssh root@100.122.83.20 "kubectl -n lurus-tally logs deploy/tally-backend --tail=100"

# Secret 缺 key：config.go required() 会 fast-fail，错误信息会指出缺哪个
ssh root@100.122.83.20 "kubectl -n lurus-tally describe pod -l app=tally-backend | grep -A2 'Error'"

# 描述当前 secret keys（不暴露值）
ssh root@100.122.83.20 "kubectl -n lurus-tally get secret tally-secrets -o jsonpath='{.data}' | jq 'keys'"

# 前端 NextAuth 报错：通常是 NEXTAUTH_URL / ZITADEL_* 配错
ssh root@100.122.83.20 "kubectl -n lurus-tally logs deploy/tally-web --tail=100 | grep -i 'auth\|zitadel\|callback'"
```

## 6. 回滚

```bash
ssh root@100.122.83.20 "kubectl -n lurus-tally rollout undo deploy/tally-backend"
ssh root@100.122.83.20 "kubectl -n lurus-tally rollout undo deploy/tally-web"
ssh root@100.122.83.20 "kubectl -n lurus-tally rollout status deploy/tally-backend"
```

## 7. 升 PROD 触发条件

按 `lurus.yaml` server-landing-policy 升 R1：
- ✅ CI 全绿（`go test ./... && bun run build && bun run lint`）
- ✅ STAGE 持续运行 ≥1 周无 crashloop / OOM
- ✅ 真实客户接入（不是内部测试）
- ✅ 监控数据接入（Prometheus + 日志）

满足后：在 governance repo 的 `deploy/argocd/appset-services.yaml` 取消 `lurus-tally` element 注释（届时 R1 = PROD = in-cluster destination 直接成立），ArgoCD 接管 PROD 部署，STAGE 继续走本 runbook 直至 ADR-0006 重审。

## Appendix: 当前运行状态查询

```bash
ssh root@100.122.83.20 "kubectl -n lurus-tally get pods -o wide"
ssh root@100.122.83.20 "kubectl -n lurus-tally top pods 2>/dev/null"
ssh root@100.122.83.20 "kubectl -n lurus-tally exec deploy/tally-backend -- env | grep -E '^(DATABASE|REDIS|NATS|PLATFORM|NEWAPI|ZITADEL)_' | sed 's/=.*/=***/'"
```

## 8. PG backup CronJob (S0.Q4)

每日 02:00 UTC（北京 10:00）`pg_dump --schema=tally --format=custom` → MinIO `s3://tally-backup/<YYYY-MM-DD>.dump`。14 天保留。Manifest: `deploy/k8s/base/cronjob-pgbackup.yaml`。

### 验证 CronJob 正常调度
```bash
ssh root@100.122.83.20 "kubectl -n lurus-tally get cronjob tally-pgbackup"
ssh root@100.122.83.20 "kubectl -n lurus-tally get jobs -l app.kubernetes.io/name=tally-pgbackup --sort-by=.metadata.creationTimestamp"
ssh root@100.122.83.20 "kubectl -n lurus-tally logs jobs/<latest-job-name>"
```

### 手动触发一次
```bash
ssh root@100.122.83.20 "kubectl -n lurus-tally create job --from=cronjob/tally-pgbackup drill-$(date +%s)"
ssh root@100.122.83.20 "kubectl -n lurus-tally logs -f jobs/drill-XXX"
```

成功标志：log 末尾 `>> ok`，MinIO bucket 见 `<date>.dump` 文件 > 1MB。

### Restore drill（S0 sprint exit 硬要求，至少跑一次）
脚本 `bin/pg-restore-drill.sh`（如存在）或手动：
```bash
# 1. 从 MinIO 拉最新 dump 到一个临时位置
ssh root@100.122.83.20 "kubectl -n lurus-tally exec deploy/tally-backend -- mc cp s3/tally-backup/$(date -u +%Y-%m-%d).dump /tmp/drill.dump"

# 2. 创建临时 schema 并 restore
ssh root@100.122.83.20 'kubectl -n lurus-tally exec deploy/tally-backend -- psql "$DATABASE_DSN" -c "CREATE SCHEMA tally_restore_test;"'
ssh root@100.122.83.20 'kubectl -n lurus-tally exec deploy/tally-backend -- pg_restore --no-owner -d "$DATABASE_DSN" --schema=tally --schema-rename=tally:tally_restore_test /tmp/drill.dump'

# 3. 对比行数（核心表）
ssh root@100.122.83.20 'kubectl -n lurus-tally exec deploy/tally-backend -- psql "$DATABASE_DSN" -c "SELECT '\''restore'\'' AS src, count(*) FROM tally_restore_test.product UNION ALL SELECT '\''live'\'', count(*) FROM tally.product;"'

# 4. 清理临时 schema
ssh root@100.122.83.20 'kubectl -n lurus-tally exec deploy/tally-backend -- psql "$DATABASE_DSN" -c "DROP SCHEMA tally_restore_test CASCADE;"'
```

**Exit 标准**：restore.product 行数与 live.product 误差 ≤ 5%（同步窗口期内的写入会有少许差）。drill log 贴 `_bmad-output/planning-artifacts/stories/S0.Q4-pg-backup-cronjob.md` Dev Agent Record。

### 已知 prereq
- K8s Secret `tally-secrets` 需包含 8 个新 key: `PG_HOST` / `PG_PORT` / `PG_USER` / `PG_PASSWORD` / `PG_DB` / `MINIO_ENDPOINT` / `MINIO_ACCESS_KEY` / `MINIO_SECRET_KEY`。如未注入，CronJob 会 `CreateContainerConfigError`。
- MinIO bucket `tally-backup` 需提前在 R6 minio CLI/UI 创建，cron 不会自建。
