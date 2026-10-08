import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { App } from "./App";
import { sessionExpiredEvent } from "./api/sessionExpiry";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  window.sessionStorage.clear();
  window.history.replaceState(null, "", window.location.pathname);
});

function signedOutFetch(entraAvailable: boolean) {
  return vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url.endsWith("/api/v1/setup/status")) {
      return Response.json({
        completed: true,
        bootstrap_available: false,
        entra_available: entraAvailable,
      });
    }
    if (url.endsWith("/api/v1/me")) {
      return new Response(null, { status: 401 });
    }
    return new Response("not found", { status: 404 });
  });
}

function authenticatedFetch(navigation = ["work", "sales"]) {
  return vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url.endsWith("/api/v1/setup/status")) {
      return Response.json({
        completed: true,
        bootstrap_available: false,
        entra_available: true,
      });
    }
    if (url.endsWith("/api/v1/me")) {
      return Response.json({
        id: "technician-id",
        navigation,
        capabilities: ["work_record.read"],
      });
    }
    if (url.endsWith("/api/v1/directory")) {
      return Response.json({
        clients: [
          {
            id: "client-id",
            display_id: "CLIENT-001",
            name: "Northwind Legal",
          },
        ],
        departments: [],
        teams: [],
        queues: [],
      });
    }
    if (url.endsWith("/auth/session/refresh")) {
      return new Response(null, { status: 204 });
    }
    return new Response("not found", { status: 404 });
  });
}

