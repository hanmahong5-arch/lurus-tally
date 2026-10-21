import { describe, it, expect, vi, afterEach } from "vitest"
import { renderHook, act } from "@testing-library/react"

// 模拟动态加载 zxing-wasm 失败。
vi.mock("zxing-wasm/reader", () => {
  throw new Error("load failed")
})

import { useScanInput } from "./use-scan-input"

afterEach(() => {
  vi.unstubAllGlobals()
})

describe("useScanInput", () => {
  it("无 BarcodeDetector 且 wasm 加载失败时返回 unsupported", async () => {
    const { result } = renderHook(() => useScanInput())
    let ok = true
    await act(async () => {
      ok = await result.current.start(document.createElement("video"))
    })
    expect(ok).toBe(false)
    expect(result.current.supported).toBe(false)
    expect(result.current.status).toBe("unsupported")
  })

  it("BarcodeDetector 不含 ean_13 / code_128 时回落并最终 unsupported", async () => {
    class Fake {
      static getSupportedFormats = async () => ["qr_code"]
      detect = async () => []
    }
    vi.stubGlobal("BarcodeDetector", Fake)
    const { result } = renderHook(() => useScanInput())
    await act(async () => {
      await result.current.start(document.createElement("video"))
    })
    expect(result.current.status).toBe("unsupported")
  })

  it("原生 BarcodeDetector 命中后回调,1.5 秒内同码去重", async () => {
    class Fake {
      static getSupportedFormats = async () => ["ean_13"]
      detect = async () => [{ rawValue: "6901234567892" }]
    }
    vi.stubGlobal("BarcodeDetector", Fake)
    vi.useFakeTimers()
    try {
      const onDetect = vi.fn()
      const { result } = renderHook(() => useScanInput(onDetect))
      await act(async () => {
        await result.current.start(document.createElement("video"))
      })
      expect(result.current.supported).toBe(true)
      await act(async () => {
        await vi.advanceTimersByTimeAsync(300)
      })
      expect(onDetect).toHaveBeenCalledTimes(1)
      expect(onDetect).toHaveBeenCalledWith("6901234567892")
      await act(async () => {
        await vi.advanceTimersByTimeAsync(900)
      })
      expect(onDetect).toHaveBeenCalledTimes(1)
      await act(async () => {
        await vi.advanceTimersByTimeAsync(900)
      })
      expect(onDetect).toHaveBeenCalledTimes(2)
      act(() => result.current.stop())
    } finally {
      vi.useRealTimers()
    }
  })
})
