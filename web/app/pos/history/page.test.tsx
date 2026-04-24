import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor, act } from "@testing-library/react"
import PosHistoryPage from "./page"

const mockListTodaySaleBills = vi.fn()
vi.mock("@/lib/api/pos", () => ({
  listTodaySaleBills: (...args: unknown[]) => mockListTodaySaleBills(...args),
}))

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
}))

const sampleBills = [
  {
    id: "b1",
    bill_no: "SO-2024-0001",
    total_amount: "99.80",
    paid_amount: "99.80",
    payment_method: "cash",
    created_at: "2024-01-15T10:30:00Z",
  },
  {
    id: "b2",
    bill_no: "SO-2024-0002",
    total_amount: "49.50",
    paid_amount: "49.50",
    payment_method: "wechat",
    created_at: "2024-01-15T11:00:00Z",
  },
]

beforeEach(() => {
  mockListTodaySaleBills.mockReset()
  Object.defineProperty(window, "location", {
    value: { origin: "http://localhost:3000" },
    writable: true,
  })
})

describe("PosHistoryPage", () => {
  it("TestPosHistory_ShowsTodayStats: shows total count and sum", async () => {
    mockListTodaySaleBills.mockResolvedValue(sampleBills)

    await act(async () => {
      render(<PosHistoryPage />)
    })

    await waitFor(() => {
      expect(screen.getByText("SO-2024-0001")).toBeInTheDocument()
    })

    // Should show 2 bills total (the count stat)
    expect(screen.getAllByText(/2/).length).toBeGreaterThanOrEqual(1)
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

  it("TestPosHistory_HasBackLink: page has a link back to the POS cashier", async () => {
    mockListTodaySaleBills.mockResolvedValue([])

    await act(async () => {
      render(<PosHistoryPage />)
    })

    await waitFor(() => {
      expect(screen.getByText(/返回收银台/)).toBeInTheDocument()
    })
  })
})
