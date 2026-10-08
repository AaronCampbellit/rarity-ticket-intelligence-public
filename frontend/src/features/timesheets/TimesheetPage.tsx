import { CustomDateEditor } from "../calendar/CustomDateEditor";
import { type FormEvent, useCallback, useEffect, useState } from "react";

import { csrfHeaders } from "../../api/browserSession";
import { clientContextHeaders } from "../../api/clientContext";
import { Notice, Page, StatePanel } from "../../design-system";
import "./timesheets.css";

type TimeEntryWire = {
  ID: string;
  MSPID: string;
  ClientID: string;
  WorkRecordID: string;
  TaskID: string;
  TechnicianID: string;
  StartedAt: string;
  EndedAt: string;
  DurationSeconds: number;
  Billable: boolean;
  Note: string;
  LaborRoleVersionID: string;
  InternalCostMinor: number;
  BillRateMinor: number;
  RateCurrency: string;
  Version: number;
  CreatedAt: string;
  CreatedBy: string;
};

type TimesheetRow = {
  entry: TimeEntryWire;
  client_name: string;
  work_item_title: string;
  labor_role_name: string;
  approval_state: "pending" | "approved" | "rejected";
  reversed_at?: string;
  replacement_time_entry_id?: string;
  last_amendment?: {
    prior_version: number;
    resulting_version: number;
    before_values: Record<string, unknown>;
    after_values: Record<string, unknown>;
    reason: string;
    amended_at: string;
    amended_by: string;
  };
};

type Timesheet = {
  week: {
    starts_at: string;
    ends_at: string;
    timezone: string;
  };
  technician_id: string;
  rows: TimesheetRow[];
  total_seconds: number;
  billable_seconds: number;
  nonbillable_seconds: number;
};

