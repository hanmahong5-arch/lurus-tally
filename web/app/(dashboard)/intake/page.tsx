"use client"

import { useEffect, useState } from "react"
import { useSearchParams } from "next/navigation"
import { PageContainer } from "@/components/ui/page-container"
import { PageHeader } from "@/components/ui/page-header"
import { useRole } from "@/hooks/use-role"
import { listIntake, type IntakeDoc } from "@/lib/api/intake"
import { CaptureBar } from "@/components/intake/capture-bar"
import { ReviewDesk } from "@/components/intake/review-desk"

const STATUS: Record<string, string> = {
  received: "已收下",
  needs_review: "待核对",
  accepted: "已入账",
  rejected: "已退回",
}

function OwnList({ mark }: { mark: string }) {
  const [items, setItems] = useState<IntakeDoc[]>([])
  useEffect(() => {
    listIntake().then((r) => setItems(r.items ?? [])).catch(() => setItems([]))
  }, [])
  if (items.length === 0) return <p className="text-sm text-muted-foreground">拍好后由老板、店长或财务核对入账。</p>
  return (
    <ul className="space-y-1 text-sm">
      {items.map((d) => <li key={d.id} className={d.id === mark ? "font-medium" : undefined}>{d.invoice_no || d.parsed?.seller_name || "未识别"} · {STATUS[d.status] ?? d.status}</li>)}
    </ul>
  )
}

export default function IntakePage() {
  const { known, can } = useRole()
  const params = useSearchParams()
  const [focus, setFocus] = useState("")
  const [reloadKey, setReloadKey] = useState(0)
  useEffect(() => { setFocus(params.get("id") ?? "") }, [params])
  if (!known) return <PageContainer><p className="text-sm">正在读取身份</p></PageContainer>
  if (!can("intake.capture")) return <PageContainer><p className="text-sm">没有收取票证的权限</p></PageContainer>
  const accept = can("intake.accept")
  const openExisting = (id: string, kind: string) => { if (kind !== "invoice") setFocus(id) }
  return (
    <PageContainer width="wide">
      <PageHeader title="待核对" subtitle="先收件。金额对上以后才登记发票，库存和应收仍走原来的审核。" />
      <CaptureBar onSent={(id) => { setFocus(id); setReloadKey((n) => n + 1) }} onExisting={openExisting} />
      {accept ? <ReviewDesk focusId={focus} reloadKey={reloadKey} onDone={() => setFocus("")} /> : <OwnList mark={focus} />}
      {accept && <p className="mt-3 text-sm text-muted-foreground md:hidden">请在电脑上核对这张票。手机只负责拍清楚并送出。</p>}
    </PageContainer>
  )
}
