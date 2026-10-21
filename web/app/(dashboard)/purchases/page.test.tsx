import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor, fireEvent, within } from "@testing-library/react"
import * as purchaseApi from "@/lib/api/purchase"
import PurchasesPage from "./page"
import { roleCan, type ShopRole } from "@/lib/roles"

let query = ""
const replace = vi.fn()

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace, refresh: vi.fn(), push: vi.fn() }),
  usePathname: () => "/purchases",
  useSearchParams: () => new URLSearchParams(query),
}))
vi.mock("@/hooks/use-tenant-id", () => ({ useTenantId: () => "t-1" }))
// Role under test; null means the session has not told us yet.
const { roleState } = vi.hoisted(() => ({ roleState: { role: null as string | null } }))
vi.mock("@/hooks/use-role", async () => {
  const actual = await vi.importActual<typeof import("@/hooks/use-role")>("@/hooks/use-role")
  return {
    ...actual,
    useRole: () => ({
      role: roleState.role,
      known: roleState.role !== null,
      can: (p: string) => actual.roleCan(roleState.role as never, p as never),
    }),
  }
})
vi.mock("@/lib/api/suppliers", () => ({ listCustomers: vi.fn().mockResolvedValue({ items: [], total: 0 }) }))
vi.mock("@/lib/api/suppliers", () => ({ listSuppliers: vi.fn().mockResolvedValue({ items: [], total: 0 }) }))
vi.mock("@/lib/api/purchase", async () => {
  const actual = await vi.importActual<typeof import("@/lib/api/purchase")>("@/lib/api/purchase")
  return { ...actual, listPurchaseBills: vi.fn(), cancelPurchaseBill: vi.fn(), restorePurchaseBill: vi.fn() }
})

const head = {
  id: "b-1",
  tenant_id: "t-1",
  bill_no: "PO-20261010-0001",
  bill_type: "入库",
  sub_type: "采购",
  status: 2,
  partner_id: "c-1",
  partner_name: "绿源苗圃",
  creator_id: "u-1",
  bill_date: "2026-10-10T00:00:00Z",
  subtotal: "200.0000",
  shipping_fee: "0.0000",
  tax_amount: "0.0000",
  total_amount: "200.0000",
  paid_amount: "50.0000",
  unpaid_amount: "150.0000",
  payment_status: "partial",
  created_at: "2026-10-10T00:00:00Z",
  updated_at: "2026-10-10T00:00:00Z",
} as purchaseApi.BillHead

