import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { render, cleanup, act, fireEvent, screen } from "@testing-library/react"
import { readFileSync } from "node:fs"
import path from "node:path"

import { roleCan, type Perm, type ShopRole } from "./roles"

// Table-driven guard: for every shop role, every page below is rendered with a
// recording fetch, and the page must neither ask for a route that role cannot
// call (it would answer 403) nor show the button that leads to one. The route
// table is the backend's own permission.RoutePerm, written by the Go test.
const routes: Record<string, Perm> = JSON.parse(
  readFileSync(
    path.resolve(__dirname, "../../internal/domain/permission/testdata/routes.json"),
    "utf-8",
  ),
)

const ROLES: ShopRole[] = ["owner", "manager", "clerk", "cashier", "finance"]

const { state } = vi.hoisted(() => ({ state: { role: "owner" as string, query: "", loading: false } }))

vi.mock("next-auth/react", () => ({
  useSession: () => ({
    data: state.loading ? null : { user: { role: state.role, isOwner: state.role === "owner" }, accessToken: "tok" },
    status: state.loading ? "loading" : "authenticated",
  }),
  signIn: vi.fn(),
  signOut: vi.fn(),
}))
vi.mock("@/auth", () => ({
  auth: async () => ({
    user: { role: state.role, isOwner: state.role === "owner" },
    accessToken: "tok",
  }),
}))
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn(), refresh: vi.fn(), back: vi.fn(), prefetch: vi.fn() }),
  useSearchParams: () => new URLSearchParams(state.query),
  useParams: () => ({ id: "b-1" }),
  usePathname: () => "/",
  redirect: vi.fn(),
}))
vi.mock("next/link", () => ({
  default: ({ href, children, ...rest }: { href: string; children: React.ReactNode }) => (
    <a href={href} {...rest}>{children}</a>
  ),
}))
vi.mock("next/image", () => ({ default: () => null }))
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }))
vi.mock("@/components/account/account-drawer-provider", () => ({
  useAccountDrawer: () => ({ openDrawer: vi.fn() }),
}))

/** "/api/proxy/receivables?x=1" or "http://be/api/v1/receivables" -> "/api/v1/receivables". */
function apiPath(url: string): string {
  const noOrigin = url.replace(/^https?:\/\/[^/]+/, "")
  const noQuery = noOrigin.split("?")[0]
  return noQuery.startsWith("/api/proxy/") ? "/api/v1" + noQuery.slice("/api/proxy".length) : noQuery
}

/** The permission a request needs, or null when the route is open to every member. */
function needed(method: string, url: string): Perm | null {
  const p = apiPath(url)
  for (const [key, perm] of Object.entries(routes)) {
    const [m, pattern] = key.split(" ")
    if (m !== method) continue
    const re = new RegExp("^" + pattern.replace(/:[^/]+/g, "[^/]+").replace(/\./g, "\\.")+ "$")
    if (re.test(p)) return perm
  }
  return null
}

const zero = { unpaid: "0", b0_30: "0", b31_60: "0", b61_90: "0", b90p: "0", oldest_bill_date: "" }
const bill = {
  id: "b-1", bill_no: "SL-1", partner_name: "老王", total_amount: "100.00", bill_date: "2026-10-10",
}
const todoSummary = {
  draft_purchases: { count: 1, items: [{ ...bill, bill_no: "PO-1", total_amount: "98765.43" }] },
  draft_sales: { count: 1, items: [bill] },
  low_stock: {
    count: 1,
    items: [{ product_id: "p-1", product_name: "螺丝", days_of_supply: "3", available_qty: "5", reorder_point: "20" }],
  },
  overdue_customers: {
    count: 1,
    items: [{ partner_id: "x-1", partner_name: "老王", overdue_amount: "7654.32", oldest_bill_date: "2026-08-01" }],
  },
  uninvoiced_sales: { count: 1, items: [bill] },
  kpis: { today_sales: "0", today_collected: "0", receivable_unpaid: "0", month_uninvoiced: "0" },
  total: 2,
  has_data: true,
  failed: [],
  generated_at: "",
}

