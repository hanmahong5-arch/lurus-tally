import { describe, it, expect, vi } from "vitest"
import { render, screen } from "@testing-library/react"

vi.mock("@/hooks/useConfirm", () => ({ useConfirm: () => async () => true }))
vi.mock("@/hooks/use-tenant-id", () => ({ useTenantId: () => "t-1" }))
vi.mock("@/lib/api/pats", () => ({
  createPAT: vi.fn(),
  revokePAT: vi.fn(),
  listPATs: vi.fn().mockResolvedValue([]),
}))

import ApiKeysPage from "./page"

describe("ApiKeysPage header", () => {
  it("uses the plain title and moves the explanation to the subtitle", async () => {
    render(<ApiKeysPage />)
    expect(await screen.findByRole("heading", { name: "接口密钥" })).toBeInTheDocument()
    expect(screen.getByText(/本店接口工具/)).toBeInTheDocument()
  })
})
