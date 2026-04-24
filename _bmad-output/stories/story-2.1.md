# Story 2.1: Zitadel OIDC 登录 + 首次登录 Profile 选择 + 租户初始化

**Epic**: 2 — 多租户与认证基础
**Story ID**: 2.1
**Profile**: both
**Type**: feat
**Estimate**: 7h
**Status**: Draft

---

## Context

Epic 1 交付了可构建、可部署的骨架（Go 后端 :18200 + Next.js 前端 :3000，12 张迁移表，CI/CD
完整）。当前登录页是占位组件，没有真实认证逻辑。Epic 2 的首要任务是打通认证链路：让新用户
能通过 Zitadel OIDC 登录，并在首次登录后选择行业 Profile（cross_border / retail），创建
tenant_profile 记录——这是后续所有 Profile 感知 UI 和业务规则的数据基础。

---

## Acceptance Criteria

1. 未认证用户访问任何受保护路由（`/dashboard/*`）时，Next.js middleware 重定向到 `/login`。
2. 用户在 Zitadel 完成 PKCE OIDC 认证后，NextAuth callback 成功，session 中含 `tenantId`、`userId`（Zitadel sub）、`profileType`（从 tenant_profile 表读取，初始值为 `null` 若未设置）。
3. 若 tenant_profile 记录存在（回头用户）：跳转 `/dashboard`，侧边栏按 profile_type 渲染差异化菜单。
4. 若 tenant_profile 记录不存在（新用户）：跳转 `/setup`，展示 Profile 选择页面；页面有两张卡片，各含 ≥3 条适用场景描述。
5. 用户在 `/setup` 选择 Profile 后，`POST /api/v1/tenant/profile` 创建 tenant_profile 记录（profile_type 为 `cross_border` 或 `retail`），然后跳转 `/setup/complete`，再跳 `/dashboard`。
6. `GET /api/v1/me` 端点在认证状态下返回 `{ userId, tenantId, profileType }`；未认证返回 401。
7. 后端 Go middleware（`AuthMiddleware`）用 Zitadel JWKS endpoint 自动拉取公钥并验签 JWT；签名无效或过期返回 401。
8. 退出登录（`/api/auth/signout`）清除 NextAuth session cookie；重定向 `/login`。
9. RLS 验证：`TestRLS_TenantProfile_CrossTenantInvisible` 集成测试通过——用户 A 的 token 查询 tenant_profile 表，看不到用户 B 的记录。
10. Dashboard layout 根据 session 中的 profile_type 动态渲染侧边栏：cross_border 显示"汇率"导航项，retail 显示"POS 收银"导航项，共享项（仪表盘、商品、库存）始终显示。

---

## Tasks / Subtasks

### Task 1: 数据库迁移 — 创建 tenant_profile 表

- [ ] 写失败测试: `TestMigration_013_TenantProfileTableExists` — 在 migration 012 rollback 后，查 `information_schema.tables WHERE table_name='tenant_profile'` 应返回 0 行
- [ ] 创建 `migrations/000013_tenant_profile.up.sql`：
  - `CREATE TABLE tally.tenant_profile (id UUID PK, tenant_id UUID NOT NULL UNIQUE REFERENCES tally.tenant(id) ON DELETE CASCADE, profile_type VARCHAR(20) NOT NULL CHECK IN ('cross_border','retail','hybrid'), inventory_method VARCHAR(20) NOT NULL DEFAULT 'wac' CHECK IN ('fifo','wac','by_weight','batch','bulk_merged'), custom_overrides JSONB NOT NULL DEFAULT '{}', created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now())`
  - `CREATE INDEX idx_tenant_profile_tenant ON tally.tenant_profile(tenant_id)`
  - `ALTER TABLE tally.tenant_profile ENABLE ROW LEVEL SECURITY`
  - `CREATE POLICY tenant_profile_rls ON tally.tenant_profile USING (tenant_id = current_setting('app.tenant_id')::UUID)`
- [ ] 创建 `migrations/000013_tenant_profile.down.sql`：`DROP TABLE IF EXISTS tally.tenant_profile`
- [ ] 验证: `migrate up` 后 `\d tally.tenant_profile` 显示表结构；`migrate down` 后表消失

### Task 2: Go 后端 — JWT 验证 middleware

