import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import axe from "axe-core";
import { afterEach, expect, it, vi } from "vitest";

import { ServiceDeskSettingsPage } from "./ServiceDeskSettingsPage";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

it("publishes a versioned workflow through the active client scope", async () => {
  document.cookie = "rarity_csrf=csrf-token";
  const workflow = {
    id: "workflow-1",
    key: "default",
    name: "Default workflow",
    version: 2,
    priority: 0,
    stable_order: 0,
    fallback: true,
    enabled: true,
    conditions: {},
    definition: {
      states: [
        { key: "new", sla_behavior: "active" },
        { key: "closed", sla_behavior: "resolved" },
      ],
      transitions: [{ from: "new", to: "closed" }],
    },
  };
  const fetcher = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (init?.method === "POST") {
        return Response.json({ ...workflow, version: 3 }, { status: 201 });
      }
      if (
        url.includes("business-calendars") ||
        url.includes("sla-policies") ||
        url.includes("notification-policies")
      ) {
        return Response.json([]);
      }
      return Response.json([workflow]);
    },
  );
  vi.stubGlobal("fetch", fetcher);
  render(<ServiceDeskSettingsPage clientID="client-1" />);

  expect(
    await screen.findByRole("heading", { name: "Default workflow" }),
  ).toBeInTheDocument();
  fireEvent.click(
    screen.getByRole("button", { name: "Publish replacement version" }),
  );
  await waitFor(() =>
    expect(
      screen.getByText("Workflow version 3 published."),
    ).toBeInTheDocument(),
  );

  const mutation = fetcher.mock.calls.find(
    ([, init]) => init?.method === "POST",
  );
  expect(mutation?.[0]).toBe("/api/v1/workflows/workflow-1/versions");
  expect(mutation?.[1]?.headers).toMatchObject({
    "X-Rarity-Client-ID": "client-1",
    "X-Rarity-CSRF": "csrf-token",
  });
  expect(String(mutation?.[1]?.body)).toContain('"expected_version":2');
});

it("publishes a versioned SLA policy bound to an exact calendar", async () => {
  document.cookie = "rarity_csrf=csrf-token";
  const calendar = {
    id: "calendar-1",
    key: "business-hours",
    name: "Business hours",
    version: 2,
    definition: {
      timezone: "America/Chicago",
      weekly: { monday: [{ start_minute: 480, end_minute: 1020 }] },
      holidays: [],
    },
  };
  const policy = {
    id: "sla-1",
    key: "default-sla",
    name: "Default SLA",
    version: 4,
    calendar,
    conditions: {},
    response_target_seconds: 3600,
    resolution_target_seconds: 28800,
    warning_percent: 75,
    pause_states: ["pending"],
    enabled: true,
    priority: 0,
    stable_order: 0,
    fallback: true,
  };
  const fetcher = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (init?.method === "POST") {
        return Response.json({ ...policy, version: 5 }, { status: 201 });
      }
      if (url.includes("business-calendars")) return Response.json([calendar]);
      if (url.includes("sla-policies")) return Response.json([policy]);
      return Response.json([]);
    },
  );
  vi.stubGlobal("fetch", fetcher);
  render(<ServiceDeskSettingsPage clientID="client-1" />);

  fireEvent.click(
    await screen.findByRole("button", { name: "Calendars and SLA" }),
  );
  expect(
    screen.getByRole("heading", { name: "Default SLA" }),
  ).toBeInTheDocument();
  fireEvent.click(
    screen.getByRole("button", { name: "Publish SLA replacement" }),
  );

  await waitFor(() =>
    expect(
      screen.getByText("SLA policy version 5 published."),
    ).toBeInTheDocument(),
  );
  const mutation = fetcher.mock.calls.find(
    ([url, init]) =>
      String(url).includes("/sla-policies/") && init?.method === "POST",
  );
  expect(String(mutation?.[1]?.body)).toContain('"calendar_id":"calendar-1"');
  expect(String(mutation?.[1]?.body)).toContain('"expected_version":4');
  expect(mutation?.[1]?.headers).not.toHaveProperty("X-Rarity-Client-ID");
});

