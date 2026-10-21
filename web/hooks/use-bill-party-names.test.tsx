import { describe, it, expect, vi, afterEach } from "vitest"
import { renderHook, waitFor } from "@testing-library/react"
import { useBillPartyNames } from "./use-bill-party-names"

const api = vi.hoisted(() => ({
  getSupplier: vi.fn(),
  getCustomer: vi.fn(),
  getWarehouse: vi.fn(),
}))
vi.mock("@/lib/api/suppliers", () => ({ getSupplier: api.getSupplier }))
vi.mock("@/lib/api/customers", () => ({ getCustomer: api.getCustomer }))
vi.mock("@/lib/api/warehouses", () => ({ getWarehouse: api.getWarehouse }))

describe("useBillPartyNames", () => {
  afterEach(() => vi.clearAllMocks())

  it("does not show the previous bill's names after the ids change", async () => {
    api.getSupplier.mockImplementation((id: string) =>
      id === "p1" ? Promise.resolve({ name: "甲供应商" }) : new Promise(() => undefined)
    )
    api.getWarehouse.mockImplementation((id: string) =>
      id === "w1" ? Promise.resolve({ name: "主仓" }) : new Promise(() => undefined)
    )
    const { result, rerender } = renderHook(
      (p: { partnerId: string; warehouseId: string }) => useBillPartyNames({ kind: "supplier", ...p }),
      { initialProps: { partnerId: "p1", warehouseId: "w1" } }
    )
    await waitFor(() => expect(result.current.partnerName).toBe("甲供应商"))
    expect(result.current.warehouseName).toBe("主仓")

    rerender({ partnerId: "p2", warehouseId: "w2" })
    await waitFor(() => expect(result.current.partnerName).toBe(""))
    expect(result.current.warehouseName).toBe("")
  })
})
