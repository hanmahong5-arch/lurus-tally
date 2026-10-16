"use client"

import { useCallback, useEffect, useRef, useState, useTransition } from "react"
import { QRCodeSVG } from "qrcode.react"

import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Modal } from "@/components/ui/modal"
import {
  BillingError,
  type BillingOverview,
  type PaymentMethod,
  getBillingOverview,
  getOrderStatus,
  subscribe,
} from "@/lib/api/billing"
import { formatCNY } from "@/lib/format"
import {
  TALLY_PLANS,
  type TallyPlan,
  formatCycle,
  formatPriceCNY,
} from "@/lib/billing/plans"
import { cn } from "@/lib/utils"

const PAYMENT_METHODS: { value: PaymentMethod; label: string }[] = [
  { value: "wallet", label: "钱包余额" },
  { value: "alipay", label: "支付宝" },
  { value: "wechat", label: "微信支付" },
]

interface FlashMessage {
  kind: "ok" | "error"
  text: string
}

// The order the buyer is paying for survives the round trip to Alipay/WeChat
// here, so the page can confirm it even if the provider drops our query string.
const PENDING_ORDER_KEY = "tally.pending_order"
const POLL_INTERVAL_MS = 2000
const POLL_RETURN_MS = 60_000
const POLL_QR_MS = 5 * 60_000

interface QRPayment {
  orderNo: string
  code: string
  planName: string
}

function readPendingOrder(): string | null {
  if (typeof window === "undefined") return null
  try {
    const fromURL = new URLSearchParams(window.location.search).get("order_no")
    return fromURL || window.sessionStorage.getItem(PENDING_ORDER_KEY)
  } catch {
    return null
  }
}

function rememberPendingOrder(orderNo: string | null) {
  try {
    if (orderNo) window.sessionStorage.setItem(PENDING_ORDER_KEY, orderNo)
    else window.sessionStorage.removeItem(PENDING_ORDER_KEY)
  } catch {
    // Storage blocked (private mode): the order_no query param still works.
  }
}

