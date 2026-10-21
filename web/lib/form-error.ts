import { ApiError, extractApiError } from "@/lib/api/errors"

const REJECTED_FALLBACK = "保存没成功，请检查填写的内容"

/**
 * Shop-owner copy for a failed save. The backend's 4xx bodies for these forms may carry an
 * English sentence or a database message; those never reach the page. 409 shows `conflict`.
 */
export function formSaveError(e: unknown, conflict: string): string {
  if (e instanceof ApiError && e.status === 409) return conflict
  const d = extractApiError(e)
  if (d.status !== undefined && d.status >= 400 && d.status < 500 && /^[A-Za-z]/.test(d.message)) {
    return REJECTED_FALLBACK
  }
  return d.message
}
