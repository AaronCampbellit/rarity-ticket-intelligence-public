import { afterEach, describe, expect, it, vi } from "vitest";

import {
  APIError,
  createOpportunityTask,
  listOpportunities,
  listProposals,
} from "./api";

afterEach(() => vi.unstubAllGlobals());

describe("sales read API", () => {
  it("loads scoped Opportunity and Proposal worklists", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(new Response("[]", { status: 200 }))
      .mockResolvedValueOnce(new Response("[]", { status: 200 }));
    vi.stubGlobal("fetch", fetcher);
    await listOpportunities("client-id");
    await listProposals("client-id");
    expect(fetcher.mock.calls.map((call) => call[0])).toEqual([
      "/api/v1/opportunities?limit=100",
      "/api/v1/proposals?limit=100",
    ]);
    for (const [, init] of fetcher.mock.calls) {
      expect(init).toMatchObject({
        credentials: "same-origin",
        headers: { "X-Rarity-Client-ID": "client-id" },
      });
    }
  });
});

describe("sales task API", () => {
  it("sends canonical tags and preserves a backend stale-tag rejection", async () => {
    const fetcher = vi.fn().mockResolvedValueOnce(
      Response.json(
        {
          error: {
            code: "tag_archived",
            message: "Selected tag was archived",
          },
        },
        { status: 422 },
      ),
    );
    vi.stubGlobal("fetch", fetcher);

    await expect(
      createOpportunityTask("client-id", "opportunity-id", {
        title: "Confirm scope",
        estimateMinutes: 30,
        tagIDs: ["tag-vpn"],
      }),
    ).rejects.toEqual(
      expect.objectContaining<Partial<APIError>>({
        code: "tag_archived",
        status: 422,
      }),
    );
    const [, init] = fetcher.mock.calls[0];
    expect(JSON.parse(String(init.body))).toEqual({
      title: "Confirm scope",
      estimate_minutes: 30,
      tag_ids: ["tag-vpn"],
    });
    expect(init.headers).toMatchObject({ "X-Rarity-Client-ID": "client-id" });
  });
});
