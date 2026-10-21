"use client"

import { useEffect, useState } from "react"
import { isWeChatBrowser } from "@/lib/wechat-env"

const DISMISS_KEY = "tally-wechat-hint-dismissed"

/**
 * 微信内置浏览器顶部提示条：部分功能受限（不能安装到桌面、不能自动打印）。
 * 仅在客户端挂载后判断 UA，服务端渲染与首屏不输出任何内容，避免水合不一致。
 */
export function WeChatOpenHint() {
  const [show, setShow] = useState(false)

  useEffect(() => {
    if (!isWeChatBrowser(window.navigator.userAgent)) return
    try {
      if (window.sessionStorage.getItem(DISMISS_KEY) === "1") return
    } catch {
      // 读不到 sessionStorage 时照常提示
    }
    setShow(true)
  }, [])

  if (!show) return null

  const copyLink = async () => {
    const url = window.location.href
    try {
      await navigator.clipboard.writeText(url)
      window.alert("链接已复制，请到浏览器粘贴打开")
    } catch {
      window.prompt("请长按复制下面的链接，到浏览器打开", url)
    }
  }

  const dismiss = () => {
    try {
      window.sessionStorage.setItem(DISMISS_KEY, "1")
    } catch {
      // 写不进去就只在本页关闭
    }
    setShow(false)
  }

  return (
    <div
      role="status"
      data-testid="wechat-open-hint"
      className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-border bg-amber-50 px-3 py-2 text-xs text-amber-900 dark:bg-amber-950 dark:text-amber-100"
    >
      <span className="min-w-0 flex-1">
        微信内打开部分功能受限（无法安装到桌面、无法自动打印），建议点右上角「…」→「在浏览器打开」，或复制链接
      </span>
      <button type="button" onClick={copyLink} className="rounded border border-current px-2 py-1">
        复制链接
      </button>
      <button type="button" onClick={dismiss} className="px-2 py-1 underline">
        本次不再提示
      </button>
    </div>
  )
}
