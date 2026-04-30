"use client"

import { useEffect, useState, useTransition } from "react"

import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import {
  BillingError,
  type BillingOverview,
  type PaymentMethod,
  getBillingOverview,
  subscribe,
} from "@/lib/api/billing"
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

export function SubscriptionPlansView() {
  const [overview, setOverview] = useState<BillingOverview | null>(null)
  const [overviewError, setOverviewError] = useState<string | null>(null)
  const [overviewSettled, setOverviewSettled] = useState(false)
  const [paymentMethod, setPaymentMethod] = useState<PaymentMethod>("wallet")
  const [pendingPlan, setPendingPlan] = useState<string | null>(null)
  const [flash, setFlash] = useState<FlashMessage | null>(null)
  const [isPending, startTransition] = useTransition()

  useEffect(() => {
    let cancelled = false
    getBillingOverview()
      .then((ov) => {
        if (cancelled) return
        setOverview(ov)
        setOverviewSettled(true)
      })
      .catch((err: Error) => {
        if (cancelled) return
        // not_found = platform has no account record yet for this Zitadel
        // user. Treat as a fresh free-tier account so the plan grid still
        // renders without a scary error banner. Subscribe will lazy-create
        // the account on platform side via /billing/subscribe.
        if (err instanceof BillingError && err.code === "not_found") {
          setOverview(null)
          setOverviewError(null)
          setOverviewSettled(true)
          return
        }
        const detail =
          err instanceof BillingError
            ? `${err.code}: ${err.message}`
            : err.message
        setOverviewError(detail)
        setOverviewSettled(true)
      })
    return () => {
      cancelled = true
    }
  }, [])

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
          window.location.assign(res.pay_url)
          return
        }
        if (res.subscription) {
          setFlash({
            kind: "ok",
            text: `已开通 ${plan.name}（${res.subscription.status}）`,
          })
          // Refresh the overview so the active plan badge updates immediately.
          const ov = await getBillingOverview()
          setOverview(ov)
        } else {
          setFlash({
            kind: "ok",
            text: "已提交订阅请求，等待支付平台回调。",
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

  const activePlanCode = overview?.subscription?.plan_code ?? "free"

  return (
    <div className="space-y-6">
      <OverviewBar overview={overview} error={overviewError} settled={overviewSettled} />

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
                  disabled={isActive || (isPending && pendingPlan === plan.code)}
                  onClick={() => handleSubscribe(plan)}
                  data-testid={`subscribe-${plan.code}`}
                >
                  {isActive
                    ? "当前套餐"
                    : isPending && pendingPlan === plan.code
                      ? "处理中…"
                      : "一键订阅"}
                </Button>
              </CardFooter>
            </Card>
          )
        })}
      </div>

      {flash && (
        <div
          role="status"
          className={cn(
            "rounded-md border px-4 py-3 text-sm",
            flash.kind === "ok"
              ? "border-emerald-300 bg-emerald-50 text-emerald-900 dark:border-emerald-800 dark:bg-emerald-950 dark:text-emerald-100"
              : "border-red-300 bg-red-50 text-red-900 dark:border-red-800 dark:bg-red-950 dark:text-red-100",
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
      <div className="rounded-md border border-amber-300 bg-amber-50 px-4 py-2 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-100">
        无法读取账户概览：{error}
      </div>
    )
  }
  if (!settled) {
    return (
      <div className="rounded-md border border-border bg-muted/30 px-4 py-2 text-sm text-muted-foreground">
        正在加载账户信息…
      </div>
    )
  }
  if (!overview) {
    // Settled but no overview = brand-new account (platform returned not_found).
    return (
      <div className="flex flex-wrap items-center gap-4 rounded-md border border-border bg-card px-4 py-3 text-sm">
        <span className="text-muted-foreground">钱包余额：</span>
        <span className="font-mono tabular-nums">¥0.00</span>
        <span className="text-muted-foreground">当前套餐：</span>
        <span className="font-medium">free（免费版）</span>
      </div>
    )
  }
  const wallet = overview.wallet?.available ?? 0
  const sub = overview.subscription
  return (
    <div className="flex flex-wrap items-center gap-4 rounded-md border border-border bg-card px-4 py-3 text-sm">
      <span className="text-muted-foreground">账户：</span>
      <span className="font-medium">{overview.account.email}</span>
      <span className="text-muted-foreground">钱包余额：</span>
      <span className="font-mono tabular-nums">¥{wallet.toFixed(2)}</span>
      <span className="text-muted-foreground">当前套餐：</span>
      <span className="font-medium">
        {sub?.plan_code ?? "free"} ({sub?.status ?? "未激活"})
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
    default:
      return err.message || "订阅失败，请稍后再试。"
  }
}