- [ ] 写失败测试: `TestAuthMiddleware_InvalidJWT_Returns401` — 向受保护路由发送格式正确但签名无效的 Bearer token，期望返回 401
- [ ] 写失败测试: `TestAuthMiddleware_NoToken_Returns401` — 不携带 Authorization header，期望返回 401
- [ ] 创建 `internal/adapter/middleware/auth.go`：
  - 使用 `github.com/lestrrat-go/jwx/v2` 或 `github.com/zitadel/oidc/v3` 拉取 `https://auth.lurus.cn/.well-known/openid-configuration` JWKS endpoint（自动缓存公钥，TTL 1h）
  - 从 `Authorization: Bearer <token>` 解析并验签 JWT
  - 提取 claims：`sub`（Zitadel user ID）、`urn:zitadel:iam:user:metadata:tenant_id`（或 `tally_tenant_id` custom claim）
  - 验证失败 → `c.AbortWithStatusJSON(401, ...)`
  - 验证成功 → 将 `userID` 和 `tenantID` 写入 Gin context
- [ ] 写失败测试: `TestAuthMiddleware_ValidJWT_InjectsUserID` — 用 test RSA key 签发 token，middleware 解析后 context 中 userID 非空
- [ ] 验证: 以上三个测试全部 pass

### Task 3: Go 后端 — tenant repo + CreateTenant + ChooseProfile use cases

- [ ] 写失败测试: `TestTenantRepo_GetByUserID_NotFound_ReturnsNil` — 查询不存在的 userID 返回 `nil, nil`（非 error）
- [ ] 写失败测试: `TestTenantUseCase_ChooseProfile_InvalidType_ReturnsError` — profile_type = "invalid" 时返回 validation error
- [ ] 写失败测试: `TestTenantUseCase_ChooseProfile_HybridNotAllowed_ReturnsError` — profile_type = "hybrid" 时返回 error（UI 不暴露 hybrid，此验证阻止直接 API 调用）
- [ ] 创建 `internal/adapter/repo/tenant_profile_repo.go`：
  - `GetByTenantID(ctx, tenantID) (*domain.TenantProfile, error)`
  - `Create(ctx, profile *domain.TenantProfile) error`
  - `Update(ctx, profile *domain.TenantProfile) error`
- [ ] 创建 `internal/app/tenant/choose_profile.go`：
  - `ChooseProfileInput { TenantID UUID, ProfileType string }`
  - 验证 profile_type ∈ {"cross_border", "retail"}（显式拒绝 "hybrid"，避免前端绕过）
  - 调用 repo.Create；若已存在（UNIQUE 冲突）返回特定错误（不允许二次修改）
- [ ] 创建 `internal/app/tenant/get_me.go`：
  - `GetMeOutput { UserID, TenantID, ProfileType string }`
  - 从 ctx 读取 tenantID → repo.GetByTenantID → 组装输出
- [ ] 验证: `go test -v -race ./internal/app/tenant/... ./internal/adapter/repo/...` 全 pass

### Task 4: Go 后端 — auth handler (登录检查 + profile 端点)

- [ ] 写失败测试: `TestAuthHandler_GetMe_Unauthenticated_Returns401` — 不携带 token，期望 401
- [ ] 写失败测试: `TestAuthHandler_ChooseProfile_ValidInput_Returns201` — 携带有效 token，body `{"profile_type":"cross_border"}`，期望 201 + 记录入库
- [ ] 创建 `internal/adapter/handler/auth/` 目录：
  - `handler.go`：注册路由 `GET /api/v1/me`、`POST /api/v1/tenant/profile`
  - `get_me_handler.go`：调用 `GetMeUseCase`
  - `choose_profile_handler.go`：解析 body → `ChooseProfileUseCase` → 201 Created
- [ ] 在主路由中挂载：`AuthMiddleware` → `TenantRLSMiddleware`（占位，Story 2.3 实现）→ auth handlers
- [ ] 验证: `go test -v ./internal/adapter/handler/auth/...` 全 pass

### Task 5: 集成测试 — RLS 跨租户隔离

- [ ] 写失败测试: `TestRLS_TenantProfile_CrossTenantInvisible`（文件 `tests/integration/rls_tenant_profile_test.go`）：
  - 创建两个 tenant（A、B）及对应 tenant_profile 记录
  - 用 tenant A 的 tenant_id 设置 `SET LOCAL app.tenant_id`，执行 `SELECT * FROM tally.tenant_profile`
  - 断言：返回行数 = 1，且 tenant_id = A（看不到 B 的行）
- [ ] 验证: `go test -v -tags integration ./tests/integration/...` pass

### Task 6: NextAuth — Zitadel OIDC provider 配置

