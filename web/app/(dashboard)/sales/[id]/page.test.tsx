import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, fireEvent, waitFor } from "@testing-library/react"
import { ApiError } from "@/lib/api/errors"

const { push } = vi.hoisted(() => ({ push: vi.fn() }))
vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "sale-1" }),
  useRouter: () => ({ push }),
}))
vi.mock("next/link", () => ({
  default: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a>,
}))
vi.mock("@/hooks/useConfirm", () => ({ useConfirm: () => async () => true }))
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
vi.mock("@/components/sale-line-editor", () => ({ SaleLineEditor: () => <div /> }))
vi.mock("@/components/payment-form", () => ({
  PaymentForm: ({
    outstandingAmount,
    payments,
    onSuccess,
  }: {
    outstandingAmount: string
    payments: unknown[]
    onSuccess: () => void
  }) => (
    <div>
      <span data-testid="pf-outstanding">{outstandingAmount}</span>
      <span data-testid="pf-count">{payments.length}</span>
      <button onClick={() => onSuccess()}>mock-pay</button>
    </div>
  ),
}))
vi.mock("@/lib/api/customers", () => ({ getCustomer: vi.fn().mockRejectedValue(new Error("x")) }))
vi.mock("@/lib/api/suppliers", () => ({ getSupplier: vi.fn().mockRejectedValue(new Error("x")) }))
vi.mock("@/lib/api/warehouses", () => ({ getWarehouse: vi.fn().mockRejectedValue(new Error("x")) }))
vi.mock("@/lib/api/products", () => ({
  getProduct: vi.fn().mockResolvedValue({ id: "p-1", name: "雪松" }),
}))
vi.mock("@/lib/api/sale", () => ({
  getSaleBill: vi.fn(),
  approveSaleBill: vi.fn(),
  cancelSaleBill: vi.fn(),
  returnSaleBill: vi.fn(),
}))

import { getSaleBill, approveSaleBill, returnSaleBill } from "@/lib/api/sale"
import SaleDetailPage from "./page"
import { takeBillCopy } from "@/lib/bill-copy"
import { roleCan, type ShopRole } from "@/lib/roles"

const head = {
  id: "sale-1",
  bill_no: "SO-1",
  sub_type: "销售",
  status: 0,
  bill_date: "2026-01-01T00:00:00Z",
  total_amount: "100",
  paid_amount: "0",
  receivable_amount: "100",
}
const detail = (extra: object) => ({ head: { ...head, ...extra }, items: [], payments: [] }) as never

describe("SaleDetailPage header", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    roleState.role = "owner"
  })

  it("shows customer and warehouse names", async () => {
    vi.mocked(getSaleBill).mockResolvedValue(detail({ partner_name: "老张", warehouse_name: "门店仓" }))
    render(<SaleDetailPage />)
    expect(await screen.findByText("老张")).toBeInTheDocument()
    expect(screen.getByText("门店仓")).toBeInTheDocument()
  })

  it("labels an approved bill as locked with the agreed wording", async () => {
    vi.mocked(getSaleBill).mockResolvedValue(detail({ status: 2 }))
    render(<SaleDetailPage />)
    expect(await screen.findByText("已审核，无法修改")).toBeInTheDocument()
    expect(screen.queryByText(/不能再改/)).toBeNull()
  })

  it("opens the delivery note print page from the 打印 button", async () => {
    vi.mocked(getSaleBill).mockResolvedValue(detail({}))
    const open = vi.spyOn(window, "open").mockReturnValue(null)
    render(<SaleDetailPage />)
    fireEvent.click(await screen.findByRole("button", { name: "打印" }))
    expect(open).toHaveBeenCalledWith("/print/sale/sale-1", "_blank")
    open.mockRestore()
  })

  it("tells the user a draft without warehouse uses the default", async () => {
    vi.mocked(getSaleBill).mockResolvedValue(detail({}))
    render(<SaleDetailPage />)
    expect(await screen.findByText("未指定仓库，审核时将使用默认仓")).toBeInTheDocument()
  })

  it("does not say unspecified when the warehouse id is set but unnamed", async () => {
    vi.mocked(getSaleBill).mockResolvedValue(detail({ warehouse_id: "wh-1", partner_id: "c-1" }))
    render(<SaleDetailPage />)
    await screen.findByText("SO-1")
    expect(screen.queryByText(/未指定仓库/)).toBeNull()
    expect(screen.getAllByText("已指定").length).toBe(2)
  })

  it("shows the Chinese message when approval is refused", async () => {
    vi.mocked(getSaleBill).mockResolvedValue(detail({}))
    vi.mocked(approveSaleBill).mockRejectedValue(
      new ApiError(422, "paid_exceeds_total", "HTTP 422", { error: "paid_exceeds_total", message: "收款金额超过了单据总额" })
    )
    render(<SaleDetailPage />)
    fireEvent.click(await screen.findByText("审核出库"))
    fireEvent.click(await screen.findByText("确认审核"))
    expect(await screen.findByText("收款金额超过了单据总额")).toBeInTheDocument()
  })

  it("names the short product and links to stock when approval lacks stock", async () => {
    vi.mocked(getSaleBill).mockResolvedValue(detail({}))
    vi.mocked(approveSaleBill).mockRejectedValue(
      new ApiError(422, "insufficient_stock", "HTTP 422", {
        error: "insufficient_stock",
        message: "库存不足",
        details: [{ product_id: "p-1", available_qty: "2", requested_qty: "5" }],
      })
    )
    render(<SaleDetailPage />)
    fireEvent.click(await screen.findByText("审核出库"))
    fireEvent.click(await screen.findByText("确认审核"))
    expect(await screen.findByText("「雪松」库存不足：可用 2，本单需要 5")).toBeInTheDocument()
    expect(screen.getByText("去盘点或进货").closest("a")).toHaveAttribute("href", "/stock")
  })
})

