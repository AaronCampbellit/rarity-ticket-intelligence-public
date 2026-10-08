import { useRef, useState } from "react";

import { useWorkspaceDirtyState } from "../../design-system";
import { formatMoney, formatMinutes } from "./view-model";
import type { ChangeOrderWorkspace } from "./types";
import "./projects.css";

type OverrideEvidence = {
  versionID: string;
  expectedVersion: number;
  reason: string;
};

export function ChangeOrderEditor({
  changeOrder,
  workspaceOwnerID = "route:project",
  onOverride,
  onApprove,
}: {
  changeOrder: ChangeOrderWorkspace;
  workspaceOwnerID?: string;
  onOverride: (evidence: OverrideEvidence) => Promise<boolean>;
  onApprove?: () => void;
}) {
  const [reason, setReason] = useState("");
  const [error, setError] = useState("");
  useWorkspaceDirtyState(Boolean(reason.trim()), workspaceOwnerID);
  const reasonRef = useRef<HTMLTextAreaElement>(null);
  const version = changeOrder.currentVersion;
  if (!version) return null;
  const versionID = version.id;

  async function overrideApproval() {
    const normalized = reason.trim();
    if (!normalized) {
      setError("Enter a reason for the override.");
      reasonRef.current?.focus();
      return;
    }
    setError("");
    const saved = await onOverride({
      versionID,
      expectedVersion: changeOrder.version,
      reason: normalized,
    });
    if (saved) setReason("");
  }

  return (
    <section
      className="project-panel change-order"
      aria-labelledby="change-order-title"
    >
      <div className="project-panel-heading">
        <div>
          <p className="project-record-id">{changeOrder.displayID}</p>
          <h2 id="change-order-title">
            Change Order Version {version.version}
          </h2>
          <p>{version.description}</p>
        </div>
        <span className={`change-state ${changeOrder.state}`}>
          {changeOrder.state}
        </span>
      </div>
      <dl className="change-deltas">
        <div>
          <dt>Revenue</dt>
          <dd>{formatMoney(version.revenueDeltaMinor, version.currency)}</dd>
        </div>
        <div>
          <dt>Cost</dt>
          <dd>{formatMoney(version.costDeltaMinor, version.currency)}</dd>
        </div>
        <div>
          <dt>Planned work</dt>
          <dd>{formatMinutes(version.laborDeltaMinutes)}</dd>
        </div>
      </dl>
      {changeOrder.state === "issued" ? (
        <>
          <label className="project-field">
            <span>Override reason</span>
            <textarea
              ref={reasonRef}
              value={reason}
              onChange={(event) => setReason(event.target.value)}
              aria-invalid={Boolean(error)}
              placeholder="Record why normal approval is being overridden"
            />
          </label>
          {error ? (
            <p className="project-error" role="alert">
              {error}
            </p>
          ) : null}
          <div className="change-actions">
            <button type="button" onClick={onApprove}>
              Record normal approval
            </button>
            <button type="button" onClick={() => void overrideApproval()}>
              Override approval
            </button>
          </div>
        </>
      ) : null}
      {changeOrder.decisions.length ? (
        <ol className="decision-history">
          {changeOrder.decisions.map((decision) => (
            <li key={decision.id}>
              <strong>{decision.override ? "Override" : "Approval"}</strong>
              <span>{decision.reason}</span>
              <small>
                {decision.decidedBy} · {decision.decidedAt}
              </small>
            </li>
          ))}
        </ol>
      ) : null}
    </section>
  );
}
