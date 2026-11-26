import { extractApiError, type ApiError } from './extract-api-error';

export interface WailsBridgeOptions {
  namespace?: string;
  timeoutMs?: number;
  mocks?: Record<string, (...args: unknown[]) => unknown>;
}

export interface WailsBridge {
  isAvailable(): boolean;
  invoke<T>(binding: string, method: string, ...args: unknown[]): Promise<T>;
  tryInvoke<T>(
    binding: string,
    method: string,
    ...args: unknown[]
  ): Promise<{ ok: true; data: T } | { ok: false; error: ApiError }>;
}

function readWindowGoRoot(): Record<string, unknown> | undefined {
  if (typeof globalThis === 'undefined') return undefined;
  const host = globalThis as unknown as { go?: Record<string, unknown> };
  return host.go && typeof host.go === 'object' ? host.go : undefined;
}

function withTimeout<T>(promise: Promise<T>, timeoutMs: number | undefined, label: string): Promise<T> {
  if (!timeoutMs || timeoutMs <= 0) return promise;
  return new Promise<T>((resolve, reject) => {
    const timer = setTimeout(() => {
      const err = new Error(`Wails RPC ${label} timed out after ${timeoutMs}ms`) as Error & {
        code?: string;
      };
      err.code = 'ETIMEDOUT';
      reject(err);
    }, timeoutMs);
    promise.then(
      (v) => {
        clearTimeout(timer);
        resolve(v);
      },
      (e) => {
        clearTimeout(timer);
        reject(e);
      },
    );
  });
}

/**
 * Typed Wails desktop IPC adapter (`window.go.<namespace>.<binding>.<method>`)
 * shared across Switch, Founder, Creator, Kova GUI, and HubDesk so React views
 * never reference `window.go.*` directly.
 */
export function createWailsBridge(options: WailsBridgeOptions = {}): WailsBridge {
  const namespace = options.namespace ?? 'main';

  function resolveMethod(
    binding: string,
    method: string,
  ): ((...args: unknown[]) => unknown) | undefined {
    const mockKey = `${binding}.${method}`;
    if (options.mocks && typeof options.mocks[mockKey] === 'function') {
      return options.mocks[mockKey];
    }
    const goRoot = readWindowGoRoot();
    const nsObj = goRoot?.[namespace];
    if (!nsObj || typeof nsObj !== 'object') return undefined;
    const bindObj = (nsObj as Record<string, unknown>)[binding];
    if (!bindObj || typeof bindObj !== 'object') return undefined;
    const fn = (bindObj as Record<string, unknown>)[method];
    return typeof fn === 'function' ? (fn as (...args: unknown[]) => unknown).bind(bindObj) : undefined;
  }

  function isAvailable(): boolean {
    const goRoot = readWindowGoRoot();
    return Boolean(goRoot && typeof goRoot[namespace] === 'object');
  }

  async function invoke<T>(binding: string, method: string, ...args: unknown[]): Promise<T> {
    const label = `${namespace}.${binding}.${method}`;
    const fn = resolveMethod(binding, method);
    if (!fn) {
      throw extractApiError(
        new Error(`Wails binding unavailable: ${label}`),
        'wails_unavailable',
      );
    }
    try {
      const result = Promise.resolve(fn(...args) as T);
      return await withTimeout(result, options.timeoutMs, label);
    } catch (err) {
      throw extractApiError(err, 'wails_rpc_failed');
    }
  }

  async function tryInvoke<T>(
    binding: string,
    method: string,
    ...args: unknown[]
  ): Promise<{ ok: true; data: T } | { ok: false; error: ApiError }> {
    try {
      return { ok: true, data: await invoke<T>(binding, method, ...args) };
    } catch (err) {
      if (err && typeof err === 'object' && 'retryable' in err && 'code' in err && 'message' in err) {
        return { ok: false, error: err as ApiError };
      }
      return { ok: false, error: extractApiError(err, 'wails_rpc_failed') };
    }
  }

  return { isAvailable, invoke, tryInvoke };
}
