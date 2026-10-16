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
  /** Set for redirect payments (支付宝, 手机微信) — send the browser there. */
  pay_url?: string
  /** Set for scan-to-pay (微信 on desktop) — render as a QR code. */
  qr_code?: string
  /** Order tracking number — present whenever pay_url or qr_code is. */
  order_no?: string
}

/**
 * Billing snapshot of the CURRENT WORKSPACE (tenant). The plan belongs to the
 * workspace, not to the signed-in user: every member sees the same plan and
 * wallet, and only the owner (`can_manage`) may change it.
 */
export interface BillingOverview {
  /** Platform account holding the workspace plan; 0 before billing is set up. */
  account_id: number
  wallet: {
    available: number
    frozen: number
    total: number
  }
  vip: {
    level: number
    level_name: string
  }
  subscription?: SubscriptionSnapshot | null
  entitlements: Record<string, string>
  /** Where to top up the plan holder's wallet on identity.lurus.cn. */
  topup_url?: string
  can_manage: boolean
}

export interface OrderStatus {
  order_no: string
  status: string
  paid: boolean
  /** True once the order can no longer change — stop polling. */
  final: boolean
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

export async function getOrderStatus(orderNo: string): Promise<OrderStatus> {
  try {
    return await apiFetch<OrderStatus>(`/billing/orders/${encodeURIComponent(orderNo)}`, {
      cache: "no-store",
      silent: true,
    })
  } catch (err) {
    toBillingError(err, "getOrderStatus")
  }
}
