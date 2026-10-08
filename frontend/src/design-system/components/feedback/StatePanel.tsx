import {
  CircleAlert,
  CircleOff,
  Clock3,
  LockKeyhole,
  RefreshCw,
} from "lucide-react";
import { useId, type ReactNode } from "react";

export type SharedState =
  "loading" | "empty" | "error" | "permission" | "conflict";

export type StatePanelProps = {
  state: SharedState;
  title: string;
  description?: ReactNode;
  action?: ReactNode;
  supportCode?: string;
};

const stateIcons = {
  loading: Clock3,
  empty: CircleOff,
  error: CircleAlert,
  permission: LockKeyhole,
  conflict: RefreshCw,
};

export function StatePanel({
  state,
  title,
  description,
  action,
  supportCode,
}: StatePanelProps) {
  const titleID = useId();
  const Icon = stateIcons[state];
  const role =
    state === "loading" ? "status" : state === "error" ? "alert" : undefined;

  return (
    <section
      className="rti-state-panel"
      data-state={state}
      role={role}
      aria-labelledby={titleID}
    >
      <Icon size={24} aria-hidden="true" />
      <div>
        <h2 id={titleID}>{title}</h2>
        {description ? <p>{description}</p> : null}
        {supportCode ? (
          <p className="rti-state-panel__support">
            Support code: <code>{supportCode}</code>
          </p>
        ) : null}
        {action ? (
          <div className="rti-state-panel__action">{action}</div>
        ) : null}
      </div>
    </section>
  );
}
