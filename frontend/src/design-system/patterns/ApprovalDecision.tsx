import { StatusBadge } from "../components/feedback/StatusBadge";

export function ApprovalDecision({
  actor,
  decision,
  reason,
  decidedAt,
  override = false,
}: {
  actor: string;
  decision: string;
  reason?: string;
  decidedAt: string;
  override?: boolean;
}) {
  return (
    <article className="rti-approval">
      <header>
        <strong>{decision}</strong>
        {override ? <StatusBadge tone="warning">Override</StatusBadge> : null}
      </header>
      <dl>
        <dt>Actor</dt>
        <dd>{actor}</dd>
        <dt>Decided</dt>
        <dd>{decidedAt}</dd>
        {reason ? (
          <>
            <dt>Reason</dt>
            <dd>{reason}</dd>
          </>
        ) : null}
      </dl>
    </article>
  );
}
