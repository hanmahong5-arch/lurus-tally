/**
 * API wrapper for the cashier-mode switch (a screen cover on ONE device, not a permission).
 * The server only keeps the PIN: GET /api/v1/tenant/cashier-mode says whether one is set,
 * PUT stores the first one, POST .../verify checks it. Whether this device is in cashier
 * mode lives in localStorage, keyed by shop, so a counter tablet never locks the boss's phone.
 */
import { apiFetch } from "./client"

export interface CashierModeState {
  has_pin: boolean
  /** The shop id the device keys its own flag with. */
  tenant_id: string
}

/** Fired on window after the mode changes so every mounted gate re-reads it. */
export const CASHIER_MODE_EVENT = "tally:cashier-mode"

const FLAG_PREFIX = "tally:cashier-mode:"

/** True when this device was put into cashier mode for the given shop. */
export function readCashierFlag(tenantId: string): boolean {
  try {
    return window.localStorage.getItem(FLAG_PREFIX + tenantId) === "1"
  } catch {
    return false
  }
}

/** Turn the cover on/off for this device only. */
export function writeCashierFlag(tenantId: string, enabled: boolean): void {
  try {
    if (enabled) window.localStorage.setItem(FLAG_PREFIX + tenantId, "1")
    else window.localStorage.removeItem(FLAG_PREFIX + tenantId)
  } catch {
    // Storage blocked (private mode): the cover just won't stick on this device.
  }
}

export async function getCashierMode(signal?: AbortSignal): Promise<CashierModeState> {
  return apiFetch<CashierModeState>("/tenant/cashier-mode", { signal, silent: true })
}

/** Store the shop's first PIN (later calls must match it). */
export async function setCashierPin(pin: string): Promise<CashierModeState> {
  return apiFetch<CashierModeState>("/tenant/cashier-mode", {
    method: "PUT",
    body: JSON.stringify({ pin }),
    silent: true,
  })
}

/** Check the PIN without changing anything (5 wrong tries lock it for 5 minutes). */
export async function verifyCashierPin(pin: string): Promise<void> {
  await apiFetch<{ ok: boolean }>("/tenant/cashier-mode/verify", {
    method: "POST",
    body: JSON.stringify({ pin }),
    silent: true,
  })
}
