import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor, fireEvent } from "@testing-library/react"

const push = vi.fn()
let path = "/dashboard"
vi.mock("next/navigation", () => ({
  usePathname: () => path,
  useRouter: () => ({ push, replace: push }),
}))

let profile = "retail"
vi.mock("@/lib/profile", () => ({
  useProfile: () => ({ profileType: profile }),
}))

// The shop only keeps the PIN; the on/off flag is this device's localStorage (real helpers).
let hasPin = true
let readFails = false
let tenant = "t1"
const getCashierMode = vi.fn(async () => {
  if (readFails) throw new Error("down")
  return { has_pin: hasPin, tenant_id: tenant }
})
const setCashierPin = vi.fn()
const verifyCashierPin = vi.fn()
vi.mock("@/lib/api/cashier-mode", async () => {
  const actual = await vi.importActual<typeof import("@/lib/api/cashier-mode")>("@/lib/api/cashier-mode")
  return {
    ...actual,
    getCashierMode: () => getCashierMode(),
    setCashierPin: (...a: unknown[]) => setCashierPin(...a),
    verifyCashierPin: (...a: unknown[]) => verifyCashierPin(...a),
  }
})
import { readCashierFlag, writeCashierFlag } from "@/lib/api/cashier-mode"

/** Puts this device into cashier mode for the current shop. */
function deviceOn(on = true) {
  writeCashierFlag(tenant, on)
}

import {
  HideInCashierMode,
  CashierModeProvider,
  CashierModeContentGate,
  CashierModeRedirect,
  CashierModeControl,
} from "./cashier-mode-gate"

beforeEach(() => {
  window.localStorage.clear()
  tenant = "t1"
  setCashierPin.mockReset()
  verifyCashierPin.mockReset()
  hasPin = true
  profile = "retail"
  path = "/dashboard"
  push.mockReset()
  getCashierMode.mockClear()
  readFails = false
})

describe("HideInCashierMode", () => {
  it("shows children when cashier mode is off", async () => {
    render(<HideInCashierMode>利润 100</HideInCashierMode>)
    expect(await screen.findByText("利润 100")).toBeInTheDocument()
  })

  it("hides children once cashier mode is on", async () => {
    deviceOn()
    render(<HideInCashierMode>利润 100</HideInCashierMode>)
    await waitFor(() => expect(screen.queryByText("利润 100")).toBeNull())
  })
})

describe("HideInCashierMode before the state is read", () => {
  it("renders nothing until the read comes back", async () => {
    render(<HideInCashierMode>利润 100</HideInCashierMode>)
    expect(screen.queryByText("利润 100")).toBeNull()
    expect(await screen.findByText("利润 100")).toBeInTheDocument()
  })

  it("does not hide anything for a shop that cannot use the POS", async () => {
    deviceOn()
    profile = "horticulture"
    render(<HideInCashierMode retailOnly>利润 100</HideInCashierMode>)
    expect(await screen.findByText("利润 100")).toBeInTheDocument()
  })
})

describe("CashierModeRedirect", () => {
  it("never redirects a shop that cannot use the POS", async () => {
    deviceOn()
    profile = "horticulture"
    render(<CashierModeRedirect />)
    await new Promise((r) => setTimeout(r, 20))
    expect(push).not.toHaveBeenCalled()
  })

  it("sends the cashier from a back-office page to the POS", async () => {
    deviceOn()
    render(<CashierModeRedirect />)
    await waitFor(() => expect(push).toHaveBeenCalledWith("/pos"))
  })

  it("leaves the POS and today's records alone", async () => {
    deviceOn()
    path = "/pos/history"
    render(<CashierModeRedirect />)
    await new Promise((r) => setTimeout(r, 20))
    expect(push).not.toHaveBeenCalled()
  })

  it("does nothing when cashier mode is off", async () => {
    render(<CashierModeRedirect />)
    await new Promise((r) => setTimeout(r, 20))
    expect(push).not.toHaveBeenCalled()
  })
})

