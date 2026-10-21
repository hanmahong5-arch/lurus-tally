"use client"

import { useCallback, useEffect, useRef, useState } from "react"

/** 同一个码在此时间窗内不重复触发。 */
export const DEDUPE_MS = 1500
/** 取帧间隔。 */
export const FRAME_INTERVAL_MS = 300

type Decoder = (video: HTMLVideoElement) => Promise<string | null>

interface BarcodeDetectorLike {
  detect(source: CanvasImageSource): Promise<{ rawValue: string }[]>
}
interface BarcodeDetectorCtor {
  new (opts?: { formats?: string[] }): BarcodeDetectorLike
  getSupportedFormats?: () => Promise<string[]>
}

async function nativeDecoder(): Promise<Decoder | null> {
  const Ctor = (window as unknown as { BarcodeDetector?: BarcodeDetectorCtor }).BarcodeDetector
  if (!Ctor) return null
  try {
    const formats = (await Ctor.getSupportedFormats?.()) ?? []
    if (formats.length === 0) return null
    if (!formats.includes("ean_13") && !formats.includes("code_128")) return null
    const det = new Ctor({ formats })
    return async (video) => {
      const res = await det.detect(video)
      return res[0]?.rawValue ?? null
    }
  } catch {
    return null
  }
}

async function wasmDecoder(): Promise<Decoder | null> {
  try {
    const mod = await import("zxing-wasm/reader")
    // wasm 由本站 /zxing/ 自托管,不依赖外部 CDN。
    await mod.prepareZXingModule({
      overrides: { locateFile: (p: string) => `/zxing/${p}` },
      fireImmediately: true,
    })
    const canvas = document.createElement("canvas")
    const ctx = canvas.getContext("2d", { willReadFrequently: true })
    if (!ctx) return null
    return async (video) => {
      if (!video.videoWidth || !video.videoHeight) return null
      canvas.width = video.videoWidth
      canvas.height = video.videoHeight
      ctx.drawImage(video, 0, 0)
      const img = ctx.getImageData(0, 0, canvas.width, canvas.height)
      const res = await mod.readBarcodes(img, { maxNumberOfSymbols: 1 })
      return res[0]?.text ?? null
    }
  } catch {
    return null
  }
}

/**
 * 摄像头扫码解码。优先浏览器原生 BarcodeDetector(需支持 ean_13 / code_128),
 * 否则动态加载 zxing-wasm;两者都不可用时 status 为 "unsupported",调用方回落手输。
 */
export function useScanInput(onDetect?: (code: string) => void) {
  const [supported, setSupported] = useState<boolean | null>(null)
  const [lastResult, setLastResult] = useState<string | null>(null)
  const timer = useRef<ReturnType<typeof setInterval> | null>(null)
  const busy = useRef(false)
  const last = useRef<{ code: string; at: number } | null>(null)
  const onDetectRef = useRef(onDetect)
  const session = useRef(0)

  useEffect(() => {
    onDetectRef.current = onDetect
  }, [onDetect])

  const stop = useCallback(() => {
    session.current++
    if (timer.current) clearInterval(timer.current)
    timer.current = null
    busy.current = false
  }, [])

  const start = useCallback(
    async (video: HTMLVideoElement): Promise<boolean> => {
      stop()
      const my = session.current
      const decode = (await nativeDecoder()) ?? (await wasmDecoder())
      if (my !== session.current) return false // 已被 stop / 再次 start 取代
      if (!decode) {
        setSupported(false)
        return false
      }
      setSupported(true)
      timer.current = setInterval(async () => {
        if (busy.current || my !== session.current) return
        busy.current = true
        try {
          const code = await decode(video)
          if (!code || my !== session.current) return
          const now = Date.now()
          if (last.current && last.current.code === code && now - last.current.at < DEDUPE_MS) return
          last.current = { code, at: now }
          if (typeof navigator !== "undefined" && typeof navigator.vibrate === "function") {
            navigator.vibrate(60)
          }
          setLastResult(code)
          onDetectRef.current?.(code)
        } catch {
          // 单帧解码失败忽略,下一帧再试
        } finally {
          busy.current = false
        }
      }, FRAME_INTERVAL_MS)
      return true
    },
    [stop]
  )

  useEffect(() => stop, [stop])

  const status = supported === false ? "unsupported" : supported ? "ready" : "idle"
  return { supported, status, start, stop, lastResult } as const
}
