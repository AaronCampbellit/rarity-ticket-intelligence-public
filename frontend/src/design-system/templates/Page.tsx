import { useId, type ReactNode } from "react";

import { useMainContentID } from "../foundations/mainContent";
import "./page.css";

export type PageProps = {
  className?: string;
  eyebrow?: string;
  title: string;
  description?: ReactNode;
  actions?: ReactNode;
  tabs?: ReactNode;
  children: ReactNode;
};

export function Page({
  className = "",
  eyebrow,
  title,
  description,
  actions,
  tabs,
  children,
}: PageProps) {
  const titleID = useId();
  const mainContentID = useMainContentID();
  return (
    <main
      id={mainContentID}
      className={`rti-page ${className}`.trim()}
      aria-labelledby={titleID}
    >
      <header className="rti-page__header">
        <div>
          {eyebrow ? <p className="rti-page__eyebrow">{eyebrow}</p> : null}
          <h1 id={titleID}>{title}</h1>
          {description ? (
            <div className="rti-page__description">{description}</div>
          ) : null}
        </div>
        {actions ? <div className="rti-page__actions">{actions}</div> : null}
      </header>
      {tabs ? <div className="rti-page__tabs">{tabs}</div> : null}
      <div className="rti-page__content">{children}</div>
    </main>
  );
}