describe("CashierModeControl", () => {
  it("asks for the first PIN twice and refuses a mismatch", async () => {
    hasPin = false
    setCashierPin.mockImplementation(async () => {
      hasPin = true
      return { has_pin: true, tenant_id: "t1" }
    })
    render(<CashierModeControl />)
    fireEvent.click(await screen.findByRole("button", { name: "开启收银模式" }))
    fireEvent.change(screen.getByLabelText("密码"), { target: { value: "1234" } })
    fireEvent.change(screen.getByLabelText("再输一遍密码"), { target: { value: "1235" } })
    fireEvent.click(screen.getByRole("button", { name: "确定" }))
    expect(await screen.findByText("两次输入的密码不一样，请重新输入")).toBeInTheDocument()
    expect(setCashierPin).not.toHaveBeenCalled()
    fireEvent.change(screen.getByLabelText("再输一遍密码"), { target: { value: "1234" } })
    fireEvent.click(screen.getByRole("button", { name: "确定" }))
    await waitFor(() => expect(setCashierPin).toHaveBeenCalledWith("1234"))
    await waitFor(() => expect(readCashierFlag("t1")).toBe(true))
  })

  it("reminds the owner that cashiers should use their own cashier account", async () => {
    render(<CashierModeControl />)
    expect(await screen.findByText(/有收银员时请让他用自己的收银账号/)).toBeInTheDocument()
  })

  it("tells the owner what to do when the PIN is forgotten and that it only covers this device", async () => {
    render(<CashierModeControl />)
    fireEvent.click(await screen.findByRole("button", { name: "开启收银模式" }))
    expect(screen.getByText(/忘记密码请联系客服/)).toBeInTheDocument()
    expect(screen.getAllByText(/只在这台设备上生效/).length).toBeGreaterThan(0)
  })

  it("turns the mode on with the PIN and says it is not a permission", async () => {
    verifyCashierPin.mockResolvedValue(undefined)
    render(<CashierModeControl />)
    fireEvent.click(await screen.findByRole("button", { name: "开启收银模式" }))
    expect(screen.getByText(/不是权限控制/)).toBeInTheDocument()
    expect(screen.queryByLabelText("再输一遍密码")).toBeNull()
    fireEvent.change(screen.getByLabelText("密码"), { target: { value: "1234" } })
    fireEvent.click(screen.getByRole("button", { name: "确定" }))
    await waitFor(() => expect(verifyCashierPin).toHaveBeenCalledWith("1234"))
    expect(setCashierPin).not.toHaveBeenCalled()
    await waitFor(() => expect(readCashierFlag("t1")).toBe(true))
  })

  it("leaving needs the PIN and only clears this device", async () => {
    deviceOn()
    verifyCashierPin.mockResolvedValue(undefined)
    render(<CashierModeControl variant="exit" />)
    fireEvent.click(await screen.findByRole("button", { name: "退出收银模式" }))
    fireEvent.change(screen.getByLabelText("密码"), { target: { value: "1234" } })
    fireEvent.click(screen.getByRole("button", { name: "确定" }))
    await waitFor(() => expect(verifyCashierPin).toHaveBeenCalledWith("1234"))
    await waitFor(() => expect(readCashierFlag("t1")).toBe(false))
  })

  it("shows the server's Chinese message when the PIN is wrong", async () => {
    deviceOn()
    const { ApiError } = await import("@/lib/api/errors")
    verifyCashierPin.mockRejectedValue(new ApiError(403, "pin_wrong", "x", { detail: "密码不对，请再试一次" }))
    render(<CashierModeControl variant="exit" />)
    fireEvent.click(await screen.findByRole("button", { name: "退出收银模式" }))
    fireEvent.change(screen.getByLabelText("密码"), { target: { value: "0000" } })
    fireEvent.click(screen.getByRole("button", { name: "确定" }))
    expect(await screen.findByText("密码不对，请再试一次")).toBeInTheDocument()
    expect(readCashierFlag("t1")).toBe(true)
  })
})

