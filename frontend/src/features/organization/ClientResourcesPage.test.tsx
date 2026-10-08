import axe from "axe-core";
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";

import { ClientResourcesPage } from "./ClientResourcesPage";
import operationsStyles from "../operations/operations.css?raw";
import { __resetClientClassificationCatalogForTests } from "../classification/useClientCatalog";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  __resetClientClassificationCatalogForTests();
});

const activeResources = [
  {
    id: "00000000-0000-4000-8000-000000000101",
    kind: "location",
    display_id: "LOC-1",
    name: "Headquarters",
    lifecycle_state: "active",
    version: 2,
  },
  {
    id: "00000000-0000-4000-8000-000000000102",
    kind: "contact",
    display_id: "CONTACT-1",
    name: "Ada Lovelace",
    lifecycle_state: "active",
    version: 3,
  },
  {
    id: "00000000-0000-4000-8000-000000000103",
    kind: "asset",
    display_id: "AST-1",
    name: "Managed firewall",
    detail: "firewall",
    lifecycle_state: "active",
    authority: "discovered",
    version: 8,
  },
] as const;

it("renders a dense accessible lifecycle table with status filtering and discovered Asset lockout", async () => {
  const user = userEvent.setup();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) =>
      Response.json(
        String(input).includes("lifecycle=inactive")
          ? [
              {
                id: "00000000-0000-4000-8000-000000000104",
                kind: "service",
                display_id: "SVC-OLD",
                name: "Legacy monitoring",
                lifecycle_state: "inactive",
                version: 5,
              },
            ]
          : activeResources,
      ),
    ),
  );
  const { container } = render(
    <ClientResourcesPage
      clientID="client-1"
      capabilities={
        new Set([
          "search.read",
          "location.update",
          "location.lifecycle",
          "contact.update",
          "contact.lifecycle",
          "asset.update",
          "asset.lifecycle",
          "service.lifecycle",
        ])
      }
    />,
  );

  const table = await screen.findByRole("table", {
    name: "Client resource lifecycle catalog",
  });
  for (const heading of [
    "Kind",
    "Display ID",
    "Name",
    "Detail",
    "Status",
    "Version",
    "Actions",
  ]) {
    expect(
      within(table).getByRole("columnheader", { name: heading }),
    ).toBeVisible();
  }
  expect(within(table).getByText("CONTACT-1")).toBeVisible();
  expect(within(table).getAllByText("Active")).toHaveLength(3);
  expect(within(table).getByText("v3")).toBeVisible();

  const discoveredRow = within(table).getByText("AST-1").closest("tr");
  expect(discoveredRow).not.toBeNull();
  expect(within(discoveredRow!).getByText("Discovered · locked")).toBeVisible();
  expect(
    within(discoveredRow!).queryByRole("button", { name: "Edit AST-1" }),
  ).toBeNull();
  expect(
    within(discoveredRow!).queryByRole("button", { name: "Deactivate AST-1" }),
  ).toBeNull();

  await user.selectOptions(screen.getByLabelText("Lifecycle"), "inactive");
  expect(await screen.findByText("Legacy monitoring")).toBeVisible();
  expect(screen.queryByText("Ada Lovelace")).toBeNull();
  expect(
    screen.getByRole("button", { name: "Reactivate SVC-OLD" }),
  ).toBeVisible();

  const result = await axe.run(container, {
    runOnly: {
      type: "tag",
      values: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"],
    },
    rules: { "color-contrast": { enabled: false } },
  });
  expect(result.violations).toEqual([]);
});

