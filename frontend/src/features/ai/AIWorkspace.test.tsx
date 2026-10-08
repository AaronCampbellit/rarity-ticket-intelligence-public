import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { DirectoryClient } from "../../api/browserSession";
import { AIProposal } from "./AIProposal";
import { AIWorkspace } from "./AIWorkspace";
import { AIWorkspaceClarificationError } from "./workspaceApi";
import workspaceStyles from "./workspace.css?raw";
import type {
  AIWorkspaceAPI,
  AIWorkspaceConversation,
  AIWorkspaceProposal,
  AIWorkspaceReadResult,
} from "./types";

afterEach(cleanup);

const conversation: AIWorkspaceConversation = {
  id: "conversation-1",
  client_id: "client-1",
  title: "Support",
  version: 1,
  created_at: "2026-08-04T12:00:00Z",
  updated_at: "2026-08-04T12:00:00Z",
};

const proposal: AIWorkspaceProposal = {
  id: "proposal-1",
  target_client_id: "client-2",
  tool_name: "ticket.transition",
  tool_version: 1,
  preview: {
    summary: "Change INC-1 status to resolved",
    target_type: "work_record",
    target_id: "ticket-1",
    target_version: 3,
    changes: { status: { before: "open", after: "resolved" } },
  },
  required_capability: "work_record.transition",
  expires_at: "2026-08-04T12:10:00Z",
  state: "pending",
  version: 1,
};

const clients: DirectoryClient[] = [
  {
    id: "client-1",
    display_id: "CLIENT-001",
    name: "Northwind Legal",
  },
  {
    id: "client-2",
    display_id: "CLIENT-002",
    name: "Contoso Manufacturing",
  },
  {
    id: "client-3",
    display_id: "CLIENT-003",
    name: "Fabrikam Services",
  },
];

function api(): AIWorkspaceAPI {
  return {
    list: vi.fn(async () => [conversation]),
    create: vi.fn(async () => conversation),
    get: vi.fn(async () => ({
      conversation,
      messages: [
        {
          id: "message-1",
          conversation_id: conversation.id,
          role: "assistant" as const,
          text: "How can I help?",
          created_at: "2026-08-04T12:00:00Z",
        },
      ],
    })),
    sendMessage: vi.fn(async (conversationID, text) => ({
      user_message: {
        id: "message-2",
        conversation_id: conversationID,
        role: "user" as const,
        text,
        created_at: "2026-08-04T12:01:00Z",
      },
      assistant_message: {
        id: "message-3",
        conversation_id: conversationID,
        role: "assistant" as const,
        text: "Open Work, then choose the queue.",
        created_at: "2026-08-04T12:01:01Z",
      },
    })),
    propose: vi.fn(async () => proposal),
    runRead: vi.fn(async () => ({
      summary: "Listed client resources",
      data: { resources: [] },
    })),
    confirm: vi.fn(async () => ({ summary: "Updated INC-1" })),
    reject: vi.fn(async () => undefined),
  };
}