describe("SaleDetailPage draft edit", () => {
  beforeEach(() => vi.clearAllMocks())

  it("opens the new-bill form in edit mode for a draft", async () => {
    vi.mocked(getSaleBill).mockResolvedValue(detail({}))
    render(<SaleDetailPage />)
    fireEvent.click(await screen.findByText("修改"))
    expect(push).toHaveBeenCalledWith("/sales/new?edit=sale-1")
  })

  it("offers no edit on an approved sale", async () => {
    vi.mocked(getSaleBill).mockResolvedValue({ head: { ...head, status: 2 }, items: [], payments: [] } as never)
    render(<SaleDetailPage />)
    await screen.findByText("SO-1")
    expect(screen.queryByText("修改")).toBeNull()
  })
})

describe("SaleDetailPage customer return", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    roleState.role = "owner"
  })

  const line = { id: "line-1", product_id: "p-1", line_no: 1, qty: "5", unit_price: "20", line_amount: "100", unit_name: "个" }
  const approved = (extra: object = {}, items: object[] = [line]) =>
    ({
      head: { ...head, status: 2, paid_amount: "100", receivable_amount: undefined },
      items,
      payments: [],
      receivable_amount: "0",
      ...extra,
    }) as never

  it("sends the picked quantity and shows the result", async () => {
    vi.mocked(getSaleBill).mockResolvedValue(approved())
    vi.mocked(returnSaleBill).mockResolvedValue({ bill_id: "r-1", bill_no: "RT-1", return_amount: "40" })
    render(<SaleDetailPage />)
    fireEvent.click(await screen.findByText("退货"))
    fireEvent.change(await screen.findByPlaceholderText("退几个"), { target: { value: "2" } })
    fireEvent.click(screen.getByText("确认退货"))
    expect(await screen.findByText(/退货成功，退货单号 RT-1/)).toBeInTheDocument()
    const open = vi.spyOn(window, "open").mockImplementation(() => null)
    fireEvent.click(screen.getByText("打印退货单"))
    expect(open).toHaveBeenCalledWith("/print/sale/r-1", "_blank")
    open.mockRestore()
    expect(returnSaleBill).toHaveBeenCalledWith(
      "sale-1",
      { items: [{ bill_item_id: "line-1", qty: "2" }], remark: undefined, refund_amount: undefined, refund_method: undefined },
      undefined,
      expect.any(String)
    )
  })

  it("hands money back with the goods when the clerk ticks the refund box", async () => {
    vi.mocked(getSaleBill).mockResolvedValue(approved({ payments: [{ id: "pay-1", pay_type: "wechat", amount: "100" }] }))
    vi.mocked(returnSaleBill).mockResolvedValue({ bill_id: "r-1", bill_no: "RT-1", return_amount: "40", refunded_amount: "40" })
    render(<SaleDetailPage />)
    fireEvent.click(await screen.findByText("退货"))
    fireEvent.change(await screen.findByPlaceholderText("退几个"), { target: { value: "2" } })
    fireEvent.click(await screen.findByLabelText(/同时退款/))
    // suggested amount = what was paid beyond the post-return total, paid back the way it was paid
    expect(screen.getByLabelText("退给客户多少钱")).toHaveValue(40)
    expect(screen.getByLabelText("退款方式")).toHaveValue("wechat")
    fireEvent.click(screen.getByText("确认退货"))
    expect(await screen.findByText(/已退款/)).toBeInTheDocument()
    expect(returnSaleBill).toHaveBeenCalledWith(
      "sale-1",
      expect.objectContaining({ refund_amount: "40.00", refund_method: "wechat" }),
      undefined,
      expect.any(String)
    )
  })

  it("does not let the refund exceed what the customer paid", async () => {
    vi.mocked(getSaleBill).mockResolvedValue(approved())
    render(<SaleDetailPage />)
    fireEvent.click(await screen.findByText("退货"))
    fireEvent.change(await screen.findByPlaceholderText("退几个"), { target: { value: "2" } })
    fireEvent.click(await screen.findByLabelText(/同时退款/))
    fireEvent.change(screen.getByLabelText("退给客户多少钱"), { target: { value: "41" } })
    expect(screen.getByText(/不能超过/)).toBeInTheDocument()
    expect(screen.getByText("确认退货").closest("button")).toBeDisabled()
  })

  it("offers no refund box on a credit sale nobody has paid yet", async () => {
    vi.mocked(getSaleBill).mockResolvedValue(
      approved({ head: { ...head, status: 2, paid_amount: "0" } })
    )
    render(<SaleDetailPage />)
    fireEvent.click(await screen.findByText("退货"))
    fireEvent.change(await screen.findByPlaceholderText("退几个"), { target: { value: "2" } })
    expect(screen.queryByLabelText(/同时退款/)).toBeNull()
  })

  it("warns when the goods went back but the refund was not booked", async () => {
    vi.mocked(getSaleBill).mockResolvedValue(approved())
    vi.mocked(returnSaleBill).mockResolvedValue({
      bill_id: "r-1",
      bill_no: "RT-1",
      return_amount: "40",
      refund_error: "货已退回入库，但退款没有记上，请到这张销售单的付款记录里手工补记",
    })
    render(<SaleDetailPage />)
    fireEvent.click(await screen.findByText("退货"))
    fireEvent.change(await screen.findByPlaceholderText("退几个"), { target: { value: "2" } })
    fireEvent.click(await screen.findByLabelText(/同时退款/))
    fireEvent.click(screen.getByText("确认退货"))
    expect(await screen.findByText(/退款没有记上/)).toBeInTheDocument()
  })

  it("lets the clerk book the refund afterwards, even after dismissing the prompt", async () => {
    window.localStorage.clear()
    vi.mocked(getSaleBill).mockResolvedValue(approved())
    vi.mocked(returnSaleBill)
      .mockResolvedValueOnce({ bill_id: "r-1", bill_no: "RT-1", return_amount: "40", refund_error: "货已退回入库，但退款没有记上，请点「补记退款」把这笔钱记上" })
      .mockResolvedValueOnce({ refunded_amount: "40" } as never)
    render(<SaleDetailPage />)
    fireEvent.click(await screen.findByText("退货"))
    fireEvent.change(await screen.findByPlaceholderText("退几个"), { target: { value: "2" } })
    fireEvent.click(await screen.findByLabelText(/同时退款/))
    fireEvent.click(screen.getByText("确认退货"))
    fireEvent.click(await screen.findByText("稍后再说"))
    // the entry stays on the page after the prompt is dismissed
    fireEvent.click(await screen.findByRole("button", { name: "补记退款" }))
    const buttons = await screen.findAllByRole("button", { name: "补记退款" })
    fireEvent.click(buttons[buttons.length - 1])
    await waitFor(() =>
      expect(returnSaleBill).toHaveBeenLastCalledWith(
        "sale-1",
        { items: [], refund_only: true, refund_amount: "40.00", refund_method: "cash", return_bill_id: "r-1" },
        undefined,
        expect.any(String)
      )
    )
    expect(await screen.findByText(/已补记退款/)).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "补记退款" })).toBeNull()
  })

  it("shows the Chinese reason when the server refuses", async () => {
    vi.mocked(getSaleBill).mockResolvedValue(approved())
    vi.mocked(returnSaleBill).mockRejectedValue(
      new ApiError(422, "return_exceeds_sold", "HTTP 422", { error: "return_exceeds_sold", message: "退货数量超过可退数量，第 1 行最多还能退 3" })
    )
    render(<SaleDetailPage />)
    fireEvent.click(await screen.findByText("退货"))
    fireEvent.change(await screen.findByPlaceholderText("退几个"), { target: { value: "2" } })
    fireEvent.click(screen.getByText("确认退货"))
    expect(await screen.findByText("退货数量超过可退数量，第 1 行最多还能退 3")).toBeInTheDocument()
  })

  it("blocks a quantity above what is left, without calling the server", async () => {
    vi.mocked(getSaleBill).mockResolvedValue(approved({}, [{ ...line, returned_qty: "2" }]))
    render(<SaleDetailPage />)
    fireEvent.click(await screen.findByText("退货"))
    fireEvent.change(await screen.findByPlaceholderText("退几个"), { target: { value: "4" } })
    expect(screen.getByText("不能超过 3")).toBeInTheDocument()
    expect(screen.getByText("确认退货").closest("button")).toBeDisabled()
  })

  it("hides the button on drafts and on fully returned sales", async () => {
    vi.mocked(getSaleBill).mockResolvedValue(detail({}))
    const { unmount } = render(<SaleDetailPage />)
    await screen.findByText("SO-1")
    expect(screen.queryByText("退货")).toBeNull()
    unmount()

    vi.mocked(getSaleBill).mockResolvedValue(approved({}, [{ ...line, returned_qty: "5" }]))
    render(<SaleDetailPage />)
    await screen.findByText("SO-1")
    expect(screen.queryByText("退货")).toBeNull()
    expect(screen.getByText(/已退 5个/)).toBeInTheDocument()
  })

  it("explains money owed back to the customer", async () => {
    vi.mocked(getSaleBill).mockResolvedValue(
      approved({ returned_amount: "40", refund_due_amount: "40", receivable_amount: "0" }, [{ ...line, returned_qty: "2" }])
    )
    render(<SaleDetailPage />)
    expect(await screen.findByText("应退给客户")).toBeInTheDocument()
    expect(screen.getByText(/系统不会自动退/)).toBeInTheDocument()
  })
})