it("publishes the default business calendar at MSP scope for the required global SLA fallback", async () => {
  document.cookie = "rarity_csrf=csrf-token";
  const fetcher = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === "/api/v1/business-calendars" && init?.method === "POST") {
        return Response.json(
          {
            id: "calendar-global",
            key: "support-hours",
            name: "Support hours",
            version: 1,
            definition: {
              timezone: "America/Chicago",
              weekly: {},
              holidays: [],
            },
          },
          { status: 201 },
        );
      }
      return Response.json([]);
    },
  );
  vi.stubGlobal("fetch", fetcher);
  render(
    <ServiceDeskSettingsPage
      clientID="client-1"
      capabilities={new Set(["sla.manage"])}
    />,
  );

  const calendarWorkspace = await screen.findByRole("region", {
    name: "Business calendars",
  });
  fireEvent.change(
    within(calendarWorkspace).getByRole("textbox", { name: "Key" }),
    { target: { value: "support-hours" } },
  );
  fireEvent.change(
    within(calendarWorkspace).getByRole("textbox", { name: "Name" }),
    { target: { value: "Support hours" } },
  );
  fireEvent.click(
    within(calendarWorkspace).getByRole("button", {
      name: "Publish calendar",
    }),
  );

  await waitFor(() =>
    expect(
      screen.getByText("Business calendar version 1 published."),
    ).toBeInTheDocument(),
  );
  const mutation = fetcher.mock.calls.find(
    ([url, init]) =>
      String(url) === "/api/v1/business-calendars" && init?.method === "POST",
  );
  expect(mutation?.[1]?.headers).not.toHaveProperty("X-Rarity-Client-ID");
  expect(
    within(calendarWorkspace).getByRole("combobox", {
      name: "Availability",
    }),
  ).toHaveValue("global");
});

it("keeps a client-specific non-fallback SLA policy in the active client scope", async () => {
  document.cookie = "rarity_csrf=csrf-token";
  const calendar = {
    id: "calendar-1",
    key: "business-hours",
    name: "Business hours",
    version: 1,
    definition: {
      timezone: "America/Chicago",
      weekly: { monday: [{ start_minute: 480, end_minute: 1020 }] },
      holidays: [],
    },
  };
  const fetcher = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (init?.method === "POST") {
        return Response.json(
          {
            id: "sla-client",
            client_id: "client-1",
            version: 1,
            calendar,
            fallback: false,
          },
          { status: 201 },
        );
      }
      if (url.includes("business-calendars")) return Response.json([calendar]);
      return Response.json([]);
    },
  );
  vi.stubGlobal("fetch", fetcher);
  render(<ServiceDeskSettingsPage clientID="client-1" />);

  fireEvent.click(
    await screen.findByRole("button", { name: "Calendars and SLA" }),
  );
  const slaWorkspace = screen.getByRole("region", { name: "SLA policies" });
  fireEvent.click(
    within(slaWorkspace).getByRole("checkbox", { name: /fallback/i }),
  );
  fireEvent.change(
    within(slaWorkspace).getByRole("combobox", { name: "Policy scope" }),
    { target: { value: "client" } },
  );
  fireEvent.change(within(slaWorkspace).getByRole("textbox", { name: "Key" }), {
    target: { value: "client-sla" },
  });
  fireEvent.change(
    within(slaWorkspace).getByRole("textbox", { name: "Name" }),
    {
      target: { value: "Client SLA" },
    },
  );
  fireEvent.click(
    within(slaWorkspace).getByRole("button", {
      name: "Publish SLA policy",
    }),
  );

  await waitFor(() =>
    expect(
      screen.getByText("SLA policy version 1 published."),
    ).toBeInTheDocument(),
  );
  const mutation = fetcher.mock.calls.find(
    ([url, init]) =>
      String(url).includes("/sla-policies") && init?.method === "POST",
  );
  expect(mutation?.[1]?.headers).toMatchObject({
    "X-Rarity-Client-ID": "client-1",
    "X-Rarity-CSRF": "csrf-token",
  });
  expect(
    within(slaWorkspace).getByRole("combobox", { name: "Policy scope" }),
  ).toHaveValue("client");
});

