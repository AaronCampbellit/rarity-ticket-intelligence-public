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

import {
  PresentationProvider,
  serializeWorkspace,
  useWorkspace,
  WorkspaceProvider,
  workspaceStorageKey,
} from "../../design-system";
import { TechnicianWorklist } from "./TechnicianWorklist";
import { __resetClientClassificationCatalogForTests } from "../classification/useClientCatalog";

vi.mock("./TicketTimer", () => ({
  TicketTimer: ({ onCapture }: { onCapture(capture: unknown): void }) => (
    <button
      type="button"
      onClick={() =>
        onCapture({ id: "capture-1", version: 1, durationSeconds: 600 })
      }
    >
      Load time capture
    </button>
  ),
}));

function wireRecord(
  owner = "",
  version = 2,
  id = "work-1",
  displayID = "INC-10482",
  title = "VPN unavailable",
) {
  return {
    ID: id,
    DisplayID: displayID,
    Type: "incident",
    Title: title,
    Description: "Remote staff cannot connect.",
    Status: "triage",
    Priority: "high",
    QueueID: "queue-1",
    PrimaryOwnerID: owner,
    UpdatedAt: "2026-07-30T12:00:00Z",
    Version: version,
  };
}

async function assertLateTimeMutationsStayOnTheirTicket(
  response: () => Response,
) {
  const pending: Array<(next: Response) => void> = [];
  const fetcher = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url.endsWith("/api/v1/me"))
      return Promise.resolve(Response.json({ id: "technician-1" }));
    if (url.includes("/api/v1/views"))
      return Promise.resolve(Response.json([]));
    if (url === "/api/v1/tag-groups")
      return Promise.resolve(
        Response.json([
          {
            id: "technology",
            label: "Technology",
            description: "",
            state: "active",
            version: 1,
          },
        ]),
      );
    if (url === "/api/v1/tags")
      return Promise.resolve(
        Response.json([
          {
            id: "tag-network",
            label: "Network",
            group_id: "technology",
            state: "active",
            synonyms: [],
            version: 1,
          },
        ]),
      );
    if (url === "/api/v1/labor-roles")
      return Promise.resolve(
        Response.json([
          {
            id: "role-1",
            key: "engineer",
            version: 1,
            current_version: {
              id: "role-version-1",
              name: "Engineer",
              internal_cost_minor: 1,
              bill_rate_minor: 1,
              currency: "USD",
            },
          },
        ]),
      );
    if (init?.method === "POST")
      return new Promise<Response>((resolve) => pending.push(resolve));
    return Promise.resolve(
      Response.json([
        wireRecord(),
        wireRecord("", 1, "work-2", "INC-10483", "Mail flow delayed"),
      ]),
    );
  });
  vi.stubGlobal("fetch", fetcher);
  render(
    <TechnicianWorklist
      clientID="client-1"
      preferencePrincipalID="technician-1"
    />,
  );
  await screen.findByRole("heading", { name: "VPN unavailable" });

  async function leaveAWithPendingRequest(
    submit: () => Promise<void>,
    staleSuccess: string,
  ) {
    await submit();
    await waitFor(() => expect(pending).toHaveLength(1));
    fireEvent.click(screen.getByText("Mail flow delayed"));
    await screen.findByRole("heading", { name: "Mail flow delayed" });
    fireEvent.click(screen.getByRole("button", { name: "Load time capture" }));
    await waitFor(() =>
      expect(
        screen.getAllByRole("combobox", { name: /Classification tags/ }),
      ).toHaveLength(2),
    );
    const [captured, manual] = screen.getAllByRole("combobox", {
      name: /Classification tags/,
    });
    fireEvent.focus(captured!);
    fireEvent.click(
      (await screen.findAllByRole("option", { name: "Network" }))[0]!,
    );
    fireEvent.focus(manual!);
    fireEvent.click(
      (await screen.findAllByRole("option", { name: "Network" }))[1]!,
    );
    fireEvent.change(screen.getByRole("combobox", { name: "Labor role" }), {
      target: { value: "role-1" },
    });
    fireEvent.change(screen.getByLabelText("Time note"), {
      target: { value: "B capture" },
    });
    fireEvent.change(screen.getByLabelText("Minutes worked"), {
      target: { value: "15" },
    });
    fireEvent.change(screen.getByLabelText("Work note"), {
      target: { value: "B manual" },
    });
    fireEvent.click(
      screen.getByRole("checkbox", { name: "Include time capture" }),
    );
    fireEvent.change(screen.getByLabelText("Message"), {
      target: { value: "B comment" },
    });
    await act(async () => pending.shift()!(response()));
    expect(
      screen.getByRole("heading", { name: "Mail flow delayed" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(
        "Classification changed. Confirm at least one current classification tag.",
      ),
    ).not.toBeInTheDocument();
    expect(screen.queryByText(staleSuccess)).not.toBeInTheDocument();
    expect(screen.getByLabelText("Time note")).toHaveValue("B capture");
    expect(screen.getByRole("combobox", { name: "Labor role" })).toHaveValue(
      "role-1",
    );
    expect(screen.getByLabelText("Work note")).toHaveValue("B manual");
    expect(
      screen.getByRole("checkbox", { name: "Include time capture" }),
    ).toBeChecked();
    expect(screen.getByLabelText("Message")).toHaveValue("B comment");
    expect(
      screen.getAllByRole("group", {
        name: /Classification tags selections/,
      })[0],
    ).toHaveTextContent("Network");
    expect(
      screen.getAllByRole("group", {
        name: /Classification tags selections/,
      })[1],
    ).toHaveTextContent("Network");
    fireEvent.click(screen.getByText("VPN unavailable"));
    await screen.findByRole("heading", { name: "VPN unavailable" });
  }

  await leaveAWithPendingRequest(async () => {
    const picker = await screen.findByRole("combobox", {
      name: /Classification tags/,
    });
    fireEvent.focus(picker);
    fireEvent.click(await screen.findByRole("option", { name: "Network" }));
    fireEvent.change(screen.getByLabelText("Minutes worked"), {
      target: { value: "10" },
    });
    fireEvent.change(screen.getByLabelText("Work note"), {
      target: { value: "Manual" },
    });
    const button = screen.getByRole("button", { name: "Record time" });
    await waitFor(() => expect(button).toBeEnabled());
    fireEvent.click(button);
  }, "Time entry recorded.");
  await leaveAWithPendingRequest(async () => {
    fireEvent.click(screen.getByRole("button", { name: "Load time capture" }));
    await waitFor(() =>
      expect(
        screen.getAllByRole("combobox", { name: /Classification tags/ }),
      ).toHaveLength(2),
    );
    const [picker] = screen.getAllByRole("combobox", {
      name: /Classification tags/,
    });
    fireEvent.focus(picker!);
    fireEvent.click(
      (await screen.findAllByRole("option", { name: "Network" }))[0]!,
    );
    fireEvent.change(screen.getByRole("combobox", { name: "Labor role" }), {
      target: { value: "role-1" },
    });
    const button = screen.getByRole("button", { name: "Record captured time" });
    await waitFor(() => expect(button).toBeEnabled());
    fireEvent.click(button);
  }, "Captured time recorded.");
  await leaveAWithPendingRequest(async () => {
    fireEvent.click(screen.getByRole("button", { name: "Load time capture" }));
    await waitFor(() =>
      expect(
        screen.getAllByRole("combobox", { name: /Classification tags/ }),
      ).toHaveLength(2),
    );
    const [picker] = screen.getAllByRole("combobox", {
      name: /Classification tags/,
    });
    fireEvent.focus(picker!);
    fireEvent.click(
      (await screen.findAllByRole("option", { name: "Network" }))[0]!,
    );
    fireEvent.change(screen.getByRole("combobox", { name: "Labor role" }), {
      target: { value: "role-1" },
    });
    fireEvent.click(
      screen.getByRole("checkbox", { name: "Include time capture" }),
    );
    fireEvent.change(screen.getByLabelText("Message"), {
      target: { value: "Comment" },
    });
    const button = screen.getByRole("button", { name: "Record message" });
    await waitFor(() => expect(button).toBeEnabled());
    fireEvent.click(button);
  }, "Message recorded.");
}

describe("TechnicianWorklist", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    localStorage.clear();
    sessionStorage.clear();
    __resetClientClassificationCatalogForTests();
  });

  it("loads live work and persists its Kanban preference", async () => {
    const fetcher = liveWorkFetcher();
    vi.stubGlobal("fetch", fetcher);

    render(
      <PresentationProvider value={{ density: "adaptive" }}>
        <TechnicianWorklist clientID="10000000-0000-4000-8000-000000000001" />
      </PresentationProvider>,
    );

    expect(
      await screen.findByRole("heading", {
        name: "VPN unavailable",
      }),
    ).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Kanban view" }));
    expect(localStorage.getItem("rti:view:anonymous:work")).toBe('"kanban"');
    expect(fetcher).toHaveBeenCalled();
  });

  it("keeps captured and manual classifications independent on stale-tag rejection", async () => {
    const fetcher = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (url.endsWith("/api/v1/me"))
          return Response.json({ id: "technician-1" });
        if (url.includes("/api/v1/views")) return Response.json([]);
        if (url === "/api/v1/tag-groups")
          return Response.json([
            {
              id: "technology",
              label: "Technology",
              description: "",
              state: "active",
              version: 1,
            },
          ]);
        if (url === "/api/v1/tags")
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
        if (url === "/api/v1/labor-roles")
          return Response.json([
            {
              id: "role-1",
              key: "engineer",
              version: 1,
              current_version: {
                id: "role-version-1",
                name: "Engineer",
                internal_cost_minor: 1,
                bill_rate_minor: 1,
                currency: "USD",
              },
            },
          ]);
        if (url.endsWith("/time-entries") && init?.method === "POST")
          return Response.json(
            { error: { code: "tag_archived" } },
            { status: 422 },
          );
        return Response.json([
          wireRecord(),
          wireRecord("", 1, "work-2", "INC-10483", "Mail flow delayed"),
        ]);
      },
    );
    vi.stubGlobal("fetch", fetcher);
    render(
      <TechnicianWorklist
        clientID="client-1"
        preferencePrincipalID="technician-1"
      />,
    );
    await screen.findByRole("heading", { name: "VPN unavailable" });
    fireEvent.click(screen.getByRole("button", { name: "Load time capture" }));
    await screen.findByRole("option", { name: "Engineer" });
    const pickers = await screen.findAllByRole("combobox", {
      name: /Classification tags/,
    });
    fireEvent.focus(pickers[0]!);
    fireEvent.click(await screen.findByRole("option", { name: "Network" }));
    fireEvent.change(screen.getByRole("combobox", { name: "Labor role" }), {
      target: { value: "role-1" },
    });
    fireEvent.change(screen.getByLabelText("Time note"), {
      target: { value: "Captured context" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Record captured time" }),
    );
    await screen.findByText(
      "Classification changed. Confirm at least one current classification tag.",
    );
    expect(document.activeElement).toBe(pickers[0]);
    expect(screen.getByLabelText("Time note")).toHaveValue("Captured context");
    expect(
      screen.getAllByRole("group", {
        name: /Classification tags selections/,
      })[0],
    ).toHaveTextContent("Network");
    fireEvent.focus(pickers[1]!);
    fireEvent.click(
      (await screen.findAllByRole("option", { name: "Network" }))[1]!,
    );
    fireEvent.change(screen.getByLabelText("Work note"), {
      target: { value: "Manual context" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Record time" }));
    await waitFor(() =>
      expect(document.activeElement).toHaveAttribute("aria-invalid", "true"),
    );
    expect(
      screen
        .getAllByRole("combobox", { name: /Classification tags/ })
        .filter((picker) => picker.getAttribute("aria-invalid") === "true"),
    ).toHaveLength(1);
    expect(screen.getByLabelText("Work note")).toHaveValue("Manual context");
    expect(
      screen.getAllByRole("group", {
        name: /Classification tags selections/,
      })[1],
    ).toHaveTextContent("Network");
    fireEvent.click(
      screen.getByRole("button", { name: "Remove time capture" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Load time capture" }));
    await screen.findByText("Time capture");
    expect(
      screen.getAllByRole("group", {
        name: /Classification tags selections/,
      })[0],
    ).not.toHaveTextContent("Network");
  });

  it("ignores late manual, captured, and comment successes after selecting another ticket", async () => {
    await assertLateTimeMutationsStayOnTheirTicket(() => Response.json({}));
  });

  it("ignores late manual, captured, and comment classification failures after selecting another ticket", async () => {
    await assertLateTimeMutationsStayOnTheirTicket(() =>
      Response.json(
        { error: { code: "classification_required" } },
        { status: 422 },
      ),
    );
  });

  it("requires, transports, and retains capture classification when a comment records time", async () => {
    let commentBody = "";
    const fetcher = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (url.endsWith("/api/v1/me"))
          return Response.json({ id: "technician-1" });
        if (url.includes("/api/v1/views")) return Response.json([]);
        if (url === "/api/v1/tag-groups")
          return Response.json([
            {
              id: "technology",
              label: "Technology",
              description: "",
              state: "active",
              version: 1,
            },
          ]);
        if (url === "/api/v1/tags")
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
        if (url === "/api/v1/labor-roles")
          return Response.json([
            {
              id: "role-1",
              key: "engineer",
              version: 1,
              current_version: {
                id: "role-version-1",
                name: "Engineer",
                internal_cost_minor: 1,
                bill_rate_minor: 1,
                currency: "USD",
              },
            },
          ]);
        if (init?.method === "POST") {
          commentBody = String(init.body);
          return Response.json(
            { error: { code: "tag_archived" } },
            { status: 422 },
          );
        }
        return Response.json([wireRecord()]);
      },
    );
    vi.stubGlobal("fetch", fetcher);
    render(
      <TechnicianWorklist
        clientID="client-1"
        preferencePrincipalID="technician-1"
      />,
    );
    await screen.findByRole("heading", { name: "VPN unavailable" });
    fireEvent.click(screen.getByRole("button", { name: "Load time capture" }));
    await waitFor(() =>
      expect(
        screen.getAllByRole("combobox", { name: /Classification tags/ }),
      ).toHaveLength(2),
    );
    const picker = screen.getAllByRole("combobox", {
      name: /Classification tags/,
    })[0]!;
    fireEvent.focus(picker);
    fireEvent.click(await screen.findByRole("option", { name: "Network" }));
    fireEvent.change(screen.getByRole("combobox", { name: "Labor role" }), {
      target: { value: "role-1" },
    });
    fireEvent.click(
      screen.getByRole("checkbox", { name: "Include time capture" }),
    );
    fireEvent.change(screen.getByLabelText("Message"), {
      target: { value: "Captured work context" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Record message" }));
    await waitFor(() =>
      expect(commentBody).toContain('"tag_ids":["tag-network"]'),
    );
    await screen.findByText(
      "Classification changed. Confirm at least one current classification tag.",
    );
    expect(document.activeElement).toBe(picker);
    expect(screen.getByLabelText("Message")).toHaveValue(
      "Captured work context",
    );
    expect(
      screen.getAllByRole("group", {
        name: /Classification tags selections/,
      })[0],
    ).toHaveTextContent("Network");
  });

  it("opens and focuses ticket classification after a terminal status rejection", async () => {
    const fetcher = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (url.endsWith("/api/v1/me"))
          return Response.json({ id: "technician-1" });
        if (url.includes("/api/v1/views")) return Response.json([]);
        if (url === "/api/v1/tag-groups")
          return Response.json([
            {
              id: "technology",
              label: "Technology",
              description: "",
              state: "active",
              version: 1,
            },
          ]);
        if (url === "/api/v1/tags")
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
        if (url.includes("/history")) return Response.json([]);
        if (url.includes("/objects/work_record/"))
          return Response.json({
            target: { object_type: "work_record", object_id: "work-1" },
            object_version: 1,
            direct: [],
            inherited: [],
            effective: [],
            classification_state: "unclassified",
          });
        if (url.endsWith("/transition") && init?.method === "POST")
          return Response.json(
            {
              error: {
                code: "classification_required",
                recovery_url: "/api/v1/objects/work_record/work-1/tags",
              },
            },
            { status: 422 },
          );
        return Response.json([
          wireRecord(),
          wireRecord("", 1, "work-2", "INC-10483", "Mail flow delayed"),
        ]);
      },
    );
    vi.stubGlobal("fetch", fetcher);
    render(
      <TechnicianWorklist
        clientID="client-1"
        preferencePrincipalID="technician-1"
      />,
    );
    await screen.findByRole("heading", { name: "VPN unavailable" });
    fireEvent.change(screen.getByLabelText("Move to status"), {
      target: { value: "resolved" },
    });
    fireEvent.change(screen.getAllByLabelText("Reason")[0]!, {
      target: { value: "Completed" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Update status" }));
    await screen.findByText(
      /Classification is required before this terminal ticket action/,
    );
    const editor = await screen.findByRole("heading", {
      name: "Classification",
    });
    expect(editor.closest("section")).toHaveFocus();
    expect(screen.getByLabelText("Move to status")).toHaveValue("resolved");
    fireEvent.click(screen.getByText("Mail flow delayed"));
    await screen.findByRole("heading", { name: "Mail flow delayed" });
    await waitFor(() =>
      expect(
        screen.queryByText(/terminal ticket action/),
      ).not.toBeInTheDocument(),
    );
    expect(
      document.querySelector(".rti-object-tag-editor__disclosure"),
    ).not.toHaveAttribute("open");
  });

  it("keeps pending captured and manual classifications through a status update", async () => {
    const fetcher = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (url.endsWith("/api/v1/me"))
          return Response.json({ id: "technician-1" });
        if (url.includes("/api/v1/views")) return Response.json([]);
        if (url === "/api/v1/tag-groups")
          return Response.json([
            {
              id: "technology",
              label: "Technology",
              description: "",
              state: "active",
              version: 1,
            },
          ]);
        if (url === "/api/v1/tags")
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
        if (url === "/api/v1/labor-roles")
          return Response.json([
            {
              id: "role-1",
              key: "engineer",
              version: 1,
              current_version: {
                id: "role-version-1",
                name: "Engineer",
                internal_cost_minor: 1,
                bill_rate_minor: 1,
                currency: "USD",
              },
            },
          ]);
        if (url.endsWith("/transition") && init?.method === "POST")
          return Response.json(wireRecord("", 3));
        return Response.json([wireRecord()]);
      },
    );
    vi.stubGlobal("fetch", fetcher);
    render(
      <TechnicianWorklist
        clientID="client-1"
        preferencePrincipalID="technician-1"
      />,
    );
    await screen.findByRole("heading", { name: "VPN unavailable" });
    fireEvent.click(screen.getByRole("button", { name: "Load time capture" }));
    await waitFor(() =>
      expect(
        screen.getAllByRole("combobox", { name: /Classification tags/ }),
      ).toHaveLength(2),
    );
    const [captured, manual] = screen.getAllByRole("combobox", {
      name: /Classification tags/,
    });
    fireEvent.focus(captured!);
    fireEvent.click(
      (await screen.findAllByRole("option", { name: "Network" }))[0]!,
    );
    fireEvent.focus(manual!);
    fireEvent.click(
      (await screen.findAllByRole("option", { name: "Network" }))[1]!,
    );
    fireEvent.change(screen.getByLabelText("Move to status"), {
      target: { value: "in_progress" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Update status" }));
    await screen.findByText("Status updated.");
    expect(
      screen.getAllByRole("group", {
        name: /Classification tags selections/,
      })[0],
    ).toHaveTextContent("Network");
    expect(
      screen.getAllByRole("group", {
        name: /Classification tags selections/,
      })[1],
    ).toHaveTextContent("Network");
  });

  it("ignores a late terminal classification failure after selecting another ticket", async () => {
    let resolveTransition: (response: Response) => void = () => undefined;
    const fetcher = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (url.endsWith("/api/v1/me"))
          return Response.json({ id: "technician-1" });
        if (url.includes("/api/v1/views")) return Response.json([]);
        if (url.endsWith("/transition") && init?.method === "POST")
          return new Promise<Response>((resolve) => {
            resolveTransition = resolve;
          });
        return Response.json([
          wireRecord(),
          wireRecord("", 1, "work-2", "INC-10483", "Mail flow delayed"),
        ]);
      },
    );
    vi.stubGlobal("fetch", fetcher);
    render(
      <TechnicianWorklist
        clientID="client-1"
        preferencePrincipalID="technician-1"
      />,
    );
    await screen.findByRole("heading", { name: "VPN unavailable" });
    fireEvent.change(screen.getByLabelText("Move to status"), {
      target: { value: "resolved" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Update status" }));
    fireEvent.click(screen.getByText("Mail flow delayed"));
    await screen.findByRole("heading", { name: "Mail flow delayed" });
    resolveTransition(
      Response.json(
        { error: { code: "classification_required" } },
        { status: 422 },
      ),
    );
    await waitFor(() =>
      expect(
        screen.queryByText(/terminal ticket action/),
      ).not.toBeInTheDocument(),
    );
    expect(
      screen.queryByRole("heading", { name: "Classification" }),
    ).not.toBeInTheDocument();
  });

  it("ignores a late terminal success after selecting another ticket", async () => {
    let resolveTransition: (response: Response) => void = () => undefined;
    const fetcher = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/api/v1/me"))
        return Promise.resolve(Response.json({ id: "technician-1" }));
      if (url.includes("/api/v1/views"))
        return Promise.resolve(Response.json([]));
      if (url.endsWith("/transition") && init?.method === "POST")
        return new Promise<Response>((resolve) => {
          resolveTransition = resolve;
        });
      return Promise.resolve(
        Response.json([
          wireRecord(),
          wireRecord("", 1, "work-2", "INC-10483", "Mail flow delayed"),
        ]),
      );
    });
    vi.stubGlobal("fetch", fetcher);
    render(
      <TechnicianWorklist
        clientID="client-1"
        preferencePrincipalID="technician-1"
      />,
    );
    await screen.findByRole("heading", { name: "VPN unavailable" });
    fireEvent.change(screen.getByLabelText("Move to status"), {
      target: { value: "resolved" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Update status" }));
    fireEvent.click(screen.getByText("Mail flow delayed"));
    await screen.findByRole("heading", { name: "Mail flow delayed" });
    await act(async () => resolveTransition(Response.json(wireRecord("", 3))));
    await waitFor(() =>
      expect(screen.queryByText("Status updated.")).not.toBeInTheDocument(),
    );
    expect(
      screen.getByRole("heading", { name: "Mail flow delayed" }),
    ).toBeInTheDocument();
  });

  it("opens a queue ticket as a full workspace on double click", async () => {
    const openRecord = vi.fn();
    vi.stubGlobal("fetch", liveWorkFetcher());
    render(
      <WorkspaceProvider
        principalID="technician-1"
        allowedRouteIDs={new Set(["work"])}
        allowedClientIDs={new Set(["10000000-0000-4000-8000-000000000001"])}
        onOpenRecord={openRecord}
      >
        <TechnicianWorklist clientID="10000000-0000-4000-8000-000000000001" />
      </WorkspaceProvider>,
    );

    const ticket = (await screen.findAllByText("VPN unavailable"))
      .map((element) => element.closest("button.work-row"))
      .find(Boolean)!;
    fireEvent.doubleClick(ticket);
    expect(openRecord).toHaveBeenCalledWith(
      expect.objectContaining({
        id: expect.stringMatching(/^ticket:/),
        routeID: "work",
        entityType: "ticket",
      }),
    );
  });

  it("explains how to recover when no permitted Client is available", () => {
    render(<TechnicianWorklist clientID="" />);

    expect(
      screen.getByText(
        "Select an available Client to load live work. If no Clients are available, ask an administrator to review your access.",
      ),
    ).toBeVisible();
  });

  it("claims live work and keeps the legacy composer public-only", async () => {
    const fetcher = vi.fn(
      async (input: RequestInfo | URL, _init?: RequestInit) => {
        const url = String(input);
        if (url.endsWith("/api/v1/me")) {
          return Response.json({ id: "technician-1" });
        }
        if (url.includes("/assign")) {
          return Response.json(wireRecord("technician-1", 3));
        }
        return Response.json([wireRecord()]);
      },
    );
    vi.stubGlobal("fetch", fetcher);

    render(<TechnicianWorklist clientID="client-1" />);
    await screen.findByRole("heading", { name: "VPN unavailable" });
    expect(screen.getByText("Public client reply")).toBeInTheDocument();
    expect(
      screen.queryByText("Internal technician note"),
    ).not.toBeInTheDocument();

    fireEvent.click(await screen.findByRole("button", { name: "Claim" }));
    await screen.findByText("Work claimed.");
    const assignment = fetcher.mock.calls.find(([url]) =>
      String(url).includes("/assign"),
    );
    expect(JSON.parse(String(assignment?.[1]?.body))).toMatchObject({
      owner_id: "technician-1",
      reason: "Claimed from technician worklist",
    });
    fireEvent.change(screen.getByLabelText("Message"), {
      target: { value: "Customer-facing update" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Record message" }));
    await screen.findByText("Client reply recorded.");
    const comment = fetcher.mock.calls.find(([url]) =>
      String(url).endsWith("/comments"),
    );
    expect(JSON.parse(String(comment?.[1]?.body))).toMatchObject({
      visibility: "client",
      body: "Customer-facing update",
    });
  });

  it("retries a failed worklist load", async () => {
    let worklistAttempts = 0;
    const fetcher = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.endsWith("/api/v1/me")) {
        return Response.json({ id: "technician-1" });
      }
      if (url.includes("/api/v1/views")) return Response.json([]);
      if (url.includes("/api/v1/work-records?")) {
        worklistAttempts += 1;
        if (worklistAttempts === 1) {
          return Response.json({}, { status: 503 });
        }
        return Response.json([wireRecord()]);
      }
      return Response.json([]);
    });
    vi.stubGlobal("fetch", fetcher);

    render(<TechnicianWorklist clientID="client-1" />);
    expect(
      await screen.findByRole("heading", {
        name: "Technician work could not be loaded",
      }),
    ).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(
      await screen.findByRole("heading", { name: "VPN unavailable" }),
    ).toBeVisible();
    expect(worklistAttempts).toBe(2);
  });

  it("restores the exact selected record when a workspace tab activates", async () => {
    const records = [
      wireRecord("", 2, "work-1", "INC-10482", "VPN unavailable"),
      wireRecord("", 1, "work-2", "INC-10483", "Mail flow delayed"),
    ];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input);
        if (url.endsWith("/api/v1/me")) {
          return Response.json({ id: "technician-1" });
        }
        if (url.includes("/api/v1/views")) return Response.json([]);
        return Response.json(records);
      }),
    );

    const view = render(
      <TechnicianWorklist clientID="client-1" selectedWorkRecordID="work-1" />,
    );
    expect(
      await screen.findByRole("heading", { name: "VPN unavailable" }),
    ).toBeVisible();
    expect(screen.queryByText("Live queue")).not.toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Workflow" }),
    ).toBeInTheDocument();

    view.rerender(
      <TechnicianWorklist clientID="client-1" selectedWorkRecordID="work-2" />,
    );

    expect(
      await screen.findByRole("heading", { name: "Mail flow delayed" }),
    ).toBeVisible();
  });

  it("loads an exact pinned record when it is outside the bounded queue page", async () => {
    const fetcher = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.endsWith("/api/v1/me")) {
        return Response.json({ id: "technician-1" });
      }
      if (url.includes("/api/v1/views")) return Response.json([]);
      if (url.endsWith("/api/v1/work-records/work-2")) {
        return Response.json(
          wireRecord("", 1, "work-2", "INC-10483", "Mail flow delayed"),
        );
      }
      return Response.json([
        wireRecord("", 2, "work-1", "INC-10482", "VPN unavailable"),
      ]);
    });
    vi.stubGlobal("fetch", fetcher);

    render(
      <TechnicianWorklist clientID="client-1" selectedWorkRecordID="work-2" />,
    );

    expect(
      await screen.findByRole("heading", { name: "Mail flow delayed" }),
    ).toBeVisible();
    expect(fetcher).toHaveBeenCalledWith(
      "/api/v1/work-records/work-2",
      expect.objectContaining({
        credentials: "same-origin",
        signal: expect.any(AbortSignal),
      }),
    );
  });

  it("protects a full ticket tab when its note form has an unsaved draft", async () => {
    const clientID = "client-1";
    const ticket = {
      id: "ticket:work-1",
      routeID: "work" as const,
      recordID: "work-1",
      clientID,
      label: "INC-10482",
      entityType: "ticket" as const,
      openedAt: 1,
    };
    sessionStorage.setItem(
      workspaceStorageKey("technician-1"),
      serializeWorkspace(
        { tabs: [ticket], activeID: ticket.id },
        new Set([clientID]),
      ),
    );
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input);
        if (url.endsWith("/api/v1/me")) {
          return Response.json({ id: "technician-1" });
        }
        if (url.includes("/api/v1/views")) return Response.json([]);
        if (url.includes("/priority")) {
          return Response.json(wireRecord("", 3));
        }
        return Response.json([wireRecord()]);
      }),
    );

    render(
      <WorkspaceProvider
        principalID="technician-1"
        allowedRouteIDs={new Set(["work"])}
        allowedClientIDs={new Set([clientID])}
      >
        <TechnicianWorklist clientID={clientID} selectedWorkRecordID="work-1" />
        <WorkDirtyProbe />
      </WorkspaceProvider>,
    );

    fireEvent.change(await screen.findByLabelText("Message"), {
      target: { value: "Draft technician update" },
    });
    await waitFor(() =>
      expect(screen.getByTestId("work-dirty")).toHaveTextContent("dirty"),
    );

    const priorityForm = screen
      .getByRole("button", { name: "Change priority" })
      .closest("form");
    expect(priorityForm).not.toBeNull();
    fireEvent.change(
      within(priorityForm as HTMLFormElement).getByLabelText("Reason"),
      { target: { value: "Customer impact increased" } },
    );
    fireEvent.click(screen.getByRole("button", { name: "Change priority" }));
    await screen.findByText("Priority updated.");
    expect(screen.getByTestId("work-dirty")).toHaveTextContent("dirty");
  });

  it("mounts internal collaboration for the exact selected work record without replacing the public composer", async () => {
    const fetcher = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.endsWith("/api/v1/me"))
        return Response.json({ id: "technician-1" });
      if (url.includes("/api/v1/views")) return Response.json([]);
      if (url.includes("/internal-content")) return Response.json([]);
      return Response.json([wireRecord()]);
    });
    vi.stubGlobal("fetch", fetcher);

    render(
      <TechnicianWorklist
        clientID="client-1"
        selectedWorkRecordID="work-1"
        preferencePrincipalID="technician-1"
      />,
    );

    expect(
      await screen.findByRole("region", { name: "Internal details" }),
    ).toBeVisible();
    await waitFor(() =>
      expect(fetcher).toHaveBeenCalledWith(
        "/api/v1/work-records/work-1/internal-content",
        expect.objectContaining({
          headers: expect.objectContaining({
            "X-Rarity-Client-ID": "client-1",
          }),
        }),
      ),
    );
    const communication = screen
      .getByRole("heading", { name: "Communication" })
      .closest("section");
    expect(communication).not.toBeNull();
    expect(within(communication!).getByLabelText("Message")).toBeInstanceOf(
      HTMLTextAreaElement,
    );
    expect(
      within(communication!).getByText("Public client reply"),
    ).toBeVisible();
    expect(
      within(communication!).queryByRole("combobox"),
    ).not.toBeInTheDocument();
    expect(
      within(communication!).queryByRole("textbox", {
        name: /internal details/i,
      }),
    ).not.toBeInTheDocument();
  });

  it("direct-loads the exact task collaboration context when a task is selected", async () => {
    let resolveSecondTask!: (response: Response) => void;
    const fetcher = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.endsWith("/api/v1/me"))
        return Response.json({ id: "technician-1" });
      if (url.includes("/api/v1/views")) return Response.json([]);
      if (url === "/api/v1/tasks/task-1")
        return Response.json({
          id: "task-1",
          title: "Call the vendor",
          status: "open",
          estimate_minutes: 30,
          parent: { type: "work_record", id: "work-1" },
        });
      if (url === "/api/v1/tasks/task-1/internal-content")
        return Response.json([]);
      if (url === "/api/v1/tasks/task-2")
        return new Promise<Response>((resolve) => {
          resolveSecondTask = resolve;
        });
      if (url === "/api/v1/tasks/task-2/internal-content")
        return Response.json([]);
      return Response.json([]);
    });
    vi.stubGlobal("fetch", fetcher);

    const view = render(
      <TechnicianWorklist
        clientID="client-1"
        selectedTaskID="task-1"
        preferencePrincipalID="technician-1"
      />,
    );

    expect(
      await screen.findByRole("heading", { name: "Call the vendor" }),
    ).toBeVisible();
    expect(
      screen.getByRole("region", { name: "Internal comments" }),
    ).toBeVisible();
    await waitFor(() =>
      expect(fetcher).toHaveBeenCalledWith(
        "/api/v1/tasks/task-1/internal-content",
        expect.objectContaining({
          headers: expect.objectContaining({
            "X-Rarity-Client-ID": "client-1",
          }),
        }),
      ),
    );

    view.rerender(
      <TechnicianWorklist
        clientID="client-1"
        selectedTaskID="task-2"
        preferencePrincipalID="technician-1"
      />,
    );
    expect(
      screen.queryByRole("heading", { name: "Call the vendor" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Loading task" })).toBeVisible();
    await act(async () =>
      resolveSecondTask(
        Response.json({
          id: "task-2",
          title: "Replace the firewall",
          status: "open",
          estimate_minutes: 60,
          parent: { type: "work_record", id: "work-2" },
        }),
      ),
    );
    expect(
      await screen.findByRole("heading", { name: "Replace the firewall" }),
    ).toBeVisible();
  });
});

