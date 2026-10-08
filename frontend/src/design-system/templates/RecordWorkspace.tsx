import type { ReactNode } from "react";

export function RecordWorkspace({
  identity,
  status,
  actions,
  activity,
  context,
  composer,
}: {
  identity: ReactNode;
  status?: ReactNode;
  actions?: ReactNode;
  activity: ReactNode;
  context: ReactNode;
  composer?: ReactNode;
}) {
  return (
    <section className="rti-record-workspace">
      <header>
        <div>{identity}</div>
        {status}
        {actions ? <div>{actions}</div> : null}
      </header>
      <div className="rti-record-workspace__body">
        <div className="rti-record-workspace__activity">
          {activity}
          {composer ? (
            <div className="rti-record-workspace__composer">{composer}</div>
          ) : null}
        </div>
        <aside aria-label="Record context">{context}</aside>
      </div>
    </section>
  );
}