it("uses the Signal catalog search and resource-type switcher without losing lifecycle actions", async () => {
  const user = userEvent.setup();
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(activeResources)),
  );
  render(
    <ClientResourcesPage
      clientID="client-1"
      capabilities={new Set(["search.read", "contact.update"])}
    />,
  );

  const search = await screen.findByRole("searchbox", {
    name: "Search client resources",
  });
  expect(
    screen.getByRole("button", { name: /All resources 3/ }),
  ).toHaveAttribute("aria-pressed", "true");

  await user.click(screen.getByRole("button", { name: /Contacts 1/ }));
  expect(screen.getByText("CONTACT-1")).toBeVisible();
  expect(screen.queryByText("LOC-1")).toBeNull();
  expect(screen.getByRole("button", { name: "Edit CONTACT-1" })).toBeVisible();

  await user.clear(search);
  await user.type(search, "no match");
  expect(screen.queryByText("CONTACT-1")).toBeNull();
  expect(screen.getByText("No matching client resources.")).toBeVisible();
});

it("edits an exact resource with quoted If-Match, reason, and explicit clears", async () => {
  document.cookie = "rarity_csrf=csrf-token";
  const user = userEvent.setup();
  const fetcher = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.includes("/contacts/") && !init?.method) {
        return Response.json({
          ...activeResources[1],
          display_name: "Ada Lovelace",
          email: "ada@example.test",
          phone: "555-0100",
          location_id: activeResources[0].id,
        });
      }
      if (init?.method === "PATCH") {
        return Response.json({ ...activeResources[1], version: 4 });
      }
      return Response.json(activeResources);
    },
  );
  vi.stubGlobal("fetch", fetcher);
  render(
    <ClientResourcesPage
      clientID="client-1"
      capabilities={new Set(["search.read", "contact.update"])}
    />,
  );

  const editButton = await screen.findByRole("button", {
    name: "Edit CONTACT-1",
  });
  editButton.focus();
  expect(editButton).toHaveFocus();
  await user.keyboard("{Enter}");
  expect(
    await screen.findByRole("dialog", { name: "Edit Contact CONTACT-1" }),
  ).toBeVisible();
  expect(screen.getByLabelText("Current version")).toHaveValue("3");
  await user.click(screen.getByLabelText("Clear email"));
  await user.clear(screen.getByLabelText("Display name"));
  await user.type(screen.getByLabelText("Display name"), "Ada Byron");
  await user.type(
    screen.getByLabelText("Change reason"),
    "Correct contact record",
  );
  await user.click(screen.getByRole("button", { name: "Save changes" }));

  await waitFor(() => {
    expect(screen.getByText("Contact CONTACT-1 updated.")).toBeVisible();
  });
  const mutation = fetcher.mock.calls.find(
    ([, init]) => init?.method === "PATCH",
  );
  expect(mutation?.[0]).toBe(
    "/api/v1/contacts/00000000-0000-4000-8000-000000000102",
  );
  expect(mutation?.[1]?.headers).toMatchObject({
    "Content-Type": "application/json",
    "If-Match": '"3"',
    "X-Rarity-Client-ID": "client-1",
    "X-Rarity-CSRF": "csrf-token",
  });
  expect(JSON.parse(String(mutation?.[1]?.body))).toEqual({
    expected_version: 3,
    reason: "Correct contact record",
    display_name: "Ada Byron",
    email: "",
  });
  expect(screen.queryByText("ada@example.test")).toBeNull();
  expect(screen.queryByText("555-0100")).toBeNull();
});

it("requires a reason for lifecycle changes and presents safe conflict feedback", async () => {
  const user = userEvent.setup();
  const fetcher = vi.fn(
    async (_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === "POST") {
        return Response.json(
          { error: { code: "resource_in_use", message: "resource is in use" } },
          { status: 409 },
        );
      }
      return Response.json(activeResources);
    },
  );
  vi.stubGlobal("fetch", fetcher);
  render(
    <ClientResourcesPage
      clientID="client-1"
      capabilities={new Set(["search.read", "location.lifecycle"])}
    />,
  );

  await user.click(
    await screen.findByRole("button", { name: "Deactivate LOC-1" }),
  );
  expect(
    screen.getByText("Version 2 will be required at confirmation."),
  ).toBeVisible();
  await user.click(
    screen.getByRole("button", { name: "Confirm deactivation" }),
  );
  expect(screen.getByText("Enter a reason.")).toBeVisible();
  await user.type(screen.getByLabelText("Reason"), "Office closed");
  await user.click(
    screen.getByRole("button", { name: "Confirm deactivation" }),
  );

  expect(
    await screen.findByRole("alert", {
      name: "Client resource change blocked",
    }),
  ).toHaveTextContent("in use");
  const mutation = fetcher.mock.calls.find(
    ([, init]) => init?.method === "POST",
  );
  expect(mutation?.[1]?.headers).toMatchObject({ "If-Match": '"2"' });
  expect(JSON.parse(String(mutation?.[1]?.body))).toEqual({
    expected_version: 2,
    reason: "Office closed",
  });
});