function WorkDirtyProbe() {
  const { state } = useWorkspace();
  return (
    <output data-testid="work-dirty">
      {state.tabs[0]?.dirty ? "dirty" : "clean"}
    </output>
  );
}

function liveWorkFetcher() {
  return vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url.endsWith("/api/v1/me")) {
      return Response.json({ id: "technician-1" });
    }
    if (url.includes("/api/v1/views")) return Response.json([]);
    return Response.json([wireRecord()]);
  });
}

it("loads records beyond the first hundred and searches the server", async () => {
  const fallback = liveWorkFetcher();
  const pages = Array.from({ length: 100 }, (_, i) =>
    wireRecord("", 1, `work-${i}`, `INC-${i}`, `Ticket ${i}`),
  );
  const fetcher = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input), "http://localhost");
    if (url.pathname === "/api/v1/work-records") {
      if (url.searchParams.has("text"))
        return Promise.resolve(
          Response.json([
            wireRecord("", 1, "old", "INC-OLD", "Old unresolved ticket"),
          ]),
        );
      if (url.searchParams.has("before_id"))
        return Promise.resolve(
          Response.json([
            wireRecord("", 1, "older", "INC-OLDER", "Older work"),
          ]),
        );
      return Promise.resolve(Response.json(pages));
    }
    return fallback(input);
  });
  vi.stubGlobal("fetch", fetcher);
  render(<TechnicianWorklist clientID="client-1" />);
  fireEvent.click(
    await screen.findByRole("button", { name: "Load more work" }),
  );
  await screen.findByText("Older work");
  expect(screen.getByText("101 records")).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Load more work" }),
  ).not.toBeInTheDocument();
  fireEvent.change(screen.getByLabelText("Search"), {
    target: { value: "Old unresolved" },
  });
  await screen.findByRole("heading", { name: "Old unresolved ticket" });
  expect(screen.getByText("1 records")).toBeInTheDocument();
  expect(
    fetcher.mock.calls.some(([url]) =>
      String(url).includes("text=Old+unresolved"),
    ),
  ).toBe(true);
});

