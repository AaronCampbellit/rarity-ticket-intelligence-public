import axe from "axe-core";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

import { AutomationPage } from "./AutomationPage";
import { __resetClientClassificationCatalogForTests } from "../classification/useClientCatalog";

afterEach(() => {
  cleanup();
  __resetClientClassificationCatalogForTests();
  vi.unstubAllGlobals();
});

it("authors an accessible catalog-backed tag condition for a managed client", async () => {
  const groupID = "22222222-2222-4222-8222-222222222222";
  const tagID = "11111111-1111-4111-8111-111111111111";
  const fetcher = vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url === "/api/v1/tag-groups") {
      return Response.json([
        {
          id: groupID,
          label: "Service",
          description: "",
          state: "active",
          position: 1,
          version: 1,
        },
      ]);
    }
    if (url === "/api/v1/tags") {
      return Response.json([
        {
          id: tagID,
          label: "Network",
          group_id: groupID,
          state: "active",
          synonyms: [],
          version: 1,
        },
      ]);
    }
    return Response.json([]);
  });
  vi.stubGlobal("fetch", fetcher);
  const { container } = render(
    <AutomationPage
      clientID="client-1"
      capabilities={new Set(["automation.manage"])}
    />,
  );

  expect(
    await screen.findByText("No automation definitions exist for this client."),
  ).toBeInTheDocument();
  fireEvent.click(screen.getByText("Create automation definition"));
  fireEvent.click(screen.getByRole("button", { name: "Add condition" }));
  const picker = screen.getByRole("combobox", { name: /Condition tags/ });
  fireEvent.focus(picker);
  fireEvent.click(await screen.findByRole("option", { name: "Network" }));
  expect(
    screen.getByLabelText("Network. Selected by a technician"),
  ).toBeInTheDocument();
  expect(fetcher).toHaveBeenCalledWith(
    "/api/v1/tag-groups",
    expect.objectContaining({ headers: { "X-Rarity-Client-ID": "client-1" } }),
  );
  expect(fetcher).toHaveBeenCalledWith(
    "/api/v1/tags",
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

it("lists typed definitions and applies a reasoned dead-letter action", async () => {
  document.cookie = "rarity_csrf=csrf-token";
  const definition = {
    id: "automation-1",
    name: "Acknowledge new work",
    version: 2,
    state: "published",
    record_version: 4,
    trigger: { event_type: "work_record.created" },
    capabilities: ["work_record.comment"],
    steps: [
      {
        id: "comment",
        kind: "action",
        action: {
          kind: "add_comment",
          parameters: { body: "Acknowledged" },
        },
      },
    ],
  };
  const deadLetter = {
    id: "dead-1",
    automation_id: "automation-1",
    automation_version: 2,
    created_at: "2026-07-30T15:00:00Z",
    error_code: "connection_failed",
    safe_message: "The approved connection could not be reached.",
    state: "open",
  };
  const fetcher = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (init?.method === "POST")
        return Response.json({ ...deadLetter, state: "retrying" });
      if (url.includes("dead-letters")) return Response.json([deadLetter]);
      return Response.json([definition]);
    },
  );
  vi.stubGlobal("fetch", fetcher);
  const { container } = render(
    <AutomationPage
      clientID="client-1"
      capabilities={
        new Set(["automation.manage", "automation.dead_letter.manage"])
      }
    />,
  );

  expect(await screen.findByText("Acknowledge new work")).toBeInTheDocument();
  expect(screen.getByText("connection_failed")).toBeInTheDocument();
  fireEvent.change(screen.getByLabelText("Reason"), {
    target: { value: "Connection repaired" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Apply action" }));
  await waitFor(() =>
    expect(
      screen.getByText("Automation configuration updated."),
    ).toBeInTheDocument(),
  );

  const mutation = fetcher.mock.calls.find(
    ([url, init]) =>
      String(url).includes("/dead-letters/dead-1/actions") &&
      init?.method === "POST",
  );
  expect(mutation?.[1]?.headers).toMatchObject({
    "X-Rarity-Client-ID": "client-1",
    "X-Rarity-CSRF": "csrf-token",
  });
  expect(String(mutation?.[1]?.body)).toContain(
    '"reason":"Connection repaired"',
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

it("loads only the automation panels granted to the principal", async () => {
  const fetcher = vi.fn(async () => Response.json([]));
  vi.stubGlobal("fetch", fetcher);
  render(
    <AutomationPage
      clientID="client-1"
      capabilities={new Set(["automation.dead_letter.manage"])}
    />,
  );
  expect(
    await screen.findByText(
      "No automation dead letters exist for this client.",
    ),
  ).toBeInTheDocument();
  expect(
    screen.queryByRole("heading", { name: "Automation definitions" }),
  ).not.toBeInTheDocument();
  expect(fetcher).toHaveBeenCalledTimes(1);
  expect(fetcher).toHaveBeenCalledWith(
    "/api/v1/automation/dead-letters",
    expect.objectContaining({
      headers: { "X-Rarity-Client-ID": "client-1" },
    }),
  );
});

it("creates a scoped external HTTP connection for typed automation steps", async () => {
  document.cookie = "rarity_csrf=csrf-token";
  const fetcher = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      if (
        String(input) === "/api/v1/automation/connections" &&
        init?.method === "POST"
      ) {
        return Response.json(
          {
            id: "connection-1",
            msp_id: "msp-1",
            client_id: "client-1",
            name: "Synthetic callback",
            endpoint: "https://httpbin.org/status/204",
          },
          { status: 201 },
        );
      }
      return Response.json([]);
    },
  );
  vi.stubGlobal("fetch", fetcher);
  render(
    <AutomationPage
      clientID="client-1"
      capabilities={new Set(["automation.manage"])}
    />,
  );

  expect(
    await screen.findByText("No automation definitions exist for this client."),
  ).toBeInTheDocument();
  fireEvent.change(screen.getByLabelText("Connection name"), {
    target: { value: "Synthetic callback" },
  });
  fireEvent.change(screen.getByLabelText("HTTPS endpoint"), {
    target: { value: "https://httpbin.org/status/204" },
  });
  fireEvent.change(
    screen.getByLabelText("Signing secret environment reference"),
    {
      target: {
        value: "env://RARITY_AUTOMATION_HTTP_SECRET_ACCEPTANCE",
      },
    },
  );
  fireEvent.click(
    screen.getByRole("button", { name: "Create external connection" }),
  );

  expect(
    await screen.findByText("External connection created: connection-1"),
  ).toBeInTheDocument();
  const mutation = fetcher.mock.calls.find(
    ([url, init]) =>
      String(url) === "/api/v1/automation/connections" &&
      init?.method === "POST",
  );
  expect(mutation?.[1]?.headers).toMatchObject({
    "X-Rarity-Client-ID": "client-1",
    "X-Rarity-CSRF": "csrf-token",
  });
  expect(JSON.parse(String(mutation?.[1]?.body))).toEqual({
    name: "Synthetic callback",
    endpoint: "https://httpbin.org/status/204",
    signing_secret_ref: "env://RARITY_AUTOMATION_HTTP_SECRET_ACCEPTANCE",
  });
});
