import { describe, it, expect, vi, afterEach } from "vitest"
import { render, screen, act, fireEvent } from "@testing-library/react"
import { InstallHint } from "./install-hint"

const IOS_UA =
  "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"

function setUA(ua: string) {
  Object.defineProperty(window.navigator, "userAgent", { value: ua, configurable: true })
}
function setStandalone(on: boolean) {
  window.matchMedia = vi.fn().mockImplementation((q: string) => ({
    matches: on && q.includes("standalone"),
    media: q,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  })) as unknown as typeof window.matchMedia
}

afterEach(() => setUA("Mozilla/5.0 (X11; Linux x86_64) Chrome/120"))

describe("InstallHint", () => {
  it("shows the install button after beforeinstallprompt and calls prompt()", async () => {
    setUA("Mozilla/5.0 (Linux; Android 13) Chrome/120 Mobile")
    setStandalone(false)
    render(<InstallHint />)
    expect(screen.queryByTestId("install-button")).toBeNull()
    const ev = Object.assign(new Event("beforeinstallprompt", { cancelable: true }), {
      prompt: vi.fn().mockResolvedValue(undefined),
    })
    act(() => {
      window.dispatchEvent(ev)
    })
    const btn = await screen.findByText("安装到桌面")
    await act(async () => {
      fireEvent.click(btn)
    })
    expect(ev.prompt).toHaveBeenCalledTimes(1)
  })

  it("shows the manual steps on iOS Safari", () => {
    setUA(IOS_UA)
    setStandalone(false)
    render(<InstallHint />)
    expect(screen.getByTestId("install-ios-hint").textContent).toContain("添加到主屏幕")
  })

  it("renders nothing when already installed", () => {
    setUA(IOS_UA)
    setStandalone(true)
    const { container } = render(<InstallHint />)
    expect(container.innerHTML).toBe("")
    act(() => {
      window.dispatchEvent(Object.assign(new Event("beforeinstallprompt"), { prompt: vi.fn() }))
    })
    expect(container.innerHTML).toBe("")
  })
})
