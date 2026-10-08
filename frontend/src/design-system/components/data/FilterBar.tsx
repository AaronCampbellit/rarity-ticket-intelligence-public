import type { ReactNode } from "react";

import { Button } from "../actions/Button";

export function FilterBar({
  activeCount,
  onReset,
  children,
}: {
  activeCount: number;
  onReset: () => void;
  children: ReactNode;
}) {
  return (
    <section className="rti-filter-bar" aria-label="Filters">
      <div className="rti-filter-bar__controls">{children}</div>
      {activeCount ? (
        <Button intent="tertiary" onClick={onReset}>
          Reset {activeCount} {activeCount === 1 ? "filter" : "filters"}
        </Button>
      ) : null}
    </section>
  );
}