describe("SaleDetailPage copy as new bill", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    sessionStorage.clear()
  })

  it("parks product names and the foreign currency, resolving names that are not loaded yet", async () => {
    vi.mocked(getSaleBill).mockResolvedValue({
      head: { ...head, status: 2, partner_id: "c-1", currency: "USD", exchange_rate: "7.2" },
      items: [{ id: "i1", product_id: "p-1", qty: "2", unit_price: "10", line_amount: "20", line_no: 1 }],
      payments: [],
    } as never)
    render(<SaleDetailPage />)
    fireEvent.click(await screen.findByText("复制为新单"))
    let copy: ReturnType<typeof takeBillCopy> = null
    await waitFor(() => {
      copy = takeBillCopy("sale")
      expect(copy).not.toBeNull()
    })
    expect(copy!.items[0].product_name).toBe("雪松")
    expect(copy!.currency).toBe("USD")
    expect(copy!.exchange_rate).toBe("7.2")
  })
})

describe("SaleDetailPage after a payment", () => {
  beforeEach(() => vi.clearAllMocks())

  const approvedUnpaid = {
    head: { ...head, status: 2, paid_amount: "0", receivable_amount: undefined },
    items: [],
    payments: [],
    receivable_amount: "100",
  } as never
  const approvedPartlyPaid = {
    head: { ...head, status: 2, paid_amount: "40", receivable_amount: undefined },
    items: [],
    payments: [{ id: "pay-1", bill_id: "sale-1", amount: "40", pay_type: "cash", pay_date: "2026-01-02T00:00:00Z" }],
    receivable_amount: "60",
  } as never

  it("re-reads the bill so paid, receivable and the payment list are right without a refresh", async () => {
    vi.mocked(getSaleBill).mockResolvedValueOnce(approvedUnpaid).mockResolvedValueOnce(approvedPartlyPaid)
    render(<SaleDetailPage />)
    expect(await screen.findByTestId("pf-outstanding")).toHaveTextContent("100")
    expect(screen.getByTestId("pf-count")).toHaveTextContent("0")

    fireEvent.click(screen.getByText("mock-pay"))

    await waitFor(() => expect(screen.getByTestId("pf-count")).toHaveTextContent("1"))
    expect(getSaleBill).toHaveBeenCalledTimes(2)
    expect(screen.getByTestId("pf-outstanding")).toHaveTextContent("60")
    expect(screen.getByText("¥40.00")).toBeInTheDocument()
    expect(screen.getByText("¥60.00")).toBeInTheDocument()
    expect(document.body.textContent).not.toContain("NaN")
  })

  it("keeps the bill on screen and shows a Chinese message when the re-read fails", async () => {
    vi.mocked(getSaleBill)
      .mockResolvedValueOnce(approvedUnpaid)
      .mockRejectedValueOnce(new ApiError(500, "internal_error", "HTTP 500", { message: "服务暂时不可用" }))
    render(<SaleDetailPage />)
    fireEvent.click(await screen.findByText("mock-pay"))
    expect(await screen.findByText("服务暂时不可用")).toBeInTheDocument()
    expect(screen.getByText("SO-1")).toBeInTheDocument()
  })
})

