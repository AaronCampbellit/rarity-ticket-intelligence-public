import axe from "axe-core";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

import { ForwardingSettingsPage } from "./ForwardingSettingsPage";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

it("lists forwarding health and creates a bounded configuration", async () => {
  document.cookie = "rarity_csrf=csrf-token";
  const connection = {
    id: "forwarding-1",
    intake_address: "intake@example.com",
    allowed_sender_domains: ["customer.example"],
    max_message_bytes: 26214400,
    rate_limit_per_minute: 60,
    enabled: true,
    health_state: "healthy",
    version: 1,
  };
  let created = false;
  const fetcher = vi.fn(
    async (_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === "POST") {
        created = true;
        return Response.json(connection, { status: 201 });
      }
      return Response.json(created ? [connection] : []);
    },
  );
  vi.stubGlobal("fetch", fetcher);
  const { container } = render(<ForwardingSettingsPage />);
  await screen.findByRole("heading", { name: "Create connection" });
  fireEvent.change(screen.getByLabelText("Intake address"), {
    target: { value: "intake@example.com" },
  });
  fireEvent.change(screen.getByLabelText("Allowed sender domains"), {
    target: { value: "customer.example" },
  });
  fireEvent.keyDown(screen.getByLabelText("Allowed sender domains"), {
    key: "Enter",
  });
  fireEvent.change(screen.getByLabelText("Reason"), {
    target: { value: "Protected mailbox configured" },
  });
  fireEvent.click(
    screen.getByRole("button", { name: "Create forwarding connection" }),
  );

  expect(
    await screen.findByRole("heading", { name: "intake@example.com" }),
  ).toBeInTheDocument();
  const mutation = fetcher.mock.calls.find(
    ([, init]) => init?.method === "POST",
  );
  expect(mutation?.[1]?.headers).toMatchObject({
    "X-Rarity-CSRF": "csrf-token",
  });
  expect(String(mutation?.[1]?.body)).toContain('"max_message_bytes":26214400');
  expect(String(mutation?.[1]?.body)).toContain(
    '"allowed_sender_domains":["customer.example"]',
  );
  await waitFor(() =>
    expect(
      screen.getByText("Forwarding connection updated."),
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
  render(<ForwardingSettingsPage />);
  expect(
    await screen.findByText("No forwarding connections are configured."),
  ).toBeInTheDocument();
});
