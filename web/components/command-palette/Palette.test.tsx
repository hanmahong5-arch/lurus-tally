import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, fireEvent } from "@testing-library/react"
import { CommandPalette } from "./Palette"

const { state } = vi.hoisted(() => ({ state: { role: null as string | null } }))
vi.mock("next-auth/react", () => ({
  useSession: () => ({
    data: state.role ? { user: { role: state.role, isOwner: state.role === "owner" } } : null,
  }),
}))

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
    state.role = null
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
      "问 AI:上月哪些商品滞销? / 帮我算 A 仓补货"
    )
    expect(screen.getByText("✨ AI 提问态")).toBeInTheDocument()
    // Empty state offers tappable starter questions.
    expect(screen.getByText("上月哪些商品滞销?")).toBeInTheDocument()
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

  describe("by shop role", () => {
    function openWithRole(role: string) {
      state.role = role
      render(<CommandPalette />)
      openPalette()
    }

    it("offers billing to the owner only", () => {
      openWithRole("owner")
      expect(screen.getByText("订阅与计费")).toBeInTheDocument()
    })

    it("hides billing from a manager", () => {
      openWithRole("manager")
      expect(screen.queryByText("订阅与计费")).toBeNull()
      expect(screen.getByText("采购管理")).toBeInTheDocument()
    })

    it("leaves a cashier with checkout and no purchasing, billing or assistant", () => {
      openWithRole("cashier")
      expect(screen.getByText("POS 收银")).toBeInTheDocument()
      expect(screen.queryByText("采购管理")).toBeNull()
      expect(screen.queryByText("订阅与计费")).toBeNull()
      expect(screen.queryByText("新建采购单")).toBeNull()
      expect(screen.queryByText("新建商品")).toBeNull()
      expect(screen.queryByText("✨ AI 提问态")).toBeNull()
      fireEvent.change(screen.getByTestId("palette-input"), { target: { value: "低库存商品" } })
      expect(screen.queryByText(/问 AI:/)).toBeNull()
    })

    it("lets a clerk draft bills and edit products but not take payments", () => {
      openWithRole("clerk")
      expect(screen.getByText("新建采购单")).toBeInTheDocument()
      expect(screen.getByText("新建商品")).toBeInTheDocument()
      expect(screen.queryByText("POS 收银")).toBeNull()
    })

    it("keeps a finance person out of drafting", () => {
      openWithRole("finance")
      expect(screen.queryByText("新建销售单")).toBeNull()
      expect(screen.queryByText("新建商品")).toBeNull()
    })
  })

  describe("when the assistant is switched off", () => {
    it("shows no 问 AI row, starters or footer hint", () => {
      render(<CommandPalette aiEnabled={false} />)
      openPalette()
      expect(screen.queryByText("✨ AI 提问态")).toBeNull()
      expect(screen.queryByText(/问 AI/)).toBeNull()
      fireEvent.change(screen.getByTestId("palette-input"), { target: { value: "低库存商品" } })
      expect(screen.queryByText(/问 AI/)).toBeNull()
    })
  })
})
