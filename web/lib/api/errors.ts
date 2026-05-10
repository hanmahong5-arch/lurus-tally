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
