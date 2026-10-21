import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor, act, fireEvent } from "@testing-library/react"
import PosHistoryPage from "./page"

const mockListTodaySaleBills = vi.fn()
const mockGetPosSummary = vi.fn()
vi.mock("@/lib/api/pos", () => ({
  listTodaySaleBills: (...args: unknown[]) => mockListTodaySaleBills(...args),
  getPosSummary: (...args: unknown[]) => mockGetPosSummary(...args),
}))

const sampleSummary = {
  date: "2026-10-10",
  count: 25,
  total_amount: "1234.50",
  paid_amount: "1000.00",
  credit_amount: "234.50",
  by_pay_type: [
    { pay_type: "cash", count: 15, amount: "700.00" },
    { pay_type: "wechat", count: 10, amount: "300.00" },
  ],
  return_count: 2,
  return_amount: "88.00",
  refund_count: 3,
  refund_amount: "12.50",
}

const { roleState } = vi.hoisted(() => ({ roleState: { role: "" as string } }))
vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: roleState.role ? { user: { role: roleState.role, isOwner: roleState.role === "owner" } } : null }),
}))

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
}))

vi.mock("@/hooks/use-checkout-queue", () => ({
  useCheckoutQueue: () => ({
    entries: [
      {
        client_ref: "abcd1234-9",
        idempotency_key: "k",
        body: { items: [], payment_method: "cash", paid_amount: "5.00" },
        status: "pending",
        created_at: 1,
        occurred_at: 1,
        attempts: 0,
        total_amount: "5.00",
      },
    ],
    retry: vi.fn(),
    discard: vi.fn(),
  }),
}))

const sampleBills = [
  {
    id: "b1",
    bill_no: "SO-2024-0001",
    total_amount: "99.80",
    paid_amount: "99.80",
    unpaid_amount: "0",
    created_at: "2024-01-15T10:30:00Z",
  },
  {
    id: "b2",
    bill_no: "SO-2024-0002",
    total_amount: "49.50",
    paid_amount: "20.00",
    unpaid_amount: "29.50",
    created_at: "2024-01-15T11:00:00Z",
  },
]

beforeEach(() => {
  roleState.role = ""
  mockListTodaySaleBills.mockReset()
  mockGetPosSummary.mockReset()
  mockGetPosSummary.mockResolvedValue({ ...sampleSummary, count: 0, total_amount: "0.00", paid_amount: "0.00", credit_amount: "0.00", by_pay_type: [], return_count: 0, return_amount: "0.00", refund_count: 0, refund_amount: "0.00" })
  Object.defineProperty(window, "location", {
    value: { origin: "http://localhost:3000" },
    writable: true,
  })
})

