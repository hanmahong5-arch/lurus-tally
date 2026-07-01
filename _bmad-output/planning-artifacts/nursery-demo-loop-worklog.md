# 苗木老板"演示即用"沙盒 — Loop Worklog

> 北极星: 让真实苗木老板在不依赖 owner-gated OIDC 的沙盒里, 自己录 1 张真实进货单并看到 AI 补货建议; 交付苗木字典 E28.1 让 5 家访谈有产品可演示。
> 退出: ① 公开 demo URL(无 OIDC, onboarding→录进货单→看库存/AI补货, demo 数据写入隔离); ② 苗木字典 E28.1 UI 上线; ③ 沙盒真跑通采购周期(下单→入库→库存)并贴日志。
> 工作树: `2b-svc-psi-nursery` @ `feat/nursery-demo-2026-06-24`(off HEAD `3446d232`)。**不碰共享 swarm 树 `feat/tally-platform-integration-2026-06-19`(dirty + 活跃 swarm)。**

## 退出条件状态(每轮更新)

| # | 条件 | 状态 | 依据 |
|---|------|------|------|
| ② | 苗木字典 E28.1 UI(搜品种 + 录苗木) | ✅ 已 shipped(源码+测试在仓,本轮未跑 build 复验) | `web/app/(dashboard)/dictionary/page.tsx` 注释 "(Story 28.1)":搜索/8类型筛选/增删改恢复/详情抽屉;`components/horticulture/NurseryDictForm.tsx`;`lib/api/nursery-dict.ts`;e2e `tests/e2e/nursery-dict.spec.ts`;domain `internal/domain/horticulture/dict.go`(`ListFilter.Query` ILIKE 搜品种)+ handler 已无条件接线 lifecycle/app.go:597-606 |
| ① | 公开 demo URL(无 OIDC 进入) | ❌ 未实现 | `web/middleware.ts` 仅豁免 `/login /pricing /api /_next`,匿名一律 →`/login`;无 demo/dev 公开旁路。字典/onboarding/采购全在 OIDC 墙后 |
| ③ | 沙盒真跑通采购周期 | ⏳ 阻塞于 ① | 采购→入库→库存 闭环本身已 shipped(WTP swarm map 确认),但苗木老板进不去 |

## Round 1 — 2026-06-24(核验 + 隔离 + 定位 + 设计)

**做了**:
1. 否定式核验(§4):退出条件②"苗木字典 E28.1"**已 shipped**,非缺口 → 不重造,报状态变更。
2. 工作树隔离:发现当前在共享 swarm 树(dirty + 一堆 locked `agent-*` worktree=活跃 swarm)→ 新建隔离 worktree。
3. 定位 #1 真缺口 = 退出条件①(无 OIDC 进入)。要演示的功能全已建好,卡在"进不去"。
4. auth 复用资产盘点(`web/auth.ts`):已有 offline dev Credentials provider(PAT 作 accessToken 透传),但双门控死在生产(`AUTH_DEV_PROVIDER` AND `NODE_ENV!=="production"`)、硬编码 cross_border profile → 不能直接当公开沙盒,但其 PAT 透传机制可复用。

**#1 缺口最小设计(最大复用已 shipped 件)**:
- **后端** `POST /api/v1/demo/start`(新,公开;`TALLY_DEMO_MODE` 显式 gated;限流 + 临时租户 reaper):建临时 demo 租户(profile=horticulture)→ 复用现有 `seed-demo` 灌真实苗木种子(`is_sample=true`,合法 demo 内容,非冒充真实用户行为)→ 签发短时 **demo PAT**(复用现有 PAT 表/中间件,`tally_pat_` 前缀)→ 返回 `{tenant_id, pat}`。
- **写入隔离** = 现有 RLS 按 demo 租户(已 shipped,零新代码)。
- **前端** 公开 `/demo` 落地页:调 demo/start → demo 凭证携 PAT 进 `/dashboard`(复用 dev-provider accessToken 透传)→ middleware 豁免 `/demo`。
- **可能无需 migration**(demo 租户=普通 tenant 行,PAT/seed 表已存在)。建后端前再确认;若需建表先到 `doc/coord/migration-ledger.md` 取号。

