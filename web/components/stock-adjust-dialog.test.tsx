import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, fireEvent, waitFor } from "@testing-library/react"

vi.mock("@/lib/api/stock", () => ({ adjustStock: vi.fn() }))
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

const { roleState } = vi.hoisted(() => ({ roleState: { role: "owner" as string } }))
vi.mock("next-auth/react", () => ({ useSession: () => ({ data: { user: { role: roleState.role } } }) }))

import { adjustStock } from "@/lib/api/stock"
import { ApiError } from "@/lib/api/errors"
import { StockAdjustDialog, formatQty, isValidCount } from "./stock-adjust-dialog"

const product = (id: string, name: string) =>
  ({ id, name, code: "C-" + id, enabled: true, measurement_strategy: "individual" }) as never

const snap = (product_id: string, warehouse_id: string, qty: string) =>
  ({ product_id, warehouse_id, on_hand_qty: qty, available_qty: qty, unit_cost: "1" }) as never

const props = () => ({
  open: true,
  onOpenChange: vi.fn(),
  products: [product("p1", "不锈钢螺丝"), product("p2", "水管")],
  snapshots: [snap("p1", "w1", "10"), snap("p2", "w2", "5")],
  warehouses: [
    { id: "w1", name: "主仓库", isDefault: true },
    { id: "w2", name: "门店仓" },
  ],
  onDone: vi.fn(),
})

beforeEach(() => {
  vi.mocked(adjustStock).mockReset()
  roleState.role = "owner"
})

describe("helpers", () => {
  it("validates counts and trims float noise", () => {
    expect(isValidCount("0")).toBe(true)
    expect(isValidCount("7.5")).toBe(true)
    expect(isValidCount("")).toBe(false)
    expect(isValidCount("-1")).toBe(false)
    expect(isValidCount("abc")).toBe(false)
    expect(formatQty(0.1 + 0.2)).toBe("0.3")
  })
})

