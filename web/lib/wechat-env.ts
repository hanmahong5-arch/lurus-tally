/** 微信环境判定：纯函数，ua 缺省时在浏览器里取 navigator.userAgent，服务端渲染返回 false。 */

function resolveUa(ua?: string): string {
  if (typeof ua === "string") return ua
  if (typeof navigator !== "undefined") return navigator.userAgent || ""
  return ""
}

/** 是否微信内置浏览器（含小程序 web-view）。 */
export function isWeChatBrowser(ua?: string): boolean {
  return /MicroMessenger/i.test(resolveUa(ua))
}

/** 是否微信小程序 web-view（UA 含 miniProgram）。 */
export function isWeChatMiniProgram(ua?: string): boolean {
  return /miniProgram/i.test(resolveUa(ua))
}
