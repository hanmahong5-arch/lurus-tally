import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, fireEvent, waitFor } from "@testing-library/react"

const push = vi.fn()
vi.mock("next/navigation", () => ({ useRouter: () => ({ push }) }))
vi.mock("@/lib/api/onboarding", () => ({ seedDemo: vi.fn(), clearDemo: vi.fn() }))
vi.mock("@/lib/api/warehouses", () => ({ listWarehouses: vi.fn() }))
const confirmMock = vi.fn()
vi.mock("@/hooks/useConfirm", () => ({ useConfirm: () => confirmMock }))
vi.mock("@/lib/telemetry", () => ({ trackEvent: vi.fn() }))
vi.mock("@/components/product-import-dialog", () => ({
  ProductImportDialog: ({ open, onImported }: { open: boolean; onImported: () => void }) =>
    open ? <button onClick={onImported}>mock-商品导入已完成</button> : null,
}))

import * as onboardingApi from "@/lib/api/onboarding"
import * as warehouseApi from "@/lib/api/warehouses"
import OnboardingWizard from "./OnboardingWizard"

describe("OnboardingWizard", () => {
  beforeEach(() => {
    push.mockClear()
    confirmMock.mockReset()
    confirmMock.mockResolvedValue(true)
    localStorage.clear()
    vi.mocked(onboardingApi.seedDemo).mockReset()
    vi.mocked(onboardingApi.clearDemo).mockReset()
    vi.mocked(warehouseApi.listWarehouses).mockReset()
  })

  it("lets the owner skip the sample data and go straight to their own records", () => {
    render(<OnboardingWizard persona="retail" />)
    fireEvent.click(screen.getByText("跳过，直接录我的数据"))
    expect(push).toHaveBeenCalledWith("/dashboard")
    expect(onboardingApi.seedDemo).not.toHaveBeenCalled()
  })

  it("opens the product import dialog (not the order-import page) and lands on products", () => {
    render(<OnboardingWizard persona="retail" />)
    fireEvent.click(screen.getByText("导入我的商品"))
    expect(push).not.toHaveBeenCalled()
    fireEvent.click(screen.getByText("mock-商品导入已完成"))
    expect(push).toHaveBeenCalledWith("/products")
  })

  it("seeds, then clears the sample data from the wizard", async () => {
    vi.mocked(warehouseApi.listWarehouses).mockResolvedValue({ items: [{ id: "w1" }], total: 1 } as never)
    vi.mocked(onboardingApi.seedDemo).mockResolvedValue({ products_created: 3 })
    vi.mocked(onboardingApi.clearDemo).mockResolvedValue({ cleared_products: 3, converted_products: [] })
    render(<OnboardingWizard persona="retail" />)
    fireEvent.click(screen.getByText("载入示例商品", { selector: "button" }))
    fireEvent.click(await screen.findByText("清除示例，开始录我的数据"))
    await waitFor(() => expect(onboardingApi.clearDemo).toHaveBeenCalled())
    await waitFor(() => expect(push).toHaveBeenCalledWith("/dashboard"))
  })

  it("stays on the step and asks for a stocktake when sample products were kept", async () => {
    vi.mocked(warehouseApi.listWarehouses).mockResolvedValue({ items: [{ id: "w1" }], total: 1 } as never)
    vi.mocked(onboardingApi.seedDemo).mockResolvedValue({ products_created: 3 })
    vi.mocked(onboardingApi.clearDemo).mockResolvedValue({
      cleared_products: 2,
      converted_products: [{ id: "p1", code: "A1", name: "示例红枫" }],
    })
    const { container } = render(<OnboardingWizard persona="retail" />)
    fireEvent.click(screen.getByText("载入示例商品", { selector: "button" }))
    fireEvent.click(await screen.findByText("清除示例，开始录我的数据"))
    expect(await screen.findByText("示例数据已清除")).toBeInTheDocument()
    expect(container.textContent).toContain("示例红枫")
    expect(push).not.toHaveBeenCalled()
    fireEvent.click(screen.getByText("去库存页盘点"))
    expect(push).toHaveBeenCalledWith("/stock")
  })

  it("shows no jargon on either step", async () => {
    vi.mocked(warehouseApi.listWarehouses).mockResolvedValue({ items: [{ id: "w1" }], total: 1 } as never)
    vi.mocked(onboardingApi.seedDemo).mockResolvedValue({ products_created: 3 })
    const { container } = render(<OnboardingWizard persona="retail" />)
    expect(container.textContent).not.toMatch(/\bPO\b|aha|AI 已生成|WAC|FIFO/)
    fireEvent.click(screen.getByText("载入示例商品", { selector: "button" }))
    await screen.findByText(/已建好 3 个示例商品/)
    expect(container.textContent).not.toMatch(/\bPO\b|aha|AI 已生成|WAC|FIFO/)
  })

  it("explains a missing warehouse in plain Chinese", async () => {
    vi.mocked(warehouseApi.listWarehouses).mockResolvedValue({ items: [], total: 0 } as never)
    render(<OnboardingWizard persona="retail" />)
    fireEvent.click(screen.getByText("载入示例商品", { selector: "button" }))
    expect(await screen.findByText(/新建一个仓库/)).toBeInTheDocument()
  })
})