describe("PosHistoryPage", () => {
  it("TestPosHistory_ShowsServerSummary: count, total and per-method amounts come from pos-summary", async () => {
    mockListTodaySaleBills.mockResolvedValue(sampleBills)
    mockGetPosSummary.mockResolvedValue(sampleSummary)

    await act(async () => {
      render(<PosHistoryPage />)
    })

    await waitFor(() => expect(screen.getByText("25")).toBeInTheDocument())
    expect(screen.getByText(/1,?234\.50/)).toBeInTheDocument()
    expect(screen.getByText(/700\.00/)).toBeInTheDocument()
    expect(screen.getByText(/300\.00/)).toBeInTheDocument()
    expect(screen.getByText(/退货 2 笔/)).toBeInTheDocument()
    expect(screen.getByText(/退款 3 笔/)).toBeInTheDocument()
    expect(screen.getByText(/-¥12\.50/)).toBeInTheDocument()
    expect(screen.getByText(/88\.00/)).toBeInTheDocument()
    expect(screen.getByText(/^¥234\.50$/)).toBeInTheDocument()
  })

  it("labels a refund-only payment method card as 代退退款 without a zero count", async () => {
    mockListTodaySaleBills.mockResolvedValue([])
    mockGetPosSummary.mockResolvedValue({ ...sampleSummary, by_pay_type: [{ pay_type: "cash", count: 0, amount: "-30.00" }] })
    await act(async () => {
      render(<PosHistoryPage />)
    })
    await waitFor(() => expect(screen.getByText(/· 代退退款/)).toBeInTheDocument())
    expect(screen.queryByText(/^0 笔$/)).not.toBeInTheDocument()
  })

  it("tells a cashier the summary only counts their own sales", async () => {
    roleState.role = "cashier"
    mockListTodaySaleBills.mockResolvedValue([])
    await act(async () => {
      render(<PosHistoryPage />)
    })
    expect(screen.getByText("只算你本人，全店请问老板或店长。")).toBeInTheDocument()
  })

  it("sends a finance user to the report and a clerk to the owner or manager", async () => {
    mockListTodaySaleBills.mockResolvedValue([])
    roleState.role = "finance"
    const first = render(<PosHistoryPage />)
    await act(async () => {})
    expect(screen.getByText("只算你本人，全店请看报表。")).toBeInTheDocument()
    first.unmount()
    roleState.role = "clerk"
    await act(async () => {
      render(<PosHistoryPage />)
    })
    expect(screen.getByText("只算你本人，全店请问老板或店长。")).toBeInTheDocument()
    expect(screen.queryByText(/全店请看报表/)).not.toBeInTheDocument()
  })

  it("TestPosHistory_SummaryError_ShowsChineseMessage", async () => {
    mockListTodaySaleBills.mockResolvedValue([])
    mockGetPosSummary.mockRejectedValue(new Error("boom"))

    await act(async () => {
      render(<PosHistoryPage />)
    })

    await waitFor(() => expect(screen.getByText("boom")).toBeInTheDocument())
    expect(screen.queryByText(/Error:/)).not.toBeInTheDocument()
  })

  it("TestPosHistory_EmptyState_ShowsMessage: empty response shows no-data message", async () => {
    mockListTodaySaleBills.mockResolvedValue([])

    await act(async () => {
      render(<PosHistoryPage />)
    })

    await waitFor(() => {
      expect(screen.getByText(/今日暂无交易/)).toBeInTheDocument()
    })
  })

  it("TestPosHistory_RefundOnlyDay_NotEmpty: a day with only a refund is not shown as no transactions", async () => {
    mockListTodaySaleBills.mockResolvedValue([])
    mockGetPosSummary.mockResolvedValue({ ...sampleSummary, count: 0, return_count: 0, refund_count: 1, refund_amount: "20.00" })

    await act(async () => {
      render(<PosHistoryPage />)
    })

    await waitFor(() => {
      expect(screen.getByText(/退款 1 笔/)).toBeInTheDocument()
    })
    expect(screen.queryByText(/今日暂无交易/)).not.toBeInTheDocument()
  })

  it("TestPosHistory_ShowsBillNumbers: bill numbers are displayed in the list", async () => {
    mockListTodaySaleBills.mockResolvedValue(sampleBills)

    await act(async () => {
      render(<PosHistoryPage />)
    })

    await waitFor(() => {
      expect(screen.getByText("SO-2024-0001")).toBeInTheDocument()
      expect(screen.getByText("SO-2024-0002")).toBeInTheDocument()
    })
  })

  it("TestPosHistory_PaidStatusColumn: shows 已收 / 赊账 from unpaid_amount", async () => {
    mockListTodaySaleBills.mockResolvedValue(sampleBills)

    await act(async () => {
      render(<PosHistoryPage />)
    })

    await waitFor(() => {
      expect(screen.getByText("已收")).toBeInTheDocument()
      expect(screen.getByText("赊账")).toBeInTheDocument()
      expect(screen.getByText(/29\.50/)).toBeInTheDocument()
    })
  })

  it("TestPosHistory_HasBackLink: page has a link back to the POS cashier", async () => {
    mockListTodaySaleBills.mockResolvedValue([])

    await act(async () => {
      render(<PosHistoryPage />)
    })

    await waitFor(() => {
      expect(screen.getByText(/返回收银台/)).toBeInTheDocument()
    })
  })

  it("TestPosHistory_ReprintLink: every bill row can reprint its receipt", async () => {
    mockListTodaySaleBills.mockResolvedValue(sampleBills)

    await act(async () => {
      render(<PosHistoryPage />)
    })

    await waitFor(() => {
      const links = screen.getAllByRole("link", { name: /补打小票/ })
      expect(links.map((l) => l.getAttribute("href"))).toEqual(["/print/pos/b1", "/print/pos/b2"])
    })
  })

  it("TestPosHistory_Cashier_SeesOwnShiftOnly: a cashier's summary and list are asked for with mine and cannot widen", async () => {
    roleState.role = "cashier"
    mockListTodaySaleBills.mockResolvedValue(sampleBills)
    mockGetPosSummary.mockResolvedValue(sampleSummary)

    await act(async () => {
      render(<PosHistoryPage />)
    })

    await waitFor(() => expect(screen.getByText("25")).toBeInTheDocument())
    expect(mockGetPosSummary).toHaveBeenLastCalledWith(undefined, undefined, expect.anything(), true)
    expect(mockListTodaySaleBills).toHaveBeenLastCalledWith(undefined, expect.anything(), true)
    expect(screen.getByText("我的今日结单")).toBeInTheDocument()
    expect(screen.queryByText("全店")).not.toBeInTheDocument()
  })

  it("TestPosHistory_CashierSession_FirstRequestIsMine: with the session present at first render, no unfiltered request goes out", async () => {
    roleState.role = "cashier"
    mockListTodaySaleBills.mockResolvedValue(sampleBills)
    mockGetPosSummary.mockResolvedValue(sampleSummary)

    await act(async () => {
      render(<PosHistoryPage />)
    })

    await waitFor(() => expect(screen.getByText("25")).toBeInTheDocument())
    expect(mockGetPosSummary).toHaveBeenCalledTimes(1)
    expect(mockGetPosSummary).toHaveBeenNthCalledWith(1, undefined, undefined, expect.anything(), true)
    expect(mockListTodaySaleBills).toHaveBeenCalledTimes(1)
    expect(mockListTodaySaleBills).toHaveBeenNthCalledWith(1, undefined, expect.anything(), true)
  })

  it("TestPosHistory_Manager_CanSwitchBetweenShopAndMine", async () => {
    roleState.role = "manager"
    mockListTodaySaleBills.mockResolvedValue(sampleBills)
    mockGetPosSummary.mockResolvedValue(sampleSummary)

    await act(async () => {
      render(<PosHistoryPage />)
    })

    await waitFor(() => expect(screen.getByText("25")).toBeInTheDocument())
    expect(mockGetPosSummary).toHaveBeenLastCalledWith(undefined, undefined, expect.anything(), false)

    await act(async () => {
      fireEvent.click(screen.getByText("只看我的"))
    })
    await waitFor(() => expect(mockGetPosSummary).toHaveBeenLastCalledWith(undefined, undefined, expect.anything(), true))
    expect(mockListTodaySaleBills).toHaveBeenLastCalledWith(undefined, expect.anything(), true)
  })

  describe("per-cashier split", () => {
    const KEY_A = "7f1c2a54-3b7e-4d0a-9c85-2e4b7a91d3f6"
    const KEY_B = "8a1c2a54-3b7e-4d0a-9c85-2e4b7a91d3f7"
    const cashiers = [
      { key: KEY_A, name: "老王", count: 3, total_amount: "500.00", paid_amount: "480.00", refund_amount: "0.00" },
      { key: KEY_B, name: "小李", count: 2, total_amount: "300.00", paid_amount: "300.00", refund_amount: "5.00" },
    ]
    const personal = { ...sampleSummary, count: 3, total_amount: "500.00" }

    it("lists cashiers, opens one on click and returns to the whole shop", async () => {
      roleState.role = "owner"
      mockListTodaySaleBills.mockResolvedValue(sampleBills)
      mockGetPosSummary.mockImplementation((_d, _t, _s, _m, cashier) =>
        Promise.resolve(cashier ? personal : { ...sampleSummary, cashiers })
      )
      await act(async () => {
        render(<PosHistoryPage />)
      })
      await waitFor(() => expect(screen.getByText("按收银员")).toBeInTheDocument())
      expect(screen.getByText("老王")).toBeInTheDocument()
      expect(screen.getByText("2 单")).toBeInTheDocument()
      expect(screen.getByText(/实收 .*480\.00/)).toBeInTheDocument()

      await act(async () => {
        fireEvent.click(screen.getByText("老王"))
      })
      await waitFor(() => expect(mockGetPosSummary).toHaveBeenLastCalledWith(undefined, undefined, expect.anything(), false, KEY_A))
      expect(await screen.findByText("老王的今日结单")).toBeInTheDocument()
      expect(screen.queryByText("按收银员")).not.toBeInTheDocument()

      await act(async () => {
        fireEvent.click(screen.getByText("返回全店"))
      })
      await waitFor(() => expect(screen.getByText("按收银员")).toBeInTheDocument())
      expect(screen.getByText("今日结单")).toBeInTheDocument()
      expect(document.body.textContent ?? "").not.toMatch(/[0-9a-f]{8}-[0-9a-f]{4}-/)
    })

    it("does not show the list for one cashier or for the cashier role", async () => {
      roleState.role = "manager"
      mockListTodaySaleBills.mockResolvedValue(sampleBills)
      mockGetPosSummary.mockResolvedValue({ ...sampleSummary, cashiers: cashiers.slice(0, 1) })
      const { unmount } = render(<PosHistoryPage />)
      await waitFor(() => expect(screen.getByText("25")).toBeInTheDocument())
      expect(screen.queryByText("按收银员")).not.toBeInTheDocument()
      unmount()

      roleState.role = "cashier"
      mockGetPosSummary.mockResolvedValue({ ...sampleSummary, cashiers })
      await act(async () => {
        render(<PosHistoryPage />)
      })
      await waitFor(() => expect(screen.getByText("25")).toBeInTheDocument())
      expect(screen.queryByText("按收银员")).not.toBeInTheDocument()
      expect(screen.queryByText("老王")).not.toBeInTheDocument()
    })
  })
})

describe("PosHistoryPage 离线视图", () => {
  it("接口失败时显示本机未上传的笔数并列出台账", async () => {
    mockGetPosSummary.mockRejectedValue(new Error("network down"))
    mockListTodaySaleBills.mockRejectedValue(new Error("network down"))
    render(<PosHistoryPage />)
    expect(await screen.findByText("离线，仅显示本机未上传的 1 笔")).toBeInTheDocument()
    expect(screen.getByText("OFF-abcd-9")).toBeInTheDocument()
  })
})
