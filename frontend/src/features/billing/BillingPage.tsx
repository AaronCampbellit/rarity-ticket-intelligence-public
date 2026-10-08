import {
  type FormEvent,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";

import { csrfHeaders } from "../../api/browserSession";
import { clientContextHeaders } from "../../api/clientContext";
import {
  DateRangePicker,
  Notice,
  Page,
  StatePanel,
  StatusBadge,
} from "../../design-system";
import "../operations/operations.css";
import { DeferredObjectTagEditor } from "../classification/ObjectTagEditor";
import { ObjectTagSummary } from "../classification/ObjectTagSummary";
import { createClassificationAPI } from "../classification/api";
import {
  classificationResponseError,
  isClassificationCreateError,
  type ClassificationAPIError,
} from "../classification/api";
import "./billing.css";

type ApprovalEntry = {
  id: string;
  work_record_id: string;
  technician_id: string;
  duration_seconds: number;
  billable: boolean;
  approval_state: "pending" | "approved" | "rejected";
  version: number;
  started_at: string;
  ended_at: string;
  note: string;
};

export function BillingPage({
  clientID,
  capabilities,
}: {
  clientID: string;
  capabilities: Set<string>;
}) {
  const [entries, setEntries] = useState<ApprovalEntry[]>([]);
  const [loadedClientID, setLoadedClientID] = useState("");
  const [state, setState] = useState<"loading" | "ready" | "saving" | "error">(
    "loading",
  );
  const [message, setMessage] = useState("");
  const [exportRange, setExportRange] = useState({ start: "", end: "" });
  const [classificationRecovery, setClassificationRecovery] = useState<{
    entryID: string;
    request: number;
    message: string;
  }>();
  const clientScopeRef = useRef(clientID);
  clientScopeRef.current = clientID;
  const canApprove = capabilities.has("time_entry.approve");
  const canExport = capabilities.has("time_entry.export");

  useEffect(() => {
    setClassificationRecovery(undefined);
    setMessage("");
    setEntries([]);
    setLoadedClientID("");
    setState("loading");
  }, [clientID]);

  const load = useCallback(
    async (requestedClientID: string, signal?: AbortSignal) => {
      const response = await fetch(
        "/api/v1/time-entries/approvals?state=pending&limit=100",
        {
          credentials: "same-origin",
          headers: clientContextHeaders(requestedClientID),
          signal,
        },
      );
      if (!response.ok) throw new Error("Approval queue unavailable");
      const found = (await response.json()) as ApprovalEntry[];
      if (requestedClientID !== clientScopeRef.current) return;
      setEntries(found);
      setLoadedClientID(requestedClientID);
      setState("ready");
    },
    [],
  );

  useEffect(() => {
    const controller = new AbortController();
    void load(clientID, controller.signal).catch(() => {
      if (!controller.signal.aborted) setState("error");
    });
    return () => controller.abort();
  }, [clientID, load]);

  async function decide(
    entry: ApprovalEntry,
    decision: "approved" | "rejected",
    reason: FormDataEntryValue | null,
  ) {
    const decisionClientID = clientScopeRef.current;
    setState("saving");
    setMessage("");
    setClassificationRecovery(undefined);
    try {
      const response = await fetch(
        `/api/v1/time-entries/${encodeURIComponent(entry.id)}/approval`,
        {
          method: "POST",
          credentials: "same-origin",
          headers: {
            "Content-Type": "application/json",
            ...clientContextHeaders(clientID),
            ...csrfHeaders(),
          },
          body: JSON.stringify({
            expected_version: entry.version,
            decision,
            reason,
          }),
        },
      );
      if (!response.ok)
        await classificationResponseError(response, "approval_failed");
      if (decisionClientID !== clientScopeRef.current) return;
      await load(decisionClientID);
      if (decisionClientID !== clientScopeRef.current) return;
      setMessage(`Time entry ${decision}.`);
    } catch (cause) {
      if (decisionClientID !== clientScopeRef.current) return;
      if (isClassificationCreateError(cause)) {
        setState("ready");
        setClassificationRecovery({
          entryID: entry.id,
          request: Date.now(),
          message:
            "Classification is required before approval. Update this time entry and try again.",
        });
      } else setState("error");
    }
  }

  async function exportCSV(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const from = exportRange.start;
    const through = exportRange.end;
    const exportClientID = clientScopeRef.current;
    setState("saving");
    setMessage("");
    setClassificationRecovery(undefined);
    try {
      const response = await fetch("/api/v1/billing-exports", {
        method: "POST",
        credentials: "same-origin",
        headers: {
          "Content-Type": "application/json",
          ...clientContextHeaders(clientID),
          ...csrfHeaders(),
        },
        body: JSON.stringify({
          from: new Date(`${from}T00:00:00.000Z`).toISOString(),
          through: new Date(`${through}T00:00:00.000Z`).toISOString(),
        }),
      });
      if (!response.ok)
        await classificationResponseError(response, "export_failed");
      const blob = await response.blob();
      if (exportClientID !== clientScopeRef.current) return;
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = "rarity-billing-export.csv";
      link.click();
      URL.revokeObjectURL(url);
      setState("ready");
      setMessage(
        `Billing CSV exported (${response.headers.get("X-Rarity-Export-ID") ?? "recorded"}).`,
      );
    } catch (cause) {
      if (exportClientID !== clientScopeRef.current) return;
      const entryID = recoveryTimeEntryID(cause);
      if (isClassificationCreateError(cause) && entryID) {
        setState("ready");
        setClassificationRecovery({
          entryID,
          request: Date.now(),
          message:
            "Classification is required before export. Update the blocking time entry and try again.",
        });
      } else setState("error");
    }
  }

  const scopedEntries = loadedClientID === clientID ? entries : [];

  return (
    <Page
      eyebrow="Approval-before-export"
      title="Billing review"
      description="Review billable time for the active Client, then export an audited CSV from approved entries."
    >
      <div className="operations-health">
        {state === "loading" ? (
          <StatePanel
            state="loading"
            title="Loading pending time entries"
            description="Retrieving billable time awaiting review."
          />
        ) : null}
        {state === "error" ? (
          <StatePanel
            state="error"
            title="Billing review is unavailable"
            description="Verify the active Client, dates, and approval state."
            supportCode="BILLING-REVIEW"
          />
        ) : null}
        {message ? (
          <Notice tone="success" title="Billing review updated">
            {message}
          </Notice>
        ) : null}
        {classificationRecovery ? (
          <Notice tone="warning" title="Classification needs attention" urgent>
            {classificationRecovery.message}
          </Notice>
        ) : null}
        <section aria-labelledby="pending-time-heading">
          <div className="billing-section-heading">
            <div>
              <h2 id="pending-time-heading">Pending approval</h2>
              <p>Reasoned decisions are recorded in the audit trail.</p>
            </div>
            <div
              className="billing-summary"
              aria-label="Approval queue summary"
            >
              <span>
                <strong>{scopedEntries.length}</strong>
                entries
              </span>
              <span>
                <strong>
                  {formatDuration(
                    scopedEntries.reduce(
                      (total, entry) => total + entry.duration_seconds,
                      0,
                    ),
                  )}
                </strong>
                pending
              </span>
            </div>
          </div>
          {state === "ready" && !scopedEntries.length ? (
            <p>No time entries are awaiting approval.</p>
          ) : null}
          {scopedEntries.length ? (
            <div className="table-scroll">
              <table>
                <thead>
                  <tr>
                    <th scope="col">Work record</th>
                    <th scope="col">Technician</th>
                    <th scope="col">Started</th>
                    <th scope="col">Duration</th>
                    <th scope="col">Note</th>
                    <th scope="col">Classification</th>
                    {canApprove ? <th scope="col">Decision</th> : null}
                  </tr>
                </thead>
                <tbody>
                  {scopedEntries.map((entry) => (
                    <tr key={entry.id}>
                      <td>{entry.work_record_id}</td>
                      <td>{entry.technician_id}</td>
                      <td>{new Date(entry.started_at).toLocaleString()}</td>
                      <td>{formatDuration(entry.duration_seconds)}</td>
                      <td>
                        <span className="billing-note">
                          {entry.note || "No note"}
                          <StatusBadge
                            tone={entry.billable ? "success" : "neutral"}
                          >
                            {entry.billable ? "Billable" : "Non-billable"}
                          </StatusBadge>
                        </span>
                      </td>
                      <td>
                        <ObjectTagSummary
                          clientID={clientID}
                          target={{
                            objectType: "time_entry",
                            objectId: entry.id,
                          }}
                        />
                        <DeferredObjectTagEditor
                          api={createClassificationAPI(
                            globalThis.fetch,
                            clientID,
                          )}
                          clientID={clientID}
                          target={{
                            objectType: "time_entry",
                            objectId: entry.id,
                          }}
                          recoveryRequest={
                            classificationRecovery?.entryID === entry.id
                              ? classificationRecovery.request
                              : 0
                          }
                        />
                      </td>
                      {canApprove ? (
                        <td>
                          <form
                            className="billing-decision-form"
                            onSubmit={(event) => {
                              event.preventDefault();
                              const form = new FormData(event.currentTarget);
                              const submitter = (
                                event.nativeEvent as SubmitEvent
                              ).submitter as HTMLButtonElement;
                              void decide(
                                entry,
                                submitter.value as "approved" | "rejected",
                                form.get("reason"),
                              );
                            }}
                          >
                            <label>
                              Reason
                              <input name="reason" required />
                            </label>
                            <button
                              name="decision"
                              value="approved"
                              type="submit"
                            >
                              Approve
                            </button>
                            <button
                              name="decision"
                              value="rejected"
                              type="submit"
                            >
                              Reject
                            </button>
                          </form>
                        </td>
                      ) : null}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}
        </section>
        {canExport ? (
          <section
            className="settings-card"
            aria-labelledby="billing-export-heading"
          >
            <h2 id="billing-export-heading">Export approved billing data</h2>
            <p>
              The through date is exclusive. Any unapproved billable entry in
              the selected period blocks export.
            </p>
            <form onSubmit={exportCSV}>
              <DateRangePicker
                label="Billing period"
                start={exportRange.start}
                end={exportRange.end}
                onChange={setExportRange}
              />
              <button
                type="submit"
                disabled={
                  !exportRange.start ||
                  !exportRange.end ||
                  exportRange.end < exportRange.start
                }
              >
                Download audited CSV
              </button>
            </form>
            {classificationRecovery &&
            !scopedEntries.some(
              (entry) => entry.id === classificationRecovery.entryID,
            ) ? (
              <DeferredObjectTagEditor
                api={createClassificationAPI(globalThis.fetch, clientID)}
                clientID={clientID}
                target={{
                  objectType: "time_entry",
                  objectId: classificationRecovery.entryID,
                }}
                recoveryRequest={classificationRecovery.request}
              />
            ) : null}
          </section>
        ) : null}
      </div>
    </Page>
  );
}

function recoveryTimeEntryID(cause: unknown) {
  const recoveryURL =
    cause instanceof Error && "recoveryURL" in cause
      ? (cause as ClassificationAPIError).recoveryURL
      : undefined;
  const matched = recoveryURL?.match(
    /^\/api\/v1\/objects\/time_entry\/([^/]+)\/tags$/,
  );
  return matched?.[1] ? decodeURIComponent(matched[1]) : undefined;
}

function formatDuration(seconds: number) {
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  return `${hours}h ${minutes}m`;
}
