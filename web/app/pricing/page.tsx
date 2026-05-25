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
import { Button } from "@/components/ui/button"
import { TALLY_PLANS, formatCycle, formatPriceCNY } from "@/lib/billing/plans"
import { cn } from "@/lib/utils"

export const metadata: Metadata = {
  title: "Lurus Tally — 定价方案",
  description:
    "AI-native 智能进销存。⌘K + AI 助手 + 多仓库。中小企业刚需档 ¥199/月起。",
}

const LOGIN_NEXT = "/subscription"
const LOGIN_HREF = `/login?next=${encodeURIComponent(LOGIN_NEXT)}`

export default function PricingPage() {
  return (
    <main className="min-h-screen bg-background text-foreground">
      <section className="mx-auto max-w-6xl px-6 py-24">
        <div className="mb-16 text-center">
          <h1 className="mb-4 text-4xl font-bold tracking-tight sm:text-5xl">
            Lurus Tally — 定价方案
          </h1>
          <p className="mx-auto max-w-2xl text-lg text-muted-foreground">
            AI-native 智能进销存：⌘K Command Palette · AI 进货建议 ·
            多仓库 · 中小企业刚需档。前 10 客户免费 90 天。
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
                <div className="mt-4 rounded-md border border-border bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
                  <div>
                    SKU 上限：
                    <span className="font-mono">
                      {plan.caps.skus === -1 ? "不限" : plan.caps.skus}
                    </span>
                  </div>
                  <div>
                    用户：
                    <span className="font-mono">
                      {plan.caps.users === -1 ? "不限" : plan.caps.users}
                    </span>
                    {" · "}仓库：
                    <span className="font-mono">
                      {plan.caps.warehouses === -1
                        ? "不限"
                        : plan.caps.warehouses}
                    </span>
                  </div>
                </div>
              </CardContent>
              <CardFooter>
                <Button asChild variant={plan.highlight ? "default" : "outline"} className="w-full">
                  <Link href={LOGIN_HREF}>
                    {plan.code === "free" ? "免费开始" : "登录后订阅"}
                  </Link>
                </Button>
              </CardFooter>
            </Card>
          ))}
        </div>

        <div className="mx-auto mt-20 max-w-3xl">
          <h2 className="mb-6 text-center text-2xl font-bold">常见问题</h2>
          <div className="grid grid-cols-1 gap-6 sm:grid-cols-2 text-left">
            <div>
              <h3 className="mb-2 text-base font-medium">前 10 客户免费 90 天是什么？</h3>
              <p className="text-sm leading-relaxed text-muted-foreground">
                跨境电商 FBA 周期 60-90 天，给足够时间看到 AI 补货建议的真实
                效果。完成 onboarding 即可激活。
              </p>
            </div>
            <div>
              <h3 className="mb-2 text-base font-medium">支持哪些支付方式？</h3>
              <p className="text-sm leading-relaxed text-muted-foreground">
                钱包余额、支付宝、微信支付。企业版支持企业转账、发票开具。
              </p>
            </div>
            <div>
              <h3 className="mb-2 text-base font-medium">能从 Excel / 店小秘迁过来吗？</h3>
              <p className="text-sm leading-relaxed text-muted-foreground">
                提供 Excel / CSV 批量导入；店小秘 / 马帮的常见字段已预置映射。
                企业版含 1 对 1 数据迁移协助。
              </p>
            </div>
            <div>
              <h3 className="mb-2 text-base font-medium">AI 会乱改我的库存吗？</h3>
              <p className="text-sm leading-relaxed text-muted-foreground">
                所有 AI 写操作 Preview Before Execute，30 秒内可 Cmd+Z 撤销，
                完整审计日志。AI 不是卖点，自动化才是。
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