describe("SaleDetailPage by role", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    roleState.role = "owner"
  })
  const sold = {
    head: { ...head, status: 2, paid_amount: "100", receivable_amount: undefined },
    items: [{ id: "line-1", product_id: "p-1", line_no: 1, qty: "5", unit_price: "20", line_amount: "100", unit_name: "个" }],
    payments: [],
    receivable_amount: "0",
  } as never

  it("keeps approve and cancel from a clerk but leaves the draft editable", async () => {
    roleState.role = "clerk"
    vi.mocked(getSaleBill).mockResolvedValue(detail({}))
    render(<SaleDetailPage />)
    await screen.findByText("修改")
    expect(screen.queryByText("审核出库")).toBeNull()
    expect(screen.queryByText("取消单据")).toBeNull()
  })

  it("gives a finance person a read-only draft", async () => {
    roleState.role = "finance"
    vi.mocked(getSaleBill).mockResolvedValue(detail({}))
    render(<SaleDetailPage />)
    await screen.findByText("SO-1")
    expect(screen.queryByText("修改")).toBeNull()
    expect(screen.queryByText("审核出库")).toBeNull()
  })

  it("hides return from a cashier and shows it to a manager", async () => {
    roleState.role = "cashier"
    vi.mocked(getSaleBill).mockResolvedValue(sold)
    const { unmount } = render(<SaleDetailPage />)
    await screen.findByText("SO-1")
    expect(screen.queryByText("退货")).toBeNull()
    unmount()

    roleState.role = "manager"
    render(<SaleDetailPage />)
    expect(await screen.findByText("退货")).toBeInTheDocument()
  })

  it("keeps the invoice block from people who cannot read payments", async () => {
    vi.mocked(getSaleBill).mockResolvedValue(sold)
    roleState.role = "clerk"
    const { unmount } = render(<SaleDetailPage />)
    await screen.findByText("SO-1")
    expect(screen.queryByText("关联发票")).toBeNull()
    unmount()

    roleState.role = "finance"
    render(<SaleDetailPage />)
    expect(await screen.findByText("关联发票")).toBeInTheDocument()
  })

  it("shows the approve button to the owner", async () => {
    roleState.role = "owner"
    vi.mocked(getSaleBill).mockResolvedValue(detail({}))
    render(<SaleDetailPage />)
    expect(await screen.findByText("审核出库")).toBeInTheDocument()
  })
})

