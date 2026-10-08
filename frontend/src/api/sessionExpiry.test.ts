import { afterEach, describe, expect, it, vi } from "vitest";

import { sessionExpiredEvent, createSessionAwareFetch } from "./sessionExpiry";

afterEach(() => {
  vi.restoreAllMocks();
});

describe("session-aware fetch", () => {
  it("announces a protected API 401 after returning the response", async () => {
    const response = new Response(null, { status: 401 });
    const original = vi.fn().mockResolvedValue(response);
    const expired = vi.fn();
    window.addEventListener(sessionExpiredEvent, expired);

    const result = await createSessionAwareFetch(original)(
      "/api/v1/work-records",
    );

    expect(result).toBe(response);
    expect(expired).toHaveBeenCalledOnce();
    window.removeEventListener(sessionExpiredEvent, expired);
  });

  it.each([403, 500])(
    "does not announce protected API status %s",
    async (status) => {
      const original = vi
        .fn()
        .mockResolvedValue(new Response(null, { status }));
      const expired = vi.fn();
      window.addEventListener(sessionExpiredEvent, expired);

      await createSessionAwareFetch(original)("/api/v1/work-records");

      expect(expired).not.toHaveBeenCalled();
      window.removeEventListener(sessionExpiredEvent, expired);
    },
  );

  it.each(["/api/v1/me", "/api/v1/setup/status"])(
    "does not announce bootstrap request %s",
    async (url) => {
      const original = vi
        .fn()
        .mockResolvedValue(new Response(null, { status: 401 }));
      const expired = vi.fn();
      window.addEventListener(sessionExpiredEvent, expired);

      await createSessionAwareFetch(original)(url);

      expect(expired).not.toHaveBeenCalled();
      window.removeEventListener(sessionExpiredEvent, expired);
    },
  );

  it("recognizes a protected API Request object", async () => {
    const original = vi
      .fn()
      .mockResolvedValue(new Response(null, { status: 401 }));
    const expired = vi.fn();
    window.addEventListener(sessionExpiredEvent, expired);

    await createSessionAwareFetch(original)(
      new Request("https://rarity.example/api/v1/sessions"),
    );

    expect(expired).toHaveBeenCalledOnce();
    window.removeEventListener(sessionExpiredEvent, expired);
  });
});
