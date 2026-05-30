/**
 * Typed errors thrown by the unified apiFetch client.
 *
 * Callers can branch on the class (instanceof) or on an HTTP status code.
 * The `body` field exposes the parsed JSON payload (if any) so domain wrappers
 * (e.g. billing.ts) can recover a service-specific code field.
 */

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
    message: string,
    public readonly body?: unknown,
  ) {
    super(message)
    this.name = "ApiError"
  }
}

/** Thrown when the request never reached the server (DNS / offline / abort). */
export class NetworkError extends Error {
  constructor(public readonly kind: "offline" | "network", message: string) {
    super(message)
    this.name = "NetworkError"
  }
}

/** Thrown after the client triggers a re-authentication redirect. */
export class UnauthorizedError extends ApiError {
  constructor(message: string, body?: unknown) {
    super(401, "unauthorized", message, body)
    this.name = "UnauthorizedError"
  }
}

/**
 * A user-presentable view of any thrown error.
 *
 * `message` is always safe to render to a paying user: it never contains the
 * `ApiError:`/`Error:` class-name prefix that `String(err)` leaks, and falls
 * back to a generic Chinese message for unrecognized values. `code`/`status`
 * are optional machine hints for callers that want to branch (they are NOT
 * meant to be rendered raw).
 */
export interface DisplayError {
  message: string
  code?: string
  status?: number
}

const GENERIC_MESSAGE = "操作失败，请稍后重试。"

/**
 * extractApiError normalizes any caught value into a {@link DisplayError} whose
 * `message` is fit for direct display. This is the single, canonical path for
 * turning a thrown error into user-facing copy — callers must use it instead of
 * `String(err)` (which leaks the class-name prefix) or reading `err.message`
 * ad hoc (which may surface a raw transport/Zitadel code).
 */
export function extractApiError(err: unknown): DisplayError {
  if (err instanceof ApiError) {
    // Prefer a server-supplied human message from the JSON body, then the
    // ApiError's own message, then a generic fallback. Never the class name.
    const body = (err.body ?? {}) as { detail?: string; message?: string; error?: string }
    const message = body.detail ?? body.message ?? (err.message || GENERIC_MESSAGE)
    return { message, code: err.code, status: err.status }
  }
  if (err instanceof NetworkError) {
    const message =
      err.kind === "offline"
        ? "网络似乎已断开，请检查连接后重试。"
        : "无法连接到服务器，请稍后重试。"
    return { message, code: err.kind }
  }
  if (err instanceof Error) {
    return { message: err.message || GENERIC_MESSAGE }
  }
  return { message: GENERIC_MESSAGE }
}