it("keeps compact creation access and creates a technician-confirmed Asset", async () => {
  document.cookie = "rarity_csrf=csrf-token";
  const user = userEvent.setup();
  let assetCreates = 0;
  const fetcher = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === "/api/v1/tag-groups") {
        return Response.json([
          {
            id: "group-1",
            label: "Technology",
            description: "",
            position: 1,
            state: "active",
            version: 1,
          },
        ]);
      }
      if (url === "/api/v1/tags") {
        return Response.json([
          {
            id: "tag-network",
            label: "Network",
            group_id: "group-1",
            state: "active",
            synonyms: [],
            version: 1,
          },
        ]);
      }
      if (init?.method === "POST") {
        assetCreates += 1;
        if (assetCreates === 1) {
          return Response.json(
            {
              error: { code: "tag_archived", message: "Network was archived" },
            },
            { status: 422 },
          );
        }
        return Response.json(
          { ...activeResources[2], authority: "technician_confirmed" },
          { status: 201 },
        );
      }
      return Response.json(activeResources.slice(0, 1));
    },
  );
  vi.stubGlobal("fetch", fetcher);
  render(
    <ClientResourcesPage
      clientID="client-1"
      capabilities={new Set(["search.read", "asset.create"])}
    />,
  );

  await user.click(await screen.findByRole("button", { name: "Add resource" }));
  await user.selectOptions(screen.getByLabelText("Resource kind"), "asset");
  await user.type(screen.getByLabelText("Display ID"), "AST-2");
  await user.type(screen.getByLabelText("Name"), "Core router");
  await user.type(screen.getByLabelText("Asset type"), "network_device");
  const tagPicker = await screen.findByRole("combobox", {
    name: /Classification tags/,
  });
  await user.click(tagPicker);
  await user.click(screen.getByRole("option", { name: /Network/ }));
  await user.click(screen.getByRole("button", { name: "Create Asset" }));

  expect(
    await screen.findByText(
      "Classification changed. Confirm at least one current classification tag.",
    ),
  ).toBeVisible();
  expect(tagPicker).toHaveFocus();
  expect(screen.getByLabelText("Display ID")).toHaveValue("AST-2");
  expect(
    screen.getByRole("group", { name: /Classification tags selections/ }),
  ).toHaveTextContent("Network");

  await user.click(screen.getByRole("button", { name: "Create Asset" }));

  expect(await screen.findByText("Asset AST-2 created.")).toBeVisible();
  const mutation = fetcher.mock.calls
    .filter(([, init]) => init?.method === "POST")
    .at(-1);
  expect(JSON.parse(String(mutation?.[1]?.body))).toMatchObject({
    display_id: "AST-2",
    name: "Core router",
    asset_type: "network_device",
    authority: "technician_confirmed",
    tag_ids: ["tag-network"],
  });
});

