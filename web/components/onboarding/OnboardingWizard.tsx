"use client"

/**
 * OnboardingWizard — client-side step wizard shown after persona selection.
 *
 * Steps:
 *  1 (seed)      — resolves the default warehouse, then calls seed-demo; or
 *                  the owner skips straight to their own data / imports it
 *  2 (replenish) — navigates to /replenish?onboarding=1; sample data can be
 *                  cleared right here
 *  3 (done)      — fires onboarding_first_po_exported telemetry (called by
 *                  the replenish page via notifyFirstPoExported)
 *
 * localStorage key: "tally_onboarding_step"
 *   Values: "seed" | "replenish" | "done"
 */

import { useState, useEffect } from "react"
import { useRouter } from "next/navigation"
import { Button } from "@/components/ui/button"
import { ErrorBanner } from "@/components/ui/error-banner"
import { useConfirm } from "@/hooks/useConfirm"
import { ProductImportDialog } from "@/components/product-import-dialog"
import { clearDemo, seedDemo, type ConvertedProduct } from "@/lib/api/onboarding"
import { ConvertedNotice } from "./ConvertedNotice"
import { extractApiError } from "@/lib/api/errors"
import { listWarehouses } from "@/lib/api/warehouses"
import { trackEvent } from "@/lib/telemetry"
import type { OnboardingPersona } from "@/lib/api/onboarding"

const STORAGE_KEY = "tally_onboarding_step"
const SIGNUP_TS_KEY = "tally_signup_ts"

type Step = "seed" | "replenish" | "done"

interface Props {
  persona: OnboardingPersona
}

/**
 * notifyFirstPoExported — called by the replenish page when the user exports
 * their first PO. Fires the telemetry event and marks onboarding complete.
 */
export function notifyFirstPoExported(): void {
  const signupTs = Number(localStorage.getItem(SIGNUP_TS_KEY) ?? "0")
  const ageMinutes = signupTs > 0 ? Math.round((Date.now() - signupTs) / 60_000) : 0
  trackEvent("onboarding_first_po_exported", {
    tenant_age_minutes: ageMinutes,
    export_format: "csv",
  })
  localStorage.setItem(STORAGE_KEY, "done")
}

function StepIndicator({ current }: { current: Step }) {
  const steps: { key: Step; label: string }[] = [
    { key: "seed", label: "载入示例商品" },
    { key: "replenish", label: "查看补货建议" },
    { key: "done", label: "生成第一张进货单" },
  ]
  const indexOf = (s: Step) => steps.findIndex((x) => x.key === s)
  const currentIdx = indexOf(current)

  return (
    <ol className="flex flex-wrap items-center gap-y-2">
      {steps.map((s, i) => {
        const done = i < currentIdx
        const active = i === currentIdx
        return (
          <li key={s.key} className="flex items-center">
            <span
              className={[
                "flex h-7 w-7 items-center justify-center rounded-full text-xs font-semibold select-none",
                done
                  ? "bg-primary text-primary-foreground"
                  : active
                    ? "border-2 border-primary text-primary"
                    : "border border-muted-foreground/30 text-muted-foreground",
              ].join(" ")}
            >
              {done ? "✓" : i + 1}
            </span>
            <span
              className={[
                "ml-2 text-sm",
                active ? "font-semibold text-foreground" : "text-muted-foreground",
              ].join(" ")}
            >
              {s.label}
            </span>
            {i < steps.length - 1 && (
              <span className="mx-4 h-px w-10 bg-muted-foreground/20" aria-hidden />
            )}
          </li>
        )
      })}
    </ol>
  )
}

