/**
 * Typed errors thrown by the unified apiFetch client, delegating error payload
 * normalisation to the shared `@lurus/frontend-client` (`@/lib/lurus-client`) primitives.
 */

import {
  extractApiError as extractSharedApiError,
  type ApiError as SharedApiError,
} from "../lurus-client"

export type { SharedApiError }

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

/**
 * Thrown when the request never reached the server (DNS / offline / abort).
 * `sent` is false only when the request provably never left the device; true
 * when it may have been delivered and only the reply was lost.
 */
export class NetworkError extends Error {
  constructor(
    public readonly kind: "offline" | "network",
    message: string,
    public readonly sent: boolean = true,
  ) {
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
 */
export interface DisplayError {
  message: string
  /** Server-supplied "what to do next" sentence, when the body carries one. */
  action?: string
  code?: string
  status?: number
}

const GENERIC_MESSAGE = "操作失败，请稍后重试。"
const SERVER_FAULT_MESSAGE = "系统出了点问题，请稍后再试；一直不行请联系客服"

/**
 * extractApiError normalizes any caught value into a {@link DisplayError} whose
 * `message` is fit for direct display, delegating transport/envelope parsing to
 * the shared `lurus-client` normalizer while preserving Tally's shop-owner copy rules.
 */
export function extractApiError(err: unknown): DisplayError {
  if (err instanceof ApiError) {
    const body = (err.body ?? {}) as { detail?: string; message?: string; action?: string; error?: unknown }
    const normalized = extractSharedApiError(
      { status: err.status, statusText: err.message, data: err.body },
      err.code,
    )
    const hasCustomText =
      Boolean(body.detail) ||
      Boolean(body.message) ||
      Boolean(err.message) ||
      Boolean(body.error)
    const message = body.detail ?? (hasCustomText ? normalized.message : GENERIC_MESSAGE)
    const action = typeof body.action === "string" && body.action ? body.action : undefined
    if (err.status >= 500 && /^[A-Za-z]/.test(message)) {
      return { message: SERVER_FAULT_MESSAGE, code: normalized.code, status: err.status }
    }
    return { message, action, code: normalized.code, status: err.status }
  }
  if (err instanceof NetworkError) {
    const message =
      err.kind === "offline"
        ? "网络似乎已断开，请检查连接后重试。"
        : "无法连接到服务器，请稍后重试。"
    return { message, code: err.kind }
  }
  if (err instanceof Error) {
    if (!err.message.trim()) {
      return { message: GENERIC_MESSAGE }
    }
    const normalized = extractSharedApiError(err)
    return { message: normalized.message }
  }
  if (typeof err === "object" && err !== null) {
    const normalized = extractSharedApiError(err)
    return {
      message: normalized.message === "请求未能完成，请稍后重试" ? GENERIC_MESSAGE : normalized.message,
      code: normalized.code !== "unknown" ? normalized.code : undefined,
      status: normalized.status,
    }
  }
  return { message: GENERIC_MESSAGE }
}
