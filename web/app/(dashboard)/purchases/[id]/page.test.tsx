import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor, fireEvent } from "@testing-library/react"
import { ApiError } from "@/lib/api/errors"
import { globalUndoStack } from "@/lib/undo/undo-stack"

// Mock next/navigation
const { routerPush } = vi.hoisted(() => ({ routerPush: vi.fn() }))
vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "bill-abc-123" }),
  useRouter: () => ({ push: routerPush }),
}))
vi.mock("@/lib/api/products", () => ({
  getProduct: vi.fn().mockResolvedValue({ id: "p-1", name: "红枫" }),
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

// Confirm dialogs auto-accept.
const { confirmSpy } = vi.hoisted(() => ({ confirmSpy: vi.fn<(opts: { body?: string }) => Promise<boolean | null>>(async () => true) }))
vi.mock("@/hooks/useConfirm", () => ({ useConfirm: () => confirmSpy }))
// Role under test; null means nobody is signed in.
const { roleState } = vi.hoisted(() => ({ roleState: { role: "owner" as string | null } }))
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

// Mock the bill line editor
vi.mock("@/components/bill-line-editor", () => ({
  BillLineEditor: ({ items }: { items: { _subtotal?: string }[] }) => (
    <div data-testid="bill-line-editor">
      {items.reduce((a, it) => a + (parseFloat(it._subtotal ?? "0") || 0), 0)}
    </div>
  ),
}))

// Mock the purchase API
vi.mock("@/lib/api/suppliers", () => ({ getSupplier: vi.fn().mockRejectedValue(new Error("x")) }))
vi.mock("@/lib/api/customers", () => ({ getCustomer: vi.fn().mockRejectedValue(new Error("x")) }))
vi.mock("@/lib/api/warehouses", () => ({ getWarehouse: vi.fn().mockRejectedValue(new Error("x")) }))

vi.mock("@/lib/api/payment", () => ({
  listPayments: vi.fn().mockResolvedValue([]),
  recordPayment: vi.fn(),
  PAY_TYPE_LABEL: { cash: "现金", transfer: "转账" },
}))

vi.mock("@/lib/api/purchase", () => ({
  getPurchaseBill: vi.fn(),
  approvePurchaseBill: vi.fn(),
  cancelPurchaseBill: vi.fn(),
  restorePurchaseBill: vi.fn(),
  returnPurchaseBill: vi.fn(),
  BILL_STATUS_LABEL: { 0: "草稿", 2: "已审核", 9: "已取消" },
}))

import {
  getPurchaseBill,
  approvePurchaseBill,
  cancelPurchaseBill,
  restorePurchaseBill,
  returnPurchaseBill,
} from "@/lib/api/purchase"
import { listPayments, recordPayment } from "@/lib/api/payment"
import { takeBillCopy } from "@/lib/bill-copy"
import PurchaseDetailPage from "./page"
import { roleCan, type ShopRole } from "@/lib/roles"

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

describe("PurchaseDetailPage header", () => {
  beforeEach(() => {
    globalUndoStack.resetForTest()
  })

  it("shows supplier and warehouse names", async () => {
    vi.mocked(getPurchaseBill).mockResolvedValue({
      ...mockDraftBill,
      head: { ...mockDraftBill.head, partner_name: "绿源苗圃", warehouse_name: "二号仓" },
    })
    render(<PurchaseDetailPage />)
    expect(await screen.findByText("绿源苗圃")).toBeInTheDocument()
    expect(screen.getByText("二号仓")).toBeInTheDocument()
  })

  it("tells the user when a draft has no warehouse", async () => {
    vi.mocked(getPurchaseBill).mockResolvedValue(mockDraftBill)
    render(<PurchaseDetailPage />)
    expect(await screen.findByText("未指定仓库，审核时将使用默认仓")).toBeInTheDocument()
  })

  it("does not claim a draft has no warehouse when one is set but unnamed", async () => {
    vi.mocked(getPurchaseBill).mockResolvedValue({
      ...mockDraftBill,
      head: { ...mockDraftBill.head, warehouse_id: "wh-1", partner_id: "sup-1" },
    })
    render(<PurchaseDetailPage />)
    await screen.findByText("PO-20260101-0001")
    expect(screen.queryByText(/未指定仓库/)).toBeNull()
    expect(screen.queryByText("未指定")).toBeNull()
    expect(screen.getAllByText("已指定").length).toBe(2)
  })

  it("resolves names from ids when the server omits them", async () => {
    const { getSupplier } = await import("@/lib/api/suppliers")
    const { getWarehouse } = await import("@/lib/api/warehouses")
    vi.mocked(getSupplier).mockResolvedValueOnce({ name: "绿源苗圃" } as never)
    vi.mocked(getWarehouse).mockResolvedValueOnce({ name: "二号仓" } as never)
    vi.mocked(getPurchaseBill).mockResolvedValue({
      ...mockDraftBill,
      head: { ...mockDraftBill.head, warehouse_id: "wh-1", partner_id: "sup-1" },
    })
    render(<PurchaseDetailPage />)
    expect(await screen.findByText("绿源苗圃")).toBeInTheDocument()
    expect(await screen.findByText("二号仓")).toBeInTheDocument()
  })

  it("shows the server's Chinese message when approval is refused", async () => {
    vi.mocked(getPurchaseBill).mockResolvedValue(mockDraftBill)
    vi.mocked(approvePurchaseBill).mockRejectedValue(
      new ApiError(422, "warehouse_required", "HTTP 422", { error: "warehouse_required", message: "请先选择仓库" })
    )
    render(<PurchaseDetailPage />)
    fireEvent.click(await screen.findByText("审核通过"))
    expect(await screen.findByText("请先选择仓库")).toBeInTheDocument()
  })
})

describe("PurchaseDetailPage line subtotals", () => {
  it("passes each line_amount to the editor so the total matches the bill", async () => {
    vi.mocked(getPurchaseBill).mockResolvedValue({
      ...mockDraftBill,
      items: [
        { id: "i1", product_id: "p1", unit_id: "u1", unit_name: "个", line_no: 1, qty: "2", unit_price: "30", line_amount: "60" },
        { id: "i2", product_id: "p2", unit_id: "u1", unit_name: "个", line_no: 2, qty: "1", unit_price: "40", line_amount: "40" },
      ],
    } as never)
    render(<PurchaseDetailPage />)
    await waitFor(() => {
      expect(screen.getByTestId("bill-line-editor")).toHaveTextContent("100")
    })
  })
})

describe("PurchaseDetailPage copy as new bill", () => {
  beforeEach(() => {
    routerPush.mockClear()
    sessionStorage.clear()
  })

  it("parks the bill content, resolving product names, and opens the new purchase page", async () => {
    vi.mocked(getPurchaseBill).mockResolvedValue({
      head: { ...mockDraftBill.head, status: 2 as const, partner_id: "s-1", warehouse_id: "wh-1", remark: "加急", shipping_fee: "5", tax_amount: "2" },
      items: [{ id: "i1", product_id: "p-1", qty: "2", unit_price: "42", unit_id: "u-box", unit_name: "箱", line_amount: "84", line_no: 1 }],
    } as never)
    render(<PurchaseDetailPage />)
    fireEvent.click(await screen.findByText("复制为新单"))
    await waitFor(() => expect(routerPush).toHaveBeenCalledWith("/purchases/new"))
    const copy = takeBillCopy("purchase")
    expect(copy).toMatchObject({ partner_id: "s-1", warehouse_id: "wh-1", remark: "加急", shipping_fee: "5", tax_amount: "2" })
    expect(copy!.items).toEqual([
      { product_id: "p-1", product_name: "红枫", unit_id: "u-box", unit_name: "箱", qty: "2", unit_price: "42" },
    ])
  })
})

describe("PurchaseDetailPage payment", () => {
  beforeEach(() => {
    roleState.role = "owner"
  })

  const approvedBill = (paid: string) => ({
    ...mockDraftBill,
    head: { ...mockDraftBill.head, status: 2 as const, paid_amount: paid },
  })

  it("does not ask for payment records unless the role allows it", async () => {
    vi.mocked(listPayments).mockClear()
    vi.mocked(getPurchaseBill).mockResolvedValue(approvedBill("0"))
    for (const role of ["clerk"]) {
      roleState.role = role
      const { unmount } = render(<PurchaseDetailPage />)
      await screen.findByText(mockDraftBill.head.bill_no)
      expect(listPayments).not.toHaveBeenCalled()
      unmount()
    }
  })

  it("pays at approval when an amount is filled in, and approves alone when it is not", async () => {
    vi.mocked(getPurchaseBill).mockResolvedValue(mockDraftBill)
    vi.mocked(approvePurchaseBill).mockResolvedValue({} as never)
    const { unmount } = render(<PurchaseDetailPage />)
    fireEvent.change(await screen.findByLabelText(/本次付款/), { target: { value: "100" } })
    fireEvent.click(screen.getByText("审核通过"))
    await waitFor(() =>
      expect(approvePurchaseBill).toHaveBeenCalledWith("bill-abc-123", undefined, {
        paid_amount: "100",
        payment_method: "transfer",
      }),
    )
    unmount()

    vi.mocked(approvePurchaseBill).mockClear()
    render(<PurchaseDetailPage />)
    fireEvent.click(await screen.findByText("审核通过"))
    await waitFor(() => expect(approvePurchaseBill).toHaveBeenCalledWith("bill-abc-123", undefined, undefined))
  })

  it("records a payment on an approved bill and shows the new paid and payable figures", async () => {
    vi.mocked(getPurchaseBill)
      .mockResolvedValueOnce(approvedBill("0") as never)
      .mockResolvedValueOnce(approvedBill("30") as never)
    vi.mocked(listPayments)
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([
        { id: "p1", bill_id: "bill-abc-123", amount: "30", pay_type: "transfer", pay_date: "2026-01-02T00:00:00Z" },
      ])
    vi.mocked(recordPayment).mockResolvedValue({} as never)
    render(<PurchaseDetailPage />)

    expect(await screen.findByText("登记付款")).toBeInTheDocument()
    expect(screen.getByText("待付金额")).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText("金额"), { target: { value: "30" } })
    fireEvent.click(screen.getByText("确认付款"))

    await waitFor(() =>
      expect(recordPayment).toHaveBeenCalledWith(
        { bill_id: "bill-abc-123", amount: "30", payment_method: "cash", remark: undefined },
        undefined,
      ),
    )
    expect(await screen.findByText("¥30.00", { selector: "td" })).toBeInTheDocument()
    expect(document.body.textContent).not.toContain("NaN")
  })
})

describe("PurchaseDetailPage draft edit", () => {
  beforeEach(() => routerPush.mockClear())

  it("opens the new-bill form in edit mode for a draft", async () => {
    vi.mocked(getPurchaseBill).mockResolvedValue(mockDraftBill)
    render(<PurchaseDetailPage />)
    fireEvent.click(await screen.findByText("修改"))
    expect(routerPush).toHaveBeenCalledWith("/purchases/new?edit=bill-abc-123")
  })

  it("offers no edit on an approved bill", async () => {
    vi.mocked(getPurchaseBill).mockResolvedValue({ ...mockDraftBill, head: { ...mockDraftBill.head, status: 2 as const } })
    render(<PurchaseDetailPage />)
    await screen.findByText("PO-20260101-0001")
    expect(screen.queryByText("修改")).toBeNull()
  })
})

describe("PurchaseDetailPage return to supplier", () => {
  beforeEach(() => {
    roleState.role = "owner"
  })

  const line = { id: "line-1", product_id: "p-1", line_no: 1, qty: "10", unit_price: "8", line_amount: "80", unit_name: "株" }
  const approved = (items: object[] = [line], paid = "0") =>
    ({ head: { ...mockDraftBill.head, status: 2 as const, total_amount: "80", paid_amount: paid }, items }) as never

  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(listPayments).mockResolvedValue([])
  })

  it("sends the picked quantity with an idempotency key and says what happened to the payable", async () => {
    vi.mocked(getPurchaseBill).mockResolvedValue(approved())
    vi.mocked(returnPurchaseBill).mockResolvedValue({
      bill_id: "r-1",
      bill_no: "PR-1",
      return_amount: "24",
      payable_reduced: "24",
      supplier_owes: "0",
    })
    render(<PurchaseDetailPage />)
    fireEvent.click(await screen.findByText("退货"))
    fireEvent.change(await screen.findByPlaceholderText("退几个"), { target: { value: "3" } })
    fireEvent.click(screen.getByText("确认退货"))
    expect(await screen.findByText(/退货成功，退货单号 PR-1/)).toBeInTheDocument()
    expect(screen.getByText(/欠供应商的钱减少/)).toBeInTheDocument()
    const open = vi.spyOn(window, "open").mockImplementation(() => null)
    fireEvent.click(screen.getByText("打印退货单"))
    expect(open).toHaveBeenCalledWith("/print/purchase/r-1", "_blank")
    open.mockRestore()
    expect(returnPurchaseBill).toHaveBeenCalledWith(
      "bill-abc-123",
      { items: [{ bill_item_id: "line-1", qty: "3" }], remark: undefined },
      undefined,
      expect.any(String)
    )
  })

  it("tells the clerk when the supplier owes money back", async () => {
    vi.mocked(getPurchaseBill).mockResolvedValue(approved([line], "80"))
    vi.mocked(returnPurchaseBill).mockResolvedValue({
      bill_id: "r-1",
      bill_no: "PR-1",
      return_amount: "24",
      payable_reduced: "0",
      supplier_owes: "24",
    })
    render(<PurchaseDetailPage />)
    fireEvent.click(await screen.findByText("退货"))
    fireEvent.change(await screen.findByPlaceholderText("退几个"), { target: { value: "3" } })
    fireEvent.click(screen.getByText("确认退货"))
    expect(await screen.findByText(/供应商还应退给你/)).toBeInTheDocument()
  })

  it("shows the Chinese reason when the stock is not enough", async () => {
    vi.mocked(getPurchaseBill).mockResolvedValue(approved())
    vi.mocked(returnPurchaseBill).mockRejectedValue(
      new ApiError(422, "insufficient_stock", "HTTP 422", { error: "insufficient_stock", message: "库存不够退，这批货可能已经卖出去了一部分" })
    )
    render(<PurchaseDetailPage />)
    fireEvent.click(await screen.findByText("退货"))
    fireEvent.change(await screen.findByPlaceholderText("退几个"), { target: { value: "3" } })
    fireEvent.click(screen.getByText("确认退货"))
    expect(await screen.findByText("库存不够退，这批货可能已经卖出去了一部分")).toBeInTheDocument()
  })

  it("blocks a quantity above what is left, and hides the button once everything went back", async () => {
    vi.mocked(getPurchaseBill).mockResolvedValue(approved([{ ...line, returned_qty: "4" }]))
    const { unmount } = render(<PurchaseDetailPage />)
    fireEvent.click(await screen.findByText("退货"))
    fireEvent.change(await screen.findByPlaceholderText("退几个"), { target: { value: "7" } })
    expect(screen.getByText("不能超过 6")).toBeInTheDocument()
    expect(screen.getByText("确认退货").closest("button")).toBeDisabled()
    unmount()

    vi.mocked(getPurchaseBill).mockResolvedValue(approved([{ ...line, returned_qty: "10" }]))
    render(<PurchaseDetailPage />)
    await screen.findByText("退货情况")
    expect(screen.queryByText("退货")).toBeNull()
  })

  it("separates the return offset from the money actually paid", async () => {
    vi.mocked(getPurchaseBill).mockResolvedValue(approved([line], "24"))
    vi.mocked(listPayments).mockResolvedValue([
      { id: "p1", bill_id: "bill-abc-123", amount: "24", pay_type: "credit", pay_date: "2026-01-02T00:00:00Z", remark: "采购退货冲抵 PR-1" },
    ] as never)
    render(<PurchaseDetailPage />)
    expect(await screen.findAllByText("退货冲抵")).toHaveLength(2)
  })

  it("shows a return to a supplier read-only, without return, payment or copy actions", async () => {
    const doc = approved([line]) as unknown as { head: object; items: object[] }
    vi.mocked(getPurchaseBill).mockResolvedValue({ ...doc, head: { ...doc.head, sub_type: "采购退货" } } as never)
    render(<PurchaseDetailPage />)
    expect(await screen.findByText("退货单，仅供查看")).toBeInTheDocument()
    expect(screen.queryByText("退货")).toBeNull()
    expect(screen.queryByText("复制为新单")).toBeNull()
    expect(screen.queryByText("付款记录")).toBeNull()
    expect(screen.queryByText("待付")).toBeNull()
  })
})

