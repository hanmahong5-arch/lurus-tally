"use client"

import { useEffect } from "react"

interface CheckoutSuccessProps {
  billNo: string
  totalAmount: string
  onDismiss: () => void
}

/**
 * CheckoutSuccess displays a full-screen overlay confirming a successful checkout.
 * Auto-dismisses after 1 second. Also provides a manual close button.
 */
export function CheckoutSuccess({ billNo, totalAmount, onDismiss }: CheckoutSuccessProps) {
  useEffect(() => {
    const timer = setTimeout(onDismiss, 1000)
    return () => clearTimeout(timer)
  }, [onDismiss])

  return (
    <div className="fixed inset-0 z-50 flex flex-col items-center justify-center bg-emerald-500">
      {/* Check icon */}
      <div className="mb-6">
        <svg
          xmlns="http://www.w3.org/2000/svg"
          viewBox="0 0 24 24"
          fill="none"
          stroke="white"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          className="h-24 w-24"
          aria-hidden="true"
        >
          <circle cx="12" cy="12" r="10" />
          <path d="M9 12l2 2 4-4" />
        </svg>
      </div>

      {/* Amount */}
      <div className="mb-2 text-5xl font-bold tabular-nums text-white">
        ¥{totalAmount}
      </div>
      <div className="mb-4 text-xl text-emerald-100">已收款</div>

      {/* Bill number */}
      <div className="mb-8 font-mono text-sm text-emerald-200">{billNo}</div>

      {/* Close button */}
      <button
        onClick={onDismiss}
        className="rounded-lg border border-white/50 px-6 py-2 text-sm font-medium text-white hover:bg-white/20 transition-colors"
        aria-label="关闭成功提示"
      >
        关闭
      </button>
    </div>
  )
}
