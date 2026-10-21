import { describe, it, expect } from "vitest"
import { isWeChatBrowser, isWeChatMiniProgram } from "./wechat-env"

const WECHAT =
  "Mozilla/5.0 (iPhone; CPU iPhone OS 16_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 MicroMessenger/8.0.40(0x18002830) NetType/WIFI Language/zh_CN"
const MINI = WECHAT + " miniProgram/wx123"
const CHROME = "Mozilla/5.0 (Linux; Android 13) AppleWebKit/537.36 Chrome/120 Mobile Safari/537.36"

describe("wechat-env", () => {
  it("识别微信内置浏览器", () => {
    expect(isWeChatBrowser(WECHAT)).toBe(true)
    expect(isWeChatBrowser(MINI)).toBe(true)
  })
  it("普通浏览器不是微信", () => {
    expect(isWeChatBrowser(CHROME)).toBe(false)
    expect(isWeChatBrowser("")).toBe(false)
  })
  it("识别小程序 web-view", () => {
    expect(isWeChatMiniProgram(MINI)).toBe(true)
    expect(isWeChatMiniProgram(WECHAT)).toBe(false)
    expect(isWeChatMiniProgram(CHROME)).toBe(false)
  })
  it("不传 ua 时读 navigator.userAgent", () => {
    expect(isWeChatBrowser()).toBe(false)
  })
})