describe("PurchaseDetailPage attention to money", () => {
  beforeEach(() => {
    roleState.role = "owner"
  })

  const approvedBill = (paid: string) => ({
    ...mockDraftBill,
    head: { ...mockDraftBill.head, status: 2 as const, paid_amount: paid },
  })

  beforeEach(() => {
    confirmSpy.mockClear()
    vi.mocked(listPayments).mockResolvedValue([])
  })

  it("the approval confirm says how much is paid at the same time, and by what", async () => {
    vi.mocked(getPurchaseBill).mockResolvedValue(mockDraftBill)
    vi.mocked(approvePurchaseBill).mockResolvedValue({} as never)
    render(<PurchaseDetailPage />)
    fireEvent.change(await screen.findByLabelText(/本次付款/), { target: { value: "100" } })
    fireEvent.change(screen.getByLabelText("付款方式"), { target: { value: "cash" } })
    fireEvent.click(screen.getByText("审核通过"))
    await waitFor(() => expect(confirmSpy).toHaveBeenCalled())
    expect(confirmSpy.mock.calls[0][0].body).toContain("审核时将同时登记付款 ¥100.00（现金）")
  })

  it("the approval confirm mentions no payment when none is filled in", async () => {
    vi.mocked(getPurchaseBill).mockResolvedValue(mockDraftBill)
    render(<PurchaseDetailPage />)
    fireEvent.click(await screen.findByText("审核通过"))
    await waitFor(() => expect(confirmSpy).toHaveBeenCalled())
    expect(confirmSpy.mock.calls[0][0].body).not.toContain("登记付款")
  })

  it("does not paint a paid amount of zero green", async () => {
    vi.mocked(getPurchaseBill).mockResolvedValue(approvedBill("0") as never)
    render(<PurchaseDetailPage />)
    const label = await screen.findByText("已付")
    expect(label.nextElementSibling?.className).not.toContain("text-success")
  })

  it("paints a real payment green", async () => {
    vi.mocked(getPurchaseBill).mockResolvedValue(approvedBill("30") as never)
    render(<PurchaseDetailPage />)
    const label = await screen.findByText("已付")
    expect(label.nextElementSibling?.className).toContain("text-success")
  })

  it("says so when the payment records could not be read", async () => {
    vi.mocked(getPurchaseBill).mockResolvedValue(approvedBill("30") as never)
    vi.mocked(listPayments).mockRejectedValue(new Error("down"))
    render(<PurchaseDetailPage />)
    expect(await screen.findByText(/付款记录暂时没有读到/)).toBeInTheDocument()
  })
})