describe("StockAdjustDialog", () => {
  it("shows names and book quantities, never ids", () => {
    render(<StockAdjustDialog {...props()} />)
    expect(screen.getByText("不锈钢螺丝")).toBeTruthy()
    expect(screen.getByText("账上库存 10")).toBeTruthy()
    expect(screen.getByText("账上库存 0")).toBeTruthy() // p2 has no stock in the default warehouse
    expect(screen.queryByText(/w1|p1/)).toBeNull()
  })

  it("shows the difference and submits only what was typed, 10 -> 7", async () => {
    vi.mocked(adjustStock).mockResolvedValue({ adjust_id: "a", adjusted: 1, unchanged: 0, items: [] })
    const p = props()
    render(<StockAdjustDialog {...p} />)
    fireEvent.change(screen.getByLabelText("不锈钢螺丝实际数量"), { target: { value: "7" } })
    expect(screen.getByText("少了 3")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: /确认盘点（1 个有差异）/ }))
    await waitFor(() => expect(adjustStock).toHaveBeenCalled())
    expect(vi.mocked(adjustStock).mock.calls[0][0]).toEqual({
      warehouse_id: "w1",
      remark: undefined,
      items: [{ product_id: "p1", counted_qty: "7" }],
    })
    await waitFor(() => expect(p.onDone).toHaveBeenCalled())
    expect(p.onOpenChange).toHaveBeenCalledWith(false)
  })

  it("blocks submit on a negative or non-numeric count", () => {
    render(<StockAdjustDialog {...props()} />)
    const submit = screen.getByRole("button", { name: "确认盘点" }) as HTMLButtonElement
    expect(submit.disabled).toBe(true) // nothing typed yet
    fireEvent.change(screen.getByLabelText("水管实际数量"), { target: { value: "-2" } })
    expect(screen.getByText("请填 0 或以上的数字")).toBeTruthy()
    expect((screen.getByRole("button", { name: "确认盘点" }) as HTMLButtonElement).disabled).toBe(true)
  })

  it("switching warehouse resets typed counts and uses that warehouse's book quantity", () => {
    render(<StockAdjustDialog {...props()} />)
    fireEvent.change(screen.getByLabelText("水管实际数量"), { target: { value: "3" } })
    fireEvent.change(screen.getByLabelText("盘点哪个仓库"), { target: { value: "w2" } })
    expect((screen.getByLabelText("水管实际数量") as HTMLInputElement).value).toBe("")
    expect(screen.getByText("账上库存 5")).toBeTruthy()
  })

  it("shows the server's Chinese message when saving fails", async () => {
    vi.mocked(adjustStock).mockRejectedValue(new ApiError(400, "x", "盘点数量不能为负数", {}))
    render(<StockAdjustDialog {...props()} />)
    fireEvent.change(screen.getByLabelText("不锈钢螺丝实际数量"), { target: { value: "1" } })
    fireEvent.click(screen.getByRole("button", { name: /确认盘点/ }))
    expect(await screen.findByText("盘点数量不能为负数")).toBeTruthy()
  })

  it("asks for a purchase price when the server cannot value stock counted into an empty book", async () => {
    vi.mocked(adjustStock)
      .mockRejectedValueOnce(
        new ApiError(400, "x", "有商品账面是 0", { code: "cost_required", product_ids: ["p2"] })
      )
      .mockResolvedValueOnce({ adjust_id: "a", adjusted: 1, unchanged: 0, items: [] })
    const p = props()
    render(<StockAdjustDialog {...p} />)
    fireEvent.change(screen.getByLabelText("水管实际数量"), { target: { value: "5" } })
    // the price box only appears once the server has asked for it
    expect(screen.queryByLabelText("水管进货价")).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: /确认盘点/ }))
    expect(await screen.findByText(/填上每件的进货价/)).toBeTruthy()
    const price = screen.getByLabelText("水管进货价")
    expect((screen.getByRole("button", { name: /确认盘点/ }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.change(price, { target: { value: "8.3" } })
    fireEvent.click(screen.getByRole("button", { name: /确认盘点/ }))
    await waitFor(() => expect(adjustStock).toHaveBeenCalledTimes(2))
    expect(vi.mocked(adjustStock).mock.calls[1][0].items).toEqual([
      { product_id: "p2", counted_qty: "5", unit_cost: "8.3" },
    ])
  })

  it("lists the unpriced products by name for a clerk and never shows a price box", async () => {
    roleState.role = "clerk"
    vi.mocked(adjustStock).mockRejectedValue(
      new ApiError(422, "x", "server text", { code: "cost_required", product_ids: ["p2"], product_names: ["水管"] })
    )
    render(<StockAdjustDialog {...props()} />)
    fireEvent.change(screen.getByLabelText("水管实际数量"), { target: { value: "5" } })
    fireEvent.click(screen.getByRole("button", { name: /确认盘点/ }))
    expect(
      await screen.findByText("这些商品还没有进价，请老板或店长先填进价，或由他们来盘点：水管。其他商品可以先把这几行清空，再确认盘点。")
    ).toBeTruthy()
    expect(screen.queryByText(/填写进货单价/)).toBeNull()
    expect(screen.queryByLabelText("水管进货价")).toBeNull()
    expect(screen.queryByText(/填上每件的进货价/)).toBeNull()
  })

  it("falls back to the server's names for products the loaded list does not know", async () => {
    roleState.role = "clerk"
    vi.mocked(adjustStock).mockRejectedValue(
      new ApiError(422, "x", "server text", {
        code: "cost_required",
        product_ids: ["p2", "p9"],
        product_names: ["水管", "角码"],
      })
    )
    render(<StockAdjustDialog {...props()} />)
    fireEvent.change(screen.getByLabelText("水管实际数量"), { target: { value: "5" } })
    fireEvent.click(screen.getByRole("button", { name: /确认盘点/ }))
    expect(await screen.findByText(/：水管、角码。/)).toBeTruthy()
  })

  it("shows two same-named products of different spec once each, merged by id", async () => {
    roleState.role = "clerk"
    vi.mocked(adjustStock).mockRejectedValue(
      new ApiError(422, "x", "server text", {
        code: "cost_required",
        product_ids: ["p2", "p8", "p9"],
        product_names: ["水管", "水管", "水管"],
        products: [
          { id: "p2", name: "水管" },
          { id: "p8", name: "水管" },
          { id: "p9", name: "水管" },
        ],
      })
    )
    render(<StockAdjustDialog {...props()} />)
    fireEvent.change(screen.getByLabelText("水管实际数量"), { target: { value: "5" } })
    fireEvent.click(screen.getByRole("button", { name: /确认盘点/ }))
    expect(await screen.findByText(/：水管、水管、水管。/)).toBeTruthy()
  })

  it("never sends a blank count, so an unfilled row cannot clear the stock", async () => {
    vi.mocked(adjustStock).mockResolvedValue({ adjust_id: "a", adjusted: 1, unchanged: 0, items: [] })
    render(<StockAdjustDialog {...props()} />)
    fireEvent.change(screen.getByLabelText("不锈钢螺丝实际数量"), { target: { value: "7" } })
    fireEvent.change(screen.getByLabelText("水管实际数量"), { target: { value: "  " } })
    fireEvent.click(screen.getByRole("button", { name: /确认盘点/ }))
    await waitFor(() => expect(adjustStock).toHaveBeenCalled())
    expect(vi.mocked(adjustStock).mock.calls[0][0].items).toEqual([{ product_id: "p1", counted_qty: "7" }])
  })

  it("tells the shopkeeper to add a warehouse first when there is none", () => {
    render(<StockAdjustDialog {...props()} warehouses={[]} />)
    expect(screen.getByText(/还没有仓库/)).toBeTruthy()
  })

  it("talks about cost only to people who may see it", () => {
    const { unmount } = render(<StockAdjustDialog {...props()} />)
    expect(screen.getByText(/成本价不变/)).toBeTruthy()
    unmount()

    roleState.role = "clerk"
    render(<StockAdjustDialog {...props()} />)
    expect(screen.getByText(/确认后库存会改成你填的数量/)).toBeTruthy()
    expect(screen.queryByText(/成本|进货价/)).toBeNull()
  })
})
