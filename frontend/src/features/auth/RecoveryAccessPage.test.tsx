import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { RecoveryAccessPage } from "./RecoveryAccessPage";

describe("RecoveryAccessPage", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        if (String(input) === "/api/v1/admin/identity/entra" && !init?.method) {
          return new Response(
            JSON.stringify({
              state: "not_connected",
              version: 1,
              credential_configured: false,
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          );
        }
        if (init?.method === "POST") {
          return new Response(
            JSON.stringify({
              id: "account-2",
              technician_id: "technician-1",
              username: "recovery.admin",
              allowed_cidrs: ["10.0.0.0/24"],
              enabled: true,
              created_at: "2026-07-30T12:00:00Z",
              version: 1,
            }),
            { status: 201, headers: { "Content-Type": "application/json" } },
          );
        }
        return new Response("[]", {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }),
    );
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("does not expose local administrator creation before sign-in", () => {
    render(<RecoveryAccessPage authenticated={false} />);

    expect(
      screen.getByRole("heading", {
        name: "Sign in to manage local administrators",
      }),
    ).toBeVisible();
    expect(
      screen.getByRole("link", { name: "Local administrator sign-in" }),
    ).toHaveAttribute("href", "#/break-glass");
    expect(
      screen.queryByRole("button", { name: "Create local administrator" }),
    ).not.toBeInTheDocument();
  });

  it("creates a local Platform Administrator without rendering its password", async () => {
    render(<RecoveryAccessPage />);
    await screen.findByRole("heading", {
      name: "Add local Platform Administrator",
    });
    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "admin@example.test" },
    });
    fireEvent.change(screen.getByLabelText("Display name"), {
      target: { value: "Local Admin" },
    });
    fireEvent.change(screen.getByLabelText("Username"), {
      target: { value: "recovery.admin" },
    });
    fireEvent.change(screen.getByLabelText("Initial password"), {
      target: { value: "never-render-this-password" },
    });
    fireEvent.change(screen.getByLabelText("Allowed networks"), {
      target: { value: "10.0.0.0/24" },
    });
    fireEvent.keyDown(screen.getByLabelText("Allowed networks"), {
      key: "Enter",
    });
    fireEvent.change(screen.getByLabelText("Reason"), {
      target: { value: "outage recovery" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Create local administrator" }),
    );

    await screen.findByRole("heading", { name: "recovery.admin" });
    expect(
      screen.queryByDisplayValue("never-render-this-password"),
    ).not.toBeInTheDocument();
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(3));
  });

  it("provides versioned password and network controls without rendering secrets", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (url === "/api/v1/admin/identity/entra" && !init?.method) {
          return new Response(
            JSON.stringify({
              state: "not_connected",
              version: 1,
              credential_configured: false,
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          );
        }
        if (!init?.method) {
          return new Response(
            JSON.stringify([
              {
                id: "account-1",
                technician_id: "technician-1",
                username: "local-admin",
                allowed_cidrs: ["10.0.0.0/8"],
                enabled: true,
                created_at: "2026-08-02T00:00:00Z",
                version: 3,
              },
            ]),
            { status: 200, headers: { "Content-Type": "application/json" } },
          );
        }
        const body = JSON.parse(String(init.body)) as Record<string, unknown>;
        if (url.endsWith("/password")) {
          return new Response(
            JSON.stringify({
              id: "account-1",
              enabled: true,
              version: Number(body.expected_version) + 1,
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          );
        }
        return new Response(
          JSON.stringify({
            id: "account-1",
            allowed_cidrs: body.allowed_cidrs,
            enabled: true,
            version: Number(body.expected_version) + 1,
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      }),
    );

    render(<RecoveryAccessPage />);
    const heading = await screen.findByRole("heading", { name: "local-admin" });
    const accountCard = heading.closest("article");
    expect(accountCard).not.toBeNull();

    fireEvent.click(
      within(accountCard as HTMLElement).getByRole("button", {
        name: "Reset password",
      }),
    );
    fireEvent.change(screen.getByLabelText("New password"), {
      target: { value: "a new sufficiently long password" },
    });
    fireEvent.change(screen.getByLabelText("Confirm new password"), {
      target: { value: "a different sufficiently long password" },
    });
    fireEvent.change(screen.getByLabelText("Password reset reason"), {
      target: { value: "scheduled rotation" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save new password" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Passwords do not match.",
    );
    expect(fetch).toHaveBeenCalledTimes(2);

    fireEvent.change(screen.getByLabelText("Confirm new password"), {
      target: { value: "a new sufficiently long password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save new password" }));
    await screen.findByText("Local administrator password reset.");
    expect(
      screen.queryByDisplayValue("a new sufficiently long password"),
    ).not.toBeInTheDocument();

    fireEvent.click(
      within(accountCard as HTMLElement).getByRole("button", {
        name: "Edit networks",
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Remove 10.0.0.0/8" }));
    fireEvent.change(screen.getByLabelText("Updated allowed networks"), {
      target: { value: "192.168.86.0/24" },
    });
    fireEvent.keyDown(screen.getByLabelText("Updated allowed networks"), {
      key: "Enter",
    });
    fireEvent.change(screen.getByLabelText("Network change reason"), {
      target: { value: "administration subnet changed" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save networks" }));
    await screen.findByText("Networks: 192.168.86.0/24");
  });
});