describe("PurchaseDetailPage for a cashier", () => {
  it("sends no request and shows no permission error", async () => {
    vi.mocked(getPurchaseBill).mockClear()
    vi.mocked(listPayments).mockClear()
    roleState.role = "cashier"
    render(<PurchaseDetailPage />)
    expect(await screen.findByText("你没有查看采购单的权限")).toBeInTheDocument()
    expect(getPurchaseBill).not.toHaveBeenCalled()
    expect(listPayments).not.toHaveBeenCalled()
    expect(screen.queryByText(/权限不足/)).toBeNull()
    roleState.role = "owner"
  })
})

describe("PurchaseDetailPage by role", () => {
  beforeEach(() => {
    vi.mocked(getPurchaseBill).mockResolvedValue(mockDraftBill)
    roleState.role = "owner"
  })

  it("keeps approve and the pay-at-approval block from a finance person", async () => {
    roleState.role = "finance"
    render(<PurchaseDetailPage />)
    await screen.findByText(mockDraftBill.head.bill_no)
    expect(screen.queryByText("审核通过")).toBeNull()
    expect(screen.queryByText("取消单据")).toBeNull()
    expect(screen.queryByText("修改")).toBeNull()
    expect(screen.queryByText("审核时同时付款（可选）")).toBeNull()
  })

  it("lets a clerk edit the draft but not approve or cancel it", async () => {
    roleState.role = "clerk"
    render(<PurchaseDetailPage />)
    expect(await screen.findByText("修改")).toBeInTheDocument()
    expect(screen.queryByText("审核通过")).toBeNull()
    expect(screen.queryByText("取消单据")).toBeNull()
  })

  it("shows a clerk products and quantities only: no amount, price, copy or print", async () => {
    roleState.role = "clerk"
    const { container } = render(<PurchaseDetailPage />)
    await screen.findByText("修改")
    expect(screen.queryByText("总金额")).toBeNull()
    expect(screen.queryByText("单价")).toBeNull()
    expect(screen.queryByText("小计")).toBeNull()
    expect(screen.queryByText("含税总额")).toBeNull()
    expect(screen.queryByText("打印")).toBeNull()
    expect(screen.queryByText("复制为新单")).toBeNull()
    expect(container.textContent).not.toMatch(/¥/)
  })

  it("shows a clerk their own approved bill with an approved tag and no money", async () => {
    roleState.role = "clerk"
    vi.mocked(getPurchaseBill).mockResolvedValue({
      ...mockDraftBill,
      head: { ...mockDraftBill.head, status: 2 },
    } as never)
    const { container } = render(<PurchaseDetailPage />)
    await screen.findByText(mockDraftBill.head.bill_no)
    expect(screen.getByText("已审核")).toBeInTheDocument()
    expect(screen.queryByText("审核通过")).toBeNull()
    expect(screen.queryByText("修改")).toBeNull()
    expect(screen.queryByText("总金额")).toBeNull()
    expect(container.textContent).not.toMatch(/¥/)
  })

  it("shows everything to a manager", async () => {
    roleState.role = "manager"
    render(<PurchaseDetailPage />)
    expect(await screen.findByText("审核通过")).toBeInTheDocument()
    expect(screen.getByText("审核时同时付款（可选）")).toBeInTheDocument()
  })
})

