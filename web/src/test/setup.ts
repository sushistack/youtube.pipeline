// External fetch blocking — rejects any non-localhost fetch in Vitest tests.
const originalFetch = globalThis.fetch;

// xyflow needs ResizeObserver + DOMRect; jsdom provides neither.
if (!("ResizeObserver" in globalThis)) {
  class ResizeObserverShim {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  Object.defineProperty(globalThis, "ResizeObserver", {
    configurable: true,
    value: ResizeObserverShim,
    writable: true,
  });
}

function createStorageShim(): Storage {
  const store = new Map<string, string>();

  return {
    get length() {
      return store.size;
    },
    clear() {
      store.clear();
    },
    getItem(key: string) {
      return store.has(key) ? store.get(key)! : null;
    },
    key(index: number) {
      return Array.from(store.keys())[index] ?? null;
    },
    removeItem(key: string) {
      store.delete(key);
    },
    setItem(key: string, value: string) {
      store.set(key, value);
    },
  };
}

if (
  !("localStorage" in globalThis) ||
  typeof globalThis.localStorage?.clear !== "function"
) {
  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: createStorageShim(),
    writable: true,
  });
}

globalThis.fetch = async (
  input: RequestInfo | URL,
  init?: RequestInit,
): Promise<Response> => {
  const url =
    typeof input === "string"
      ? input
      : input instanceof URL
        ? input.href
        : input.url;
  const parsed = new URL(url, "http://localhost");
  const host = parsed.hostname;

  if (host === "localhost" || host === "127.0.0.1" || host === "::1") {
    return originalFetch(input, init);
  }

  throw new Error(`external fetch blocked in test: ${url}`);
};