it("direct-loads a selected asset outside the lifecycle catalog", async () => {
  const fetcher = vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url === "/api/v1/client-resources/off-page") {
      return Response.json({
        id: "off-page",
        kind: "asset",
        display_id: "AST-999",
        name: "Archive firewall",
        lifecycle_state: "inactive",
        version: 1,
      });
    }
    if (url.includes("/objects/asset/off-page/tags")) {
      return Response.json({
        target: { object_type: "asset", object_id: "off-page" },
        object_version: 1,
        direct: [],
        inherited: [],
        effective: [],
        classification_state: "unclassified",
      });
    }
    if (url === "/api/v1/tag-groups" || url === "/api/v1/tags") {
      return Response.json([]);
    }
    return Response.json([]);
  });
  vi.stubGlobal("fetch", fetcher);

  render(
    <ClientResourcesPage
      clientID="client-1"
      capabilities={new Set(["search.read"])}
      selectedAssetID="off-page"
    />,
  );

  expect(
    await screen.findByRole("heading", { name: "Archive firewall" }),
  ).toBeVisible();
  expect(fetcher).toHaveBeenCalledWith(
    "/api/v1/client-resources/off-page",
    expect.objectContaining({
      headers: expect.objectContaining({ "X-Rarity-Client-ID": "client-1" }),
    }),
  );
});

it("ignores stale catalog responses after the Client changes", async () => {
  let resolveFirst: ((response: Response) => void) | undefined;
  const first = new Promise<Response>((resolve) => {
    resolveFirst = resolve;
  });
  const fetcher = vi.fn(
    async (_input: RequestInfo | URL, init?: RequestInit) => {
      const client = (init?.headers as Record<string, string>)[
        "X-Rarity-Client-ID"
      ];
      if (client === "client-1") return first;
      return Response.json([
        {
          ...activeResources[0],
          id: "00000000-0000-4000-8000-000000000201",
          display_id: "LOC-2",
          name: "Contoso HQ",
        },
      ]);
    },
  );
  vi.stubGlobal("fetch", fetcher);
  const view = render(
    <ClientResourcesPage
      clientID="client-1"
      capabilities={new Set(["search.read"])}
    />,
  );
  view.rerender(
    <ClientResourcesPage
      clientID="client-2"
      capabilities={new Set(["search.read"])}
    />,
  );
  expect(await screen.findByText("Contoso HQ")).toBeVisible();
  resolveFirst?.(Response.json(activeResources));
  await waitFor(() => expect(screen.queryByText("Headquarters")).toBeNull());
});

it("removes prior Client rows, actions, dialog, and notices before a failed replacement load", async () => {
  const user = userEvent.setup();
  let resolveReplacement: ((response: Response) => void) | undefined;
  const replacement = new Promise<Response>((resolve) => {
    resolveReplacement = resolve;
  });
  const fetcher = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const client = (init?.headers as Record<string, string>)[
        "X-Rarity-Client-ID"
      ];
      if (client === "client-2") return replacement;
      if (init?.method === "POST") {
        return Response.json(
          { error: { code: "resource_in_use", message: "resource is in use" } },
          { status: 409 },
        );
      }
      if (String(input).includes("/contacts/")) {
        return Response.json({
          ...activeResources[1],
          display_name: "Ada Lovelace",
          email: "ada@example.test",
        });
      }
      return Response.json(activeResources);
    },
  );
  vi.stubGlobal("fetch", fetcher);
  const capabilities = new Set([
    "search.read",
    "contact.create",
    "contact.update",
    "location.lifecycle",
  ]);
  const view = render(
    <ClientResourcesPage clientID="client-1" capabilities={capabilities} />,
  );

  await user.click(
    await screen.findByRole("button", { name: "Deactivate LOC-1" }),
  );
  await user.type(screen.getByLabelText("Reason"), "Still referenced");
  await user.click(
    screen.getByRole("button", { name: "Confirm deactivation" }),
  );
  expect(
    await screen.findByRole("alert", {
      name: "Client resource change blocked",
    }),
  ).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  await user.click(screen.getByRole("button", { name: "Edit CONTACT-1" }));
  expect(
    await screen.findByRole("dialog", { name: "Edit Contact CONTACT-1" }),
  ).toBeVisible();

  view.rerender(
    <ClientResourcesPage clientID="client-2" capabilities={capabilities} />,
  );

  expect(screen.queryByText("CONTACT-1")).toBeNull();
  expect(screen.queryByRole("button", { name: "Edit CONTACT-1" })).toBeNull();
  expect(
    screen.queryByRole("dialog", { name: "Edit Contact CONTACT-1" }),
  ).toBeNull();
  expect(
    screen.queryByRole("alert", {
      name: "Client resource change blocked",
    }),
  ).toBeNull();

  resolveReplacement?.(new Response(null, { status: 503 }));
  expect(
    await screen.findByText("Client resources are unavailable"),
  ).toBeVisible();
  expect(screen.queryByText("CONTACT-1")).toBeNull();
  await user.click(screen.getByRole("button", { name: "Add resource" }));
  await user.selectOptions(screen.getByLabelText("Resource kind"), "contact");
  expect(
    within(screen.getByLabelText("Location")).queryByRole("option", {
      name: "Headquarters (LOC-1)",
    }),
  ).toBeNull();
});