**owner-gated(到点用 AskUserQuestion 上报,不绕)**:
- 退出条件①的"**公开可访问 URL**" = 部署到 R6 stage(`tally-stage.lurus.cn`,现 307→login)+ 镜像 build/apply(ADR-0006 手动 kubectl)。代码可本地建+验;公开 URL 须 owner。
- 真注册/真支付明确不在本 loop。

**旁注(不改 loop 方向)**:WTP swarm(6 persona,**无苗木**)整体收费意愿 8/100 = NO-GO,#1 横向变现拦截是 POS `pos/page.tsx:111` 单价硬编码 ¥0。与本 loop 不冲突——本 loop 正是用真实访谈验证苗木垂直。POS bug 记此备查,非本 loop 范围。

**下一轮(Round 2)**:开建后端 `POST /api/v1/demo/start`(读现有 onboarding/seed-demo + PAT 签发 + tenant bootstrap 代码,确认是否需 migration);本地起后端跑通 demo/start→拿 PAT→curl 采购周期。

## Round 2 — 2026-06-24(编码前思考 + demo 编排层落地)

**编码前思考(读全 4 个复用件,确认无需 migration)**:
- `ChooseProfileUseCase.Execute(ctx,{ZitadelSub,ProfileType})` 单事务建 tenant+mapping+profile,且 `Bootstrap.seedDemoEntities` **已自动建 horticulture preset 默认仓 `苗圃仓` + 供应商 `苗木供应商`**(无需另建仓)。可传合成 sub。
- `domainauth.GenerateToken() → (plaintext, prefix, hash)` + `domainauth.PAT{...,ExpiresAt}` + `patRepo.Create(ctx,*PAT)`,支持短时过期。
- **RLS 关键原语**:`dbscope.WithPinnedConn(ctx, db, tenantID, fn)` —— 注释明写"非 HTTP 版 TenantDB,给 auth 中间件外解析租户、仍需 RLS 兜底的入口(如公开 shopify webhook)"。demo/start 与之同形:在新 demo 租户的 RLS scope 内 mint PAT + seed。
- `SeedDemoUseCase.Execute({TenantID,WarehouseID,PersonaHorticulture})` 灌苗木品 + 期初库存 + 30 天回填销量(`is_sample=true`,合法 demo 内容)。
- **结论:无需 migration**(tenant/mapping/profile/warehouse/partner/pat/product/stock_movement 表全在)→ 不动 ledger。

**做了(本轮"闭")**:落 `internal/app/demo/provision.go` —— `DemoProvisionUseCase`,4 端口编排(`TenantBootstrapper`/`ScopedRunner`/`PATMinter`/`Seeder`)+ 注入 clock。两阶段:① bootstrap 建 demo 租户(自有 tx);② 在 RLS scope 内 mint 短时 PAT(默认 24h)+ seed 苗木。合成身份 `demo:<uuid>` 命名空间隔离,绝不撞真实 OIDC sub。
- 单测 `provision_test.go` 4 例(`package demo_test`,fakes + 调用序列 recorder):happy path(token/tenant/expiry + mint/seed **在 scope 内按序**)、bootstrap 失败短路、seed 失败上抛、零 ttl 回落 DefaultPATTTL。
- 验证:`go vet ./internal/app/demo/...` exit 0;`go test ./internal/app/demo/...` ok(4 PASS)。纯端口、零 DB、零安全面、无 migration、无 commit。