describe("AIWorkspace", () => {
  it("groups the complete approved catalog and filters every action by capability", async () => {
    const user = userEvent.setup();
    render(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-1"
        capabilities={
          new Set([
            "work_record.read",
            "search.read",
            "project.read",
            "work_record.transition",
            "work_record.edit",
            "comment.internal.create",
            "comment.public.create",
            "work_record.route",
            "work_record.create",
            "work_record.assign",
            "project.create",
            "task.create",
            "client.create",
            "location.create",
            "location.update",
            "location.lifecycle",
            "knowledge.read",
            "knowledge.edit",
            "opportunity.read",
            "opportunity.transition",
            "opportunity.activity.create",
            "proposal.read",
            "proposal.create",
            "knowledge.publish",
            "prospect.create",
          ])
        }
        api={api()}
        onClose={() => undefined}
      />,
    );
    await screen.findByText("How can I help?");
    await user.click(screen.getByRole("button", { name: "Prepare action" }));

    const action = screen.getByRole("combobox", { name: "Action" });
    expect(
      Array.from(action.querySelectorAll("optgroup"), (group) => group.label),
    ).toEqual(["Read", "Work", "Client resources", "Knowledge", "Sales"]);
    expect(action).toHaveTextContent("Get ticket");
    expect(action).toHaveTextContent("Search tickets");
    expect(action).toHaveTextContent("List projects");
    expect(action).toHaveTextContent("Get project");
    expect(action).toHaveTextContent("Change status");
    expect(action).toHaveTextContent("Add client-visible reply");
    expect(action).toHaveTextContent("Route ticket");
    expect(action).toHaveTextContent("Create project with tasks");
    expect(action).toHaveTextContent("Add project task");
    expect(action).toHaveTextContent("Create client");
    expect(action).toHaveTextContent("Search knowledge");
    expect(action).toHaveTextContent("Get knowledge article");
    expect(action).toHaveTextContent("Create knowledge draft");
    expect(action).toHaveTextContent("Revise knowledge draft");
    expect(action).toHaveTextContent("List prospects");
    expect(action).toHaveTextContent("Create prospect");
    expect(action).toHaveTextContent("Create ticket");
    expect(action).toHaveTextContent("Assign ticket");
    expect(action).toHaveTextContent("List opportunities");
    expect(action).toHaveTextContent("Get opportunity");
    expect(action).toHaveTextContent("Move opportunity stage");
    expect(action).toHaveTextContent("Add opportunity activity");
    expect(action).toHaveTextContent("List proposals");
    expect(action).toHaveTextContent("Get proposal");
    expect(action).toHaveTextContent("Create proposal draft");
    expect(action).toHaveTextContent("Publish internal knowledge");
    expect(action).not.toHaveTextContent("Issue proposal");
    expect(action).not.toHaveTextContent("Accept proposal");
    expect(action).not.toHaveTextContent("Convert opportunity");
    expect(action.textContent?.toLowerCase()).not.toContain("email");
  });

  it("shows only authorized catalog actions", async () => {
    const user = userEvent.setup();
    render(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-1"
        capabilities={new Set(["knowledge.read"])}
        api={api()}
        onClose={() => undefined}
      />,
    );
    await screen.findByText("How can I help?");
    await user.click(screen.getByRole("button", { name: "Prepare action" }));

    const action = screen.getByRole("combobox", { name: "Action" });
    expect(
      Array.from(action.querySelectorAll("optgroup"), (group) => group.label),
    ).toEqual(["Knowledge"]);
    expect(
      within(action)
        .getAllByRole("option")
        .map((option) => option.textContent),
    ).toEqual(["Search knowledge", "Get knowledge article"]);
  });

  it("uses required action-specific fields and no generic value for second-wave writes", async () => {
    const user = userEvent.setup();
    render(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-1"
        capabilities={
          new Set([
            "work_record.create",
            "work_record.assign",
            "opportunity.transition",
            "opportunity.activity.create",
            "proposal.create",
            "knowledge.publish",
          ])
        }
        api={api()}
        onClose={() => undefined}
      />,
    );
    await screen.findByText("How can I help?");
    await user.click(screen.getByRole("button", { name: "Prepare action" }));
    const action = screen.getByRole("combobox", { name: "Action" });

    await user.selectOptions(action, "ticket_create");
    expect(screen.getByLabelText("Ticket type")).toBeRequired();
    expect(screen.getByLabelText("Ticket status")).toBeRequired();
    expect(screen.getByLabelText("Ticket priority")).toBeRequired();
    expect(screen.queryByLabelText("New value")).not.toBeInTheDocument();

    await user.selectOptions(action, "ticket_assign");
    expect(screen.getByLabelText("Technician")).toBeRequired();

    await user.selectOptions(action, "opportunity_transition");
    expect(screen.getByLabelText("Destination stage")).toBeRequired();

    await user.selectOptions(action, "opportunity_activity_create");
    expect(screen.getByLabelText("Activity kind")).toBeRequired();
    expect(screen.getByLabelText("Activity summary")).toBeRequired();
    expect(screen.getByLabelText("Activity details")).toBeRequired();
    expect(screen.getByText("Time")).toBeVisible();
    expect(screen.getByText("Recorded when confirmed")).toBeVisible();
    expect(screen.queryByLabelText("Activity time")).not.toBeInTheDocument();

    await user.selectOptions(action, "proposal_create");
    expect(screen.getByLabelText("Proposal display ID")).toBeRequired();

    await user.selectOptions(action, "knowledge_publish");
    expect(screen.getByLabelText("Publication reason")).toBeRequired();
  });

  it("submits a Ticket create from the keyboard with the exact selected Client", async () => {
    const user = userEvent.setup();
    const workspaceAPI = api();
    render(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-1"
        capabilities={new Set(["work_record.create"])}
        api={workspaceAPI}
        onClose={() => undefined}
      />,
    );
    await screen.findByText("How can I help?");
    await user.click(screen.getByRole("button", { name: "Prepare action" }));
    await user.type(screen.getByLabelText("Ticket display ID"), "INC-2042");
    await user.selectOptions(screen.getByLabelText("Ticket type"), "incident");
    await user.type(screen.getByLabelText("Ticket title"), "VPN outage");
    await user.type(
      screen.getByLabelText("Ticket description"),
      "Users cannot connect.",
    );
    await user.type(screen.getByLabelText("Ticket status"), "new");
    await user.selectOptions(screen.getByLabelText("Ticket priority"), "high");
    await user.type(screen.getByLabelText("Service"), "Managed IT");
    screen.getByLabelText("Service").focus();
    await user.keyboard("{Enter}");

    expect(workspaceAPI.propose).toHaveBeenCalledWith(
      "conversation-1",
      "ticket.create",
      {
        client: "Northwind Legal",
        display_id: "INC-2042",
        type: "incident",
        title: "VPN outage",
        description: "Users cannot connect.",
        status: "new",
        priority: "high",
        service: "Managed IT",
      },
    );
  });

  it("lets a keyboard user cancel structured preparation without closing chat", async () => {
    const user = userEvent.setup();
    render(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-1"
        capabilities={new Set(["proposal.create"])}
        api={api()}
        onClose={() => undefined}
      />,
    );
    await screen.findByText("How can I help?");
    await user.click(screen.getByRole("button", { name: "Prepare action" }));
    const cancel = screen.getByRole("button", { name: "Cancel" });
    cancel.focus();
    await user.keyboard("{Enter}");

    expect(
      screen.queryByRole("heading", { name: "Prepare an action" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Prepare action" }),
    ).toBeVisible();
    expect(screen.getByText("How can I help?")).toBeVisible();
  });

  it("keeps MSP-global Client creation available before the first Client exists", async () => {
    const user = userEvent.setup();
    const workspaceAPI = api();
    render(
      <AIWorkspace
        open
        clients={[]}
        pageClientID=""
        capabilities={new Set(["client.create"])}
        api={workspaceAPI}
        onClose={() => undefined}
      />,
    );
    await screen.findByText("How can I help?");
    await user.click(screen.getByRole("button", { name: "Prepare action" }));

    expect(screen.queryByLabelText("Target client")).not.toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Action" })).toHaveValue(
      "client",
    );
    await user.type(screen.getByLabelText("Display ID"), "FIRST-100");
    await user.type(screen.getByLabelText("Client name"), "First Client");
    await user.click(screen.getByRole("button", { name: "Review action" }));

    expect(workspaceAPI.propose).toHaveBeenCalledWith(
      "conversation-1",
      "client.create",
      { display_id: "FIRST-100", name: "First Client" },
    );
  });

  it("shows a safe exact-reference clarification for a structured read", async () => {
    const user = userEvent.setup();
    const workspaceAPI = api();
    workspaceAPI.runRead = vi.fn(async () => {
      throw new AIWorkspaceClarificationError(
        "More than one exact Project matches. Use its display ID and try again.",
      );
    });
    render(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-1"
        capabilities={new Set(["project.read"])}
        api={workspaceAPI}
        onClose={() => undefined}
      />,
    );
    await screen.findByText("How can I help?");
    await user.click(screen.getByRole("button", { name: "Prepare action" }));
    await user.selectOptions(
      screen.getByRole("combobox", { name: "Action" }),
      "project_get",
    );
    await user.type(screen.getByLabelText("Project"), "Modernization");
    await user.click(screen.getByRole("button", { name: "Run read" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "More than one exact Project matches. Use its display ID and try again.",
    );
  });

  it("dispatches MSP-global Prospect input without a Target client selector", async () => {
    const user = userEvent.setup();
    const workspaceAPI = api();
    workspaceAPI.propose = vi.fn(async () => ({
      ...proposal,
      target_client_id: undefined,
      tool_name: "prospect.create",
      preview: {
        summary: "Create prospect PRO-200",
        target_type: "prospect",
        target_id: "prospect-1",
        changes: {
          display_id: { before: null, after: "PRO-200" },
          name: { before: null, after: "Alpha Services" },
        },
      },
      required_capability: "prospect.create",
    }));
    render(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-1"
        capabilities={new Set(["prospect.create"])}
        api={workspaceAPI}
        onClose={() => undefined}
      />,
    );
    await screen.findByText("How can I help?");
    await user.click(screen.getByRole("button", { name: "Prepare action" }));
    await user.selectOptions(
      screen.getByRole("combobox", { name: "Action" }),
      "prospect_create",
    );

    expect(screen.queryByLabelText("Target client")).not.toBeInTheDocument();
    await user.type(screen.getByLabelText("Display ID"), "PRO-200");
    await user.type(screen.getByLabelText("Prospect name"), "Alpha Services");
    await user.type(
      screen.getByLabelText("Optional email"),
      "hello@example.com",
    );
    await user.click(screen.getByRole("button", { name: "Review action" }));

    expect(workspaceAPI.propose).toHaveBeenCalledWith(
      "conversation-1",
      "prospect.create",
      {
        display_id: "PRO-200",
        name: "Alpha Services",
        email: "hello@example.com",
      },
    );
    expect(await screen.findByText("New prospect")).toBeVisible();
    expect(screen.getByText("Alpha Services (PRO-200)")).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Confirm Create prospect PRO-200" }),
    ).toBeEnabled();
  });

  it("dispatches a Client-bound operational read and drops stale target results", async () => {
    const user = userEvent.setup();
    let resolveRead:
      | ((value: { summary: string; data: Record<string, unknown> }) => void)
      | undefined;
    const workspaceAPI = api();
    workspaceAPI.runRead = vi.fn(
      () =>
        new Promise<AIWorkspaceReadResult>((resolve) => {
          resolveRead = resolve;
        }),
    );
    render(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-1"
        capabilities={new Set(["opportunity.read"])}
        api={workspaceAPI}
        onClose={() => undefined}
      />,
    );
    await screen.findByText("How can I help?");
    await user.click(screen.getByRole("button", { name: "Prepare action" }));
    await user.selectOptions(
      screen.getByLabelText("Action"),
      "opportunity_get",
    );
    await user.type(screen.getByLabelText("Opportunity"), "OPP-2042");
    await user.click(screen.getByRole("button", { name: "Run read" }));

    expect(workspaceAPI.runRead).toHaveBeenCalledWith(
      "conversation-1",
      "opportunity.get",
      { client: "Northwind Legal", opportunity: "OPP-2042" },
      expect.any(AbortSignal),
    );
    await user.selectOptions(
      screen.getByLabelText("Target client"),
      "client-2",
    );
    resolveRead?.({
      summary: "Found opportunity OPP-2042",
      data: {
        display_id: "OPP-2042",
        name: "Managed services expansion",
        stage: { name: "Discovery" },
      },
    });
    await waitFor(() =>
      expect(
        screen.queryByLabelText("Structured read results"),
      ).not.toBeInTheDocument(),
    );
  });

  it("drops a proposal prepared for an action that changed while loading", async () => {
    const user = userEvent.setup();
    let resolveProposal: ((value: AIWorkspaceProposal) => void) | undefined;
    const workspaceAPI = api();
    workspaceAPI.propose = vi.fn(
      () =>
        new Promise<AIWorkspaceProposal>((resolve) => {
          resolveProposal = resolve;
        }),
    );
    render(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-1"
        capabilities={new Set(["work_record.assign", "proposal.create"])}
        api={workspaceAPI}
        onClose={() => undefined}
      />,
    );
    await screen.findByText("How can I help?");
    await user.click(screen.getByRole("button", { name: "Prepare action" }));
    await user.selectOptions(
      screen.getByLabelText("Action"),
      "proposal_create",
    );
    await user.type(screen.getByLabelText("Opportunity"), "OPP-2042");
    await user.type(screen.getByLabelText("Proposal display ID"), "PROP-2042");
    await user.click(screen.getByRole("button", { name: "Review action" }));
    await waitFor(() => expect(workspaceAPI.propose).toHaveBeenCalledTimes(1));
    await user.selectOptions(screen.getByLabelText("Action"), "ticket_assign");
    resolveProposal?.(proposal);

    await waitFor(() =>
      expect(screen.queryByRole("status")).not.toBeInTheDocument(),
    );
    expect(
      screen.queryByLabelText("AI action preview"),
    ).not.toBeInTheDocument();
  });

  it("filters Client-resource modes by capability and changes the dynamic form", async () => {
    const user = userEvent.setup();
    const workspaceAPI = api() as AIWorkspaceAPI & {
      runRead: ReturnType<typeof vi.fn>;
    };
    workspaceAPI.runRead = vi.fn(async () => ({
      summary: "Listed client resources",
      data: { resources: [] },
    }));
    render(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-1"
        capabilities={
          new Set(["search.read", "contact.create", "asset.create"])
        }
        api={workspaceAPI}
        onClose={() => undefined}
      />,
    );
    await screen.findByText("How can I help?");
    await user.click(screen.getByRole("button", { name: "Prepare action" }));

    expect(
      screen.getByRole("option", { name: "Add client resource" }),
    ).toBeVisible();
    expect(
      screen.getByRole("option", { name: "Look up client resources" }),
    ).toBeVisible();
    await user.selectOptions(
      screen.getByLabelText("Action"),
      "client_resource_add",
    );
    expect(screen.getByLabelText("Resource kind")).toHaveValue("contact");
    expect(
      screen.queryByRole("option", { name: "Location" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("option", { name: "Service" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("option", { name: "Contract" }),
    ).not.toBeInTheDocument();

    await user.selectOptions(screen.getByLabelText("Resource kind"), "contact");
    expect(screen.getByLabelText("Location")).toHaveDisplayValue("No location");
    expect(screen.getByLabelText("Display name")).toBeRequired();
    expect(screen.getByLabelText("Email")).not.toBeRequired();
    expect(screen.getByLabelText("Phone")).not.toBeRequired();
  });

  it("loads canonical Locations and proposes Client-resource business fields without trusted metadata", async () => {
    const user = userEvent.setup();
    const workspaceAPI = api() as AIWorkspaceAPI & {
      runRead: ReturnType<typeof vi.fn>;
    };
    workspaceAPI.runRead = vi.fn(async (_conversationID, _toolName, input) => ({
      summary: "Listed client resources",
      data: {
        resources:
          input.client === "CLIENT-002"
            ? [
                {
                  id: "location-2",
                  kind: "location",
                  display_id: "LOC-2",
                  name: "Contoso HQ",
                  version: 4,
                },
              ]
            : [],
      },
    }));
    const contactProposal: AIWorkspaceProposal = {
      ...proposal,
      target_client_id: "client-2",
      tool_name: "contact.create",
      preview: {
        summary: "Create contact Ada Lovelace",
        target_type: "contact",
        target_id: "contact-1",
        target_version: 0,
        location_target: {
          id: "location-2",
          client_id: "client-2",
          kind: "location",
          display_id: "LOC-2",
          name: "Contoso HQ",
        },
        changes: {
          display_id: { before: null, after: "CON-1" },
          display_name: { before: null, after: "Ada Lovelace" },
          location: { before: null, after: "LOC-2" },
          lifecycle_state: { before: null, after: "active" },
        },
      },
    };
    workspaceAPI.propose = vi.fn(async () => contactProposal);
    render(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-1"
        capabilities={new Set(["search.read", "contact.create"])}
        api={workspaceAPI}
        onClose={() => undefined}
      />,
    );
    await screen.findByText("How can I help?");
    await user.click(screen.getByRole("button", { name: "Prepare action" }));
    await user.selectOptions(
      screen.getByLabelText("Action"),
      "client_resource_add",
    );
    await user.selectOptions(
      screen.getByLabelText("Target client"),
      "client-2",
    );
    await screen.findByRole("option", { name: "Contoso HQ (LOC-2)" });
    await user.type(screen.getByLabelText("Display ID"), "CON-1");
    await user.type(screen.getByLabelText("Display name"), "Ada Lovelace");
    await user.selectOptions(screen.getByLabelText("Location"), "LOC-2");
    await user.click(screen.getByRole("button", { name: "Review action" }));

    expect(workspaceAPI.propose).toHaveBeenCalledWith(
      "conversation-1",
      "contact.create",
      {
        client: "CLIENT-002",
        display_id: "CON-1",
        display_name: "Ada Lovelace",
        location: "LOC-2",
      },
    );
    expect(await screen.findByText("Contoso HQ (LOC-2)")).toBeVisible();
    expect(
      screen.getByText("Contoso Manufacturing (CLIENT-002)"),
    ).toBeVisible();
    await user.click(
      screen.getByRole("button", {
        name: "Confirm Create contact Ada Lovelace",
      }),
    );
    expect(workspaceAPI.confirm).toHaveBeenCalledWith("proposal-1", 1);
    expect(await screen.findByText("Updated INC-1")).toBeVisible();
  });

  it("lists typed Client resources without changing the page Client", async () => {
    const user = userEvent.setup();
    const workspaceAPI = api() as AIWorkspaceAPI & {
      runRead: ReturnType<typeof vi.fn>;
    };
    workspaceAPI.runRead = vi.fn(async () => ({
      summary: "Listed client resources",
      data: {
        resources: [
          {
            id: "asset-2",
            kind: "asset",
            display_id: "AST-2",
            name: "Firewall",
            detail: "network_device",
            version: 1,
          },
        ],
      },
    }));
    render(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-1"
        capabilities={new Set(["search.read"])}
        api={workspaceAPI}
        onClose={() => undefined}
      />,
    );
    await screen.findByText("How can I help?");
    await user.click(screen.getByRole("button", { name: "Prepare action" }));
    await user.selectOptions(
      screen.getByLabelText("Action"),
      "client_resource_list",
    );
    await user.selectOptions(
      screen.getByLabelText("Target client"),
      "client-2",
    );
    await user.selectOptions(screen.getByLabelText("Resource kind"), "asset");
    await user.click(screen.getByRole("button", { name: "Look up resources" }));

    expect(workspaceAPI.runRead).toHaveBeenCalledWith(
      "conversation-1",
      "client_resource.list",
      { client: "CLIENT-002", kind: "asset" },
      expect.any(AbortSignal),
    );
    expect(await screen.findByText("Firewall")).toBeVisible();
    expect(screen.getByText("AST-2 · network_device")).toBeVisible();
    expect(
      within(screen.getByLabelText("Client resource results")).getByText(
        "Contoso Manufacturing (CLIENT-002) · Assets",
      ),
    ).toBeVisible();
  });

  it("does not create a conversation or issue another Location read after the drawer closes", async () => {
    const user = userEvent.setup();
    const workspaceAPI = api() as AIWorkspaceAPI & {
      create: ReturnType<typeof vi.fn>;
      runRead: ReturnType<typeof vi.fn>;
    };
    const view = render(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-1"
        capabilities={new Set(["search.read", "contact.create"])}
        api={workspaceAPI}
        onClose={() => undefined}
      />,
    );
    await screen.findByText("How can I help?");
    await user.click(screen.getByRole("button", { name: "Prepare action" }));
    await user.selectOptions(
      screen.getByLabelText("Action"),
      "client_resource_add",
    );
    await waitFor(() => expect(workspaceAPI.runRead).toHaveBeenCalledTimes(1));
    workspaceAPI.create.mockClear();
    workspaceAPI.runRead.mockClear();

    view.rerender(
      <AIWorkspace
        open={false}
        clients={clients}
        pageClientID="client-1"
        capabilities={new Set(["search.read", "contact.create"])}
        api={workspaceAPI}
        onClose={() => undefined}
      />,
    );

    expect(screen.queryByRole("dialog", { name: "AI workspace" })).toBeNull();
    expect(workspaceAPI.create).not.toHaveBeenCalled();
    expect(workspaceAPI.runRead).not.toHaveBeenCalled();
  });

  it("does not use a same-display-ID Location loaded for another Client to confirm a proposal", async () => {
    const user = userEvent.setup();
    let resolveContosoLocations:
      | ((value: { summary: string; data: { resources: unknown[] } }) => void)
      | undefined;
    const contosoLocations = new Promise<{
      summary: string;
      data: { resources: unknown[] };
    }>((resolve) => {
      resolveContosoLocations = resolve;
    });
    const workspaceAPI = api();
    workspaceAPI.runRead = vi.fn(async (_conversationID, _toolName, input) => {
      if (input.client === "CLIENT-002") return contosoLocations;
      return {
        summary: "Listed client resources",
        data: {
          resources: [
            {
              id: "northwind-location-1",
              kind: "location",
              display_id: "LOC-SHARED",
              name: "Northwind HQ",
              version: 1,
            },
          ],
        },
      };
    });
    workspaceAPI.propose = vi.fn(async () => ({
      ...proposal,
      target_client_id: "client-2",
      tool_name: "contact.create",
      preview: {
        summary: "Create contact Ada Lovelace",
        target_type: "contact",
        target_id: "contact-1",
        target_version: 0,
        changes: {
          display_name: { before: null, after: "Ada Lovelace" },
          location: { before: null, after: "LOC-SHARED" },
        },
      },
    }));
    render(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-1"
        capabilities={new Set(["search.read", "contact.create"])}
        api={workspaceAPI}
        onClose={() => undefined}
      />,
    );
    await screen.findByText("How can I help?");
    await user.click(screen.getByRole("button", { name: "Prepare action" }));
    await user.selectOptions(
      screen.getByLabelText("Action"),
      "client_resource_add",
    );
    await screen.findByRole("option", { name: "Northwind HQ (LOC-SHARED)" });
    await user.selectOptions(
      screen.getByLabelText("Target client"),
      "client-2",
    );
    await user.type(screen.getByLabelText("Display ID"), "CON-1");
    await user.type(screen.getByLabelText("Display name"), "Ada Lovelace");
    await user.click(screen.getByRole("button", { name: "Review action" }));

    expect(
      await screen.findByRole("alert", { name: "Target location unavailable" }),
    ).toBeVisible();
    expect(
      screen.getByRole("button", {
        name: "Confirm Create contact Ada Lovelace",
      }),
    ).toBeDisabled();
    resolveContosoLocations?.({
      summary: "Listed client resources",
      data: { resources: [] },
    });
  });

  it("does not display a late lookup after its Client or kind selection changes", async () => {
    const user = userEvent.setup();
    let resolveAssets:
      | ((value: { summary: string; data: { resources: unknown[] } }) => void)
      | undefined;
    const assets = new Promise<{
      summary: string;
      data: { resources: unknown[] };
    }>((resolve) => {
      resolveAssets = resolve;
    });
    const workspaceAPI = api();
    workspaceAPI.runRead = vi.fn(async (_conversationID, _toolName, input) => {
      if (input.client === "CLIENT-001" && input.kind === "asset")
        return assets;
      return { summary: "Listed client resources", data: { resources: [] } };
    });
    render(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-1"
        capabilities={new Set(["search.read"])}
        api={workspaceAPI}
        onClose={() => undefined}
      />,
    );
    await screen.findByText("How can I help?");
    await user.click(screen.getByRole("button", { name: "Prepare action" }));
    await user.selectOptions(
      screen.getByLabelText("Action"),
      "client_resource_list",
    );
    await user.selectOptions(screen.getByLabelText("Resource kind"), "asset");
    await user.click(screen.getByRole("button", { name: "Look up resources" }));
    await waitFor(() =>
      expect(workspaceAPI.runRead).toHaveBeenCalledWith(
        "conversation-1",
        "client_resource.list",
        { client: "CLIENT-001", kind: "asset" },
        expect.any(AbortSignal),
      ),
    );

    await user.selectOptions(
      screen.getByLabelText("Target client"),
      "client-2",
    );
    await user.selectOptions(screen.getByLabelText("Resource kind"), "service");
    resolveAssets?.({
      summary: "Listed client resources",
      data: {
        resources: [
          {
            id: "asset-1",
            kind: "asset",
            display_id: "AST-1",
            name: "Late firewall",
            version: 1,
          },
        ],
      },
    });

    await waitFor(() =>
      expect(screen.queryByText("Late firewall")).not.toBeInTheDocument(),
    );
    expect(screen.queryByLabelText("Client resource results")).toBeNull();
  });

  it("does not submit a lookup captured before conversation creation after selectors change", async () => {
    const user = userEvent.setup();
    let resolveConversation:
      ((value: AIWorkspaceConversation) => void) | undefined;
    const createdConversation = new Promise<AIWorkspaceConversation>(
      (resolve) => {
        resolveConversation = resolve;
      },
    );
    const workspaceAPI = api() as AIWorkspaceAPI & {
      create: ReturnType<typeof vi.fn>;
      runRead: ReturnType<typeof vi.fn>;
    };
    workspaceAPI.list = vi.fn(async () => []);
    workspaceAPI.create = vi.fn(async () => createdConversation);
    workspaceAPI.runRead = vi.fn(async () => ({
      summary: "Listed client resources",
      data: {
        resources: [
          {
            id: "asset-1",
            kind: "asset",
            display_id: "AST-1",
            name: "Stale firewall",
            version: 1,
          },
        ],
      },
    }));
    render(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-1"
        capabilities={new Set(["search.read"])}
        api={workspaceAPI}
        onClose={() => undefined}
      />,
    );
    await screen.findByText("Ask about RTI or prepare an action");
    await user.click(screen.getByRole("button", { name: "Prepare action" }));
    await user.selectOptions(
      screen.getByLabelText("Action"),
      "client_resource_list",
    );
    await user.selectOptions(screen.getByLabelText("Resource kind"), "asset");
    await user.click(screen.getByRole("button", { name: "Look up resources" }));
    await waitFor(() => expect(workspaceAPI.create).toHaveBeenCalledTimes(1));

    await user.selectOptions(
      screen.getByLabelText("Target client"),
      "client-2",
    );
    await user.selectOptions(screen.getByLabelText("Resource kind"), "service");
    resolveConversation?.(conversation);

    await waitFor(() => expect(screen.queryByRole("status")).toBeNull());
    expect(workspaceAPI.runRead).not.toHaveBeenCalled();
    expect(screen.queryByText("Stale firewall")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Client resource results")).toBeNull();
  });

  it.each([
    [
      "location",
      { client: "CLIENT-001", display_id: "LOC-1", name: "Head Office" },
    ],
    [
      "contact",
      {
        client: "CLIENT-001",
        display_id: "CON-1",
        display_name: "Ada Lovelace",
      },
    ],
    [
      "asset",
      {
        client: "CLIENT-001",
        display_id: "AST-1",
        name: "Firewall",
        asset_type: "network_device",
      },
    ],
    [
      "service",
      { client: "CLIENT-001", display_id: "SVC-1", name: "Managed backup" },
    ],
    [
      "contract",
      {
        client: "CLIENT-001",
        display_id: "CTR-1",
        name: "Support agreement",
        starts_on: "2026-08-05",
      },
    ],
  ] as const)("prepares the exact %s create proposal", async (kind, input) => {
    const user = userEvent.setup();
    const workspaceAPI = api();
    workspaceAPI.propose = vi.fn(async () => ({
      ...proposal,
      tool_name: `${kind}.create`,
      preview: {
        summary: `Create ${kind} ${input.display_id}`,
        target_type: kind,
        target_id: `${kind}-1`,
        target_version: 0,
        changes: {
          display_id: { before: null, after: input.display_id },
          lifecycle_state: { before: null, after: "active" },
        },
      },
    }));
    render(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-1"
        capabilities={
          new Set([
            "location.create",
            "contact.create",
            "asset.create",
            "service.create",
            "contract.create",
          ])
        }
        api={workspaceAPI}
        onClose={() => undefined}
      />,
    );
    await screen.findByText("How can I help?");
    await user.click(screen.getByRole("button", { name: "Prepare action" }));
    await user.selectOptions(
      screen.getByLabelText("Action"),
      "client_resource_add",
    );
    await user.selectOptions(screen.getByLabelText("Resource kind"), kind);
    await user.type(screen.getByLabelText("Display ID"), input.display_id);
    const name =
      kind === "contact"
        ? (input as { display_name: string }).display_name
        : (input as { name: string }).name;
    await user.type(
      screen.getByLabelText(kind === "contact" ? "Display name" : "Name"),
      name,
    );
    if (kind === "asset") {
      await user.type(screen.getByLabelText("Asset type"), input.asset_type);
    }
    if (kind === "contract") {
      await user.type(screen.getByLabelText("Starts on"), input.starts_on);
    }
    await user.click(screen.getByRole("button", { name: "Review action" }));

    expect(workspaceAPI.propose).toHaveBeenCalledWith(
      "conversation-1",
      `${kind}.create`,
      input,
    );
    expect(
      await screen.findByText(`Create ${kind} ${input.display_id}`),
    ).toBeVisible();
    expect(
      screen.getByRole("button", {
        name: `Confirm Create ${kind} ${input.display_id}`,
      }),
    ).toBeEnabled();
    if (kind === "location") {
      await user.click(screen.getByRole("button", { name: "Reject" }));
      expect(workspaceAPI.reject).toHaveBeenCalledWith("proposal-1", 1);
      expect(await screen.findByText("Action rejected.")).toBeVisible();
    }
  });

  it("renders canonical Client preview data as labeled plain English instead of serialized JSON", async () => {
    const user = userEvent.setup();
    const workspaceAPI = api();
    workspaceAPI.propose = vi.fn(async () => ({
      ...proposal,
      target_client_id: "client-1",
      tool_name: "location.create",
      required_capability: "location.create",
      preview: {
        summary: "Create location Acceptance Test Location",
        target_type: "location",
        target_id: "location-acceptance",
        target_version: 0,
        changes: {
          client: {
            before: null,
            after: {
              display_id: "NORTHWIND",
              name: "Northwind Legal",
            },
          },
          display_id: { before: null, after: "ACCEPT-LOC-20260806" },
          name: { before: null, after: "Acceptance Test Location" },
        },
      },
    }));
    render(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-1"
        capabilities={new Set(["location.create"])}
        api={workspaceAPI}
        onClose={() => undefined}
      />,
    );
    await screen.findByText("How can I help?");
    await user.click(screen.getByRole("button", { name: "Prepare action" }));
    await user.selectOptions(
      screen.getByLabelText("Action"),
      "client_resource_add",
    );
    await user.type(screen.getByLabelText("Display ID"), "ACCEPT-LOC-20260806");
    await user.type(screen.getByLabelText("Name"), "Acceptance Test Location");
    await user.click(screen.getByRole("button", { name: "Review action" }));

    const preview = await screen.findByRole("region", {
      name: "AI action preview",
    });
    const clientDetails = within(preview).getByRole("group", {
      name: "Client details",
    });
    expect(within(clientDetails).getByText("Display ID")).toBeVisible();
    expect(within(clientDetails).getByText("NORTHWIND")).toBeVisible();
    expect(within(clientDetails).getByText("Name")).toBeVisible();
    expect(within(clientDetails).getByText("Northwind Legal")).toBeVisible();
    expect(preview).not.toHaveTextContent(
      '{"display_id":"NORTHWIND","name":"Northwind Legal"}',
    );
  });

  it("renders second-wave effects as semantic definition lists without raw JSON", () => {
    const secondWaveProposal: AIWorkspaceProposal = {
      ...proposal,
      target_client_id: "client-1",
      tool_name: "ticket.create",
      required_capability: "work_record.create",
      preview: {
        summary: "Create incident INC-2042 for Northwind Legal",
        target_type: "work_record",
        target_id: "ticket-new",
        changes: {
          queue: { before: null, after: { name: "Service Desk" } },
          workflow: {
            before: null,
            after: { name: "Incident workflow", version: 8 },
          },
          sla_policy: {
            before: null,
            after: { name: "Gold SLA", response: "30 minutes" },
          },
          owner: {
            before: "Unassigned",
            after: { display_name: "Avery Chen", email: "avery@example.com" },
          },
          stage: {
            before: { name: "Discovery" },
            after: { name: "Qualified" },
          },
          proposal_draft: {
            before: null,
            after: {
              display_id: "PROP-2042",
              opportunity_display_id: "OPP-2042",
              state: "draft",
            },
          },
          publication_impact: {
            before: "Internal draft",
            after: {
              state: "published",
              delivery: "Internal only; no external delivery",
            },
          },
        },
      },
    };
    const { container } = render(
      <AIProposal
        proposal={secondWaveProposal}
        targetClient={clients[0]}
        busy={false}
        onConfirm={() => undefined}
        onReject={() => undefined}
      />,
    );

    const preview = screen.getByRole("region", { name: "AI action preview" });
    expect(
      container.querySelector("dl.ai-proposal__changes"),
    ).toBeInTheDocument();
    for (const label of [
      "queue",
      "workflow",
      "sla policy",
      "owner",
      "stage",
      "proposal draft",
      "publication impact",
    ]) {
      expect(within(preview).getByText(label)).toBeVisible();
    }
    expect(preview).toHaveTextContent("Avery Chen");
    expect(preview).toHaveTextContent("Qualified");
    expect(preview).toHaveTextContent("OPP-2042");
    expect(preview).toHaveTextContent("Internal only; no external delivery");
    expect(preview).not.toHaveTextContent("[object Object]");
    expect(preview).not.toHaveTextContent(
      '{"name":"Incident workflow","version":8}',
    );
  });

  it.each([
    ["location", "name", "Name", "Branch HQ", { name: "Branch HQ" }],
    ["contact", "clear", "Clear email", "", { email: "" }],
    ["asset", "asset_type", "Asset type", "server", { asset_type: "server" }],
    ["service", "criticality", "Criticality", "high", { criticality: "high" }],
    ["contract", "clear", "Clear end date", "", { clear_ends_on: true }],
  ] as const)(
    "prepares a closed %s update with only approved public business input",
    async (kind, interaction, fieldLabel, value, patch) => {
      const user = userEvent.setup();
      const workspaceAPI = api();
      workspaceAPI.propose = vi.fn(async () => ({
        ...proposal,
        target_client_id: "client-2",
        tool_name: `${kind}.update`,
        preview: {
          summary: `Update ${kind} RESOURCE-2`,
          target_type: kind,
          target_id: `${kind}-2`,
          target_version: 7,
          changes: {
            reason: { before: null, after: "Correct business record" },
          },
        },
      }));
      render(
        <AIWorkspace
          open
          clients={clients}
          pageClientID="client-1"
          capabilities={new Set([`${kind}.update`])}
          api={workspaceAPI}
          onClose={() => undefined}
        />,
      );
      await screen.findByText("How can I help?");
      await user.click(screen.getByRole("button", { name: "Prepare action" }));
      await user.selectOptions(
        screen.getByLabelText("Action"),
        "client_resource_update",
      );
      expect(screen.getByLabelText("Resource kind")).toHaveValue(kind);
      await user.selectOptions(
        screen.getByLabelText("Target client"),
        "client-2",
      );
      await user.type(screen.getByLabelText("Resource"), "RESOURCE-2");
      if (interaction === "clear") {
        await user.click(screen.getByLabelText(fieldLabel));
      } else if (interaction === "criticality") {
        await user.selectOptions(screen.getByLabelText(fieldLabel), value);
      } else {
        await user.type(screen.getByLabelText(fieldLabel), value);
      }
      await user.type(
        screen.getByLabelText("Reason"),
        "Correct business record",
      );
      await user.click(screen.getByRole("button", { name: "Review action" }));

      expect(workspaceAPI.propose).toHaveBeenCalledWith(
        "conversation-1",
        `${kind}.update`,
        {
          client: "CLIENT-002",
          resource: "RESOURCE-2",
          patch,
          reason: "Correct business record",
        },
      );
      expect(
        await screen.findByText(`Update ${kind} RESOURCE-2`),
      ).toBeVisible();
    },
  );

  it.each(["location", "contact", "asset", "service", "contract"] as const)(
    "prepares exact %s deactivate and exposes reactivate mode",
    async (kind) => {
      const user = userEvent.setup();
      const workspaceAPI = api();
      workspaceAPI.propose = vi.fn(async () => ({
        ...proposal,
        target_client_id: "client-2",
        tool_name: `${kind}.deactivate`,
        preview: {
          summary: `Deactivate ${kind} RESOURCE-2`,
          target_type: kind,
          target_id: `${kind}-2`,
          target_version: 4,
          changes: { lifecycle_state: { before: "active", after: "inactive" } },
        },
      }));
      render(
        <AIWorkspace
          open
          clients={clients}
          pageClientID="client-1"
          capabilities={new Set([`${kind}.lifecycle`])}
          api={workspaceAPI}
          onClose={() => undefined}
        />,
      );
      await screen.findByText("How can I help?");
      await user.click(screen.getByRole("button", { name: "Prepare action" }));
      expect(
        screen.getByRole("option", { name: "Reactivate client resource" }),
      ).toBeVisible();
      await user.selectOptions(
        screen.getByLabelText("Action"),
        "client_resource_deactivate",
      );
      expect(screen.getByLabelText("Resource kind")).toHaveValue(kind);
      await user.selectOptions(
        screen.getByLabelText("Target client"),
        "client-2",
      );
      await user.type(screen.getByLabelText("Resource"), "RESOURCE-2");
      await user.type(screen.getByLabelText("Reason"), "No longer used");
      await user.click(screen.getByRole("button", { name: "Review action" }));

      expect(workspaceAPI.propose).toHaveBeenCalledWith(
        "conversation-1",
        `${kind}.deactivate`,
        {
          client: "CLIENT-002",
          resource: "RESOURCE-2",
          reason: "No longer used",
        },
      );
    },
  );

  it("refreshes the ordinary catalog after a confirmed AI resource mutation", async () => {
    const user = userEvent.setup();
    const workspaceAPI = api();
    const refresh = vi.fn(async () => undefined);
    workspaceAPI.propose = vi.fn(async () => ({
      ...proposal,
      target_client_id: "client-2",
      tool_name: "location.deactivate",
      preview: {
        summary: "Deactivate location LOC-2",
        target_type: "location",
        target_id: "location-2",
        target_version: 4,
        changes: { lifecycle_state: { before: "active", after: "inactive" } },
      },
    }));
    render(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-1"
        capabilities={new Set(["location.lifecycle"])}
        api={workspaceAPI}
        onClientResourcesChanged={refresh}
        onClose={() => undefined}
      />,
    );
    await screen.findByText("How can I help?");
    await user.click(screen.getByRole("button", { name: "Prepare action" }));
    await user.selectOptions(
      screen.getByLabelText("Action"),
      "client_resource_deactivate",
    );
    await user.type(screen.getByLabelText("Resource"), "LOC-2");
    await user.type(screen.getByLabelText("Reason"), "Office closed");
    await user.click(screen.getByRole("button", { name: "Review action" }));
    await user.click(
      await screen.findByRole("button", {
        name: "Confirm Deactivate location LOC-2",
      }),
    );
    await waitFor(() => expect(refresh).toHaveBeenCalledTimes(1));
  });

  it("uses defined design-system tokens for an opaque drawer surface", () => {
    expect(workspaceStyles).toContain(
      "background: var(--rti-surface-overlay);",
    );
    expect(workspaceStyles).toContain(".ai-workspace__structured-data > div");
    expect(workspaceStyles).toContain("background: var(--rti-surface-panel);");
    expect(workspaceStyles).toContain("overflow-wrap: anywhere;");
    expect(workspaceStyles).not.toMatch(
      /var\(--rti-(?:surface|text|border|accent|radius-card|danger|success)\)/,
    );
  });

  it("keeps long nested proposal values inside the drawer at narrow widths", () => {
    expect(workspaceStyles).toMatch(
      /\.ai-proposal__nested-data,\s*\.ai-proposal__nested-list\s*\{[^}]*min-width:\s*0;/s,
    );
    expect(workspaceStyles).toMatch(
      /\.ai-proposal__nested-data dd,\s*\.ai-proposal__nested-list li\s*\{[^}]*min-width:\s*0;[^}]*overflow-wrap:\s*anywhere;/s,
    );
    expect(workspaceStyles).toMatch(
      /@media \(max-width:\s*620px\)[\s\S]*?\.ai-proposal__nested-data > div[\s\S]*?grid-template-columns:\s*1fr;/,
    );
    expect(workspaceStyles).toMatch(
      /@media \(min-width:\s*1200px\)[\s\S]*?\.ai-workspace\s*\{[^}]*width:\s*min\(48rem,\s*100vw\);/,
    );
    expect(workspaceStyles).toMatch(
      /@media \(max-width:\s*620px\)[\s\S]*?\.ai-workspace__action-grid--wide[\s\S]*?grid-template-columns:\s*1fr;/,
    );
  });

  it("stays available, sends guidance, and confirms an exact ticket preview", async () => {
    const user = userEvent.setup();
    const workspaceAPI = api();
    const view = render(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-1"
        api={workspaceAPI}
        onClose={() => undefined}
      />,
    );

    expect(
      await screen.findByRole("dialog", { name: "AI workspace" }),
    ).toBeVisible();
    expect(screen.getByText("All authorized clients.")).toBeVisible();
    expect(await screen.findByText("How can I help?")).toBeVisible();

    await user.type(
      screen.getByRole("textbox", { name: "Ask Rarity AI" }),
      "How do queues work?",
    );
    await user.click(screen.getByRole("button", { name: "Send" }));
    expect(
      await screen.findByText("Open Work, then choose the queue."),
    ).toBeVisible();

    await user.click(screen.getByRole("button", { name: "Prepare action" }));
    expect(screen.getByRole("combobox", { name: "Target client" })).toHaveValue(
      "client-1",
    );
    expect(
      screen.getByRole("combobox", { name: "Target client" }),
    ).toBeRequired();
    await user.selectOptions(
      screen.getByRole("combobox", { name: "Target client" }),
      "client-2",
    );
    await user.type(
      screen.getByRole("textbox", { name: "Ticket ID" }),
      "ticket-1",
    );
    await user.type(
      screen.getByRole("spinbutton", { name: "Current version" }),
      "3",
    );
    await user.type(
      screen.getByRole("textbox", { name: "New value" }),
      "resolved",
    );
    await user.type(
      screen.getByRole("textbox", { name: "Reason" }),
      "Issue fixed",
    );
    await user.click(screen.getByRole("button", { name: "Review action" }));

    expect(workspaceAPI.propose).toHaveBeenCalledWith(
      "conversation-1",
      "ticket.transition",
      {
        client_id: "client-2",
        id: "ticket-1",
        expected_version: 3,
        status: "resolved",
        reason: "Issue fixed",
      },
    );

    expect(
      await screen.findByText("Change INC-1 status to resolved"),
    ).toBeVisible();
    expect(
      screen.getByText("Contoso Manufacturing (CLIENT-002)"),
    ).toBeVisible();
    expect(screen.getByText("open")).toBeVisible();
    expect(screen.getByText("resolved")).toBeVisible();
    const confirm = screen.getByRole("button", {
      name: "Confirm Change INC-1 status to resolved",
    });
    expect(confirm).toBeEnabled();
    await user.click(confirm);
    expect(await screen.findByText("Updated INC-1")).toBeVisible();
    expect(workspaceAPI.confirm).toHaveBeenCalledWith("proposal-1", 1);

    view.rerender(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-1"
        api={workspaceAPI}
        onClose={() => undefined}
      />,
    );
    expect(screen.getByRole("dialog", { name: "AI workspace" })).toBeVisible();
  });

  it("updates the page-derived target without reloading the global conversation", async () => {
    const user = userEvent.setup();
    const workspaceAPI = api();
    const view = render(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-1"
        api={workspaceAPI}
        onClose={() => undefined}
      />,
    );
    await screen.findByText("How can I help?");
    await user.click(screen.getByRole("button", { name: "Prepare action" }));
    expect(screen.getByRole("combobox", { name: "Target client" })).toHaveValue(
      "client-1",
    );
    expect(workspaceAPI.list).toHaveBeenCalledTimes(1);
    expect(workspaceAPI.get).toHaveBeenCalledTimes(1);
    view.rerender(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-2"
        api={workspaceAPI}
        onClose={() => undefined}
      />,
    );
    expect(screen.getByRole("combobox", { name: "Target client" })).toHaveValue(
      "client-2",
    );
    expect(screen.getByText("How can I help?")).toBeVisible();
    expect(workspaceAPI.list).toHaveBeenCalledTimes(1);
    expect(workspaceAPI.get).toHaveBeenCalledTimes(1);
  });

  it("preserves an explicit target when the page filter changes without reloading conversation", async () => {
    const user = userEvent.setup();
    const workspaceAPI = api();
    const view = render(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-1"
        api={workspaceAPI}
        onClose={() => undefined}
      />,
    );
    await screen.findByText("How can I help?");
    await user.click(screen.getByRole("button", { name: "Prepare action" }));
    await user.selectOptions(
      screen.getByRole("combobox", { name: "Target client" }),
      "client-2",
    );

    view.rerender(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-3"
        api={workspaceAPI}
        onClose={() => undefined}
      />,
    );

    expect(screen.getByRole("combobox", { name: "Target client" })).toHaveValue(
      "client-2",
    );
    expect(screen.getByText("How can I help?")).toBeVisible();
    expect(workspaceAPI.list).toHaveBeenCalledTimes(1);
    expect(workspaceAPI.get).toHaveBeenCalledTimes(1);
  });

  it("prepares a project using only the title and tasks supplied by the user", async () => {
    const user = userEvent.setup();
    const workspaceAPI = api();
    render(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-1"
        api={workspaceAPI}
        onClose={() => undefined}
      />,
    );
    await screen.findByText("How can I help?");

    await user.click(screen.getByRole("button", { name: "Prepare action" }));
    expect(screen.getByRole("combobox", { name: "Target client" })).toHaveValue(
      "client-1",
    );
    await user.selectOptions(
      screen.getByRole("combobox", { name: "Action" }),
      "project",
    );
    await user.type(
      screen.getByRole("textbox", { name: "Project title" }),
      "Onboarding",
    );
    await user.type(
      screen.getByRole("textbox", { name: "Project tasks" }),
      "A\nB\nC",
    );
    await user.click(screen.getByRole("button", { name: "Review action" }));

    expect(workspaceAPI.propose).toHaveBeenCalledWith(
      "conversation-1",
      "project.create",
      {
        client_id: "client-1",
        name: "Onboarding",
        tasks: ["A", "B", "C"],
      },
    );
  });

  it("shows an exact proposal returned directly from a chat command", async () => {
    const user = userEvent.setup();
    const workspaceAPI = api();
    const projectProposal: AIWorkspaceProposal = {
      ...proposal,
      tool_name: "project.create",
      target_client_id: "client-2",
      preview: {
        summary: "Create project Onboarding with 3 tasks",
        target_type: "project",
        target_id: "project-1",
        changes: {
          tasks: { before: [], after: ["A", "B", "C"] },
        },
      },
      required_capability: "project.create",
    };
    workspaceAPI.sendMessage = vi.fn(async (conversationID, text) => ({
      user_message: {
        id: "message-2",
        conversation_id: conversationID,
        role: "user" as const,
        text,
        created_at: "2026-08-04T12:01:00Z",
      },
      assistant_message: {
        id: "message-3",
        conversation_id: conversationID,
        role: "assistant" as const,
        text: "Review the exact project preview before confirming.",
        created_at: "2026-08-04T12:01:01Z",
      },
      proposal: projectProposal,
    }));
    render(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-1"
        api={workspaceAPI}
        onClose={() => undefined}
      />,
    );
    await screen.findByText("How can I help?");

    await user.type(
      screen.getByRole("textbox", { name: "Ask Rarity AI" }),
      "Create a project titled Onboarding for Contoso Manufacturing with tasks A, B, C",
    );
    await user.click(screen.getByRole("button", { name: "Send" }));

    expect(
      await screen.findByText("Create project Onboarding with 3 tasks"),
    ).toBeVisible();
    expect(
      screen.getByText("Contoso Manufacturing (CLIENT-002)"),
    ).toBeVisible();
  });

  it("confirms a canonical Location proposal with create capability alone", async () => {
    const user = userEvent.setup();
    const workspaceAPI = api();
    const contactProposal: AIWorkspaceProposal = {
      ...proposal,
      target_client_id: "client-2",
      tool_name: "contact.create",
      required_capability: "contact.create",
      preview: {
        summary: "Create contact Ada Lovelace",
        target_type: "contact",
        target_id: "contact-1",
        target_version: 0,
        location_target: {
          id: "location-2",
          client_id: "client-2",
          kind: "location",
          display_id: "LOC-2",
          name: "Contoso HQ",
        },
        changes: {
          display_id: { before: null, after: "CON-1" },
          display_name: { before: null, after: "Ada Lovelace" },
          location: { before: null, after: "LOC-2" },
          lifecycle_state: { before: null, after: "active" },
        },
      },
    };
    workspaceAPI.sendMessage = vi.fn(async (conversationID, text) => ({
      user_message: {
        id: "message-2",
        conversation_id: conversationID,
        role: "user" as const,
        text,
        created_at: "2026-08-04T12:01:00Z",
      },
      assistant_message: {
        id: "message-3",
        conversation_id: conversationID,
        role: "assistant" as const,
        text: "Review the exact contact preview before confirming.",
        created_at: "2026-08-04T12:01:01Z",
      },
      proposal: contactProposal,
    }));
    render(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-1"
        capabilities={new Set(["contact.create"])}
        api={workspaceAPI}
        onClose={() => undefined}
      />,
    );
    await screen.findByText("How can I help?");

    await user.type(
      screen.getByRole("textbox", { name: "Ask Rarity AI" }),
      "Create contact Ada Lovelace at LOC-2 for Contoso Manufacturing",
    );
    await user.click(screen.getByRole("button", { name: "Send" }));

    expect(await screen.findByText("Contoso HQ (LOC-2)")).toBeVisible();
    expect(workspaceAPI.runRead).not.toHaveBeenCalled();
    const confirm = screen.getByRole("button", {
      name: "Confirm Create contact Ada Lovelace",
    });
    expect(confirm).toBeEnabled();
    await user.click(confirm);
    expect(workspaceAPI.confirm).toHaveBeenCalledWith("proposal-1", 1);
  });

  it("fails closed when a proposal Location identity is not exact", async () => {
    const user = userEvent.setup();
    const workspaceAPI = api();
    const contactProposal: AIWorkspaceProposal = {
      ...proposal,
      target_client_id: "client-2",
      tool_name: "contact.create",
      required_capability: "contact.create",
      preview: {
        summary: "Create contact Ada Lovelace",
        target_type: "contact",
        target_id: "contact-1",
        target_version: 0,
        location_target: {
          id: "location-2",
          client_id: "client-3",
          kind: "location",
          display_id: "LOC-OTHER",
          name: "Wrong Location",
        },
        changes: {
          display_id: { before: null, after: "CON-1" },
          display_name: { before: null, after: "Ada Lovelace" },
          location: { before: null, after: "LOC-2" },
          lifecycle_state: { before: null, after: "active" },
        },
      },
    };
    workspaceAPI.sendMessage = vi.fn(async (conversationID, text) => ({
      user_message: {
        id: "message-2",
        conversation_id: conversationID,
        role: "user" as const,
        text,
        created_at: "2026-08-04T12:01:00Z",
      },
      assistant_message: {
        id: "message-3",
        conversation_id: conversationID,
        role: "assistant" as const,
        text: "Review the exact contact preview before confirming.",
        created_at: "2026-08-04T12:01:01Z",
      },
      proposal: contactProposal,
    }));
    render(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-1"
        capabilities={new Set(["contact.create"])}
        api={workspaceAPI}
        onClose={() => undefined}
      />,
    );
    await screen.findByText("How can I help?");

    await user.type(
      screen.getByRole("textbox", { name: "Ask Rarity AI" }),
      "Create contact Ada Lovelace at LOC-2 for Contoso Manufacturing",
    );
    await user.click(screen.getByRole("button", { name: "Send" }));

    expect(
      await screen.findByRole("alert", { name: "Target location unavailable" }),
    ).toBeVisible();
    expect(
      screen.getByRole("button", {
        name: "Confirm Create contact Ada Lovelace",
      }),
    ).toBeDisabled();
    expect(workspaceAPI.runRead).not.toHaveBeenCalled();
    expect(workspaceAPI.confirm).not.toHaveBeenCalled();
  });

  it("identifies a new Client from its exact creation preview", async () => {
    const user = userEvent.setup();
    const workspaceAPI = api();
    const clientProposal: AIWorkspaceProposal = {
      id: "proposal-client-1",
      tool_name: "client.create",
      tool_version: 1,
      preview: {
        summary: "Create client Cedar Grove",
        target_type: "client",
        target_id: "019fb3c2-0000-7000-8000-000000000501",
        changes: {
          name: { before: null, after: "Cedar Grove" },
          display_id: { before: null, after: "CLIENT-CEDAR" },
          lifecycle_state: { before: null, after: "active" },
        },
      },
      required_capability: "client.create",
      expires_at: "2026-08-04T12:10:00Z",
      state: "pending",
      version: 1,
    };
    workspaceAPI.sendMessage = vi.fn(async (conversationID, text) => ({
      user_message: {
        id: "message-2",
        conversation_id: conversationID,
        role: "user" as const,
        text,
        created_at: "2026-08-04T12:01:00Z",
      },
      assistant_message: {
        id: "message-3",
        conversation_id: conversationID,
        role: "assistant" as const,
        text: "Review the exact Client preview before confirming.",
        created_at: "2026-08-04T12:01:01Z",
      },
      proposal: clientProposal,
    }));
    render(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-1"
        api={workspaceAPI}
        onClose={() => undefined}
      />,
    );
    await screen.findByText("How can I help?");

    await user.type(
      screen.getByRole("textbox", { name: "Ask Rarity AI" }),
      "Create a client named Cedar Grove with display ID CLIENT-CEDAR",
    );
    await user.click(screen.getByRole("button", { name: "Send" }));

    expect(await screen.findByText("Create client Cedar Grove")).toBeVisible();
    expect(screen.getByText("New client")).toBeVisible();
    expect(screen.getByText("Cedar Grove (CLIENT-CEDAR)")).toBeVisible();
    expect(screen.queryByText("Target client")).not.toBeInTheDocument();
  });

  it("fails closed when a proposal target is absent from the authorized Client directory", async () => {
    const user = userEvent.setup();
    const workspaceAPI = api();
    const unresolvedProposal: AIWorkspaceProposal = {
      ...proposal,
      target_client_id: "client-not-loaded",
    };
    workspaceAPI.sendMessage = vi.fn(async (conversationID, text) => ({
      user_message: {
        id: "message-2",
        conversation_id: conversationID,
        role: "user" as const,
        text,
        created_at: "2026-08-04T12:01:00Z",
      },
      assistant_message: {
        id: "message-3",
        conversation_id: conversationID,
        role: "assistant" as const,
        text: "Review the exact ticket preview before confirming.",
        created_at: "2026-08-04T12:01:01Z",
      },
      proposal: unresolvedProposal,
    }));
    render(
      <AIWorkspace
        open
        clients={clients}
        pageClientID="client-1"
        api={workspaceAPI}
        onClose={() => undefined}
      />,
    );
    await screen.findByText("How can I help?");

    await user.type(
      screen.getByRole("textbox", { name: "Ask Rarity AI" }),
      "Resolve ticket INC-1 for the newly created client",
    );
    await user.click(screen.getByRole("button", { name: "Send" }));

    expect(
      await screen.findByText("Change INC-1 status to resolved"),
    ).toBeVisible();
    expect(screen.getByText("client-not-loaded")).toBeVisible();
    expect(
      screen.getByRole("alert", { name: "Target client unavailable" }),
    ).toBeVisible();
    expect(
      screen.getByRole("button", {
        name: "Confirm Change INC-1 status to resolved",
      }),
    ).toBeDisabled();
    expect(workspaceAPI.confirm).not.toHaveBeenCalled();
  });

  it("keeps chat and MSP-global Client creation available when no authorized Clients exist", async () => {
    const user = userEvent.setup();
    const workspaceAPI = api();
    render(
      <AIWorkspace
        open
        clients={[]}
        pageClientID=""
        api={workspaceAPI}
        onClose={() => undefined}
      />,
    );
    await screen.findByText("How can I help?");

    await user.click(screen.getByRole("button", { name: "Prepare action" }));

    expect(
      screen.getByText(
        "No authorized clients are available for structured actions. You can continue using chat.",
      ),
    ).toBeVisible();
    expect(
      screen.getByRole("combobox", { name: "Target client" }),
    ).toBeRequired();
    expect(
      screen.getByRole("combobox", { name: "Target client" }),
    ).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Review action" }),
    ).toBeDisabled();
    expect(
      screen.getByRole("textbox", { name: "Ask Rarity AI" }),
    ).toBeEnabled();
    expect(screen.getByRole("combobox", { name: "Action" })).toHaveTextContent(
      "Create client",
    );
  });
});
