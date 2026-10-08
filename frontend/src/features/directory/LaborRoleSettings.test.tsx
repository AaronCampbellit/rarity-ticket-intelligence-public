import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

import { LaborRoleSettings } from "./LaborRoleSettings";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

it("lists effective labor versions and appends reasoned replacements", async () => {
  const role = {
    id: "role-1",
    msp_id: "msp-1",
    key: "service_desk",
    version: 2,
    current_version: {
      id: "role-version-2",
      labor_role_id: "role-1",
      msp_id: "msp-1",
      name: "Service Desk",
      internal_cost_minor: 3500,
      bill_rate_minor: 12500,
      currency: "USD",
      effective_from: "2026-08-01T00:00:00Z",
      enabled: true,
      created_at: "2026-08-01T00:00:00Z",
      created_by: "admin",
    },
  };
  let versionBody: Record<string, unknown> | undefined;
  const fetcher = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/versions") && init?.method === "POST") {
        versionBody = JSON.parse(String(init.body)) as Record<string, unknown>;
        return Response.json(
          {
            ...role,
            version: 3,
            current_version: {
              ...role.current_version,
              id: "role-version-3",
              internal_cost_minor: 4000,
              bill_rate_minor: 13000,
              enabled: false,
            },
          },
          { status: 201 },
        );
      }
      return Response.json([role]);
    },
  );
  vi.stubGlobal("fetch", fetcher);

  render(<LaborRoleSettings />);

  expect(await screen.findByText("Service Desk")).toBeVisible();
  expect(screen.getByText("$35.00 cost / $125.00 bill")).toBeVisible();
  expect(screen.getByText("Version 2 · Enabled")).toBeVisible();

  fireEvent.change(
    screen.getByRole("spinbutton", { name: "Internal hourly cost" }),
    {
      target: { value: "40" },
    },
  );
  fireEvent.change(
    screen.getByRole("spinbutton", { name: "Hourly bill rate" }),
    {
      target: { value: "130" },
    },
  );
  fireEvent.change(screen.getByLabelText("Effective from"), {
    target: { value: "2026-09-01T00:00" },
  });
  fireEvent.click(screen.getByRole("checkbox", { name: "Enabled" }));
  fireEvent.change(screen.getByRole("textbox", { name: "Version reason" }), {
    target: { value: "Retire legacy role" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Publish role version" }));

  expect(
    await screen.findByText("Labor role version published."),
  ).toBeVisible();
  expect(versionBody).toMatchObject({
    expected_version: 2,
    name: "Service Desk",
    internal_cost_minor: 4000,
    bill_rate_minor: 13000,
    currency: "USD",
    enabled: false,
    reason: "Retire legacy role",
  });
  expect(String(versionBody?.effective_from)).toContain("2026-09-01");
});

it("creates an effective-dated labor role", async () => {
  let createBody: Record<string, unknown> | undefined;
  const fetcher = vi.fn(
    async (_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === "POST") {
        createBody = JSON.parse(String(init.body)) as Record<string, unknown>;
        return Response.json(
          {
            id: "role-1",
            key: createBody.key,
            version: 1,
            current_version: {
              id: "version-1",
              name: createBody.name,
              internal_cost_minor: createBody.internal_cost_minor,
              bill_rate_minor: createBody.bill_rate_minor,
              currency: createBody.currency,
              effective_from: createBody.effective_from,
              enabled: true,
            },
          },
          { status: 201 },
        );
      }
      return Response.json([]);
    },
  );
  vi.stubGlobal("fetch", fetcher);

  render(<LaborRoleSettings />);

  await screen.findByText("No labor roles configured.");
  fireEvent.change(
    screen.getByRole("textbox", { name: "New labor role key" }),
    {
      target: { value: "service_desk" },
    },
  );
  fireEvent.change(
    screen.getByRole("textbox", { name: "New labor role name" }),
    {
      target: { value: "Service Desk" },
    },
  );
  fireEvent.change(
    screen.getByRole("spinbutton", { name: "New internal hourly cost" }),
    { target: { value: "35" } },
  );
  fireEvent.change(
    screen.getByRole("spinbutton", { name: "New hourly bill rate" }),
    { target: { value: "125" } },
  );
  fireEvent.change(screen.getByLabelText("New role effective from"), {
    target: { value: "2026-08-01T00:00" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Create labor role" }));

  await waitFor(() =>
    expect(screen.getByText("Labor role created.")).toBeVisible(),
  );
  expect(createBody).toMatchObject({
    key: "service_desk",
    name: "Service Desk",
    internal_cost_minor: 3500,
    bill_rate_minor: 12500,
    currency: "USD",
  });
});