it("preserves a stored global SLA policy scope independently of fallback", async () => {
  document.cookie = "rarity_csrf=csrf-token";
  const globalCalendar = {
    id: "calendar-global",
    key: "global-hours",
    name: "Global hours",
    version: 1,
    definition: {
      timezone: "America/Chicago",
      weekly: { monday: [{ start_minute: 480, end_minute: 1020 }] },
      holidays: [],
    },
  };
  const clientCalendar = {
    ...globalCalendar,
    id: "calendar-client",
    client_id: "client-1",
    key: "client-hours",
    name: "Client hours",
  };
  const policy = {
    id: "sla-global",
    client_id: "",
    key: "global-critical",
    name: "Global critical",
    version: 2,
    calendar: globalCalendar,
    conditions: { priority: "critical" },
    response_target_seconds: 1800,
    resolution_target_seconds: 14400,
    warning_percent: 75,
    pause_states: ["pending"],
    enabled: true,
    priority: 100,
    stable_order: 1,
    fallback: false,
  };
  const fetcher = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (init?.method === "POST") {
        return Response.json({ ...policy, version: 3 }, { status: 201 });
      }
      if (url.includes("business-calendars")) {
        return Response.json([globalCalendar, clientCalendar]);
      }
      if (url.includes("sla-policies")) return Response.json([policy]);
      return Response.json([]);
    },
  );
  vi.stubGlobal("fetch", fetcher);
  render(
    <ServiceDeskSettingsPage
      clientID="client-1"
      capabilities={new Set(["sla.manage"])}
    />,
  );

  const slaWorkspace = await screen.findByRole("region", {
    name: "SLA policies",
  });
  expect(
    within(slaWorkspace).getByRole("combobox", { name: "Policy scope" }),
  ).toHaveValue("global");
  const calendarOptions = within(slaWorkspace).getAllByRole("option");
  expect(calendarOptions.map((option) => option.textContent)).toContain(
    "Global hours v1 · MSP-wide",
  );
  expect(calendarOptions.map((option) => option.textContent)).not.toContain(
    "Client hours v1 · Active client",
  );
  fireEvent.click(
    within(slaWorkspace).getByRole("button", {
      name: "Publish SLA replacement",
    }),
  );

  await screen.findByText("SLA policy version 3 published.");
  const mutation = fetcher.mock.calls.find(
    ([url, init]) =>
      String(url).includes("/sla-policies/") && init?.method === "POST",
  );
  expect(mutation?.[1]?.headers).not.toHaveProperty("X-Rarity-Client-ID");
});

it("keeps MSP-global setup available without an active client", async () => {
  const fetcher = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === "/api/v1/directory") {
        return Response.json({
          clients: [],
          departments: [],
          teams: [],
          queues: [{ id: "global", key: "triage", name: "Global triage" }],
        });
      }
      if (url === "/api/v1/routing-rules/versions") {
        return new Response(null, { status: 404 });
      }
      return Response.json([]);
    },
  );
  vi.stubGlobal("fetch", fetcher);
  render(
    <ServiceDeskSettingsPage
      clientID=""
      capabilities={
        new Set([
          "workflow.publish",
          "routing.manage",
          "sla.manage",
          "notification.manage",
        ])
      }
    />,
  );

  expect(
    await screen.findByText("Select a client to manage workflows."),
  ).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Routing" }));
  expect(
    screen.getByRole("button", { name: "Publish routing rules" }),
  ).toBeEnabled();
  fireEvent.click(screen.getByRole("button", { name: "Calendars and SLA" }));
  expect(
    screen.getByRole("heading", { name: "Create business calendar" }),
  ).toBeInTheDocument();
  expect(
    fetcher.mock.calls.some(
      ([, init]) =>
        init?.headers &&
        "X-Rarity-Client-ID" in (init.headers as Record<string, string>),
    ),
  ).toBe(false);
});

