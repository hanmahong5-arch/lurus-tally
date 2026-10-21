"use client"

import Link from "next/link"

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { useProfile } from "@/lib/profile"

/** 汇率历史 only matters to cross-border shops; everyone else never sees it. */
export function ExchangeRateCard() {
  const { profileType } = useProfile()
  if (profileType !== "cross_border" && profileType !== "hybrid") return null
  return (
    <section>
      <h2 className="mb-3 text-sm font-medium">其他报表</h2>
      <Link href="/finance/exchange-rates">
        <Card className="transition-colors hover:bg-muted/50">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <span aria-hidden="true">💱</span>
              汇率历史
            </CardTitle>
            <CardDescription>跨境结汇所需的美元 / 欧元 / 港币历史曲线。</CardDescription>
          </CardHeader>
          <CardContent />
        </Card>
      </Link>
    </section>
  )
}
