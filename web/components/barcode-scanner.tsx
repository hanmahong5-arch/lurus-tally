"use client"

import { useCallback, useEffect, useRef, useState } from "react"
import { ScanLine } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { useScanInput } from "@/hooks/use-scan-input"

interface Props {
  /** 命中条码(摄像头识别或手输确认)后回调,随后自动关闭。 */
  onScan: (code: string) => void
  className?: string
  label?: string
}

/** 浏览器是否具备取景能力(http 非 localhost 时 mediaDevices 不存在)。SSR 返回 false。 */
function canUseCamera(): boolean {
  return typeof navigator !== "undefined" && !!navigator.mediaDevices?.getUserMedia
}

/**
 * 「扫码」按钮 + 全屏取景对话框。无摄像头 API 时按钮不显示;
 * 权限被拒 / 无摄像头 / 解码器不可用时,对话框退化为「请手动输入条码」。
 */
export function BarcodeScanner({ onScan, className, label = "扫码" }: Props) {
  // 首次渲染恒为 false,挂载后再检测,避免 SSR 水合不一致。
  const [available, setAvailable] = useState(false)
  const [open, setOpen] = useState(false)
  const [manual, setManual] = useState(false)
  const [text, setText] = useState("")
  const videoRef = useRef<HTMLVideoElement>(null)
  const streamRef = useRef<MediaStream | null>(null)

  useEffect(() => {
    setAvailable(canUseCamera())
  }, [])

  const finish = useCallback(
    (code: string) => {
      setOpen(false)
      onScan(code)
    },
    [onScan]
  )
  const { start, stop } = useScanInput(finish)

  const release = useCallback(() => {
    stop()
    streamRef.current?.getTracks().forEach((t) => t.stop())
    streamRef.current = null
  }, [stop])

  useEffect(() => {
    if (!open) return
    let cancelled = false
    setManual(false)
    setText("")
    ;(async () => {
      try {
        const stream = await navigator.mediaDevices.getUserMedia({
          video: { facingMode: "environment" },
          audio: false,
        })
        if (cancelled) {
          stream.getTracks().forEach((t) => t.stop())
          return
        }
        streamRef.current = stream
        const video = videoRef.current
        if (!video) return
        video.srcObject = stream
        await video.play().catch(() => {})
        const ok = await start(video)
        if (!ok && !cancelled) setManual(true)
      } catch {
        if (!cancelled) setManual(true)
      }
    })()
    return () => {
      cancelled = true
      release()
    }
  }, [open, start, release])

  if (!available) return null

  return (
    <>
      <Button type="button" variant="outline" size="sm" className={className} onClick={() => setOpen(true)}>
        <ScanLine aria-hidden />
        <span>{label}</span>
      </Button>
      {open && (
        <div
          role="dialog"
          aria-modal="true"
          aria-label="扫码取景"
          className="fixed inset-0 z-[100] flex flex-col bg-black text-white"
        >
          <div className="relative flex-1">
            <video ref={videoRef} playsInline muted className="absolute inset-0 h-full w-full object-cover" />
            {!manual && (
              <div className="pointer-events-none absolute inset-0 flex items-center justify-center">
                <div className="h-40 w-72 rounded-lg border-2 border-white/80 shadow-[0_0_0_9999px_rgba(0,0,0,0.5)]" />
              </div>
            )}
            {manual && (
              <div className="absolute inset-0 flex flex-col items-center justify-center gap-3 bg-black p-6">
                <p className="text-sm">请手动输入条码</p>
                <form
                  className="flex w-full max-w-sm gap-2"
                  onSubmit={(e) => {
                    e.preventDefault()
                    const c = text.trim()
                    if (c) finish(c)
                  }}
                >
                  <Input
                    aria-label="条码"
                    autoFocus
                    value={text}
                    onChange={(e) => setText(e.target.value)}
                    className="bg-white text-black"
                  />
                  <Button type="submit" disabled={!text.trim()}>
                    确定
                  </Button>
                </form>
              </div>
            )}
          </div>
          <div className="flex items-center justify-between p-4">
            <span className="text-sm text-white/80">{manual ? "无法使用摄像头扫码" : "把条码放入框内"}</span>
            <Button type="button" variant="secondary" onClick={() => setOpen(false)}>
              关闭
            </Button>
          </div>
        </div>
      )}
    </>
  )
}
