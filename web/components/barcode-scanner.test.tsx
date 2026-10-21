import { describe, it, expect, vi, afterEach } from "vitest"
import { render, screen, fireEvent, waitFor } from "@testing-library/react"
import { BarcodeScanner } from "./barcode-scanner"

vi.mock("zxing-wasm/reader", () => {
  throw new Error("load failed")
})

afterEach(() => {
  vi.unstubAllGlobals()
  Object.defineProperty(navigator, "mediaDevices", { value: undefined, configurable: true })
})

function setMedia(getUserMedia?: () => Promise<unknown>) {
  Object.defineProperty(navigator, "mediaDevices", {
    value: getUserMedia ? { getUserMedia } : undefined,
    configurable: true,
  })
}

describe("BarcodeScanner", () => {
  it("不支持 getUserMedia 时不渲染按钮", () => {
    setMedia(undefined)
    render(<BarcodeScanner onScan={vi.fn()} />)
    expect(screen.queryByRole("button", { name: "扫码" })).not.toBeInTheDocument()
  })

  it("权限被拒时显示手输并可提交条码", async () => {
    setMedia(() => Promise.reject(new DOMException("denied", "NotAllowedError")))
    const onScan = vi.fn()
    render(<BarcodeScanner onScan={onScan} />)
    fireEvent.click(await screen.findByRole("button", { name: "扫码" }))
    expect(await screen.findByText("请手动输入条码")).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText("条码"), { target: { value: "ABC123456" } })
    fireEvent.click(screen.getByRole("button", { name: "确定" }))
    await waitFor(() => expect(onScan).toHaveBeenCalledWith("ABC123456"))
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
  })
})