const tier = { sku_count: 0, revenue_share: "0%" }
const monthly = {
  month: "2026-10", date_from: "2026-10-01", date_to: "2026-10-31", net_sales: "0.00", cost_of_goods: "0.00",
  gross_profit: "0.00", net_purchases: "0.00", receipts: "0.00", payments: "0.00", receivable: "0.00",
  payable: "0.00", inventory_value: "0.00",
}

function payload(url: string): unknown {
  const p = apiPath(url)
  if (p.endsWith("/todo/summary")) return todoSummary
  if (p.endsWith("/weekly-summary")) {
    return { replenish: { count: 0, amount_cny: "0" }, oversell: { count: 0 }, dead_stock: { count: 0 }, generated_at: "" }
  }
  if (p.endsWith("/reports/monthly")) return monthly
  if (p.endsWith("/reports/gross-margin")) return { overall_margin: "0%", top10: [], bottom10: [], days: 30 }
  if (p.endsWith("/reports/abc")) return { a: tier, b: tier, c: tier, total_skus: 0, period: "365d" }
  if (p.endsWith("/reports/dead-stock")) return { items: [], count: 0, threshold_days: 90 }
  if (p.endsWith("/reports/sales-top")) return { top_products: [], metric: "revenue", days: 7 }
  if (p.endsWith("/receivables")) {
    return {
      direction: "ar",
      as_of: "2026-10-10",
      items: [{ ...zero, partner_id: "x-1", partner_name: "测试", unpaid: "100" }],
      total: { ...zero, partner_id: null, partner_name: "合计", unpaid: "100" },
    }
  }
  if (p.endsWith("/statement")) {
    return {
      direction: "ar", partner_id: "x-1", partner_name: "测试", from: "2026-10-01", to: "2026-10-10",
      opening: "0", billed: "0", returned: "0", paid: "0", adjustment: "0", closing: "0", lines: [],
    }
  }
  if (/\/(sale|purchase)-bills\/[^/]+$/.test(p)) {
    return {
      head: { ...bill, status: 2, total_amount: "100.00", paid_amount: "0", partner_id: "x-1", warehouse_id: "x-1" },
      items: [],
      payments: [],
    }
  }
  if (p.endsWith("/units")) {
    return { items: [{ id: "u-1", code: "box", name: "箱", unit_type: "count", is_system: false }], total: 1 }
  }
  if (/\/(customers|suppliers|warehouses)$/.test(p)) {
    return { items: [{ id: "x-1", tenant_id: "t", tenantId: "t", name: "测试", code: "c-1", isDefault: false }], total: 1 }
  }
  return { items: [], total: 0 }
}

interface Req { method: string; url: string }
let seen: Req[] = []

beforeEach(() => {
  seen = []
  state.loading = false
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url
      seen.push({ method: (init?.method ?? "GET").toUpperCase(), url })
      return { ok: true, status: 200, json: async () => payload(url), text: async () => "" } as Response
    }),
  )
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

async function settle() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 30))
  })
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type Load = () => Promise<Record<string, any>>

interface PageCase {
  name: string
  load: Load
  /** The page address query, e.g. "tab=audit". */
  query?: string
  /** Click this row text after loading, to open the detail drawer. */
  openRow?: string
  /** Async server components are awaited with these props; client components are rendered. */
  server?: boolean
  props?: Record<string, unknown>
  /** Buttons or links that lead to a gated route: shown only to roles holding the permission. */
  controls: { text: string; perm: Perm }[]
  /** Requests that may go out while the role is still unknown (the page shows an explanation once it is known). */
  allowWhileLoading?: string[]
}

const ACCOUNT = { load: () => import("../app/(dashboard)/account/account-center"), props: { ssoConsoleURL: "" } }

