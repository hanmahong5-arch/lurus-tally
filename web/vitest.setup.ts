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

// Global stub: next-auth/react's useSession() throws without a <SessionProvider>.
// Component tests render pages bare, so default it to "no session" — useTenantId()
// then resolves to undefined, matching the pre-migration behaviour where the dev
// tenant env var was unset in tests. Tests needing a real session override this
// module locally with their own vi.mock("next-auth/react", ...).
vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: null, status: "unauthenticated" }),
  signIn: vi.fn(),
  signOut: vi.fn(),
}))
