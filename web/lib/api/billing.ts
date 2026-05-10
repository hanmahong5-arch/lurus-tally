/**
 * Tally billing API client.
 *
 * Wraps the Tally Go backend's /api/v1/billing/* endpoints (which themselves
 * proxy lurus-platform's internal subscription checkout). The frontend only
 * ever talks to its own backend — it never hits platform directly, so the
 * INTERNAL_API_KEY stays inside the cluster.
 *
 * Uses apiFetch internally with `silent: true` because billing surfaces its own
 * domain-specific BillingError (with platform code) rather than the generic toast.
 */
import { apiFetch } from "./client"
import { ApiError, NetworkError } from "./errors"

export type BillingCycle = "forever" | "monthly" | "yearly"

export type PaymentMethod = "wallet" | "alipay" | "wechat" | "stripe"

export interface SubscribeRequest {
  plan_code: string
  billing_cycle: BillingCycle
  payment_method: PaymentMethod
  return_url?: string
}

export interface SubscriptionSnapshot {
  plan_code: string
  status: string
  expires_at?: string
}

export interface SubscribeResponse {
  /** Set when payment_method=wallet and activation succeeded immediately. */
  subscription?: SubscriptionSnapshot
  /** Set for external payment methods — caller should redirect to it. */
  pay_url?: string
  /** Order tracking number — present whenever pay_url is. */
  order_no?: string
}

export interface BillingOverview {
  account: {
    id: number
    username: string
    email: string
    vip_tier: string
    vip_expires_at?: string
  }
  wallet?: {
    available: number
    frozen: number
    total: number
  }
  subscription?: SubscriptionSnapshot | null
  entitlements?: Record<string, string>
}

/**
 * BillingError carries the platform-mapped error code so the UI can branch
 * (e.g. show "top up wallet" CTA on insufficient_balance).
 */
export class BillingError extends Error {
  constructor(
    public readonly code: string,
    message: string,
    public readonly httpStatus: number,
  ) {
    super(message)
    this.name = "BillingError"
  }
}

function toBillingError(err: unknown, fallbackOp: string): never {
  if (err instanceof ApiError) {
    const body = (err.body ?? {}) as { error?: string; detail?: string; message?: string }
    const code = body.error ?? err.code ?? "unknown"
    const message = body.detail ?? body.message ?? err.message ?? `${fallbackOp}: HTTP ${err.status}`
    throw new BillingError(code, message, err.status)
  }
  if (err instanceof NetworkError) {
    throw new BillingError(err.kind, `${fallbackOp}: ${err.message}`, 0)
  }
  throw err
}

export async function getBillingOverview(): Promise<BillingOverview> {
  try {
    return await apiFetch<BillingOverview>("/billing/overview", {
      cache: "no-store",
      silent: true,
    })
  } catch (err) {
    toBillingError(err, "getBillingOverview")
  }
}

export async function subscribe(req: SubscribeRequest): Promise<SubscribeResponse> {
  try {
    return await apiFetch<SubscribeResponse>("/billing/subscribe", {
      method: "POST",
      body: JSON.stringify(req),
      silent: true,
    })
  } catch (err) {
    toBillingError(err, "subscribe")
  }
}