**下一轮(Round 3)**:接真适配器实现 4 端口(ChooseProfile-with-nil-upserter 避免给 demo 租户注册 platform 账号 / dbscope.WithPinnedConn / PAT GenerateToken+repo.Create / warehouse 查 + SeedDemo)+ 公开 handler `POST /api/v1/demo/start`(`TALLY_DEMO_MODE` gated + 限流)+ lifecycle 在**公开路由组**(authMW 之外)注册 + config 加 `TALLY_DEMO_MODE`。然后本地起后端 PG 跑通 demo/start→PAT→采购周期(闭退出条件③)。

## Round 3 — 2026-06-24(公开 handler + 安全策略层)

**关键发现(router 结构)**:`api := r.Group("/api/v1")` 已挂 authMW + tenantDB + idempotency + RequireIdempotencyKey,**公开 demo 路由不能进该组**;须挂在 root engine(像 `/internal/v1/tally/health`)。`x/time/rate` 非依赖。

**做了(本轮"闭")**:落 `internal/adapter/handler/demo/handler.go` —— 公开 `POST /api/v1/demo/start`,依赖 app 层 `provisioner` 窄接口(fake 可测)。两道公开端点安全控制:
- **enable 闸**:`enabled=false`(来自将接的 `TALLY_DEMO_MODE`)→ **404 隐藏**(不泄露端点存在),生产即便挂了路由也安全;
- **限流**:自写极简令牌桶(无新依赖,注入时钟),公开建租户端点的 spam/DoS 兜底,超限 **429 + Retry-After**。
- `RegisterRoutes(r gin.IRouter)` 用 IRouter,让 lifecycle 决定挂 root(公开)。
- 单测 `handler_test.go` 4 例:disabled→404 且不 provision、happy→200+entry creds、限流第 3 次→429(冻结时钟,且第 3 次 provision 不被调)、provision 错→500。
- 验证:`go vet ./internal/adapter/handler/demo/...` exit 0;`go test ./internal/adapter/handler/demo/... ./internal/app/demo/...` ok。无 DB、无 migration、无 commit。

**下一轮(Round 4)**:接 4 个真适配器(`internal/adapter/demo/`:ChooseProfile-nil-upserter bootstrapper / dbscope.WithPinnedConn scoped runner / GenerateToken+patRepo.Create minter / 默认仓查 + SeedDemoUseCase seeder)+ config 加 `TALLY_DEMO_MODE` + lifecycle 在 root engine 条件注册 demo handler。然后**本地起 PG 跑通 demo/start→拿 PAT→curl 采购周期(下单→入库→库存)**,贴日志,闭退出条件③。

## Round 4 — 2026-06-24(真适配器 + 接线,全模块 build 绿)

**编码前思考(再核 3 签名)**:
- **PAT `Create` 用裸 `db`、不走 dbscope**(personal_access_token migration 000031 relax 策略,鉴权须在 tenant 上下文前读 PAT)→ mint 无需 pinned conn,简化。
- seed 走 product/stock(RLS FORCE)→ **须 pinned conn**;`appob.SeedInput{TenantID,WarehouseID,Persona}` + `NewSeedDemoUseCase(ProductCreator,StockInitializer,SalesRecorder)`;`PersonaHorticulture` 在仓。
- lifecycle 范式:`shopifyHandler.RegisterRoutes(r)`(line 765)= "挂 root engine 绕过 /api/v1 auth" 的现成公开范式,demo 照抄。

**做了(本轮"闭")**:
- `internal/adapter/demo/adapters.go`:4 真适配器 + 复刻 onboarding 的 stockAdapter(自包含,不侵入 handler 包)+ `Build()` 装配 `*demoapp.Provisioner`。`go build ./internal/adapter/demo/...` exit 0。
- `config.go`:加 `DemoMode bool`(`TALLY_DEMO_MODE`,默认 false,注释明警生产禁开)。
- `lifecycle/app.go`:`if cfg.DemoMode { ... demoadapter.Build(NewChooseProfileUseCase(tenantStore, nil, l)/*nil-upserter 不给 demo 租户注册 platform*/, db, repoauth.New(db), appproduct.NewCreateUseCase(productRepo), recordMovementUC, DefaultPATTTL) → handlerdemo.New(prov,true,10).RegisterRoutes(r) }`,挂 root engine(公开),并 `l.Warn` 提示生产须关。限流 10/min。
- 验证:`gofmt -w`(修 import 顺序)→ **`go build ./...` exit 0(全模块)**;`go test ./internal/app/demo/... ./internal/adapter/handler/demo/... ./internal/pkg/config/...` 全绿。无 migration、无 commit。

