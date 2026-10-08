import type { ReactNode } from "react";
import { Maximize2 } from "lucide-react";
import { ObjectTagSummary } from "../classification/ObjectTagSummary";

import { CapacityView } from "./CapacityView";
import { FinancialSummary } from "./FinancialSummary";
import { formatMoney, formatMinutes } from "./view-model";
import type { ProjectWorkspace } from "./types";
import "./projects.css";

export function ProjectPage({
  project,
  clientID,
  onPreviewTask,
  onOpenTask,
  actions,
  children,
}: {
  actions?: ReactNode;
  children?: ReactNode;
  project: ProjectWorkspace;
  clientID: string;
  onPreviewTask?: (taskID: string, title: string) => void;
  onOpenTask?: (taskID: string, title: string) => void;
}) {
  return (
    <Page
      eyebrow={project.displayID}
      title={project.name}
      description={`${project.clientName} · Proposal Version ${project.originalProposalVersion} · ${project.plannedStart} to ${project.plannedEnd}`}
      actions={
        <>
          <StatusBadge>{project.lifecycleState}</StatusBadge>
          {actions}
        </>
      }
    >
      <div className="project-page">
        <ObjectTagSummary
          clientID={clientID}
          target={{ objectType: "project", objectId: project.id }}
        />
        <section
          className="project-panel phase-plan"
          aria-labelledby="phase-plan-title"
        >
          <div className="project-panel-heading">
            <div>
              <h2 id="phase-plan-title">Delivery plan</h2>
              <p>
                Ordered Phases, owners, work, deliverables, and task progress
              </p>
            </div>
          </div>
          <ol>
            {project.phases.map((phase) => (
              <li key={phase.id}>
                <span className="phase-position">{phase.position}</span>
                <div className="phase-main">
                  <header>
                    <div>
                      <strong>{phase.name}</strong>
                      <span>
                        {phase.ownerName} ·{" "}
                        {phase.participatingTeams.join(", ")}
                      </span>
                    </div>
                    <span>{phase.state}</span>
                  </header>
                  <div className="phase-progress">
                    <span>
                      {formatMinutes(phase.actualMinutes)} actual of{" "}
                      {formatMinutes(phase.plannedMinutes)} planned
                    </span>
                    <span>
                      {phase.tasks.filter((task) => task.completed).length}/
                      {phase.tasks.length} tasks
                    </span>
                  </div>
                  {phase.financials ? (
                    <dl className="phase-financials">
                      <div>
                        <dt>Recognized work</dt>
                        <dd>
                          {formatMoney(
                            phase.financials.billableWorkMinor,
                            phase.financials.currency,
                          )}
                        </dd>
                      </div>
                      <div>
                        <dt>Actual labor</dt>
                        <dd>
                          {phase.financials.actualLaborComplete
                            ? formatMoney(
                                phase.financials.actualLaborMinor,
                                phase.financials.currency,
                              )
                            : "Rate required"}
                        </dd>
                      </div>
                      <div>
                        <dt>Recognized profit</dt>
                        <dd>
                          {phase.financials.profitAvailable
                            ? formatMoney(
                                phase.financials.profitMinor,
                                phase.financials.currency,
                              )
                            : "Rate required"}
                        </dd>
                      </div>
                    </dl>
                  ) : null}
                  <ul>
                    {phase.tasks.map((task) => (
                      <li key={task.id}>
                        <button
                          type="button"
                          className="project-task-target"
                          onClick={() => onPreviewTask?.(task.id, task.title)}
                          onDoubleClick={() =>
                            onOpenTask?.(task.id, task.title)
                          }
                        >
                          <span aria-hidden="true">
                            {task.completed ? "✓" : "○"}
                          </span>
                          <span>{task.title}</span>
                          <small>
                            {task.ownerName}
                            {task.subtasks
                              ? ` · ${task.subtasks} subtasks`
                              : ""}
                            {task.plannedMinutes
                              ? ` · ${formatMinutes(task.plannedMinutes)} planned`
                              : ""}
                            {task.actualMinutes
                              ? ` · ${formatMinutes(task.actualMinutes)} actual`
                              : ""}
                          </small>
                          <ObjectTagSummary
                            clientID={clientID}
                            target={{ objectType: "task", objectId: task.id }}
                          />
                        </button>
                        {onOpenTask ? (
                          <button
                            type="button"
                            className="project-task-open"
                            aria-label={`Open task ${task.title}`}
                            onClick={() => onOpenTask(task.id, task.title)}
                          >
                            <Maximize2 size={15} aria-hidden="true" />
                          </button>
                        ) : null}
                      </li>
                    ))}
                  </ul>
                </div>
              </li>
            ))}
          </ol>
        </section>
        <section
          className="project-panel project-task-plan"
          aria-labelledby="project-task-title"
        >
          <div className="project-panel-heading">
            <div>
              <h2 id="project-task-title">Project tasks</h2>
              <p>Work moved from the accepted Opportunity into delivery.</p>
            </div>
          </div>
          {project.projectTasks?.length ? (
            <ul>
              {project.projectTasks.map((task) => (
                <li key={task.id}>
                  <button
                    type="button"
                    className="project-task-target"
                    onClick={() => onPreviewTask?.(task.id, task.title)}
                    onDoubleClick={() => onOpenTask?.(task.id, task.title)}
                  >
                    <span aria-hidden="true">
                      {task.status === "completed" ? "✓" : "○"}
                    </span>
                    <span>
                      <strong>{task.title}</strong>
                      <small>
                        {task.ownerName}
                        {task.subtasks ? ` · ${task.subtasks} subtasks` : ""}
                        {task.plannedMinutes
                          ? ` · ${formatMinutes(task.plannedMinutes)} planned`
                          : ""}
                        {task.actualMinutes
                          ? ` · ${formatMinutes(task.actualMinutes)} actual`
                          : ""}
                      </small>
                    </span>
                    <span>{task.status}</span>
                    <ObjectTagSummary
                      clientID={clientID}
                      target={{ objectType: "task", objectId: task.id }}
                    />
                  </button>
                  {onOpenTask ? (
                    <button
                      type="button"
                      className="project-task-open"
                      aria-label={`Open task ${task.title}`}
                      onClick={() => onOpenTask(task.id, task.title)}
                    >
                      <Maximize2 size={15} aria-hidden="true" />
                    </button>
                  ) : null}
                </li>
              ))}
            </ul>
          ) : (
            <p>No project-level tasks are pending.</p>
          )}
        </section>
        <div className="project-dashboard-grid">
          {project.resourcePlans?.length ? (
            <section className="project-panel" aria-labelledby="staffing-title">
              <div className="project-panel-heading">
                <div>
                  <h2 id="staffing-title">Staffing plan</h2>
                  <p>Persisted role and team allocations for this Project.</p>
                </div>
              </div>
              <ul className="staffing-plan-list">
                {project.resourcePlans.map((plan) => (
                  <li key={plan.id}>
                    <strong>{plan.resourceName}</strong>
                    <span>
                      {plan.resourceType} · {plan.startsOn} to {plan.endsOn}
                    </span>
                    <span>{formatMinutes(plan.plannedMinutes)}</span>
                  </li>
                ))}
              </ul>
            </section>
          ) : project.capacity.length ? (
            <CapacityView resources={project.capacity} />
          ) : (
            <section className="project-panel" aria-labelledby="staffing-title">
              <h2 id="staffing-title">Staffing plan</h2>
              <p>No resource plans have been assigned.</p>
            </section>
          )}
          <section
            className="project-panel"
            aria-labelledby="project-cost-title"
          >
            <div className="project-panel-heading">
              <div>
                <h2 id="project-cost-title">Project costs</h2>
                <p>Incurred and committed non-labor costs.</p>
              </div>
            </div>
            {project.costActuals?.length ? (
              <ul className="staffing-plan-list">
                {project.costActuals.map((cost) => (
                  <li key={cost.id}>
                    <strong>{cost.description}</strong>
                    <span>
                      {cost.costType} · {cost.incurredAt}
                      {cost.committed ? " · committed" : " · incurred"}
                    </span>
                    <span>{formatMoney(cost.amountMinor, cost.currency)}</span>
                  </li>
                ))}
              </ul>
            ) : (
              <p>No Project costs have been recorded.</p>
            )}
          </section>
          {project.financialsVisible === false ? (
            <section
              className="project-panel"
              aria-labelledby="financial-title"
            >
              <h2 id="financial-title">Project financials</h2>
              <p>Additional permission is required to view financial data.</p>
            </section>
          ) : (
            <FinancialSummary summary={project.financials} />
          )}
        </div>
        {children ? (
          <section
            className="project-support"
            aria-label="Project collaboration and settings"
          >
            {children}
          </section>
        ) : null}
      </div>
    </Page>
  );
}
import { Page, StatusBadge } from "../../design-system";