- [ ] 写失败测试（Vitest）: `test_auth_session_contains_profile_type` — mock NextAuth session，断言 session.user.profileType 字段存在
- [ ] 创建 `web/app/api/auth/[...nextauth]/route.ts`：
  - NextAuth v5 配置，Zitadel provider（PKCE flow，issuer `https://auth.lurus.cn`）
  - `clientId` 从 `process.env.ZITADEL_CLIENT_ID` 读取（PKCE 无 secret）
  - `callbacks.jwt`：从 Zitadel token 提取 `sub`（写入 token.userId）和 `tally_tenant_id` custom claim
  - `callbacks.session`：从 jwt token 映射到 session，补充 `profileType`（调 `/api/v1/me` 拉取）
  - `pages.signIn: "/login"`
- [ ] 创建 `web/lib/auth.ts`：导出 `auth`（server-side session helper）、`signIn`、`signOut`
- [ ] 验证: `cd web && bun run test`，相关测试 pass；`bun run typecheck` 无 error

### Task 7: Next.js middleware — 路由保护

- [ ] 写失败测试（Vitest）: `test_middleware_redirects_unauthenticated_to_login` — mock `auth()` 返回 null，访问 `/dashboard`，断言响应 redirect URL 含 `/login`
- [ ] 创建或修改 `web/middleware.ts`（Next.js App Router 约定位置）：
  - 使用 NextAuth `auth` wrapper
  - 匹配路径: `/(dashboard|setup)(.*)`
  - 无 session → redirect `/login`
  - 有 session 但无 profileType → redirect `/setup`（已在 `/setup` 则放行）
- [ ] 验证: middleware 单元测试 pass；`bun run build` 无 build error

### Task 8: Next.js — 登录页与 Zitadel 跳转

- [ ] 写失败测试（Vitest/RTL）: `test_login_page_renders_sign_in_button` — 渲染 `<LoginPage />`，断言"使用邮箱账号登录"按钮存在
- [ ] 修改 `web/app/(auth)/login/page.tsx`（当前为占位）：
  - Server Component，用 `auth()` 检查 session；若已登录 redirect `/dashboard`
  - 渲染品牌 Logo + "使用邮箱账号登录" 按钮（触发 `signIn("zitadel")`）
  - 暗黑模式默认，shadcn/ui Card 布局
- [ ] 验证: 组件测试 pass；页面在 `bun run dev` 下可访问

### Task 9: Next.js — Onboarding Profile 选择页

- [ ] 写失败测试（Vitest/RTL）: `test_setup_page_renders_two_profile_cards` — 渲染 `<SetupPage />`，断言存在两个卡片：一个含文本 "跨境贸易"，另一个含文本 "线下零售"
- [ ] 写失败测试: `test_setup_page_each_card_has_min_three_scenarios` — 断言每个卡片内子元素（场景描述）≥ 3 个
- [ ] 创建 `web/app/(onboarding)/setup/page.tsx`：
  - Server Component wrapper（检查 session；若 profileType 已存在则 redirect `/dashboard`）
  - Client 内层组件 `<ProfilePicker />`：
    - 卡片 A（cross_border）：图标 + 标题"跨境贸易 / 外贸批发" + 适用场景 ≥3 条（如：标准 SKU 批发出口、多币种收付款、海运清关管理）
    - 卡片 B（retail）：图标 + 标题"线下零售 / 实体门店" + 适用场景 ≥3 条（如：散装/称重商品、柜台即时收银、断网离线开单）
    - 选中卡片高亮（shadcn/ui ring 样式）；"继续"按钮激活后调 `POST /api/v1/tenant/profile`
    - 成功后 router.push(`/setup/complete`)
- [ ] 创建 `web/app/(onboarding)/setup/complete/page.tsx`：
  - 显示"配置完成！正在跳转..."；3 秒后 router.push(`/dashboard`)（或直接 redirect）
- [ ] 验证: 两个组件测试 pass；`bun run build` 无 error

### Task 10: Next.js — ProfileContext + Dashboard layout Profile 感知侧边栏

- [ ] 写失败测试（Vitest/RTL）: `test_profile_context_provides_profile_type` — Provider 包裹，`useProfile()` 返回传入的 profileType
- [ ] 写失败测试: `test_dashboard_layout_cross_border_shows_exchange_rate_nav` — session profileType="cross_border"，渲染 Dashboard layout，断言侧边栏含"汇率"导航项
- [ ] 写失败测试: `test_dashboard_layout_retail_shows_pos_nav` — session profileType="retail"，断言侧边栏含"POS 收银"导航项
- [ ] 创建 `web/lib/profile.tsx`：
  - `ProfileContext`（React Context）、`ProfileProvider` Client Component
  - `useProfile()` hook：`{ profileType: 'cross_border' | 'retail' | 'hybrid' | null }`
  - `ProfileGate` 组件（props: `profiles: ProfileType[]`, `children`）：当 profileType ∈ profiles 时渲染 children
