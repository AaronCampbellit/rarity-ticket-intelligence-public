import type { ReactNode } from "react";

import type { PageFamily } from "../../app/routes";
export type DirectionPageProps = {
  family: PageFamily;
  context?: ReactNode;
  actions?: ReactNode;
  viewControls?: ReactNode;
  children: ReactNode;
};

export function DirectionPage({
  family,
  context,
  actions,
  viewControls,
  children,
}: DirectionPageProps) {
  return (
    <section
      className="rti-direction-page"
      data-testid="direction-page"
      data-layout="console"
      data-family={family}
    >
      {context ? (
        <aside className="rti-direction-page__context">{context}</aside>
      ) : null}
      {actions || viewControls ? (
        <header className="rti-direction-page__toolbar">
          {viewControls ? (
            <div className="rti-direction-page__views">{viewControls}</div>
          ) : null}
          {actions ? (
            <div className="rti-direction-page__actions">{actions}</div>
          ) : null}
        </header>
      ) : null}
      <div className="rti-direction-page__stage">{children}</div>
    </section>
  );
}
