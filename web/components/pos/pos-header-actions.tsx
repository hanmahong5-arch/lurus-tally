"use client"

import Link from "next/link"
import { useEffect } from "react"
import { clearSwitchMarker, signOutForSwitch } from "@/components/account/switch-user"
import { useSignOutBroadcast } from "@/hooks/use-signout-broadcast"
import { useConfirm } from "@/hooks/useConfirm"
import { useRole } from "@/hooks/use-role"
import { useQueueKey } from "@/hooks/use-checkout-queue"
import { countUnsent } from "@/lib/pos/checkout-queue"

const BUTTON_CLASS =
  "inline-flex min-h-[36px] items-center rounded-md px-3 py-1.5 text-xs text-zinc-500 hover:bg-zinc-200 hover:text-zinc-900 dark:hover:bg-zinc-800 dark:hover:text-zinc-100 transition-colors"

/** Back to the back office; a cashier has no back office, so the link is not shown. */
export function BackOfficeLink() {
  const { role } = useRole()
  if (role === "cashier") return null
  return (
    <Link href="/dashboard" className={BUTTON_CLASS}>
      退出 POS ←
    </Link>
  )
}

/** Ends the login so the next person at a shared till signs in as themselves. */
export function SignOutButton() {
  const confirm = useConfirm()
  const { role } = useRole()
  const { key: queueKey } = useQueueKey()
  useSignOutBroadcast()
  useEffect(() => {
    // Someone is signed in again, so the hand-over marker must not greet the next sign-in.
    clearSwitchMarker()
  }, [])
  return (
    <button
      type="button"
      onClick={async () => {
        // 离线结账还没上传的，退出前提醒；队列按人存放，不随退出清除，下次登录继续上传。
        const unsent = queueKey ? await countUnsent(queueKey).catch(() => 0) : 0
        if (unsent > 0) {
          const ok = await confirm({
            title: "还有离线结账没上传",
            body: `有 ${unsent} 笔离线结账尚未上传，退出后将在下次登录继续上传，确定退出？`,
            confirmText: "确定退出",
            cancelText: "先不退出",
            danger: true,
          })
          if (!ok) return
        }
        await signOutForSwitch(confirm, role)
      }}
      className={BUTTON_CLASS}
    >
      退出登录
    </button>
  )
}
