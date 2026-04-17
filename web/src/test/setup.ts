// External fetch blocking — rejects any non-localhost fetch in Vitest tests.
const originalFetch = globalThis.fetch;

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
