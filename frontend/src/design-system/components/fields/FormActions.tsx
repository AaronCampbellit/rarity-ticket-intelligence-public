import type { ReactNode } from "react";

export function FormActions({ children }: { children: ReactNode }) {
  return <div className="rti-form-actions">{children}</div>;
}
