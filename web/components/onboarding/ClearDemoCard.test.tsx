import { describe, it, expect, vi, beforeEach } from "vitest"
import { useState } from "react"
import { render, screen, fireEvent, waitFor } from "@testing-library/react"

const refresh = vi.fn()
vi.mock("next/navigation", () => ({ useRouter: () => ({ refresh }) }))
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }))
const confirmMock = vi.fn()
vi.mock("@/hooks/useConfirm", () => ({ useConfirm: () => confirmMock }))
vi.mock("@/lib/api/onboarding", () => ({ clearDemo: vi.fn() }))

import * as api from "@/lib/api/onboarding"
import { ApiError } from "@/lib/api/errors"
import { ClearDemoCard } from "./ClearDemoCard"

describe("ClearDemoCard", () => {
  beforeEach(() => {
    refresh.mockClear()
    confirmMock.mockReset()
    vi.mocked(api.clearDemo).mockReset()
  })

  it("clears after confirmation and refreshes the page", async () => {
    confirmMock.mockResolvedValue(true)
    vi.mocked(api.clearDemo).mockResolvedValue({ cleared_products: 1, converted_products: [] })
    const onCleared = vi.fn()
    render(<ClearDemoCard onCleared={onCleared} />)
    fireEvent.click(screen.getByText("清除示例数据"))
    await waitFor(() => expect(api.clearDemo).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(refresh).toHaveBeenCalled())
    expect(onCleared).toHaveBeenCalled()
  })

  it("names the sample products kept as real ones and asks for a stocktake", async () => {
    confirmMock.mockResolvedValue(true)
    vi.mocked(api.clearDemo).mockResolvedValue({
      cleared_products: 2,
      converted_products: [{ id: "p1", code: "A1", name: "示例红枫" }],
    })
    const onCleared = vi.fn()
    const { container } = render(<ClearDemoCard onCleared={onCleared} />)
    fireEvent.click(screen.getByText("清除示例数据"))
    expect(await screen.findByText("示例数据已清除")).toBeInTheDocument()
    expect(container.textContent).toContain("示例红枫")
    expect(container.textContent).toContain("盘点核对")
    expect(onCleared).not.toHaveBeenCalled()
    expect(refresh).not.toHaveBeenCalled()
    fireEvent.click(screen.getByText("知道了"))
    expect(onCleared).toHaveBeenCalled()
    expect(refresh).toHaveBeenCalled()
  })

  it("stays mounted with the notice until acknowledged, even if the parent hides it on refresh", async () => {
    confirmMock.mockResolvedValue(true)
    vi.mocked(api.clearDemo).mockResolvedValue({
      cleared_products: 1,
      converted_products: [{ id: "p1", code: "A1", name: "示例红枫" }],
    })
    // Parent mimics the dashboard: refresh turns has_demo off and unmounts the card.
    function Parent() {
      const [hasDemo, setHasDemo] = useState(true)
      refresh.mockImplementation(() => setHasDemo(false))
      return hasDemo ? <ClearDemoCard /> : <p>已清除</p>
    }
    const { container } = render(<Parent />)
    fireEvent.click(screen.getByText("清除示例数据"))
    expect(await screen.findByText("示例数据已清除")).toBeInTheDocument()
    expect(container.textContent).toContain("示例红枫")
    expect(container.textContent).toContain("盘点核对")
    fireEvent.click(screen.getByText("知道了"))
    expect(container.textContent).toContain("已清除")
    refresh.mockReset()
  })

  it("does nothing when the owner backs out", async () => {
    confirmMock.mockResolvedValue(false)
    render(<ClearDemoCard />)
    fireEvent.click(screen.getByText("清除示例数据"))
    await waitFor(() => expect(confirmMock).toHaveBeenCalled())
    expect(api.clearDemo).not.toHaveBeenCalled()
  })

  it("shows a plain Chinese message, not the error class, when clearing fails", async () => {
    confirmMock.mockResolvedValue(true)
    vi.mocked(api.clearDemo).mockRejectedValue("boom")
    const { container } = render(<ClearDemoCard />)
    fireEvent.click(screen.getByText("清除示例数据"))
    expect(await screen.findByRole("alert")).toBeInTheDocument()
    expect(container.textContent).toContain("操作失败")
    expect(container.textContent).not.toContain("boom")
    expect(refresh).not.toHaveBeenCalled()
  })

  it("shows the backend Chinese message with bill numbers on a 409", async () => {
    confirmMock.mockResolvedValue(true)
    const msg = "示例商品已被你已审核的单据用到（SL-20261011-0001），这些单不能再修改"
    vi.mocked(api.clearDemo).mockRejectedValue(new ApiError(409, "demo_in_use", msg))
    const { container } = render(<ClearDemoCard />)
    fireEvent.click(screen.getByText("清除示例数据"))
    expect(await screen.findByRole("alert")).toBeInTheDocument()
    expect(container.textContent).toContain("SL-20261011-0001")
    expect(container.textContent).not.toContain("demo_in_use")
    expect(refresh).not.toHaveBeenCalled()
  })

  it("shows the action as a hint and disables the button after demo_in_use", async () => {
    confirmMock.mockResolvedValue(true)
    const action = "是你卖的货：在库存页用“盘点”把数量改成实际数"
    vi.mocked(api.clearDemo).mockRejectedValue(
      new ApiError(409, "demo_in_use", "x", { message: "这些示例数据会一直算在报表里", action }),
    )
    const { container } = render(<ClearDemoCard />)
    fireEvent.click(screen.getByText("清除示例数据"))
    expect(await screen.findByText(action)).toBeInTheDocument()
    expect(container.textContent).toContain("示例商品已经用在你自己的单据里，清除不了")
    expect(screen.getByRole("button", { name: "清除示例数据" })).toBeDisabled()
  })

  it("keeps the button enabled after a transient failure", async () => {
    confirmMock.mockResolvedValue(true)
    vi.mocked(api.clearDemo).mockRejectedValue(new ApiError(500, "clear_demo_failed", "x", { message: "清除示例数据没有成功" }))
    render(<ClearDemoCard />)
    fireEvent.click(screen.getByText("清除示例数据"))
    expect(await screen.findByRole("alert")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "清除示例数据" })).not.toBeDisabled()
  })
})
