import { describe, it, expect } from "vitest"
import { renderHook } from "@testing-library/react"
import { ApiError, NetworkError } from "@/lib/api/errors"
import { useSubmitKey } from "./submit-key"

describe("useSubmitKey", () => {
  it("keeps the key after a NetworkError when the payload is unchanged", () => {
    const { result } = renderHook(() => useSubmitKey())
    const first = result.current.keyFor({ a: 1 })
    result.current.failed(new NetworkError("network", "boom"))
    expect(result.current.keyFor({ a: 1 })).toBe(first)
  })

  it("issues a new key after an HTTP error response", () => {
    const { result } = renderHook(() => useSubmitKey())
    const first = result.current.keyFor({ a: 1 })
    result.current.failed(new ApiError(422, "warehouse_required", "请选择仓库"))
    expect(result.current.keyFor({ a: 1 })).not.toBe(first)
  })

  it("issues a new key when the payload changed after a NetworkError", () => {
    const { result } = renderHook(() => useSubmitKey())
    const first = result.current.keyFor({ a: 1 })
    result.current.failed(new NetworkError("offline", "offline"))
    expect(result.current.keyFor({ a: 2 })).not.toBe(first)
  })

  it("resets after success", () => {
    const { result } = renderHook(() => useSubmitKey())
    const first = result.current.keyFor({ a: 1 })
    result.current.succeeded()
    expect(result.current.keyFor({ a: 1 })).not.toBe(first)
  })

  it.each([502, 503, 504])("keeps the key after a gateway %i", (status) => {
    const { result } = renderHook(() => useSubmitKey())
    const first = result.current.keyFor({ a: 1 })
    result.current.failed(new ApiError(status, "gateway", "gateway"))
    expect(result.current.keyFor({ a: 1 })).toBe(first)
  })
})