export function SubscriptionPlansView() {
  const [overview, setOverview] = useState<BillingOverview | null>(null)
  const [overviewError, setOverviewError] = useState<string | null>(null)
  const [overviewSettled, setOverviewSettled] = useState(false)
  const [paymentMethod, setPaymentMethod] = useState<PaymentMethod>("wallet")
  const [pendingPlan, setPendingPlan] = useState<string | null>(null)
  const [flash, setFlash] = useState<FlashMessage | null>(null)
  const [qr, setQR] = useState<QRPayment | null>(null)
  const [isPending, startTransition] = useTransition()
  const pollRef = useRef(0)

  const refreshOverview = useCallback(async () => {
    try {
      setOverview(await getBillingOverview())
      setOverviewError(null)
    } catch (err) {
      setOverviewError(
        err instanceof BillingError ? `${err.code}: ${err.message}` : (err as Error).message,
      )
    } finally {
      setOverviewSettled(true)
    }
  }, [])

  // Poll an order until it settles or the window closes. Any newer poll
  // supersedes an older one (e.g. closing the QR dialog, starting another).
  const watchOrder = useCallback(
    async (orderNo: string, windowMs: number, onPaid: () => void) => {
      const ticket = ++pollRef.current
      const deadline = Date.now() + windowMs
      while (pollRef.current === ticket && Date.now() < deadline) {
        try {
          const st = await getOrderStatus(orderNo)
          if (pollRef.current !== ticket) return
          if (st.paid) {
            rememberPendingOrder(null)
            onPaid()
            await refreshOverview()
            return
          }
          if (st.final) {
            rememberPendingOrder(null)
            setFlash({ kind: "error", text: `订单未完成支付（${st.status}），可以重新下单。` })
            return
          }
        } catch {
          // Transient: keep polling until the window closes.
        }
        await new Promise((r) => setTimeout(r, POLL_INTERVAL_MS))
      }
    },
    [refreshOverview],
  )

  useEffect(() => {
    const polls = pollRef
    void refreshOverview()
    // Back from Alipay / WeChat H5: confirm the order the buyer just paid.
    const returning = readPendingOrder()
    if (returning) {
      setFlash({ kind: "ok", text: "正在确认支付结果..." })
      void watchOrder(returning, POLL_RETURN_MS, () =>
        setFlash({ kind: "ok", text: "支付成功，套餐已开通。" }),
      )
    }
    // Unmount cancels any poll still running.
    return () => {
      polls.current++
    }
  }, [refreshOverview, watchOrder])

  function handleSubscribe(plan: TallyPlan) {
    setFlash(null)
    setPendingPlan(plan.code)
    startTransition(async () => {
      try {
        const res = await subscribe({
          plan_code: plan.code,
          billing_cycle: plan.cycle,
          payment_method: paymentMethod,
          return_url: "/subscription",
        })
        if (res.pay_url) {
          rememberPendingOrder(res.order_no ?? null)
          window.location.assign(res.pay_url)
          return
        }
        if (res.qr_code && res.order_no) {
          const orderNo = res.order_no
          setQR({ orderNo, code: res.qr_code, planName: plan.name })
          void watchOrder(orderNo, POLL_QR_MS, () => {
            setQR(null)
            setFlash({ kind: "ok", text: `已开通 ${plan.name}` })
          })
          return
        }
        if (res.subscription) {
          setFlash({
            kind: "ok",
            text: `已开通 ${plan.name}（${res.subscription.status}）`,
          })
          // Refresh the overview so the active plan badge updates immediately.
          await refreshOverview()
        } else {
          setFlash({
            kind: "ok",
            text: "订单已创建，支付完成后套餐自动开通。",
          })
        }
      } catch (err) {
        const text =
          err instanceof BillingError
            ? mapErrorMessage(err)
            : err instanceof Error
              ? err.message
              : "订阅失败，请稍后再试。"
        setFlash({ kind: "error", text })
      } finally {
        setPendingPlan(null)
      }
    })
  }

  function closeQR() {
    pollRef.current++
    setQR(null)
  }

  // Only the workspace owner may change the plan. Before the overview loads
  // (or when it failed) we leave the buttons enabled: the backend enforces it.
  const canManage = overview?.can_manage ?? true

  const activePlanCode = overview?.subscription?.plan_code ?? "free"

  return (
    <div className="space-y-6">
      <OverviewBar overview={overview} error={overviewError} settled={overviewSettled} />

      {!canManage && (
        <div className="rounded-md border border-border bg-muted/30 px-4 py-2 text-sm text-muted-foreground">
          套餐属于整个企业，只有企业所有者可以变更。如需升级，请联系所有者。
        </div>
      )}

      <PaymentMethodPicker value={paymentMethod} onChange={setPaymentMethod} />

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {TALLY_PLANS.map((plan) => {
          const isActive = plan.code === activePlanCode
          return (
            <Card
              key={plan.code}
              className={cn(
                "relative",
                plan.highlight && "ring-2 ring-primary/40",
              )}
            >
              {plan.highlight && (
                <span className="absolute -top-2 right-4 rounded-full bg-primary px-2 py-0.5 text-xs font-medium text-primary-foreground">
                  推荐
                </span>
              )}
              <CardHeader>
                <CardTitle>{plan.name}</CardTitle>
                <CardDescription>{plan.tagline}</CardDescription>
                <div className="mt-3 flex items-baseline gap-1">
                  <span className="text-2xl font-semibold tabular-nums">
                    {formatPriceCNY(plan.priceCNY)}
                  </span>
                  <span className="text-xs text-muted-foreground">
                    {formatCycle(plan.cycle)}
                  </span>
                </div>
              </CardHeader>
              <CardContent>
                <ul className="space-y-1 text-sm text-muted-foreground">
                  {plan.features.map((f) => (
                    <li key={f} className="flex gap-2">
                      <span aria-hidden>✓</span>
                      <span>{f}</span>
                    </li>
                  ))}
                </ul>
              </CardContent>
              <CardFooter>
                <Button
                  className="w-full"
                  variant={isActive ? "outline" : plan.highlight ? "default" : "secondary"}
                  disabled={isActive || isPending || !canManage}
                  onClick={() => handleSubscribe(plan)}
                  data-testid={`subscribe-${plan.code}`}
                >
                  {isActive
                    ? "当前套餐"
                    : isPending && pendingPlan === plan.code
                      ? "处理中..."
                      : "一键订阅"}
                </Button>
              </CardFooter>
            </Card>
          )
        })}
      </div>

      <Modal
        open={qr !== null}
        onOpenChange={(open) => {
          if (!open) closeQR()
        }}
        title="微信扫码支付"
        description={qr ? `开通 ${qr.planName}：请用微信扫描二维码，支付完成后自动开通。` : undefined}
        footer={
          <Button variant="outline" onClick={closeQR}>
            取消
          </Button>
        }
      >
        {qr && (
          <div className="mt-4 flex flex-col items-center gap-2">
            <QRCodeSVG value={qr.code} size={200} data-testid="pay-qr" />
            <span className="text-xs text-muted-foreground">订单号 {qr.orderNo}</span>
          </div>
        )}
      </Modal>

      {flash && (
        <div
          role="status"
          className={cn(
            "rounded-md border px-4 py-3 text-sm",
            flash.kind === "ok"
              ? "border-success/30 bg-success/10 text-success"
              : "border-destructive/30 bg-destructive/10 text-destructive",
          )}
        >
          {flash.text}
        </div>
      )}
    </div>
  )
}

