/**
 * callApi — thin wrapper that runs an async request and, on failure, normalises
 * the error through `extractApiError`, hands it to an optional `onError` sink
 * (toast / structured log / store), then re-throws the NORMALISED `ApiError` so
 * the caller's control flow still observes a failure.
 *
 * This is the single seam every frontend should funnel requests through, so that
 * "what does a failure look like" is answered once, not per call site.
 */

import { extractApiError, type ApiError } from './extract-api-error';

export type ApiErrorHandler = (e: ApiError) => void;

/**
 * @param req          The request thunk (e.g. `() => axios.get(...)` or `() => fetch(...)`).
 * @param onError      Optional side-effect run with the normalised error before re-throw.
 * @param fallbackCode Code applied when the error carries none.
 * @throws ApiError    The normalised error (NOT the raw transport error).
 */
export async function callApi<T>(
  req: () => Promise<T>,
  onError?: ApiErrorHandler,
  fallbackCode?: string,
): Promise<T> {
  try {
    return await req();
  } catch (err) {
    const apiError = extractApiError(err, fallbackCode);
    onError?.(apiError);
    throw apiError;
  }
}

/**
 * Non-throwing variant: returns a discriminated result instead of throwing, for
 * call sites that prefer branching over try/catch.
 */
export async function tryCallApi<T>(
  req: () => Promise<T>,
  fallbackCode?: string,
): Promise<{ ok: true; data: T } | { ok: false; error: ApiError }> {
  try {
    return { ok: true, data: await req() };
  } catch (err) {
    return { ok: false, error: extractApiError(err, fallbackCode) };
  }
}