describe("CashierModeProvider and CashierModeContentGate", () => {
  function page() {
    return (
      <CashierModeProvider>
        <CashierModeContentGate>
          <HideInCashierMode>
            <span>卡片</span>
          </HideInCashierMode>
          <CashierModeControl />
          <p>报表内容</p>
        </CashierModeContentGate>
      </CashierModeProvider>
    )
  }

  it("reads the mode once for the whole page", async () => {
    render(page())
    await screen.findByText("报表内容")
    expect(getCashierMode).toHaveBeenCalledTimes(1)
  })

  it("renders no content in cashier mode on a back-office path", async () => {
    deviceOn()
    path = "/reports"
    render(page())
    await waitFor(() => expect(getCashierMode).toHaveBeenCalled())
    await new Promise((r) => setTimeout(r, 20))
    expect(screen.queryByText("报表内容")).toBeNull()
  })

  it("renders nothing for a retail shop while the state is not known yet", async () => {
    render(page())
    expect(screen.queryByText("报表内容")).toBeNull()
    expect(await screen.findByText("报表内容")).toBeInTheDocument()
  })

  it("keeps the POS paths visible in cashier mode", async () => {
    deviceOn()
    path = "/pos/history"
    render(page())
    expect(await screen.findByText("报表内容")).toBeInTheDocument()
  })

  it("asks for a refresh, not an open page, when the read fails", async () => {
    readFails = true
    render(page())
    expect(await screen.findByText(/请刷新/)).toBeInTheDocument()
    expect(screen.queryByText("报表内容")).toBeNull()
  })

  it("never blocks a shop that cannot use the POS", async () => {
    deviceOn()
    profile = "horticulture"
    path = "/reports"
    render(page())
    expect(await screen.findByText("报表内容")).toBeInTheDocument()
  })
})

describe("support reset of the PIN", () => {
  it("releases a device that still has the local flag when the shop has no PIN", async () => {
    hasPin = false
    deviceOn()
    path = "/reports"
    render(
      <CashierModeProvider>
        <CashierModeContentGate>
          <p>报表内容</p>
        </CashierModeContentGate>
      </CashierModeProvider>,
    )
    expect(await screen.findByText("报表内容")).toBeInTheDocument()
    expect(readCashierFlag("t1")).toBe(false)
  })
})

describe("CashierModeControl first PIN", () => {
  it("Enter on the empty second box only moves focus there", async () => {
    hasPin = false
    render(<CashierModeControl />)
    fireEvent.click(await screen.findByRole("button", { name: "开启收银模式" }))
    fireEvent.change(screen.getByLabelText("密码"), { target: { value: "1234" } })
    fireEvent.keyDown(screen.getByLabelText("密码"), { key: "Enter" })
    await waitFor(() => expect(document.activeElement?.id).toBe("cashier-pin2"))
    expect(screen.queryByRole("alert")).toBeNull()
    expect(setCashierPin).not.toHaveBeenCalled()
  })

  it("tells the owner to wait 5 minutes when locked out", async () => {
    const { ApiError } = await import("@/lib/api/errors")
    verifyCashierPin.mockRejectedValue(new ApiError(429, "pin_locked", "x"))
    render(<CashierModeControl />)
    fireEvent.click(await screen.findByRole("button", { name: "开启收银模式" }))
    fireEvent.change(screen.getByLabelText("密码"), { target: { value: "1234" } })
    fireEvent.click(screen.getByRole("button", { name: "确定" }))
    expect(await screen.findByText(/5 分钟后再试/)).toBeInTheDocument()
  })
})

describe("two devices of one shop", () => {
  function page() {
    return (
      <CashierModeProvider>
        <CashierModeContentGate>
          <p>报表内容</p>
        </CashierModeContentGate>
      </CashierModeProvider>
    )
  }

  it("the counter device is covered while another device (finance) still reaches the reports", async () => {
    path = "/reports"
    // Device A: switched on here.
    deviceOn()
    const a = render(page())
    await waitFor(() => expect(getCashierMode).toHaveBeenCalled())
    await new Promise((r) => setTimeout(r, 20))
    expect(screen.queryByText("报表内容")).toBeNull()
    a.unmount()
    // Device B: same shop, same server answer, but its own storage never had the flag.
    window.localStorage.clear()
    render(page())
    expect(await screen.findByText("报表内容")).toBeInTheDocument()
  })

  it("keeps the flag per shop", async () => {
    path = "/reports"
    writeCashierFlag("other-shop", true)
    render(page())
    expect(await screen.findByText("报表内容")).toBeInTheDocument()
  })
})
