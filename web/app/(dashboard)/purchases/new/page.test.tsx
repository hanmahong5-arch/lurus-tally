/**
 * Unit tests for purchases/new/page.tsx draft integration.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import * as idbStorage from "@/lib/draft/idb-storage"

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), back: vi.fn(), refresh: vi.fn() }),
}))

vi.mock("@/lib/draft/idb-storage")

vi.mock("@/lib/profile", () => ({
  useProfile: () => ({ profileType: "cross_border" }),
  ProfileGate: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}))

// Stub out BillLineEditor (does network calls).
vi.mock("@/components/bill-line-editor", () => ({
  BillLineEditor: () => <div data-testid="bill-line-editor" />,
}))

// Stub cross-border components.
vi.mock("@/components/cross-border/currency-selector", () => ({
  CurrencySelector: ({ value, onChange }: { value: string; onChange: (v: string) => void }) => (
    <select value={value} onChange={(e) => onChange(e.target.value)} data-testid="currency-selector" />
  ),
}))
vi.mock("@/components/cross-border/rate-input", () => ({
  RateInput: () => <input data-testid="rate-input" />,
}))

import NewPurchasePage from "./page"

describe("purchases/new/page.tsx — draft integration", () => {
  beforeEach(() => {
    vi.mocked(idbStorage.isDraftStorageAvailable).mockReturnValue(true)
    vi.mocked(idbStorage.draftGet).mockResolvedValue(undefined)
    vi.mocked(idbStorage.draftSet).mockResolvedValue(undefined)
    vi.mocked(idbStorage.draftDel).mockResolvedValue(undefined)
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it("TestPurchaseNewPage_DraftRestored_ShowsToast", async () => {
    const savedAt = new Date().toISOString()
    vi.mocked(idbStorage.draftGet).mockResolvedValue({
      value: { billDate: "2025-01-01", remark: "草稿采购单" },
      savedAt,
    })

    render(<NewPurchasePage />)

    await waitFor(() => {
      expect(screen.getByText(/已恢复/)).toBeTruthy()
    })
  })
})
