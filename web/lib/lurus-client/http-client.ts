import { extractApiError } from './extract-api-error';

export interface HttpClientOptions {
  baseUrl?: string;
  getToken?: () => string | null | undefined | Promise<string | null | undefined>;
  defaultHeaders?: Record<string, string>;
  timeoutMs?: number;
  fetchImpl?: typeof fetch;
}

export interface HttpRequestInit {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';
  headers?: Record<string, string>;
  query?: Record<string, string | number | boolean | undefined | null>;
  body?: unknown;
  signal?: AbortSignal;
  timeoutMs?: number;
  fallbackCode?: string;
}

export interface HttpClient {
  request<T>(path: string, init?: HttpRequestInit): Promise<T>;
  requestRaw(path: string, init?: HttpRequestInit): Promise<Response>;
  get<T>(path: string, init?: Omit<HttpRequestInit, 'method' | 'body'>): Promise<T>;
  post<T>(path: string, body?: unknown, init?: Omit<HttpRequestInit, 'method' | 'body'>): Promise<T>;
  put<T>(path: string, body?: unknown, init?: Omit<HttpRequestInit, 'method' | 'body'>): Promise<T>;
  del<T>(path: string, init?: Omit<HttpRequestInit, 'method' | 'body'>): Promise<T>;
}

function joinUrl(
  baseUrl: string | undefined,
  path: string,
  query?: Record<string, string | number | boolean | undefined | null>,
): string {
  const lowerPath = path.toLowerCase();
  const isAbsolute = lowerPath.startsWith('http://') || lowerPath.startsWith('https://');
  const root = isAbsolute || !baseUrl
    ? path
    : `${baseUrl.replace(/\/+$/, '')}/${path.replace(/^\/+/, '')}`;
  if (!query) return root;
  const params = new URLSearchParams();
  for (const [k, v] of Object.entries(query)) {
    if (v !== undefined && v !== null) params.set(k, String(v));
  }
  const qs = params.toString();
  if (!qs) return root;
  return root.includes('?') ? `${root}&${qs}` : `${root}?${qs}`;
}

async function parseErrorBody(res: Response): Promise<unknown> {
  const ct = res.headers.get('content-type') ?? '';
  try {
    if (ct.includes('application/json')) return await res.json();
    const text = await res.text();
    try {
      return JSON.parse(text);
    } catch {
      return text;
    }
  } catch {
    return undefined;
  }
}

function createTimeoutController(
  timeoutMs: number | undefined,
  external?: AbortSignal,
): { signal: AbortSignal | undefined; clear: () => void } {
  if (!timeoutMs && !external) return { signal: undefined, clear: () => {} };
  const ctrl = new AbortController();
  const onExternalAbort = () => ctrl.abort(external?.reason);
  if (external) {
    if (external.aborted) ctrl.abort(external.reason);
    else external.addEventListener('abort', onExternalAbort, { once: true });
  }
  const timer = timeoutMs
    ? setTimeout(() => {
        const err = new Error(`Request timed out after ${timeoutMs}ms`) as Error & { code?: string };
        err.code = 'ETIMEDOUT';
        ctrl.abort(err);
      }, timeoutMs)
    : undefined;
  return {
    signal: ctrl.signal,
    clear: () => {
      if (timer !== undefined) clearTimeout(timer);
      if (external) external.removeEventListener('abort', onExternalAbort);
    },
  };
}

/**
 * Creates a typed HTTP transport that centralises `fetch`, bearer auth, timeout
 * cancellation, and `extractApiError` failure normalisation across Lurus frontends.
 */
export function createHttpClient(options: HttpClientOptions = {}): HttpClient {
  const transport = options.fetchImpl ?? globalThis.fetch;

  async function requestRaw(path: string, init: HttpRequestInit = {}): Promise<Response> {
    const url = joinUrl(options.baseUrl, path, init.query);
    const headers: Record<string, string> = {
      ...(options.defaultHeaders ?? {}),
      ...(init.headers ?? {}),
    };
    if (options.getToken && !headers.Authorization && !headers.authorization) {
      const token = await options.getToken();
      if (token) headers.Authorization = `Bearer ${token}`;
    }
    const isRawBody =
      init.body === undefined ||
      typeof init.body === 'string' ||
      init.body instanceof Uint8Array ||
      (typeof FormData !== 'undefined' && init.body instanceof FormData);
    if (init.body !== undefined && !isRawBody && !headers['Content-Type'] && !headers['content-type']) {
      headers['Content-Type'] = 'application/json';
    }
    const serialized =
      init.body === undefined
        ? undefined
        : isRawBody
          ? (init.body as BodyInit)
          : JSON.stringify(init.body);
    const timeout = createTimeoutController(init.timeoutMs ?? options.timeoutMs, init.signal);
    try {
      const res = await transport(url, {
        method: init.method ?? 'GET',
        headers,
        body: serialized,
        signal: timeout.signal,
      });
      if (!res.ok) {
        const data = await parseErrorBody(res);
        throw extractApiError(
          { status: res.status, statusText: res.statusText, data },
          init.fallbackCode ?? `http_${res.status}`,
        );
      }
      return res;
    } catch (err) {
      if (err && typeof err === 'object' && 'retryable' in err && 'code' in err && 'message' in err) {
        throw err;
      }
      if (timeout.signal?.aborted && timeout.signal.reason instanceof Error) {
        throw extractApiError(timeout.signal.reason, init.fallbackCode ?? 'ETIMEDOUT');
      }
      throw extractApiError(err, init.fallbackCode);
    } finally {
      timeout.clear();
    }
  }

  async function request<T>(path: string, init: HttpRequestInit = {}): Promise<T> {
    const res = await requestRaw(path, init);
    if (res.status === 204) return undefined as T;
    const ct = res.headers.get('content-type') ?? '';
    if (ct.includes('application/json') || ct === '') {
      const text = await res.text();
      return (text ? JSON.parse(text) : undefined) as T;
    }
    return (await res.text()) as unknown as T;
  }

  return {
    request,
    requestRaw,
    get: (path, init) => request(path, { ...init, method: 'GET' }),
    post: (path, body, init) => request(path, { ...init, method: 'POST', body }),
    put: (path, body, init) => request(path, { ...init, method: 'PUT', body }),
    del: (path, init) => request(path, { ...init, method: 'DELETE' }),
  };
}
