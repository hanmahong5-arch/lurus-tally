"use client"

import { useEffect, useState } from "react"
import Link from "next/link"
import { toast } from "sonner"

import { getSubscriptionLockState, getSubscriptionState } from "@/lib/api/billing"
import { useRole } from "@/hooks/use-role"

const REFRESH_MS = 5 * 60 * 1000

/**
 * SubscriptionBanner: a permanent strip at the top of the back office while the
 * plan has lapsed and the shop is read-only. The owner gets a renew button;
 * everyone else is told to ask the owner. Any failure to read the status shows
 * nothing, because a platform hiccup must never look like a lock.
 */
export function SubscriptionBanner() {
  const { can } = useRole()
  const [locked, setLocked] = useState(false)
  const [checking, setChecking] = useState(false)

  useEffect(() => {
    let alive = true
    const load = () => {
      getSubscriptionLockState()
        .then((st) => {
          if (alive) setLocked(st.locked === true)
        })
        .catch(() => undefined)
    }
    load()
    const timer = setInterval(load, REFRESH_MS)
    // The payment return page asks the platform itself; re-read the result here.
    window.addEventListener("tally:subscription-refreshed", load)
    return () => {
      alive = false
      clearInterval(timer)
      window.removeEventListener("tally:subscription-refreshed", load)
    }
  }, [])

  async function recheck() {
    setChecking(true)
    try {
      const st = await getSubscriptionState({ refresh: true })
      if (st.locked === true) {
        toast.info("暂未查到续费记录，请稍后再试")
      } else {
        setLocked(false)
        toast.success("套餐已恢复")
      }
    } catch {
      toast.error("暂时无法确认套餐状态，请稍后再试")
    } finally {
      setChecking(false)
    }
  }

  if (!locked) return null
  const isOwner = can("billing.manage")
  return (
    <div
      role="alert"
      className="flex flex-wrap items-center justify-center gap-3 bg-amber-100 px-4 py-2 text-sm text-amber-950"
    >
      <span>套餐已到期，现在只能查看和导出，续费后恢复开单。</span>
      <button
        type="button"
        onClick={recheck}
        disabled={checking}
        className="rounded-md border border-amber-900 px-3 py-1 text-xs font-medium hover:bg-amber-200 disabled:opacity-60"
      >
        {checking ? "正在确认…" : "我已续费，刷新状态"}
      </button>
      {isOwner ? (
        <Link
          href="/subscription"
          className="rounded-md bg-amber-900 px-3 py-1 text-xs font-medium text-amber-50 hover:bg-amber-800"
        >
          续费
        </Link>
      ) : (
        <span className="font-medium">请联系老板续费</span>
      )}
    </div>
  )
}
