import axe from "axe-core";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

import { DirectorySettingsPage } from "./DirectorySettingsPage";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

it("creates a reasoned department and refreshes the directory", async () => {
  document.cookie = "rarity_csrf=csrf-token";
  const base = {
    clients: [{ id: "client-1", display_id: "ACME", name: "Acme" }],
    departments: [] as Array<{ id: string; key: string; name: string }>,
    teams: [],
    queues: [],
  };
  const fetcher = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input) === "/api/v1/admin/labor-roles" && !init?.method) {
        return Response.json([]);
      }
      if (init?.method === "POST") {
        base.departments = [
          { id: "department-1", key: "service", name: "Service" },
        ];
        return Response.json(base.departments[0], { status: 201 });
      }
      return Response.json(base);
    },
  );
  vi.stubGlobal("fetch", fetcher);
  const { container } = render(
    <DirectorySettingsPage
      capabilities={
        new Set(["organization.read", "organization.manage", "client.create"])
      }
    />,
  );
  await screen.findByRole("heading", { name: "Add department" });
  expect(
    screen.getByRole("heading", { name: "Labor roles and rates" }),
  ).toBeVisible();
  const keys = screen.getAllByLabelText("Stable key");
  const names = screen.getAllByLabelText("Name");
  const reasons = screen.getAllByLabelText("Reason");
  fireEvent.change(keys[0], { target: { value: "service" } });
  fireEvent.change(names[0], { target: { value: "Service" } });
  fireEvent.change(reasons[0], {
    target: { value: "Establish support ownership" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Add department" }));
  await waitFor(() =>
    expect(screen.getByText("Department created.")).toBeInTheDocument(),
  );
  expect(screen.getAllByText("Service").length).toBeGreaterThan(0);
  const mutation = fetcher.mock.calls.find(
    ([, init]) => init?.method === "POST",
  );
  expect(mutation?.[1]?.headers).toMatchObject({
    "X-Rarity-CSRF": "csrf-token",
  });
  expect(String(mutation?.[1]?.body)).toContain(
    '"reason":"Establish support ownership"',
  );
  fireEvent.change(screen.getByLabelText("Availability technician ID"), {
    target: { value: "technician-1" },
  });
  fireEvent.change(screen.getByLabelText("Available from"), {
    target: { value: "2026-08-03T09:00" },
  });
  fireEvent.change(screen.getByLabelText("Available through"), {
    target: { value: "2026-08-03T17:00" },
  });
  fireEvent.change(screen.getByLabelText("Available minutes"), {
    target: { value: "480" },
  });
  fireEvent.click(
    screen.getByRole("button", { name: "Add availability window" }),
  );
  await waitFor(() =>
    expect(
      screen.getByText("Availability window created."),
    ).toBeInTheDocument(),
  );
  const availabilityMutation = fetcher.mock.calls.find(([input, init]) => {
    return (
      String(input) === "/api/v1/admin/technicians/technician-1/availability" &&
      init?.method === "POST"
    );
  });
  expect(String(availabilityMutation?.[1]?.body)).toContain(
    '"available_minutes":480',
  );
  fireEvent.change(screen.getByLabelText("Rate technician ID"), {
    target: { value: "technician-1" },
  });
  fireEvent.change(screen.getByLabelText("Internal hourly cost"), {
    target: { value: "85" },
  });
  fireEvent.change(screen.getByLabelText("Rate currency"), {
    target: { value: "USD" },
  });
  fireEvent.change(screen.getByLabelText("Rate effective at"), {
    target: { value: "2026-08-01T00:00" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Add labor cost rate" }));
  await waitFor(() =>
    expect(screen.getByText("Labor cost rate created.")).toBeInTheDocument(),
  );
  const rateMutation = fetcher.mock.calls.find(([input, init]) => {
    return (
      String(input) ===
        "/api/v1/admin/technicians/technician-1/labor-cost-rates" &&
      init?.method === "POST"
    );
  });
  expect(String(rateMutation?.[1]?.body)).toContain(
    '"hourly_rate":{"minor":8500,"currency":"USD"}',
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

it("shows directory data without mutation forms for read-only operators", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      Response.json({
        clients: [{ id: "client-1", display_id: "ACME", name: "Acme" }],
        departments: [],
        teams: [],
        queues: [],
      }),
    ),
  );
  render(
    <DirectorySettingsPage capabilities={new Set(["organization.read"])} />,
  );
  expect(await screen.findByText("Acme")).toBeInTheDocument();
  expect(
    screen.queryByRole("heading", { name: "Add department" }),
  ).not.toBeInTheDocument();
});

it("shows deployed client envelope names in the directory overview", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      Response.json({
        clients: [
          {
            ID: "client-bravo",
            DisplayID: "BRAVO",
            Name: "Bravo Isolation Demo",
          },
        ],
        departments: [],
        teams: [],
        queues: [],
      }),
    ),
  );

  render(
    <DirectorySettingsPage capabilities={new Set(["organization.read"])} />,
  );

  expect(await screen.findByText("Bravo Isolation Demo")).toBeInTheDocument();
  expect(screen.getByText("BRAVO")).toBeInTheDocument();
});

it("searches active technicians and submits a complete team membership snapshot", async () => {
  document.cookie = "rarity_csrf=csrf-token";
  const directory = {
    clients: [],
    departments: [{ id: "department-1", key: "service", name: "Service" }],
    teams: [
      {
        id: "team-1",
        department_id: "department-1",
        key: "tier-one",
        name: "Tier One",
        version: 4,
        member_ids: ["tech-1"],
      },
    ],
    queues: [],
    technicians: [
      {
        id: "tech-1",
        display_name: "Alex Morgan",
        email: "alex@example.test",
      },
      {
        id: "tech-2",
        display_name: "Sam Rivera",
        email: "sam@example.test",
      },
    ],
  };
  const fetcher = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input) === "/api/v1/admin/labor-roles") {
        return Response.json([]);
      }
      if (String(input) === "/api/v1/directory") {
        return Response.json(directory);
      }
      if (
        String(input) === "/api/v1/admin/teams/team-1/members" &&
        init?.method === "PUT"
      ) {
        directory.teams[0] = {
          ...directory.teams[0],
          version: 5,
          member_ids: ["tech-1", "tech-2"],
        };
        return Response.json(directory.teams[0]);
      }
      return Response.json({}, { status: 404 });
    },
  );
  vi.stubGlobal("fetch", fetcher);
  render(
    <DirectorySettingsPage
      capabilities={new Set(["organization.read", "organization.manage"])}
    />,
  );

  await screen.findByRole("heading", { name: "Manage team members" });
  fireEvent.change(screen.getByLabelText("Search Tier One members"), {
    target: { value: "sam" },
  });
  fireEvent.click(
    screen.getByRole("button", { name: "Choose Tier One members" }),
  );
  expect(
    screen.queryByRole("option", { name: /Alex Morgan/ }),
  ).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("option", { name: /Sam Rivera/ }));
  fireEvent.change(
    screen.getByLabelText("Reason for Tier One membership change"),
    { target: { value: "Balance on-call coverage" } },
  );
  fireEvent.click(
    screen.getByRole("button", { name: "Save Tier One members" }),
  );
  await screen.findByText("Tier One membership updated.");

  const request = fetcher.mock.calls.find(
    ([input, init]) =>
      String(input) === "/api/v1/admin/teams/team-1/members" &&
      init?.method === "PUT",
  );
  expect(request?.[1]?.headers).toMatchObject({
    "Content-Type": "application/json",
    "X-Rarity-CSRF": "csrf-token",
  });
  expect(JSON.parse(String(request?.[1]?.body))).toEqual({
    technician_ids: ["tech-1", "tech-2"],
    expected_version: 4,
    reason: "Balance on-call coverage",
  });
});

