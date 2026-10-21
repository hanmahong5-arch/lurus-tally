import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"

vi.mock("@/lib/api/importing", () => ({
  uploadOrderCSV: vi.fn(),
  listMappings: vi.fn(),
}))
vi.mock("@/lib/api/products", () => ({ getProduct: vi.fn() }))

import { listMappings } from "@/lib/api/importing"
import { getProduct } from "@/lib/api/products"
import { ApiError } from "@/lib/api/errors"
import ImportsPage from "./page"

const PID = "11111111-aaaa-bbbb-cccc-222222222222"

describe("imports page mapping table", () => {
  beforeEach(() => {
    vi.mocked(listMappings).mockResolvedValue({
      items: [
        { id: "m1", platform: "amazon", platform_sku: "AMZ-1", product_id: PID, updated_at: "2026-10-01T00:00:00Z" },
        { id: "m2", platform: "amazon", platform_sku: "AMZ-2", product_id: "99999999-0000-0000-0000-000000000000", updated_at: "2026-10-01T00:00:00Z" },
      ],
      total: 2,
    })
    vi.mocked(getProduct).mockImplementation(async (id: string) => {
      if (id === PID) return { id, name: "螺丝刀" } as never
      throw new ApiError(404, "not_found", "not found")
    })
  })

  it("names the platform code and shop product in plain words, never a short id", async () => {
    const { container } = render(<ImportsPage />)
    expect(await screen.findByText("螺丝刀")).toBeInTheDocument()
    expect(screen.getByText("商品编码对应表")).toBeInTheDocument()
    expect(screen.getByText("平台商品编码")).toBeInTheDocument()
    expect(screen.getByText("未找到商品")).toBeInTheDocument()
    expect(container.textContent).not.toMatch(/SKU|11111111|99999999/)
  })

  it("shows the name of a product no matter how many products the shop has", async () => {
    render(<ImportsPage />)
    expect(await screen.findByText("螺丝刀")).toBeInTheDocument()
    expect(getProduct).toHaveBeenCalledWith(PID)
  })

  it("only says not found on a real 404, otherwise says the name is temporarily unavailable", async () => {
    vi.mocked(getProduct).mockRejectedValue(new ApiError(500, "x", "boom"))
    render(<ImportsPage />)
    await waitFor(() => expect(screen.getAllByText("商品名暂时取不到")).toHaveLength(2))
    expect(screen.queryByText("未找到商品")).toBeNull()
  })
})
