import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { TeamsSettingsPage } from "./TeamsSettingsPage";
import type { TeamsConnection, TeamsSettingsAPI } from "./types";

const existing: TeamsConnection = {
  id: "connection-1",
  clientID: "client-1",
  name: "Service Desk",
  credentialConfigured: true,
  enabled: false,
  health: "pending",
  version: 2,
};

function api(overrides: Partial<TeamsSettingsAPI> = {}): TeamsSettingsAPI {
  return {
    list: vi.fn().mockResolvedValue([existing]),
    create: vi
      .fn()
      .mockResolvedValue({ ...existing, id: "connection-2", version: 1 }),
    updateName: vi
      .fn()
      .mockResolvedValue({ ...existing, name: "NOC", version: 3 }),
    setEnabled: vi.fn().mockResolvedValue({
      ...existing,
      enabled: true,
      health: "pending",
      version: 3,
    }),
    replaceCredential: vi.fn().mockResolvedValue({ ...existing, version: 3 }),
    test: vi
      .fn()
      .mockResolvedValue({ ...existing, health: "healthy", version: 3 }),
    ...overrides,
  };
}

afterEach(cleanup);

describe("TeamsSettingsPage", () => {
  it("creates a write-only connection and never redisplays its webhook", async () => {
    const user = userEvent.setup();
    const client = api({ list: vi.fn().mockResolvedValue([]) });
    render(<TeamsSettingsPage api={client} />);
    await screen.findByText(
      "No Teams connections are configured for this scope.",
    );

    await user.click(screen.getByRole("button", { name: "Add connection" }));
    await user.type(screen.getByLabelText("Connection name"), "Service Desk");
    const webhook = screen.getByLabelText("Incoming Webhook URL");
    expect(webhook).toHaveAttribute("type", "password");
    await user.type(webhook, "https://teams.example.test/hook/secret");
    await user.type(
      screen.getByLabelText("Audit reason"),
      "configure notifications",
    );
    await user.click(screen.getByRole("button", { name: "Save connection" }));

    expect(client.create).toHaveBeenCalledWith({
      name: "Service Desk",
      webhookURL: "https://teams.example.test/hook/secret",
      reason: "configure notifications",
    });
    expect(await screen.findByText(/connection created/i)).toBeVisible();
    expect(
      screen.queryByDisplayValue("https://teams.example.test/hook/secret"),
    ).not.toBeInTheDocument();
  });

  it("requires an audit reason for test, credential, and enable actions", async () => {
    const user = userEvent.setup();
    const client = api();
    render(<TeamsSettingsPage api={client} />);
    await screen.findByRole("heading", { name: "Service Desk" });

    expect(
      screen.getByRole("button", { name: "Test connection" }),
    ).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Enable delivery" }),
    ).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Replace webhook" }),
    ).toBeDisabled();

    await user.type(
      screen.getByLabelText("Audit reason for next action"),
      "validate before enablement",
    );
    await user.click(screen.getByRole("button", { name: "Test connection" }));
    await waitFor(() =>
      expect(client.test).toHaveBeenCalledWith("connection-1", {
        reason: "validate before enablement",
      }),
    );
    expect(
      await screen.findByText("Teams connection test succeeded."),
    ).toBeVisible();
  });

  it("preserves editable values when a mutation fails safely", async () => {
    const user = userEvent.setup();
    const client = api({
      replaceCredential: vi
        .fn()
        .mockRejectedValue(new Error("secret backend detail")),
    });
    render(<TeamsSettingsPage api={client} />);
    await screen.findByRole("heading", { name: "Service Desk" });
    const replacement = screen.getByLabelText("Replacement webhook URL");
    await user.type(replacement, "https://teams.example.test/new-secret");
    await user.type(
      screen.getByLabelText("Audit reason for next action"),
      "rotate credential",
    );
    await user.click(screen.getByRole("button", { name: "Replace webhook" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Teams connection request failed (request_failed).",
    );
    expect(replacement).toHaveValue("https://teams.example.test/new-secret");
    expect(screen.queryByText("secret backend detail")).not.toBeInTheDocument();
  });

  it("applies a failed test state so later mutations use its new version", async () => {
    const user = userEvent.setup();
    const failed = {
      ...existing,
      health: "failed" as const,
      lastErrorCode: "connection_test_failed",
      version: 3,
    };
    const client = api({ test: vi.fn().mockResolvedValue(failed) });
    render(<TeamsSettingsPage api={client} />);
    await screen.findByRole("heading", { name: "Service Desk" });

    const reason = screen.getByLabelText("Audit reason for next action");
    await user.type(reason, "validate callback");
    await user.click(screen.getByRole("button", { name: "Test connection" }));
    expect(await screen.findByText(/connection test failed/i)).toBeVisible();
    expect(
      screen.getByText(/Safe error:.*connection_test_failed/),
    ).toBeVisible();

    await user.type(reason, "enable after review");
    await user.click(screen.getByRole("button", { name: "Enable delivery" }));
    expect(client.setEnabled).toHaveBeenCalledWith(
      "connection-1",
      expect.objectContaining({ expectedVersion: 3 }),
    );
  });
});
