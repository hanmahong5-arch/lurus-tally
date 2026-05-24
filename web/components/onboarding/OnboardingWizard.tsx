"use client"

/**
 * OnboardingWizard — client-side step wizard shown after persona selection.
 *
 * Steps:
 *  1 (seed)      — resolves the default warehouse, then calls seed-demo
 *  2 (replenish) — navigates to /replenish?onboarding=1
 *  3 (done)      — fires onboarding_first_po_exported telemetry (called by
 *                  the replenish page via notifyFirstPoExported)
 *
 * localStorage key: "tally_onboarding_step"
 *   Values: "seed" | "replenish" | "done"
 */

import { useState, useEffect } from "react"
import { useRouter } from "next/navigation"
import { Button } from "@/components/ui/button"
import { seedDemo } from "@/lib/api/onboarding"
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
    { key: "seed", label: "种入示例数据" },
    { key: "replenish", label: "查看补货建议" },
    { key: "done", label: "生成第一张采购单" },
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
  const [seededCount, setSeededCount] = useState<number | null>(null)

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
    throw new Error("请先在设置中创建至少一个仓库，再进行示例数据种入。")
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
      const msg = err instanceof Error ? err.message : "示例数据种入失败，请稍后重试。"
      setError(msg)
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

      {step === "seed" && (
        <div className="rounded-lg border bg-card p-6 space-y-4 max-w-lg">
          <h2 className="text-lg font-semibold">第 1 步：种入示例数据</h2>
          <p className="text-sm text-muted-foreground">
            我们将为你创建几个示例商品和库存，其中包含一个{" "}
            <span className="font-medium text-foreground">低库存商品</span>
            ，让你立刻体验 AI 补货建议。示例数据可在完成后一键清除，不影响真实业务数据。
          </p>
          {error && <p className="text-sm text-destructive">{error}</p>}
          <Button onClick={handleSeed} disabled={loading} className="w-full sm:w-auto">
            {loading ? "正在种入…" : "种入示例数据"}
          </Button>
        </div>
      )}

      {step === "replenish" && (
        <div className="rounded-lg border bg-card p-6 space-y-4 max-w-lg">
          <h2 className="text-lg font-semibold">
            {seededCount !== null ? `已创建 ${seededCount} 个示例商品 ✓` : "示例数据已就绪 ✓"}
          </h2>
          <p className="text-sm text-muted-foreground">
            其中有一个低库存商品，AI 已生成补货建议。前往补货页接受建议并导出采购单——
            这是你在 Tally 的第一张 PO，也是核心 aha 时刻。
          </p>
          <Button onClick={handleGoReplenish} className="w-full sm:w-auto">
            前往查看补货建议 →
          </Button>
        </div>
      )}
    </div>
  )
}
