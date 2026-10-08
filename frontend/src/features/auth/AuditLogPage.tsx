import { useEffect, useState } from "react";

import { Page, StatePanel } from "../../design-system";
type AuditEntry = {
  id: string;
  occurred_at: string;
  client_id?: string;
  actor_type: string;
  actor_id: string;
  action: string;
  subject_type: string;
  subject_id: string;
  subject_version: number;
  source: string;
  reason?: string;
  correlation_id: string;
};

export function AuditLogPage() {
  const [entries, setEntries] = useState<AuditEntry[]>([]);
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");

  useEffect(() => {
    const controller = new AbortController();
    void fetch("/api/v1/admin/audit?limit=100", {
      credentials: "same-origin",
      signal: controller.signal,
    })
      .then(async (response) => {
        if (!response.ok) throw new Error(`audit_${response.status}`);
        return (await response.json()) as AuditEntry[];
      })
      .then((found) => {
        setEntries(found);
        setState("ready");
      })
      .catch(() => {
        if (!controller.signal.aborted) setState("error");
      });
    return () => controller.abort();
  }, []);

  return (
    <Page
      eyebrow="Administration"
      title="Audit ledger"
      description="The newest append-only authorization and business mutation evidence visible to your current tenant scope."
    >
      <div className="audit-log">
        {state === "loading" ? (
          <StatePanel
            state="loading"
            title="Loading audit evidence"
            description="Retrieving append-only authorization and mutation records."
          />
        ) : null}
        {state === "error" ? (
          <StatePanel
            state="permission"
            title="Audit evidence is unavailable"
            description="You may not have audit access in the current tenant scope."
          />
        ) : null}
        {state === "ready" && !entries.length ? (
          <StatePanel
            state="empty"
            title="No audit evidence"
            description="No append-only evidence is available in this scope."
          />
        ) : null}
        {entries.length ? (
          <div className="audit-table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Time</th>
                  <th>Action</th>
                  <th>Subject</th>
                  <th>Actor</th>
                  <th>Reason</th>
                </tr>
              </thead>
              <tbody>
                {entries.map((entry) => (
                  <tr key={entry.id}>
                    <td>
                      <time dateTime={entry.occurred_at}>
                        {new Date(entry.occurred_at).toLocaleString()}
                      </time>
                      <small>{entry.source}</small>
                    </td>
                    <td>{entry.action}</td>
                    <td>
                      {entry.subject_type} v{entry.subject_version}
                      <small>{entry.subject_id}</small>
                    </td>
                    <td>
                      {entry.actor_type}
                      <small>{entry.actor_id}</small>
                    </td>
                    <td>{entry.reason || "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </div>
    </Page>
  );
}
