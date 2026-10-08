import type { ReactNode } from "react";

export function Disclosure({
  summary,
  children,
  open = false,
}: {
  summary: ReactNode;
  children: ReactNode;
  open?: boolean;
}) {
  return (
    <details className="rti-disclosure" open={open}>
      <summary>{summary}</summary>
      <div className="rti-disclosure__body">{children}</div>
    </details>
  );
}
