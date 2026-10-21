import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor, fireEvent } from "@testing-library/react"

const mockSearch = vi.hoisted(() => ({ value: new URLSearchParams() }))

vi.mock("next/link", () => ({
  default: ({ href, children, ...props }: { href: string; children: React.ReactNode; [key: string]: unknown }) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}))

vi.mock("next/navigation", () => ({ useSearchParams: () => mockSearch.value }))
vi.mock("@/lib/api/receivables", () => ({ getReceivables: vi.fn(), listBillsOldestFirst: vi.fn() }))
vi.mock("@/lib/api/customers", () => ({ listCustomers: vi.fn().mockResolvedValue({ items: [], total: 0 }) }))
vi.mock("@/lib/api/suppliers", () => ({ listSuppliers: vi.fn().mockResolvedValue({ items: [], total: 0 }) }))
vi.mock("@/hooks/use-tenant-id", () => ({ useTenantId: () => undefined }))
vi.mock("@/hooks/use-role", () => ({ useRole: () => ({ role: "owner", known: true, can: () => true }) }))

import { getReceivables, listBillsOldestFirst } from "@/lib/api/receivables"
import PaymentsPage from "./page"

const saleRow = {
  id: "s1",
  bill_no: "SL-0001",
  bill_date: "2026-01-05T00:00:00Z",
  partner_name: "老张花木店",
  total_amount: "100",
  paid_amount: "30",
  unpaid_amount: "70",
  payment_status: "partial" as const,
}

const agingResult = {
  direction: "ar",
  as_of: "2026-10-10",
  items: [
    {
      partner_id: "p-1",
      partner_name: "老张花木店",
      unpaid: "1100.5",
      b0_30: "100",
      b31_60: "0",
      b61_90: "0",
      b90p: "1000.5",
      oldest_bill_date: "2026-05-01",
    },
    {
      partner_id: null,
      partner_name: "未指定往来单位",
      unpaid: "20",
      b0_30: "20",
      b31_60: "0",
      b61_90: "0",
      b90p: "0",
      oldest_bill_date: "2026-10-01",
    },
  ],
  total: {
    partner_id: null,
    partner_name: "合计",
    unpaid: "1120.5",
    b0_30: "120",
    b31_60: "0",
    b61_90: "0",
    b90p: "1000.5",
    oldest_bill_date: "2026-05-01",
  },
}

type ListCall = [string, Record<string, unknown>]

/** The most recent list call as [kind, params]. */
function lastListCall(): ListCall {
  const calls = vi.mocked(listBillsOldestFirst).mock.calls as unknown as ListCall[]
  return calls[calls.length - 1]
}

