/**
 * Unit tests for products/new/page.tsx draft integration.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import * as idbStorage from "@/lib/draft/idb-storage"

// Mock next/navigation before importing the page.
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), back: vi.fn(), refresh: vi.fn() }),
}))

// Mock idb-storage to control draft state.
vi.mock("@/lib/draft/idb-storage")

// Mock profile (cross_border is the stub default).
vi.mock("@/lib/profile", () => ({
  useProfile: () => ({ profileType: "cross_border" }),
  ProfileGate: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}))

// Stub out child components that do network calls.
vi.mock("@/components/unit-selector", () => ({
  UnitSelector: () => <select data-testid="unit-selector" />,
}))
vi.mock("@/components/cross-border/hs-code-input", () => ({
  HsCodeInput: ({ value, onChange }: { value: string; onChange: (v: string) => void }) => (
    <input data-testid="hs-code-input" value={value} onChange={(e) => onChange(e.target.value)} />
  ),
}))

// Import page AFTER mocks are set up.
import NewProductPage from "./page"

describe("products/new/page.tsx — draft integration", () => {
  beforeEach(() => {
    vi.mocked(idbStorage.isDraftStorageAvailable).mockReturnValue(true)
    vi.mocked(idbStorage.draftGet).mockResolvedValue(undefined)
    vi.mocked(idbStorage.draftSet).mockResolvedValue(undefined)
    vi.mocked(idbStorage.draftDel).mockResolvedValue(undefined)
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it("TestProductNewPage_DraftRestored_ShowsToast", async () => {
    const savedAt = new Date().toISOString()
    vi.mocked(idbStorage.draftGet).mockResolvedValue({
      value: { name: "草稿商品", code: "SKU-DRAFT" },
      savedAt,
    })

    render(<NewProductPage />)

    await waitFor(() => {
      expect(screen.getByText(/已恢复/)).toBeTruthy()
    })
  })

  it("TestProductNewPage_DraftBadge_ShowsLocalAfterTyping", async () => {
    vi.mocked(idbStorage.draftGet).mockResolvedValue(undefined)

    render(<NewProductPage />)

    // Wait for mount effects to settle.
    await waitFor(() => {
      // Before typing, badge should not show "未保存".
      expect(screen.queryByText("未保存")).toBeNull()
    })

    // Trigger setValue by interacting with the name input.
    const nameInput = screen.getAllByRole("textbox").find(
      (el) => (el as HTMLInputElement).placeholder === "商品全称"
    )
    expect(nameInput).toBeTruthy()
    // Fire change event to simulate typing.
    const { fireEvent: fe } = await import("@testing-library/react")
    fe.change(nameInput!, { target: { value: "新商品" } })

    await waitFor(() => {
      expect(screen.getByText("未保存")).toBeTruthy()
    })
  })
})
