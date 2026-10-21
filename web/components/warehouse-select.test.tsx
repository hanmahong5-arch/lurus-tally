/**
 * Tests for WarehouseSelect — default / sole warehouse auto-selection and the empty-state link.
 */
import { describe, it, expect, vi, afterEach } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import { WarehouseSelect } from "./warehouse-select"

const api = vi.hoisted(() => ({ listWarehouses: vi.fn() }))
vi.mock("@/lib/api/warehouses", () => api)
vi.mock("next/link", () => ({
  default: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a>,
}))

const wh = (id: string, name: string, is_default = false) => ({ id, name, is_default })

describe("WarehouseSelect", () => {
  afterEach(() => vi.clearAllMocks())

  it("selects the default warehouse when nothing is chosen", async () => {
    api.listWarehouses.mockResolvedValue({ items: [wh("w1", "主仓"), wh("w2", "门店仓", true)], total: 2 })
    const onChange = vi.fn()
    render(<WarehouseSelect value="" onChange={onChange} />)
    await waitFor(() => expect(onChange).toHaveBeenCalledWith("w2"))
  })

  it("auto-selects the only warehouse", async () => {
    api.listWarehouses.mockResolvedValue({ items: [wh("w1", "主仓")], total: 1 })
    const onChange = vi.fn()
    render(<WarehouseSelect value="" onChange={onChange} />)
    await waitFor(() => expect(onChange).toHaveBeenCalledWith("w1"))
  })

  it("keeps an existing valid selection", async () => {
    api.listWarehouses.mockResolvedValue({ items: [wh("w1", "主仓"), wh("w2", "门店仓", true)], total: 2 })
    const onChange = vi.fn()
    render(<WarehouseSelect value="w1" onChange={onChange} />)
    await screen.findByRole("option", { name: "主仓" })
    expect(onChange).not.toHaveBeenCalled()
  })

  it("shows a link to /warehouses when there is none", async () => {
    api.listWarehouses.mockResolvedValue({ items: [], total: 0 })
    render(<WarehouseSelect value="" onChange={vi.fn()} />)
    const link = await screen.findByRole("link", { name: /添加一个仓库/ })
    expect(link.getAttribute("href")).toBe("/warehouses")
  })

  it("asks for the backend's maximum page so the default warehouse is not cut off", async () => {
    api.listWarehouses.mockResolvedValue({ items: [wh("w1", "主仓")], total: 1 })
    render(<WarehouseSelect value="" onChange={vi.fn()} />)
    await waitFor(() => expect(api.listWarehouses).toHaveBeenCalledWith(expect.objectContaining({ limit: 200 })))
  })
})