it("reports an unavailable routing directory instead of treating it as zero queues", async () => {
  const fetcher = vi.fn(async (input: RequestInfo | URL) =>
    String(input) === "/api/v1/directory"
      ? new Response(null, { status: 403 })
      : new Response(null, { status: 404 }),
  );
  vi.stubGlobal("fetch", fetcher);
  render(
    <ServiceDeskSettingsPage
      clientID=""
      capabilities={new Set(["routing.manage"])}
    />,
  );

  expect(
    await screen.findByText("Routing destinations could not be loaded"),
  ).toBeInTheDocument();
  expect(
    screen.queryByText(/Create an MSP-wide queue/),
  ).not.toBeInTheDocument();
});

it("publishes and replaces the MSP routing rule set from plain-English controls", async () => {
  document.cookie = "rarity_csrf=csrf-token";
  const directory = {
    clients: [{ id: "client-1", display_id: "NW", name: "Northwind Legal" }],
    departments: [],
    teams: [],
    queues: [
      { id: "queue-global", key: "triage", name: "Global triage" },
      {
        id: "queue-client",
        client_id: "client-1",
        key: "northwind",
        name: "Northwind queue",
      },
    ],
  };
  const current = {
    id: "routing-set",
    version: 2,
    rules: [
      {
        id: "rule-critical",
        position: 1,
        client_id: "client-1",
        record_type: "incident",
        priority: "critical",
        queue_id: "queue-client",
      },
      {
        id: "rule-fallback",
        position: 2,
        queue_id: "queue-global",
      },
    ],
  };
  const fetcher = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === "/api/v1/directory") return Response.json(directory);
      if (url === "/api/v1/routing-rules/versions" && init?.method === "GET") {
        return Response.json(current);
      }
      if (url === "/api/v1/routing-rules/versions" && init?.method === "POST") {
        return Response.json({ ...current, version: 3 }, { status: 201 });
      }
      return Response.json([]);
    },
  );
  vi.stubGlobal("fetch", fetcher);
  render(<ServiceDeskSettingsPage clientID="client-1" />);

  fireEvent.click(await screen.findByRole("button", { name: "Routing" }));
  expect(
    screen.getByText(
      "Rules are evaluated from top to bottom. The final destination catches work that does not match an earlier rule.",
    ),
  ).toBeInTheDocument();
  expect(
    screen.getByRole("combobox", { name: "Fallback destination" }),
  ).toHaveValue("queue-global");
  expect(screen.getByRole("combobox", { name: "Rule 1 client" })).toHaveValue(
    "client-1",
  );
  expect(screen.getByRole("button", { name: "Move rule 1 up" })).toBeDisabled();
  expect(
    screen.getByRole("button", { name: "Move rule 1 down" }),
  ).toBeInTheDocument();
  fireEvent.click(
    screen.getByRole("button", { name: "Publish routing replacement" }),
  );

  await waitFor(() =>
    expect(
      screen.getByText("Routing rule set version 3 published."),
    ).toBeInTheDocument(),
  );
  const mutation = fetcher.mock.calls.find(
    ([url, init]) =>
      String(url) === "/api/v1/routing-rules/versions" &&
      init?.method === "POST",
  );
  expect(mutation?.[1]?.headers).not.toHaveProperty("X-Rarity-Client-ID");
  expect(JSON.parse(String(mutation?.[1]?.body))).toEqual({
    expected_version: 2,
    rules: [
      {
        position: 1,
        client_id: "client-1",
        record_type: "incident",
        priority: "critical",
        queue_id: "queue-client",
      },
      { position: 2, queue_id: "queue-global" },
    ],
  });
});

