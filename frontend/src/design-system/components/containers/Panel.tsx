import { useId, type ElementType, type ReactNode } from "react";

import "./containers.css";

export function Panel({
  title,
  description,
  actions,
  children,
  as: Component = "section",
}: {
  title: string;
  description?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
  as?: ElementType;
}) {
  const titleID = useId();
  return (
    <Component className="rti-panel" aria-labelledby={titleID}>
      <header className="rti-panel__header">
        <div>
          <h2 id={titleID}>{title}</h2>
          {description ? <p>{description}</p> : null}
        </div>
        {actions ? <div className="rti-panel__actions">{actions}</div> : null}
      </header>
      <div className="rti-panel__body">{children}</div>
    </Component>
  );
}
