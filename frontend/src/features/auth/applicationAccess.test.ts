import { afterEach, describe, expect, it, vi } from "vitest";

import {
  loadApplicationAccess,
  rememberReturnHash,
  safeReturnHash,
  takeReturnHash,
} from "./applicationAccess";

afterEach(() => {
  vi.unstubAllGlobals();
  window.sessionStorage.clear();
});

describe("application access", () => {
  it("stops at public setup before requesting a principal", async () => {
    const fetcher = vi.fn().mockResolvedValue(
      Response.json({
        completed: false,
        bootstrap_available: true,
        entra_available: false,
      }),
    );
    vi.stubGlobal("fetch", fetcher);

    await expect(loadApplicationAccess()).resolves.toEqual({
      kind: "setup",
      entraAvailable: false,
    });
    expect(fetcher).toHaveBeenCalledTimes(1);
  });

  it("treats only a principal 401 as unauthenticated", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValueOnce(
          Response.json({
            completed: true,
            bootstrap_available: false,
            entra_available: true,
          }),
        )
        .mockResolvedValueOnce(new Response(null, { status: 401 })),
    );

    await expect(loadApplicationAccess()).resolves.toEqual({
      kind: "unauthenticated",
      entraAvailable: true,
    });
  });

  it("returns the authenticated principal with the active auth mode", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValueOnce(
          Response.json({
            completed: true,
            bootstrap_available: false,
            entra_available: false,
          }),
        )
        .mockResolvedValueOnce(
          Response.json({
            id: "technician-id",
            navigation: ["work"],
            capabilities: ["work_record.read"],
          }),
        ),
    );

    await expect(loadApplicationAccess()).resolves.toEqual({
      kind: "authenticated",
      entraAvailable: false,
      principal: {
        id: "technician-id",
        navigation: ["work"],
        capabilities: ["work_record.read"],
      },
    });
  });

  it("fails closed when principal resolution has a server error", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValueOnce(
          Response.json({
            completed: true,
            bootstrap_available: false,
            entra_available: true,
          }),
        )
        .mockResolvedValueOnce(new Response(null, { status: 500 })),
    );

    await expect(loadApplicationAccess()).rejects.toMatchObject({
      status: 500,
    });
  });
});

describe("post-authentication return hashes", () => {
  it.each([
    ["#/sales?workRecordID=abc", "#/sales?workRecordID=abc"],
    ["#/login", undefined],
    ["#/break-glass", undefined],
    ["#/setup", undefined],
    ["#/unknown", undefined],
    ["https://evil.example", undefined],
  ] as const)("normalizes %s to %s", (hash, expected) => {
    expect(safeReturnHash(hash, false)).toBe(expected);
  });

  it("consumes a remembered safe hash exactly once", () => {
    rememberReturnHash("#/sales", false);

    expect(takeReturnHash(false)).toBe("#/sales");
    expect(takeReturnHash(false)).toBeUndefined();
  });

  it("does not replace an existing return destination", () => {
    rememberReturnHash("#/sales", false);
    rememberReturnHash("#/work", false);

    expect(takeReturnHash(false)).toBe("#/sales");
  });
});
