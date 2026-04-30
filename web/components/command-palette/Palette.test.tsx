import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, fireEvent } from "@testing-library/react"
import { CommandPalette } from "./Palette"

// Mock next/navigation
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn() }),
}))

/**
 * Helper that opens the palette by firing the keyboard shortcut.
 * JSDOM does not dispatch window keydown the same way real browsers do;
 * we render the component and fire keydown on the window.
 */
function openPalette() {
  fireEvent.keyDown(window, { key: "k", metaKey: true })
}

describe("CommandPalette", () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it("TestCommandPalette_CmdK_OpensPanel", async () => {
    render(<CommandPalette />)

    // Panel should not be visible initially.
    expect(screen.queryByTestId("command-palette")).not.toBeInTheDocument()

    openPalette()

    expect(screen.getByTestId("command-palette")).toBeInTheDocument()
    expect(screen.getByTestId("palette-input")).toBeInTheDocument()
  })

  it("TestCommandPalette_EscKey_ClosesPanel", () => {
    render(<CommandPalette />)
    openPalette()

    expect(screen.getByTestId("command-palette")).toBeInTheDocument()

    const input = screen.getByTestId("palette-input")
    fireEvent.keyDown(input, { key: "Escape" })

    expect(screen.queryByTestId("command-palette")).not.toBeInTheDocument()
  })

  it("TestCommandPalette_TypeQuery_FiltersList", () => {
    render(<CommandPalette />)
    openPalette()

    const input = screen.getByTestId("palette-input")
    fireEvent.change(input, { target: { value: "商品" } })

    // Should show 商品管理 page
    expect(screen.getByText("商品管理")).toBeInTheDocument()
  })

  it("TestCommandPalette_LongQuery_ShowsAIItem", () => {
    render(<CommandPalette />)
    openPalette()

    const input = screen.getByTestId("palette-input")
    // Must be >5 chars to trigger AI suggestion
    fireEvent.change(input, { target: { value: "低库存商品" } })

    expect(screen.getByText(/Ask AI:/)).toBeInTheDocument()
  })

  it("TestCommandPalette_TabOnLongQuery_EntersAIMode", () => {
    render(<CommandPalette />)
    openPalette()

    const input = screen.getByTestId("palette-input")
    fireEvent.change(input, { target: { value: "低库存商品" } })
    fireEvent.keyDown(input, { key: "Tab" })

    // Placeholder should change to AI mode
    expect(input).toHaveAttribute("placeholder", "Ask AI…")
  })

  it("TestCommandPalette_AIQuerySelected_CallsOnAIQuery", () => {
    const onAIQuery = vi.fn()
    render(<CommandPalette onAIQuery={onAIQuery} />)
    openPalette()

    const input = screen.getByTestId("palette-input")
    fireEvent.change(input, { target: { value: "低库存商品" } })

    // Click the AI ask item
    const aiItem = screen.getByTestId("palette-item-ai-ask")
    fireEvent.click(aiItem)

    expect(onAIQuery).toHaveBeenCalledWith("低库存商品")
  })

  it("TestCommandPalette_StaticPages_AlwaysVisible", () => {
    render(<CommandPalette />)
    openPalette()

    expect(screen.getByText("商品管理")).toBeInTheDocument()
    expect(screen.getByText("采购管理")).toBeInTheDocument()
    expect(screen.getByText("销售管理")).toBeInTheDocument()
  })
})