export default function OnboardingWizard({ persona }: Props) {
  const router = useRouter()
  const [step, setStep] = useState<Step>("seed")
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [errorAction, setErrorAction] = useState<string | null>(null)
  const confirm = useConfirm()
  const [importOpen, setImportOpen] = useState(false)
  const [seededCount, setSeededCount] = useState<number | null>(null)
  // Sample products kept as real ones because approved bills use them.
  const [converted, setConverted] = useState<ConvertedProduct[]>([])

  // Restore step from localStorage so page refreshes don't restart the wizard.
  useEffect(() => {
    const saved = localStorage.getItem(STORAGE_KEY) as Step | null
    if (saved === "replenish") {
      setStep("replenish")
    }
    if (!localStorage.getItem(SIGNUP_TS_KEY)) {
      localStorage.setItem(SIGNUP_TS_KEY, String(Date.now()))
    }
  }, [])

  async function resolveWarehouseId(): Promise<string> {
    const result = await listWarehouses({ limit: 1 })
    if (result.items.length > 0) {
      return result.items[0].id
    }
    // No warehouse exists yet — seed-demo cannot proceed without one.
    throw new Error("请先在左侧「仓库」里新建一个仓库，再载入示例商品。")
  }

  async function handleSeed() {
    setLoading(true)
    setError(null)
    try {
      const warehouseId = await resolveWarehouseId()
      const result = await seedDemo(persona, warehouseId)
      setSeededCount(result.products_created)
      localStorage.setItem(STORAGE_KEY, "replenish")
      setStep("replenish")
    } catch (err) {
      setError(extractApiError(err).message)
    } finally {
      setLoading(false)
    }
  }

  /** Leave the sample data alone and start on the shop's own records. */
  function finish(to: string) {
    localStorage.setItem(STORAGE_KEY, "done")
    router.push(to)
  }

  async function handleClearAndStart() {
    const ok = await confirm({
      title: "清除示例数据？",
      body: "示例商品、示例库存和示例销售单会全部删掉，你自己录的商品、单据和仓库不受影响。如果有示例商品已经被你审核过的单据用到，它会转成真实商品保留下来，请你用盘点核对数量和进价。",
      confirmText: "清除",
      cancelText: "先留着",
      danger: true,
    })
    if (!ok) return
    setLoading(true)
    setError(null)
    setErrorAction(null)
    try {
      const result = await clearDemo()
      const kept = result?.converted_products ?? []
      if (kept.length > 0) {
        setConverted(kept)
      } else {
        finish("/dashboard")
      }
    } catch (err) {
      const shown = extractApiError(err)
      setError(shown.message)
      setErrorAction(shown.action ?? null)
    } finally {
      setLoading(false)
    }
  }

  function handleGoReplenish() {
    router.push("/replenish?onboarding=1")
  }

  return (
    <div className="space-y-8">
      <StepIndicator current={step} />
      <ProductImportDialog open={importOpen} onOpenChange={setImportOpen} onImported={() => finish("/products")} />

      {step === "seed" && (
        <div className="rounded-lg border bg-card p-6 space-y-4 max-w-lg">
          <h2 className="text-lg font-semibold">第 1 步：先看看示例，还是直接录自己的货？</h2>
          <p className="text-sm text-muted-foreground">
            想先体验一下，我们帮你建几个示例商品和库存，其中有一个
            <span className="font-medium text-foreground">库存偏低的商品</span>
            ，可以马上看到补货建议。示例数据随时可以一键清除，不会混进你的真实账。
          </p>
          {error && <ErrorBanner hint={errorAction}>{error}</ErrorBanner>}
          <div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap">
            <Button onClick={handleSeed} disabled={loading}>
              {loading ? "正在载入…" : "载入示例商品"}
            </Button>
            <Button variant="outline" onClick={() => setImportOpen(true)} disabled={loading}>
              导入我的商品
            </Button>
            <Button variant="ghost" onClick={() => finish("/dashboard")} disabled={loading}>
              跳过，直接录我的数据
            </Button>
          </div>
        </div>
      )}

      {step === "replenish" && (
        <div className="rounded-lg border bg-card p-6 space-y-4 max-w-lg">
          <h2 className="text-lg font-semibold">
            {seededCount !== null ? `已建好 ${seededCount} 个示例商品 ✓` : "示例商品已就绪 ✓"}
          </h2>
          <p className="text-sm text-muted-foreground">
            其中有一个库存偏低的商品，系统已经算出该补多少货。去补货页确认并导出，
            就得到了你的第一张进货单。
          </p>
          {error && <ErrorBanner hint={errorAction}>{error}</ErrorBanner>}
          {converted.length > 0 && <ConvertedNotice products={converted} />}
          <div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap">
            {converted.length > 0 ? (
              <Button onClick={() => finish("/stock")}>去库存页盘点</Button>
            ) : (
              <>
                <Button onClick={handleGoReplenish} disabled={loading}>
                  前往查看补货建议 →
                </Button>
                <Button variant="outline" onClick={handleClearAndStart} disabled={loading}>
                  {loading ? "正在清除…" : "清除示例，开始录我的数据"}
                </Button>
              </>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
