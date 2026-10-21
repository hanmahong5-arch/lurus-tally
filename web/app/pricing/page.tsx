import Link from "next/link"
import type { Metadata } from "next"
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { buttonVariants } from "@/components/ui/button"
import { SEED_TRIAL, TALLY_PLANS, formatCycle, formatPriceCNY } from "@/lib/billing/plans"
import { cn } from "@/lib/utils"

export const metadata: Metadata = {
  title: "Lurus Tally — 定价方案",
  description:
    "进销存，开单记账查库存。中小店铺刚需档 ¥58/月起。",
}

const LOGIN_NEXT = SEED_TRIAL ? "/dashboard" : "/subscription"
const LOGIN_HREF = `/login?callbackUrl=${encodeURIComponent(LOGIN_NEXT)}`

export default function PricingPage() {
  return (
    <main className="min-h-screen bg-background text-foreground">
      <section className="mx-auto max-w-6xl px-6 py-24">
        <div className="mb-16 text-center">
          <h1 className="mb-4 text-4xl font-bold tracking-tight sm:text-5xl">
            Lurus Tally — 定价方案
          </h1>
          <p className="mx-auto max-w-2xl text-lg text-muted-foreground">
            进销存，开单记账查库存。
          </p>
        </div>

        <div className="grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5">
          {TALLY_PLANS.map((plan) => (
            <Card
              key={plan.code}
              className={cn(
                "relative flex flex-col",
                plan.highlight && "ring-2 ring-primary/40",
              )}
            >
              {plan.highlight && (
                <span className="absolute -top-2 right-4 rounded-full bg-primary px-2 py-0.5 text-xs font-medium text-primary-foreground">
                  推荐
                </span>
              )}
              <CardHeader>
                <CardTitle className="text-lg">{plan.name}</CardTitle>
                <CardDescription className="min-h-[2.5rem]">
                  {plan.tagline}
                </CardDescription>
                <div className="mt-3 flex items-baseline gap-1">
                  <span className="font-mono text-2xl font-semibold tabular-nums">
                    {formatPriceCNY(plan.priceCNY)}
                  </span>
                  <span className="text-xs text-muted-foreground">
                    {formatCycle(plan.cycle)}
                  </span>
                </div>
              </CardHeader>
              <CardContent className="flex-1">
                <ul className="space-y-1.5 text-sm text-muted-foreground">
                  {plan.features.map((f) => (
                    <li key={f} className="flex gap-2">
                      <span aria-hidden className="text-emerald-500">
                        ✓
                      </span>
                      <span>{f}</span>
                    </li>
                  ))}
                </ul>
              </CardContent>
              <CardFooter>
                <Link
                  href={LOGIN_HREF}
                  className={cn(
                    buttonVariants({ variant: plan.highlight ? "default" : "outline" }),
                    "w-full"
                  )}
                >
                  {plan.code === "free" ? "免费开始" : SEED_TRIAL ? "免费试用" : "登录后订阅"}
                </Link>
              </CardFooter>
            </Card>
          ))}
        </div>

        <div className="mx-auto mt-20 max-w-3xl">
          <h2 className="mb-6 text-center text-2xl font-bold">常见问题</h2>
          <div className="grid grid-cols-1 gap-6 sm:grid-cols-2 text-left">
            {SEED_TRIAL && (
              <div>
                <h3 className="mb-2 text-base font-medium">想先试用怎么办？</h3>
                <p className="text-sm leading-relaxed text-muted-foreground">
                  试用期全部功能免费，点击“免费试用”登录即可使用，正式收费前会提前通知。
                </p>
              </div>
            )}
            <div>
              <h3 className="mb-2 text-base font-medium">
                {SEED_TRIAL ? "什么时候开始收费？" : "支持哪些支付方式？"}
              </h3>
              <p className="text-sm leading-relaxed text-muted-foreground">
                {SEED_TRIAL
                  ? "试用期全部功能免费，正式收费前会提前通知。"
                  : "钱包余额、支付宝、微信支付。企业版支持企业转账、发票开具。"}
              </p>
            </div>
            <div>
              <h3 className="mb-2 text-base font-medium">能从 Excel / 店小秘迁过来吗？</h3>
              <p className="text-sm leading-relaxed text-muted-foreground">
                提供 Excel / CSV 批量导入商品，以及亚马逊、Shopify 订单导入。
                企业版含 1 对 1 数据迁移协助。
              </p>
            </div>
            <div>
              <h3 className="mb-2 text-base font-medium">会不会改乱我的库存？</h3>
              <p className="text-sm leading-relaxed text-muted-foreground">
                每次库存变动都有单据可查，谁在什么时候改了什么，都留有记录。
              </p>
            </div>
          </div>
        </div>

        <div className="mt-16 text-center">
          <p className="text-sm text-muted-foreground">
            已有 Tally 账号？{" "}
            <Link href="/login" className="text-primary hover:underline">
              立即登录
            </Link>
          </p>
        </div>
      </section>
    </main>
  )
}
