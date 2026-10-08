import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { PresentationProvider, WorkspaceProvider } from "../../design-system";
import { HomePage } from "./HomePage";

const liveWork = {
  ID: "work-1",
  DisplayID: "INC-1048",
  Type: "incident",
  Title: "Password resets failing after policy rollout",
  Description: "Affected users cannot reset passwords.",
  Status: "triage",
  Priority: "critical",
  QueueID: "queue-1",
  PrimaryOwnerID: "",
  UpdatedAt: "2026-08-05T12:00:00Z",
  Version: 1,
};

beforeEach(() => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(new Response("not found", { status: 404 })),
  );
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("HomePage", () => {
  it("hosts Mentions only with mention.read and preserves operational modules", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input);
        if (url.startsWith("/api/v1/mentions/widget")) {
          return Response.json({
            counts: { unread: 0, read: 0, archived: 0 },
            items: [],
          });
        }
        return new Response("not found", { status: 404 });
      }),
    );
    const view = render(
      <HomePage
        capabilities={new Set(["mention.read"])}
        availableRoutes={new Set(["home"])}
        principalID="technician-1"
        onNavigate={vi.fn()}
        onOpenMention={vi.fn()}
      />,
    );

    expect(
      await screen.findByRole("heading", { name: "Mentions" }),
    ).toBeVisible();
    expect(screen.getByRole("heading", { name: "Schedule" })).toBeVisible();
    view.rerender(
      <HomePage
        capabilities={new Set()}
        availableRoutes={new Set(["home"])}
        principalID="technician-1"
        onNavigate={vi.fn()}
        onOpenMention={vi.fn()}
      />,
    );
    expect(screen.queryByRole("heading", { name: "Mentions" })).toBeNull();
  });

  it("renders authorized live work in the Signal queue", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(Response.json([liveWork])),
    );
    render(
      <PresentationProvider value={{ density: "adaptive" }}>
        <HomePage
          capabilities={new Set(["work_record.read"])}
          availableRoutes={new Set(["home", "work"])}
          clientID="10000000-0000-4000-8000-000000000001"
          principalID="20000000-0000-4000-8000-000000000001"
          onNavigate={vi.fn()}
        />
      </PresentationProvider>,
    );

    expect(screen.getByLabelText("Signal live queue")).toBeVisible();
    expect(
      screen.queryByLabelText("Atlas work canvas"),
    ).not.toBeInTheDocument();
    expect(
      await screen.findByText("Password resets failing after policy rollout"),
    ).toBeVisible();
  });

  it("opens a ticket workspace on double click", async () => {
    const openRecord = vi.fn();
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(Response.json([liveWork])),
    );
    render(
      <WorkspaceProvider
        principalID="technician-1"
        allowedRouteIDs={new Set(["home", "work"])}
        allowedClientIDs={new Set(["10000000-0000-4000-8000-000000000001"])}
        onOpenRecord={openRecord}
      >
        <HomePage
          capabilities={new Set(["work_record.read"])}
          availableRoutes={new Set(["home", "work"])}
          clientID="10000000-0000-4000-8000-000000000001"
          principalID="technician-1"
          onNavigate={vi.fn()}
        />
      </WorkspaceProvider>,
    );

    const title = await screen.findByText(
      "Password resets failing after policy rollout",
    );
    fireEvent.doubleClick(title.closest("button")!);
    expect(openRecord).toHaveBeenCalledWith(
      expect.objectContaining({
        id: expect.stringMatching(/^ticket:/),
        routeID: "work",
        entityType: "ticket",
      }),
    );
  });

  it("shows honest technician entry points without fabricated live counts", () => {
    render(
      <HomePage
        capabilities={new Set(["work_record.read", "work_record.update"])}
        availableRoutes={new Set(["home", "work", "ai-assist"])}
        principalID="technician-1"
        onNavigate={vi.fn()}
      />,
    );

    expect(screen.getByRole("heading", { name: "Live work" })).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Open Triage and update work" }),
    ).toBeVisible();
    expect(screen.queryByText("SLA attention")).not.toBeInTheDocument();
    expect(screen.queryByText(/systems operational/i)).not.toBeInTheDocument();
  });

  it("shows authorized operational controls and links only to live sources", () => {
    const navigate = vi.fn();
    render(
      <HomePage
        capabilities={new Set(["work_record.read", "audit.read"])}
        availableRoutes={new Set(["home", "work", "operations", "audit"])}
        principalID="technician-1"
        onNavigate={navigate}
      />,
    );

    expect(
      screen.getByRole("heading", { name: "Platform health" }),
    ).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: /View live health/i }));
    expect(navigate).toHaveBeenCalledWith("operations");
    expect(
      screen.queryByRole("button", { name: /Manage the pipeline/i }),
    ).not.toBeInTheDocument();
  });

  it("opens a selected role-aware workspace", () => {
    const navigate = vi.fn();
    render(
      <HomePage
        capabilities={new Set(["work_record.read"])}
        availableRoutes={new Set(["home", "work", "knowledge"])}
        principalID="technician-1"
        onNavigate={navigate}
      />,
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Open Search knowledge" }),
    );
    expect(navigate).toHaveBeenCalledWith("knowledge");
  });

  it("renders authoritative work and operational feeds without invented data", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input);
        if (url.startsWith("/api/v1/work-records")) {
          return Response.json([
            {
              ID: "record-1",
              DisplayID: "INC-1048",
              Type: "incident",
              Title: "Mail flow delayed",
              Description: "",
              Status: "open",
              Priority: "critical",
              QueueID: "queue-1",
              PrimaryOwnerID: "technician-1",
              UpdatedAt: "2026-08-05T08:00:00Z",
              Version: 3,
            },
          ]);
        }
        if (url === "/api/v1/integrations/health") {
          return Response.json({
            state: "healthy",
            generated_at: "2026-08-05T08:00:00Z",
            connections: [{ id: "graph-1", state: "healthy" }],
          });
        }
        if (url.includes("/time-entries/approvals")) {
          return Response.json([{ id: "time-1", approval_state: "pending" }]);
        }
        if (url.includes("/admin/audit")) {
          return Response.json([
            {
              id: "audit-1",
              occurred_at: "2026-08-05T08:00:00Z",
              action: "work.updated",
            },
          ]);
        }
        return new Response("not found", { status: 404 });
      }),
    );

    render(
      <HomePage
        capabilities={new Set(["audit.read"])}
        availableRoutes={
          new Set(["home", "work", "operations", "billing", "audit"])
        }
        clientID="client-1"
        principalID="technician-1"
        onNavigate={vi.fn()}
      />,
    );

    expect(await screen.findByText(/INC-1048/)).toBeVisible();
    expect(screen.getByText(/Assigned to you/)).toBeVisible();
    await waitFor(() => {
      expect(screen.getByText("healthy")).toBeVisible();
      expect(screen.getByText("1 time entry needs review.")).toBeVisible();
      expect(screen.getByText("1 recent event available.")).toBeVisible();
    });
    expect(
      screen.getByText(/Calendar access is not available for your role/i),
    ).toBeVisible();
  });

  it("removes prior-client work before the next client request settles", async () => {
    let resolveSecond: ((response: Response) => void) | undefined;
    const second = new Promise<Response>((resolve) => {
      resolveSecond = resolve;
    });
    let requests = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(() => {
        requests += 1;
        if (requests === 1) {
          return Promise.resolve(
            Response.json([
              {
                ID: "record-1",
                DisplayID: "INC-OLD",
                Type: "incident",
                Title: "Prior client record",
                Description: "",
                Status: "open",
                Priority: "high",
                QueueID: "queue-1",
                PrimaryOwnerID: "",
                UpdatedAt: "2026-08-05T08:00:00Z",
                Version: 1,
              },
            ]),
          );
        }
        return second;
      }),
    );

    const view = render(
      <HomePage
        capabilities={new Set()}
        availableRoutes={new Set(["home", "work"])}
        clientID="client-1"
        principalID="technician-1"
        onNavigate={vi.fn()}
      />,
    );
    expect(await screen.findByText(/INC-OLD/)).toBeVisible();

    view.rerender(
      <HomePage
        capabilities={new Set()}
        availableRoutes={new Set(["home", "work"])}
        clientID="client-2"
        principalID="technician-1"
        onNavigate={vi.fn()}
      />,
    );

    expect(screen.queryByText(/INC-OLD/)).not.toBeInTheDocument();
    expect(screen.getByText("Loading live work…")).toBeVisible();
    resolveSecond?.(Response.json([]));
  });
});