export function TimesheetPage({
  clientID,
  capabilities,
  selectedTimeEntryID,
}: {
  clientID: string;
  capabilities: Set<string>;
  selectedTimeEntryID?: string;
}) {
  const [timesheet, setTimesheet] = useState<Timesheet>();
  const [technicianID, setTechnicianID] = useState("");
  const [editingID, setEditingID] = useState("");
  const [state, setState] = useState<"loading" | "ready" | "saving" | "error">(
    "loading",
  );
  const [message, setMessage] = useState("");
  const [directEntry, setDirectEntry] = useState<TimesheetRow>();
  const canReview = capabilities.has("timesheet.review");
  const canApprove = capabilities.has("time_entry.approve");
  const canAmend =
    capabilities.has("time_entry.update_own") ||
    capabilities.has("time_entry.amend");
  const displayedRows = [
    ...(directEntry ? [directEntry] : []),
    ...(timesheet?.rows ?? []),
  ];

  const load = useCallback(
    async (selectedTechnicianID: string, signal?: AbortSignal) => {
      const query = new URLSearchParams();
      if (selectedTechnicianID.trim()) {
        query.set("technician_id", selectedTechnicianID.trim());
      }
      const suffix = query.size ? `?${query.toString()}` : "";
      const response = await fetch(`/api/v1/timesheets/week${suffix}`, {
        credentials: "same-origin",
        headers: clientContextHeaders(clientID),
        signal,
      });
      if (!response.ok) throw new Error("timesheet_unavailable");
      setTimesheet((await response.json()) as Timesheet);
      setState("ready");
    },
    [clientID],
  );

  useEffect(() => {
    if (!clientID) return;
    const controller = new AbortController();
    setState("loading");
    void load("", controller.signal).catch(() => {
      if (!controller.signal.aborted) setState("error");
    });
    return () => controller.abort();
  }, [clientID, load]);

  useEffect(() => {
    setDirectEntry(undefined);
    if (
      !clientID ||
      !selectedTimeEntryID ||
      timesheet?.rows.some((row) => row.entry.ID === selectedTimeEntryID)
    )
      return;
    const controller = new AbortController();
    void fetch(
      `/api/v1/time-entries/${encodeURIComponent(selectedTimeEntryID)}`,
      {
        credentials: "same-origin",
        headers: clientContextHeaders(clientID),
        signal: controller.signal,
      },
    )
      .then(async (response) => {
        if (!response.ok) throw new Error("time_entry_unavailable");
        const exact = (await response.json()) as TimesheetRow;
        if (!controller.signal.aborted) setDirectEntry(exact);
      })
      .catch(() => {
        if (!controller.signal.aborted) setState("error");
      });
    return () => controller.abort();
  }, [clientID, selectedTimeEntryID, timesheet]);

  async function saveCorrection(
    row: TimesheetRow,
    event: FormEvent<HTMLFormElement>,
    reverse: boolean,
  ) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const values = {
      started_at: new Date(String(form.get("started_at"))).toISOString(),
      ended_at: new Date(String(form.get("ended_at"))).toISOString(),
      billable: form.get("billable") === "on",
      note: String(form.get("note") ?? ""),
    };
    const body = reverse
      ? {
          expected_version: row.entry.Version,
          reason: String(form.get("reason") ?? ""),
          replacement: values,
        }
      : {
          expected_version: row.entry.Version,
          ...values,
          reason: String(form.get("reason") ?? ""),
        };
    setState("saving");
    setMessage("");
    try {
      const action = reverse ? "reverse" : "amend";
      const response = await fetch(
        `/api/v1/time-entries/${encodeURIComponent(row.entry.ID)}:${action}`,
        {
          method: "POST",
          credentials: "same-origin",
          headers: {
            "Content-Type": "application/json",
            ...clientContextHeaders(clientID),
            ...csrfHeaders(),
          },
          body: JSON.stringify(body),
        },
      );
      if (!response.ok) throw new Error("timesheet_mutation_failed");
      setEditingID("");
      await load(technicianID);
      setMessage(
        reverse
          ? "Approved time reversed and replacement created."
          : "Time entry corrected.",
      );
    } catch {
      setState("error");
    }
  }

  async function decide(
    row: TimesheetRow,
    decision: "approved" | "rejected",
    reason: string,
  ) {
    setState("saving");
    setMessage("");
    try {
      const response = await fetch(
        `/api/v1/time-entries/${encodeURIComponent(row.entry.ID)}/approval`,
        {
          method: "POST",
          credentials: "same-origin",
          headers: {
            "Content-Type": "application/json",
            ...clientContextHeaders(clientID),
            ...csrfHeaders(),
          },
          body: JSON.stringify({
            expected_version: row.entry.Version,
            decision,
            reason,
          }),
        },
      );
      if (!response.ok) throw new Error("timesheet_decision_failed");
      await load(technicianID);
      setMessage(`Time entry ${decision}.`);
    } catch {
      setState("error");
    }
  }

  return (
    <Page
      eyebrow="Workforce time"
      title="Timesheets"
      description="Review the active Client's weekly time in the MSP calendar timezone."
    >
      <div className="timesheet-page">
        {canReview ? (
          <form
            className="timesheet-review-filter"
            onSubmit={(event) => {
              event.preventDefault();
              setState("loading");
              void load(technicianID).catch(() => setState("error"));
            }}
          >
            <label>
              <span>Technician ID</span>
              <input
                value={technicianID}
                onChange={(event) => setTechnicianID(event.target.value)}
                required
              />
            </label>
            <button type="submit">Load technician week</button>
          </form>
        ) : null}
        {state === "loading" ? (
          <StatePanel
            state="loading"
            title="Loading weekly time"
            description="Calculating the current MSP week."
          />
        ) : null}
        {state === "error" ? (
          <StatePanel
            state="error"
            title="Timesheet is unavailable"
            description="Verify the active Client, technician scope, and entry state."
            supportCode="TIMESHEET-WEEK"
          />
        ) : null}
        {message ? (
          <Notice tone="success" title="Timesheet updated">
            {message}
          </Notice>
        ) : null}
        {timesheet ? (
          <>
            <section className="timesheet-summary" aria-label="Weekly summary">
              <div>
                <strong>{formatDuration(timesheet.total_seconds)} total</strong>
                <span>
                  {formatDuration(timesheet.billable_seconds)} billable
                </span>
              </div>
              <div>
                <strong>{formatWeek(timesheet)}</strong>
                <span>{timesheet.week.timezone}</span>
              </div>
            </section>
            <section className="timesheet-day-totals" aria-label="Daily totals">
              {dailyTotals(timesheet).map((day) => (
                <span key={day.key}>
                  {day.label}: {formatDuration(day.seconds)}
                </span>
              ))}
            </section>
            {!displayedRows.length ? (
              <p>No time has been recorded for this week.</p>
            ) : (
              <div className="table-scroll">
                <table className="timesheet-table">
                  <thead>
                    <tr>
                      <th scope="col">Day</th>
                      <th scope="col">Client / work</th>
                      <th scope="col">Labor role</th>
                      <th scope="col">Duration</th>
                      <th scope="col">State</th>
                      <th scope="col">Actions</th>
                    </tr>
                  </thead>
                  <tbody>
                    {displayedRows.map((row) => (
                      <tr
                        key={row.entry.ID}
                        id={`time-entry-${row.entry.ID}`}
                        aria-current={
                          row.entry.ID === selectedTimeEntryID
                            ? "true"
                            : undefined
                        }
                      >
                        <td>
                          {new Date(row.entry.StartedAt).toLocaleDateString()}
                        </td>
                        <td>
                          <strong>{row.work_item_title}</strong>
                          <span>{row.client_name}</span>
                          <small>{row.entry.Note || "No work note"}</small>
                          <CustomDateEditor
                            objectType="time_entry"
                            objectID={row.entry.ID}
                            clientID={row.entry.ClientID}
                          />
                          {row.last_amendment ? (
                            <aside className="timesheet-amendment">
                              <strong>Last correction</strong>
                              <span>{row.last_amendment.reason}</span>
                              <small>
                                {formatDurationChange(row.last_amendment)}
                              </small>
                            </aside>
                          ) : null}
                        </td>
                        <td>{row.labor_role_name || "Unassigned"}</td>
                        <td>{formatDuration(row.entry.DurationSeconds)}</td>
                        <td>{row.approval_state}</td>
                        <td>
                          {editingID === row.entry.ID ? (
                            <CorrectionForm
                              row={row}
                              reverse={row.approval_state === "approved"}
                              onCancel={() => setEditingID("")}
                              onSubmit={(event, reverse) =>
                                void saveCorrection(row, event, reverse)
                              }
                            />
                          ) : (
                            <div className="timesheet-actions">
                              {canAmend &&
                              (row.approval_state === "pending" ||
                                row.approval_state === "approved") ? (
                                <button
                                  type="button"
                                  onClick={() => setEditingID(row.entry.ID)}
                                >
                                  {row.approval_state === "approved"
                                    ? "Reverse time"
                                    : "Edit time"}
                                </button>
                              ) : null}
                              {canReview &&
                              canApprove &&
                              row.approval_state === "pending" ? (
                                <ReviewForm row={row} onDecide={decide} />
                              ) : null}
                            </div>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </>
        ) : null}
      </div>
    </Page>
  );
}

function CorrectionForm({
  row,
  reverse,
  onCancel,
  onSubmit,
}: {
  row: TimesheetRow;
  reverse: boolean;
  onCancel: () => void;
  onSubmit: (event: FormEvent<HTMLFormElement>, reverse: boolean) => void;
}) {
  return (
    <form
      className="timesheet-entry-form"
      onSubmit={(event) => onSubmit(event, reverse)}
    >
      <label>
        <span>Started</span>
        <input
          name="started_at"
          type="datetime-local"
          defaultValue={toLocalInput(row.entry.StartedAt)}
          required
        />
      </label>
      <label>
        <span>Ended</span>
        <input
          name="ended_at"
          type="datetime-local"
          defaultValue={toLocalInput(row.entry.EndedAt)}
          required
        />
      </label>
      <label>
        <span>Work note</span>
        <input name="note" defaultValue={row.entry.Note} />
      </label>
      <label>
        <span>{reverse ? "Reversal reason" : "Correction reason"}</span>
        <input name="reason" required />
      </label>
      <label className="timesheet-check">
        <input
          name="billable"
          type="checkbox"
          defaultChecked={row.entry.Billable}
        />
        Billable
      </label>
      <button type="submit">
        {reverse ? "Reverse and replace" : "Save correction"}
      </button>
      <button type="button" onClick={onCancel}>
        Cancel
      </button>
    </form>
  );
}

function ReviewForm({
  row,
  onDecide,
}: {
  row: TimesheetRow;
  onDecide: (
    row: TimesheetRow,
    decision: "approved" | "rejected",
    reason: string,
  ) => Promise<void>;
}) {
  return (
    <form
      className="timesheet-review-form"
      onSubmit={(event) => {
        event.preventDefault();
        const data = new FormData(event.currentTarget);
        const submitter = (event.nativeEvent as SubmitEvent)
          .submitter as HTMLButtonElement;
        void onDecide(
          row,
          submitter.value as "approved" | "rejected",
          String(data.get("reason") ?? ""),
        );
      }}
    >
      <label>
        <span>Review reason</span>
        <input name="reason" required />
      </label>
      <button type="submit" value="approved">
        Approve time
      </button>
      <button type="submit" value="rejected">
        Reject time
      </button>
    </form>
  );
}

function toLocalInput(value: string): string {
  const date = new Date(value);
  const offset = date.getTimezoneOffset() * 60_000;
  return new Date(date.getTime() - offset).toISOString().slice(0, 16);
}

function formatWeek(timesheet: Timesheet): string {
  const starts = new Date(timesheet.week.starts_at).toLocaleDateString();
  const ends = new Date(
    new Date(timesheet.week.ends_at).getTime() - 1,
  ).toLocaleDateString();
  return `${starts} – ${ends}`;
}

function dailyTotals(
  timesheet: Timesheet,
): Array<{ key: string; label: string; seconds: number }> {
  const dateFormatter = new Intl.DateTimeFormat("en-US", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    timeZone: timesheet.week.timezone,
  });
  const dayFormatter = new Intl.DateTimeFormat("en-US", {
    weekday: "long",
    timeZone: timesheet.week.timezone,
  });
  const totals = new Map<
    string,
    { key: string; label: string; seconds: number }
  >();
  for (const row of timesheet.rows) {
    if (row.reversed_at) continue;
    const startedAt = new Date(row.entry.StartedAt);
    const key = dateFormatter.format(startedAt);
    const current = totals.get(key) ?? {
      key,
      label: dayFormatter.format(startedAt),
      seconds: 0,
    };
    current.seconds += row.entry.DurationSeconds;
    totals.set(key, current);
  }
  return [...totals.values()];
}

function formatDuration(seconds: number): string {
  const minutes = Math.round(seconds / 60);
  const hours = Math.floor(minutes / 60);
  const remainder = minutes % 60;
  if (hours === 0) return `${remainder}m`;
  if (remainder === 0) return `${hours}h`;
  return `${hours}h ${remainder}m`;
}

function formatDurationChange(
  amendment: NonNullable<TimesheetRow["last_amendment"]>,
): string {
  const before = Number(amendment.before_values.duration_seconds ?? 0);
  const after = Number(amendment.after_values.duration_seconds ?? 0);
  return `${formatDuration(before)} → ${formatDuration(after)}`;
}