describe("PurchasesPage", () => {
  beforeEach(() => {
    query = ""
    replace.mockReset()
    vi.mocked(purchaseApi.listPurchaseBills).mockReset()
    roleState.role = "owner"
  })

  it("shows the customer name, payment tag and the backend summary", async () => {
    vi.mocked(purchaseApi.listPurchaseBills).mockResolvedValue({
      items: [head],
      total: 1,
      summary: { count: 1, total_amount: "200.0000", paid_amount: "50.0000", unpaid_amount: "150.0000" },
    })
    render(<PurchasesPage />)
    expect(await screen.findByText("绿源苗圃")).toBeTruthy()
    expect(within(screen.getByRole("table")).getByText("部分")).toBeTruthy()
    const bar = screen.getByTestId("bill-summary")
    expect(bar.textContent).toContain("共 1 单")
    expect(bar.textContent).toContain("合计 ¥200.00")
    expect(bar.textContent).toContain("已付 ¥50.00")
    expect(bar.textContent).toContain("未付 ¥150.00")
    expect(screen.queryByText("c-1")).toBeNull()
  })

  it("passes the URL filters to the list call and the export link", async () => {
    query = "range=today&pay=open&mine=1&q=abc&partner=c-1&pname=%E8%80%81%E7%8E%8B"
    vi.mocked(purchaseApi.listPurchaseBills).mockResolvedValue({ items: [head], total: 1 })
    render(<PurchasesPage />)
    await screen.findByText("绿源苗圃")
    const call = vi.mocked(purchaseApi.listPurchaseBills).mock.calls[0][0]!
    expect(call).toMatchObject({ payment_status: "open", mine: true, q: "abc", partner_id: "c-1" })
    expect(call.date_from).toMatch(/^\d{4}-\d{2}-\d{2}$/)
    expect(call.date_from).toBe(call.date_to)

    const link = screen.getByRole("link", { name: "导出当前筛选" })
    const u = new URL(link.getAttribute("href")!, "http://x")
    expect(u.pathname).toBe("/api/proxy/exports/bills.csv")
    expect(u.searchParams.get("type")).toBe("purchase")
    expect(u.searchParams.get("payment_status")).toBe("open")
    expect(u.searchParams.get("mine")).toBe("1")
    expect(u.searchParams.get("q")).toBe("abc")
    expect(u.searchParams.get("partner_id")).toBe("c-1")
    expect(u.searchParams.get("date_from")).toBe(call.date_from)
  })

  it("shows the empty hint with a clear-filters button", async () => {
    query = "pay=unpaid"
    vi.mocked(purchaseApi.listPurchaseBills).mockResolvedValue({ items: [], total: 0 })
    render(<PurchasesPage />)
    expect(await screen.findByText("这个时间段没有单据")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "清除筛选" }))
    await waitFor(() => expect(replace).toHaveBeenCalled())
    expect(replace.mock.calls.at(-1)?.[0]).toBe("/purchases")
  })

  it("does not call a customer with a profile 散客 when the name is missing", async () => {
    vi.mocked(purchaseApi.listPurchaseBills).mockResolvedValue({
      items: [{ ...head, partner_name: undefined }, { ...head, id: "b-2", partner_id: undefined, partner_name: undefined }],
      total: 2,
    })
    render(<PurchasesPage />)
    await waitFor(() => expect(screen.getAllByRole("row").length).toBe(3))
    const cells = within(screen.getByRole("table")).queryAllByText("散客")
    expect(cells.length).toBe(0)
  })

  it("shows a dash instead of a guessed balance for old responses and drafts", async () => {
    vi.mocked(purchaseApi.listPurchaseBills).mockResolvedValue({
      items: [{ ...head, unpaid_amount: undefined, payment_status: undefined, status: 0 }],
      total: 1,
    })
    render(<PurchasesPage />)
    await waitFor(() => expect(screen.getAllByRole("row").length).toBe(2))
    expect(within(screen.getByRole("table")).queryByText("¥150.00")).toBeNull()
    expect(within(screen.getByRole("table")).queryByText("¥200.00", { selector: ".text-warning" })).toBeNull()
  })

  it("shows no debt for draft and cancelled bills even when the backend sends unpaid_amount", async () => {
    vi.mocked(purchaseApi.listPurchaseBills).mockResolvedValue({
      items: [
        { ...head, id: "d", status: 0, unpaid_amount: "777.0000" },
        { ...head, id: "x", status: 9, unpaid_amount: "888.0000" },
      ],
      total: 2,
    })
    render(<PurchasesPage />)
    await waitFor(() => expect(screen.getAllByRole("row").length).toBe(3))
    const table = within(screen.getByRole("table"))
    expect(table.queryByText("¥777.00")).toBeNull()
    expect(table.queryByText("¥888.00")).toBeNull()
  })

  it("drops the settlement filter when switching to the draft tab", async () => {
    query = "pay=unpaid"
    vi.mocked(purchaseApi.listPurchaseBills).mockResolvedValue({ items: [], total: 0 })
    render(<PurchasesPage />)
    fireEvent.click(await screen.findByRole("tab", { name: "草稿" }))
    await waitFor(() => expect(replace).toHaveBeenCalled())
    expect(String(replace.mock.calls.at(-1)![0])).not.toContain("pay=")
  })

  it("offers 清除筛选 on an empty draft tab and resets the tab", async () => {
    vi.mocked(purchaseApi.listPurchaseBills).mockResolvedValue({ items: [], total: 0 })
    render(<PurchasesPage />)
    fireEvent.click(await screen.findByRole("tab", { name: "草稿" }))
    fireEvent.click(await screen.findByRole("button", { name: "清除筛选" }))
    await waitFor(() =>
      expect(vi.mocked(purchaseApi.listPurchaseBills).mock.calls.at(-1)![0]!.status).toBeUndefined(),
    )
  })

  it("does not call the list API for a reversed custom range", async () => {
    query = "range=custom&from=2026-10-09&to=2026-10-01"
    vi.mocked(purchaseApi.listPurchaseBills).mockResolvedValue({ items: [], total: 0 })
    render(<PurchasesPage />)
    expect((await screen.findAllByText("开始日期不能晚于结束日期")).length).toBeGreaterThan(0)
    expect(vi.mocked(purchaseApi.listPurchaseBills)).not.toHaveBeenCalled()
  })

  it("shows a clerk only their own drafts, with products and no money", async () => {
    roleState.role = "clerk"
    const mine = { ...head, status: 0, partner_name: "老张苗圃" } as purchaseApi.BillHead
    // The backend drops every amount for a caller without cost.view.
    for (const k of ["subtotal", "total_amount", "paid_amount", "unpaid_amount", "payment_status"]) {
      delete (mine as unknown as Record<string, unknown>)[k]
    }
    vi.mocked(purchaseApi.listPurchaseBills).mockResolvedValue({ items: [mine], total: 1 })
    const { container } = render(<PurchasesPage />)
    expect(await screen.findByText("我的采购单")).toBeTruthy()
    expect(await screen.findByText("老张苗圃")).toBeTruthy()
    expect(screen.getByRole("link", { name: "+ 新建采购单" })).toBeTruthy()
    expect(container.textContent).not.toMatch(/合计|未付|已付|导出|开票|¥/)
    expect(screen.queryByText("取消")).toBeNull()
    // Every draft, not just this month's: no date window goes to the backend.
    const call = vi.mocked(purchaseApi.listPurchaseBills).mock.calls[0][0]!
    expect(call.date_from).toBeUndefined()
    expect(call.date_to).toBeUndefined()
  })

  it("shows a clerk their own approved bill with an approved tag and no money, under the new subtitle", async () => {
    roleState.role = "clerk"
    const approved = { ...head, status: 2, partner_name: "老李苗圃" } as purchaseApi.BillHead
    for (const k of ["subtotal", "total_amount", "paid_amount", "unpaid_amount", "payment_status"]) {
      delete (approved as unknown as Record<string, unknown>)[k]
    }
    vi.mocked(purchaseApi.listPurchaseBills).mockResolvedValue({ items: [approved], total: 1 })
    const { container } = render(<PurchasesPage />)
    expect(await screen.findByText("老李苗圃")).toBeTruthy()
    expect(screen.getByText("你建的草稿，以及最近 7 天已审核的单；价格和金额由老板或店长填写")).toBeTruthy()
    // The tag is not tucked behind a desktop-only column: a phone sees it too.
    const tag = screen.getByText("已审核")
    expect(tag.closest("td")?.className ?? "").not.toContain("hidden")
    expect(container.textContent).not.toMatch(/¥/)
  })

  it("tells a clerk with no drafts how to start one", async () => {
    roleState.role = "clerk"
    vi.mocked(purchaseApi.listPurchaseBills).mockResolvedValue({ items: [], total: 0 })
    render(<PurchasesPage />)
    expect(await screen.findByText("还没有你建的采购草稿")).toBeTruthy()
  })

  it("does not read purchases for a cashier", async () => {
    roleState.role = "cashier"
    render(<PurchasesPage />)
    expect(await screen.findByText("这里看不了采购单")).toBeTruthy()
    expect(screen.queryByRole("link", { name: "+ 新建采购单" })).toBeNull()
    expect(purchaseApi.listPurchaseBills).not.toHaveBeenCalled()
  })

  it("reads the list for finance", async () => {
    roleState.role = "finance"
    vi.mocked(purchaseApi.listPurchaseBills).mockResolvedValue({ items: [head], total: 1 })
    render(<PurchasesPage />)
    expect(await screen.findByText("绿源苗圃")).toBeTruthy()
  })
})

