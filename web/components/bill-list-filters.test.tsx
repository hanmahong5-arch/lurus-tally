import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { render, screen, fireEvent, act, waitFor } from "@testing-library/react"
import { BillListFilters, useBillFilters, parseBillFilters, toBillFilterParams, billRangeError } from "./bill-list-filters"

let query = ""
const replace = vi.fn()

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace }),
  usePathname: () => "/sales",
  useSearchParams: () => new URLSearchParams(query),
}))
vi.mock("@/hooks/use-tenant-id", () => ({ useTenantId: () => "t-1" }))
vi.mock("@/lib/api/customers", () => ({
  getCustomer: vi.fn().mockResolvedValue({ id: "c-9", name: "老王花店" }),
  listCustomers: vi.fn().mockResolvedValue({ items: [], total: 0 }) }))
vi.mock("@/lib/api/suppliers", () => ({ listSuppliers: vi.fn().mockResolvedValue({ items: [], total: 0 }) }))

function Harness({ kind }: { kind: "sale" | "purchase" }) {
  const { filters, update } = useBillFilters()
  return <BillListFilters kind={kind} filters={filters} onChange={update} />
}

function lastUrl(): URL {
  const arg = replace.mock.calls.at(-1)?.[0] as string
  return new URL(arg, "http://x")
}

describe("BillListFilters", () => {
  beforeEach(() => {
    query = ""
    replace.mockReset()
  })
  afterEach(() => vi.useRealTimers())

  it("defaults to 本月", () => {
    render(<Harness kind="sale" />)
    expect(screen.getByRole("button", { name: "本月" }).getAttribute("aria-pressed")).toBe("true")
  })

  it("writes the preset to the URL on switch", () => {
    render(<Harness kind="sale" />)
    fireEvent.click(screen.getByRole("button", { name: "今天" }))
    expect(lastUrl().pathname).toBe("/sales")
    expect(lastUrl().searchParams.get("range")).toBe("today")
  })

  it("drops the range param when going back to the default", () => {
    query = "range=today"
    render(<Harness kind="sale" />)
    fireEvent.click(screen.getByRole("button", { name: "本月" }))
    expect(replace.mock.calls.at(-1)?.[0]).toBe("/sales")
  })

  it("restores state from the URL", () => {
    query = "range=last_month&pay=open&mine=1&partner=c-9&pname=%E8%80%81%E7%8E%8B&q=abc"
    render(<Harness kind="sale" />)
    expect(screen.getByRole("button", { name: "上月" }).getAttribute("aria-pressed")).toBe("true")
    expect(screen.getByRole("button", { name: "本月" }).getAttribute("aria-pressed")).toBe("false")
    expect((screen.getByLabelText("收款状态") as HTMLSelectElement).value).toBe("open")
    expect(screen.getByRole("switch").getAttribute("aria-checked")).toBe("true")
    expect((screen.getByLabelText("搜索") as HTMLInputElement).value).toBe("abc")
    expect((screen.getByPlaceholderText("全部客户") as HTMLInputElement).value).toBe("老王")
  })

  it("maps 未结清 to payment_status=open", () => {
    render(<Harness kind="sale" />)
    fireEvent.change(screen.getByLabelText("收款状态"), { target: { value: "open" } })
    expect(lastUrl().searchParams.get("pay")).toBe("open")
    expect(toBillFilterParams(parseBillFilters(lastUrl().searchParams)).payment_status).toBe("open")
    expect(screen.getByRole("option", { name: "未结清" })).toBeTruthy()
  })

  it("uses 收 wording for sales and 付 for purchases", () => {
    const { unmount } = render(<Harness kind="sale" />)
    expect(screen.getByRole("option", { name: "未收" })).toBeTruthy()
    unmount()
    render(<Harness kind="purchase" />)
    expect(screen.getByLabelText("付款状态")).toBeTruthy()
    expect(screen.getByRole("option", { name: "未付" })).toBeTruthy()
  })

  it("shows date inputs only for 自定义 and writes both bounds", () => {
    query = "range=custom"
    render(<Harness kind="sale" />)
    fireEvent.change(screen.getByLabelText("开始日期"), { target: { value: "2026-10-01" } })
    expect(lastUrl().searchParams.get("range")).toBe("custom")
    expect(lastUrl().searchParams.get("from")).toBe("2026-10-01")
  })

  it("prefills the custom range from the preset on screen", () => {
    render(<Harness kind="sale" />)
    fireEvent.click(screen.getByRole("button", { name: "自定义" }))
    const sp = lastUrl().searchParams
    expect(sp.get("range")).toBe("custom")
    expect(sp.get("from")).toMatch(/^\d{4}-\d{2}-01$/)
    expect(sp.get("to")).toMatch(/^\d{4}-\d{2}-\d{2}$/)
    expect(toBillFilterParams(parseBillFilters(sp)).date_from).toBe(sp.get("from"))
  })

  it("keeps the status tab in the URL and restores it", () => {
    query = "status=2"
    expect(parseBillFilters(new URLSearchParams(query)).status).toBe(2)
    expect(parseBillFilters(new URLSearchParams("status=7")).status).toBeUndefined()
    render(<Harness kind="sale" />)
    fireEvent.click(screen.getByRole("button", { name: "今天" }))
    expect(lastUrl().searchParams.get("status")).toBe("2")
  })

  it("hides date inputs for presets", () => {
    render(<Harness kind="sale" />)
    expect(screen.queryByLabelText("开始日期")).toBeNull()
  })

  it("debounces the search box by 300ms", () => {
    vi.useFakeTimers()
    render(<Harness kind="sale" />)
    const input = screen.getByLabelText("搜索")
    fireEvent.change(input, { target: { value: "老" } })
    fireEvent.change(input, { target: { value: "老王" } })
    act(() => {
      vi.advanceTimersByTime(299)
    })
    expect(replace).not.toHaveBeenCalled()
    act(() => {
      vi.advanceTimersByTime(2)
    })
    expect(replace).toHaveBeenCalledTimes(1)
    expect(lastUrl().searchParams.get("q")).toBe("老王")
  })

  it("keeps extra keystrokes when the URL echoes the pushed value back", () => {
    vi.useFakeTimers()
    const { rerender } = render(<Harness kind="sale" />)
    const input = screen.getByLabelText("搜索") as HTMLInputElement
    fireEvent.change(input, { target: { value: "ab" } })
    act(() => {
      vi.advanceTimersByTime(301)
    })
    fireEvent.change(input, { target: { value: "abc" } })
    query = "q=ab"
    rerender(<Harness kind="sale" />)
    expect(input.value).toBe("abc")
    // a different value (back button / clear) still syncs in
    query = "q=zz"
    rerender(<Harness kind="sale" />)
    expect(input.value).toBe("zz")
  })

  it("looks up the partner name when the URL carries only the id", async () => {
    query = "partner=c-9"
    render(<Harness kind="sale" />)
    await waitFor(() => expect((screen.getByPlaceholderText("全部客户") as HTMLInputElement).value).toBe("老王花店"))
  })

  it("flags a custom range that starts after it ends", () => {
    const f = parseBillFilters(new URLSearchParams("range=custom&from=2026-10-09&to=2026-10-01"))
    expect(billRangeError(f)).toBe("开始日期不能晚于结束日期")
    expect(billRangeError(parseBillFilters(new URLSearchParams("range=custom&from=2026-10-01&to=2026-10-09")))).toBe("")
  })

  it("toggles 只看我的", () => {
    render(<Harness kind="purchase" />)
    fireEvent.click(screen.getByRole("switch"))
    expect(lastUrl().searchParams.get("mine")).toBe("1")
  })
})