describe("PurchaseDetailPage entries match the role's permissions", () => {
  beforeEach(() => {
    vi.mocked(getPurchaseBill).mockResolvedValue(mockDraftBill)
    roleState.role = "owner"
  })

  it.each(["owner", "manager", "clerk", "cashier", "finance"] as const)("%s sees only the entries the backend accepts on a draft", async (role: ShopRole) => {
    roleState.role = role
    render(<PurchaseDetailPage />)
    if (!roleCan(role, "purchase.view")) {
      // Not a role that reads purchases; the page is not reachable, nothing to assert.
      return
    }
    await screen.findByText(mockDraftBill.head.bill_no)
    // Copying needs cost.view as well: a price-blind clerk never gets the button.
    expect(!!screen.queryByText("复制为新单")).toBe(roleCan(role, "bill.draft") && roleCan(role, "cost.view"))
    expect(!!screen.queryByText("修改")).toBe(roleCan(role, "bill.draft"))
    expect(!!screen.queryByText("取消单据")).toBe(roleCan(role, "bill.cancel") && roleCan(role, "cost.view"))
    expect(!!screen.queryByText("审核通过")).toBe(roleCan(role, "bill.approve") && roleCan(role, "cost.view"))
  })
})

describe("PurchaseDetailPage zero purchase price", () => {
  const zeroBill = {
    ...mockDraftBill,
    items: [
      { id: "i1", product_id: "p1", unit_id: "u1", unit_name: "个", line_no: 1, qty: "2", unit_price: "30", line_amount: "60" },
      { id: "i2", product_id: "p2", unit_id: "u1", unit_name: "个", line_no: 2, qty: "1", unit_price: "0", line_amount: "0" },
    ],
  }

  beforeEach(() => {
    roleState.role = "owner"
    confirmSpy.mockClear()
    confirmSpy.mockResolvedValue(true)
    routerPush.mockClear()
    vi.mocked(getPurchaseBill).mockReset()
    vi.mocked(approvePurchaseBill).mockReset()
    vi.mocked(approvePurchaseBill).mockResolvedValue({} as never)
  })

  it("shows how many lines still need a purchase price", async () => {
    vi.mocked(getPurchaseBill).mockResolvedValue(zeroBill as never)
    render(<PurchaseDetailPage />)
    expect(await screen.findByText(/待补进货价 1 行/)).toBeInTheDocument()
  })

  it("shows no banner when every line is priced", async () => {
    vi.mocked(getPurchaseBill).mockResolvedValue({
      ...zeroBill,
      items: [zeroBill.items[0]],
    } as never)
    render(<PurchaseDetailPage />)
    await screen.findByText("审核通过")
    expect(screen.queryByText(/待补进货价/)).not.toBeInTheDocument()
  })

  it("approving with a zero-price line asks about gifts and sends allow_zero_price once confirmed", async () => {
    vi.mocked(getPurchaseBill).mockResolvedValue(zeroBill as never)
    render(<PurchaseDetailPage />)
    fireEvent.click(await screen.findByText("审核通过"))
    await waitFor(() =>
      expect(approvePurchaseBill).toHaveBeenCalledWith("bill-abc-123", undefined, { allow_zero_price: true }),
    )
    expect(confirmSpy).toHaveBeenCalledWith(expect.objectContaining({ confirmText: "确认是赠品，按 0 入账", distinguishDismiss: true }))
  })

  it("choosing to modify sends the user to the edit page without approving", async () => {
    confirmSpy.mockResolvedValue(false)
    vi.mocked(getPurchaseBill).mockResolvedValue(zeroBill as never)
    render(<PurchaseDetailPage />)
    fireEvent.click(await screen.findByText("审核通过"))
    await waitFor(() => expect(routerPush).toHaveBeenCalledWith("/purchases/new?edit=bill-abc-123"))
    expect(approvePurchaseBill).not.toHaveBeenCalled()
  })

  it("dismissing the gift dialog (Esc) neither navigates nor approves", async () => {
    confirmSpy.mockResolvedValue(null)
    vi.mocked(getPurchaseBill).mockResolvedValue(zeroBill as never)
    render(<PurchaseDetailPage />)
    fireEvent.click(await screen.findByText("审核通过"))
    await waitFor(() => expect(confirmSpy).toHaveBeenCalled())
    await new Promise((r) => setTimeout(r, 0))
    expect(routerPush).not.toHaveBeenCalled()
    expect(approvePurchaseBill).not.toHaveBeenCalled()
  })

  it("the gift dialog body ends with the payment note when a payment is filled in", async () => {
    vi.mocked(getPurchaseBill).mockResolvedValue(zeroBill as never)
    render(<PurchaseDetailPage />)
    fireEvent.change(await screen.findByLabelText(/本次付款/), { target: { value: "100" } })
    fireEvent.click(screen.getByText("审核通过"))
    await waitFor(() => expect(confirmSpy).toHaveBeenCalled())
    const body = confirmSpy.mock.calls[0][0].body ?? ""
    expect(body).toContain("成本会记为 0")
    expect(body).toMatch(/审核时将同时登记付款 ¥100\.00（.+）。$/)
  })
})
