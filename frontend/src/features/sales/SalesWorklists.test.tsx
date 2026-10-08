import axe from "axe-core";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import { OpportunityWorklist, ProposalWorklist } from "./SalesWorklists";
import salesStyles from "./sales.css?raw";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  localStorage.clear();
});

it("lays out opportunity detail metadata and forms as readable desktop blocks", () => {
  const compactStyles = salesStyles.replace(/\s+/g, "");
  expect(compactStyles).toContain(
    ".detail-carddl{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,9rem),1fr))",
  );
  expect(compactStyles).toContain(
    ".detail-cardform{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,14rem),1fr))",
  );
  expect(compactStyles).toContain(".detail-cardform>button{width:fit-content");
});

it("shows stage names, records activity, and offers only allowed transitions", async () => {
  document.cookie = "rarity_csrf=csrf-token";
  let taskCreateAttempts = 0;
  const fetcher = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === "/api/v1/opportunities?limit=100") {
        return Response.json([
          {
            id: "opportunity-1",
            client_id: "client-1",
            pipeline_id: "pipeline-1",
            stage_id: "stage-1",
            display_id: "OPP-1",
            name: "Managed services",
            amount: { minor: 125000, currency: "USD" },
            fields: { expected_close_on: "2026-08-30" },
            custom_fields: { procurement_reference: "PO pending" },
            team_id: "team-1",
            contact_ids: ["contact-1"],
            proposal_issued: false,
            approval_granted: false,
            version: 1,
            updated_at: "2026-07-30T12:00:00Z",
          },
        ]);
      }
      if (url === "/api/v1/pipelines") {
        return Response.json([
          {
            id: "pipeline-1",
            key: "default",
            name: "Default",
            stages: [
              {
                id: "stage-1",
                pipeline_id: "pipeline-1",
                key: "qualified",
                name: "Qualified",
                position: 1,
                probability: 40,
                forecast_category: "weighted",
                required_fields: [],
                allowed_next_stage_ids: ["stage-2"],
                requires_proposal: false,
                requires_approval: false,
              },
              {
                id: "stage-2",
                pipeline_id: "pipeline-1",
                key: "proposal",
                name: "Proposal",
                position: 2,
                probability: 70,
                forecast_category: "committed",
                required_fields: [],
                allowed_next_stage_ids: [],
                requires_proposal: true,
                requires_approval: false,
              },
            ],
          },
        ]);
      }
      if (url === "/api/v1/opportunity-forecast") {
        return Response.json([
          {
            pipeline_id: "pipeline-1",
            stage_id: "stage-1",
            stage_name: "Qualified",
            forecast_category: "weighted",
            probability: 40,
            opportunity_count: 1,
            amount: { minor: 125000, currency: "USD" },
            weighted_amount: { minor: 50000, currency: "USD" },
          },
        ]);
      }
      if (url === "/api/v1/tag-groups") {
        return Response.json([
          {
            id: "technology",
            label: "Technology",
            description: "",
            state: "active",
            system_managed: false,
            version: 1,
          },
        ]);
      }
      if (url === "/api/v1/tags") {
        return Response.json([
          {
            id: "tag-vpn",
            label: "VPN",
            group_id: "technology",
            state: "active",
            synonyms: [],
            version: 1,
          },
        ]);
      }
      if (url === "/api/v1/objects/task/task-1/tags") {
        return Response.json({
          target: { object_type: "task", object_id: "task-1" },
          object_version: 1,
          direct: [],
          inherited: [],
          effective: [
            {
              id: "assignment-1",
              tag: {
                id: "tag-vpn",
                label: "VPN",
                group_id: "technology",
                state: "active",
                synonyms: [],
                version: 1,
              },
              source: "human",
              inherited: false,
            },
          ],
          classification_state: "classified",
        });
      }
      if (url.endsWith("/activities?limit=100")) return Response.json([]);
      if (url.endsWith("/tasks") && !init?.method) return Response.json([]);
      if (url.endsWith("/attachments") && !init?.method)
        return Response.json([]);
      if (url.endsWith("/activities") && init?.method === "POST") {
        return Response.json(
          {
            id: "activity-1",
            opportunity_id: "opportunity-1",
            kind: "call",
            summary: "Discovery completed",
            occurred_at: "2026-07-30T13:00:00Z",
          },
          { status: 201 },
        );
      }
      if (
        url === "/api/v1/opportunities/opportunity-1" &&
        init?.method === "PATCH"
      ) {
        return Response.json({
          id: "opportunity-1",
          client_id: "client-1",
          pipeline_id: "pipeline-1",
          stage_id: "stage-2",
          display_id: "OPP-1",
          name: "Managed services",
          amount: { minor: 125000, currency: "USD" },
          fields: { expected_close_on: "2026-08-30" },
          custom_fields: { procurement_reference: "PO pending" },
          team_id: "team-1",
          contact_ids: ["contact-1"],
          proposal_issued: false,
          approval_granted: false,
          version: 2,
          updated_at: "2026-07-30T13:00:00Z",
        });
      }
      if (url.endsWith("/tasks") && init?.method === "POST") {
        taskCreateAttempts += 1;
        if (taskCreateAttempts === 1) {
          return Response.json(
            { error: { code: "tag_archived", message: "VPN was archived" } },
            { status: 422 },
          );
        }
        return Response.json(
          {
            id: "task-1",
            title: "Prepare discovery scope",
            status: "open",
            position: 1,
            version: 1,
          },
          { status: 201 },
        );
      }
      if (url.endsWith("/custom-fields") && init?.method === "PUT") {
        return Response.json({
          id: "opportunity-1",
          client_id: "client-1",
          pipeline_id: "pipeline-1",
          stage_id: "stage-1",
          display_id: "OPP-1",
          name: "Managed services",
          amount: { minor: 125000, currency: "USD" },
          fields: { expected_close_on: "2026-08-30" },
          custom_fields: { risk_summary: "Weekend cutover" },
          team_id: "team-1",
          contact_ids: ["contact-1"],
          proposal_issued: false,
          approval_granted: false,
          version: 2,
          updated_at: "2026-07-30T13:00:00Z",
        });
      }
      if (url.endsWith("/participants") && init?.method === "PUT") {
        return Response.json({
          id: "opportunity-1",
          client_id: "client-1",
          pipeline_id: "pipeline-1",
          stage_id: "stage-1",
          display_id: "OPP-1",
          name: "Managed services",
          amount: { minor: 125000, currency: "USD" },
          fields: { expected_close_on: "2026-08-30" },
          custom_fields: { risk_summary: "Weekend cutover" },
          team_id: "team-2",
          contact_ids: ["contact-2", "contact-3"],
          proposal_issued: false,
          approval_granted: false,
          version: 3,
          updated_at: "2026-07-30T14:00:00Z",
        });
      }
      if (url.endsWith("/attachments") && init?.method === "POST") {
        return Response.json(
          {
            id: "attachment-1",
            opportunity_id: "opportunity-1",
            filename: "scope.txt",
            content_type: "text/plain",
            size_bytes: 5,
            sha256: "00",
            version: 1,
            created_at: "2026-07-30T13:00:00Z",
          },
          { status: 201 },
        );
      }
      return Response.json({}, { status: 500 });
    },
  );
  vi.stubGlobal("fetch", fetcher);

  const { container } = render(
    <OpportunityWorklist
      clientID="client-1"
      principalID="principal-1"
      capabilities={
        new Set([
          "opportunity.read",
          "opportunity.transition",
          "opportunity.activity.create",
          "opportunity.update",
          "attachment.create",
          "task.create",
        ])
      }
    />,
  );
  expect((await screen.findAllByText("Qualified")).length).toBeGreaterThan(0);
  fireEvent.click(screen.getByRole("button", { name: "Kanban view" }));
  expect(localStorage.getItem("rti:view:principal-1:sales")).toBe('"kanban"');
  fireEvent.click(screen.getByRole("button", { name: "List view" }));
  expect(screen.getByText("$500.00")).toBeInTheDocument();
  expect(
    screen.getByRole("button", { name: "Move to Proposal" }),
  ).toBeInTheDocument();
  const transitionButton = screen.getByRole("button", {
    name: "Move to Proposal",
  });
  expect(transitionButton).toBeDisabled();
  fireEvent.change(screen.getByLabelText("Transition reason"), {
    target: { value: "Discovery completed" },
  });
  expect(transitionButton).toBeEnabled();
  fireEvent.click(transitionButton);
  await waitFor(() =>
    expect(
      fetcher.mock.calls.some(
        ([input, init]) =>
          String(input) === "/api/v1/opportunities/opportunity-1" &&
          init?.method === "PATCH",
      ),
    ).toBe(true),
  );
  const transitionMutation = fetcher.mock.calls.find(
    ([input, init]) =>
      String(input) === "/api/v1/opportunities/opportunity-1" &&
      init?.method === "PATCH",
  );
  expect(String(transitionMutation?.[1]?.body)).toContain(
    '"reason":"Discovery completed"',
  );
  const customFields = screen.getByRole("group", { name: "Custom fields" });
  expect(within(customFields).getByLabelText("Field 1")).toHaveValue(
    "procurement_reference",
  );
  expect(within(customFields).getByLabelText("Value")).toHaveValue(
    "PO pending",
  );
  fireEvent.change(within(customFields).getByLabelText("Field 1"), {
    target: { value: "risk_summary" },
  });
  fireEvent.change(within(customFields).getByLabelText("Value"), {
    target: { value: "Weekend cutover" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save custom fields" }));
  await waitFor(() =>
    expect(
      fetcher.mock.calls.some(
        ([input, init]) =>
          String(input).endsWith("/custom-fields") && init?.method === "PUT",
      ),
    ).toBe(true),
  );
  const fieldMutation = fetcher.mock.calls.find(
    ([input, init]) =>
      String(input).endsWith("/custom-fields") && init?.method === "PUT",
  );
  expect(String(fieldMutation?.[1]?.body)).toContain(
    '"risk_summary":"Weekend cutover"',
  );
  fireEvent.change(screen.getByLabelText("Participating team ID"), {
    target: { value: "team-2" },
  });
  fireEvent.change(screen.getByLabelText("Contact ID 1"), {
    target: { value: "contact-2" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Add contact id" }));
  fireEvent.change(screen.getByLabelText("Contact ID 2"), {
    target: { value: "contact-3" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save participants" }));
  await waitFor(() =>
    expect(
      fetcher.mock.calls.some(
        ([input, init]) =>
          String(input).endsWith("/participants") && init?.method === "PUT",
      ),
    ).toBe(true),
  );
  const participantMutation = fetcher.mock.calls.find(
    ([input, init]) =>
      String(input).endsWith("/participants") && init?.method === "PUT",
  );
  expect(String(participantMutation?.[1]?.body)).toContain(
    '"contact_ids":["contact-2","contact-3"]',
  );
  fireEvent.change(screen.getByLabelText("Type"), {
    target: { value: "call" },
  });
  fireEvent.change(screen.getByLabelText("Summary"), {
    target: { value: "Discovery completed" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Add activity" }));
  await waitFor(() =>
    expect(screen.getByText(/Discovery completed/)).toBeInTheDocument(),
  );
  const mutation = fetcher.mock.calls.find(
    ([input, init]) =>
      String(input).endsWith("/activities") && init?.method === "POST",
  );
  expect(mutation?.[1]?.headers).toMatchObject({
    "X-Rarity-Client-ID": "client-1",
    "X-Rarity-CSRF": "csrf-token",
  });
  fireEvent.change(screen.getByLabelText("Task title"), {
    target: { value: "Prepare discovery scope" },
  });
  fireEvent.change(screen.getByLabelText("Task owner ID"), {
    target: { value: "technician-1" },
  });
  fireEvent.change(screen.getByLabelText("Task planned minutes"), {
    target: { value: "120" },
  });
  const taskClassification = await screen.findByRole("combobox", {
    name: /Classification tags/,
  });
  fireEvent.focus(taskClassification);
  fireEvent.click(await screen.findByRole("option", { name: "VPN" }));
  fireEvent.click(screen.getByRole("button", { name: "Add task" }));
  await screen.findByText(
    "Classification changed. Confirm at least one current classification tag.",
  );
  expect(document.activeElement).toBe(taskClassification);
  expect(screen.getByLabelText("Task title")).toHaveValue(
    "Prepare discovery scope",
  );
  expect(
    screen.getByRole("group", { name: /Classification tags selections/ }),
  ).toHaveTextContent("VPN");
  fireEvent.click(screen.getByRole("button", { name: "Add task" }));
  await waitFor(() =>
    expect(screen.getByText("Prepare discovery scope")).toBeInTheDocument(),
  );
  const taskMutation = fetcher.mock.calls.find(
    ([input, init]) =>
      String(input).endsWith("/tasks") && init?.method === "POST",
  );
  expect(String(taskMutation?.[1]?.body)).toContain(
    '"owner_id":"technician-1"',
  );
  expect(String(taskMutation?.[1]?.body)).toContain('"estimate_minutes":120');
  expect(String(taskMutation?.[1]?.body)).toContain('"tag_ids":["tag-vpn"]');
  expect(await screen.findByText("VPN")).toBeInTheDocument();
  const file = new File(["scope"], "scope.txt", { type: "text/plain" });
  const user = userEvent.setup();
  const attachmentInput = screen.getByLabelText("Add attachment");
  await user.upload(attachmentInput, file);
  await waitFor(() =>
    expect((attachmentInput as HTMLInputElement).files?.[0]).toBe(file),
  );
  fireEvent.submit(attachmentInput.closest("form")!);
  await waitFor(() =>
    expect(screen.getByText("scope.txt")).toBeInTheDocument(),
  );
  const attachmentMutation = fetcher.mock.calls.find(
    ([input, init]) =>
      String(input).endsWith("/attachments") && init?.method === "POST",
  );
  expect(attachmentMutation?.[1]?.headers).toMatchObject({
    "Content-Type": "text/plain",
    "X-Rarity-Filename": "scope.txt",
    "X-Rarity-Client-ID": "client-1",
    "X-Rarity-CSRF": "csrf-token",
  });
  const result = await axe.run(container, {
    runOnly: {
      type: "tag",
      values: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"],
    },
    rules: { "color-contrast": { enabled: false } },
  });
  expect(result.violations).toEqual([]);
});

it("hides mutations from read-only users", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) =>
      Response.json(String(input).includes("pipelines") ? [] : []),
    ),
  );
  render(
    <OpportunityWorklist
      clientID="client-1"
      capabilities={new Set(["opportunity.read"])}
    />,
  );
  expect(
    await screen.findByText("No active opportunities for this Client."),
  ).toBeInTheDocument();
  expect(screen.queryByText("Create opportunity")).not.toBeInTheDocument();
  expect(screen.queryByText("Add activity")).not.toBeInTheDocument();
});

it("issues a typed immutable proposal version with approval rules", async () => {
  document.cookie = "rarity_csrf=csrf-token";
  const fetcher = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === "/api/v1/proposals?limit=100") {
        return Response.json([
          {
            id: "proposal-1",
            client_id: "client-1",
            opportunity_id: "opportunity-1",
            display_id: "PROP-1",
            current_version: 0,
            state: "draft",
            version: 1,
            updated_at: "2026-07-30T12:00:00Z",
          },
        ]);
      }
      if (url === "/api/v1/opportunities?limit=100") return Response.json([]);
      if (url.endsWith("/versions") && init?.method === "POST") {
        return Response.json(
          {
            id: "version-1",
            proposal_id: "proposal-1",
            version: 1,
            state: "issued",
            currency: "USD",
            lines: [],
            subtotal: { minor: 100000, currency: "USD" },
            tax_total: { minor: 0, currency: "USD" },
            total: { minor: 100000, currency: "USD" },
            cost: { minor: 50000, currency: "USD" },
            margin: { minor: 50000, currency: "USD" },
            requires_internal_approval: true,
          },
          { status: 201 },
        );
      }
      return Response.json({}, { status: 500 });
    },
  );
  vi.stubGlobal("fetch", fetcher);
  render(
    <ProposalWorklist
      clientID="client-1"
      capabilities={new Set(["proposal.read", "proposal.issue"])}
    />,
  );
  expect((await screen.findAllByText("PROP-1")).length).toBeGreaterThan(0);
  fireEvent.click(screen.getByText("Issue new immutable version"));
  fireEvent.change(screen.getByLabelText("Line type"), {
    target: { value: "recurring_service" },
  });
  fireEvent.change(screen.getByLabelText("Description"), {
    target: { value: "Managed services" },
  });
  fireEvent.change(screen.getByLabelText("Unit price"), {
    target: { value: "1000" },
  });
  fireEvent.change(screen.getByLabelText("Recurrence"), {
    target: { value: "monthly" },
  });
  fireEvent.change(screen.getByLabelText("Approval above amount"), {
    target: { value: "500" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Issue version" }));
  await waitFor(() =>
    expect(
      fetcher.mock.calls.some(
        ([input, init]) =>
          String(input).endsWith("/versions") && init?.method === "POST",
      ),
    ).toBe(true),
  );
  const mutation = fetcher.mock.calls.find(
    ([input, init]) =>
      String(input).endsWith("/versions") && init?.method === "POST",
  );
  const body = JSON.parse(String(mutation?.[1]?.body)) as Record<
    string,
    unknown
  >;
  expect(body).toMatchObject({
    expected_version: 0,
    currency: "USD",
    approval_rule: {
      maximum_without_approval_minor: 50000,
      minimum_margin_basis_points: 0,
    },
  });
  expect(body.lines).toEqual([
    expect.objectContaining({
      type: "recurring_service",
      recurrence: "monthly",
      quantity: 1,
    }),
  ]);
});

it("uses the loaded approval version before approval and offline acceptance", async () => {
  document.cookie = "rarity_csrf=csrf-token";
  const fetcher = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === "/api/v1/proposals?limit=100") {
        return Response.json([
          {
            id: "proposal-1",
            client_id: "client-1",
            opportunity_id: "opportunity-1",
            display_id: "PROP-1",
            current_version: 1,
            current_version_id: "version-1",
            state: "issued",
            version: 2,
            updated_at: "2026-07-30T12:00:00Z",
          },
        ]);
      }
      if (url === "/api/v1/opportunities?limit=100") return Response.json([]);
      if (url === "/api/v1/proposal-versions/version-1") {
        return Response.json({
          id: "version-1",
          proposal_id: "proposal-1",
          version: 1,
          state: "issued",
          currency: "USD",
          lines: [],
          subtotal: { minor: 100000, currency: "USD" },
          tax_total: { minor: 0, currency: "USD" },
          total: { minor: 100000, currency: "USD" },
          cost: { minor: 50000, currency: "USD" },
          margin: { minor: 50000, currency: "USD" },
          requires_internal_approval: true,
        });
      }
      if (url.endsWith("/internal-approval") && init?.method === "POST") {
        return Response.json({
          id: "approval-1",
          proposal_version_id: "version-1",
          state: "approved",
          reason: "Margin reviewed",
          version: 4,
        });
      }
      if (url.endsWith("/internal-approval")) {
        return Response.json({
          id: "approval-1",
          proposal_version_id: "version-1",
          state: "pending",
          version: 3,
        });
      }
      if (url.endsWith("/accept") && init?.method === "POST") {
        return Response.json({ id: "acceptance-1" }, { status: 201 });
      }
      return Response.json({}, { status: 500 });
    },
  );
  vi.stubGlobal("fetch", fetcher);
  render(
    <ProposalWorklist
      clientID="client-1"
      capabilities={
        new Set([
          "proposal.read",
          "proposal.approve",
          "proposal.acceptance.record",
        ])
      }
    />,
  );
  expect(await screen.findByText("pending")).toBeInTheDocument();
  fireEvent.change(screen.getByLabelText("Approval reason"), {
    target: { value: "Margin reviewed" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Approve version" }));
  expect(
    await screen.findByText("Record offline customer acceptance"),
  ).toBeInTheDocument();
  const approvalMutation = fetcher.mock.calls.find(
    ([input, init]) =>
      String(input).endsWith("/internal-approval") && init?.method === "POST",
  );
  expect(approvalMutation?.[1]?.headers).toMatchObject({
    "If-Match": '"3"',
  });
  expect(String(approvalMutation?.[1]?.body)).toContain(
    '"reason":"Margin reviewed"',
  );
});
