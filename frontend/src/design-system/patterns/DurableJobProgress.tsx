import type { ReactNode } from "react";

import {
  StatusBadge,
  type FeedbackTone,
} from "../components/feedback/StatusBadge";

export type DurableJobState =
  "queued" | "running" | "succeeded" | "failed" | "cancelled";

const jobLabels: Record<DurableJobState, string> = {
  queued: "Queued",
  running: "Running",
  succeeded: "Succeeded",
  failed: "Failed",
  cancelled: "Cancelled",
};

const jobTones: Record<DurableJobState, FeedbackTone> = {
  queued: "neutral",
  running: "info",
  succeeded: "success",
  failed: "danger",
  cancelled: "warning",
};

export function DurableJobProgress({
  state,
  label,
  queuedAt,
  startedAt,
  completedAt,
  progress,
  supportCode,
  actions,
}: {
  state: DurableJobState;
  label: string;
  queuedAt?: string;
  startedAt?: string;
  completedAt?: string;
  progress?: { completed: number; total: number };
  supportCode?: string;
  actions?: ReactNode;
}) {
  return (
    <section className="rti-job" aria-label={`${label} job status`}>
      <header>
        <div>
          <h3>{label}</h3>
          <StatusBadge tone={jobTones[state]}>{jobLabels[state]}</StatusBadge>
        </div>
        {actions ? <div>{actions}</div> : null}
      </header>
      {progress ? (
        <div className="rti-job__progress">
          <progress value={progress.completed} max={progress.total} />
          <span>
            {progress.completed} of {progress.total} complete
          </span>
        </div>
      ) : null}
      <dl className="rti-job__metadata">
        {queuedAt ? (
          <>
            <dt>Queued</dt>
            <dd>{queuedAt}</dd>
          </>
        ) : null}
        {startedAt ? (
          <>
            <dt>Started</dt>
            <dd>{startedAt}</dd>
          </>
        ) : null}
        {completedAt ? (
          <>
            <dt>Completed</dt>
            <dd>{completedAt}</dd>
          </>
        ) : null}
        {supportCode ? (
          <>
            <dt>Support code</dt>
            <dd>
              <code>{supportCode}</code>
            </dd>
          </>
        ) : null}
      </dl>
    </section>
  );
}
