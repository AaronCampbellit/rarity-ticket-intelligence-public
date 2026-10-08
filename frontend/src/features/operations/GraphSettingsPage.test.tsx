import axe from "axe-core";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

import { GraphSettingsPage } from "./GraphSettingsPage";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

it("creates a mailbox through the write-only credential boundary", async () => {
  document.cookie = "rarity_csrf=csrf-token";
  const mailbox = {
    id: "graph-1",
    mailbox_address: "support@example.com",
    tenant_id: "tenant",
    client_id: "application",
    credential_configured: true,
    client_state_configured: true,
    enabled: true,
    health_state: "pending",
    version: 1,
  };
  let created = false;
  const fetcher = vi.fn(
    async (_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === "POST") {
        created = true;
        return Response.json(mailbox, { status: 201 });
      }
      return Response.json(created ? [mailbox] : []);
    },
  );
  vi.stubGlobal("fetch", fetcher);
  const { container } = render(<GraphSettingsPage />);
  await screen.findByRole("heading", { name: "Add mailbox" });
  fireEvent.change(screen.getByLabelText("Mailbox address"), {
    target: { value: "support@example.com" },
  });
  fireEvent.change(screen.getByLabelText("Tenant ID"), {
    target: { value: "tenant" },
  });
  fireEvent.change(screen.getByLabelText("Application client ID"), {
    target: { value: "application" },
  });
  fireEvent.change(screen.getByLabelText("Application client secret"), {
    target: { value: "top-secret-value" },
  });
  fireEvent.change(screen.getByLabelText("Reason"), {
    target: { value: "Support mailbox onboarding" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Add Graph mailbox" }));
  expect(
    await screen.findByRole("heading", { name: "support@example.com" }),
  ).toBeInTheDocument();
  expect(
    screen.getByRole("heading", { name: "Availability" }),
  ).toBeInTheDocument();
  expect(
    screen.getByRole("heading", { name: "Replace credential" }),
  ).toBeInTheDocument();
  expect(
    screen.getByRole("heading", { name: "Availability" }).closest("form"),
  ).toHaveClass("settings-action-form");
  const mutation = fetcher.mock.calls.find(
    ([, init]) => init?.method === "POST",
  );
  expect(mutation?.[1]?.headers).toMatchObject({
    "X-Rarity-CSRF": "csrf-token",
  });
  expect(String(mutation?.[1]?.body)).toContain(
    '"client_secret":"top-secret-value"',
  );
  await waitFor(() =>
    expect(
      screen.getByText("Graph mailbox configuration updated."),
    ).toBeInTheDocument(),
  );
  const result = await axe.run(container, {
    runOnly: {
      type: "tag",
      values: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"],
    },
    rules: { "color-contrast": { enabled: false } },
  });
  expect(result.violations).toEqual([]);
});

it("shows a truthful empty state", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json([])),
  );
  render(<GraphSettingsPage />);
  expect(
    await screen.findByText("No Graph mailboxes are configured."),
  ).toBeInTheDocument();
});