it("shows the current server membership and requires an explicit merge after a conflict", async () => {
  document.cookie = "rarity_csrf=csrf-token";
  let directoryVersion = 4;
  let directoryMemberIDs = ["tech-1"];
  let putCalls = 0;
  const directory = () => ({
    clients: [],
    departments: [{ id: "department-1", key: "service", name: "Service" }],
    teams: [
      {
        id: "team-1",
        department_id: "department-1",
        key: "tier-one",
        name: "Tier One",
        version: directoryVersion,
        member_ids: directoryMemberIDs,
      },
    ],
    queues: [],
    technicians: [
      ...(directoryVersion === 4
        ? [
            {
              id: "tech-1",
              display_name: "Alex Morgan",
              email: "alex@example.test",
            },
          ]
        : []),
      { id: "tech-2", display_name: "Sam Rivera", email: "sam@example.test" },
      { id: "tech-3", display_name: "Jamie Chen", email: "jamie@example.test" },
    ],
  });
  const fetcher = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input) === "/api/v1/admin/labor-roles") {
        return Response.json([]);
      }
      if (String(input) === "/api/v1/directory") {
        return Response.json(directory());
      }
      if (
        String(input) === "/api/v1/admin/teams/team-1/members" &&
        init?.method === "PUT"
      ) {
        putCalls += 1;
        if (putCalls === 1) {
          directoryVersion = 5;
          directoryMemberIDs = ["tech-3"];
          return Response.json(
            { error: { code: "version_conflict" } },
            { status: 409 },
          );
        }
        directoryVersion = 6;
        directoryMemberIDs = ["tech-1", "tech-2", "tech-3"];
        return Response.json({
          ...directory().teams[0],
        });
      }
      return Response.json({}, { status: 404 });
    },
  );
  vi.stubGlobal("fetch", fetcher);
  render(
    <DirectorySettingsPage
      capabilities={new Set(["organization.read", "organization.manage"])}
    />,
  );

  await screen.findByRole("heading", { name: "Manage team members" });
  fireEvent.click(
    screen.getByRole("button", { name: "Choose Tier One members" }),
  );
  fireEvent.click(screen.getByRole("option", { name: /Sam Rivera/ }));
  const reason = screen.getByLabelText("Reason for Tier One membership change");
  fireEvent.change(reason, { target: { value: "Preserve this reason" } });
  fireEvent.click(
    screen.getByRole("button", { name: "Save Tier One members" }),
  );

  expect(
    await screen.findByRole("alert", { name: "Tier One membership changed" }),
  ).toHaveTextContent("current membership was reloaded");
  expect(reason).toHaveValue("Preserve this reason");
  expect(
    screen.getByRole("button", { name: "Remove Alex Morgan" }),
  ).toBeInTheDocument();
  expect(
    screen.getByRole("button", { name: "Remove Sam Rivera" }),
  ).toBeInTheDocument();
  expect(screen.getByText("Current server members")).toBeInTheDocument();
  expect(screen.getByText("Added on server: Jamie Chen")).toBeInTheDocument();
  expect(
    screen.getByText("Only in pending selection: Alex Morgan, Sam Rivera"),
  ).toBeInTheDocument();

  const save = screen.getByRole("button", { name: "Save Tier One members" });
  expect(save).toBeDisabled();
  fireEvent.click(save);
  expect(putCalls).toBe(1);

  fireEvent.click(
    screen.getByRole("button", {
      name: "Merge current server members into pending selection",
    }),
  );
  expect(putCalls).toBe(1);
  expect(
    screen.getByRole("button", { name: "Remove Jamie Chen" }),
  ).toBeInTheDocument();
  expect(reason).toHaveValue("Preserve this reason");

  fireEvent.click(
    screen.getByRole("button", { name: "Save Tier One members" }),
  );
  await screen.findByText("Tier One membership updated.");
  const requests = fetcher.mock.calls
    .filter(
      ([input, init]) =>
        String(input) === "/api/v1/admin/teams/team-1/members" &&
        init?.method === "PUT",
    )
    .map(([, init]) => JSON.parse(String(init?.body)));
  expect(requests).toEqual([
    {
      technician_ids: ["tech-1", "tech-2"],
      expected_version: 4,
      reason: "Preserve this reason",
    },
    {
      technician_ids: ["tech-1", "tech-2", "tech-3"],
      expected_version: 5,
      reason: "Preserve this reason",
    },
  ]);
});
