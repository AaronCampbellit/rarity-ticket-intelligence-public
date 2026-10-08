import type { ReactNode } from "react";

export function CommandBar({ children }: { children: ReactNode }) {
  return <div className="rti-command-bar">{children}</div>;
}
