import { extractApiError } from "@/lib/api/errors"

/** Print pages show this on paper-sized screens, so it never carries the server's English text. */
export function printErrorMessage(e: unknown): string {
  const { status, code } = extractApiError(e)
  if (status === 404 || status === 400) return "找不到这张单据，请回到单据详情重新打开。"
  if (code === "offline" || code === "network") return "网络不通，请检查网络后重新打开。"
  return "单据暂时打不开，请回到单据详情重新打开。"
}
