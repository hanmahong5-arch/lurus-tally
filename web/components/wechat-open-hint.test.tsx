import { describe, it, expect, vi, afterEach, beforeEach } from "vitest"
import { render, screen, fireEvent, act } from "@testing-library/react"
import { WeChatOpenHint } from "./wechat-open-hint"

function setUA(ua: string) {
  Object.defineProperty(window.navigator, "userAgent", { value: ua, configurable: true })
}
const WECHAT_UA = "Mozilla/5.0 (iPhone) Mobile/15E148 MicroMessenger/8.0.40"

beforeEach(() => window.sessionStorage.clear())
afterEach(() => setUA("Mozilla/5.0 (X11; Linux x86_64) Chrome/120"))

describe("WeChatOpenHint", () => {
  it("非微信浏览器不显示", () => {
    setUA("Mozilla/5.0 (X11; Linux x86_64) Chrome/120")
    render(<WeChatOpenHint />)
    expect(screen.queryByTestId("wechat-open-hint")).toBeNull()
  })

  it("微信内显示提示条，点「本次不再提示」后关闭并记入会话", () => {
    setUA(WECHAT_UA)
    const { unmount } = render(<WeChatOpenHint />)
    expect(screen.getByTestId("wechat-open-hint").textContent).toContain("在浏览器打开")
    fireEvent.click(screen.getByText("本次不再提示"))
    expect(screen.queryByTestId("wechat-open-hint")).toBeNull()
    unmount()
    render(<WeChatOpenHint />)
    expect(screen.queryByTestId("wechat-open-hint")).toBeNull()
  })

  it("复制链接优先走剪贴板，失败时回落 prompt", async () => {
    setUA(WECHAT_UA)
    const alertSpy = vi.spyOn(window, "alert").mockImplementation(() => {})
    const promptSpy = vi.spyOn(window, "prompt").mockImplementation(() => null)
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true })
    render(<WeChatOpenHint />)
    await act(async () => {
      fireEvent.click(screen.getByText("复制链接"))
    })
    expect(writeText).toHaveBeenCalledTimes(1)
    expect(promptSpy).not.toHaveBeenCalled()

    writeText.mockRejectedValueOnce(new Error("denied"))
    await act(async () => {
      fireEvent.click(screen.getByText("复制链接"))
    })
    expect(promptSpy).toHaveBeenCalledTimes(1)
    alertSpy.mockRestore()
    promptSpy.mockRestore()
  })
})