it("discards an older page response after the client changes", async () => {
  const fallback = liveWorkFetcher();
  let finish!: (response: Response) => void;
  const fetcher = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input), "http://localhost");
    if (url.pathname === "/api/v1/work-records") {
      const client = new Headers(init?.headers).get("X-Rarity-Client-ID");
      if (client === "client-2") return Promise.resolve(Response.json([]));
      if (url.searchParams.has("before_id"))
        return new Promise<Response>((resolve) => {
          finish = resolve;
        });
      return Promise.resolve(
        Response.json(
          Array.from({ length: 100 }, (_, i) =>
            wireRecord("", 1, `work-${i}`, `INC-${i}`, `Ticket ${i}`),
          ),
        ),
      );
    }
    return fallback(input);
  });
  vi.stubGlobal("fetch", fetcher);
  const { rerender } = render(<TechnicianWorklist clientID="client-1" />);
  fireEvent.click(
    await screen.findByRole("button", { name: "Load more work" }),
  );
  rerender(<TechnicianWorklist clientID="client-2" />);
  await screen.findByText("0 records");
  await act(async () =>
    finish(
      Response.json([
        wireRecord("", 1, "late", "INC-LATE", "Private old-client ticket"),
      ]),
    ),
  );
  expect(
    screen.queryByText("Private old-client ticket"),
  ).not.toBeInTheDocument();
  expect(screen.getByText("0 records")).toBeInTheDocument();
});
