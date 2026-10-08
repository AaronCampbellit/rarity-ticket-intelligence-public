import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import axe from "axe-core";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  App,
  workspaceItemFromHash,
  workspaceItemFromMentionLink,
} from "./App";
import { routeManifest } from "./app/routes";
import type {
  AIAssistAPI,
  AISettingsAPI,
  AIWorkspaceAPI,
  AIWorkspaceProposal,
} from "./features/ai/types";
import type { TeamsSettingsAPI } from "./features/teams/types";
import type { ClassificationAdminAPI } from "./features/classification/types";
import type { MentionDeepLink } from "./features/mentions/types";
import { navigation } from "./navigation";

const aiSettingsAPI: AISettingsAPI = {
  listConnections: vi.fn().mockResolvedValue([]),
  createConnection: vi.fn(),
  updateConnection: vi.fn(),
  setConnectionEnabled: vi.fn(),
  replaceCredential: vi.fn(),
  testConnection: vi.fn(),
  discoverModels: vi.fn(),
  listModels: vi.fn(),
  updateModels: vi.fn(),
  getPolicy: vi.fn().mockResolvedValue({
    enabled: false,
    providerDisclosureAccepted: false,
    promptVersion: "v1",
    allowedFeatures: [],
    summaryModelProfileId: "",
    replyDraftModelProfileId: "",
    similarSuggestionsModelProfileId: "",
    costLimitEnabled: true,
    allowUnmeteredUnknown: false,
    monthlyCostLimitMinor: 0,
    version: 1,
  }),
  updatePolicy: vi.fn(),
};

const aiAssistAPI: AIAssistAPI = {
  submit: vi.fn(),
  getJob: vi.fn(),
  cancel: vi.fn(),
  retry: vi.fn(),
  getRecommendation: vi.fn(),
  decide: vi.fn(),
};

const aiWorkspaceAPI: AIWorkspaceAPI = {
  list: vi.fn().mockResolvedValue([]),
  create: vi.fn(),
  get: vi.fn(),
  sendMessage: vi.fn(),
  propose: vi.fn(),
  runRead: vi.fn(),
  confirm: vi.fn(),
  reject: vi.fn(),
};
const teamsSettingsAPI: TeamsSettingsAPI = {
  list: vi.fn().mockResolvedValue([]),
  create: vi.fn(),
  updateName: vi.fn(),
  setEnabled: vi.fn(),
  replaceCredential: vi.fn(),
  test: vi.fn(),
};

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  window.sessionStorage.clear();
  window.localStorage.clear();
  window.history.replaceState(null, "", window.location.pathname);
});

beforeEach(() => {
  let notificationRead = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/api/v1/setup/status")) {
        return Response.json({
          completed: true,
          bootstrap_available: false,
          entra_available: false,
        });
      }
      if (url.endsWith("/api/v1/me")) {
        return Response.json({
          id: "technician-id",
          navigation: navigation.map(({ page }) => page),
          capabilities: [
            "work_record.read",
            "sales.read",
            "sales.write",
            "project.read",
            "project.write",
            "ai.assist",
          ],
        });
      }
      if (url.endsWith("/api/v1/directory")) {
        return Response.json({
          clients: [
            {
              id: "client-id",
              display_id: "CLIENT-001",
              name: "Northwind Legal",
            },
            {
              id: "client-2",
              display_id: "CLIENT-002",
              name: "Contoso Manufacturing",
            },
          ],
          departments: [],
          teams: [],
          queues: [],
        });
      }
      if (url.endsWith("/auth/session/refresh")) {
        return new Response(null, { status: 204 });
      }
      if (url.endsWith("/api/v1/notifications/unread-count")) {
        return Response.json({ count: notificationRead ? 0 : 1 });
      }
      if (url.startsWith("/api/v1/notifications?")) {
        return Response.json({
          notifications: [
            {
              id: "00000000-0000-4000-8000-000000000101",
              title: "Calendar schedule changed",
              body: "Northwind's on-site visit moved to 2:00 PM.",
              action_path: "#/home",
              content_classification: "internal",
              created_at: "2026-08-16T14:00:00Z",
              version: 3,
            },
          ],
        });
      }
      if (
        url.endsWith(
          "/api/v1/notifications/00000000-0000-4000-8000-000000000101/read",
        ) &&
        init?.method === "PATCH"
      ) {
        notificationRead = true;
        return Response.json({
          id: "00000000-0000-4000-8000-000000000101",
          title: "Calendar schedule changed",
          body: "Northwind's on-site visit moved to 2:00 PM.",
          action_path: "#/home",
          content_classification: "internal",
          created_at: "2026-08-16T14:00:00Z",
          read_at: "2026-08-16T14:05:00Z",
          version: 4,
        });
      }
      return new Response("not found", { status: 404 });
    }),
  );
});