it("keeps inactive-view creation Location choices scoped to the latest Client", async () => {
  const user = userEvent.setup();
  let resolveFirstLocations: ((response: Response) => void) | undefined;
  const firstLocations = new Promise<Response>((resolve) => {
    resolveFirstLocations = resolve;
  });
  let firstActiveRequests = 0;
  const secondLocation = {
    ...activeResources[0],
    id: "00000000-0000-4000-8000-000000000201",
    display_id: "LOC-2",
    name: "Contoso HQ",
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const client = (init?.headers as Record<string, string>)[
        "X-Rarity-Client-ID"
      ];
      if (url.includes("lifecycle=inactive")) return Response.json([]);
      if (client === "client-1") {
        firstActiveRequests += 1;
        if (firstActiveRequests === 1) return Response.json(activeResources);
        return firstLocations;
      }
      return Response.json([secondLocation]);
    }),
  );
  const capabilities = new Set([
    "search.read",
    "contact.create",
    "contact.lifecycle",
  ]);
  const view = render(
    <ClientResourcesPage clientID="client-1" capabilities={capabilities} />,
  );
  await screen.findByText("Headquarters");
  await user.selectOptions(screen.getByLabelText("Lifecycle"), "inactive");
  await screen.findByText("No matching client resources.");

  view.rerender(
    <ClientResourcesPage clientID="client-2" capabilities={capabilities} />,
  );
  await user.click(screen.getByRole("button", { name: "Add resource" }));
  await user.selectOptions(screen.getByLabelText("Resource kind"), "contact");
  const location = screen.getByLabelText("Location");
  expect(
    await within(location).findByRole("option", {
      name: "Contoso HQ (LOC-2)",
    }),
  ).toBeVisible();
  expect(
    within(location).queryByRole("option", { name: "Headquarters (LOC-1)" }),
  ).toBeNull();

  resolveFirstLocations?.(Response.json([activeResources[0]]));
  await waitFor(() =>
    expect(
      within(location).queryByRole("option", {
        name: "Headquarters (LOC-1)",
      }),
    ).toBeNull(),
  );
});

