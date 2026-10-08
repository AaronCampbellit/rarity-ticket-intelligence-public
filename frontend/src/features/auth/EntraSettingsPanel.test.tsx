import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { EntraSettingsPanel } from "./EntraSettingsPanel";

describe("EntraSettingsPanel", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("stages write-only settings, verifies discovery, and can return to local-only identity", async () => {
    const fetcher = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (!init?.method) {
          return new Response(
            JSON.stringify({
              state: "not_connected",
              version: 1,
              credential_configured: false,
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          );
        }
        const body = JSON.parse(String(init.body)) as Record<string, unknown>;
        if (init.method === "PUT") {
          return new Response(
            JSON.stringify({
              state: "verification_required",
              version: 2,
              tenant_id: body.tenant_id,
              client_id: body.client_id,
              redirect_url: body.redirect_url,
              credential_configured: true,
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          );
        }
        return new Response(
          JSON.stringify({
            state: url.endsWith("/verify") ? "connected" : "not_connected",
            version: url.endsWith("/verify") ? 3 : 4,
            credential_configured: url.endsWith("/verify"),
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      },
    );
    vi.stubGlobal("fetch", fetcher);

    render(<EntraSettingsPanel />);
    await screen.findByText("Not connected");
    fireEvent.change(screen.getByLabelText("Tenant ID"), {
      target: { value: "tenant-id" },
    });
    fireEvent.change(screen.getByLabelText("Client ID"), {
      target: { value: "client-id" },
    });
    fireEvent.change(screen.getByLabelText(/^Client secret/), {
      target: { value: "write-only-secret" },
    });
    fireEvent.change(screen.getByLabelText("Redirect URL"), {
      target: { value: "https://rarity.example/auth/callback" },
    });
    fireEvent.change(screen.getByLabelText("Configuration reason"), {
      target: { value: "connect workforce SSO" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Stage configuration" }),
    );
    await screen.findByText("Verification required");
    expect(
      screen.queryByDisplayValue("write-only-secret"),
    ).not.toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Verification reason"), {
      target: { value: "verify tenant discovery" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Verify configuration" }),
    );
    await screen.findByText("Connected");

    fireEvent.change(screen.getByLabelText("Disable reason"), {
      target: { value: "return to local only" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Disable Microsoft Entra" }),
    );
    await waitFor(() =>
      expect(screen.getByText("Not connected")).toBeInTheDocument(),
    );
  });
});
