import "@testing-library/jest-dom"
import { vi } from "vitest"

// Global stub: components that call useConfirm() during render shouldn't crash
// in component tests that don't wrap with <ConfirmProvider>. Tests verifying
// confirm-dialog behaviour can locally override via vi.mock + render-wrapped.
vi.mock("@/hooks/useConfirm", async () => {
  const actual = await vi.importActual<typeof import("@/hooks/useConfirm")>("@/hooks/useConfirm")
  return {
    ...actual,
    useConfirm: () => async () => true,
  }
})
