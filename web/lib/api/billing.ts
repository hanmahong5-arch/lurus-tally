/**
 * Tally billing API client.
 *
 * Wraps the Tally Go backend's /api/v1/billing/* endpoints (which themselves
 * proxy lurus-platform's internal subscription checkout). The frontend only
 * ever talks to its own backend — it never hits platform directly, so the
 * INTERNAL_API_KEY stays inside the cluster.
 */

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

const BASE = "/api/v1"

async function readError(res: Response, fallback: string): Promise<BillingError> {
  let code = "unknown"
  let message = fallback
  try {
    const body = (await res.json()) as { error?: string; detail?: string; message?: string }
    if (body.error) code = body.error
    if (body.detail) message = body.detail
    else if (body.message) message = body.message
  } catch {
    // fall through with defaults
  }
  return new BillingError(code, message, res.status)
}

export async function getBillingOverview(): Promise<BillingOverview> {
  const res = await fetch(`${BASE}/billing/overview`, {
    headers: { "Content-Type": "application/json" },
    cache: "no-store",
  })
  if (!res.ok) {
    throw await readError(res, `getBillingOverview: HTTP ${res.status}`)
  }
  return res.json()
}

export async function subscribe(req: SubscribeRequest): Promise<SubscribeResponse> {
  const res = await fetch(`${BASE}/billing/subscribe`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  })
  if (!res.ok) {
    throw await readError(res, `subscribe: HTTP ${res.status}`)
  }
  return res.json()
}
