export const sessionExpiredEvent = "rarity:session-expired";

export function createSessionAwareFetch(
  originalFetch: typeof fetch,
): typeof fetch {
  return async (input: RequestInfo | URL, init?: RequestInit) => {
    const response = await originalFetch(input, init);
    if (
      response.status === 401 &&
      isProtectedAPIRequest(input) &&
      !isAccessResolutionRequest(input)
    ) {
      window.dispatchEvent(new CustomEvent(sessionExpiredEvent));
    }
    return response;
  };
}

function requestPath(input: RequestInfo | URL): string {
  const value =
    input instanceof Request
      ? input.url
      : input instanceof URL
        ? input.href
        : input;
  return new URL(value, window.location.origin).pathname;
}

function isProtectedAPIRequest(input: RequestInfo | URL): boolean {
  return requestPath(input).startsWith("/api/v1/");
}

function isAccessResolutionRequest(input: RequestInfo | URL): boolean {
  const path = requestPath(input);
  return path === "/api/v1/me" || path === "/api/v1/setup/status";
}
