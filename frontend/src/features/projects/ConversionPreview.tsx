import { Button, Page } from "../../design-system";
import { formatMoney, formatMinutes } from "./view-model";
import type { ConversionPreviewModel } from "./types";
import "./projects.css";

type Selection = {
  previewHash: string;
  selectedTaskIDs: string[];
  taskVersions: Record<string, number>;
};

export function ConversionPreview({
  preview,
  onConvert,
}: {
  preview: ConversionPreviewModel;
  onConvert: (selection: Selection) => void;
}) {
  const selectedTaskIDs = preview.tasks.map((task) => task.id);
  const taskVersions = Object.fromEntries(
    preview.tasks.map((task) => [task.id, task.version]),
  );

  return (
    <Page
      className="project-page"
      eyebrow="Conversion preview"
      title={preview.projectName}
      description="Review Client matching, Phase mapping, tasks, hours, and budget before creating the Project."
      actions={
        <Button
          intent="primary"
          onClick={() =>
            onConvert({
              previewHash: preview.hash,
              selectedTaskIDs,
              taskVersions,
            })
          }
        >
          Create project
        </Button>
      }
    >
      <section className="conversion-summary" aria-label="Conversion summary">
        <div>
          <span>Client</span>
          <strong>{preview.client.name}</strong>
          <small>
            {preview.client.action === "match"
              ? "Matched existing Client"
              : "Create from Prospect"}
          </small>
        </div>
        <div>
          <span>Original budget</span>
          <strong>
            {formatMoney(preview.originalBudgetMinor, preview.currency)}
          </strong>
          <small>Locked to Proposal Version</small>
        </div>
        <div>
          <span>Planned work</span>
          <strong>{formatMinutes(preview.plannedMinutes)}</strong>
          <small>{preview.phases.length} ordered Phases</small>
        </div>
      </section>

      <div className="conversion-grid">
        <section
          className="project-panel"
          aria-labelledby="phase-mapping-title"
        >
          <div className="project-panel-heading">
            <div>
              <h2 id="phase-mapping-title">Phase mapping</h2>
              <p>Every Proposal Line is assigned exactly once.</p>
            </div>
          </div>
          <ol className="phase-preview-list">
            {preview.phases.map((phase) => (
              <li key={phase.position}>
                <span>{phase.position}</span>
                <div>
                  <strong>{phase.name}</strong>
                  <small>
                    {phase.proposalLineIDs.length} Proposal Line
                    {phase.proposalLineIDs.length === 1 ? "" : "s"}
                  </small>
                </div>
                <div>
                  <strong>{formatMinutes(phase.plannedMinutes)}</strong>
                  <small>
                    {formatMoney(phase.budgetMinor, preview.currency)}
                  </small>
                </div>
              </li>
            ))}
          </ol>
        </section>

        <aside className="project-panel" aria-labelledby="task-selection-title">
          <div className="project-panel-heading">
            <div>
              <h2 id="task-selection-title">Opportunity tasks</h2>
              <p>Completed sales history stays on the Opportunity.</p>
            </div>
          </div>
          <fieldset className="conversion-tasks">
            <legend className="sr-only">Tasks to move</legend>
            {preview.tasks.map((task) => (
              <label key={task.id}>
                <input
                  type="checkbox"
                  aria-label={task.title}
                  checked
                  disabled
                  readOnly
                />
                <span>
                  <strong>{task.title}</strong>
                  <small>Included in reviewed conversion</small>
                </span>
              </label>
            ))}
          </fieldset>
        </aside>
      </div>
    </Page>
  );
}
