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

  it("TestCommandPalette_TypedQuery_ShowsAskAIItemFirst", () => {
    render(<CommandPalette />)
    openPalette()

    const input = screen.getByTestId("palette-input")
    // AI-first: the "问 AI:<query>" row surfaces immediately on any input — no
    // ≥5-char gate, no Tab.
    fireEvent.change(input, { target: { value: "低库存商品" } })

    expect(screen.getByText(/问 AI:/)).toBeInTheDocument()
  })

  it("TestCommandPalette_CmdK_OpensInAIAskPosture", () => {
    render(<CommandPalette />)
    openPalette()

    const input = screen.getByTestId("palette-input")
    // ⌘K lands directly in AI-ask posture: the placeholder invites a question
    // (no Tab detour needed) and the AI-ask badge is shown.
    expect(input).toHaveAttribute(
      "placeholder",
      "问 AI:上月哪些 SKU 滞销? / 帮我算 A 仓补货"
    )
    expect(screen.getByText("✨ AI 提问态")).toBeInTheDocument()
    // Empty state offers tappable starter questions.
    expect(screen.getByText("上月哪些 SKU 滞销?")).toBeInTheDocument()
  })

  it("TestCommandPalette_EnterOnQuery_AsksAIWithoutTab", () => {
    const onAIQuery = vi.fn()
    render(<CommandPalette onAIQuery={onAIQuery} />)
    openPalette()

    const input = screen.getByTestId("palette-input")
    fireEvent.change(input, { target: { value: "低库存商品" } })
    // Enter (AI row is first + selected by default) sends straight to AI — no Tab.
    fireEvent.keyDown(input, { key: "Enter" })

    expect(onAIQuery).toHaveBeenCalledWith("低库存商品")
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
