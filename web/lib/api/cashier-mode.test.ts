import { describe, it, expect, vi, beforeEach } from "vitest"

const apiFetch = vi.fn()
vi.mock("./client", () => ({ apiFetch: (...a: unknown[]) => apiFetch(...a) }))

import {
  getCashierMode,
  setCashierPin,
  verifyCashierPin,
  readCashierFlag,
  writeCashierFlag,
} from "./cashier-mode"

beforeEach(() => {
  apiFetch.mockReset()
  window.localStorage.clear()
})

describe("cashier-mode api", () => {
  it("reads whether a PIN is set", async () => {
    apiFetch.mockResolvedValue({ has_pin: true, tenant_id: "t1" })
    expect(await getCashierMode()).toEqual({ has_pin: true, tenant_id: "t1" })
    expect(apiFetch.mock.calls[0][0]).toBe("/tenant/cashier-mode")
  })

  it("stores the first PIN", async () => {
    apiFetch.mockResolvedValue({ has_pin: true, tenant_id: "t1" })
    await setCashierPin("1234")
    const [url, opts] = apiFetch.mock.calls[0]
    expect(url).toBe("/tenant/cashier-mode")
    expect(opts.method).toBe("PUT")
    expect(JSON.parse(opts.body)).toEqual({ pin: "1234" })
  })

  it("verifies the PIN with a POST", async () => {
    apiFetch.mockResolvedValue({ ok: true })
    await verifyCashierPin("1234")
    const [url, opts] = apiFetch.mock.calls[0]
    expect(url).toBe("/tenant/cashier-mode/verify")
    expect(opts.method).toBe("POST")
    expect(JSON.parse(opts.body)).toEqual({ pin: "1234" })
  })
})

describe("device flag", () => {
  it("is per shop and per device", () => {
    expect(readCashierFlag("t1")).toBe(false)
    writeCashierFlag("t1", true)
    expect(readCashierFlag("t1")).toBe(true)
    expect(readCashierFlag("t2")).toBe(false)
    writeCashierFlag("t1", false)
    expect(readCashierFlag("t1")).toBe(false)
  })
})