describe("PurchasesPage entries match the role's permissions", () => {
  beforeEach(() => {
    roleState.role = null
    vi.mocked(purchaseApi.listPurchaseBills).mockReset()
  })

  it.each(["owner", "manager", "clerk", "cashier", "finance"] as const)("%s sees only the entries the backend accepts", async (role: ShopRole) => {
    roleState.role = role
    vi.mocked(purchaseApi.listPurchaseBills).mockResolvedValue({
      items: [{ ...head, status: 0, partner_name: "老张苗圃" } as purchaseApi.BillHead],
      total: 1,
    })
    render(<PurchasesPage />)
    if (!roleCan(role, "purchase.view")) {
      expect(await screen.findByText("这里看不了采购单")).toBeTruthy()
      expect(screen.queryByRole("link", { name: "+ 新建采购单" })).toBeNull()
      return
    }
    await screen.findByText("老张苗圃")
    expect(!!screen.queryByRole("link", { name: "+ 新建采购单" })).toBe(roleCan(role, "bill.draft"))
    // A clerk's list is a drafts-only view with no cancel button of its own.
    expect(!!screen.queryByRole("button", { name: "取消" })).toBe(roleCan(role, "bill.cancel"))
    expect(!!screen.queryByText("导出当前筛选")).toBe(roleCan(role, "report.view"))
  })
})
