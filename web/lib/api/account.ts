/**
 * Account summary aggregator — combines /me identity + /billing/overview into
 * a single payload for the Tier 1 sidebar card and Tier 2 drawer.
 *
 * Both upstream endpoints are tolerated independently: if /billing/overview
 * fails (new tenant, platform unreachable), we still return identity data so
 * the sidebar card never goes blank.
 */

import { apiFetch } from "./client"
import { BillingError, type BillingOverview } from "./billing"

export interface AccountIdentity {
  user_id: string
  tenant_id: string
  email: string
  display_name: string
  role: string
  is_owner: boolean
  profile_type: string
  /** Set when the user has uploaded a profile avatar (Phase 3). Resolved
   *  against the same /api/proxy prefix everything else uses. */
  avatar_url?: string
  phone?: string
}

export interface AccountSummary {
  identity: AccountIdentity
  billing: BillingOverview | null
  billingError: string | null
}

export type AccountStatusLight = "green" | "amber" | "red"

const SEVEN_DAYS_MS = 7 * 24 * 60 * 60 * 1000

/** Compute the single status-light colour for the sidebar card. */
export function computeStatusLight(billing: BillingOverview | null): AccountStatusLight {
  const sub = billing?.subscription
  const available = billing?.wallet?.available ?? 0

  if (sub?.status === "expired" || sub?.status === "cancelled") return "red"

  if (sub?.expires_at) {
    const ms = new Date(sub.expires_at).getTime() - Date.now()
    if (ms <= SEVEN_DAYS_MS) return "amber"
  }
  if (available <= 0) return "amber"

  if (sub?.status === "active" && available > 0) return "green"

  return "amber"
}

/** Days remaining (rounded up). Negative when expired. Null when no expiry. */
export function daysUntilExpiry(expiresAt: string | undefined | null): number | null {
  if (!expiresAt) return null
  const ms = new Date(expiresAt).getTime() - Date.now()
  return Math.ceil(ms / (24 * 60 * 60 * 1000))
}

type BillingResult =
  | { kind: "ok"; overview: BillingOverview }
  | { kind: "not_found" }
  | { kind: "error"; message: string }

async function fetchBilling(): Promise<BillingResult> {
  try {
    const overview = await apiFetch<BillingOverview>("/billing/overview", {
      cache: "no-store",
      silent: true,
    })
    return { kind: "ok", overview }
  } catch (err) {
    if (err instanceof BillingError && err.code === "not_found") {
      return { kind: "not_found" }
    }
    const message =
      err instanceof BillingError
        ? `${err.code}: ${err.message}`
        : err instanceof Error
          ? err.message
          : String(err)
    return { kind: "error", message }
  }
}

export async function fetchAccountSummary(): Promise<AccountSummary> {
  const [identity, billingResult] = await Promise.all([
    apiFetch<AccountIdentity>("/me", { cache: "no-store", silent: true }),
    fetchBilling(),
  ])

  if (billingResult.kind === "ok") {
    return { identity, billing: billingResult.overview, billingError: null }
  }
  if (billingResult.kind === "not_found") {
    return { identity, billing: null, billingError: null }
  }
  return { identity, billing: null, billingError: billingResult.message }
}
