export interface KeyValueStorageLike {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

export interface BrowserStorageOptions {
  prefix?: string;
  storage?: KeyValueStorageLike | null;
}

export interface BrowserStorage {
  get<T>(key: string, fallback: T): T;
  set<T>(key: string, value: T): boolean;
  getRaw(key: string): string | null;
  setRaw(key: string, value: string): boolean;
  remove(key: string): void;
}

function resolveDefaultStorage(): KeyValueStorageLike | null {
  if (typeof globalThis === 'undefined' || !('localStorage' in globalThis)) {
    return null;
  }
  try {
    return globalThis.localStorage;
  } catch {
    return null;
  }
}

/**
 * SSR-safe, quota-resilient browser storage adapter so feature slices across
 * Lurus web and desktop apps never touch `window.localStorage` directly.
 */
export function createBrowserStorage(options: BrowserStorageOptions = {}): BrowserStorage {
  const prefix = options.prefix ? `${options.prefix}:` : '';
  const qualify = (key: string): string => `${prefix}${key}`;
  const getStore = (): KeyValueStorageLike | null =>
    options.storage !== undefined ? options.storage : resolveDefaultStorage();

  function getRaw(key: string): string | null {
    const store = getStore();
    if (!store) return null;
    try {
      return store.getItem(qualify(key));
    } catch {
      return null;
    }
  }

  function setRaw(key: string, value: string): boolean {
    const store = getStore();
    if (!store) return false;
    try {
      store.setItem(qualify(key), value);
      return true;
    } catch {
      return false;
    }
  }

  function remove(key: string): void {
    const store = getStore();
    if (!store) return;
    try {
      store.removeItem(qualify(key));
    } catch {
      return;
    }
  }

  function get<T>(key: string, fallback: T): T {
    const raw = getRaw(key);
    if (raw === null) return fallback;
    try {
      return JSON.parse(raw) as T;
    } catch {
      return fallback;
    }
  }

  function set<T>(key: string, value: T): boolean {
    try {
      return setRaw(key, JSON.stringify(value));
    } catch {
      return false;
    }
  }

  return { get, set, getRaw, setRaw, remove };
}
