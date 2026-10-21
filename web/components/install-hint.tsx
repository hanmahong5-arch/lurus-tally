"use client"

import { useEffect, useState } from "react"
import { isWeChatBrowser } from "@/lib/wechat-env"

interface InstallPromptEvent extends Event {
  prompt: () => Promise<void>
  userChoice?: Promise<{ outcome: "accepted" | "dismissed" }>
}

function isStandalone(): boolean {
  const nav = window.navigator as Navigator & { standalone?: boolean }
  return (
    (typeof window.matchMedia === "function" && window.matchMedia("(display-mode: standalone)").matches) ||
    nav.standalone === true
  )
}

function isIosSafari(): boolean {
  const ua = window.navigator.userAgent
  const ios = /iPhone|iPad|iPod/.test(ua)
  const otherBrowser = /CriOS|FxiOS|EdgiOS|OPiOS/.test(ua)
  return ios && !otherBrowser && /Safari/.test(ua)
}

/**
 * "安装到手机桌面" entry. Three states: a button when the browser offers an install prompt,
 * a text hint on iOS Safari, nothing when already installed or not installable.
 */
export function InstallHint() {
  const [standalone, setStandalone] = useState(true)
  const [ios, setIos] = useState(false)
  const [wechat, setWechat] = useState(false)
  const [prompt, setPrompt] = useState<InstallPromptEvent | null>(null)

  useEffect(() => {
    setStandalone(isStandalone())
    setIos(isIosSafari())
    setWechat(isWeChatBrowser(window.navigator.userAgent))
    const onPrompt = (e: Event) => {
      e.preventDefault()
      setPrompt(e as InstallPromptEvent)
    }
    const onInstalled = () => {
      setPrompt(null)
      setStandalone(true)
    }
    window.addEventListener("beforeinstallprompt", onPrompt)
    window.addEventListener("appinstalled", onInstalled)
    return () => {
      window.removeEventListener("beforeinstallprompt", onPrompt)
      window.removeEventListener("appinstalled", onInstalled)
    }
  }, [])

  if (standalone) return null

  // 微信内置浏览器不能安装到桌面，也不会触发安装事件：只给文字引导。
  if (wechat) {
    return (
      <p
        data-testid="install-wechat-hint"
        className="rounded-md border border-border bg-card px-3 py-2 text-xs text-muted-foreground"
      >
        微信内无法安装，请在浏览器打开后再安装
      </p>
    )
  }

  if (prompt) {
    return (
      <button
        type="button"
        data-testid="install-button"
        onClick={async () => {
          const p = prompt
          await p.prompt()
          // Keep the entry if the user dismissed the system dialog; appinstalled hides it after a real install.
          const choice = await p.userChoice
          if (choice?.outcome === "accepted") setPrompt(null)
        }}
        className="block w-full rounded-md border border-border bg-card px-3 py-2 text-center text-xs hover:bg-muted"
      >
        安装到桌面
      </button>
    )
  }

  if (ios) {
    return (
      <p
        data-testid="install-ios-hint"
        className="rounded-md border border-border bg-card px-3 py-2 text-xs text-muted-foreground"
      >
        安装到手机桌面：点浏览器底部的「分享」按钮，再选「添加到主屏幕」。
      </p>
    )
  }

  return null
}
