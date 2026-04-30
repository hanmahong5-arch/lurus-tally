/**
 * Unit tests for sales/new/page.tsx draft integration.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import * as idbStorage from "@/lib/draft/idb-storage"

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), back: vi.fn(), refresh: vi.fn() }),
  useSearchParams: () => ({ get: () => null }),
}))

vi.mock("@/lib/draft/idb-storage")

vi.mock("@/lib/profile", () => ({
  useProfile: () => ({ profileType: "cross_border" }),
  ProfileGate: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}))

vi.mock("@/components/sale-line-editor", () => ({
  SaleLineEditor: () => <div data-testid="sale-line-editor" />,
}))

vi.mock("@/components/cross-border/currency-selector", () => ({
  CurrencySelector: ({ value, onChange }: { value: string; onChange: (v: string) => void }) => (
    <select value={value} onChange={(e) => onChange(e.target.value)} data-testid="currency-selector" />
  ),
}))

vi.mock("@/components/cross-border/rate-input", () => ({
  RateInput: () => <input data-testid="rate-input" />,
}))

import NewSalePage from "./page"

describe("sales/new/page.tsx — draft integration", () => {
  beforeEach(() => {
    vi.mocked(idbStorage.isDraftStorageAvailable).mockReturnValue(true)
    vi.mocked(idbStorage.draftGet).mockResolvedValue(undefined)
    vi.mocked(idbStorage.draftSet).mockResolvedValue(undefined)
    vi.mocked(idbStorage.draftDel).mockResolvedValue(undefined)
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it("TestSaleNewPage_DraftRestored_ShowsToast", async () => {
    const savedAt = new Date().toISOString()
    vi.mocked(idbStorage.draftGet).mockResolvedValue({
      value: { customerName: "测试客户", billDate: "2025-01-01" },
      savedAt,
    })

    render(<NewSalePage />)

    await waitFor(() => {
      expect(screen.getByText(/已恢复/)).toBeTruthy()
    })
  })
})
