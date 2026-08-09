> 从 SKILL.md 拆出的深度材料(2026-07-01 渐进披露批次2)。§5 Frontend / Next.js 14 陷阱(路由组 / id_token / ES3 / 卡片网格 / industry gate)。

## 5. Frontend / Next.js 14 陷阱

### 5.1 (dashboard) 是路由组，URL 里不出现

`web/app/(dashboard)/products/page.tsx` → URL `/products`（不是 `/dashboard/products`）。

**Middleware matcher 大坑**：
```ts
// WRONG: 永远不匹配 /products /dictionary /projects
matcher: ["/(dashboard|setup|pos)(.*)"]

// RIGHT: 反向白名单（保护除 /login + /api + 内部资源外所有）
matcher: ["/((?!login|api|_next|favicon.ico).*)"]
```

S28.1 部署后用户进 `/dictionary` 没被拦截 → client fetch `/api/proxy/*` → proxy 拿不到 session → 401（"Error: unauthorized"）。修了 middleware 后 307 → /login → Zitadel → 回跳 → fetch 200。

### 5.2 NextAuth + Zitadel — 用 id_token 不是 access_token

Zitadel 默认 access_token 是 opaque（不是 JWT），backend 没法验证。在 `auth.ts` jwt callback 里用 id_token：

```ts
async jwt({ token, account, profile }) {
  if (account?.id_token) {
    token.accessToken = account.id_token  // 用 id_token 而非 access_token
  }
  // ...
}
```

`/api/proxy/[...path]/route.ts` 拿 `session.accessToken` (=id_token) 转 backend Bearer。

### 5.3 tsc strict 默认 ES3 → 不能 spread iterator

`tsconfig.json` 没设 `target` → TypeScript 默认 ES3 → 不能 `[...iter]` 扩展 RegExp matchAll 等迭代器。在 `.spec.ts` / `.test.ts` 里用 `Array.from(...)`：

```ts
// WRONG
for (const m of [...body.matchAll(/.../g)]) { ... }

// RIGHT
for (const m of Array.from(body.matchAll(/.../g))) { ... }
```

S28.1 CI 失败过一次。**新加 .spec.ts 时直接用 Array.from**。

### 5.4 卡片网格 + Sheet drawer 模式

S28.2 项目列表页落地：3-col 响应式（lg=3 / md=2 / sm=1）shadcn `<Card>`，点卡开 `<Sheet>` drawer 显详情 + 编辑。模板：
- 顶 bar：搜索 input (debounced 300ms) + 状态 `<Select>` + "新建" `<Button>`
- 中：卡片网格（每卡 = 名称 heading + 编号 muted + 客户 badge + 金额大字 + 状态色 badge + 时间）
- 空态："暂无 X，点击'新建 X'添加第一个"
- 抽屉：`<Sheet>` 内 `<Tabs>` 详情/编辑/删除

文件参考：`web/app/(dashboard)/projects/page.tsx`

### 5.5 Sidebar industry profile gate

非通用菜单（如苗木字典）按 `tenant_profile.industry` gate：
```typescript
// web/app/(dashboard)/sidebar.tsx
type NavItem = {
  href: string; label: string; icon: string
  industry?: string[]  // 缺失=显示给所有 profile
}
const navItems: NavItem[] = [
  { href: "/products", label: "商品管理", icon: "📦" },
  { href: "/projects", label: "项目", icon: "🏗️" },  // core 通用
  { href: "/dictionary", label: "苗木字典", icon: "🌿", industry: ["horticulture"] },
]
```
`useSession()` / `auth()` 拿 profileType 过滤。

profile 值：`cross_border` / `retail` / `hybrid` / `horticulture`（4 个，加新行业在 `internal/domain/tenant/tenant_profile.go` ProfileType 常量 + `IsUserSelectableProfile()` map）。

