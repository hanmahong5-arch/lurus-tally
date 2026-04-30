import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor, fireEvent } from "@testing-library/react"
import { globalUndoStack } from "@/lib/undo/undo-stack"

// Mock next/navigation
vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "bill-abc-123" }),
  useRouter: () => ({ push: vi.fn() }),
}))

// Mock next/link
vi.mock("next/link", () => ({
  default: ({
    href,
    children,
    ...props
  }: {
    href: string
    children: React.ReactNode
    [key: string]: unknown
  }) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}))

// Mock the bill line editor
vi.mock("@/components/bill-line-editor", () => ({
  BillLineEditor: () => <div data-testid="bill-line-editor" />,
}))

// Mock the purchase API
vi.mock("@/lib/api/purchase", () => ({
  getPurchaseBill: vi.fn(),
  approvePurchaseBill: vi.fn(),
  cancelPurchaseBill: vi.fn(),
  restorePurchaseBill: vi.fn(),
  BILL_STATUS_LABEL: { 0: "草稿", 2: "已审核", 9: "已取消" },
}))

import {
  getPurchaseBill,
  cancelPurchaseBill,
  restorePurchaseBill,
} from "@/lib/api/purchase"
import PurchaseDetailPage from "./page"

const mockDraftBill = {
  head: {
    id: "bill-abc-123",
    tenant_id: "tenant-1",
    bill_no: "PO-20260101-0001",
    bill_type: "入库",
    sub_type: "采购",
    status: 0 as const,
    creator_id: "creator-1",
    bill_date: "2026-01-01T00:00:00Z",
    subtotal: "100",
    shipping_fee: "0",
    tax_amount: "0",
    total_amount: "100",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  },
  items: [],
}

describe("PurchaseDetailPage undo-aware cancel", () => {
  beforeEach(() => {
    globalUndoStack.resetForTest()
    vi.mocked(getPurchaseBill).mockResolvedValue(mockDraftBill)
    vi.mocked(cancelPurchaseBill).mockResolvedValue(undefined)
    vi.mocked(restorePurchaseBill).mockResolvedValue(undefined)
  })

  it("TestPurchaseCancel_PushesUndoStack", async () => {
    render(<PurchaseDetailPage />)

    await waitFor(() => {
      expect(screen.getByText("取消单据")).toBeInTheDocument()
    })

    expect(globalUndoStack.size()).toBe(0)

    fireEvent.click(screen.getByText("取消单据"))

    // Entry should be pushed before the async cancel call resolves
    expect(globalUndoStack.size()).toBe(1)

    await waitFor(() => {
      expect(cancelPurchaseBill).toHaveBeenCalled()
    })
  })

  it("TestPurchaseCancel_Undo_CallsRestoreEndpoint", async () => {
    render(<PurchaseDetailPage />)

    await waitFor(() => {
      expect(screen.getByText("取消单据")).toBeInTheDocument()
    })

    fireEvent.click(screen.getByText("取消单据"))

    await waitFor(() => {
      expect(globalUndoStack.size()).toBe(1)
    })

    const entry = globalUndoStack.pop()
    expect(entry).toBeDefined()
    expect(entry?.action.type).toBe("cancel_purchase")

    await entry!.action.revert()

    expect(restorePurchaseBill).toHaveBeenCalledWith("bill-abc-123", undefined)
  })
})
