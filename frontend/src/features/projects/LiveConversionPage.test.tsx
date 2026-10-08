import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { LiveConversionPage } from "./LiveConversionPage";

describe("LiveConversionPage", () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("binds selected incomplete tasks to the preview and conversion requests", async () => {
    const onConverted = vi.fn();
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input);
        if (url.includes("/api/v1/proposals?")) {
          return Response.json([
            {
              id: "proposal-1",
              opportunity_id: "opportunity-1",
              display_id: "PROP-100",
              current_version: 2,
              current_version_id: "version-2",
              state: "accepted",
              version: 3,
              updated_at: "2026-07-30T12:00:00Z",
            },
          ]);
        }
        if (url.includes("/api/v1/opportunities?")) {
          return Response.json([
            {
              id: "opportunity-1",
              pipeline_id: "pipeline-1",
              stage_id: "stage-1",
              display_id: "OPP-100",
              name: "Northwind rollout",
              amount: { minor: 120000, currency: "USD" },
              fields: {},
              proposal_issued: true,
              approval_granted: true,
              version: 4,
              updated_at: "2026-07-30T12:00:00Z",
            },
          ]);
        }
        if (url.includes("/api/v1/proposal-versions/version-2")) {
          return Response.json({
            id: "version-2",
            proposal_id: "proposal-1",
            version: 2,
            state: "accepted",
            currency: "USD",
            lines: [
              {
                id: "line-1",
                type: "fixed_fee",
                description: "Delivery",
                planned_minutes: 600,
              },
            ],
          });
        }
        if (url.includes("/api/v1/opportunities/opportunity-1/tasks")) {
          return Response.json([
            {
              id: "kickoff",
              title: "Schedule kickoff",
              status: "open",
              position: 1,
              version: 2,
            },
            {
              id: "qualified",
              title: "Qualify prospect",
              status: "completed",
              position: 2,
              version: 3,
            },
          ]);
        }
        if (url.includes("/conversion-preview")) {
          return Response.json({
            hash: "preview-hash",
            opportunity_id: "opportunity-1",
            proposal_version_id: "version-2",
            client: {
              action: "match",
              client_id: "client-1",
              name: "Northwind",
            },
            project_display_id: "PRJ-PROP-100",
            project_name: "Northwind rollout",
            phases: [
              {
                position: 1,
                name: "Delivery",
                proposal_line_ids: ["line-1"],
                planned_minutes: 600,
                budget: { minor: 120000, currency: "USD" },
              },
            ],
            tasks: [{ id: "kickoff", version: 2 }],
            original_budget: { minor: 120000, currency: "USD" },
            planned_minutes: 600,
          });
        }
        if (url.endsWith("/convert")) {
          return Response.json(
            {
              project_id: "project-1",
              client_id: "client-1",
              conversion_id: "conversion-1",
            },
            { status: 201 },
          );
        }
        return new Response(null, { status: 404 });
      }),
    );

    render(
      <LiveConversionPage clientID="client-1" onConverted={onConverted} />,
    );
    await screen.findByRole("option", { name: "PROP-100 · Version 2" });
    await screen.findByText(
      "1 proposal lines will be mapped to the initial phase.",
    );
    const kickoff = await screen.findByRole("checkbox", {
      name: "Schedule kickoff",
    });
    expect(kickoff).toBeEnabled();
    expect(kickoff).not.toBeChecked();
    expect(
      screen.getByRole("checkbox", { name: "Qualify prospect" }),
    ).toBeDisabled();
    fireEvent.click(kickoff);
    fireEvent.click(screen.getByRole("button", { name: "Preview conversion" }));

    await screen.findByRole("heading", { name: "Northwind rollout" });
    expect(
      screen.getByText("Every Proposal Line is assigned exactly once."),
    ).toBeInTheDocument();
    const previewCall = vi
      .mocked(fetch)
      .mock.calls.find(([url]) => String(url).includes("/conversion-preview"));
    const previewBody = JSON.parse(String(previewCall?.[1]?.body));
    expect(previewBody).toEqual(
      expect.objectContaining({
        selected_task_ids: ["kickoff"],
        task_versions: { kickoff: 2 },
        phases: [expect.objectContaining({ proposal_line_ids: ["line-1"] })],
      }),
    );
    expect(
      screen.getByRole("checkbox", { name: "Schedule kickoff" }),
    ).toBeChecked();
    expect(
      screen.getByRole("checkbox", { name: "Schedule kickoff" }),
    ).toBeDisabled();

    fireEvent.click(screen.getByRole("button", { name: "Create project" }));
    await waitFor(() => expect(onConverted).toHaveBeenCalledWith("project-1"));
    const conversionCall = vi
      .mocked(fetch)
      .mock.calls.find(([url]) => String(url).endsWith("/convert"));
    const conversionBody = JSON.parse(String(conversionCall?.[1]?.body));
    const {
      idempotency_key: idempotencyKey,
      preview_hash: previewHash,
      ...convertedRequest
    } = conversionBody;
    expect(idempotencyKey).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i,
    );
    expect(previewHash).toBe("preview-hash");
    expect(convertedRequest).toEqual(previewBody);
  });

  it("resets selection and ignores an aborted stale task load when proposals change", async () => {
    let resolveSecondTasks: (response: Response) => void = () => undefined;
    let secondTaskSignal: AbortSignal | null = null;
    let firstTaskLoads = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (url.includes("/api/v1/proposals?")) {
          return Response.json([
            {
              id: "proposal-1",
              opportunity_id: "opportunity-1",
              display_id: "PROP-100",
              current_version: 1,
              current_version_id: "version-1",
              state: "accepted",
              version: 2,
              updated_at: "2026-07-30T12:00:00Z",
            },
            {
              id: "proposal-2",
              opportunity_id: "opportunity-2",
              display_id: "PROP-200",
              current_version: 1,
              current_version_id: "version-2",
              state: "accepted",
              version: 2,
              updated_at: "2026-07-30T12:00:00Z",
            },
          ]);
        }
        if (url.includes("/api/v1/opportunities?")) {
          return Response.json([
            {
              id: "opportunity-1",
              pipeline_id: "pipeline-1",
              stage_id: "stage-1",
              display_id: "OPP-100",
              name: "First opportunity",
              amount: { minor: 10000, currency: "USD" },
              fields: {},
              proposal_issued: true,
              approval_granted: true,
              version: 1,
              updated_at: "2026-07-30T12:00:00Z",
            },
            {
              id: "opportunity-2",
              pipeline_id: "pipeline-1",
              stage_id: "stage-1",
              display_id: "OPP-200",
              name: "Second opportunity",
              amount: { minor: 20000, currency: "USD" },
              fields: {},
              proposal_issued: true,
              approval_granted: true,
              version: 1,
              updated_at: "2026-07-30T12:00:00Z",
            },
          ]);
        }
        if (url.includes("/api/v1/proposal-versions/")) {
          return Response.json({
            id: url.endsWith("version-2") ? "version-2" : "version-1",
            proposal_id: url.endsWith("version-2")
              ? "proposal-2"
              : "proposal-1",
            version: 1,
            state: "accepted",
            currency: "USD",
            lines: [],
          });
        }
        if (url.includes("/opportunities/opportunity-1/tasks")) {
          firstTaskLoads += 1;
          return Response.json([
            {
              id: "shared-task",
              title: firstTaskLoads === 1 ? "Old task" : "Fresh task",
              status: "open",
              position: 1,
              version: firstTaskLoads === 1 ? 1 : 4,
            },
          ]);
        }
        if (url.includes("/opportunities/opportunity-2/tasks")) {
          secondTaskSignal = init?.signal ?? null;
          return new Promise<Response>((resolve) => {
            resolveSecondTasks = resolve;
          });
        }
        return new Response(null, { status: 404 });
      }),
    );

    render(<LiveConversionPage clientID="client-1" onConverted={vi.fn()} />);
    const oldTask = await screen.findByRole("checkbox", { name: "Old task" });
    fireEvent.click(oldTask);
    expect(oldTask).toBeChecked();

    fireEvent.change(screen.getByLabelText("Accepted proposal"), {
      target: { value: "proposal-2" },
    });
    await waitFor(() => expect(secondTaskSignal).not.toBeNull());
    expect(screen.queryByRole("checkbox", { name: "Old task" })).toBeNull();

    fireEvent.change(screen.getByLabelText("Accepted proposal"), {
      target: { value: "proposal-1" },
    });
    await waitFor(() => expect(secondTaskSignal?.aborted).toBe(true));
    const freshTask = await screen.findByRole("checkbox", {
      name: "Fresh task",
    });
    expect(freshTask).not.toBeChecked();

    await act(async () => {
      resolveSecondTasks(
        Response.json([
          {
            id: "stale-task",
            title: "Stale task",
            status: "open",
            position: 1,
            version: 9,
          },
        ]),
      );
    });
    expect(screen.queryByRole("checkbox", { name: "Stale task" })).toBeNull();
    expect(
      screen.getByRole("checkbox", { name: "Fresh task" }),
    ).not.toBeChecked();
  });
});