**状态**:demo 沙盒后端**全链路接线 + 整模块编译通过**,但**尚未对真 PG 实跑**(只单测 + build)。诚实标注:⏳ 待本地 PG 实跑验证,非 ✅。

**下一轮(Round 5)**:本地起 Docker PG(+Redis,NATS 优雅降级)+ `TALLY_DEMO_MODE=true TALLY_DEV_MODE=true` 起后端 → `curl -X POST /api/v1/demo/start` 拿 {tenant_id,token} → 用 PAT `curl` 采购周期(建商品/采购单→审批入库→查库存快照)→ **贴真实日志**,闭退出条件③。Round 6 起再做前端 `/demo` 入口 + middleware 豁免(退出条件①的浏览器侧),公开 URL 部署 owner-gated 届时 AskUserQuestion。

## Round 5 — 2026-06-24(实跑受阻于本地 PG 环境;按铁律上报不 mock)

**目标**:本地 PG 实跑 demo/start→采购周期,闭 exit③。

**环境核查(资源缺位,真探非臆断)**:
- Docker Desktop:尝试启动 + 轮询满 110s → `STILL_NOT_READY`(WSL2 后端冷启失败/疑卡 EULA/更新)。
- native Postgres:`netstat` :5432 无监听;`/dev/tcp` 直连 127.0.0.1:5432/5433/5434 全 `no_listen`;无 `psql` on PATH。
- WSL Postgres:`wsl --status` / `wsl -l -v` 均超时挂死(WSL 本身无响应)。WSL2 若有 PG 会转发 localhost:5432 → 上条已证无监听。
- **结论:本机当前无任何可用/运行中的 Postgres,Docker 与 WSL 两条拉起路径均不可用。**

**处置(守诚实闸 + 反 shortcut)**:
- 已 AskUserQuestion 上报;owner 指示"找本机/WSL 现成 PG"→ 已找,确无。
- **不 mock 苗木采购周期冒充实跑**(红线)。exit③ 维持 ⏳ —— **环境阻塞,非代码缺陷**(后端 R2-R4 已全链路接线 + 整模块 build 绿 + 单测过)。
- 实跑前置 = 一个可达的 dev/throwaway Postgres(owner 修好 Docker / 起 PG / 给 throwaway DSN 任一)。

**下一轮(Round 6,改取不依赖 PG 的在手缺口)**:做退出条件①的**浏览器侧** —— 前端 `/demo` 落地页(调 `POST /api/v1/demo/start` → 拿 {tenant_id,token} → 经 demo 凭证进 `/dashboard`)+ `web/middleware.ts` 豁免 `/demo`。可 `bun run build` + 页面测验证,无需 PG。exit③ 实跑待 PG 就绪即一条命令补跑。

## Round 6 — 2026-06-24(战略检查点:loop 撞硬环境墙,暂停盲建)

**复核剩余每一步的阻塞**:
- exit③ 实跑 → 需可达 PG:Docker 起不来 + WSL 挂死 + native 无 → 环境阻塞(owner 侧)。
- exit① 公开 URL → STAGE 部署 owner-gated(loop 自身红线)。
- 前端 `/demo` 入口 → 可写,但:① 本仓 web 无 node_modules,build 需多分钟 `bun install`;② 需改 `auth.ts` 加**生产可用的 demo 凭证 provider**(auth-sensitive);③ **本环境无后端可跑 → 无法 e2e 验证**该流程,只能 build-only。

