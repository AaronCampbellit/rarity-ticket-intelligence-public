import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { WorkspaceProvider, useWorkspace } from "../../design-system";
import { projectModel, ProjectWorklist } from "./ProjectWorklist";
import type { ProjectWorkspaceResponse } from "./api";
import { __resetClientClassificationCatalogForTests } from "../classification/useClientCatalog";

describe("ProjectWorklist", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    localStorage.clear();
    __resetClientClassificationCatalogForTests();
  });

  it("switches the portfolio to Kanban and opens project records", async () => {
    const openRecord = vi.fn();
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        if (String(input) === "/api/v1/objects/project/project-1/tags") {
          return Response.json({
            target: { object_type: "project", object_id: "project-1" },
            object_version: 1,
            direct: [],
            inherited: [],
            effective: [],
            classification_state: "unclassified",
          });
        }
        if (String(input) === "/api/v1/tag-groups") {
          return Response.json([
            {
              id: "technology",
              label: "Technology",
              description: "",
              state: "active",
              version: 1,
            },
          ]);
        }
        if (String(input) === "/api/v1/tags") {
          return Response.json([
            {
              id: "tag-network",
              label: "Network",
              group_id: "technology",
              state: "active",
              synonyms: [],
              version: 1,
            },
          ]);
        }
        if (String(input).includes("/api/v1/projects?")) {
          return Response.json([
            {
              id: "project-1",
              display_id: "PRJ-1042",
              name: "Northwind rollout",
              lifecycle_state: "active",
              version: 1,
            },
          ]);
        }
        return Response.json({
          id: "project-1",
          display_id: "PRJ-1042",
          name: "Northwind rollout",
          client_name: "Northwind",
          lifecycle_state: "active",
          original_proposal_version: 1,
          phases: [],
          project_tasks: [],
          resource_plans: [],
          cost_actuals: [],
          capacity: [],
          change_orders: [],
          financials_visible: false,
          version: 1,
        });
      }),
    );

    render(
      <WorkspaceProvider
        principalID="principal-1"
        allowedRouteIDs={new Set(["project"])}
        allowedClientIDs={new Set(["client-1"])}
        onOpenRecord={openRecord}
      >
        <ProjectWorklist clientID="client-1" principalID="principal-1" />
        <ProjectPreviewProbe />
      </WorkspaceProvider>,
    );

    const project = await screen.findByRole("button", {
      name: "PRJ-1042 Northwind rollout",
    });
    expect(
      await screen.findByText("Unclassified — needs review"),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "List view" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    fireEvent.click(screen.getByRole("button", { name: "Kanban view" }));
    expect(localStorage.getItem("rti:view:principal-1:project")).toBe(
      '"kanban"',
    );
    const card = screen.getByRole("button", {
      name: "Open PRJ-1042 Northwind rollout",
    });
    fireEvent.click(card);
    expect(screen.getByTestId("project-preview")).toHaveTextContent("PRJ-1042");
    fireEvent.doubleClick(card);
    expect(openRecord).toHaveBeenCalledWith(
      expect.objectContaining({
        id: "project:project-1",
        entityType: "project",
      }),
    );
    expect(project).not.toBeInTheDocument();
  });

  it("renders a selected task with its parent project and time entry tools", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        if (String(input) === "/api/v1/tag-groups") {
          return Response.json([
            {
              id: "technology",
              label: "Technology",
              description: "",
              state: "active",
              version: 1,
            },
          ]);
        }
        if (String(input) === "/api/v1/tags") {
          return Response.json([
            {
              id: "tag-network",
              label: "Network",
              group_id: "technology",
              state: "active",
              synonyms: [],
              version: 1,
            },
          ]);
        }
        if (
          String(input).endsWith("/time-entries") &&
          init?.method === "POST"
        ) {
          return Response.json(
            { error: { code: "tag_archived" } },
            { status: 422 },
          );
        }
        if (String(input).includes("/api/v1/projects?")) {
          return Response.json([
            {
              id: "project-1",
              display_id: "PRJ-1042",
              name: "Northwind rollout",
              lifecycle_state: "active",
              version: 1,
            },
          ]);
        }
        return Response.json({
          id: "project-1",
          display_id: "PRJ-1042",
          name: "Northwind rollout",
          client_name: "Northwind",
          lifecycle_state: "active",
          original_proposal_version: 1,
          phases: [
            {
              id: "phase-1",
              position: 1,
              name: "Kickoff",
              state: "active",
              participating_teams: [],
              planned_start: "2026-08-10",
              planned_end: "2026-08-12",
              planned_minutes: 120,
              actual_minutes: 15,
              budget: { minor: 0, currency: "USD" },
              deliverables: [],
              completion_criteria: [],
              tasks: [
                {
                  id: "task-1",
                  title: "Schedule kickoff",
                  status: "open",
                  owner_name: "Alex Morgan",
                  subtasks: 0,
                  estimate_minutes: 60,
                  actual_minutes: 15,
                  version: 1,
                },
              ],
              version: 1,
            },
          ],
          project_tasks: [],
          resource_plans: [],
          cost_actuals: [],
          capacity: [],
          change_orders: [],
          financials_visible: false,
          version: 1,
        });
      }),
    );

    render(
      <ProjectWorklist
        clientID="client-1"
        capabilities={new Set(["time_entry.create"])}
        selectedRecord={{
          id: "task:task-1",
          routeID: "project",
          recordID: "task-1",
          parentRecordID: "project-1",
          clientID: "client-1",
          label: "Schedule kickoff",
          entityType: "task",
          openedAt: 1,
        }}
      />,
    );

    expect(
      await screen.findByRole("heading", { name: "Schedule kickoff" }),
    ).toBeVisible();
    expect(
      await screen.findByText("PRJ-1042 · Northwind rollout"),
    ).toBeVisible();
    expect(screen.getByText("Alex Morgan")).toBeVisible();
    expect(screen.getByText("2026-08-10 — 2026-08-12")).toBeVisible();
    expect(screen.getByLabelText("Task note")).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Record task time" }),
    ).toBeVisible();
    const timePicker = await screen.findByRole("combobox", {
      name: /Classification tags/,
    });
    fireEvent.focus(timePicker);
    fireEvent.click(await screen.findByRole("option", { name: "Network" }));
    fireEvent.change(screen.getByLabelText("Technician ID"), {
      target: { value: "tech-1" },
    });
    fireEvent.change(screen.getByLabelText("Actual minutes"), {
      target: { value: "30" },
    });
    fireEvent.change(screen.getByLabelText("Task note"), {
      target: { value: "Keep task context" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Record task time" }));
    await screen.findByText(
      "Classification changed. Confirm at least one current classification tag.",
    );
    expect(document.activeElement).toBe(timePicker);
    expect(screen.getByLabelText("Task note")).toHaveValue("Keep task context");
    expect(
      screen.getByRole("group", { name: /Classification tags selections/ }),
    ).toHaveTextContent("Network");
  });

  it("loads a selected project directly even when it is outside the portfolio page", async () => {
    const fetcher = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes("/api/v1/projects?")) {
        return Response.json([
          {
            id: "project-other",
            display_id: "PRJ-1000",
            name: "Other project",
            lifecycle_state: "active",
            version: 1,
          },
        ]);
      }
      if (url.endsWith("/api/v1/projects/project-target")) {
        return Response.json({
          id: "project-target",
          display_id: "PRJ-9999",
          name: "Restored project",
          client_name: "Northwind",
          lifecycle_state: "active",
          original_proposal_version: 1,
          phases: [],
          project_tasks: [],
          resource_plans: [],
          cost_actuals: [],
          capacity: [],
          change_orders: [],
          financials_visible: false,
          version: 1,
        });
      }
      if (url === "/api/v1/projects/project-target/internal-content") {
        return Response.json([]);
      }
      return Response.json({}, { status: 404 });
    });
    vi.stubGlobal("fetch", fetcher);

    render(
      <ProjectWorklist
        clientID="client-1"
        selectedRecord={{
          id: "project:project-target",
          routeID: "project",
          recordID: "project-target",
          clientID: "client-1",
          label: "PRJ-9999",
          entityType: "project",
          openedAt: 1,
        }}
      />,
    );

    expect(
      await screen.findByRole("heading", { name: "Restored project" }),
    ).toBeVisible();
    expect(
      fetcher.mock.calls.some(([input]) =>
        String(input).endsWith("/api/v1/projects/project-target"),
      ),
    ).toBe(true);
    expect(
      await screen.findByRole("region", { name: "Internal comments" }),
    ).toBeVisible();
    await waitFor(() =>
      expect(fetcher).toHaveBeenCalledWith(
        "/api/v1/projects/project-target/internal-content",
        expect.objectContaining({
          headers: expect.objectContaining({
            "X-Rarity-Client-ID": "client-1",
          }),
        }),
      ),
    );
  });

  it("switches directly to a new record without waiting for the portfolio request", async () => {
    let failPortfolio = false;
    const workspace = (id: string, displayID: string, name: string) => ({
      id,
      display_id: displayID,
      name,
      client_name: "Northwind",
      lifecycle_state: "active",
      original_proposal_version: 1,
      phases: [],
      project_tasks: [],
      resource_plans: [],
      cost_actuals: [],
      capacity: [],
      change_orders: [],
      financials_visible: false,
      version: 1,
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input);
        if (url.includes("/api/v1/tag-groups")) {
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
        if (url.includes("/api/v1/tags")) {
          return Response.json([
            {
              id: "tag-vpn",
              label: "VPN",
              group_id: "group-1",
              state: "active",
              synonyms: [],
              version: 1,
            },
          ]);
        }
        if (url.includes("/api/v1/projects?")) {
          return failPortfolio
            ? Response.json({}, { status: 503 })
            : Response.json([]);
        }
        if (url.endsWith("/project-1")) {
          return Response.json(workspace("project-1", "PRJ-1", "First"));
        }
        if (url.endsWith("/project-2")) {
          return Response.json(workspace("project-2", "PRJ-2", "Second"));
        }
        return Response.json({}, { status: 404 });
      }),
    );
    const record = (id: string, label: string) => ({
      id: `project:${id}`,
      routeID: "project" as const,
      recordID: id,
      clientID: "client-1",
      label,
      entityType: "project" as const,
      openedAt: 1,
    });
    const view = render(
      <ProjectWorklist
        clientID="client-1"
        selectedRecord={record("project-1", "PRJ-1")}
      />,
    );
    expect(await screen.findByRole("heading", { name: "First" })).toBeVisible();

    failPortfolio = true;
    view.rerender(
      <ProjectWorklist
        clientID="client-1"
        selectedRecord={record("project-2", "PRJ-2")}
      />,
    );
    expect(
      await screen.findByRole("heading", { name: "Second" }),
    ).toBeVisible();
    expect(screen.queryByRole("heading", { name: "First" })).toBeNull();
  });

  it("uses the server-calculated financial summary", () => {
    const model = projectModel({
      id: "project-1",
      display_id: "PRJ-1",
      name: "Rollout",
      client_name: "Northwind",
      lifecycle_state: "planned",
      original_proposal_version: 1,
      original_baseline: {
        currency: "USD",
        revenue_minor: 20000,
        cost_minor: 7000,
        planned_minutes: 600,
      },
      current_baseline: {
        currency: "USD",
        revenue_minor: 24000,
        cost_minor: 7000,
        planned_minutes: 600,
      },
      phases: [
        {
          id: "phase-1",
          position: 1,
          name: "Delivery",
          state: "planned",
          participating_teams: [],
          planned_minutes: 600,
          actual_minutes: 60,
          budget: { minor: 12000, currency: "USD" },
          deliverables: [],
          completion_criteria: [],
          tasks: [],
          financials: {
            original_budget: { minor: 10000, currency: "USD" },
            current_budget: { minor: 12000, currency: "USD" },
            planned_labor: { minor: 4000, currency: "USD" },
            actual_labor: { minor: 3000, currency: "USD" },
            cost_actuals: { minor: 1000, currency: "USD" },
            committed_cost: { minor: 500, currency: "USD" },
            billable_work: { minor: 9000, currency: "USD" },
            profit: { minor: 5000, currency: "USD" },
            projected_profit: { minor: 6500, currency: "USD" },
            margin_basis_points: 5555,
            actual_labor_complete: true,
            profit_available: true,
          },
          version: 1,
        },
      ],
      project_tasks: [],
      resource_plans: [],
      cost_actuals: [],
      capacity: [
        {
          id: "technician-1",
          name: "Alex Morgan",
          available_minutes: 2400,
          scheduled_minutes: 2880,
          actual_minutes: 720,
          remaining_minutes: 0,
          overbooked_minutes: 1200,
        },
      ],
      change_orders: [],
      financials: {
        original_budget: { minor: 20000, currency: "USD" },
        current_budget: { minor: 24000, currency: "USD" },
        planned_labor: { minor: 7000, currency: "USD" },
        actual_labor: { minor: 6000, currency: "USD" },
        cost_actuals: { minor: 2000, currency: "USD" },
        committed_cost: { minor: 1000, currency: "USD" },
        billable_work: { minor: 16000, currency: "USD" },
        profit: { minor: 8000, currency: "USD" },
        projected_profit: { minor: 14000, currency: "USD" },
        margin_basis_points: 5000,
        actual_labor_complete: true,
        profit_available: true,
      },
      financials_visible: true,
      version: 1,
    } as ProjectWorkspaceResponse);
    expect(model.financials.actualLaborMinor).toBe(6000);
    expect(model.financials.billableWorkMinor).toBe(16000);
    expect(model.financials.profitMinor).toBe(8000);
    expect(model.capacity).toEqual([
      expect.objectContaining({
        id: "technician-1",
        availableMinutes: 2400,
        overbookedMinutes: 1200,
      }),
    ]);
    expect(model.phases[0].financials?.profitMinor).toBe(5000);
  });

  it("loads persisted project detail and preserves financial authorization", async () => {
    const openRecord = vi.fn();
    let projectTaskAttempts = 0;
    const fetcher = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (url.includes("/api/v1/tag-groups")) {
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
        if (url.includes("/api/v1/tags")) {
          return Response.json([
            {
              id: "tag-vpn",
              label: "VPN",
              group_id: "group-1",
              state: "active",
              synonyms: [],
              version: 1,
            },
          ]);
        }
        if (url.includes("/api/v1/projects?")) {
          return new Response(
            JSON.stringify([
              {
                id: "project-1",
                display_id: "PRJ-1042",
                name: "Northwind rollout",
                lifecycle_state: "planned",
                version: 1,
              },
            ]),
            { status: 200, headers: { "Content-Type": "application/json" } },
          );
        }
        if (url === "/api/v1/phases/phase-1/tasks" && init?.method === "POST") {
          projectTaskAttempts += 1;
          if (projectTaskAttempts === 1) {
            return Response.json(
              { error: { code: "tag_archived" } },
              { status: 422 },
            );
          }
          return Response.json({
            id: "new-task",
            title: "Confirm technical handoff",
            status: "open",
            position: 2,
            version: 1,
          });
        }
        return new Response(
          JSON.stringify({
            id: "project-1",
            display_id: "PRJ-1042",
            name: "Northwind rollout",
            client_name: "Northwind",
            lifecycle_state: "planned",
            planned_start: "2026-08-01",
            planned_end: "2026-09-01",
            original_proposal_version: 3,
            original_baseline: {
              currency: "",
              revenue_minor: 0,
              cost_minor: 0,
              planned_minutes: 0,
            },
            current_baseline: {
              currency: "",
              revenue_minor: 0,
              cost_minor: 0,
              planned_minutes: 0,
            },
            phases: [
              {
                id: "phase-1",
                position: 1,
                name: "Discovery",
                state: "planned",
                participating_teams: null,
                planned_minutes: 600,
                actual_minutes: 30,
                budget: { minor: 0, currency: "" },
                deliverables: ["Runbook"],
                completion_criteria: ["Approved"],
                tasks: [
                  {
                    id: "phase-task-1",
                    title: "Validate requirements",
                    status: "open",
                    owner_name: "Alex Morgan",
                    subtasks: 0,
                    estimate_minutes: 120,
                    actual_minutes: 30,
                    version: 1,
                  },
                ],
                version: 1,
              },
            ],
            project_tasks: [
              {
                id: "task-1",
                title: "Schedule kickoff",
                status: "open",
                owner_name: "Alex Morgan",
                subtasks: 2,
                estimate_minutes: 60,
                actual_minutes: 15,
                version: 1,
              },
            ],
            resource_plans: [
              {
                id: "plan-1",
                resource_type: "team",
                resource_name: "Delivery",
                starts_on: "2026-08-01",
                ends_on: "2026-08-15",
                planned_minutes: 1200,
                version: 1,
              },
            ],
            cost_actuals: [],
            capacity: [],
            change_orders: [
              {
                order: {
                  id: "change-order-1",
                  display_id: "CO-1041",
                  state: "draft",
                  version: 1,
                },
              },
            ],
            financials_visible: false,
            version: 2,
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      },
    );
    vi.stubGlobal("fetch", fetcher);

    render(
      <WorkspaceProvider
        principalID="principal-1"
        allowedRouteIDs={new Set(["project"])}
        allowedClientIDs={new Set(["client-1"])}
        onOpenRecord={openRecord}
      >
        <ProjectWorklist
          clientID="client-1"
          selectedRecord={{
            id: "project:project-1",
            routeID: "project",
            recordID: "project-1",
            clientID: "client-1",
            label: "PRJ-1042",
            entityType: "project",
            openedAt: 1,
          }}
          capabilities={
            new Set([
              "project.resource.plan",
              "project.edit",
              "change_order.update",
              "task.create",
              "time_entry.create",
            ])
          }
        />
      </WorkspaceProvider>,
    );
    await screen.findByRole("heading", { name: "Northwind rollout" });
    expect(screen.getByText("Discovery")).toBeInTheDocument();
    expect(screen.getByText("Validate requirements")).toBeInTheDocument();
    const projectTasks = screen
      .getByRole("heading", { name: "Project tasks" })
      .closest("section");
    expect(projectTasks).not.toBeNull();
    expect(
      within(projectTasks as HTMLElement).getByText("Schedule kickoff"),
    ).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", { name: "Open task Schedule kickoff" }),
    );
    expect(openRecord).toHaveBeenCalledWith(
      expect.objectContaining({
        id: "task:task-1",
        entityType: "task",
        parentRecordID: "project-1",
      }),
    );
    expect(screen.getByText("Delivery")).toBeInTheDocument();
    expect(
      screen.getByText(
        "Additional permission is required to view financial data.",
      ),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByText("Add phase resource plan"));
    fireEvent.change(screen.getByLabelText("Phase"), {
      target: { value: "phase-1" },
    });
    fireEvent.change(screen.getByLabelText("Resource type"), {
      target: { value: "team" },
    });
    fireEvent.change(screen.getByLabelText("Role or team ID"), {
      target: { value: "team-1" },
    });
    fireEvent.change(screen.getByLabelText("Starts on"), {
      target: { value: "2026-08-01" },
    });
    fireEvent.change(screen.getByLabelText("Ends on"), {
      target: { value: "2026-08-15" },
    });
    const resourcePlanForm = screen
      .getByRole("button", { name: "Add resource plan" })
      .closest("form");
    expect(resourcePlanForm).not.toBeNull();
    fireEvent.change(
      within(resourcePlanForm as HTMLFormElement).getByLabelText(
        "Planned minutes",
      ),
      {
        target: { value: "1200" },
      },
    );
    fireEvent.click(screen.getByRole("button", { name: "Add resource plan" }));
    await waitFor(() =>
      expect(
        fetcher.mock.calls.some(
          ([input, init]) =>
            String(input).endsWith("/resource-plans") &&
            init?.method === "POST",
        ),
      ).toBe(true),
    );
    fireEvent.click(screen.getByText("Edit phase: Discovery"));
    fireEvent.change(screen.getByLabelText("Phase name"), {
      target: { value: "Technical discovery" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save phase" }));
    await waitFor(() =>
      expect(
        fetcher.mock.calls.some(
          ([input, init]) =>
            String(input) === "/api/v1/phases/phase-1" &&
            init?.method === "PATCH" &&
            (init.headers as Record<string, string>)["If-Match"] === '"1"',
        ),
      ).toBe(true),
    );
    fireEvent.click(screen.getByText("Create Change Order"));
    fireEvent.change(screen.getByLabelText("Change Order ID"), {
      target: { value: "CO-1042" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create draft" }));
    await waitFor(() =>
      expect(
        fetcher.mock.calls.some(
          ([input, init]) =>
            String(input) === "/api/v1/projects/project-1/change-orders" &&
            init?.method === "POST" &&
            String(init.body).includes('"display_id":"CO-1042"'),
        ),
      ).toBe(true),
    );
    fireEvent.change(screen.getByLabelText("Description"), {
      target: { value: "Expand deployment scope" },
    });
    fireEvent.change(screen.getByLabelText("Revenue delta"), {
      target: { value: "250000" },
    });
    fireEvent.change(screen.getByLabelText("Cost delta"), {
      target: { value: "100000" },
    });
    fireEvent.change(screen.getByLabelText("Labor delta (minutes)"), {
      target: { value: "1200" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Issue version" }));
    await waitFor(() =>
      expect(
        fetcher.mock.calls.some(
          ([input, init]) =>
            String(input) === "/api/v1/change-orders/change-order-1/versions" &&
            init?.method === "POST" &&
            String(init.body).includes('"expected_version":1'),
        ),
      ).toBe(true),
    );
    fireEvent.click(screen.getByText("Add Project task or subtask"));
    const projectTaskForm = screen
      .getByRole("button", { name: "Add Project task" })
      .closest("form");
    expect(projectTaskForm).not.toBeNull();
    const taskTagPicker = await within(
      projectTaskForm as HTMLFormElement,
    ).findByRole("combobox", { name: /Classification tags/ });
    fireEvent.focus(taskTagPicker);
    fireEvent.click(screen.getByRole("option", { name: /VPN/ }));
    fireEvent.change(screen.getByLabelText("Task title"), {
      target: { value: "Confirm technical handoff" },
    });
    fireEvent.change(screen.getByLabelText("Task owner ID"), {
      target: { value: "technician-1" },
    });
    fireEvent.change(
      within(projectTaskForm as HTMLFormElement).getByLabelText(
        "Planned minutes",
      ),
      { target: { value: "90" } },
    );
    fireEvent.change(screen.getByLabelText("Parent task"), {
      target: { value: "phase:phase-1:phase-task-1" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add Project task" }));
    await screen.findByText(
      "Classification changed. Confirm at least one current classification tag.",
    );
    expect(document.activeElement).toBe(taskTagPicker);
    expect(screen.getByLabelText("Task title")).toHaveValue(
      "Confirm technical handoff",
    );
    expect(
      within(projectTaskForm as HTMLFormElement).getByRole("group", {
        name: /Classification tags selections/,
      }),
    ).toHaveTextContent("VPN");
    fireEvent.click(screen.getByRole("button", { name: "Add Project task" }));
    await waitFor(() =>
      expect(
        fetcher.mock.calls.some(
          ([input, init]) =>
            String(input) === "/api/v1/phases/phase-1/tasks" &&
            init?.method === "POST" &&
            String(init.body).includes('"parent_task_id":"phase-task-1"') &&
            String(init.body).includes('"owner_id":"technician-1"') &&
            String(init.body).includes('"estimate_minutes":90'),
        ),
      ).toBe(true),
    );
    fireEvent.click(screen.getByText("Record Project task time"));
    const taskTimeForm = screen
      .getByRole("button", { name: "Record task time" })
      .closest("form");
    expect(taskTimeForm).not.toBeNull();
    const timeTagPicker = await within(
      taskTimeForm as HTMLFormElement,
    ).findByRole("combobox", { name: /Classification tags/ });
    fireEvent.focus(timeTagPicker);
    fireEvent.click(
      within(taskTimeForm as HTMLFormElement).getByRole("option", {
        name: /VPN/,
      }),
    );
    fireEvent.change(screen.getByLabelText("Delivery task"), {
      target: { value: "phase-task-1" },
    });
    expect(
      screen.getByRole("button", { name: "Record task time" }),
    ).toBeDisabled();
    fireEvent.focus(timeTagPicker);
    fireEvent.click(
      within(taskTimeForm as HTMLFormElement).getByRole("option", {
        name: /VPN/,
      }),
    );
    fireEvent.change(screen.getByLabelText("Technician ID"), {
      target: { value: "technician-1" },
    });
    fireEvent.change(screen.getByLabelText("Actual minutes"), {
      target: { value: "45" },
    });
    fireEvent.click(screen.getByLabelText("Billable"));
    fireEvent.click(screen.getByRole("button", { name: "Record task time" }));
    await waitFor(() =>
      expect(
        fetcher.mock.calls.some(
          ([input, init]) =>
            String(input) === "/api/v1/tasks/phase-task-1/time-entries" &&
            init?.method === "POST" &&
            String(init.body).includes('"technician_id":"technician-1"') &&
            String(init.body).includes('"billable":true'),
        ),
      ).toBe(true),
    );
    fireEvent.click(
      screen.getByText("Record Project cost", { selector: "summary" }),
    );
    fireEvent.change(screen.getByLabelText("Cost Phase"), {
      target: { value: "phase-1" },
    });
    fireEvent.change(screen.getByLabelText("Cost type"), {
      target: { value: "license" },
    });
    fireEvent.change(screen.getByLabelText("Cost description"), {
      target: { value: "Security license" },
    });
    fireEvent.change(screen.getByLabelText("Cost amount"), {
      target: { value: "2500" },
    });
    fireEvent.change(screen.getByLabelText("Incurred or committed on"), {
      target: { value: "2026-08-01" },
    });
    fireEvent.click(screen.getByLabelText(/Committed, not yet incurred/));
    fireEvent.click(
      screen.getByRole("button", { name: "Record Project cost" }),
    );
    await waitFor(() =>
      expect(
        fetcher.mock.calls.some(
          ([input, init]) =>
            String(input) === "/api/v1/projects/project-1/cost-actuals" &&
            init?.method === "POST" &&
            String(init.body).includes('"minor":250000') &&
            String(init.body).includes('"committed":true'),
        ),
      ).toBe(true),
    );
    fireEvent.click(
      screen.getByText("Recognize billable work", { selector: "summary" }),
    );
    fireEvent.change(screen.getByLabelText("Recognition Phase"), {
      target: { value: "phase-1" },
    });
    fireEvent.change(screen.getByLabelText("Recognition description"), {
      target: { value: "Accepted milestone" },
    });
    fireEvent.change(screen.getByLabelText("Recognized amount"), {
      target: { value: "12000" },
    });
    fireEvent.change(screen.getByLabelText("Recognized on"), {
      target: { value: "2026-08-15" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Recognize billable work" }),
    );
    await waitFor(() =>
      expect(
        fetcher.mock.calls.some(
          ([input, init]) =>
            String(input) === "/api/v1/projects/project-1/billable-work" &&
            init?.method === "POST" &&
            String(init.body).includes('"minor":1200000') &&
            String(init.body).includes('"phase_id":"phase-1"'),
        ),
      ).toBe(true),
    );
  });

  it("retries the project list after a load failure", async () => {
    let attempts = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        if (!String(input).includes("/api/v1/projects?")) {
          return Response.json({}, { status: 404 });
        }
        attempts += 1;
        return attempts === 1
          ? Response.json({}, { status: 503 })
          : Response.json([]);
      }),
    );

    render(<ProjectWorklist clientID="client-1" />);

    expect(
      await screen.findByRole("heading", {
        name: "Projects could not be loaded",
      }),
    ).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    expect(
      await screen.findByRole("heading", { name: "No projects" }),
    ).toBeVisible();
    expect(attempts).toBe(2);
  });

  it("retries a failed selected-project detail request", async () => {
    let detailAttempts = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input);
        if (url.includes("/api/v1/projects?")) {
          return Response.json([
            {
              id: "project-1",
              display_id: "PRJ-1042",
              name: "Northwind rollout",
              lifecycle_state: "active",
              version: 1,
            },
          ]);
        }
        if (url === "/api/v1/projects/project-1") detailAttempts += 1;
        return detailAttempts === 1 && url === "/api/v1/projects/project-1"
          ? Response.json({}, { status: 503 })
          : Response.json({
              id: "project-1",
              display_id: "PRJ-1042",
              name: "Northwind rollout",
              client_name: "Northwind",
              lifecycle_state: "active",
              original_proposal_version: 1,
              phases: [],
              project_tasks: [],
              resource_plans: [],
              cost_actuals: [],
              capacity: [],
              change_orders: [],
              financials_visible: false,
              version: 1,
            });
      }),
    );

    render(
      <ProjectWorklist
        clientID="client-1"
        selectedRecord={{
          id: "project:project-1",
          routeID: "project",
          recordID: "project-1",
          clientID: "client-1",
          label: "PRJ-1042",
          entityType: "project",
          openedAt: 1,
        }}
      />,
    );

    expect(
      await screen.findByRole("heading", {
        name: "Delivery record could not be loaded",
      }),
    ).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(
      await screen.findByRole("heading", { name: "Northwind rollout" }),
    ).toBeVisible();
    expect(detailAttempts).toBe(2);
  });

  it("direct-loads a standalone task without a project parent or portfolio membership", async () => {
    let resolveSecondTask!: (response: Response) => void;
    const fetcher = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url === "/api/v1/tasks/task-standalone")
        return Response.json({
          id: "task-standalone",
          title: "Call the vendor",
          status: "open",
          estimate_minutes: 45,
          parent: { type: "work_record", id: "ticket-1" },
        });
      if (url === "/api/v1/objects/task/task-standalone/tags")
        return Response.json({
          target: { object_type: "task", object_id: "task-standalone" },
          object_version: 1,
          direct: [],
          inherited: [],
          effective: [],
          classification_state: "unclassified",
        });
      if (url === "/api/v1/tasks/task-standalone/internal-content")
        return Response.json([]);
      if (url === "/api/v1/tasks/task-second")
        return new Promise<Response>((resolve) => {
          resolveSecondTask = resolve;
        });
      if (url === "/api/v1/tasks/task-second/internal-content")
        return Response.json([]);
      return Response.json([]);
    });
    vi.stubGlobal("fetch", fetcher);
    const view = render(
      <ProjectWorklist
        clientID="client-1"
        selectedRecord={{
          id: "task:task-standalone",
          routeID: "project",
          recordID: "task-standalone",
          clientID: "client-1",
          label: "Call the vendor",
          entityType: "task",
          openedAt: 0,
        }}
      />,
    );
    expect(
      await screen.findByRole("heading", { name: "Call the vendor" }),
    ).toBeInTheDocument();
    expect(screen.getByText("45 minutes")).toBeInTheDocument();
    expect(fetcher).toHaveBeenCalledWith(
      "/api/v1/tasks/task-standalone",
      expect.objectContaining({
        headers: expect.objectContaining({ "X-Rarity-Client-ID": "client-1" }),
      }),
    );
    expect(
      await screen.findByRole("region", { name: "Internal notes" }),
    ).toBeVisible();
    await waitFor(() =>
      expect(fetcher).toHaveBeenCalledWith(
        "/api/v1/tasks/task-standalone/internal-content",
        expect.objectContaining({
          headers: expect.objectContaining({
            "X-Rarity-Client-ID": "client-1",
          }),
        }),
      ),
    );

    view.rerender(
      <ProjectWorklist
        clientID="client-1"
        selectedRecord={{
          id: "task:task-second",
          routeID: "project",
          recordID: "task-second",
          clientID: "client-1",
          label: "Replace the firewall",
          entityType: "task",
          openedAt: 1,
        }}
      />,
    );
    expect(
      screen.queryByRole("heading", { name: "Call the vendor" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Loading task" })).toBeVisible();
    await act(async () =>
      resolveSecondTask(
        Response.json({
          id: "task-second",
          title: "Replace the firewall",
          status: "open",
          estimate_minutes: 60,
          parent: { type: "work_record", id: "ticket-2" },
        }),
      ),
    );
    expect(
      await screen.findByRole("heading", { name: "Replace the firewall" }),
    ).toBeVisible();
  });
});

function ProjectPreviewProbe() {
  const { state } = useWorkspace();
  return (
    <output data-testid="project-preview">{state.preview?.label ?? ""}</output>
  );
}