it("publishes a versioned notification policy with exact destinations", async () => {
  document.cookie = "rarity_csrf=csrf-token";
  const policy = {
    id: "notification-1",
    key: "sla-warning",
    name: "SLA warning",
    event_type: "sla.warning",
    version: 3,
    conditions: { priority: "critical" },
    destinations: [
      {
        channel: "teams",
        recipient_ref: "teams-connection-1",
        content_classification: "restricted",
      },
    ],
    quiet_period_seconds: 600,
    critical_bypass: true,
    enabled: true,
    priority: 100,
    stable_order: 10,
  };
  const fetcher = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (init?.method === "POST") {
        return Response.json({ ...policy, version: 4 }, { status: 201 });
      }
      if (url.includes("notification-policies")) {
        return Response.json([policy]);
      }
      return Response.json([]);
    },
  );
  vi.stubGlobal("fetch", fetcher);
  render(<ServiceDeskSettingsPage clientID="client-1" />);

  fireEvent.click(await screen.findByRole("button", { name: "Notifications" }));
  expect(
    screen.getByRole("heading", { name: "SLA warning" }),
  ).toBeInTheDocument();
  fireEvent.click(
    screen.getByRole("button", {
      name: "Publish notification replacement",
    }),
  );

  await waitFor(() =>
    expect(
      screen.getByText("Notification policy version 4 published."),
    ).toBeInTheDocument(),
  );
  const mutation = fetcher.mock.calls.find(
    ([url, init]) =>
      String(url).includes("/notification-policies/") &&
      init?.method === "POST",
  );
  const body = String(mutation?.[1]?.body);
  expect(body).toContain('"expected_version":3');
  expect(body).toContain('"channel":"teams"');
  expect(body).toContain('"quiet_period_seconds":600');
  expect(mutation?.[1]?.headers).toMatchObject({
    "X-Rarity-Client-ID": "client-1",
    "X-Rarity-CSRF": "csrf-token",
  });
});

it("honors panel permissions and has no detectable WCAG A or AA violations", async () => {
  const fetcher = vi.fn(async () => Response.json([]));
  vi.stubGlobal("fetch", fetcher);
  const { container } = render(
    <ServiceDeskSettingsPage
      clientID="client-1"
      capabilities={new Set(["notification.manage"])}
    />,
  );

  expect(
    await screen.findByRole("heading", { name: "Create notification policy" }),
  ).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Workflows" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Calendars and SLA" }),
  ).not.toBeInTheDocument();
  expect(fetcher).toHaveBeenCalledTimes(2);
  expect(fetcher).toHaveBeenCalledWith(
    "/api/v1/notification-policies",
    expect.objectContaining({
      headers: { "X-Rarity-Client-ID": "client-1" },
    }),
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

it("offers a Mentions preset with only recipient email and Teams channels", async () => {
  const fetcher = vi.fn(async (input: RequestInfo | URL) =>
    String(input).includes("notification-preferences")
      ? Response.json({
          technician_id: "tech",
          event_type: "mention.occurred",
          email_enabled: false,
          teams_enabled: false,
          email_available: false,
          teams_available: false,
          time_zone: "UTC",
          version: 0,
        })
      : Response.json([]),
  );
  vi.stubGlobal("fetch", fetcher);
  render(
    <ServiceDeskSettingsPage
      clientID="client-1"
      capabilities={new Set(["notification.manage"])}
    />,
  );
  fireEvent.click(
    await screen.findByRole("button", { name: "Use Mentions preset" }),
  );
  expect(screen.getByLabelText("Event type")).toHaveValue("mention.occurred");
  const channel = screen.getByLabelText("Channel");
  expect(channel).toHaveTextContent("Email");
  expect(channel).toHaveTextContent("Microsoft Teams");
  expect(channel).not.toHaveTextContent("In-app");
  expect(channel).not.toHaveTextContent("Webhook");
});

it("treats null fresh-install collections as empty lists", async () => {
  const fetcher = vi.fn(async () => Response.json(null));
  vi.stubGlobal("fetch", fetcher);

  render(<ServiceDeskSettingsPage clientID="client-1" />);

  expect(
    await screen.findByRole("heading", { name: "Create workflow" }),
  ).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Calendars and SLA" }));
  expect(
    screen.getByRole("heading", { name: "Create business calendar" }),
  ).toBeInTheDocument();
});
