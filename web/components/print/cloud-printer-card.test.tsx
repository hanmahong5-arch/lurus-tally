import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"

const { cloud, role, getStatus } = vi.hoisted(() => ({
  cloud: { v: { available: true, configured: false, autoPrint: false, ready: true } },
  role: { v: "owner" },
  getStatus: vi.fn(),
}))

vi.mock("next-auth/react", () => ({
  useSession: () => ({ data: { user: { role: role.v, isOwner: role.v === "owner", tenantId: "t1" } } }),
}))
vi.mock("@/hooks/use-cloud-print", () => ({ useCloudPrint: () => cloud.v, forgetCloudPrint: vi.fn() }))
vi.mock("@/lib/api/print", () => ({ getPrinterStatus: getStatus, printTestPage: vi.fn() }))
vi.mock("@/lib/api/print-settings", () => ({ putPrintSettings: vi.fn() }))

import { CloudPrinterCard } from "./cloud-printer-card"

const base = { shop_name: "", phone: "", address: "", receipt_footer: "" }

beforeEach(() => {
  getStatus.mockReset()
  getStatus.mockResolvedValue({ configured: true, online: true, status: "在线，工作状态正常。", cloud_print_available: true })
  cloud.v = { available: true, configured: false, autoPrint: false, ready: true }
  role.v = "owner"
})

describe("CloudPrinterCard", () => {
  it("is hidden when the platform has no cloud printing", () => {
    cloud.v = { available: false, configured: false, autoPrint: false, ready: true }
    render(<CloudPrinterCard settings={base} loading={false} onSaved={vi.fn()} />)
    expect(screen.queryByTestId("cloud-printer-card")).toBeNull()
  })

  it("shows an unconfigured card with save and no test button enabled", () => {
    render(<CloudPrinterCard settings={base} loading={false} onSaved={vi.fn()} />)
    expect(screen.getByTestId("cloud-printer-card")).toBeInTheDocument()
    expect(screen.getByText(/状态：未配置/)).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "保存云打印机" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "打印测试页" })).toBeDisabled()
  })

  it("shows the online state and masked key for a configured printer", async () => {
    const settings = { ...base, printer: { sn: "SN123", key: "****1234", name: "前台", paper: "80" as const, auto_print: true } }
    render(<CloudPrinterCard settings={settings} loading={false} onSaved={vi.fn()} />)
    expect(await screen.findByText(/状态：在线/)).toBeInTheDocument()
    expect((screen.getByLabelText(/打印机密钥/) as HTMLInputElement).value).toBe("****1234")
    expect(screen.getByRole("button", { name: "打印测试页" })).toBeEnabled()
  })

  it("is read-only for a cashier", () => {
    role.v = "cashier"
    render(<CloudPrinterCard settings={base} loading={false} onSaved={vi.fn()} />)
    expect(screen.getByLabelText(/打印机编号/)).toBeDisabled()
    expect(screen.queryByRole("button", { name: "保存云打印机" })).toBeNull()
  })
})