it("refreshes Contact and Asset Location choices after ordinary Location lifecycle mutations", async () => {
  document.cookie = "rarity_csrf=csrf-token";
  const user = userEvent.setup();
  let locations: Array<{
    id: string;
    kind: "location";
    display_id: string;
    name: string;
    lifecycle_state: "active" | "inactive";
    version: number;
  }> = [{ ...activeResources[0] }];
  const createdID = "00000000-0000-4000-8000-000000000202";
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === "/api/v1/locations" && init?.method === "POST") {
        const body = JSON.parse(String(init.body));
        const created = {
          id: createdID,
          kind: "location" as const,
          display_id: body.display_id,
          name: body.name,
          lifecycle_state: "active" as const,
          version: 1,
        };
        locations = [...locations, created];
        return Response.json(created, { status: 201 });
      }
      if (url.endsWith("/deactivate") && init?.method === "POST") {
        locations = locations.map((item) =>
          item.id === createdID
            ? { ...item, lifecycle_state: "inactive" as const, version: 3 }
            : item,
        );
        return Response.json(locations.find((item) => item.id === createdID));
      }
      if (url.endsWith("/reactivate") && init?.method === "POST") {
        locations = locations.map((item) =>
          item.id === createdID
            ? { ...item, lifecycle_state: "active" as const, version: 4 }
            : item,
        );
        return Response.json(locations.find((item) => item.id === createdID));
      }
      if (url.includes(`/locations/${createdID}`) && init?.method === "PATCH") {
        const body = JSON.parse(String(init.body));
        locations = locations.map((item) =>
          item.id === createdID
            ? { ...item, name: body.name, version: 2 }
            : item,
        );
        return Response.json(locations.find((item) => item.id === createdID));
      }
      if (url.includes(`/locations/${createdID}`)) {
        return Response.json(locations.find((item) => item.id === createdID));
      }
      if (url.includes("lifecycle=inactive")) {
        return Response.json(
          locations.filter((item) => item.lifecycle_state === "inactive"),
        );
      }
      return Response.json(
        locations.filter((item) => item.lifecycle_state === "active"),
      );
    }),
  );
  render(
    <ClientResourcesPage
      clientID="client-1"
      capabilities={
        new Set([
          "search.read",
          "location.create",
          "location.update",
          "location.lifecycle",
          "contact.create",
          "asset.create",
        ])
      }
    />,
  );

  await user.click(await screen.findByRole("button", { name: "Add resource" }));
  await user.type(screen.getByLabelText("Display ID"), "LOC-2");
  await user.type(screen.getByLabelText("Name"), "Branch office");
  await user.click(screen.getByRole("button", { name: "Create Location" }));
  await screen.findByText("Location LOC-2 created.");
  await user.click(screen.getByRole("button", { name: "Add resource" }));
  await user.selectOptions(screen.getByLabelText("Resource kind"), "contact");
  expect(
    await within(screen.getByLabelText("Location")).findByRole("option", {
      name: "Branch office (LOC-2)",
    }),
  ).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Cancel" }));

  await user.click(screen.getByRole("button", { name: "Edit LOC-2" }));
  await user.clear(await screen.findByLabelText("Name"));
  await user.type(screen.getByLabelText("Name"), "Renamed branch");
  await user.type(screen.getByLabelText("Change reason"), "Rename office");
  await user.click(screen.getByRole("button", { name: "Save changes" }));
  await screen.findByText("Location LOC-2 updated.");
  await user.click(screen.getByRole("button", { name: "Add resource" }));
  await user.selectOptions(screen.getByLabelText("Resource kind"), "asset");
  expect(
    await within(screen.getByLabelText("Location")).findByRole("option", {
      name: "Renamed branch (LOC-2)",
    }),
  ).toBeVisible();
  expect(
    within(screen.getByLabelText("Location")).queryByRole("option", {
      name: "Branch office (LOC-2)",
    }),
  ).toBeNull();
  await user.click(screen.getByRole("button", { name: "Cancel" }));

  await user.click(screen.getByRole("button", { name: "Deactivate LOC-2" }));
  await user.type(screen.getByLabelText("Reason"), "Office closed");
  await user.click(
    screen.getByRole("button", { name: "Confirm deactivation" }),
  );
  await screen.findByText("Location LOC-2 deactivated.");
  await user.click(screen.getByRole("button", { name: "Add resource" }));
  await user.selectOptions(screen.getByLabelText("Resource kind"), "contact");
  expect(
    within(screen.getByLabelText("Location")).queryByRole("option", {
      name: "Renamed branch (LOC-2)",
    }),
  ).toBeNull();
  await user.click(screen.getByRole("button", { name: "Cancel" }));

  await user.selectOptions(screen.getByLabelText("Lifecycle"), "inactive");
  await user.click(
    await screen.findByRole("button", { name: "Reactivate LOC-2" }),
  );
  await user.type(screen.getByLabelText("Reason"), "Office reopened");
  await user.click(
    screen.getByRole("button", { name: "Confirm reactivation" }),
  );
  await screen.findByText("Location LOC-2 reactivated.");
  await user.click(screen.getByRole("button", { name: "Add resource" }));
  await user.selectOptions(screen.getByLabelText("Resource kind"), "contact");
  expect(
    await within(screen.getByLabelText("Location")).findByRole("option", {
      name: "Renamed branch (LOC-2)",
    }),
  ).toBeVisible();
});

