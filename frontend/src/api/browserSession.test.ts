import { afterEach, describe, expect, it, vi } from "vitest";

import {
  configureEntraSettings,
  csrfHeaders,
  disableEntraSettings,
  loadPrincipalNavigation,
  loadSetupStatus,
  loadEntraSettings,
  loadDirectory,
  listSessions,
  loginLocalAdministrator,
  logout,
  resetRecoveryAccountPassword,
  revokeSession,
  updateRecoveryAccountNetworks,
  verifyEntraSettings,
} from "./browserSession";

afterEach(() => {
  vi.unstubAllGlobals();
  document.cookie = "rarity_session=; Max-Age=0; path=/";
  document.cookie = "rarity_csrf=; Max-Age=0; path=/";
});

describe("browser session API", () => {
  it("loads the complete public setup capability response", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        Response.json({
          completed: true,
          bootstrap_available: false,
          entra_available: true,
        }),
      ),
    );

    await expect(loadSetupStatus()).resolves.toEqual({
      completed: true,
      bootstrap_available: false,
      entra_available: true,
    });
  });

  it.each([401, 500])(
    "preserves principal response status %s for access resolution",
    async (status) => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue(new Response(null, { status })),
      );

      await expect(loadPrincipalNavigation()).rejects.toMatchObject({ status });
    },
  );

  it("copies the non-HttpOnly CSRF cookie into mutation headers", () => {
    document.cookie = "rarity_csrf=csrf-token; path=/";
    expect(csrfHeaders()).toEqual({ "X-Rarity-CSRF": "csrf-token" });
  });

  it("loads directory context and logs out with same-origin credentials", async () => {
    document.cookie = "rarity_csrf=csrf-token; path=/";
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ clients: [] }), { status: 200 }),
      )
      .mockResolvedValueOnce(new Response(null, { status: 204 }));
    vi.stubGlobal("fetch", fetcher);
    await loadDirectory();
    await logout();
    expect(fetcher.mock.calls[0][1]).toMatchObject({
      credentials: "same-origin",
    });
    expect(fetcher.mock.calls[1][1]).toMatchObject({
      method: "POST",
      headers: { "X-Rarity-CSRF": "csrf-token" },
    });
  });

  it("normalizes deployed directory client envelopes for the active client selector", async () => {
    const fetcher = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          clients: [
            {
              ID: "client-alpha",
              DisplayID: "CLIENT-001",
              Name: "Northwind Legal",
            },
          ],
          departments: [],
          teams: [],
          queues: [],
        }),
        { status: 200 },
      ),
    );
    vi.stubGlobal("fetch", fetcher);

    await expect(loadDirectory()).resolves.toMatchObject({
      clients: [
        {
          id: "client-alpha",
          display_id: "CLIENT-001",
          name: "Northwind Legal",
        },
      ],
    });
  });

  it("normalizes deployed directory relationship envelopes for stable rendering", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        Response.json({
          clients: [],
          departments: [
            { ID: "department-1", Key: "service-desk", Name: "Service Desk" },
          ],
          teams: [
            {
              ID: "team-1",
              DepartmentID: "department-1",
              Key: "support",
              Name: "Support",
            },
          ],
          queues: [
            {
              ID: "queue-1",
              ClientID: "client-1",
              DepartmentID: "department-1",
              TeamID: "team-1",
              Key: "global-triage",
              Name: "Global Triage",
            },
          ],
        }),
      ),
    );

    await expect(loadDirectory()).resolves.toMatchObject({
      departments: [
        { id: "department-1", key: "service-desk", name: "Service Desk" },
      ],
      teams: [
        {
          id: "team-1",
          department_id: "department-1",
          key: "support",
          name: "Support",
        },
      ],
      queues: [
        {
          id: "queue-1",
          client_id: "client-1",
          department_id: "department-1",
          team_id: "team-1",
          key: "global-triage",
          name: "Global Triage",
        },
      ],
    });
  });

  it("submits local administrator credentials without placing them in the URL", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(new Response(null, { status: 204 }));
    vi.stubGlobal("fetch", fetcher);

    await loginLocalAdministrator(
      " recovery-admin ",
      "correct horse battery staple",
    );

    expect(fetcher).toHaveBeenCalledWith(
      "/auth/local/login",
      expect.objectContaining({
        method: "POST",
        credentials: "same-origin",
        body: "username=recovery-admin&password=correct+horse+battery+staple",
      }),
    );
  });

  it("includes the CSRF token when a revoked session cookie remains during local sign-in", async () => {
    document.cookie = "rarity_session=revoked-session; path=/";
    document.cookie = "rarity_csrf=csrf-token; path=/";
    const fetcher = vi
      .fn()
      .mockResolvedValue(new Response(null, { status: 204 }));
    vi.stubGlobal("fetch", fetcher);

    await loginLocalAdministrator(
      "recovery-admin",
      "correct horse battery staple",
    );

    expect(fetcher).toHaveBeenCalledWith(
      "/auth/local/login",
      expect.objectContaining({
        method: "POST",
        headers: {
          "Content-Type": "application/x-www-form-urlencoded",
          "X-Rarity-CSRF": "csrf-token",
        },
      }),
    );
  });

  it("lists and revokes visible browser sessions with CSRF protection", async () => {
    document.cookie = "rarity_csrf=csrf-token; path=/";
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(new Response("[]", { status: 200 }))
      .mockResolvedValueOnce(new Response(null, { status: 204 }));
    vi.stubGlobal("fetch", fetcher);

    await listSessions();
    await revokeSession("session-id");

    expect(fetcher.mock.calls[0][0]).toBe("/api/v1/sessions");
    expect(fetcher.mock.calls[1]).toEqual([
      "/api/v1/sessions/session-id",
      expect.objectContaining({
        method: "DELETE",
        headers: { "X-Rarity-CSRF": "csrf-token" },
      }),
    ]);
  });

  it("resets local administrator passwords and networks with optimistic versions", async () => {
    document.cookie = "rarity_csrf=csrf-token; path=/";
    const account = {
      id: "admin-id",
      technician_id: "technician-id",
      username: "local-admin",
      allowed_cidrs: ["10.0.0.0/8"],
      enabled: true,
      created_at: "2026-08-02T00:00:00Z",
      version: 3,
    };
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ ...account, version: 4 }), {
          status: 200,
        }),
      )
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            ...account,
            allowed_cidrs: ["192.168.86.0/24"],
            version: 5,
          }),
          { status: 200 },
        ),
      );
    vi.stubGlobal("fetch", fetcher);

    await resetRecoveryAccountPassword(
      account,
      "a new sufficiently long password",
      "scheduled rotation",
    );
    await updateRecoveryAccountNetworks(
      { ...account, version: 4 },
      ["192.168.86.0/24"],
      "network change",
    );

    expect(fetcher.mock.calls[0]).toEqual([
      "/api/v1/admin/local-administrators/admin-id/password",
      expect.objectContaining({
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "X-Rarity-CSRF": "csrf-token",
        },
        body: JSON.stringify({
          expected_version: 3,
          password: "a new sufficiently long password",
          reason: "scheduled rotation",
        }),
      }),
    ]);
    expect(fetcher.mock.calls[1]).toEqual([
      "/api/v1/admin/local-administrators/admin-id/networks",
      expect.objectContaining({
        method: "PATCH",
        body: JSON.stringify({
          expected_version: 4,
          allowed_cidrs: ["192.168.86.0/24"],
          reason: "network change",
        }),
      }),
    ]);
  });

  it("manages write-only Entra settings through versioned operations", async () => {
    document.cookie = "rarity_csrf=csrf-token; path=/";
    const fetcher = vi.fn().mockImplementation(
      async () =>
        new Response(
          JSON.stringify({
            state: "verification_required",
            version: 2,
            tenant_id: "tenant",
            client_id: "client",
            redirect_url: "https://rarity.example/auth/callback",
            credential_configured: true,
          }),
          { status: 200 },
        ),
    );
    vi.stubGlobal("fetch", fetcher);

    await loadEntraSettings();
    await configureEntraSettings({
      expected_version: 1,
      tenant_id: "tenant",
      client_id: "client",
      client_secret: "write-only-secret",
      redirect_url: "https://rarity.example/auth/callback",
      reason: "connect workforce SSO",
    });
    await verifyEntraSettings(2, "verify discovery");
    await disableEntraSettings(3, "return to local only");

    expect(fetcher.mock.calls[0][0]).toBe("/api/v1/admin/identity/entra");
    expect(fetcher.mock.calls[1][1]).toMatchObject({
      method: "PUT",
      body: expect.stringContaining("write-only-secret"),
    });
    expect(fetcher.mock.calls[2][0]).toBe(
      "/api/v1/admin/identity/entra/verify",
    );
    expect(fetcher.mock.calls[3][0]).toBe(
      "/api/v1/admin/identity/entra/disable",
    );
  });
});
