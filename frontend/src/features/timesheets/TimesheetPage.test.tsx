import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { TimesheetPage } from "./TimesheetPage";

const entry = {
  entry: {
    ID: "entry-1",
    MSPID: "msp-1",
    ClientID: "client-1",
    WorkRecordID: "ticket-1",
    TaskID: "",
    TechnicianID: "technician-1",
    StartedAt: "2026-08-04T18:00:00Z",
    EndedAt: "2026-08-04T19:30:00Z",
    DurationSeconds: 5400,
    Billable: true,
    Note: "Investigated VPN outage",
    LaborRoleVersionID: "role-version-1",
    InternalCostMinor: 3500,
    BillRateMinor: 12500,
    RateCurrency: "USD",
    Version: 2,
    CreatedAt: "2026-08-04T19:30:00Z",
    CreatedBy: "technician-1",
  },
  client_name: "Campbell Co",
  work_item_title: "VPN unavailable",
  labor_role_name: "Service Desk",
  approval_state: "pending",
};

function week(rows = [entry]) {
  return {
    week: {
      starts_at: "2026-08-03T05:00:00Z",
      ends_at: "2026-08-10T05:00:00Z",
      timezone: "America/Chicago",
    },
    technician_id: "technician-1",
    rows,
    total_seconds: 5400,
    billable_seconds: 5400,
    nonbillable_seconds: 0,
  };
}

describe("TimesheetPage", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("shows weekly totals and amends pending time with reasoned evidence", async () => {
    let mutationBody: Record<string, unknown> | undefined;
    const fetcher = vi.fn(
      async (_input: RequestInfo | URL, init?: RequestInit) => {
        if (init?.method === "POST") {
          mutationBody = JSON.parse(String(init.body)) as Record<
            string,
            unknown
          >;
          return Response.json(
            {
              ...entry,
              entry: { ...entry.entry, Version: 3, Note: "Corrected note" },
            },
            { status: 200 },
          );
        }
        return Response.json(week());
      },
    );
    vi.stubGlobal("fetch", fetcher);

    render(
      <TimesheetPage
        clientID="client-1"
        capabilities={new Set(["timesheet.read_own", "time_entry.update_own"])}
      />,
    );

    expect(await screen.findByText("VPN unavailable")).toBeVisible();
    expect(screen.getByText("1h 30m total")).toBeVisible();
    expect(screen.getByText("America/Chicago")).toBeVisible();
    expect(screen.getByText("Tuesday: 1h 30m")).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "Edit time" }));
    fireEvent.change(screen.getByRole("textbox", { name: "Work note" }), {
      target: { value: "Corrected note" },
    });
    fireEvent.change(
      screen.getByRole("textbox", { name: "Correction reason" }),
      {
        target: { value: "Corrected ticket notes" },
      },
    );
    fireEvent.click(screen.getByRole("button", { name: "Save correction" }));

    expect(await screen.findByText("Time entry corrected.")).toBeVisible();
    expect(mutationBody).toMatchObject({
      expected_version: 2,
      billable: true,
      note: "Corrected note",
      reason: "Corrected ticket notes",
    });
    expect(JSON.stringify(mutationBody)).not.toMatch(
      /duration_seconds|internal_cost|bill_rate/,
    );
  });

  it("direct-loads a selected time entry outside the current week", async () => {
    const offWeek = {
      ...entry,
      entry: { ...entry.entry, ID: "off-week" },
      work_item_title: "Historical migration",
    };
    const fetcher = vi.fn(async (input: RequestInfo | URL) =>
      String(input).endsWith("/time-entries/off-week")
        ? Response.json(offWeek)
        : Response.json(week([])),
    );
    vi.stubGlobal("fetch", fetcher);
    render(
      <TimesheetPage
        clientID="client-1"
        capabilities={new Set(["timesheet.read_own"])}
        selectedTimeEntryID="off-week"
      />,
    );
    expect(await screen.findByText("Historical migration")).toBeInTheDocument();
    expect(
      screen.getByRole("row", { name: /Historical migration/ }),
    ).toHaveAttribute("aria-current", "true");
  });

  it("lets a scoped reviewer approve a technician week", async () => {
    let approvalBody: Record<string, unknown> | undefined;
    const reviewedEntry = {
      ...entry,
      last_amendment: {
        id: "amendment-1",
        time_entry_id: "entry-1",
        msp_id: "msp-1",
        client_id: "client-1",
        prior_version: 1,
        resulting_version: 2,
        before_values: { duration_seconds: 3600 },
        after_values: { duration_seconds: 5400 },
        reason: "Corrected captured duration",
        amended_at: "2026-08-04T20:00:00Z",
        amended_by: "reviewer-1",
      },
    };
    const fetcher = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (url.endsWith("/approval") && init?.method === "POST") {
          approvalBody = JSON.parse(String(init.body)) as Record<
            string,
            unknown
          >;
          return Response.json(
            {
              id: "entry-1",
              version: 3,
              approval_state: "approved",
            },
            { status: 200 },
          );
        }
        return Response.json(week([reviewedEntry]));
      },
    );
    vi.stubGlobal("fetch", fetcher);

    render(
      <TimesheetPage
        clientID="client-1"
        capabilities={
          new Set([
            "timesheet.review",
            "time_entry.approve",
            "time_entry.amend",
          ])
        }
      />,
    );

    await screen.findByText("VPN unavailable");
    expect(screen.getByText("Corrected captured duration")).toBeVisible();
    expect(screen.getByText("1h → 1h 30m")).toBeVisible();
    fireEvent.change(screen.getByRole("textbox", { name: "Technician ID" }), {
      target: { value: "technician-2" },
    });
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(fetcher).toHaveBeenCalledTimes(1);
    fireEvent.change(screen.getByRole("textbox", { name: "Review reason" }), {
      target: { value: "Matches support activity" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Approve time" }));

    await waitFor(() =>
      expect(screen.getByText("Time entry approved.")).toBeVisible(),
    );
    expect(approvalBody).toEqual({
      expected_version: 2,
      decision: "approved",
      reason: "Matches support activity",
    });
  });
});