it("clears Location choices during AI refresh and ignores an older overlapping reload", async () => {
  const user = userEvent.setup();
  let resolveOlderReload: ((response: Response) => void) | undefined;
  const olderReload = new Promise<Response>((resolve) => {
    resolveOlderReload = resolve;
  });
  let activeResponse: () => Promise<Response> = async () =>
    Response.json([activeResources[0]]);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      if (String(input).includes("lifecycle=inactive"))
        return Response.json([]);
      return activeResponse();
    }),
  );
  const capabilities = new Set([
    "search.read",
    "contact.create",
    "contact.lifecycle",
  ]);
  const view = render(
    <ClientResourcesPage
      clientID="client-1"
      capabilities={capabilities}
      refreshToken={0}
    />,
  );
  await user.click(await screen.findByRole("button", { name: "Add resource" }));
  await user.selectOptions(screen.getByLabelText("Resource kind"), "contact");
  expect(
    await within(screen.getByLabelText("Location")).findByRole("option", {
      name: "Headquarters (LOC-1)",
    }),
  ).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  await user.selectOptions(screen.getByLabelText("Lifecycle"), "inactive");
  await screen.findByText("No matching client resources.");

  activeResponse = () => olderReload;
  view.rerender(
    <ClientResourcesPage
      clientID="client-1"
      capabilities={capabilities}
      refreshToken={1}
    />,
  );
  await user.click(screen.getByRole("button", { name: "Add resource" }));
  await user.selectOptions(screen.getByLabelText("Resource kind"), "contact");
  expect(
    within(screen.getByLabelText("Location")).queryByRole("option", {
      name: "Headquarters (LOC-1)",
    }),
  ).toBeNull();
  await user.click(screen.getByRole("button", { name: "Cancel" }));

  const refreshedLocation = {
    ...activeResources[0],
    id: "00000000-0000-4000-8000-000000000203",
    display_id: "LOC-3",
    name: "Reactivated branch",
  };
  activeResponse = async () => Response.json([refreshedLocation]);
  view.rerender(
    <ClientResourcesPage
      clientID="client-1"
      capabilities={capabilities}
      refreshToken={2}
    />,
  );
  await user.click(screen.getByRole("button", { name: "Add resource" }));
  await user.selectOptions(screen.getByLabelText("Resource kind"), "contact");
  const location = screen.getByLabelText("Location");
  expect(
    await within(location).findByRole("option", {
      name: "Reactivated branch (LOC-3)",
    }),
  ).toBeVisible();

  resolveOlderReload?.(Response.json([activeResources[0]]));
  await waitFor(() =>
    expect(
      within(location).queryByRole("option", {
        name: "Headquarters (LOC-1)",
      }),
    ).toBeNull(),
  );
});

it("keeps lifecycle filters and mutation controls hidden without authority", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(activeResources)),
  );
  render(
    <ClientResourcesPage
      clientID="client-1"
      capabilities={new Set(["search.read"])}
    />,
  );
  await screen.findByText("Headquarters");
  expect(screen.getByLabelText("Lifecycle")).toHaveTextContent("Active");
  expect(screen.getByLabelText("Lifecycle")).not.toHaveTextContent("Inactive");
  expect(screen.queryByRole("button", { name: /^Edit / })).toBeNull();
  expect(screen.queryByRole("button", { name: /^Deactivate / })).toBeNull();
  expect(screen.queryByRole("button", { name: "Add resource" })).toBeNull();
  expect(operationsStyles).toContain("min-width: 52rem");
  expect(operationsStyles).toContain("@media (max-width: 760px)");
});
