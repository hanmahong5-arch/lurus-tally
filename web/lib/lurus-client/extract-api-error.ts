/**
 * extractApiError normalises caught request failures (axios errors, fetch Response
 * envelopes, native Errors, upstream JSON payloads, or plain strings) into a flat,
 * actionable `ApiError` contract shared across Platform, Newhub, and Tally Web.
 *
 * `message` is guaranteed to be a non-empty, user-safe string; `details` preserves
 * the raw diagnostic body strictly for logging and must not be rendered directly.
 */

export interface ApiError {
  /** Machine-readable error code (upstream code, axios code, or fallback). */
  code: string;
  /** Human-readable message — always non-empty. */
  message: string;
  /** True when retrying the same request could plausibly succeed (network / timeout / 5xx / 429). */
  retryable: boolean;
  /** HTTP status when known. */
  status?: number;
  /** Raw parsed body / cause, for logging only — do not render to users. */
  details?: unknown;
}

const DEFAULT_MESSAGE = '请求未能完成，请稍后重试';
const DEFAULT_CODE = 'unknown';

const NETWORK_CODES = new Set([
  'ECONNABORTED',
  'ETIMEDOUT',
  'ENETUNREACH',
  'ECONNREFUSED',
  'ERR_NETWORK',
  'ERR_CANCELED',
]);

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null;
}

function nonEmptyString(v: unknown): string | undefined {
  return typeof v === 'string' && v.trim() ? v : undefined;
}

function statusRetryable(status: number | undefined): boolean {
  if (status === undefined) return false;
  if (status === 429) return true;
  return status >= 500 && status <= 599;
}

function fromBody(body: unknown): { code?: string; message?: string } {
  if (typeof body === 'string') return { message: nonEmptyString(body) };
  if (!isRecord(body)) return {};

  const err = body.error;
  if (isRecord(err)) {
    return {
      code: nonEmptyString(err.code),
      message:
        nonEmptyString(err.message) ??
        nonEmptyString(err.description) ??
        nonEmptyString(err.title),
    };
  }
  return {
    code: nonEmptyString(err) ?? nonEmptyString(body.code),
    message: nonEmptyString(body.message) ?? nonEmptyString(err),
  };
}

function isAxiosLike(
  err: unknown,
): err is {
  isAxiosError?: boolean;
  code?: string;
  message?: string;
  response?: { status?: number; data?: unknown };
  request?: unknown;
} {
  return (
    isRecord(err) &&
    (err.isAxiosError === true || 'response' in err || 'request' in err) &&
    ('message' in err || 'code' in err || 'response' in err)
  );
}

function isResponseLike(err: unknown): err is { status: number; data?: unknown; statusText?: string } {
  return isRecord(err) && typeof err.status === 'number' && !('response' in err) && !('isAxiosError' in err);
}

/**
 * Normalises `err` into a structured `ApiError`, falling back to `fallbackCode` when unclassified.
 */
export function extractApiError(err: unknown, fallbackCode: string = DEFAULT_CODE): ApiError {
  if (isAxiosLike(err)) {
    const status = err.response?.status;
    const noResponse = err.response === undefined;
    const body = fromBody(err.response?.data);
    const code = body.code ?? nonEmptyString(err.code) ?? fallbackCode;
    const message = body.message ?? nonEmptyString(err.message) ?? DEFAULT_MESSAGE;
    const retryable =
      noResponse || statusRetryable(status) || (err.code ? NETWORK_CODES.has(err.code) : false);
    return { code, message, retryable, status, details: err.response?.data ?? err.message };
  }

  if (isResponseLike(err)) {
    const body = fromBody(err.data);
    return {
      code: body.code ?? fallbackCode,
      message: body.message ?? nonEmptyString(err.statusText) ?? DEFAULT_MESSAGE,
      retryable: statusRetryable(err.status),
      status: err.status,
      details: err.data,
    };
  }

  if (isRecord(err) && ('error' in err || 'message' in err) && !(err instanceof Error)) {
    const body = fromBody(err);
    if (body.code || body.message) {
      return {
        code: body.code ?? fallbackCode,
        message: body.message ?? DEFAULT_MESSAGE,
        retryable: false,
        details: err,
      };
    }
  }

  if (err instanceof Error) {
    const code = nonEmptyString((err as { code?: unknown }).code as string) ?? fallbackCode;
    return {
      code,
      message: nonEmptyString(err.message) ?? DEFAULT_MESSAGE,
      retryable: typeof code === 'string' && NETWORK_CODES.has(code),
      details: err.stack ?? err.message,
    };
  }

  return {
    code: fallbackCode,
    message: nonEmptyString(err) ?? DEFAULT_MESSAGE,
    retryable: false,
    details: err,
  };
}