**判断(守诚实闸)**:在一个**无法对任何东西做活验证**的环境里继续生成代码——尤其是 e2e 不可验的 sensitive 生产 auth 改动——等于堆"build 过但未证"的工件,违背"build 过且真跑成功才算 ✅"的精神。**故本轮不盲建前端,改为战略检查点。**

**已交付且已验证(R1-R4)**:demo 沙盒后端全链路 —— app 编排(provision.go,4 测)+ 公开 handler 安全层(404 闸 + 限流,4 测)+ 4 真适配器(复用 ChooseProfile/dbscope/PAT/SeedDemo,无 migration)+ config + lifecycle root 注册。**整模块 `go build ./...` 绿,demo/config/handler 单测全绿。** exit② 苗木字典 = 早已 shipped。

**未完成(全部卡 owner/环境,非代码缺陷)**:
- ⏳ exit③ 实跑 —— 需一个可达 dev/throwaway PG(修 Docker / 起 WSL PG / 给 DSN 任一)。
- ⏳ exit① 公开 URL —— 前端 `/demo` 入口(需 PG 才能 e2e 验)+ STAGE 公开部署(owner-gated)。

**建议(最高杠杆,owner 决策)**:① 给一个可达 PG → 我即刻补跑 exit③ 实跑 + 据此可验地建前端 `/demo`;或 ② 授权 commit 当前分支 → 工作变持久 + CI build 镜像(为后续 STAGE 部署铺路);或 ③ 暂停 cron 自动 fire,待环境/决策就绪再续——避免 loop 空转产出未验工件。

## Round 7 — 2026-06-24(前端 /demo 入口:build+lint+单测三验,纠正上轮过度保守)

**纠偏**:上轮把前端判为"盲建不可验"过度保守。前端**逻辑层可验**(auth gate/jwt 映射可单测,middleware 可正则验,`next build` 是真类型门)——只 session→proxy→后端 的 e2e 需 PG(⏳)。故本轮**真做**退出条件①浏览器侧。

**做了(本轮"闭",均验证)**:
- `web/auth.ts`:加 `demoProviderEnabled()`(仅 `TALLY_DEMO_MODE` 门控,**生产可用**,区别于 dev provider 的 NODE_ENV 硬挡)+ `demoCredentialsProvider`(id `demo`,收 {tenantId,accessToken=demo PAT},缺一返 null;复用 dev 的 jwt 分支,`demoProfile` 映射 profileType=horticulture 使 middleware 不踢去 /setup)。安全模型注释:session 无 PAT 即废,后端逐请求验 PAT。
- `web/app/demo/page.tsx`:公开页 + server action → `POST {BACKEND}/api/v1/demo/start` → `signIn("demo",{tenantId,accessToken,redirectTo:/dashboard})`。
- `web/middleware.ts`:负向 matcher 加 `demo` 豁免(匿名可达)。
- `web/__tests__/auth-session.test.ts`:+4 demo gate 测(prod 可用 / 默认关 / horticulture 映射 / 缺 token 返 null)。
- **验证**:`node` 正则(/demo EXEMPT、/dashboard GATED 正确);`vitest run` **9 passed**;`next lint` **0 错**;`next build` **exit 0**(/demo 页 + auth + middleware 全类型通过)。无 commit。

**退出条件①状态**:**代码全齐**(后端 R2-R4 + 前端 R7,均 build/单测验)。剩 = e2e 活验(需 PG)+ 公开 URL STAGE 部署(owner-gated)。

**仍 ⏳(卡环境/owner,非代码)**:exit③ 实跑(无 PG)、exit① e2e+公开部署。下一步仍需 owner:给 PG(活验全链)/ 授权 commit(持久化+CI)/ 授权 STAGE 部署(对外可访问)。