const PAGES: PageCase[] = [
  {
    name: "客户",
    load: () => import("../app/(dashboard)/customers/page"),
    controls: [
      { text: "新增客户", perm: "customer.write" },
      { text: "收款", perm: "payment.record" },
      { text: "对账单", perm: "payment.read" },
      { text: "欠款", perm: "payment.read" },
    ],
  },
  {
    name: "供应商",
    load: () => import("../app/(dashboard)/suppliers/page"),
    controls: [
      { text: "新增供应商", perm: "catalog.write" },
      { text: "付款", perm: "payment.record" },
      { text: "对账单", perm: "payment.read" },
      { text: "欠供应商款", perm: "payment.read" },
    ],
  },
  {
    name: "仓库",
    load: () => import("../app/(dashboard)/warehouses/page"),
    controls: [
      { text: "新建仓库", perm: "catalog.write" },
      { text: "编辑", perm: "catalog.write" },
      { text: "删除", perm: "catalog.write" },
    ],
  },
  {
    name: "仓库详情",
    load: () => import("../app/(dashboard)/warehouses/page"),
    openRow: "测试",
    controls: [
      { text: "删除", perm: "catalog.write" },
    ],
  },
  {
    name: "单位",
    load: () => import("../app/(dashboard)/units/page"),
    controls: [
      { text: "新建单位", perm: "product.write" },
      { text: "删除", perm: "product.write" },
    ],
  },
  {
    name: "待办",
    load: () => import("../app/(dashboard)/todo/page"),
    server: true,
    controls: [
      { text: "去审核", perm: "bill.approve" },
      { text: "去补货", perm: "bill.draft" },
      { text: "去收款", perm: "payment.record" },
      { text: "去开票", perm: "invoice.manage" },
    ],
  },
  {
    name: "库存",
    load: () => import("../app/(dashboard)/stock/page"),
    controls: [{ text: "导出表格", perm: "export.stock" }],
  },
  {
    name: "商品",
    load: () => import("../app/(dashboard)/products/page"),
    controls: [
      { text: "新建商品", perm: "product.write" },
      { text: "批量导入", perm: "product.write" },
    ],
  },
  {
    name: "销售",
    load: () => import("../app/(dashboard)/sales/page"),
    controls: [
      { text: "新建销售单", perm: "bill.draft" },
      { text: "快速收银", perm: "bill.checkout" },
    ],
  },
  {
    name: "采购",
    load: () => import("../app/(dashboard)/purchases/page"),
    controls: [{ text: "新建采购单", perm: "bill.draft" }],
  },
  {
    name: "首页",
    load: () => import("../app/(dashboard)/dashboard/page"),
    server: true,
    props: { searchParams: {} },
    controls: [
      { text: "去审核", perm: "bill.approve" },
      { text: "去收款", perm: "payment.record" },
    ],
  },
  { name: "付款与对账", load: () => import("../app/(dashboard)/payments/page"), controls: [] },
  { name: "发票", load: () => import("../app/(dashboard)/invoices/page"), controls: [] },
  { name: "对账单", load: () => import("../app/(dashboard)/statement/page"), query: "partner=x-1", controls: [] },
  {
    name: "打印设置",
    load: () => import("../app/(dashboard)/settings/print/page"),
    controls: [{ text: "保存店铺信息", perm: "settings.manage" }],
  },
  { name: "销售详情", load: () => import("../app/(dashboard)/sales/[id]/page"), controls: [] },
  {
    name: "采购详情",
    load: () => import("../app/(dashboard)/purchases/[id]/page"),
    controls: [],
    allowWhileLoading: ["GET /api/v1/purchase-bills/b-1"],
  },
  {
    name: "报表",
    load: () => import("../app/(dashboard)/reports/page"),
    server: true,
    props: { searchParams: {} },
    controls: [{ text: "净销售额", perm: "report.view" }],
  },
  { name: "账户", ...ACCOUNT, controls: [{ text: "活动日志", perm: "settings.manage" }] },
  ...["audit", "security", "notifications", "team", "api-keys", "wallet", "data"].map((tab) => ({
    name: `账户 ${tab}`,
    ...ACCOUNT,
    query: `tab=${tab}`,
    controls: [] as PageCase["controls"],
  })),
]

async function mount(c: PageCase) {
  const mod = await c.load()
  const Comp = (mod.default ?? mod.AccountCenter) as React.ComponentType<Record<string, unknown>>
  const node = c.server
    ? await (Comp as unknown as (p: Record<string, unknown>) => Promise<React.ReactElement>)(c.props ?? {})
    : <Comp {...(c.props ?? {})} />
  const out = render(node)
  await settle()
  if (c.openRow) {
    fireEvent.click(screen.getAllByText(c.openRow)[0])
    await settle()
  }
  return out
}

