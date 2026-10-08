import axe from "axe-core";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

import { WebhooksPage } from "./WebhooksPage";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

it("renders scoped connection metadata and durable delivery failures", async () => {
  const fetcher = vi.fn(async (input: RequestInfo | URL) =>
    String(input).endsWith("/connections")
      ? Response.json([
          {
            id: "connection-1",
            name: "Ticket events",
            direction: "outbound",
            endpoint_url: "https://hooks.example.test/rarity",
            credential_configured: true,
            enabled: true,
            event_types: ["work_record.created"],
            retry_window_seconds: 86400,
            version: 2,
          },
        ])
      : Response.json([
          {
            connection_id: "connection-1",
            connection_name: "Ticket events",
            event_id: "event-1",
            event_type: "work_record.created",
            state: "failed",
            attempt_count: 7,
            error_code: "remote_5xx",
          },
        ]),
  );
  vi.stubGlobal("fetch", fetcher);
  const { container } = render(
    <WebhooksPage
      clientID="client-1"
      capabilities={new Set(["integration.read", "integration.manage"])}
    />,
  );

  expect(
    await screen.findByRole("heading", { name: "Ticket events" }),
  ).toBeInTheDocument();
  expect(screen.getByText("remote_5xx")).toBeInTheDocument();
  expect(screen.getByText("Configured")).toBeInTheDocument();
  expect(
    screen.getByRole("heading", { name: "Availability" }),
  ).toBeInTheDocument();
  expect(
    screen.getByRole("heading", { name: "Signing credential" }),
  ).toBeInTheDocument();
  expect(
    screen.getByRole("heading", { name: "Signing credential" }).closest("form"),
  ).toHaveClass("settings-action-form");
  expect(fetcher).toHaveBeenCalledWith(
    "/api/v1/integrations/webhooks/deliveries",
    expect.objectContaining({ headers: { "X-Rarity-Client-ID": "client-1" } }),
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

it("does not request connection configuration for read-only operators", async () => {
  const fetcher = vi.fn(async () => Response.json([]));
  vi.stubGlobal("fetch", fetcher);
  render(
    <WebhooksPage
      clientID="client-1"
      capabilities={new Set(["integration.read"])}
    />,
  );
  expect(
    await screen.findByText(
      "No outbound webhook deliveries exist for this client.",
    ),
  ).toBeInTheDocument();
  expect(fetcher).toHaveBeenCalledTimes(1);
  expect(fetcher).toHaveBeenCalledWith(
    "/api/v1/integrations/webhooks/deliveries",
    expect.anything(),
  );
});

it("submits signing credentials through the write-only mutation boundary", async () => {
  document.cookie = "rarity_csrf=csrf-token";
  const fetcher = vi.fn(
    async (_input: RequestInfo | URL, init?: RequestInit) =>
      init?.method === "POST"
        ? Response.json({ id: "created", version: 1 }, { status: 201 })
        : Response.json([]),
  );
  vi.stubGlobal("fetch", fetcher);
  render(
    <WebhooksPage
      clientID="client-1"
      capabilities={new Set(["integration.read", "integration.manage"])}
    />,
  );
  await screen.findByRole("heading", { name: "Create connection" });
  fireEvent.change(screen.getByLabelText("Name"), {
    target: { value: "Events" },
  });
  fireEvent.change(screen.getByLabelText("Endpoint URL"), {
    target: { value: "https://hooks.example.test/rarity" },
  });
  fireEvent.change(screen.getByLabelText("Event types"), {
    target: { value: "work_record.created" },
  });
  fireEvent.keyDown(screen.getByLabelText("Event types"), { key: "Enter" });
  fireEvent.change(screen.getByLabelText("Signing secret"), {
    target: { value: "12345678901234567890123456789012" },
  });
  fireEvent.change(screen.getByLabelText("Reason"), {
    target: { value: "New integration" },
  });
  fireEvent.click(
    screen.getByRole("button", { name: "Create webhook connection" }),
  );

  await waitFor(() =>
    expect(fetcher.mock.calls.some(([, init]) => init?.method === "POST")).toBe(
      true,
    ),
  );
  const mutation = fetcher.mock.calls.find(
    ([, init]) => init?.method === "POST",
  );
  expect(mutation?.[1]?.headers).toMatchObject({
    "X-Rarity-Client-ID": "client-1",
    "X-Rarity-CSRF": "csrf-token",
  });
  expect(String(mutation?.[1]?.body)).toContain(
    '"signing_secret":"12345678901234567890123456789012"',
  );
});