- [ ] 创建 `web/app/(dashboard)/layout.tsx`：
  - Server Component：用 `auth()` 拿 session，提取 profileType
  - 包裹 `ProfileProvider value={profileType}`
  - 侧边栏共享项：仪表盘、商品、库存、采购、销售（始终显示）
  - `<ProfileGate profiles={['cross_border']}>` 包裹：汇率管理
  - `<ProfileGate profiles={['retail']}>` 包裹：POS 收银
- [ ] 创建 `web/app/(dashboard)/page.tsx`：占位 dashboard 首页（"欢迎，{tenantName}"，待 Epic 3 填充内容）
- [ ] 验证: 三个测试全 pass；`bun run build` 无 error

---

## File List (anticipated)

### 新增（服务 repo `2b-svc-psi/`）

| 文件 | 操作 |
|------|------|
| `migrations/000013_tenant_profile.up.sql` | create |
| `migrations/000013_tenant_profile.down.sql` | create |
| `internal/domain/entity/tenant_profile.go` | create — TenantProfile struct + ProfileType constants |
| `internal/adapter/middleware/auth.go` | create — JWT 验证 middleware |
| `internal/adapter/repo/tenant_profile_repo.go` | create — GORM CRUD |
| `internal/app/tenant/choose_profile.go` | create — ChooseProfile use case |
| `internal/app/tenant/get_me.go` | create — GetMe use case |
| `internal/adapter/handler/auth/handler.go` | create — 路由注册 |
| `internal/adapter/handler/auth/get_me_handler.go` | create |
| `internal/adapter/handler/auth/choose_profile_handler.go` | create |
| `internal/adapter/handler/auth/get_me_handler_test.go` | create |
| `internal/adapter/handler/auth/choose_profile_handler_test.go` | create |
| `internal/adapter/middleware/auth_test.go` | create |
| `internal/adapter/repo/tenant_profile_repo_test.go` | create |
| `internal/app/tenant/choose_profile_test.go` | create |
| `tests/integration/rls_tenant_profile_test.go` | create |
| `web/app/api/auth/[...nextauth]/route.ts` | create |
| `web/lib/auth.ts` | create |
| `web/lib/profile.tsx` | create |
| `web/middleware.ts` | create |
| `web/app/(onboarding)/setup/page.tsx` | create |
| `web/app/(onboarding)/setup/complete/page.tsx` | create |
| `web/app/(dashboard)/layout.tsx` | create |
| `web/app/(dashboard)/page.tsx` | create |

### 修改

| 文件 | 操作 |
|------|------|
| `web/app/(auth)/login/page.tsx` | modify — 从占位替换为真实 Zitadel 登录 UI |
| `web/package.json` | modify — 添加 `next-auth@5`、`@auth/core` 依赖 |
| `internal/adapter/handler/` (路由注册入口) | modify — 挂载 auth handler |
| `cmd/server/main.go` 或 lifecycle 初始化 | modify — 注入 AuthMiddleware 依赖 |

---

## Dev Notes

### Zitadel PKCE Client 注册（外部依赖，开发前需用户操作）

- 在 Zitadel Console（auth.lurus.cn）中为 Tally 注册一个 **Native / SPA** OIDC 应用：
  - Grant type: `Authorization Code + PKCE`（无 client secret）
  - Redirect URI: `https://tally-stage.lurus.cn/api/auth/callback/zitadel`（stage）
  - Post-logout URI: `https://tally-stage.lurus.cn/login`
  - `clientId` 配置到 `ZITADEL_CLIENT_ID` 环境变量
- `ZITADEL_ISSUER=https://auth.lurus.cn` 是 NextAuth Zitadel provider 的 issuer 值

### Zitadel custom claim：tally_tenant_id

- 推荐做法：在 Zitadel 的 Action（Webhook）中于 JWT 中注入 `tally_tenant_id` claim（调 Tally 内部 API 查租户映射）。若 V1 先不做 Action，可在 NextAuth `callbacks.jwt` 中用 `sub`（Zitadel user sub）调 `/api/v1/me` 拉取 tenantID，写入 JWT token 缓存。
- 本 story 采用后者（不依赖 Zitadel Action 配置），`sub` 作为 userID，首次登录通过 `user_identity_mapping` 表查 tenantID（沿用 platform 已有模式）。

### JWT 验证库选型

- 推荐 `github.com/lestrrat-go/jwx/v2`：轻量、无 CGO、支持自动 JWKS 刷新、在 Lurus platform 代码库中已有先例。
- 备选 `github.com/zitadel/oidc/v3/pkg/oidc`：官方 SDK，功能更重但更贴合 Zitadel。
- 两者均可；dev agent 按实际 `go.mod` 现有依赖优先选择已存在的库，避免引入新依赖。

