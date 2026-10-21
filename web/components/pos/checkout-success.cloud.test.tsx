import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { render, screen, fireEvent, waitFor } from "@testing-library/react"

const { cloud, printReceipt, toast } = vi.hoisted(() => ({
  cloud: { v: { available: true, configured: true, autoPrint: true, ready: true } },
  printReceipt: vi.fn(),
  toast: { success: vi.fn(), error: vi.fn() },
}))

vi.mock("next-auth/react", () => ({ useSession: () => ({ data: { user: { role: "cashier", isOwner: false } } }) }))
vi.mock("@/components/cashier-mode-gate", () => ({ HideInCashierMode: ({ children }: { children: React.ReactNode }) => <>{children}</> }))
vi.mock("@/hooks/use-cloud-print", () => ({ useCloudPrint: () => cloud.v, forgetCloudPrint: vi.fn() }))
vi.mock("@/lib/api/print", () => ({ printReceipt }))
vi.mock("sonner", () => ({ toast }))
vi.mock("@/lib/pos/print-prefs", () => ({ getAutoPrintReceipt: () => false }))

import { CheckoutSuccess } from "./checkout-success"
import { resetCloudPrintGuard } from "@/lib/pos/cloud-print-send"

const props = { billNo: "SO-1", totalAmount: "9.90", billId: "bill-1", onDismiss: vi.fn() }

beforeEach(() => {
  printReceipt.mockReset()
  toast.success.mockReset()
  toast.error.mockReset()
  resetCloudPrintGuard()
  cloud.v = { available: true, configured: true, autoPrint: true, ready: true }
})
afterEach(() => vi.restoreAllMocks())

describe("CheckoutSuccess cloud printing", () => {
  it("auto-sends to the cloud printer and does not open the browser print frame", async () => {
    printReceipt.mockResolvedValue("ord-1")
    const { container } = render(<CheckoutSuccess {...props} />)
    await waitFor(() => expect(printReceipt).toHaveBeenCalledWith("bill-1", expect.anything()))
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("已发送到云打印机"))
    expect(container.querySelector("iframe")).toBeNull()
  })

  it("falls back to the browser print frame when the cloud print fails", async () => {
    printReceipt.mockRejectedValue(new Error("boom"))
    const { container } = render(<CheckoutSuccess {...props} />)
    await waitFor(() => expect(toast.error).toHaveBeenCalled())
    await waitFor(() => expect(container.querySelector("iframe")).not.toBeNull())
  })

  it("does nothing on the cloud when auto print is off", () => {
    cloud.v = { available: true, configured: true, autoPrint: false, ready: true }
    render(<CheckoutSuccess {...props} />)
    expect(printReceipt).not.toHaveBeenCalled()
  })

  it("asks to confirm a second reprint within 60 seconds", async () => {
    cloud.v = { available: true, configured: true, autoPrint: false, ready: true }
    printReceipt.mockResolvedValue("ord-1")
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false)
    render(<CheckoutSuccess {...props} />)
    const btn = screen.getByRole("button", { name: "云打印重打" })

    fireEvent.click(btn)
    await waitFor(() => expect(printReceipt).toHaveBeenCalledTimes(1))
    expect(confirm).not.toHaveBeenCalled()

    fireEvent.click(btn)
    await waitFor(() => expect(confirm).toHaveBeenCalledTimes(1))
    expect(printReceipt).toHaveBeenCalledTimes(1)

    confirm.mockReturnValue(true)
    fireEvent.click(btn)
    await waitFor(() => expect(printReceipt).toHaveBeenCalledTimes(2))
  })

  it("hides the reprint button when cloud printing is unavailable", () => {
    cloud.v = { available: false, configured: false, autoPrint: false, ready: true }
    render(<CheckoutSuccess {...props} />)
    expect(screen.queryByRole("button", { name: "云打印重打" })).toBeNull()
  })
})
