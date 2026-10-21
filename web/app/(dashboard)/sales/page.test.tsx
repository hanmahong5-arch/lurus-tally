import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor, fireEvent, within } from "@testing-library/react"
import * as saleApi from "@/lib/api/sale"
import SalesPage from "./page"
import { roleCan, type ShopRole } from "@/lib/roles"

let query = ""
const replace = vi.fn()

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace, refresh: vi.fn(), push: vi.fn() }),
  usePathname: () => "/sales",
  useSearchParams: () => new URLSearchParams(query),
}))
vi.mock("@/hooks/use-tenant-id", () => ({ useTenantId: () => "t-1" }))
// Shop role of the signed-in person; null means the session has not told us yet.
const roleState = vi.hoisted(() => ({ role: null as string | null }))
vi.mock("next-auth/react", () => ({
  useSession: () => ({
    data: roleState.role ? { user: { role: roleState.role, isOwner: roleState.role === "owner" } } : null,
  }),
}))
vi.mock("@/lib/api/customers", () => ({ listCustomers: vi.fn().mockResolvedValue({ items: [], total: 0 }) }))
vi.mock("@/lib/api/suppliers", () => ({ listSuppliers: vi.fn().mockResolvedValue({ items: [], total: 0 }) }))
vi.mock("@/lib/api/sale", async () => {
  const actual = await vi.importActual<typeof import("@/lib/api/sale")>("@/lib/api/sale")
  return { ...actual, listSaleBills: vi.fn(), cancelSaleBill: vi.fn(), restoreSaleBill: vi.fn() }
})

const head = {
  id: "b-1",
  tenant_id: "t-1",
  bill_no: "SL-20261010-0001",
  bill_type: "出库",
  sub_type: "销售",
  status: 2,
  partner_id: "c-1",
  partner_name: "老王花店",
  creator_id: "u-1",
  bill_date: "2026-10-10T00:00:00Z",
  subtotal: "200.0000",
  shipping_fee: "0.0000",
  tax_amount: "0.0000",
  total_amount: "200.0000",
  paid_amount: "50.0000",
  receivable_amount: "150.0000",
  unpaid_amount: "150.0000",
  payment_status: "partial",
  created_at: "2026-10-10T00:00:00Z",
  updated_at: "2026-10-10T00:00:00Z",
} as saleApi.SaleBillHead

