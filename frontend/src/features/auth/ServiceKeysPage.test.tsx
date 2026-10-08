import axe from "axe-core";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

import "../../app.css";
import { ServiceKeysPage } from "./ServiceKeysPage";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

it("lists metadata and reveals an issued token only from the mutation response", async () => {
  document.cookie = "rarity_csrf=csrf-token";
  const key = {
    id: "key-1",
    name: "Monitoring",
    prefix: "prefix123",
    capabilities: ["work_record.create"],
    data_scopes: ["work_records"],
    created_at: "2026-07-30T15:00:00Z",
    expires_at: "2027-07-30T15:00:00Z",
  };
  const fetcher = vi.fn(
    async (_input: RequestInfo | URL, init?: RequestInit) =>
      init?.method === "POST"
        ? Response.json({ ...key, id: "key-2", token: "rsk_prefix_secret" })
        : Response.json([key]),
  );
  vi.stubGlobal("fetch", fetcher);
  const { container } = render(<ServiceKeysPage clientID="client-1" />);

  expect(await screen.findByText("Monitoring")).toBeInTheDocument();
  fireEvent.change(screen.getByLabelText("Name"), {
    target: { value: "Ticket intake" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Choose Capabilities" }));
  fireEvent.click(screen.getByRole("option", { name: "Create work records" }));
  fireEvent.click(screen.getByRole("button", { name: "Choose Data scopes" }));
  fireEvent.click(screen.getByRole("option", { name: "Work records" }));
  fireEvent.click(screen.getByRole("button", { name: "Issue service key" }));

  expect(
    await screen.findByDisplayValue("rsk_prefix_secret"),
  ).toBeInTheDocument();
  expect(
    screen.getByText(
      "Copy the new token now. Rarity will not display it again.",
    ),
  ).toBeInTheDocument();
  const mutation = fetcher.mock.calls.find(
    ([, init]) => init?.method === "POST",
  );
  expect(mutation?.[1]?.headers).toMatchObject({
    "X-Rarity-Client-ID": "client-1",
    "X-Rarity-CSRF": "csrf-token",
  });
  expect(String(mutation?.[1]?.body)).not.toContain("token");

  const result = await axe.run(container, {
    runOnly: {
      type: "tag",
      values: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"],
    },
    rules: { "color-contrast": { enabled: false } },
  });
  expect(result.violations).toEqual([]);
});

it("requires a reason to rotate or revoke", async () => {
  const key = {
    id: "key-1",
    name: "Monitoring",
    prefix: "prefix123",
    capabilities: ["work_record.create"],
    data_scopes: ["work_records"],
    created_at: "2026-07-30T15:00:00Z",
    expires_at: "2027-07-30T15:00:00Z",
  };
  const fetcher = vi.fn(async () => Response.json([key]));
  vi.stubGlobal("fetch", fetcher);
  render(<ServiceKeysPage clientID="client-1" />);
  expect(await screen.findByText("Monitoring")).toBeInTheDocument();
  expect(
    screen.getByRole("heading", { name: "Rotate" }).closest("form"),
  ).toHaveClass("settings-action-form");
  expect(
    screen.getByRole("heading", { name: "Revoke" }).closest("form"),
  ).toHaveClass("settings-action-form");

  fireEvent.click(screen.getByRole("button", { name: "Revoke key" }));
  await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1));
});

it("lays out legacy settings forms as a responsive field grid", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json([])),
  );
  render(<ServiceKeysPage clientID="client-1" />);

  const form = await screen
    .findByRole("button", { name: "Issue service key" })
    .then((button) => button.closest("form"));
  const nameLabel = screen.getByText("Name").closest("label");

  expect(form).not.toBeNull();
  expect(nameLabel).not.toBeNull();
  expect(getComputedStyle(form!).display).toBe("grid");
  expect(getComputedStyle(nameLabel!).display).toBe("grid");
});