describe("pages ask only for routes the role may call", () => {
  for (const c of PAGES) {
    for (const role of ROLES) {
      it(`${c.name} / ${role}`, async () => {
        state.role = role
        state.query = c.query ?? ""
        await mount(c)
        const forbidden = seen
          .map((r) => ({ ...r, perm: needed(r.method, r.url) }))
          .filter((r) => r.perm !== null && !roleCan(role, r.perm))
          .map((r) => `${r.method} ${apiPath(r.url)} needs ${r.perm}`)
        expect(forbidden).toEqual([])

        const text = document.body.textContent ?? ""
        for (const ctl of c.controls) {
          const shown = text.includes(ctl.text)
          if (roleCan(role, ctl.perm)) {
            // A control that is missing for everyone means its label went stale.
            expect(shown, `${ctl.text} should be offered to ${role}`).toBe(true)
            continue
          }
          expect(shown, `${ctl.text} must be hidden from ${role}`).toBe(false)
        }
      })
    }
  }
})

// A full page load starts with the session still loading, the role unknown. Nothing
// restricted may be asked for then: a 403 that wins the race shows "no permission".
describe("pages ask for nothing restricted while the session is loading", () => {
  for (const c of PAGES) {
    it(c.name, async () => {
      state.role = "cashier"
      state.query = c.query ?? ""
      state.loading = true
      await mount(c)
      const restricted = seen
        .map((r) => ({ ...r, perm: needed(r.method, r.url) }))
        .filter((r) => r.perm !== null)
        .map((r) => `${r.method} ${apiPath(r.url)} needs ${r.perm}`)
        .filter((line) => !(c.allowWhileLoading ?? []).some((a) => line.startsWith(a + " ")))
      expect(restricted).toEqual([])
    })
  }
})

// Local print choices live in this browser only: every role can open and set them.
describe("打印设置 local choices", () => {
  for (const role of ROLES) {
    it(`小票纸宽 is offered to ${role}`, async () => {
      state.role = role
      state.query = ""
      await mount(PAGES.find((c) => c.name === "打印设置")!)
      const text = document.body.textContent ?? ""
      expect(text).toContain("小票纸宽")
      expect(text).toContain("自动打印小票")
    })
  }
})

describe("采购详情 without purchase.view", () => {
  it("cashier gets an explanation and no bill request", async () => {
    state.role = "cashier"
    state.query = ""
    await mount(PAGES.find((c) => c.name === "采购详情")!)
    expect(document.body.textContent).toContain("没有查看采购单的权限")
    expect(seen.filter((r) => apiPath(r.url) === "/api/v1/purchase-bills/b-1")).toEqual([])
  })
})

// The backend already empties these sections for the roles below; the page must
// not depend on that alone, so a body that still carries the money is not shown.
describe("待办 hides money the role may not see", () => {
  for (const role of ROLES) {
    it(`${role}`, async () => {
      state.role = role
      state.query = ""
      await mount(PAGES.find((c) => c.name === "待办")!)
      const text = document.body.textContent ?? ""
      expect(text.includes("98,765.43") || text.includes("98765.43")).toBe(roleCan(role, "cost.view"))
      expect(text.includes("7,654.32") || text.includes("7654.32")).toBe(roleCan(role, "payment.read"))
    })
  }
})

// A section whose count is larger than the rows sent says how many are left,
// for every role that sees the section at all.
describe("待办 count larger than items", () => {
  it("owner is told how many bills are not listed", async () => {
    const saved = todoSummary.draft_sales.count
    todoSummary.draft_sales.count = 5
    try {
      state.role = "owner"
      state.query = ""
      await mount(PAGES.find((c) => c.name === "待办")!)
      expect(document.body.textContent).toContain("还有 4 张")
    } finally {
      todoSummary.draft_sales.count = saved
    }
  })

  it("no note when count equals the rows sent", async () => {
    state.role = "owner"
    state.query = ""
    await mount(PAGES.find((c) => c.name === "待办")!)
    expect(document.body.textContent).not.toContain("还有")
  })
})