describe("PaymentsPage", () => {
  beforeEach(() => {
    mockSearch.value = new URLSearchParams()
    vi.mocked(listBillsOldestFirst).mockReset()
    vi.mocked(getReceivables).mockReset()
    vi.mocked(getReceivables).mockResolvedValue(agingResult as never)
    vi.mocked(listBillsOldestFirst).mockResolvedValue({
      items: [saleRow],
      total: 45,
      summary: { count: 45, total_amount: "5000", paid_amount: "1000", unpaid_amount: "4000.5" },
    } as never)
  })

  it("requests open approved bills oldest first and shows the unpaid total", async () => {
    render(<PaymentsPage />)
    await waitFor(() => expect(screen.getByText("¥4,000.50")).toBeInTheDocument())
    const [kind, params] = lastListCall()
    expect(kind).toBe("sale")
    expect(params).toMatchObject({ payment_status: "open", status: 2, page: 1, size: 20 })
    expect(screen.getByText("未收合计")).toBeInTheDocument()
  })

  it("shows the returned amount in the summary when there is one", async () => {
    vi.mocked(listBillsOldestFirst).mockResolvedValue({
      items: [saleRow],
      total: 1,
      summary: { count: 1, total_amount: "1000", paid_amount: "0", returned_amount: "300", unpaid_amount: "700" },
    } as never)
    render(<PaymentsPage />)
    await waitFor(() => expect(screen.getByText(/退货冲减/)).toBeInTheDocument())
    expect(screen.getByText("¥700.00")).toBeInTheDocument()
  })

  it("reads the ageing as of the day carried in the link", async () => {
    mockSearch.value = new URLSearchParams("tab=sales&as_of=2026-09-30")
    render(<PaymentsPage />)
    await waitFor(() => expect(getReceivables).toHaveBeenCalled())
    expect(vi.mocked(getReceivables).mock.calls[0][0]).toBe("ar")
    expect(vi.mocked(getReceivables).mock.calls[0][1]).toMatchObject({ asOf: "2026-09-30" })
    await waitFor(() => expect(screen.getByText(/截至 2026-09-30/)).toBeInTheDocument())
  })

  it("says the list below is current and offers a way back to today", async () => {
    mockSearch.value = new URLSearchParams("tab=sales&as_of=2026-09-30")
    render(<PaymentsPage />)
    await waitFor(() => expect(screen.getByText(/下方单据列表是当前还没结清的单据/)).toBeInTheDocument())
    expect(screen.getByRole("link", { name: "看今天" })).toHaveAttribute("href", "/payments?tab=sales")
  })

  it("ignores a malformed as_of", async () => {
    mockSearch.value = new URLSearchParams("as_of=yesterday")
    render(<PaymentsPage />)
    await waitFor(() => expect(getReceivables).toHaveBeenCalled())
    expect(vi.mocked(getReceivables).mock.calls[0][1]?.asOf).toBeUndefined()
  })

  it("requests page 2 when paging forward", async () => {
    render(<PaymentsPage />)
    await waitFor(() => expect(screen.getByText("¥4,000.50")).toBeInTheDocument())
    fireEvent.click(screen.getByRole("button", { name: "下一页" }))
    await waitFor(() => expect(lastListCall()[1]).toMatchObject({ payment_status: "open", page: 2 }))
  })

  it("defaults to the all-time range with no date bounds", async () => {
    render(<PaymentsPage />)
    await waitFor(() => expect(screen.getByText("¥4,000.50")).toBeInTheDocument())
    const params = (vi.mocked(listBillsOldestFirst).mock.calls as unknown as ListCall[])[0][1]
    expect(params.date_from).toBeUndefined()
    expect(params.date_to).toBeUndefined()
    expect(screen.getByRole("button", { name: "全部" })).toHaveAttribute("aria-pressed", "true")
  })

  it("choosing 全部 in the status dropdown drops payment_status", async () => {
    render(<PaymentsPage />)
    await waitFor(() => expect(screen.getByText("¥4,000.50")).toBeInTheDocument())
    fireEvent.change(screen.getByLabelText("收款状态"), { target: { value: "" } })
    await waitFor(() => expect(lastListCall()[1].payment_status).toBeUndefined())
  })

  it("blocks the request when the custom range is reversed", async () => {
    render(<PaymentsPage />)
    await waitFor(() => expect(screen.getByText("¥4,000.50")).toBeInTheDocument())
    fireEvent.click(screen.getByRole("button", { name: "自定义" }))
    fireEvent.change(screen.getByLabelText("开始日期"), { target: { value: "2026-05-10" } })
    fireEvent.change(screen.getByLabelText("结束日期"), { target: { value: "2026-05-01" } })
    await waitFor(() => expect(screen.getAllByText("开始日期不能晚于结束日期").length).toBeGreaterThan(0))
    const calls = vi.mocked(listBillsOldestFirst).mock.calls as unknown as ListCall[]
    expect(calls.every((c) => !(c[1].date_from === "2026-05-10" && c[1].date_to === "2026-05-01"))).toBe(true)
  })

  it("purchases tab lists purchase bills with the unpaid label", async () => {
    const summary = { count: 1, total_amount: "100", paid_amount: "30", unpaid_amount: "70" }
    vi.mocked(listBillsOldestFirst).mockImplementation((async (kind: string) =>
      kind === "purchase"
        ? { items: [{ ...saleRow, id: "p1", bill_no: "PO-1", partner_name: "绿源苗圃" }], total: 1, summary }
        : { items: [saleRow], total: 1, summary }) as never)
    render(<PaymentsPage />)
    await waitFor(() => expect(screen.getByText("未收合计")).toBeInTheDocument())
    fireEvent.click(screen.getByText("应付（采购）"))
    await waitFor(() => expect(screen.getByText("绿源苗圃")).toBeInTheDocument())
    expect(lastListCall()[0]).toBe("purchase")
    expect(lastListCall()[1]).toMatchObject({ payment_status: "open", status: 2, page: 1, size: 20 })
    expect(screen.getByText("未付合计")).toBeInTheDocument()
    expect(vi.mocked(getReceivables).mock.calls.some((c) => c[0] === "ap")).toBe(true)
  })

  describe("aging panel", () => {
    it("renders per-partner rows, the total row and a walk-in row without a link", async () => {
      render(<PaymentsPage />)
      const panel = await screen.findByTestId("aging-panel")
      expect(panel).toHaveTextContent("按客户汇总")
      expect(panel).toHaveTextContent("¥1,100.50")
      expect(panel).toHaveTextContent("未指定往来单位")
      expect(panel).toHaveTextContent("合计（共 2 家）")
      expect(panel).toHaveTextContent("¥1,120.50")
      // Only the real customer gets a drill-down button.
      expect(screen.getAllByRole("button", { name: "查看单据" })).toHaveLength(1)
    })

    it("查看单据 narrows the bill list to that partner", async () => {
      render(<PaymentsPage />)
      fireEvent.click(await screen.findByRole("button", { name: "查看单据" }))
      await waitFor(() => expect(lastListCall()[1]).toMatchObject({ partner_id: "p-1", payment_status: "open" }))
    })

    it("hides itself when nobody owes anything", async () => {
      vi.mocked(getReceivables).mockResolvedValue({ ...agingResult, items: [] } as never)
      render(<PaymentsPage />)
      await waitFor(() => expect(screen.getByText("¥4,000.50")).toBeInTheDocument())
      expect(screen.queryByTestId("aging-panel")).not.toBeInTheDocument()
    })

    it("a failed summary shows a note and leaves the bill list working", async () => {
      vi.mocked(getReceivables).mockRejectedValue(new Error("boom"))
      render(<PaymentsPage />)
      await waitFor(() => expect(screen.getByText(/汇总暂时加载失败/)).toBeInTheDocument())
      expect(screen.getByText("¥4,000.50")).toBeInTheDocument()
    })
  })

  describe("deep link from the customer / supplier page", () => {
    it("?tab=purchases opens payables filtered to that supplier", async () => {
      mockSearch.value = new URLSearchParams({ partner: "sup-9", pname: "绿源苗圃", tab: "purchases" })
      render(<PaymentsPage />)
      await waitFor(() => expect(vi.mocked(listBillsOldestFirst)).toHaveBeenCalled())
      const [kind, params] = lastListCall()
      expect(kind).toBe("purchase")
      expect(params).toMatchObject({ partner_id: "sup-9", payment_status: "open" })
      expect(vi.mocked(getReceivables).mock.calls[0][0]).toBe("ap")
    })

    it("?partner= alone opens receivables filtered to that customer", async () => {
      mockSearch.value = new URLSearchParams({ partner: "cus-3", pname: "老张花木店" })
      render(<PaymentsPage />)
      await waitFor(() => expect(vi.mocked(listBillsOldestFirst)).toHaveBeenCalled())
      expect(lastListCall()[0]).toBe("sale")
      expect(lastListCall()[1]).toMatchObject({ partner_id: "cus-3" })
    })
  })
})
