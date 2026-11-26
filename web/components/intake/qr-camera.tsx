"use client"

import { useEffect, useRef, useState } from "react"
import { Button } from "@/components/ui/button"
import { isTaxQR } from "@/lib/intake/tax-qr"
import { isWeChatBrowser } from "@/lib/wechat-env"

export interface Shot {
  qr?: string
  fileBase64?: string
  contentType: string
}

function frameOf(video: HTMLVideoElement): string | null {
  if (!video.videoWidth) return null
  const canvas = document.createElement("canvas")
  canvas.width = video.videoWidth
  canvas.height = video.videoHeight
  const ctx = canvas.getContext("2d")
  if (!ctx) return null
  ctx.drawImage(video, 0, 0)
  return canvas.toDataURL("image/jpeg", 0.85)
}

/** Reads a tax QR from the camera and keeps that frame. WeChat never opens the camera. */
export function QrCamera({ onShot }: { onShot: (shot: Shot) => void }) {
  const [open, setOpen] = useState(false)
  const [wechat, setWechat] = useState(false)
  const [note, setNote] = useState("")
  const videoRef = useRef<HTMLVideoElement>(null)
  const streamRef = useRef<MediaStream | null>(null)

  useEffect(() => { setWechat(isWeChatBrowser()) }, [])

  useEffect(() => {
    if (!open) return
    let cancelled = false
    let timer = 0
    const stopTracks = () => {
      streamRef.current?.getTracks().forEach((t) => t.stop())
      streamRef.current = null
    }
    ;(async () => {
      try {
        const stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: "environment" }, audio: false })
        if (cancelled) {
          stream.getTracks().forEach((t) => t.stop())
          return
        }
        streamRef.current = stream
        const video = videoRef.current
        if (!video) return
        video.srcObject = stream
        await video.play().catch(() => {})
        const mod = await import("zxing-wasm/reader")
        await mod.prepareZXingModule({ overrides: { locateFile: (p: string) => `/zxing/${p}` }, fireImmediately: true })
        timer = window.setInterval(async () => {
          const live = videoRef.current
          if (!live?.videoWidth) return
          const canvas = document.createElement("canvas")
          canvas.width = live.videoWidth
          canvas.height = live.videoHeight
          const ctx = canvas.getContext("2d")
          if (!ctx) return
          ctx.drawImage(live, 0, 0)
          const img = ctx.getImageData(0, 0, canvas.width, canvas.height)
          const res = await mod.readBarcodes(img, { formats: ["QRCode"], maxNumberOfSymbols: 1 })
          const text = res[0]?.text ?? ""
          if (!isTaxQR(text)) return
          const data = canvas.toDataURL("image/jpeg", 0.85)
          setOpen(false)
          onShot({ qr: text, fileBase64: data.slice(data.indexOf(",") + 1), contentType: "image/jpeg" })
        }, 400)
      } catch {
        if (!cancelled) setNote("摄像头打不开，请改用上传或粘贴二维码")
      }
    })()
    return () => {
      cancelled = true
      window.clearInterval(timer)
      stopTracks()
    }
  }, [open, onShot])

  const sendFrame = () => {
    const video = videoRef.current
    const data = video ? frameOf(video) : null
    setOpen(false)
    if (!data) return
    onShot({ fileBase64: data.slice(data.indexOf(",") + 1), contentType: "image/jpeg" })
  }

  if (wechat) return <p className="text-sm text-muted-foreground">微信里打不开摄像头。请在浏览器中打开后再拍照。</p>

  return (
    <div>
      <Button type="button" variant="outline" onClick={() => { setNote(""); setOpen(true) }}>拍照</Button>
      {note && <p className="mt-2 text-sm">{note}</p>}
      {open && (
        <div className="mt-3">
          <video ref={videoRef} className="max-h-64 w-full bg-black" muted playsInline />
          <div className="mt-2 flex gap-2">
            <Button type="button" onClick={sendFrame}>看清了就送出</Button>
            <Button type="button" variant="outline" onClick={() => setOpen(false)}>取消</Button>
          </div>
        </div>
      )}
    </div>
  )
}
