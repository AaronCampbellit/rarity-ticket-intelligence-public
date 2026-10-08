import { describe, expect, it, vi } from "vitest";

import { createTeamsSettingsAPI } from "./api";

const wire = {
  id: "connection-1",
  client_id: "client-1",
  name: "Service Desk",
  credential_configured: true,
  enabled: false,
  health: "pending",
  version: 2,
};

describe("Teams settings API", () => {
  it("maps safe fields and uses strict lifecycle routes", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(JSON.stringify([wire]), { status: 200 }),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify(wire), { status: 200 }),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify(wire), { status: 200 }),
      );
    const api = createTeamsSettingsAPI(fetcher, "client-1");

    expect((await api.list())[0]).toMatchObject({
      credentialConfigured: true,
      clientID: "client-1",
    });
    await api.replaceCredential("connection 1", {
      expectedVersion: 2,
      webhookURL: "https://teams.example.test/hook/secret",
      reason: "rotate",
    });
    await api.test("connection 1", { reason: "validate" });

    expect(fetcher.mock.calls.map((call) => call[0])).toEqual([
      "/api/v1/integrations/teams/connections",
      "/api/v1/integrations/teams/connections/connection%201/credential",
      "/api/v1/integrations/teams/connections/connection%201/test",
    ]);
    for (const [, init] of fetcher.mock.calls) {
      expect(new Headers(init?.headers).get("X-Rarity-Client-ID")).toBe(
        "client-1",
      );
    }
    const credentialBody = JSON.parse(String(fetcher.mock.calls[1][1]?.body));
    expect(credentialBody).toEqual({
      expected_version: 2,
      webhook_url: "https://teams.example.test/hook/secret",
      reason: "rotate",
    });
    expect(JSON.stringify(wire)).not.toContain("hook/secret");
  });
});