describe("App", () => {
  it("exposes one notification center in the authenticated top bar and marks its calendar item read", async () => {
    window.location.hash = "#/home";
    const fetcher = vi.mocked(fetch);
    render(<App build={{ revision: "abc123" }} />);

    await screen.findByRole(
      "navigation",
      { name: "Primary" },
      { timeout: 10_000 },
    );

    const bell = await screen.findByRole("button", {
      name: "Notifications, 1 unread",
    });
    expect(bell).toBeVisible();
    expect(
      screen.getAllByRole("button", { name: /Notifications,/ }),
    ).toHaveLength(1);

    fireEvent.click(bell);
    const dialog = await screen.findByRole("dialog", {
      name: "Notifications",
    });
    const calendarItem = within(dialog)
      .getByRole("heading", { name: "Calendar schedule changed" })
      .closest("li");
    expect(calendarItem).not.toBeNull();
    fireEvent.click(
      within(calendarItem!).getByRole("button", {
        name: "Mark Calendar schedule changed as read",
      }),
    );

    await waitFor(() => expect(calendarItem).toHaveTextContent("Read"));
    const patchCall = fetcher.mock.calls.find(
      ([input, init]) =>
        String(input).endsWith(
          "/api/v1/notifications/00000000-0000-4000-8000-000000000101/read",
        ) && init?.method === "PATCH",
    );
    expect(patchCall).toBeDefined();
    expect(JSON.parse(String(patchCall?.[1]?.body))).toEqual({
      expected_version: 3,
    });
  });

  it("builds cross-client workspace records only from a valid authenticated mention link", () => {
    const link = {
      href: "#/project?parentID=project-1&mentionOccurrenceID=occurrence-1&sourceID=source-1",
      clientId: "client-2",
      parentType: "project",
      parentId: "project-1",
      sourceId: "source-1",
      tokenId: "token-1",
      sourceAvailable: true,
      itemVersion: 4,
    } satisfies MentionDeepLink & { tokenId: string };
    expect(
      workspaceItemFromMentionLink(link, new Set(["client-1", "client-2"])),
    ).toEqual({
      id: "project:project-1",
      routeID: "project",
      recordID: "project-1",
      clientID: "client-2",
      label: "Mentioned project",
      entityType: "project",
      openedAt: 0,
    });
    expect(
      workspaceItemFromMentionLink(
        { ...link, clientId: "client-forged" },
        new Set(["client-1", "client-2"]),
      ),
    ).toBeUndefined();
    const { tokenId: _tokenId, ...missingToken } = link;
    expect(
      workspaceItemFromMentionLink(
        missingToken,
        new Set(["client-1", "client-2"]),
      ),
    ).toBeUndefined();
    expect(
      workspaceItemFromMentionLink(
        {
          ...link,
          href: "#/work?parentID=task-1&mentionOccurrenceID=occurrence-2",
          clientId: "client-1",
          parentType: "task",
          parentId: "task-1",
          sourceId: undefined,
          tokenId: undefined,
          sourceAvailable: false,
        },
        new Set(["client-1", "client-2"]),
      ),
    ).toMatchObject({
      id: "task:task-1",
      routeID: "work",
      recordID: "task-1",
      clientID: "client-1",
      entityType: "task",
    });
    expect(
      workspaceItemFromMentionLink(
        { ...link, clientId: "" },
        new Set(["client-1", "client-2"]),
      ),
    ).toBeUndefined();
    expect(
      workspaceItemFromMentionLink(
        {
          ...link,
          href: "#/work?parentID=project-1&mentionOccurrenceID=occurrence-1",
        },
        new Set(["client-1", "client-2"]),
      ),
    ).toBeUndefined();
    expect(
      workspaceItemFromMentionLink(
        {
          ...link,
          href: "#/project?parentID=project-1&mentionOccurrenceID=occurrence-1",
          sourceAvailable: false,
        },
        new Set(["client-1", "client-2"]),
      ),
    ).toBeUndefined();
  });

  it("opens an authenticated mention in its resolved client without trusting widget metadata", async () => {
    window.location.hash = "#/home";
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input);
        if (url.endsWith("/api/v1/setup/status")) {
          return Response.json({ completed: true, entra_available: false });
        }
        if (url.endsWith("/api/v1/me")) {
          return Response.json({
            id: "technician-id",
            navigation: ["project"],
            capabilities: ["mention.read", "project.read"],
          });
        }
        if (url.endsWith("/api/v1/directory")) {
          return Response.json({
            clients: [
              { id: "client-1", display_id: "C-1", name: "First Client" },
              { id: "client-2", display_id: "C-2", name: "Second Client" },
            ],
            departments: [],
            teams: [],
            queues: [],
          });
        }
        if (url.endsWith("/auth/session/refresh")) {
          return new Response(null, { status: 204 });
        }
        if (url.startsWith("/api/v1/mentions/widget")) {
          return Response.json({
            counts: { unread: 1, read: 0, archived: 0 },
            items: [
              {
                id: "item-1",
                parent_type: "project",
                parent_id: "project-1",
                parent_display_id: "PRJ-204",
                parent_subject: "Datacenter move",
                latest_occurrence_id: "occurrence-1",
                author_label: "Mira Patel",
                origin: "team",
                state: "unread",
                last_mentioned_at: "2026-08-08T13:00:00Z",
                version: 3,
              },
            ],
          });
        }
        if (url === "/api/v1/mentions/occurrences/occurrence-1/resolve") {
          return Response.json({
            href: "#/project?parentID=project-1&mentionOccurrenceID=occurrence-1&sourceID=source-1",
            client_id: "client-2",
            parent_type: "project",
            parent_id: "project-1",
            source_id: "source-1",
            token_id: "token-1",
            source_available: true,
            item_version: 4,
          });
        }
        return new Response("not found", { status: 404 });
      }),
    );
    render(<App build={{ revision: "abc123" }} />);

    fireEvent.click(
      await screen.findByRole(
        "button",
        { name: "Open PRJ-204" },
        { timeout: 10_000 },
      ),
    );
    await waitFor(() => {
      expect(window.location.hash).toBe(
        "#/project?parentID=project-1&mentionOccurrenceID=occurrence-1&sourceID=source-1",
      );
      expect(
        screen.getByRole("combobox", { name: "Active client" }),
      ).toHaveValue("client-2");
      expect(
        screen.getByRole("navigation", { name: "Open records" }),
      ).toHaveTextContent("Mentioned project");
    });
  });

  it("does not disclose or navigate when resolver client metadata is missing", async () => {
    window.location.hash = "#/home";
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input);
        if (url.endsWith("/api/v1/setup/status"))
          return Response.json({ completed: true, entra_available: false });
        if (url.endsWith("/api/v1/me"))
          return Response.json({
            id: "technician-id",
            navigation: [],
            capabilities: ["mention.read"],
          });
        if (url.endsWith("/api/v1/directory"))
          return Response.json({
            clients: [],
            departments: [],
            teams: [],
            queues: [],
          });
        if (url.endsWith("/auth/session/refresh"))
          return new Response(null, { status: 204 });
        if (url.startsWith("/api/v1/mentions/widget"))
          return Response.json({
            counts: { unread: 1, read: 0, archived: 0 },
            items: [
              {
                id: "item-1",
                parent_type: "work_record",
                parent_id: "secret-work",
                parent_display_id: "RTY-1",
                parent_subject: "Hidden",
                latest_occurrence_id: "occurrence-1",
                author_label: "Mira",
                origin: "direct",
                state: "unread",
                last_mentioned_at: "2026-08-08T13:00:00Z",
                version: 1,
              },
            ],
          });
        if (url === "/api/v1/mentions/occurrences/occurrence-1/resolve")
          return Response.json({
            href: "#/work?parentID=secret-work&mentionOccurrenceID=occurrence-1",
            parent_type: "work_record",
            parent_id: "secret-work",
            source_available: false,
            item_version: 2,
          });
        return new Response("not found", { status: 404 });
      }),
    );
    render(<App build={{ revision: "abc123" }} />);

    fireEvent.click(
      await screen.findByRole(
        "button",
        { name: "Open RTY-1" },
        { timeout: 10_000 },
      ),
    );
    const alert = await screen.findByRole("alert");
    expect(window.location.hash).toBe("#/home");
    expect(alert).toHaveTextContent("could not be opened");
    expect(alert).not.toHaveTextContent("secret-work");
  });

  it("renders Classification settings only when the session grants its navigation route", async () => {
    window.location.hash = "#/classification-settings";
    render(<App build={{ revision: "abc123" }} />);

    expect(
      await screen.findByRole(
        "heading",
        { name: "Classification settings" },
        { timeout: 10_000 },
      ),
    ).toBeVisible();
    expect(
      screen.getByRole("link", { name: "Classification settings" }),
    ).toBeVisible();
  });

  it("does not expose Classification settings without the session navigation grant", async () => {
    const classificationAdminAPI = {
      catalog: vi.fn().mockResolvedValue({ groups: [], tags: [] }),
    } as unknown as ClassificationAdminAPI;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input);
        if (url.endsWith("/api/v1/setup/status")) {
          return Response.json({
            completed: true,
            bootstrap_available: false,
            entra_available: false,
          });
        }
        if (url.endsWith("/api/v1/me")) {
          return Response.json({
            id: "technician-id",
            navigation: ["work"],
            capabilities: ["work_record.read"],
          });
        }
        if (url.endsWith("/api/v1/directory"))
          return Response.json({
            clients: [],
            departments: [],
            teams: [],
            queues: [],
          });
        if (url.endsWith("/auth/session/refresh"))
          return new Response(null, { status: 204 });
        return new Response("not found", { status: 404 });
      }),
    );
    window.location.hash = "#/classification-settings";
    render(
      <App
        build={{ revision: "abc123" }}
        classificationAdminAPI={classificationAdminAPI}
      />,
    );

    await screen.findByRole("navigation", { name: "Primary" });
    expect(
      screen.queryByRole("link", { name: "Classification settings" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("heading", { name: "Classification settings" }),
    ).not.toBeInTheDocument();
    expect(classificationAdminAPI.catalog).not.toHaveBeenCalled();
  });

  it("restores typed project and task context from browser history hashes", () => {
    expect(
      workspaceItemFromHash(
        "#/project?recordType=project&recordID=project-1&clientID=client-1&label=Project",
      ),
    ).toEqual({
      id: "project:project-1",
      routeID: "project",
      recordID: "project-1",
      clientID: "client-1",
      label: "Project",
      entityType: "project",
      openedAt: 0,
    });
    expect(
      workspaceItemFromHash(
        "#/project?recordType=task&recordID=task-1&parentRecordID=project-1&clientID=client-id&label=Schedule+kickoff",
      ),
    ).toEqual({
      id: "task:task-1",
      routeID: "project",
      recordID: "task-1",
      parentRecordID: "project-1",
      clientID: "client-id",
      label: "Schedule kickoff",
      entityType: "task",
      openedAt: 0,
    });
    expect(
      workspaceItemFromHash(
        "#/project?recordType=task&recordID=task-1&clientID=client-id&label=Missing+parent",
      ),
    ).toEqual({
      id: "task:task-1",
      routeID: "project",
      recordID: "task-1",
      clientID: "client-id",
      label: "Missing parent",
      entityType: "task",
      openedAt: 0,
    });
  });

  it("renders the development design-system catalog when explicitly enabled", async () => {
    window.location.hash = "#/design-system";
    render(<App build={{ revision: "abc123" }} catalogEnabled />);

    expect(
      await screen.findByRole(
        "heading",
        { name: "Rarity design system" },
        { timeout: 10_000 },
      ),
    ).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "Presentation settings" }),
    ).not.toBeInTheDocument();
  });

  it("does not expose the design-system catalog when disabled", async () => {
    window.location.hash = "#/design-system";
    render(<App build={{ revision: "abc123" }} catalogEnabled={false} />);

    await screen.findByRole("navigation", { name: "Primary" });
    expect(
      screen.queryByRole("heading", { name: "Rarity design system" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "Design system" }),
    ).not.toBeInTheDocument();
  });

  it("places Sales and Projects in the shared operational shell", async () => {
    window.location.hash = "#/sales";
    render(<App build={{ revision: "abc123" }} />);

    expect(
      await screen.findByRole("navigation", { name: "Primary" }),
    ).toBeVisible();
    expect(
      await screen.findByRole("heading", {
        name: "Opportunities",
      }),
    ).toBeVisible();
    expect(screen.getByText("Build abc123")).toBeVisible();
  });

  it("uses the supplied Rarity brand assets across the shared shell", async () => {
    window.location.hash = "#/sales";
    render(<App build={{ revision: "abc123" }} />);

    await screen.findByRole("navigation", { name: "Primary" });
    expect(screen.getByRole("img", { name: "Rarity" })).toBeVisible();
    expect(
      screen.getByRole("img", { name: "Rarity symbol", hidden: true }),
    ).toBeInTheDocument();
  });

  it("provides active-session visibility and revocation navigation", async () => {
    window.location.hash = "#/sessions";
    render(<App build={{ revision: "abc123" }} />);

    expect(
      await screen.findByRole("heading", { name: "Active sessions" }),
    ).toBeVisible();
    expect(screen.getByRole("link", { name: "Sessions" })).toHaveAttribute(
      "aria-current",
      "page",
    );
  });

  it("places technician work in the shared navigation", async () => {
    window.location.hash = "#/work";
    render(<App build={{ revision: "abc123" }} />);

    await screen.findByRole("combobox", { name: "Active client" });
    expect(
      await screen.findByRole("heading", { name: "Technician work" }),
    ).toBeVisible();
    expect(screen.getByRole("link", { name: "Work" })).toHaveAttribute(
      "aria-current",
      "page",
    );
  });

  it("keeps a lower-role principal signed in on its permitted route when directory access is forbidden", async () => {
    window.location.hash = "#/sales";
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input);
        if (url.endsWith("/api/v1/setup/status")) {
          return new Response(
            JSON.stringify({ completed: true, entra_available: false }),
            { status: 200 },
          );
        }
        if (url.endsWith("/api/v1/me")) {
          return new Response(
            JSON.stringify({
              id: "technician-id",
              navigation: ["work"],
              capabilities: ["work_record.read"],
            }),
            { status: 200 },
          );
        }
        if (url.endsWith("/api/v1/directory")) {
          return new Response(
            JSON.stringify({
              error: {
                code: "forbidden",
                message: "action is not permitted",
              },
            }),
            { status: 403 },
          );
        }
        return new Response("not found", { status: 404 });
      }),
    );

    render(<App build={{ revision: "abc123" }} />);

    expect(
      await screen.findByRole("button", { name: "Sign out" }),
    ).toBeVisible();
    expect(screen.getByRole("link", { name: "Home" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(window.location.hash).toBe("#/home");
    expect(
      screen.queryByRole("link", { name: "Sales" }),
    ).not.toBeInTheDocument();

    window.location.hash = "#/sales";

    await waitFor(() => {
      expect(window.location.hash).toBe("#/home");
    });
    expect(
      await screen.findByRole("heading", { name: "Your operational home" }),
    ).toBeVisible();
  });

  it("navigates to the reviewed conversion workflow", async () => {
    window.location.hash = "#/sales";
    render(<App build={{ revision: "abc123" }} />);

    fireEvent.click(await screen.findByRole("link", { name: "Conversion" }));
    await act(async () => {
      await vi.dynamicImportSettled();
    });

    expect(
      await screen.findByRole("heading", { name: "Create Project" }),
    ).toBeVisible();
    expect(window.location.hash).toBe("#/conversion");
  });

  it("navigates to AI provider and policy settings", async () => {
    window.location.hash = "#/sales";
    render(
      <App build={{ revision: "abc123" }} aiSettingsAPI={aiSettingsAPI} />,
    );

    fireEvent.click(await screen.findByRole("button", { name: "Admin" }));
    fireEvent.click(screen.getByRole("link", { name: "AI settings" }));
    await act(async () => {
      await vi.dynamicImportSettled();
    });

    expect(
      await screen.findByRole("heading", { name: "AI providers and policy" }),
    ).toBeVisible();
    expect(window.location.hash).toBe("#/ai-settings");
    window.history.replaceState(null, "", "#/sales");
  });

  it("navigates to the technician AI assistance workspace", async () => {
    window.history.replaceState(null, "", "#/ai-assist");
    render(
      <App
        build={{ revision: "abc123" }}
        aiSettingsAPI={aiSettingsAPI}
        aiAssistAPI={aiAssistAPI}
      />,
    );

    expect(
      await screen.findByRole("heading", { name: "AI assistance" }),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Generate summary" }),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Generate summary" }),
    ).toBeDisabled();
    expect(screen.getByText(/provide a Work Record UUID/i)).toBeVisible();
    window.history.replaceState(null, "", "#/sales");
  });

  it("passes the authorized Client directory and page filter separately to the global drawer", async () => {
    window.location.hash = "#/sales";
    render(
      <App build={{ revision: "abc123" }} aiWorkspaceAPI={aiWorkspaceAPI} />,
    );

    fireEvent.change(
      await screen.findByRole("combobox", { name: "Active client" }),
      {
        target: { value: "client-2" },
      },
    );
    fireEvent.click(screen.getByRole("button", { name: "Open AI workspace" }));
    fireEvent.click(
      await screen.findByRole("button", { name: "Prepare action" }),
    );

    const targetClient = screen.getByRole("combobox", {
      name: "Target client",
    });
    expect(targetClient).toHaveValue("client-2");
    expect(targetClient).toHaveTextContent("Northwind Legal (CLIENT-001)");
    expect(targetClient).toHaveTextContent(
      "Contoso Manufacturing (CLIENT-002)",
    );
  });

  it("refreshes the authorized Client directory after AI creates a Client", async () => {
    window.location.hash = "#/sales";
    const createdClient = {
      id: "client-created",
      display_id: "CLIENT-CEDAR",
      name: "Cedar Grove",
    };
    const clientCreateProposal: AIWorkspaceProposal = {
      id: "proposal-client",
      tool_name: "client.create",
      tool_version: 1,
      preview: {
        summary: "Create client Cedar Grove",
        target_type: "client",
        target_id: createdClient.id,
        changes: {
          name: { before: null, after: createdClient.name },
          display_id: {
            before: null,
            after: createdClient.display_id,
          },
          lifecycle_state: { before: null, after: "active" },
        },
      },
      required_capability: "client.create",
      expires_at: "2026-08-04T12:10:00Z",
      state: "pending",
      version: 1,
    };
    const projectProposal: AIWorkspaceProposal = {
      id: "proposal-project",
      target_client_id: createdClient.id,
      tool_name: "project.create",
      tool_version: 1,
      preview: {
        summary: "Create project Onboarding",
        target_type: "project",
        target_id: "project-1",
        changes: {
          name: { before: null, after: "Onboarding" },
        },
      },
      required_capability: "project.create",
      expires_at: "2026-08-04T12:10:00Z",
      state: "pending",
      version: 1,
    };
    let resolveInitialDirectory: (response: Response) => void = () => undefined;
    const initialDirectoryResponse = new Promise<Response>((resolve) => {
      resolveInitialDirectory = resolve;
    });
    let directoryLoads = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input);
        if (url.endsWith("/api/v1/setup/status")) {
          return Response.json({
            completed: true,
            bootstrap_available: false,
            entra_available: false,
          });
        }
        if (url.endsWith("/api/v1/me")) {
          return Response.json({
            id: "technician-id",
            navigation: navigation.map(({ page }) => page),
            capabilities: [
              "work_record.read",
              "sales.read",
              "sales.write",
              "project.read",
              "project.write",
              "ai.assist",
              "client.create",
            ],
          });
        }
        if (url.endsWith("/api/v1/directory")) {
          directoryLoads += 1;
          if (directoryLoads === 1) return initialDirectoryResponse;
          return Response.json({
            clients: [
              {
                id: "client-id",
                display_id: "CLIENT-001",
                name: "Northwind Legal",
              },
              ...(directoryLoads > 1 ? [createdClient] : []),
            ],
            departments: [],
            teams: [],
            queues: [],
          });
        }
        if (url.endsWith("/auth/session/refresh")) {
          return new Response(null, { status: 204 });
        }
        return new Response("not found", { status: 404 });
      }),
    );
    const workspaceAPI: AIWorkspaceAPI = {
      list: vi.fn().mockResolvedValue([
        {
          id: "conversation-1",
          title: "Support",
          version: 1,
          created_at: "2026-08-04T12:00:00Z",
          updated_at: "2026-08-04T12:00:00Z",
        },
      ]),
      create: vi.fn(),
      get: vi.fn().mockResolvedValue({
        conversation: {
          id: "conversation-1",
          title: "Support",
          version: 1,
          created_at: "2026-08-04T12:00:00Z",
          updated_at: "2026-08-04T12:00:00Z",
        },
        messages: [
          {
            id: "message-1",
            conversation_id: "conversation-1",
            role: "assistant",
            text: "How can I help?",
            created_at: "2026-08-04T12:00:00Z",
          },
        ],
      }),
      sendMessage: vi.fn(async (conversationID, text) => {
        const clientCreate = text.includes("Create a client");
        return {
          user_message: {
            id: clientCreate ? "message-2" : "message-4",
            conversation_id: conversationID,
            role: "user" as const,
            text,
            created_at: "2026-08-04T12:01:00Z",
          },
          assistant_message: {
            id: clientCreate ? "message-3" : "message-5",
            conversation_id: conversationID,
            role: "assistant" as const,
            text: "Review the exact preview before confirming.",
            created_at: "2026-08-04T12:01:01Z",
          },
          proposal: clientCreate ? clientCreateProposal : projectProposal,
        };
      }),
      propose: vi.fn(),
      runRead: vi.fn(),
      confirm: vi.fn().mockResolvedValue({
        summary: "Created client CLIENT-CEDAR",
        data: {
          client_id: createdClient.id,
          display_id: createdClient.display_id,
          name: createdClient.name,
          lifecycle_state: "active",
        },
      }),
      reject: vi.fn(),
    };
    render(
      <App build={{ revision: "abc123" }} aiWorkspaceAPI={workspaceAPI} />,
    );
    fireEvent.click(
      await screen.findByRole("button", { name: "Open AI workspace" }),
    );
    await waitFor(() => expect(directoryLoads).toBe(1));
    await screen.findByText("How can I help?");

    fireEvent.change(screen.getByRole("textbox", { name: "Ask Rarity AI" }), {
      target: {
        value: "Create a client named Cedar Grove with display ID CLIENT-CEDAR",
      },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    fireEvent.click(
      await screen.findByRole("button", {
        name: "Confirm Create client Cedar Grove",
      }),
    );

    expect(
      await screen.findByText("Created client CLIENT-CEDAR"),
    ).toBeVisible();
    await waitFor(() => expect(directoryLoads).toBe(2));
    expect(
      screen.getByRole("combobox", { name: "Active client" }),
    ).toHaveTextContent("Cedar Grove");
    await act(async () => {
      resolveInitialDirectory(
        Response.json({
          clients: [
            {
              id: "client-id",
              display_id: "CLIENT-001",
              name: "Northwind Legal",
            },
          ],
          departments: [],
          teams: [],
          queues: [],
        }),
      );
      await initialDirectoryResponse;
    });
    expect(
      screen.getByRole("combobox", { name: "Active client" }),
    ).toHaveTextContent("Cedar Grove");

    fireEvent.click(screen.getByRole("button", { name: "Prepare action" }));
    expect(
      screen.getByRole("combobox", { name: "Target client" }),
    ).toHaveTextContent("Cedar Grove (CLIENT-CEDAR)");
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    fireEvent.change(screen.getByRole("textbox", { name: "Ask Rarity AI" }), {
      target: { value: "Create project Onboarding for Cedar Grove" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));

    expect(await screen.findByText("Create project Onboarding")).toBeVisible();
    expect(screen.getByText("Cedar Grove (CLIENT-CEDAR)")).toBeVisible();
    expect(
      screen.getByRole("button", {
        name: "Confirm Create project Onboarding",
      }),
    ).toBeEnabled();
  });

  it("navigates to protected Teams connection settings", async () => {
    window.location.hash = "#/sales";
    render(
      <App
        build={{ revision: "abc123" }}
        teamsSettingsAPI={teamsSettingsAPI}
      />,
    );
    fireEvent.click(await screen.findByRole("button", { name: "Admin" }));
    fireEvent.click(screen.getByRole("link", { name: "Teams settings" }));
    await act(async () => {
      await vi.dynamicImportSettled();
    });
    expect(
      await screen.findByRole("heading", {
        name: "Microsoft Teams connections",
      }),
    ).toBeVisible();
    expect(window.location.hash).toBe("#/teams-settings");
  });

  it("provides a keyboard skip link to the active workflow", async () => {
    window.location.hash = "#/sales";
    render(<App build={{ revision: "abc123" }} />);

    expect(
      await screen.findByRole("link", { name: "Skip to main content" }),
    ).toHaveAttribute("href", "#main-content");
    expect(screen.getByRole("main")).toHaveAttribute("id", "main-content");
  });

  it("keeps desktop-only routes out of the phone workspace", async () => {
    vi.stubGlobal("matchMedia", (query: string) => ({
      matches: query.includes("max-width"),
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    }));
    window.location.hash = "#/operations";

    render(<App build={{ revision: "abc123" }} />);

    expect(
      await screen.findByText(/This workspace needs a larger screen/i),
    ).toBeVisible();
    expect(
      screen.queryByRole("link", { name: "Operations" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Return to mobile Home" }),
    ).toBeVisible();
  });

  it.each([
    "sales",
    "proposal",
    "conversion",
    "project",
    "ai-settings",
    "ai-assist",
    "teams-settings",
  ])(
    "has no detectable WCAG A or AA violations in the %s workflow",
    async (page) => {
      window.location.hash = `#/${page}`;
      const { container } = render(
        <App
          build={{ revision: "abc123" }}
          aiSettingsAPI={aiSettingsAPI}
          aiAssistAPI={aiAssistAPI}
          teamsSettingsAPI={teamsSettingsAPI}
        />,
      );

      await screen.findByRole("navigation", { name: "Primary" });
      const result = await axe.run(container, {
        runOnly: {
          type: "tag",
          values: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"],
        },
      });

      expect(result.violations).toEqual([]);
    },
  );

  it("renders every protected route in the Signal console with one active main landmark", async () => {
    const routes = routeManifest.filter(
      ({ protected: isProtected }) => isProtected,
    );

    for (const route of routes) {
      window.location.hash = `#/${route.id}`;
      const view = render(
        <App
          build={{ revision: "route-smoke" }}
          aiSettingsAPI={aiSettingsAPI}
          aiAssistAPI={aiAssistAPI}
          teamsSettingsAPI={teamsSettingsAPI}
          catalogEnabled
        />,
      );

      await screen.findByRole("navigation", { name: "Primary" });
      await waitFor(() => {
        expect(
          view.container.querySelector(".rarity-app"),
          `${route.id} should use the Signal shell`,
        ).not.toHaveAttribute("data-direction");
        expect(
          view.container.querySelectorAll("#main-content"),
          `${route.id} should expose one active main landmark`,
        ).toHaveLength(1);
        expect(
          view.container.querySelector(".rarity-topbar__title"),
          `${route.id} should identify its active route`,
        ).toHaveTextContent(route.label);
        const directionPage = view.container.querySelector(
          ".rti-workspace-stage__page:not([hidden]) .rti-direction-page",
        );
        expect(
          directionPage,
          `${route.id} should use its Signal page composition`,
        ).toHaveAttribute("data-layout", "console");
        expect(directionPage).toHaveAttribute("data-family", route.family);
        expect(
          view.container.querySelector(".rti-atlas-rail"),
        ).not.toBeInTheDocument();
        expect(
          view.container.querySelector(".rti-signal-context"),
        ).not.toBeInTheDocument();
        expect(
          screen.queryByLabelText("Design direction"),
        ).not.toBeInTheDocument();
        expect(
          screen.queryByText("Synthetic preview data"),
        ).not.toBeInTheDocument();
        expect(
          screen.queryByText("Northstar Managed Services"),
        ).not.toBeInTheDocument();
        expect(
          screen.queryByRole("navigation", { name: "Open records" }),
        ).not.toBeInTheDocument();
      });

      cleanup();
      window.sessionStorage.clear();
    }
  }, 60_000);
});
