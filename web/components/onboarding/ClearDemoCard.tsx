"use client"

/**
 * ClearDemoCard — "清除示例数据" for the dashboard banner and the account
 * data tab. Calls clear-demo, then refreshes the page so the sample products,
 * stock and bills disappear from every figure. The shop's own records and
 * warehouses are never touched. A sample product that an approved bill still
 * uses is kept as a real product and listed so the owner can recount it.
 */

import { useState } from "react"
import { useRouter } from "next/navigation"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { ErrorBanner } from "@/components/ui/error-banner"
import { useConfirm } from "@/hooks/useConfirm"
import { clearDemo, type ConvertedProduct } from "@/lib/api/onboarding"
import { ConvertedNotice } from "./ConvertedNotice"
import { extractApiError } from "@/lib/api/errors"

interface Props {
  /** "banner": one line above the figures; "panel": full description. */
  variant?: "banner" | "panel"
  /** Called after a successful clear (e.g. to hide the surrounding UI). */
  onCleared?: () => void
}

export function ClearDemoCard({ variant = "banner", onCleared }: Props) {
  const router = useRouter()
  const confirm = useConfirm()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [action, setAction] = useState<string | null>(null)
  // Set after a demo_in_use 409: retrying would hit the same wall, so the
  // button stays off for the rest of the session.
  const [blocked, setBlocked] = useState(false)
  // Sample products kept as real ones because approved bills use them.
  const [converted, setConverted] = useState<ConvertedProduct[]>([])

  async function handleClear() {
    const ok = await confirm({
      title: "清除示例数据？",
      body: "示例商品、示例库存和示例销售单会全部删掉，你自己录的商品、单据和仓库不受影响。如果有示例商品已经被你审核过的单据用到，它会转成真实商品保留下来，请你用盘点核对数量和进价。",
      confirmText: "清除",
      cancelText: "先留着",
      danger: true,
    })
    if (!ok) return
    setBusy(true)
    setError(null)
    setAction(null)
    try {
      const result = await clearDemo()
      const kept = result?.converted_products ?? []
      setConverted(kept)
      toast.success(kept.length > 0 ? "示例数据已清除，部分商品转成了真实商品" : "示例数据已清除")
      // With kept products the card must stay mounted (the dashboard drops it
      // once has_demo turns false), so the refresh waits for "知道了".
      if (kept.length === 0) {
        onCleared?.()
        router.refresh()
      }
    } catch (err) {
      const shown = extractApiError(err)
      setError(shown.message)
      setAction(shown.action ?? null)
      if (shown.status === 409 && shown.code === "demo_in_use") setBlocked(true)
    } finally {
      setBusy(false)
    }
  }

  if (converted.length > 0) {
    return (
      <div className="space-y-2">
        <ConvertedNotice products={converted} />
        <Button size="sm" variant="outline" onClick={() => {
            onCleared?.()
            router.refresh()
          }}
        >
          知道了
        </Button>
      </div>
    )
  }

  return (
    <div
      className={
        variant === "banner"
          ? "flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border bg-muted/50 px-4 py-3"
          : "space-y-3"
      }
    >
      <p className="text-sm text-muted-foreground">
        {blocked
          ? "示例商品已经用在你自己的单据里，清除不了"
          : variant === "banner"
          ? "下面的数字里含有示例数据（示例商品和单据）。开始录真实业务前，先清掉它们。"
          : "刚开通时系统放了几个示例商品和单据，方便你熟悉操作，它们会混进库存、报表和待办里。清除只删示例内容，你自己录的数据都保留；如果有示例商品已被你审核过的单据用到，它会转成真实商品保留，请用盘点核对数量和进价。"}
      </p>
      {error && <ErrorBanner hint={action}>{error}</ErrorBanner>}
      <Button size="sm" variant="outline" onClick={handleClear} disabled={busy || blocked}>
        {busy ? "正在清除…" : "清除示例数据"}
      </Button>
    </div>
  )
}