describe("authenticated application boundary", () => {
  it("does not mount the shell or product preview while access is unresolved", () => {
    window.location.hash = "#/sales";
    vi.stubGlobal(
      "fetch",
      vi.fn(() => new Promise<Response>(() => {})),
    );

    render(<App build={{ revision: "abc123" }} />);

    expect(screen.getByRole("status")).toHaveTextContent("Loading Rarity");
    expect(
      screen.queryByRole("navigation", { name: "Primary" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText("Northwind security modernization"),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /Notifications,/ }),
    ).not.toBeInTheDocument();
  });

  it("routes signed-out Entra users to a Microsoft-only login", async () => {
    window.location.hash = "#/sales";
    vi.stubGlobal("fetch", signedOutFetch(true));

    render(<App build={{ revision: "abc123" }} />);

    expect(
      await screen.findByRole("link", { name: "Sign in with Microsoft" }),
    ).toBeVisible();
    expect(window.location.hash).toBe("#/login");
    expect(
      window.sessionStorage.getItem("rarity.post-authentication-return"),
    ).toBe("#/sales");
    expect(screen.queryByLabelText("Username")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("navigation", { name: "Primary" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText("Northwind security modernization"),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /Notifications,/ }),
    ).not.toBeInTheDocument();
  });

  it("shows local sign-in for an Entra-free installation", async () => {
    window.location.hash = "#/work";
    vi.stubGlobal("fetch", signedOutFetch(false));

    render(<App build={{ revision: "abc123" }} />);

    expect(await screen.findByLabelText("Username")).toBeVisible();
    expect(
      screen.queryByRole("link", { name: "Sign in with Microsoft" }),
    ).not.toBeInTheDocument();
  });

  it("allows only the separate local recovery route in Entra mode", async () => {
    window.location.hash = "#/break-glass";
    vi.stubGlobal("fetch", signedOutFetch(true));

    render(<App build={{ revision: "abc123" }} />);

    expect(
      await screen.findByRole("heading", {
        name: "Local administrator sign-in",
      }),
    ).toBeVisible();
    expect(window.location.hash).toBe("#/break-glass");
    expect(
      screen.queryByRole("link", { name: "Sign in with Microsoft" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /Notifications,/ }),
    ).not.toBeInTheDocument();
  });

  it.each([
    ["entra", "Microsoft sign-in was not completed. Try again."],
    ["unavailable", "Microsoft sign-in is temporarily unavailable."],
  ])("renders the safe %s callback error", async (code, message) => {
    window.history.replaceState(null, "", `#/login?error=${code}`);
    vi.stubGlobal("fetch", signedOutFetch(true));

    render(<App build={{ revision: "abc123" }} />);

    expect(await screen.findByRole("alert")).toHaveTextContent(message);
  });

  it("does not render arbitrary callback error text", async () => {
    window.history.replaceState(
      null,
      "",
      "#/login?error=provider-secret-description",
    );
    vi.stubGlobal("fetch", signedOutFetch(true));

    render(<App build={{ revision: "abc123" }} />);

    await screen.findByRole("link", { name: "Sign in with Microsoft" });
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(
      screen.queryByText("provider-secret-description"),
    ).not.toBeInTheDocument();
  });

  it("renders incomplete setup without the application shell", async () => {
    window.location.hash = "#/sales";
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        Response.json({
          completed: false,
          bootstrap_available: true,
          entra_available: false,
        }),
      ),
    );

    render(<App build={{ revision: "abc123" }} />);

    expect(
      await screen.findByRole("heading", {
        name: "Configure this Rarity installation",
      }),
    ).toBeVisible();
    expect(window.location.hash).toBe("#/setup");
    expect(
      screen.queryByRole("navigation", { name: "Primary" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /Notifications,/ }),
    ).not.toBeInTheDocument();
  });

  it("fails closed with Retry when setup status cannot be resolved", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(null, { status: 500 })),
    );

    render(<App build={{ revision: "abc123" }} />);

    expect(
      await screen.findByRole("heading", { name: "Rarity is unavailable" }),
    ).toBeVisible();
    expect(
      screen.queryByRole("navigation", { name: "Primary" }),
    ).not.toBeInTheDocument();
  });

  it("does not remember an unknown signed-out hash", async () => {
    window.location.hash = "#/unknown";
    vi.stubGlobal("fetch", signedOutFetch(true));

    render(<App build={{ revision: "abc123" }} />);

    await screen.findByRole("link", { name: "Sign in with Microsoft" });
    expect(window.location.hash).toBe("#/login");
    expect(
      window.sessionStorage.getItem("rarity.post-authentication-return"),
    ).toBeNull();
  });

  it("lands on the role-aware Home after authentication", async () => {
    window.sessionStorage.setItem(
      "rarity.post-authentication-return",
      "#/work",
    );
    window.location.hash = "#/login";
    vi.stubGlobal("fetch", authenticatedFetch(["work", "sales"]));

    render(<App build={{ revision: "abc123" }} />);

    expect(
      await screen.findByRole("navigation", { name: "Primary" }),
    ).toBeVisible();
    await waitFor(() => expect(window.location.hash).toBe("#/home"));
    expect(
      await screen.findByRole("heading", { name: "Your operational home" }),
    ).toBeVisible();
  });

  it("rejects a remembered route outside the principal navigation", async () => {
    window.sessionStorage.setItem(
      "rarity.post-authentication-return",
      "#/sales",
    );
    window.location.hash = "#/login";
    vi.stubGlobal("fetch", authenticatedFetch(["work"]));

    render(<App build={{ revision: "abc123" }} />);

    await screen.findByRole("navigation", { name: "Primary" });
    await waitFor(() => expect(window.location.hash).toBe("#/home"));
    expect(
      screen.queryByRole("link", { name: "Sales" }),
    ).not.toBeInTheDocument();
  });

  it("propagates production-shaped principal capabilities into the AI workspace catalog", async () => {
    const fetcher = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.endsWith("/api/v1/setup/status")) {
        return Response.json({
          completed: true,
          bootstrap_available: false,
          entra_available: true,
        });
      }
      if (url.endsWith("/api/v1/me")) {
        return Response.json({
          id: "technician-id",
          navigation: ["work", "ai-assist"],
          capabilities: ["ai.assist", "work_record.transition"],
        });
      }
      if (url.endsWith("/api/v1/directory")) {
        return Response.json({
          clients: [
            {
              id: "client-id",
              display_id: "CLIENT-001",
              name: "Northwind Legal",
            },
          ],
          departments: [],
          teams: [],
          queues: [],
        });
      }
      if (url.endsWith("/api/v1/ai/workspace/conversations")) {
        return Response.json([]);
      }
      return new Response("not found", { status: 404 });
    });
    vi.stubGlobal("fetch", fetcher);
    render(<App build={{ revision: "abc123" }} />);

    await screen.findByRole("navigation", { name: "Primary" });
    fireEvent.click(screen.getByRole("button", { name: "Open AI workspace" }));
    fireEvent.click(
      await screen.findByRole("button", { name: "Prepare action" }),
    );

    const action = screen.getByRole("combobox", { name: "Action" });
    expect(action).toHaveTextContent("Change status");
    expect(action).not.toHaveTextContent("Change priority");
    expect(action).not.toHaveTextContent("Add client-visible reply");
  });

  it("closes the shell and preserves one return route on session expiry", async () => {
    window.history.replaceState(null, "", "#/sales");
    vi.stubGlobal("fetch", authenticatedFetch(["work", "sales"]));
    await act(async () => {
      render(<App build={{ revision: "abc123" }} />);
    });
    await screen.findByRole("navigation", { name: "Primary" });

    act(() => {
      window.dispatchEvent(new CustomEvent(sessionExpiredEvent));
      window.dispatchEvent(new CustomEvent(sessionExpiredEvent));
    });

    expect(
      await screen.findByRole("link", { name: "Sign in with Microsoft" }),
    ).toBeVisible();
    expect(window.location.hash).toBe("#/login");
    expect(
      window.sessionStorage.getItem("rarity.post-authentication-return"),
    ).toBe("#/sales");
    expect(
      screen.queryByRole("navigation", { name: "Primary" }),
    ).not.toBeInTheDocument();
  });

  it.each([204, 500])(
    "explicit logout status %s clears return state and closes the shell",
    async (logoutStatus) => {
      window.history.replaceState(null, "", "#/sales");
      const fetcher = authenticatedFetch(["work", "sales"]);
      fetcher.mockImplementation(async (input: RequestInfo | URL) => {
        if (String(input).endsWith("/auth/logout")) {
          return new Response(null, { status: logoutStatus });
        }
        const url = String(input);
        if (url.endsWith("/api/v1/setup/status")) {
          return Response.json({
            completed: true,
            bootstrap_available: false,
            entra_available: true,
          });
        }
        if (url.endsWith("/api/v1/me")) {
          return Response.json({
            id: "technician-id",
            navigation: ["work", "sales"],
            capabilities: ["work_record.read"],
          });
        }
        if (url.endsWith("/api/v1/directory")) {
          return Response.json({
            clients: [],
            departments: [],
            teams: [],
            queues: [],
          });
        }
        if (url.endsWith("/auth/session/refresh")) {
          return new Response(null, { status: 204 });
        }
        return new Response("not found", { status: 404 });
      });
      vi.stubGlobal("fetch", fetcher);
      render(<App build={{ revision: "abc123" }} />);
      await screen.findByRole("navigation", { name: "Primary" });
      window.sessionStorage.setItem(
        "rarity.post-authentication-return",
        "#/work",
      );
      window.sessionStorage.setItem(
        "rti:workspace:v2:technician-id",
        "principal-scoped-workspace",
      );

      fireEvent.click(screen.getByRole("button", { name: "Sign out" }));

      expect(
        await screen.findByRole("link", { name: "Sign in with Microsoft" }),
      ).toBeVisible();
      expect(window.location.hash).toBe("#/login");
      expect(
        window.sessionStorage.getItem("rarity.post-authentication-return"),
      ).toBeNull();
      expect(
        window.sessionStorage.getItem("rti:workspace:v2:technician-id"),
      ).toBeNull();
    },
  );
});