describe("toBillFilterParams", () => {
  it("resolves the preset on the Shanghai calendar and omits empty filters", () => {
    const p = toBillFilterParams(parseBillFilters(new URLSearchParams("")), new Date("2026-01-10T04:00:00Z"))
    expect(p).toEqual({
      date_from: "2026-01-01",
      date_to: "2026-01-31",
      partner_id: undefined,
      payment_status: undefined,
      q: undefined,
      mine: undefined,
    })
  })
})

describe("invoice_status filter", () => {
  beforeEach(() => {
    query = ""
    replace.mockReset()
  })

  it("writes 开票状态 to the URL and the API params", () => {
    render(<Harness kind="sale" />)
    fireEvent.change(screen.getByLabelText("开票状态"), { target: { value: "partial" } })
    expect(lastUrl().searchParams.get("inv")).toBe("partial")
    expect(toBillFilterParams(parseBillFilters(lastUrl().searchParams)).invoice_status).toBe("partial")
  })

  it("restores 开票状态 from the URL and ignores junk", () => {
    query = "inv=full"
    const { unmount } = render(<Harness kind="sale" />)
    expect((screen.getByLabelText("开票状态") as HTMLSelectElement).value).toBe("full")
    unmount()
    query = "inv=bogus"
    render(<Harness kind="sale" />)
    expect((screen.getByLabelText("开票状态") as HTMLSelectElement).value).toBe("")
  })
})