function PaymentMethodPicker({
  value,
  onChange,
}: {
  value: PaymentMethod
  onChange: (m: PaymentMethod) => void
}) {
  return (
    <div className="flex items-center gap-2 text-sm">
      <span className="text-muted-foreground">支付方式：</span>
      <div className="inline-flex rounded-lg border border-border p-0.5">
        {PAYMENT_METHODS.map((m) => (
          <button
            type="button"
            key={m.value}
            onClick={() => onChange(m.value)}
            className={cn(
              "rounded-md px-3 py-1 text-xs transition-colors",
              value === m.value
                ? "bg-primary text-primary-foreground"
                : "text-muted-foreground hover:text-foreground",
            )}
            data-testid={`pm-${m.value}`}
          >
            {m.label}
          </button>
        ))}
      </div>
    </div>
  )
}

// withReturnTo: lurus-platform 充值页读 return_to(只认 *.lurus.cn 等白名单),
// 付完款给出「返回 Tally」按钮,用户不用自己找回来。
export function withReturnTo(topupUrl: string): string {
  if (typeof window === "undefined") return topupUrl
  try {
    const u = new URL(topupUrl)
    u.searchParams.set("return_to", window.location.href)
    return u.toString()
  } catch {
    return topupUrl
  }
}

function OverviewBar({
  overview,
  error,
  settled,
}: {
  overview: BillingOverview | null
  error: string | null
  settled: boolean
}) {
  if (error) {
    return (
      <div className="rounded-md border border-warning/30 bg-warning/10 px-4 py-2 text-sm text-warning">
        无法读取账户概览：{error}
      </div>
    )
  }
  if (!settled) {
    return (
      <div className="rounded-md border border-border bg-muted/30 px-4 py-2 text-sm text-muted-foreground">
        正在加载账户信息...
      </div>
    )
  }
  if (!overview) {
    return null
  }
  const sub = overview.subscription
  return (
    <div className="flex flex-wrap items-center gap-4 rounded-md border border-border bg-card px-4 py-3 text-sm">
      <span className="text-muted-foreground">企业钱包余额：</span>
      <span className="font-mono tabular-nums">{formatCNY(overview.wallet.available)}</span>
      {overview.can_manage && overview.topup_url && (
        <a
          href={withReturnTo(overview.topup_url)}
          target="_blank"
          rel="noreferrer"
          className="text-xs text-primary underline-offset-2 hover:underline"
        >
          充值
        </a>
      )}
      <span className="text-muted-foreground">当前套餐：</span>
      <span className="font-medium">
        {sub?.plan_code ?? "free"} ({sub?.status ?? "免费版"})
      </span>
    </div>
  )
}

function mapErrorMessage(err: BillingError): string {
  switch (err.code) {
    case "insufficient_balance":
      return "钱包余额不足，请先充值或选择其他支付方式。"
    case "platform_unavailable":
      return "计费服务暂时不可用，请稍后再试。"
    case "platform_auth_failed":
      return "Tally 与计费平台之间的鉴权失败，请联系管理员。"
    case "unauthorized":
      return "请先登录后再尝试订阅。"
    case "not_found":
      return "未找到对应的套餐，请刷新页面。"
    case "owner_only":
      return "只有企业所有者可以变更套餐。"
    case "not_linked":
      return "企业计费账户尚未开通，请企业所有者打开本页面完成开通。"
    case "method_not_available":
      return "该支付方式暂未开通，请选择其他支付方式。"
    default:
      return err.message || "订阅失败，请稍后再试。"
  }
}
