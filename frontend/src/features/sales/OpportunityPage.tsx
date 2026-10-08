import { useRef, useState } from "react";

import { Button, Notice, Page, Select, Textarea } from "../../design-system";
import { formatMoney } from "./view-model";
import type { OpportunityWorkspace } from "./types";
import "./sales.css";

type OpportunityPageProps = {
  opportunity: OpportunityWorkspace;
  onTransition: (stageID: string, expectedVersion: number) => Promise<unknown>;
};

export function OpportunityPage({
  opportunity,
  onTransition,
}: OpportunityPageProps) {
  const [note, setNote] = useState("");
  const [proposalID, setProposalID] = useState(
    opportunity.proposalVersions[0]?.id ?? "",
  );
  const [error, setError] = useState("");
  const [pending, setPending] = useState(false);
  const proposalRef = useRef<HTMLSelectElement>(null);

  async function moveToNextStage() {
    const next = opportunity.nextStage;
    if (!next) return;
    if (next.requiresProposal && !proposalID) {
      setError("A proposal is required for this stage.");
      proposalRef.current?.focus();
      return;
    }
    setError("");
    setPending(true);
    try {
      await onTransition(next.id, opportunity.version);
    } catch {
      setError(
        "This opportunity changed. Review the latest version and retry.",
      );
    } finally {
      setPending(false);
    }
  }

  return (
    <Page
      eyebrow={opportunity.id}
      title={opportunity.name}
      description={`${opportunity.clientName} · ${opportunity.ownerName} · ${opportunity.teamName}`}
      actions={
        <div className="record-value">
          <strong>
            {formatMoney(opportunity.expectedValueMinor, opportunity.currency)}
          </strong>
          <span>Expected close {opportunity.expectedCloseOn}</span>
        </div>
      }
    >
      <div className="sales-page">
        <section className="stage-strip" aria-label="Pipeline position">
          <div>
            <span>Current stage</span>
            <strong>{opportunity.stage.name}</strong>
            <small>{opportunity.stage.ageDays} days in stage</small>
          </div>
          <div>
            <span>Probability</span>
            <strong>{opportunity.stage.probability}%</strong>
            <small>
              Weighted{" "}
              {formatMoney(
                Math.round(
                  (opportunity.expectedValueMinor *
                    opportunity.stage.probability) /
                    100,
                ),
                opportunity.currency,
              )}
            </small>
          </div>
          {opportunity.nextStage ? (
            <Button
              className="sales-primary"
              disabled={pending}
              loading={pending}
              loadingLabel={`Moving to ${opportunity.nextStage.name}`}
              intent="primary"
              onClick={moveToNextStage}
            >
              {`Move to ${opportunity.nextStage.name}`}
            </Button>
          ) : null}
        </section>

        {error ? (
          <Notice tone="danger" title="Opportunity could not be updated" urgent>
            {error}
          </Notice>
        ) : null}

        <div className="sales-grid">
          <section className="sales-panel" aria-labelledby="activity-heading">
            <div className="panel-heading">
              <div>
                <h2 id="activity-heading">Activity</h2>
                <p>Commercial history and next actions</p>
              </div>
            </div>
            <label className="sales-field">
              <span>Opportunity note</span>
              <Textarea
                value={note}
                onChange={(event) => setNote(event.target.value)}
                placeholder="Add context for the sales team"
              />
            </label>
            <ol className="activity-list">
              {opportunity.activities.map((activity) => (
                <li key={activity.id}>
                  <time>{activity.at}</time>
                  <p>{activity.text}</p>
                </li>
              ))}
            </ol>
          </section>

          <aside
            className="sales-panel sales-context"
            aria-label="Opportunity details"
          >
            <label className="sales-field">
              <span>Proposal</span>
              <Select
                ref={proposalRef}
                value={proposalID}
                onChange={(event) => setProposalID(event.target.value)}
                aria-invalid={error.includes("proposal")}
              >
                <option value="">No proposal selected</option>
                {opportunity.proposalVersions.map((version) => (
                  <option key={version.id} value={version.id}>
                    Version {version.version} · {version.state}
                  </option>
                ))}
              </Select>
            </label>
            <section aria-labelledby="tasks-heading">
              <h2 id="tasks-heading">Tasks</h2>
              <ul className="task-list">
                {opportunity.tasks.map((task) => (
                  <li key={task.id}>
                    <span aria-hidden="true">{task.completed ? "✓" : "○"}</span>
                    <span>{task.title}</span>
                  </li>
                ))}
              </ul>
            </section>
          </aside>
        </div>
      </div>
    </Page>
  );
}
