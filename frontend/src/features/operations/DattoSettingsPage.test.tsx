import axe from "axe-core";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

import { DattoSettingsPage } from "./DattoSettingsPage";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

it("creates a Datto connection through the write-only credential boundary", async () => {
  document.cookie = "rarity_csrf=csrf-token";
  const connection = {
    id: "datto-1",
    name: "Primary RMM",
    api_url: "https://example.centrastage.net",
    credential_configured: true,
    sync_interval_seconds: 900,
    enabled: true,
    health_state: "pending",
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
  const { container } = render(<DattoSettingsPage />);
  await screen.findByRole("heading", { name: "Add connection" });
  fireEvent.change(screen.getByLabelText("Display name"), {
    target: { value: "Primary RMM" },
  });
  fireEvent.change(screen.getByLabelText("Datto API URL"), {
    target: { value: "https://example.centrastage.net" },
  });
  fireEvent.change(screen.getByLabelText("API key"), {
    target: { value: "api-key" },
  });
  fireEvent.change(screen.getByLabelText("API secret"), {
    target: { value: "top-secret-value" },
  });
  fireEvent.change(screen.getByLabelText("Reason"), {
    target: { value: "Onboard RMM" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Add Datto connection" }));
  expect(
    await screen.findByRole("heading", { name: "Primary RMM" }),
  ).toBeInTheDocument();
  expect(
    screen.getByRole("heading", { name: "Availability" }),
  ).toBeInTheDocument();
  expect(
    screen.getByRole("heading", { name: "Scheduling" }),
  ).toBeInTheDocument();
  expect(
    screen.getByRole("heading", { name: "Replace credential" }),
  ).toBeInTheDocument();
  expect(
    screen.getByRole("heading", { name: "Scheduling" }).closest("form"),
  ).toHaveClass("settings-action-form");
  const mutation = fetcher.mock.calls.find(
    ([, init]) => init?.method === "POST",
  );
  expect(mutation?.[1]?.headers).toMatchObject({
    "X-Rarity-CSRF": "csrf-token",
  });
  expect(String(mutation?.[1]?.body)).toContain(
    '"api_secret":"top-secret-value"',
  );
  await waitFor(() =>
    expect(
      screen.getByText("Datto connection configuration updated."),
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
  render(<DattoSettingsPage />);
  expect(
    await screen.findByText("No Datto connections are configured."),
  ).toBeInTheDocument();
});
