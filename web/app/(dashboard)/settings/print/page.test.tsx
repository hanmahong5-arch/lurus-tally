import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"

const { role, getSettings } = vi.hoisted(() => ({ role: { v: "cashier" }, getSettings: vi.fn() }))
vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: { user: { role: role.v, isOwner: role.v === "owner", tenantId: "t1" } } }),
}))
vi.mock("@/lib/api/print-settings", () => ({
  EMPTY_PRINT_SETTINGS: { shop_name: "", phone: "", address: "", receipt_footer: "" },
  getPrintSettings: getSettings,
  putPrintSettings: vi.fn(),
}))
vi.mock("@/lib/pos/print-prefs", () => ({
  getAutoPrintReceipt: () => false,
  setAutoPrintReceipt: vi.fn(),
  getReceiptPaper: () => "80",
  setReceiptPaper: vi.fn(),
}))

import PrintSettingsPage from "./page"

beforeEach(() => {
  getSettings.mockReset()
  getSettings.mockResolvedValue({ shop_name: "老李五金", phone: "123", address: "路1号", receipt_footer: "谢谢" })
})

describe("print settings page", () => {
  it("shows the shop info read-only to a cashier", async () => {
    role.v = "cashier"
    render(<PrintSettingsPage />)
    const name = (await screen.findByLabelText("店名")) as HTMLInputElement
    await waitFor(() => expect(name.value).toBe("老李五金"))
    expect(name).toBeDisabled()
    expect(screen.queryByRole("button", { name: "保存店铺信息" })).toBeNull()
    expect(getSettings).toHaveBeenCalled()
  })

  it("lets the owner edit", async () => {
    role.v = "owner"
    render(<PrintSettingsPage />)
    const name = (await screen.findByLabelText("店名")) as HTMLInputElement
    await waitFor(() => expect(name.value).toBe("老李五金"))
    expect(name).not.toBeDisabled()
    expect(screen.getByRole("button", { name: "保存店铺信息" })).toBeInTheDocument()
  })
})