### GORM + RLS 注意事项

- `tenant_profile_repo` 所有操作必须通过带 `SET LOCAL app.tenant_id = ?` 的事务执行，否则 RLS policy 的 `current_setting('app.tenant_id')` 为空 → 返回 0 行而非 error（静默失败）。
- `CreateTenantProfile` 调用前需确保事务已注入正确的 `app.tenant_id`；集成测试中手动执行 `SET LOCAL`。

### migration 000013 与现有表的关系

- `tally.tenant` 表在 migration 000002 中创建。`tenant_profile.tenant_id` 有 FK 到 `tenant.id`，确保顺序正确（000013 > 000002）。
- `UNIQUE(tenant_id)` 约束保证一个 tenant 只有一条 profile 记录，`ChooseProfile` 用 `INSERT ... ON CONFLICT DO NOTHING` 或直接 `INSERT`（遇冲突上层返回"已设置"错误）。

### Profile 选择锁定

- profile_type 一旦由用户选择，本 story 不提供修改入口（Story 2.6 的企业设置向导处理更新逻辑）。
- `ChooseProfileUseCase` 若记录已存在应返回错误 `ErrProfileAlreadySet`，handler 返回 409 Conflict。

### hybrid profile 处理

- schema 的 CHECK constraint 允许 `hybrid` 值（为未来管理员后台保留）。
- `ChooseProfileUseCase` 显式拒绝 profile_type = "hybrid"（返回 validation error）。
- 前端 `/setup` 页面只渲染两张卡片（cross_border / retail），不暴露 hybrid 选项。

### inventory_method 默认值

- `tenant_profile` 创建时：cross_border → `inventory_method = 'fifo'`；retail → `inventory_method = 'wac'`。此默认值在 `ChooseProfileUseCase` 中按 profile_type 设置，不依赖 DB DEFAULT。

### 测试中的 Zitadel mock

- 后端单元测试：`AuthMiddleware` 测试用本地生成的 RSA key pair 签发 JWT，不依赖真实 Zitadel；JWKS endpoint 使用 `httptest.Server` mock。
- 前端组件测试：NextAuth session 通过 `vi.mock('next-auth/react', ...)` mock，不需要真实 Zitadel。

### 契约引用

- 见 `doc/coord/contracts.md`：Tally 消费 Zitadel OIDC（auth.lurus.cn）和 2l-svc-platform identity API。本 story 仅使用 Zitadel；platform 同步在 Story 2.2 处理。

---

## Flagged Assumptions

| # | 假设 | 影响 | 开发前需确认 |
|---|------|------|-------------|
| A1 | Zitadel 中 Tally PKCE client 尚未注册 | dev 阶段无法做真实 E2E 测试，需在 mock 下完成单元测试 | 用户在 auth.lurus.cn 注册 PKCE client，提供 clientId |
| A2 | `user_identity_mapping` 表在 tally schema 中已有（沿用 platform 模式） | 若无此表则需额外 migration | 检查 migration 000001-000012 是否包含 user_identity_mapping；若无，Task 1 中补加 |
| A3 | NextAuth v5（beta）API 与 Zitadel provider 兼容 | 若 v5 不稳，降回 NextAuth v4 + Zitadel provider | dev agent 检查 `web/package.json` 当前 next-auth 版本后决定 |
| A4 | `web/middleware.ts` 使用 NextAuth v5 `auth` wrapper 模式（非 v4 `withAuth`） | API 不同需调整 | 与 A3 联动 |
| A5 | Dashboard layout 侧边栏在本 story 只做骨架（共享项 + Profile 感知项），不实现实际导航功能 | Epic 3 补全 UI 细节 | 已明确为 out of scope；dev agent 按骨架实现即可 |

---

## Out of Scope

- Platform 租户同步回调 `/internal/v1/tally/tenant/sync`（Story 2.2）
- `SET LOCAL app.tenant_id` 全局 RLS middleware（Story 2.3）
- 跨租户数据隔离 E2E 全量测试（Story 2.4）
- RBAC 四角色（Story 2.5）
- 企业设置向导三步引导 / 修改 Profile（Story 2.6）
- ProfileResolver Go 侧 + ProfileMiddleware（Epic 3 / Story 3.7 或 Story 2.3 扩展）
- hybrid profile UI 入口
- 修改已设置的 profile_type（设置后锁定）

---

## Dev Agent Record

(populated by bmad-dev during implementation)