describe("SalesPage", () => {
  beforeEach(() => {
    query = ""
    replace.mockReset()
    roleState.role = null
    vi.mocked(saleApi.listSaleBills).mockReset()
  })

  it("shows the customer name, payment tag and the backend summary", async () => {
    vi.mocked(saleApi.listSaleBills).mockResolvedValue({
      items: [head],
      total: 1,
      summary: { count: 1, total_amount: "200.0000", paid_amount: "50.0000", unpaid_amount: "150.0000" },
    })
    render(<SalesPage />)
    expect(await screen.findByText("老王花店")).toBeTruthy()
    expect(within(screen.getByRole("table")).getByText("部分")).toBeTruthy()
    const bar = screen.getByTestId("bill-summary")
    expect(bar.textContent).toContain("共 1 单")
    expect(bar.textContent).toContain("合计 ¥200.00")
    expect(bar.textContent).toContain("已收 ¥50.00")
    expect(bar.textContent).toContain("未收 ¥150.00")
    expect(screen.queryByText("c-1")).toBeNull()
  })

  it("passes the URL filters to the list call and the export link", async () => {
    query = "range=today&pay=open&mine=1&q=abc&partner=c-1&pname=%E8%80%81%E7%8E%8B"
    vi.mocked(saleApi.listSaleBills).mockResolvedValue({ items: [head], total: 1 })
    render(<SalesPage />)
    await screen.findByText("老王花店")
    const call = vi.mocked(saleApi.listSaleBills).mock.calls[0][0]!
    expect(call).toMatchObject({ payment_status: "open", mine: true, q: "abc", partner_id: "c-1" })
    expect(call.date_from).toMatch(/^\d{4}-\d{2}-\d{2}$/)
    expect(call.date_from).toBe(call.date_to)

    const link = screen.getByRole("link", { name: "导出当前筛选" })
    const u = new URL(link.getAttribute("href")!, "http://x")
    expect(u.pathname).toBe("/api/proxy/exports/bills.csv")
    expect(u.searchParams.get("type")).toBe("sale")
    expect(u.searchParams.get("payment_status")).toBe("open")
    expect(u.searchParams.get("mine")).toBe("1")
    expect(u.searchParams.get("q")).toBe("abc")
    expect(u.searchParams.get("partner_id")).toBe("c-1")
    expect(u.searchParams.get("date_from")).toBe(call.date_from)
  })

  it("shows the empty hint with a clear-filters button", async () => {
    query = "pay=unpaid"
    vi.mocked(saleApi.listSaleBills).mockResolvedValue({ items: [], total: 0 })
    render(<SalesPage />)
    expect(await screen.findByText("这个时间段没有单据")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "清除筛选" }))
    await waitFor(() => expect(replace).toHaveBeenCalled())
    expect(replace.mock.calls.at(-1)?.[0]).toBe("/sales")
  })

  it("does not call a customer with a profile 散客 when the name is missing", async () => {
    vi.mocked(saleApi.listSaleBills).mockResolvedValue({
      items: [{ ...head, partner_name: undefined }, { ...head, id: "b-2", partner_id: undefined, partner_name: undefined }],
      total: 2,
    })
    render(<SalesPage />)
    await waitFor(() => expect(screen.getAllByRole("row").length).toBe(3))
    const cells = within(screen.getByRole("table")).queryAllByText("散客")
    expect(cells.length).toBe(1)
  })

  it("shows a dash instead of a guessed balance for old responses and drafts", async () => {
    vi.mocked(saleApi.listSaleBills).mockResolvedValue({
      items: [{ ...head, unpaid_amount: undefined, payment_status: undefined, status: 0 }],
      total: 1,
    })
    render(<SalesPage />)
    await waitFor(() => expect(screen.getAllByRole("row").length).toBe(2))
    expect(within(screen.getByRole("table")).queryByText("¥150.00")).toBeNull()
    expect(within(screen.getByRole("table")).queryByText("¥200.00", { selector: ".text-warning" })).toBeNull()
  })

  it("shows no debt for draft and cancelled bills even when the backend sends unpaid_amount", async () => {
    vi.mocked(saleApi.listSaleBills).mockResolvedValue({
      items: [
        { ...head, id: "d", status: 0, unpaid_amount: "777.0000" },
        { ...head, id: "x", status: 9, unpaid_amount: "888.0000" },
      ],
      total: 2,
    })
    render(<SalesPage />)
    await waitFor(() => expect(screen.getAllByRole("row").length).toBe(3))
    const table = within(screen.getByRole("table"))
    expect(table.queryByText("¥777.00")).toBeNull()
    expect(table.queryByText("¥888.00")).toBeNull()
  })

  it("drops the settlement filter when switching to the draft tab", async () => {
    query = "pay=unpaid"
    vi.mocked(saleApi.listSaleBills).mockResolvedValue({ items: [], total: 0 })
    render(<SalesPage />)
    fireEvent.click(await screen.findByRole("tab", { name: "草稿" }))
    await waitFor(() => expect(replace).toHaveBeenCalled())
    expect(String(replace.mock.calls.at(-1)![0])).not.toContain("pay=")
  })

  it("offers 清除筛选 on an empty draft tab and resets the tab", async () => {
    vi.mocked(saleApi.listSaleBills).mockResolvedValue({ items: [], total: 0 })
    render(<SalesPage />)
    fireEvent.click(await screen.findByRole("tab", { name: "草稿" }))
    fireEvent.click(await screen.findByRole("button", { name: "清除筛选" }))
    await waitFor(() =>
      expect(vi.mocked(saleApi.listSaleBills).mock.calls.at(-1)![0]!.status).toBeUndefined(),
    )
  })

  it("does not call the list API for a reversed custom range", async () => {
    query = "range=custom&from=2026-10-09&to=2026-10-01"
    vi.mocked(saleApi.listSaleBills).mockResolvedValue({ items: [], total: 0 })
    render(<SalesPage />)
    expect((await screen.findAllByText("开始日期不能晚于结束日期")).length).toBeGreaterThan(0)
    expect(vi.mocked(saleApi.listSaleBills)).not.toHaveBeenCalled()
  })

  it("gives a cashier quick checkout but no draft, export or reports entry", async () => {
    roleState.role = "cashier"
    vi.mocked(saleApi.listSaleBills).mockResolvedValue({ items: [head], total: 1 })
    render(<SalesPage />)
    await screen.findByText("老王花店")
    expect(screen.getByRole("link", { name: "+ 快速收银" })).toBeTruthy()
    expect(screen.queryByRole("link", { name: "+ 新建销售单" })).toBeNull()
    expect(screen.queryByText("导出当前筛选")).toBeNull()
  })

  it("gives a clerk drafts but no quick checkout or export", async () => {
    roleState.role = "clerk"
    vi.mocked(saleApi.listSaleBills).mockResolvedValue({ items: [head], total: 1 })
    render(<SalesPage />)
    await screen.findByText("老王花店")
    expect(screen.getByRole("link", { name: "+ 新建销售单" })).toBeTruthy()
    expect(screen.queryByRole("link", { name: "+ 快速收银" })).toBeNull()
    expect(screen.queryByText("导出当前筛选")).toBeNull()
  })

  it("gives a manager everything", async () => {
    roleState.role = "manager"
    vi.mocked(saleApi.listSaleBills).mockResolvedValue({ items: [head], total: 1 })
    render(<SalesPage />)
    await screen.findByText("老王花店")
    expect(screen.getByRole("link", { name: "+ 快速收银" })).toBeTruthy()
    expect(screen.getByRole("link", { name: "+ 新建销售单" })).toBeTruthy()
    expect(screen.getByText("导出当前筛选")).toBeTruthy()
  })
})

describe("SalesPage entries match the role's permissions", () => {
  beforeEach(() => {
    query = ""
    roleState.role = null
    vi.mocked(saleApi.listSaleBills).mockReset()
  })

  it.each(["owner", "manager", "clerk", "cashier", "finance"] as const)("%s sees only the entries the backend accepts", async (role: ShopRole) => {
    roleState.role = role
    vi.mocked(saleApi.listSaleBills).mockResolvedValue({
      items: [head, { ...head, id: "b-d", bill_no: "SL-D", status: 0 }],
      total: 2,
    })
    render(<SalesPage />)
    await screen.findAllByText("老王花店")
    expect(!!screen.queryByRole("button", { name: "取消" })).toBe(roleCan(role, "bill.cancel"))
    expect(!!screen.queryByRole("link", { name: "+ 快速收银" })).toBe(roleCan(role, "bill.checkout"))
    expect(!!screen.queryByRole("link", { name: "+ 新建销售单" })).toBe(roleCan(role, "bill.draft"))
    expect(!!screen.queryByText("导出当前筛选")).toBe(roleCan(role, "report.view"))
  })
})