describe("SaleDetailPage entries match the role's permissions", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    roleState.role = "owner"
  })
  const sold = {
    head: { ...head, status: 2, paid_amount: "100", receivable_amount: "0" },
    items: [{ id: "line-1", product_id: "p-1", line_no: 1, qty: "5", unit_price: "20", line_amount: "100", unit_name: "个" }],
    payments: [],
    receivable_amount: "0",
  } as never

  it.each(["owner", "manager", "clerk", "cashier", "finance"] as const)("%s sees only the entries the backend accepts on a draft", async (role: ShopRole) => {
    roleState.role = role
    vi.mocked(getSaleBill).mockResolvedValue(detail({}))
    render(<SaleDetailPage />)
    await screen.findByText("SO-1")
    expect(!!screen.queryByText("复制为新单")).toBe(roleCan(role, "bill.draft") || roleCan(role, "bill.checkout"))
    expect(!!screen.queryByText("修改")).toBe(roleCan(role, "bill.draft"))
    expect(!!screen.queryByText("取消单据")).toBe(roleCan(role, "bill.cancel"))
    expect(!!screen.queryByText("审核出库")).toBe(roleCan(role, "bill.approve"))
  })

  it.each(["owner", "manager", "clerk", "cashier", "finance"] as const)("%s sees only the entries the backend accepts on an approved sale", async (role: ShopRole) => {
    roleState.role = role
    vi.mocked(getSaleBill).mockResolvedValue(sold)
    render(<SaleDetailPage />)
    await screen.findByText("SO-1")
    expect(!!screen.queryByText("复制为新单")).toBe(roleCan(role, "bill.draft") || roleCan(role, "bill.checkout"))
    expect(!!screen.queryByText("退货")).toBe(roleCan(role, "bill.return"))
  })
})
