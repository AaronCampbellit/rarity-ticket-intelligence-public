import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { RoleManagementPage } from "./RoleManagementPage";

describe("RoleManagementPage", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("loads roles and submits a reasoned versioned capability replacement", async () => {
    document.cookie = "rarity_csrf=csrf-token";
    const fetcher = vi.fn(
      async (_input: RequestInfo | URL, init?: RequestInit) => {
        if (init?.method === "PUT") {
          return Response.json({
            id: "role-1",
            key: "technician",
            name: "Technician",
            system_role: true,
            capabilities: ["work_record.read", "work_record.update"],
            version: 4,
          });
        }
        return Response.json([
          {
            id: "role-1",
            key: "technician",
            name: "Technician",
            system_role: true,
            capabilities: ["work_record.read"],
            version: 3,
          },
        ]);
      },
    );
    vi.stubGlobal("fetch", fetcher);
    render(<RoleManagementPage />);

    expect(
      await screen.findByRole("heading", { name: "Technician" }),
    ).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", { name: "Choose Capabilities" }),
    );
    fireEvent.click(screen.getByRole("option", { name: /Work Record Update/ }));
    fireEvent.change(screen.getByLabelText("Reason for change"), {
      target: { value: "Align service desk access" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save capabilities" }));

    await waitFor(() =>
      expect(
        screen.getByText("Role capabilities updated and audited."),
      ).toBeInTheDocument(),
    );
    const [, request] = fetcher.mock.calls[1];
    expect(request?.headers).toEqual(
      expect.objectContaining({ "X-Rarity-CSRF": "csrf-token" }),
    );
    expect(String(request?.body)).toContain('"expected_version":3');
    expect(String(request?.body)).toContain(
      '"reason":"Align service desk access"',
    );
  });

  it("creates a custom role and assigns the scoped time reviewer role", async () => {
    const mutations: Array<{
      url: string;
      body: Record<string, unknown>;
    }> = [];
    const roles = [
      {
        id: "review-role",
        key: "time_reviewer",
        name: "Time reviewer",
        system_role: true,
        capabilities: [
          "time_entry.read_scoped",
          "time_entry.amend",
          "time_entry.approve",
          "timesheet.review",
        ],
        version: 1,
      },
    ];
    const fetcher = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (init?.method === "POST") {
          const body = JSON.parse(String(init.body)) as Record<string, unknown>;
          mutations.push({ url, body });
          if (url.endsWith("/role-assignments")) {
            return Response.json(
              { id: "assignment-1", ...body },
              { status: 201 },
            );
          }
          const created = {
            id: "service-lead",
            key: body.key,
            name: body.name,
            system_role: false,
            capabilities: body.capabilities,
            version: 1,
          };
          roles.push(created as (typeof roles)[number]);
          return Response.json(created, { status: 201 });
        }
        return Response.json(roles);
      },
    );
    vi.stubGlobal("fetch", fetcher);
    render(<RoleManagementPage />);

    await screen.findByRole("heading", { name: "Time reviewer" });
    fireEvent.change(screen.getByRole("textbox", { name: "New role key" }), {
      target: { value: "service_lead" },
    });
    fireEvent.change(screen.getByRole("textbox", { name: "New role name" }), {
      target: { value: "Service lead" },
    });
    fireEvent.change(screen.getByRole("textbox", { name: "Capability 1" }), {
      target: { value: "timesheet.review" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add capability" }));
    fireEvent.change(screen.getByRole("textbox", { name: "Capability 2" }), {
      target: { value: "time_entry.approve" },
    });
    fireEvent.change(screen.getByRole("textbox", { name: "New role reason" }), {
      target: { value: "Delegate service review" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create role" }));

    expect(await screen.findByText("Role created and audited.")).toBeVisible();

    fireEvent.change(
      screen.getByRole("textbox", { name: "Assignment technician ID" }),
      { target: { value: "technician-2" } },
    );
    fireEvent.change(
      screen.getByRole("combobox", { name: "Assignment role" }),
      { target: { value: "time_reviewer" } },
    );
    fireEvent.change(
      screen.getByRole("textbox", { name: "Assignment client ID" }),
      { target: { value: "client-1" } },
    );
    fireEvent.change(
      screen.getByRole("textbox", { name: "Assignment reason" }),
      { target: { value: "Review client time" } },
    );
    fireEvent.click(screen.getByRole("button", { name: "Assign role" }));

    expect(
      await screen.findByText("Role assignment created and audited."),
    ).toBeVisible();
    expect(mutations).toEqual([
      {
        url: "/api/v1/admin/roles",
        body: {
          key: "service_lead",
          name: "Service lead",
          capabilities: ["timesheet.review", "time_entry.approve"],
          reason: "Delegate service review",
        },
      },
      {
        url: "/api/v1/admin/role-assignments",
        body: {
          principal_id: "technician-2",
          role_key: "time_reviewer",
          client_id: "client-1",
          reason: "Review client time",
        },
      },
    ]);
  });
});
